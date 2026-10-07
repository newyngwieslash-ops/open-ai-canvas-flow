# Tasks：fix-asset-sync-remote-conflict

## 1. 同步模块（web/src/services/user-data-sync.ts）

- [x] 1.1 新增 `acknowledgeRemoteAssets(assets: Asset[])`：无条件写入 `acknowledgedAssets` 基线并加入 `verifiedAssets`
- [x] 1.2 增量校验对 entity 素材优先比较 `primaryVersionId`，缺失时回退 `updatedAt`
- [x] 1.3 校验失败改为隔离：跳过失败素材与其推送，聚合失败信息在推送完成后抛出
- [x] 1.4 推送循环对 entity 素材跳过通用 `upsertRemoteAsset`，仅对齐基线并告警

## 2. 调用点（web/src/pages/projects/detail/assets.tsx）

- [x] 2.1 `syncPersonalCharacterProjection` 写 store 的同时调用 `acknowledgeRemoteAssets`

## 3. 测试

- [x] 3.1 新增/扩展专项测试覆盖：acknowledge 后不再误报、entity 版本判定分型、单素材失败隔离、entity 跳过 upsert
- [x] 3.2 运行 `web` 专项测试与 `bun run build`

## 4. 文档

- [x] 4.1 `docs/content/docs/progress/pending-test.mdx` 记录本次修复与待浏览器验收项
