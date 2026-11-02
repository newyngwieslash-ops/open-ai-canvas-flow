# Brainstorm Summary

- Change: add-asset-multi-reference-generation
- Date: 2026-11-02

## Confirmed Technical Approach

1. **单一有序集合持久化与 CAS 控制**：
   - 资产版本 `DefinitionJSON` 及实体 `payload.data.definition` 中的单数 `referenceImage` 迁移为唯一有序数组 `referenceImages: [{ resourceId }]`（空数组 `[]` 表示明确清空参考图，拒绝重复 `resourceId`）。
   - 专用写接口收敛为 `PUT /api/projects/:id/assets/:assetId/reference-images`（接收 `referenceImages: [{ resourceId }]` 与 `expectedVersionId`），创建接口（`POST /projects/:id/characters`、`POST /projects/:id/asset-entities`）及资产摘要 DTO（`ProjectAsset.referenceImages`）同步切换，移除旧单数写接口，不保留长期双写。
   - 普通 `definition` 更新（`preserveReferenceDefinition`）保护 `referenceImages` 不被篡改或覆盖；若提交的有序集合与当前主版本完全一致，直接返回当前版本摘要而不产生冗余新版本。
2. **有界上限（16 张）与服务端可信模型能力准入**：
   - 应用持久集合上限设为 `16` 张；实际生成准入上限取 `min(16, resolvedModel.references.maxImages)`，并在入队前校验每张图片就绪状态、尺寸、MIME 格式及请求大小预算。
   - 客户端传入的 `capabilityConfig` 不得突破服务端已确认的渠道/协议能力；保存多张参考图后若切换到仅支持较少张数的模型，保留已存参考集合但禁用生成并给出明确提示。
3. **生成来源重建、稳定锁序与显式一次性迁移**：
   - `prepareProjectImageTask` 按 `referenceSource` 重建：`original` 取基准版本完整有序 `referenceImages`，`current` 取基准版本 `primary` 成品单图，`none` 为空数组，强制单候选输出（`count=1`、`manual`）。
   - 数据库事务对多张参考图与候选资源加锁时按 `resourceId` 字典序稳定排序（消除反向排序死锁），业务 payload 仍保持用户保存顺序；资源引用扫描覆盖历史版本 `definition`、`payload` 与任务输入。
   - 提供显式可重入迁移函数：将存量 `referenceImage.resourceId` 等价转换为单元素 `referenceImages`，无参考转为空数组，若出现新旧字段并存且不一致或非法资源则输出异常报告并拒绝静默合并。

## Key Trade-offs and Risks

- **Breaking 单字段切换 vs 长期双字段兼容**：按项目边界规则直接完成显式迁移并切换为唯一 `referenceImages` 合同，避免双字段读写不一致。
- **模型声明张数 vs 协议实际写入**：不仅检查 `maxImages` 计数，还在协议 payload 构造层断言全部参考图按序写入。
- **并发加锁顺序 vs 用户展示顺序**：数据库行锁按去重排序后的 `resourceId` 顺序获取，用户排序保存在 `DefinitionJSON` 数组顺序中，两者解耦。

## Testing Strategy

- **后端单元与集成测试（`go test ./internal/app ./internal/repository ./internal/handler`）**：
  - `PUT /projects/:id/assets/:assetId/reference-images` 的有序保存、清空、重复拒绝、越权拒绝、CAS 冲突、同序幂等、保留声音与成品表现；
  - 存量单参考 `referenceImage` 可重入迁移、冲突双字段报错阻断、历史版本与任务引用保护；
  - `prepareProjectImageTask` 与 `validateProjectImageReferenceCapability` 对 0/1/多张、超限、未知协议、伪造顺序/来源的入队前拦截；
  - 反向排序并发更新与资源删除竞争测试。
- **前端专项测试（`bun test`）与类型检查**：
  - `project-reference-image-assets.test.ts` 与 `project-asset-image-modal.test.tsx` 扩展多参考有序选择、键盘上移/下移/移除/清空、模型张数上限不兼容提示（不丢已存参考图）及手动采用闭环。

## Spec Patches

None
