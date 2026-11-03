# 修复方案

单一方案，两处最小修改，全部对齐服务端已有的簿记立场：

## 1. `web/src/lib/canvas/canvas-content.ts`

`canvasContentSnapshot` 对每个节点剥掉顶层 `updatedAt`（仅节点顶层，`metadata` 内的任务观察字段不动）：

```ts
nodes: content.nodes.map(({ updatedAt: _u, ...node }) => node)
```

- `sameCanvasContent` 与 `canvasContentHash` 都走这个快照，一处修改两个消费者同时生效。
- 剥离发生在快照层而非比较层，`rebaseCanvasLocalEdits` 等拿到仍是完整节点对象（带时间戳），不破坏回写链路。

## 2. `web/src/services/user-data-sync.ts` `buildCanvasOperations`

生成 `patch_node` 前过滤：若 `diff.patch` 的键全部属于簿记集（当前只有 `updatedAt`），跳过该操作（`diff.previous` 同步丢弃，不产生空 before 基线）。真实字段 + `updatedAt` 混合的补丁保留（服务端本来就会跳过簿记基线）。

## 不做的事

- 不改 `submitApproval` 的静默吞错（根因消除后不再触发；单改它会掩盖其他真实同步失败）。
- 不钉死两个打架写入方的具体调用（修复让系统对它们免疫；钉死是可选后续）。
- 不动服务端 `canvasNodeBookkeepingFields`（已正确）。

## 验证

- RED 回归测试：节点仅 `updatedAt` 不同的两份文档，`sameCanvasContent` 必须为 true（现版本为 false）。
- `buildCanvasOperations`：基线与目标仅节点 `updatedAt` 不同 → 0 个操作；混合变更 → 保留且 patch 含真实字段。
- 既有专项套件全绿（agent-canvas-patch / canvas-operations-build / user-data-sync 相关）。
