---
comet_change: add-asset-multi-reference-generation
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-02-add-asset-multi-reference-generation
status: final
---

# 同一资产有序多参考图版本化管理与单候选生成技术设计

## 1. 背景与边界

首版 `add-project-reference-image-assets` 为项目内角色、场景和道具资产落地了单张原始参考图（`referenceImage: { resourceId }`）、版本 CAS 控制与单张手动候选采用闭环。但在实际创作中，单一角色或场景往往需要同时提供正面、侧面、局部服饰或道具特写等多张参考图。

本设计在 `docs/openspec/changes/add-asset-multi-reference-generation/` 的权威规格约束下，将单数 `referenceImage` 合同直接升级为唯一、有界、有序的 `referenceImages: [{ resourceId }]` 数组合同，补齐一次性可重入数据迁移、按稳定顺序加锁的资源安全、服务端可信模型能力准入及真实资产弹窗交互。

## 2. 架构与数据流设计

### 2.1 规范存储结构与 API 合同

1. **版本与实体 JSON 存储**：
   - `AssetVersion.DefinitionJSON` 与实体 `Asset.PayloadJSON`（`data.definition`）统一使用有序数组字段：
     ```json
     {
       "referenceImages": [
         { "resourceId": "res_1" },
         { "resourceId": "res_2" }
       ]
     }
     ```
   - 空切片 `[]`（或 `referenceImages: []`）表示明确无原始参考图；数组中不允许空白 `resourceId` 或重复 `resourceId`，长度上限为应用常量 `MaxProjectAssetReferenceImages = 16`。
   - `preserveReferenceDefinition(currentJSON, nextJSON)`：
     - 拒绝普通 `definition` 编辑请求写入或篡改 `referenceImages`（以及遗留未迁移的 `referenceImage` 字段）；
     - 将当前版本已有的 `referenceImages` 原样复制到新版本 `definition` 中。

2. **HTTP 接口与 DTO**：
   - **专用更新接口**：`PUT /api/projects/:id/assets/:assetId/reference-images`
     - 请求体：`SetProjectReferenceImagesRequest { ReferenceImages []ProjectReferenceImage `json:"referenceImages"`, ExpectedVersionID string `json:"expectedVersionId"` }`
     - 行为：
       - 校验 `ExpectedVersionID` 非空且等于当前 `asset.PrimaryVersionID`（不匹配返回 `409 project_asset_version_changed`）；
       - 校验 `len(ReferenceImages) <= 16`、无重复 `resourceId`，并逐个校验 `projectImageResource(userID, item.ResourceID)`（归属当前用户、`kind=image`、`status=ready`、宽高 > 0、`image/*` MIME）；
       - **同序幂等优化**：若归一化后的 `ReferenceImages` 序列与当前主版本的 `referenceImages` 完全相同，则不创建新版本，直接返回当前 `ProjectAssetSummary`；
       - 否则在事务内按稳定排序锁定所有涉及的 `resourceId` 与当前版本成品 `representations`，调用 `prepareNextCharacterVersion` 创建新版本，保留既有声音绑定、成品 `representations` 与文本设定，仅更新 `definition.referenceImages`。
   - **创建接口**：
     - `POST /api/projects/:id/characters`（`CreateProjectCharacterRequest`）与 `POST /api/projects/:id/asset-entities`（`CreateProjectAssetEntityRequest`）统一接收 `ReferenceImages []ProjectReferenceImage `json:"referenceImages,omitempty"``，移除单数 `ReferenceImage`。
   - **读取摘要 DTO**：
     - `ProjectAssetSummary` 输出 `ReferenceImages []ProjectReferenceImage `json:"referenceImages"``（始终返回非 nil 数组 `[]` 或有序元素列表），移除单数 `ReferenceImage`。

### 2.2 显式一次性可重入迁移

1. **迁移函数 `MigrateLegacyProjectReferenceImages(repo *repository.Repository)`**：
   - 扫描全部 `AssetVersion` 与 `Asset` 记录：
     - 若 JSON 中仅存在有效单数 `referenceImage: { "resourceId": "xxx" }`：校验该资源在 `resources` 表中存在且归属该资产所属用户后，转换为 `referenceImages: [{ "resourceId": "xxx" }]` 并删除 `referenceImage` 键；
     - 若 JSON 中 `referenceImage` 为 `null`/空且无 `referenceImages`：规范化为空数组或移除旧键；
     - 若已迁移为合法 `referenceImages` 且无 `referenceImage`：幂等跳过（可重入）；
     - **异常阻断**：若 JSON 损坏、同时存在 `referenceImage` 与 `referenceImages` 且内容不一致、或引用的 `resourceId` 非法/不存在，汇总到 `ReferenceMigrationReport` 并返回明确错误，事务整体回滚，绝不静默丢弃或猜测合并。
   - 迁移不改变 `AssetVersion.ID`、`Asset.PrimaryVersionID`、历史 `AssetRepresentation` 或在途/待采用任务的 `BaseVersionID`。

### 2.3 生成任务重建、模型能力准入与资源锁

1. **入队前任务准备（`prepareProjectImageTask`）**：
   - 继续强制单候选输出（拒绝 `count/n/batchSize/num_images/sample_count != 1` 及 `sequential_image_generation != disabled`，强制 `config["count"] = "1"`）。
   - 拒绝视频/音频参考与局部重绘遮罩（`len(typed.ReferenceVideos) > 0 || len(typed.ReferenceAudios) > 0 || typed.Mask != nil`）。
   - 按 `meta.ReferenceSource` 从基准版本 `meta.VersionID` 重建有序参考输入：
     - `original`：读取版本 `referenceImages` 有序切片；若为空报错 `"当前版本没有原始参考图"`；若超过 `MaxProjectAssetReferenceImages (16)` 报错拒绝；
     - `current`：读取版本 `primary` 成品图片单元素切片；若无报错 `"当前版本没有成品图片"`；
     - `none`：若客户端传入非空 `typed.ReferenceImages` 则拒绝（`"无参考模式不能携带参考图片"`），重建为空切片。
   - **客户端防篡改核验**：若客户端提交了 `typed.ReferenceImages`，其长度与每个位置的 `storageKey` / `url` / `dataUrl` 必须与服务端按 `ReferenceSource` 解析出的有序资源列表逐项严格一致，否则报错 `"参考图片与所选版本来源不一致"`。
   - 服务端使用权威 `model.Resource` 元数据按序覆写 `input["referenceImages"]`。

2. **服务端可信模型能力准入（`validateProjectImageReferenceCapability`）**：
   - 当 `len(typed.ReferenceImages) > 0` 时：
     - 解析可信模型能力 `profile *ImageCapabilityConfig`：
       - 系统渠道：从 `repo.ChannelModelByKey` + `normalizedChannelModelCapability` 获取服务端持久配置；
       - 自定义渠道：先按 `DefaultImageCapabilityConfig(interfaceType, model)` 获取服务端协议基线；仅当协议已知且基线支持参考图时，若客户端携带 `CapabilityConfig.Image`，其 `References.MaxImages` 只能**收敛（取较小值）**而不能突破服务端协议/模型上限，防止伪造能力绕过准入。
     - 计算有效张数上限 `effectiveMax := min(MaxProjectAssetReferenceImages, profile.References.MaxImages)`；若 `effectiveMax < 1` 或 `len(typed.ReferenceImages) > effectiveMax`，在入队前直接拒绝并返回清晰中文原因。
     - 校验每张参考图的文件字节大小与总请求预算，以及协议实际多图传递支持（例如仅支持单图字段的协议在 `effectiveMax` 层即限制为 1）。

3. **稳定资源锁与引用扫描（`repository/asset_profile.go` & `resource.go`）**：
   - `definitionResourceIDs(raw string)` 同时解析 `referenceImages[].resourceId`（及迁移前遗留 `referenceImage.resourceId`），去重后返回。
   - `LockAssetImageResources` 收集版本 `definition` 中的全部参考图 ID 与成品 `representations` 资源 ID，去重并按 `resourceId` 字典序升序执行 `SELECT ... FOR UPDATE`（SQLite 下事务写锁 + 归属与状态核验），消除两个并发请求以相反顺序排列同一批图片时的死锁风险。
   - 资源物理删除引用检查覆盖所有历史 `AssetVersion.DefinitionJSON`、`Asset.PayloadJSON` 与 `Task.InputJSON` 中的 `referenceImages`。

### 2.4 前端真实弹窗与 API 适配

1. **API 与纯函数（`web/src/services/api/projects.ts` & `project-reference-image.ts`）**：
   - `ProjectAsset` 类型更新为 `referenceImages: Array<{ resourceId: string }>`；
   - `updateProjectAssetReferenceImages(projectId, assetId, { referenceImages, expectedVersionId })` 调用 `PUT /projects/:id/assets/:assetId/reference-images`；
   - `referenceImagesForAsset(asset, source)` 返回 `ReferenceImage[]`（`original` 返回完整有序数组，`current` 返回单元素数组，`none` 返回 `[]`）；
   - `referenceModelError(config, model, referenceCount)`：当 `referenceCount > 0` 时，检查有效上限 `effectiveMax = Math.min(16, capability.references.maxImages)`；若模型不支持参考图或 `referenceCount > effectiveMax`，返回明确提示（例如 `"当前模型最多支持 N 张参考图（当前已选 M 张），请调整参考图或更换模型"`），同时不删除已保存的参考图。

2. **弹窗有序参考图交互（`web/src/pages/projects/detail/project-asset-image-modal.tsx`）**：
   - 支持从素材库多选或上传追加参考图（去重，上限 16 张）；
   - 展示有序参考缩略图列表（标注序号 `#1`, `#2`...），每项提供键盘可达的「上移」「下移」「移除」按钮，顶部提供「清空参考」按钮；
   - 编辑已有资产时，调整顺序、增删或清空后点击「保存参考图集合」调用 `updateProjectAssetReferenceImages`；
   - `ModelRequirements.input.imageCount` 传入实际参考图张数（`source === "original"` 为参考图数组长度，`source === "current"` 为 1，`source === "none"` 为 0）。

## 3. 测试策略

1. **后端测试**：
   - `backend/internal/app/project_reference_image_test.go`：
     - 创建/更新多张有序参考图、刷新保序、清空（`[]`）、同序幂等不升版本、重复资源拒绝、超 16 张拒绝、越权/未就绪资源拒绝、CAS 409 冲突、普通设定更新保留 `referenceImages`、保留角色声音与成品表现；
     - 显式迁移测试：单参考版本与 payload 迁移、空参考迁移、可重入重复执行、双字段不一致阻断报告；
     - 生成准入测试：0/1/3 张按序写入 `input["referenceImages"]`、超出模型 `maxImages` 拒绝、客户端伪造更高 `maxImages` 拒绝、伪造顺序/来源拒绝、反向排序并发加锁与资源删除保护。
2. **前端测试**：
   - `web/test/project-reference-image-api.test.ts`、`web/test/project-reference-image-assets.test.ts`、`web/test/project-asset-image-modal.test.tsx`：
     - 覆盖 `PUT /reference-images` 契约、多参考图选择/上移/下移/移除/清空、模型张数不兼容时保留已保存图片并阻止提交、多参考生成候选与手动采用。
