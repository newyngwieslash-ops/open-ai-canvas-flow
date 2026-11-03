# Proposal

## Why

v1.6.8.9–8.12 上游（`9fdebd07`）对画布同步核心做了整体重写：新增 500 操作分批提交（`submitCanvasMutationBatches`）、单飞保存与故障恢复体系（`sync-recovery` 的 `syncSingleFlight`/`CanvasSyncPendingError`）、节点编辑租约（`canvasEditLeases`）、`rebaseCanvasLocalEdits` 三方合并与未验证恢复备份。本地同期在同一文件 `web/src/services/user-data-sync.ts` 上平行演进了 7 个提交（确认基线持久化、历史回执恢复基线、素材冲突隔离与收尾报告、收敛 create_node 重放、悬空连线请求恢复、基线认领、force 分支移除）。v1.6.8.12 融合（merge `c361b1ac` + 修正 `b5e96193`）采用逐冲突块机械融合，留下结构性缝隙：`canvas-sync-progress` 集成测试融合版 34 项失败（纯上游在本机同样有 5 项预存失败），表现为定向保存串行语义破坏、journal 重放丢编辑凭证、新建画布不触发 create_canvas 写入等。

## What Changes

- 以纯上游 `user-data-sync.ts` 实现（`9fdebd07` 版）为唯一底座，替换当前融合版全文。
- 在上游结构上**重新实现**本地 7 个提交的功能语义（不是行级回贴 patch）：
  1. 确认基线持久化（`readAcknowledgedCanvases`/`persistAcknowledgedCanvas`，双回执防 SSE 批次驱逐）。
  2. 历史回执恢复基线（`confirmedCanvasBaseline`：内存基线 → 本地快照 → `getRemoteCanvasRevision` 指定版本回执，哈希核验后才可作为合并基线）。
  3. 素材冲突隔离与收尾报告（`conflictedAssetIds`/`conflictedAssetTitles` 单素材隔离 + 聚合报错，`remoteAssetConflictsWithBaseline` 版本号优先判定）。
  4. 收敛 create_node 重放（409 后取服务端权威文档，`dropConvergedCreateNodeOperations` 剔除已存在节点，空批次返回现网 revision，先持久化收敛请求再重试）。
  5. 保存前脏基线预修复（`saveRemoteUserDataBatch` 内对哈希漂移基线做历史回执恢复）。
  6. 后台自动同步的无基线画布恢复路径（`confirmedCanvasBaseline` 失败落入 `unbasedProjects` 隔离）。
  7. `acknowledgeRemoteAssets` 基线认领（角色投影写 store 时认领，消除远端版本误报）。
- 保留上游分批/单飞/租约体系不动；本地功能作为上游扩展点实现，不再改动上游主干行为。
- `agent-canvas-patch.ts`、`canvas-task-mutation.ts` 等支撑文件已含本地功能（提交 `26a90e6d`），本 change 不重写它们，仅校正 `user-data-sync.ts` 对它们的接线。

## Goals

- `canvas-sync-progress` 集成测试回到纯上游本机基线（5 项预存失败），且本地功能回归测试（`user-data-asset-sync`、`canvas-create-node-convergence`、`canvas-task-mutation` 等）全绿。
- `tsc --noEmit`、`bun run build`、`go build ./...` 通过；后端失败项维持已归因的 Windows 环境预存集合不变。

## Non-Goals

- 不改上游分批提交、单飞保存、编辑租约、`rebaseCanvasLocalEdits` 的行为。
- 不重写 `canvas-task-mutation.ts`、`agent-canvas-patch.ts`、`canvas-edit-leases.ts` 等已稳定的支撑模块。
- 不处理后端 Go 侧逻辑（`operations.go` 任务时间栅栏语义已裁定为上游优先）。
- 不新增数据库结构或公开 API。

## Scope

- 主文件：`web/src/services/user-data-sync.ts`（全文替换 + 扩展点重实现）。
- 触达：`web/test/canvas-sync-progress.test.ts` 仅在本地功能需要新断言时补充；不修改上游既有断言。

## Key Unknowns

- 上游 `rebaseCanvasLocalEdits` 与本地 `confirmedCanvasBaseline` 的组合是否覆盖旧客户端基线恢复场景（待集成测试验证）。
- `submitCanvasMutationBatches.send` 回调内嵌收敛重试后，批次状态（`remainingOperations`）与收敛后请求的一致性。

## Draft Acceptance Scenarios

1. **上游基线不回退**：`bun test test/canvas-sync-progress.test.ts` 失败数 ≤ 5（与纯上游在本机一致），且失败集合与纯上游相同。
2. **确认基线持久化**：保存成功后本地快照含双回执；刷新后无内存基线时从快照恢复，哈希不匹配拒绝采用云端状态。
3. **历史回执恢复**：本地 revision 有服务端完整回执且哈希一致时，作为三方合并基线；404 或哈希不匹配时报"缺少已确认基线"并保留本地编辑。
4. **素材冲突隔离**：单素材远端版本变化只隔离该素材，聚合报错含素材名，其余素材与画布照常推送。
5. **收敛重放**：journal 重放 409 且服务端已存在同 ID 节点时，剔除该 create_node 后重试；全部收敛时采用现网 revision，不再重发。
6. **定向保存隔离**：全局后台保存失败不影响已排队的前台 scoped 保存（上游 `syncSingleFlight` 键控语义 + 本地断言共存）。
