# 任务清单

## 1. RED：节点级 updatedAt 回归测试

- [x] 1.1 写失败测试：`sameCanvasContent` 对仅节点 `updatedAt` 不同的两份文档返回 true；`buildCanvasOperations` 对同样输入生成 0 个操作；混合变更保留真实字段
- [x] 1.2 运行确认现版本失败（RED 证据）

## 2. 修复实现

- [x] 2.1 `web/src/lib/canvas/canvas-content.ts`：`canvasContentSnapshot` 剥离节点顶层 `updatedAt`
- [x] 2.2 `web/src/services/user-data-sync.ts`：`buildCanvasOperations` 过滤纯簿记 patch_node

## 3. 验证

- [x] 3.1 新回归测试转绿
- [x] 3.2 相关专项套件全绿（agent-canvas-patch、canvas-operations-build、user-data-sync 系列）
- [x] 3.3 `tsc --noEmit` 无错
- [x] 3.4 提交代码
