# upstream-sync Specification

## Purpose
定义本仓库与上游 `ddcat-ai/open-ai-canvas`（原始项目）的同步行为契约：在历史连通的前提下，如何以可追溯、可重复的方式融合上游更新，同时不破坏本地已有迭代，且任何提交只进入本仓库的 origin 远端。

## Requirements

### Requirement: 上游同步以合并提交方式集成

当需要融合上游 `ddcat-ai/open-ai-canvas` 的更新时，系统 SHALL 通过 `git merge upstream/main` 产生的合并提交把上游变更引入集成分支：本地既有提交保持原样不被重写，上游提交的作者与语义在合并后可追溯。

#### Scenario: 融合 v1.6.1 更新

- **WHEN** 从 main 切出集成分支并执行 `git merge upstream/main`（v1.6.1，`0b1d5073`）
- **THEN** 合并完成后 `git log` 同时包含本地全部既有提交与上游 46+ 个新提交
- **AND** 本地自 merge-base 以来的业务改动不因合并被覆盖丢失（双改文件以人工裁决结果为准）

### Requirement: 冲突裁决保留本地工具态边界

融合冲突裁决 SHALL 保留本地工具态产物的本地版本（`AGENTS.md` 的 Comet 注入块、`.zcode/`、`openspec/`、`docs/openspec/` 等 Comet/OpenSpec 产物），业务文件以"双方改动语义并存"为裁决目标。

#### Scenario: 双改文档文件裁决

- **WHEN** 上游与本地对同一文档文件（如 `docs/content/docs/overview/features.mdx`）均有修改
- **THEN** 裁决结果同时保留本地条目与上游新增条目，不整文件取单一侧

### Requirement: 推送目标仅限本仓库 origin

同步流程产生的任何提交与分支 SHALL 只推送到 `origin`（`https://github.com/newyngwieslash-ops/open-ai-canvas-flow.git`）；`upstream` remote SHALL 配置为仅 fetch，不得作为推送目标。

#### Scenario: 推送后远端核对

- **WHEN** 集成分支验证通过并执行 `git push origin <branch>`
- **THEN** 变更出现在 `newyngwieslash-ops/open-ai-canvas-flow`
- **AND** `ddcat-ai/open-ai-canvas` 不收到任何来自本仓库的推送

### Requirement: 融合后验证覆盖后端迁移与前端构建

融合完成后 SHALL 至少执行后端测试套与前端构建作为验收：后端迁移应自动升至 v47 且测试通过，前端构建成功；未通过时不得进入推送。

#### Scenario: 数据库迁移升级

- **WHEN** 融合后的后端以任一现有数据目录启动
- **THEN** `schema_migrations` 记录推进至 version 47，新增上传配额预留与技能分类策展表
- **AND** `cd backend && go test ./...` 通过
