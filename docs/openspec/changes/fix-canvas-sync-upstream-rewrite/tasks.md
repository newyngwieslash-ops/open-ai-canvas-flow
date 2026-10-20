# Tasks

## 1. 基线与准备

- [x] 1.1 备份当前融合版 `user-data-sync.ts` 到 `.local/uds-fused-backup.ts`（不入库），记录 `canvas-sync-progress` 融合版失败集合（34 项）。
- [x] 1.2 用纯上游版（`9fdebd07`）替换 `user-data-sync.ts`，跑 `canvas-sync-progress` 记录预存失败集合（验收下限），跑 `tsc --noEmit` 确认底座自洽。

## 2. 功能重实现（每步后跑全量 canvas-sync-progress 对比基线）

- [x] 2.1 确认基线持久化：新增 `readAcknowledgedCanvases`/`persistAcknowledgedCanvas`，在保存确认尾部与 `persistConfirmedCanvasBaselines` 并行调用；验证刷新恢复与哈希核验。
- [x] 2.2 历史回执恢复：新增 `confirmedCanvasBaseline`，替换 `applyAgentCanvasPatches` 等处的直接 `acknowledgedProjects.get` 取值；验证 404/哈希不匹配拒绝路径。
- [x] 2.3 保存前脏基线预修复：`saveRemoteUserDataBatch` 的 `projectsBeforeRepair` 循环，恢复失败落入 `unbasedProjects` 隔离。
- [x] 2.4 素材冲突隔离：**DEFERRED**——上游 #798–#809 已以"备份采用云端"统一冲突语义，entity 投影链路移出为独立后续 change（spec 已标注）。
- [x] 2.5 收敛 create_node 重放：`submitCanvasMutation` send 回调 409 分支，fence 之前插入收敛逻辑；递归 `submitCanvasMutation(convergedEntry)`；全部收敛返回现网 revision。
- [x] 2.6 `acknowledgeRemoteAssets` 基线认领导出（调用方 `assets.tsx` 已存在）。

## 3. 回归与交付

- [x] 3.1 全量回归：`canvas-sync-progress` 失败集合 == 纯上游预存集合；`canvas-create-node-convergence`、`user-data-asset-sync`、`canvas-task-mutation`、`agent-canvas-patch` 全绿。
- [x] 3.2 构建验证：`tsc --noEmit`、`bun run build`、`go build ./...`。
- [x] 3.3 更新 `pending-test.mdx` 融合缺口条目为已修复状态，CHANGELOG 登记。
- [x] 3.4 提交并推送 `sync/upstream-1.6.3`。
