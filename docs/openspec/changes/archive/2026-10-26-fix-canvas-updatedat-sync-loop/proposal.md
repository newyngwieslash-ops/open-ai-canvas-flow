# 修复画布 updatedAt 回写死循环（hotfix）

## Why

复现画布上，用户在 Agent 面板点击「同意执行」后审批请求从未到达服务端，运行永远停在 `waiting_approval`，「本轮待办」无限转圈，且无任何错误提示。

## 根因

两层叠加（DB 取证：revision 65–68 四次纯 `updatedAt` 翻转，时间与两次点击一一对应）：

1. **保存成功后的回写覆盖本地时间戳**：`saveRemoteUserDataBatch` 保存成功后把云端文档（含旧节点 `updatedAt`）写回编辑器 store；编辑器状态写入器（`stampCanvasNodeChanges`）随后给节点盖上新时间戳 → 下一轮脏检测再次判脏。
2. **节点级 `updatedAt` 被当作内容参与脏检测**：`canvasContentSnapshot` 只剥项目级 `updatedAt`/`revision`/`viewport`，节点级 `updatedAt` 算内容 → `drainRemoteUserDataChanges` 两轮不收敛，抛 `CanvasSyncPendingError`。
3. **审批提交把该错误静默吞掉**：`submitApproval` catch 分支对 `CanvasSyncPendingError` 直接 `return`（canvas-cloud-agent-panel.tsx ~810），用户点击无任何反馈。

服务端早已把节点顶层 `updatedAt` 定为簿记字段（`canvasNodeBookkeepingFields`，`operations.go:212/395`，不参与 CAS 基线校验），前端脏检测却把它算内容——两边立场不一致是死循环的根。

## What Changes

- 节点级 `updatedAt` 不再参与前端画布脏检测与内容哈希（与服务端簿记立场对齐）。
- `buildCanvasOperations` 不再生成纯 `updatedAt` 的 patch_node 操作（止住幻影 revision）。
- 已有的审批静默吞错按旧行为保留（本次不扩大范围；根因消除后该路径不再被触发）。

## 影响面

- CAS 冲突检测、任务终态收敛、Agent 增量合并、删除保护均不受影响（`updatedAt` 本就不在这些路径的比较范围内）。
- 内容哈希算法变化 → 升级后首次加载触发一轮「未核对缓存」重校验，自愈不丢数据。
- 节点搜索「最近更新」排序在纯簿记触碰后显示更旧时间——内容没变不算更新，语义更准确。
