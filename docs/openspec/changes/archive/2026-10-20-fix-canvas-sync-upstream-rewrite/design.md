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
