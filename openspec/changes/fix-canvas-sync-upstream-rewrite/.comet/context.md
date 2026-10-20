# Comet Design Handoff

- Change: fix-canvas-sync-upstream-rewrite
- Phase: design
- Mode: compact
- Context hash: c975f5833ba7d412b88861f71f3f6fa4c32a567bde8da5cbe848c08fbdecbfbf

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/fix-canvas-sync-upstream-rewrite/proposal.md

- Source: openspec/changes/fix-canvas-sync-upstream-rewrite/proposal.md
- Lines: 1-50
- SHA256: 284aaca971188433691661ecb5628aca7aa7ec2787b9f4e8299eed479b08f13c

```md
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

```

## openspec/changes/fix-canvas-sync-upstream-rewrite/design.md

- Source: openspec/changes/fix-canvas-sync-upstream-rewrite/design.md
- Lines: 1-64
- SHA256: a081fac7bb79ddb14c9b05195af641dbe9a9d99d2ded42b5c0ba716dabec537c

```md
# Design

## 高层方案

单一策略：**纯上游底座 + 扩展点注入**。`web/src/services/user-data-sync.ts` 全文替换为 `9fdebd07` 上游版，然后以最小侵入方式在其结构上重实现本地 7 项功能。不做行级 patch 回贴，不保留当前融合版的任何手工合并痕迹。

## 架构决策

### D1: 底座取纯上游，扩展以函数级追加为主

上游 v1.6.8.9+ 的同步核心（`syncSingleFlight` 单飞、`submitCanvasMutationBatches` 分批、`canvasEditLeases` 租约、`rebaseCanvasLocalEdits` 合并、`persistConfirmedCanvasBaselines` 基线持久化）职责清晰且行为经过上游集成测试覆盖。本地功能全部实现为**新增函数 + 在明确定义的挂载点调用**，不修改上游函数内部逻辑。唯一例外见 D3。

挂载点清单（以 `9fdebd07` 版行为准，实施前先重新核对行号）：

| 本地功能 | 挂载点 | 方式 |
| --- | --- | --- |
| 确认基线持久化（双回执） | `persistConfirmedCanvasBaselines` 旁路新增 `persistAcknowledgedCanvas`，在保存确认尾部调用 | 追加 |
| 历史回执恢复基线 | 新增 `confirmedCanvasBaseline`；在 `applyAgentCanvasPatches` 的 `acknowledgedProjects.get(id)` 处替换为恢复调用 | 替换取值 |
| 保存前脏基线预修复 | `saveRemoteUserDataBatch` 的 `projectsBeforeRepair` 循环处 | 追加 |
| 素材冲突隔离 | `syncRemoteAssetBatch` 的 409 分支与批量保存收尾 | 替换 409 处理 |
| 收敛 create_node 重放 | `submitCanvasMutation` 的 `send` 回调 409 分支，fence 处理之前 | 追加 |
| 后台无基线恢复 | 自动同步基线判定处：先 `confirmedCanvasBaseline`，catch 落 `unbasedProjects` | 追加 |
| `acknowledgeRemoteAssets` | 新增导出（`assets.tsx` 已有调用方） | 追加 |

### D2: 数据结构

- 确认基线双回执沿用 `yingce:canvas-acknowledged:<id>` 键，值为 `[当前, 前驱]` 数组；前驱按 revision+哈希匹配本地文档后轮换（ponytail 上限：两条足够覆盖回执与草稿写入之间的崩溃窗口；多标签页长期错位 fail-closed）。
- `PendingCanvasMutation` 保持上游类型不变（`CanvasMutationBatchState & {...}`）；收敛重放通过 `pendingCanvasMutations.set` 替换 entry 实现，不新增字段。

### D3: 唯一允许的上游行为微调

素材增量校验处，上游对 409 直接抛 `assetRemoteVersionConflict`；本地需求要求单素材隔离。实现为：try/catch 包裹单个素材校验，冲突记入 `conflictedAssetIds` 后 continue；批量收尾统一聚合报错。这改变错误传播粒度但不改变判定函数（`remoteAssetConflictsWithBaseline` 逻辑与上游逐字段一致，仅 entity 优先版本号）。

### D4: 收敛重放与分批的交互

`submitCanvasMutation`（上游 `submitCanvasMutationBatches` 包装）的 `send` 回调内处理 409：先取服务端权威文档做收敛，收敛请求持久化后**递归调用 `submitCanvasMutation(convergedEntry)`** 重新进入分批循环（而非在 send 内继续消费 `remainingOperations`），保证批次状态由包装器统一管理。全部收敛时返回合成响应 `{ project: { ...remote, revision: Number(remote.revision) }, mutationId }`。

### D5: 测试策略

- 基线：先跑纯上游版记录 `canvas-sync-progress` 失败集合（本机 5 项预存）作为验收下限。
- 逐步重实现，每完成一项功能跑全量 `canvas-sync-progress`：失败集合新增项立即定位回退。
- 本地功能回归：`canvas-create-node-convergence`（收敛）、`user-data-asset-sync`（隔离/认领）、`canvas-task-mutation`（fence/收敛库）必须全绿。
- 最终：`tsc --noEmit` + `bun run build` + `go build ./...`。

## 数据流

```
保存请求 → syncSingleFlight 键控排队 → performRemoteUserDataSave
  → drain → saveRemoteUserDataBatch
      ├─ 预修复：脏基线 → confirmedCanvasBaseline（历史回执）
      ├─ dirtyProjects/dirtyAssets 计算
      ├─ 素材：syncRemoteAssetBatch（409 → 隔离，收尾聚合报告）
      └─ 画布：buildCanvasOperations → submitCanvasMutation
            → submitCanvasMutationBatches（≤500 分批）
                → send：applyRemoteCanvasOperations
                    └─ 409 → 收敛 create_node → 递归 submitCanvasMutation | fence 重试
  → 确认尾部：acknowledgedProjects 推进 + persistConfirmedCanvasBaselines + persistAcknowledgedCanvas
```

## 迁移

1. 备份当前融合版到 `.local/`（不入库）。
2. 底座替换 → 逐功能重实现（每步可独立验证）。
3. 全量验证 + 提交。

```

## openspec/changes/fix-canvas-sync-upstream-rewrite/tasks.md

- Source: openspec/changes/fix-canvas-sync-upstream-rewrite/tasks.md
- Lines: 1-22
- SHA256: 12c5d7265b3f18944462733b6f4018fc6005c1e1591b9f00cbffc42645833376

```md
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

```

## openspec/changes/fix-canvas-sync-upstream-rewrite/specs/canvas-sync-local-extensions/spec.md

- Source: openspec/changes/fix-canvas-sync-upstream-rewrite/specs/canvas-sync-local-extensions/spec.md
- Lines: 1-49
- SHA256: c3277507d755987c1ff572f3a53d675f8fcd425af97803a4691984f9a842d2f4

```md
# 画布同步本地功能扩展

## ADDED Requirements

### Requirement: 确认基线持久化恢复

画布保存确认后，系统 SHALL 在用户作用域本地存储中为每个画布维护至多两条确认基线回执（当前基线与其前驱），并在打开画布、Agent 刷新、增量同步与保存路径统一使用以下恢复顺序：内存 `acknowledgedProjects` 基线 → 本地快照候选（revision 与内容哈希均匹配当前本地文档）→ 服务端指定版本完整操作回执（`getRemoteCanvasRevision`，ID、revision、哈希全部一致才可采用）。

#### Scenario: 刷新后从本地快照恢复基线

- **WHEN** 浏览器刷新后内存基线为空，本地存在与当前画布 revision 和 remoteContentHash 一致的确认快照
- **THEN** 该快照被用作三方合并基线，本地编辑不丢失
- **AND** 哈希不匹配的候选不得被采用为基线

#### Scenario: 无任何可信基线时拒绝自动合并

- **WHEN** 内存基线、本地快照、服务端回执均无法通过核验
- **THEN** 系统报"缺少已确认基线，无法自动合并"并保留本地编辑
- **AND** 不得用当日云端状态冒充基线

### Requirement: 素材冲突隔离与聚合报告

增量同步校验素材远端版本时，系统 SHALL 对发生远端冲突的单个素材只做隔离（记录 ID 与标题，不中断整批），其余素材与画布照常推送；推送完成后 SHALL 聚合报告冲突素材名并停止覆盖。entity（角色）素材 SHALL 优先比较 `primaryVersionId` 版本号，任一方缺失时回退 `updatedAt` 比较。

#### Scenario: 单素材冲突不阻塞整批

- **WHEN** 增量校验中某素材远端版本与基线不一致
- **THEN** 该素材被隔离，其余素材与画布修改完成推送
- **AND** 最终错误信息包含冲突素材名并说明其余内容已保存

### Requirement: 收敛 create_node 重放

journal 重放收到 409 `canvas_field_conflict` 时，系统 SHALL 先读取服务端权威画布文档，用 `dropConvergedCreateNodeOperations` 剔除服务端已存在的 create_node 操作；收敛请求先持久化再重试，全部操作已收敛时 SHALL 返回现网 revision 而不重发任何操作。

#### Scenario: 幽灵 create 重放解除阻塞

- **WHEN** 响应丢失导致 journal 保留 create_node 重放，而服务端已提交同 ID 节点
- **THEN** 重试 409 后该 create_node 被剔除并以新 mutationId 重试剩余操作
- **AND** 全部操作收敛时调用方收到现网 revision，画布不再被旧 journal 阻塞

### Requirement: 保存前脏基线预修复

批量保存画布前，系统 SHALL 对哈希漂移的确认基线（本地 revision > 0 且基线内容哈希与自身不符）执行历史回执恢复，恢复结果重新写入 `acknowledgedProjects`；登录恢复的脏缓存不得被静默视为已保存状态。

#### Scenario: 登录恢复的脏缓存不误报已保存

- **WHEN** 增量会话中某画布基线来自登录时的脏缓存且哈希自漂移
- **THEN** 系统先从历史回执恢复可信基线再判定 dirty
- **AND** 恢复失败时该画布进入无基线隔离，不阻塞其他画布

```
