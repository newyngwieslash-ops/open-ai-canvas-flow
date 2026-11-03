# Brainstorm Summary: add-character-turnaround-splitting

## 1. 背景与目标

针对角色资产已有的三视图整图（`turnaround_sheet`），提供视觉建议与人工确认分离的视角拆分流程：
1. **视觉建议草稿**：`POST /api/projects/:id/characters/:assetId/turnaround-split-plans` 仅接受属于当前角色基准版本的 owned ready 三视图整图与具备视觉能力的系统/逻辑文本模型，返回待人工确认的 `front`、`side`、`back` 整数像素框与标签草稿（缺失视角、多人或歧义时返回待确认人工状态，绝不伪造置信度或自动保存）。
2. **服务端原像素裁切与原子版本提交**：`POST /api/projects/:id/characters/:assetId/turnaround-splits` 接收幂等键、基准版本 `expectedVersionId`、源资源 ID、源图尺寸及用户已逐一确认的三个唯一角色视角整数像素框（`front` / `side` / `back`），在服务端校验 PNG/JPEG 头部、文件大小、总像素与边界预算后从原图无损裁切输出三张 PNG 资源，并在事务内保留原有 `turnaround_sheet`、`primary`、`expression_sheet`、声音绑定与角色定义，仅合并更新 `front` / `side` / `back` 三个表现并推进新版本。
3. **下游默认引用不变**：画布与 Agent 下游默认引用仍保持 `turnaround_sheet -> primary -> front` 的单一默认图优先级，拆分出的三张视图不会自动塞入多图参考列表。

## 2. 已确认构建配置

- `isolation: current`（绑定 `sync/upstream-1.6.3`）
- `build_mode: executing-plans`
- `tdd_mode: tdd`
- `review_mode: standard`
