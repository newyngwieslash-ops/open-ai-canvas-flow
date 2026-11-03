---
comet_change: add-character-turnaround-splitting
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-03-add-character-turnaround-splitting
status: final
---

# Technical Design: 角色三视图整图原像素拆分与版本化保存 (`add-character-turnaround-splitting`)

## 1. 架构概览与边界

本设计实现 `docs/openspec/changes/add-character-turnaround-splitting/specs/character-turnaround-splitting/spec.md` 定义的 6 项核心需求：

1. **受权来源和识别草稿（`TurnaroundSplitPlan`）**：
   - 接口：`POST /api/projects/:id/characters/:assetId/turnaround-split-plans` 与 `GET /api/projects/:id/characters/:assetId/turnaround-split-plans/:taskId`
   - 仅允许对当前项目角色的 `expectedVersionId == asset.PrimaryVersionID` 且在该版本 `representations` 中绑定的 `turnaround_sheet`（或明确三视图源）owned ready 图片资源发起视觉分析；普通单人肖像（仅 `primary` 无 `turnaround_sheet`）、他人资源、未知尺寸或非视觉模型在入队前直接拒绝（400/404/409/422）。
   - 解析严格 JSON 草稿（`views: [{ role, box: { x, y, width, height }, note }]`, `manualReason`），不伪造 `confidence`；缺视角、多人、重叠或坐标越界时保留未确认人工状态（`RequiresManualReview: true`），绝不自动创建角色表现。

2. **服务端原像素裁切与输入限制（`CropTurnaroundViewsFromSource`）**：
   - 服务端直接从 `Resource` 物理存储读取源图字节，仅支持静态 PNG / JPEG（拒绝 SVG、GIF/WebP 动画、未知格式或伪装头部）。
   - 先通过 `image.DecodeConfig` 校验头部尺寸与预算（源字节 `<= 32 MiB`、宽/高 `<= 16384`、总像素 `<= 40,000,000`），并归一化 EXIF 方向（JPEG 方向统一至显示像素坐标系），校验客户端提交的 `sourceWidth` / `sourceHeight` 与服务端解码后实际尺寸完全一致。
   - 校验用户确认的三个视角框：必须严格包含且仅包含 `front`、`side`、`back` 三个唯一角色，每个框均为正整数像素矩形、完全落在源图边界内、互不发生不可裁切的非法重叠、且三框总像素不超过源图像素预算。输出无损 PNG，像素 100% 来自源图对应子区域（`SubImage`），绝不缩放重绘或生成背景。

3. **幂等保存、完整表现合并与原子版本提交（`ExecuteCharacterTurnaroundSplit`）**：
   - 接口：`POST /api/projects/:id/characters/:assetId/turnaround-splits`
   - 绑定幂等键 `idempotencyKey` 与请求摘要 `requestDigest`：相同幂等键与相同参数重放时直接返回已创建的版本与三视图资源结果（即使角色 `PrimaryVersionID` 已推进为新版本也不误报 CAS 冲突）；同键异参返回 `409 Conflict`。
   - 先将裁切出的三张 PNG 写入暂存文件并登记暂存路径；随后在数据库事务中再次校验任务未取消、`asset.PrimaryVersionID == expectedVersionID`、源资源仍为 `ready`，创建三条 `model.Resource` 记录（`metadata` 记录 `sourceResourceId`、`role`、`cropBox`、`sourceWidth`、`sourceHeight`、`splitOperationId`），读取当前版本全部 `AssetRepresentation` 并仅替换 `front`、`side`、`back`，完整保留 `turnaround_sheet`、`primary`、`expression_sheet`、声音绑定 `CharacterVoiceBinding` 与 `DefinitionJSON`。
   - 若事务 CAS 冲突、已取消或任一写入失败，事务整体回滚并立即清理无引用的暂存物理文件，不产生半套可见资源或推进主版本。

4. **前端真实角色裁框组件与默认引用回归**：
   - 新增 `web/src/pages/projects/detail/character-turnaround-split.ts` 与 `character-turnaround-split-modal.tsx`，并在角色详情/卡片提供「拆分三视图」入口：支持原图预览、建议框加载、歧义/缺视角转人工提示、`front` / `side` / `back` 标签与整数像素框调整（含键盘方向键微调）、逐框确认门禁（未满 3 个唯一有效视角确认时禁用保存）、缩放预览坐标与原图整数像素坐标双向转换、CAS/重放/处理状态提示。
   - 保持 `web/src/lib/canvas/canvas-character-reference.ts` 与 `backend/internal/app/cloud_agent_character.go` 的 `turnaround_sheet -> primary -> front` 单图默认引用优先级不变。
