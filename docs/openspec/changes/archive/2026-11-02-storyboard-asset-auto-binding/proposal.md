# Proposal

## Why

章节分镜目前不能稳定利用项目已有角色、场景和道具：工作区资产来自既有镜头引用，首次生成时可能没有候选，生成输入和输出协议也存在资产信息断点。需要把“新分镜自动绑定”和“旧分镜安全补绑”统一为一个可解释、可确认的资产绑定能力，让后续图像和视频生成获得正确参考图，而不改写用户已有创作。

## What Changes

- 保留工作区引用资产读模型，独立、完整分页加载当前项目可绑定候选；将有明确大小边界的资产摘要真正写入章节生成提示词，超预算显式提示或按明确上下文缩减，不将部分候选宣称为完整集合。
- 新分镜优先解析模型可见的结构化资产 ID，再以保守的唯一名称或已有别名匹配兜底；复用同一确定性匹配逻辑识别角色、场景、道具，拒绝非法或跨项目 ID，不猜歧义，不盲目继承上一镜引用。
- 为既有镜头增加非破坏性批量匹配预览与确认入口，只追加选定版本引用；无版本、缺图、歧义、容量不足均显式展示，当前每镜上限为 6，计算已有和新增后的总量。
- 新增小型追加型批量写入能力，校验权限、归属和预览并发基线，原子拒绝冲突并支持重复确认幂等；仅实际新增引用时复用失效策略，不覆盖脚本、人工引用和既有产物。
- 不新建资产系统、不自动生成媒体、不重生成或替换旧镜头；旧镜头匹配不调用外部付费模型或创建模型任务。

## Capabilities

### New Capabilities

- `storyboard-asset-binding`: 项目章节分镜的角色、场景、道具候选发现、确定性解析、生成时绑定与既有镜头安全补绑，以及引用变更的容量、权限、并发和失效约束。

### Modified Capabilities

无。已核对根规范目录，现有 `upstream-sync` 不涉及本能力。

## Impact

- 前端涉及项目章节生成输入、输出解析、项目资产查询和镜头补绑预览；重点断点为 `project-chapter-ai.ts`、`canvas-project-domain.ts` 与 `chapter-storyboard-production.ts`。共享画布的其他调用方保持原行为。
- 后端涉及章节生成提示词的 `canvasGenerationInput` 消费链路、项目候选分页读取、镜头引用追加与失效策略。`ProjectUnitAssets` 保持仅返回工作区引用资产；新镜头可复用支持 `assetVersionIds` 的批量替换链路，旧镜头不得调用整章替换 API。
- API 方案增加追加型批量能力，最终路径和类型在实现时遵循现有项目约定；预计不新增数据库 schema 或外部依赖。
- 实施时同步 `docs/content/docs/overview/features.mdx`、`docs/content/docs/backend/http-api.mdx`、`docs/content/docs/backend/code-map.mdx`、`docs/content/docs/progress/pending-test.mdx`；无 schema 变化不改数据库文档。
- 本次仅产出开案文档，不代表功能已实现。工作区已有 43 个 tracked 改动及 untracked 内容均为既存变更；后续触及 `projects.ts`、OpenAPI 和文档时必须保留原有 diff，不归因、不覆盖、不提交这些变更。
