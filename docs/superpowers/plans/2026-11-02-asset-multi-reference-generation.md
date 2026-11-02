---
change: add-asset-multi-reference-generation
design-doc: docs/superpowers/specs/2026-11-02-asset-multi-reference-generation-design.md
base-ref: e2f6656b802b9f76cac1e8a1b3ed691674045e8c
---

# 同一资产多参考图版本化管理与单候选生成实施计划

> **面向执行者：** 请按任务顺序执行本计划，每项任务勾选与 OpenSpec `tasks.md` 通过 `comet-task` 标识一一映射。

**目标：** 将项目角色、场景与道具资产的单张原始参考图合同升级为唯一、有界（最多 16 张）、有序的 `referenceImages` 集合合同，提供可重入一次性数据迁移、服务端模型能力准入、稳定资源锁与真实弹窗多参考图交互。

**架构：** 后端在 `AssetVersion.DefinitionJSON` 与实体 `payload.data.definition` 中统一保存 `referenceImages: [{ resourceId }]`，通过 `PUT /api/projects/:id/assets/:assetId/reference-images` 执行 CAS 版本演进与同序幂等判断；任务入队前按 `original`/`current`/`none` 重建有序参考输入并校验 `min(16, maxImages)`；前端 `ProjectAssetImageModal` 提供多图选择、上移/下移/移除/清空与模型上限提示。

**技术栈：** Go 1.25、Gin、GORM、SQLite/PostgreSQL、React 19、TypeScript、TanStack Query、Bun Test。

---

## 1. 规范集合与迁移

### Task 1.1: 统一有序 `referenceImages` DTO、专用更新接口与 `definition` 保护

- [x] 1.1 将创建/摘要/专用更新 DTO 与 definition 保护统一为有序 referenceImages；API 测试覆盖清空、排序、重复、越权、CAS 及保留声音/成品，同步 HTTP API。 <!-- comet-task:021c6d38-a120-42f7-8d07-5d7058300247 -->

**涉及文件：**
- 修改：`backend/internal/app/project_reference_image.go`
- 修改：`backend/internal/app/project_workbench.go`
- 修改：`backend/internal/handler/project.go`
- 修改：`backend/internal/service/aliases_project.go`（如涉及导出别名）
- 修改：`docs/content/docs/backend/http-api.mdx`
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. 定义 `MaxProjectAssetReferenceImages = 16`，将 `CreateProjectCharacterRequest`、`CreateProjectAssetEntityRequest`、`ProjectAssetSummary` 中的单数 `ReferenceImage` 替换为有序切片 `ReferenceImages []ProjectReferenceImage `json:"referenceImages"``。
2. 新增 `SetProjectReferenceImagesRequest { ReferenceImages []ProjectReferenceImage `json:"referenceImages"`, ExpectedVersionID string `json:"expectedVersionId"` }` 与 `Service.SetProjectReferenceImages`，路由挂载 `PUT /api/projects/:id/assets/:assetId/reference-images`。
3. 校验无空白/重复 `resourceId`、长度 `<= 16`、资源归属与 `ready` 状态；若与当前主版本序列完全一致则直接返回当前摘要（不新建版本）；否则创建新版本并保留声音与成品 `representations`。
4. 更新 `preserveReferenceDefinition` 保护 `referenceImages` 不被普通设定编辑篡改。

---

### Task 1.2: 显式可重入单图迁移与异常阻断报告

- [x] 1.2 实现显式可重入单图迁移与异常报告；迁移测试覆盖历史版本、payload、无图、冲突双字段及已有候选基准，验证原图可见且无长期双写。 <!-- comet-task:5caf9361-20b7-4f8a-8110-f53798a4daf3 -->

**涉及文件：**
- 修改：`backend/internal/app/project_reference_image.go`
- 修改：`backend/internal/repository/asset_profile.go`
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. 实现 `MigrateLegacyProjectReferenceImages`：扫描 `AssetVersion.DefinitionJSON` 与 `Asset.PayloadJSON`（`data.definition`），将合法的单数 `referenceImage` 等价改写为单元素 `referenceImages` 并移除旧键；空值规范化为空数组；已迁移记录幂等跳过。
2. 当检测到损坏 JSON、双字段并存且不一致、或引用资源不存在/越权时，生成结构化异常报告并回滚报错，不猜测合并。
3. 验证迁移后原图仍可见、既有候选 `BaseVersionID` 仍可按 CAS 规则正常采用。

---

### Task 1.3: 数据库 JSON 迁移文档与维护窗口说明同步

- [x] 1.3 补充备份/维护窗口/回滚说明及 backend-database JSON 文档；使用迁移前后统计和资源映射 fixture 核对记录不丢失。 <!-- comet-task:598afbe8-dcf0-4627-ad88-d3ebcbe7d14a -->

**涉及文件：**
- 修改：`docs/content/docs/backend/backend-database.mdx`
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. 在 `docs/content/docs/backend/backend-database.mdx` 中同步 `asset_versions.definition_json` 与 `assets.payload_json` 的 `referenceImages` 结构、显式迁移步骤、备份与回滚边界。
2. 在测试中通过迁移前后统计与资源映射 fixture 断言版本数、主版本 ID 与资源引用数零丢失。

---

## 2. 生成与资源安全

### Task 2.1: 应用上限与服务端可信模型能力准入

- [x] 2.1 实现应用/可信模型的最小张数上限及格式、大小预算；能力测试覆盖 0/1/多张、未知协议和客户端扩张能力，证明拒绝发生在入队前。 <!-- comet-task:1be17b29-84d6-4ab6-9555-76606d6eaad6 -->

**涉及文件：**
- 修改：`backend/internal/app/project_image_candidate.go`
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. 在 `validateProjectImageReferenceCapability` 中计算 `effectiveMax = min(MaxProjectAssetReferenceImages, profile.References.MaxImages)`。
2. 自定义渠道若客户端传入 `CapabilityConfig.Image`，其 `MaxImages` 仅允许在不突破服务端协议基线的前提下取较小值，禁止客户端伪造能力扩大上限。
3. 校验每张参考图格式、尺寸与字节预算，超限或不支持在任务入队前直接拒绝。

---

### Task 2.2: 从版本重建有序 `original`/`current`/`none` 输入与单候选约束

- [x] 2.2 从版本重建有序 original/current/none 输入并维持单输出 manual；协议 payload 测试断言全部参考顺序，拒绝伪造来源及多输出旁路。 <!-- comet-task:a0995d80-5d50-45bb-b397-81a60b26cda0 -->

**涉及文件：**
- 修改：`backend/internal/app/project_image_candidate.go`
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. `prepareProjectImageTask` 在 `original` 模式下读取版本完整有序 `referenceImages`，在 `current` 模式下读取版本 `primary` 成品单图，在 `none` 模式下为空。
2. 核对客户端 `typed.ReferenceImages` 与服务端权威有序资源列表逐项一致（长度、`storageKey`、`url`/`dataUrl`），不一致立即拒绝；按保存顺序写入 `input["referenceImages"]` 并透传至下游图片协议 payload。

---

### Task 2.3: 资源锁稳定 ID 排序与历史/任务/payload 引用扫描

- [x] 2.3 资源锁按稳定 ID 排序并扩展历史/任务/payload 引用扫描；SQLite/PostgreSQL 并发测试覆盖反向排序、删除竞争、事务回滚，同步资源合同。 <!-- comet-task:721ec71d-a5ed-46ae-8b09-97371bc444b0 -->

**涉及文件：**
- 修改：`backend/internal/repository/asset_profile.go`
- 修改：`backend/internal/app/resource_references.go`（或对应资源引用扫描文件）
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. `definitionResourceIDs` 与 `LockAssetImageResources` 完整提取 `referenceImages` 中的全部 `resourceId`，去重后按字典序升序加锁并校验资源归属与 `ready` 状态。
2. 扩展资源删除前的引用扫描，确保任何历史版本 `definition_json`、`payload_json` 或任务 `input_json` 引用的参考图均不可被物理删除。

---

### Task 2.4: 单候选取消、失败、恢复、采用 CAS 与幂等回归

- [x] 2.4 回归单候选取消、失败、恢复、采用 CAS 和幂等；测试确保不自动采用、不重复版本、不回滚已保存参考。 <!-- comet-task:da07a81a-4c9f-4721-837b-b08eab259bbe -->

**涉及文件：**
- 修改：`backend/internal/app/project_image_candidate.go`
- 测试：`backend/internal/app/project_reference_image_test.go`

**实施要点：**
1. 确保 `finalizeManualProjectImageTask` 与 `AdoptProjectImageCandidate` 在多参考图版本上锁定全部关联参考图资源，候选采用后新版本完整保留原有序 `referenceImages`。
2. 补充多参考任务取消、失败、CAS 冲突与幂等重放回归测试。

---

## 3. 真实界面与集成

### Task 3.1: 扩展真实资产弹窗有序多参考缩略图、排序/清空与模型限制提示

- [x] 3.1 扩展真实资产弹窗的有序缩略图、移除/清空、键盘排序及模型限制提示；组件/API 测试覆盖三分类、刷新保序和不兼容模型不丢图，同步功能/pending-test。 <!-- comet-task:551187c2-86cf-4f83-850b-ce313c2c82d6 -->

**涉及文件：**
- 修改：`web/src/services/api/projects.ts`
- 修改：`web/src/pages/projects/detail/project-reference-image.ts`
- 修改：`web/src/pages/projects/detail/project-asset-image-modal.tsx`
- 修改：`docs/content/docs/overview/features.mdx`
- 修改：`docs/content/docs/progress/pending-test.mdx`
- 测试：`web/test/project-reference-image-api.test.ts`
- 测试：`web/test/project-reference-image-assets.test.ts`

**实施要点：**
1. `projects.ts` 更新 `ProjectAsset.referenceImages` 与 `updateProjectAssetReferenceImages`（`PUT /projects/:id/assets/:assetId/reference-images`）。
2. `project-reference-image.ts` 提供 `referenceImagesForAsset(asset, source)` 与支持多张计数的 `referenceModelError(config, model, referenceCount)`。
3. `project-asset-image-modal.tsx` 支持多张参考图追加、序号展示、上移/下移/移除/清空、保存参考图集合，以及模型最大参考张数不兼容时的明确禁用原因提示（不丢失已保存参考图）。

---

### Task 3.2: 真实弹窗可控协议 Mock 闭环验证

- [x] 3.2 真实页面以可控协议 mock 验证保存→生成→手动采用、取消/失败/冲突，检查明暗主题与弹窗滚动；无收费授权不真实调用模型。 <!-- comet-task:e0b099bf-c259-4ab0-bbf1-96aac8759863 -->

**涉及文件：**
- 测试：`web/test/project-asset-image-modal.test.tsx`

**实施要点：**
1. 在 `project-asset-image-modal.test.tsx` 中挂载真实 `ProjectAssetImageModal`，验证多张参考图选择、排序保存、按序提交生成、候选手动采用、409 冲突保留候选及不兼容模型拦截。

---

### Task 3.3: 专项测试、类型检查与构建验证

- [x] 3.3 执行最小专项测试及必要构建/类型检查，核对迁移部署演练与已知渠道矩阵；记录实际结果和未覆盖项，不把静态阅读当端到端通过。 <!-- comet-task:076eb2cd-6406-4962-9b0e-2b8a3c45e4b2 -->

**涉及文件：**
- 修改：`docs/content/docs/progress/pending-test.mdx`

**实施要点：**
1. 执行后端 `go test ./internal/app ./internal/repository ./internal/handler` 与前端 `bun test test/project-reference-image-api.test.ts test/project-reference-image-assets.test.ts test/project-asset-image-modal.test.tsx`、`bun run typecheck`、`bun run lint`。
2. 将实际验证结果与待真实浏览器/在线模型验收项同步记录到 `pending-test.mdx`。
