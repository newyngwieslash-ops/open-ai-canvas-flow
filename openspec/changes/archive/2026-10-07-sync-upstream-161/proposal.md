# Proposal

## Why

本仓库与上游 `ddcat-ai/open-ai-canvas` 同源（本地根提交即上游初始提交，历史连通），但已落后上游一个 minor 版本：上游已发布 v1.6.1（3D 预演台重构、技能分类策展、分片上传配额预留、Agent 视频首尾帧等 46+ 提交），而本地 main 停留在 v1.6.0 附近并叠加了自己的迭代。趁差异面还小（本地 57 个文件 vs 上游 302 个文件，预演三方冲突仅 10 个文件约 4 处冲突块），现在融合成本最低；且历史连通使 `git merge` 一次融合后，未来同步上游只需 `fetch + merge`，不再需要重导 patch。

## What Changes

- 添加 `upstream` remote（`https://github.com/ddcat-ai/open-ai-canvas.git`，仅 fetch，不推送）。
- 从 main 切出集成分支，将 `upstream/main`（v1.6.1，`0b1d5073`）以普通三方合并（git merge）融入，保留双方全部提交历史与合并语义。
- 人工裁决双方都改过的 10 个文件（约 4 处冲突块）：`.gitignore`、3 个文档 mdx、4 个 web 页面/组件、`user-data-sync.ts`、`use-appearance-store.ts`。
- 数据库迁移随上游升至 v47（`upload_reservations` v45、`skill_curation` v46、`skill_curation_roots` v47）；本地 `backend/internal/database/` 自 merge-base 零改动，迁移文件无冲突。
- 融合通过后同步 CHANGELOG / 相关专题文档中双方并存的差异描述，并将集成合并提交推送至 `origin`（仅 `https://github.com/newyngwieslash-ops/open-ai-canvas-flow.git`）。

## Capabilities

### New Capabilities

- `upstream-sync`：定义本仓库与上游 `ddcat-ai/open-ai-canvas` 的同步行为约束——历史连通前提、集成方式（merge 优于重放）、冲突裁决边界（本地工具态文件不参与融合）、推送目标仅限 origin。本次集成是该能力的首次执行。

### Modified Capabilities

<!-- 无：本次不修改既有业务能力的需求级行为；上游 v1.6.1 的功能（预演台、技能策展等）以自身代码与文档为准，不在本仓库规格中重复声明。 -->

## Impact

- **代码**：web（302 个上游侧文件，含 10 个双改文件裁决）、backend（迁移 v45–v47、技能策展、上传配额）、docs（CHANGELOG、backend-database.mdx 等）。
- **数据库**：schema_migrations 升至 v47，启动时自动执行；升级前建议备份现有数据目录。
- **部署**：无新增容器；`docker-compose.*.yml` 如有上游改动将随合并带入。
- **风险**：本机到 GitHub 网络不稳（已完成深度 fetch，后续无需联网）；`character-script-suggestions` feature 分支 18 个未验收提交不在本次范围，合并基于 main 不触碰该分支。
- **推送边界**：一切提交只推送到 `origin`（newyngwieslash-ops/open-ai-canvas-flow）；upstream remote 配置为仅 fetch。
