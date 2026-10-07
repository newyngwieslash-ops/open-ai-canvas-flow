# Comet Design Handoff

- Change: sync-upstream-161
- Phase: design
- Mode: compact
- Context hash: e3ecaf2d6167f228cdb1e2318ea06dcbc195e07399175bbd66aad1b080badd8b

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/sync-upstream-161/proposal.md

- Source: openspec/changes/sync-upstream-161/proposal.md
- Lines: 1-31
- SHA256: 09915f37e0202ee9cf759f27cfe1f29e9b919e7a2040b10056023518e58696ee

```md
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

```

## openspec/changes/sync-upstream-161/design.md

- Source: openspec/changes/sync-upstream-161/design.md
- Lines: 1-61
- SHA256: a0881ba960fd44ad1747d6fe2f1ea07e13e13ee866ed2fe8afac447e9f1bc120

```md
# Design

## Context

本仓库与上游 `ddcat-ai/open-ai-canvas` 历史连通（本地根提交 `aea35896` 即上游初始提交；`git merge-base main FETCH_HEAD` = `125864f6`）。上游 main 头为 `0b1d5073`（v1.6.1），自 merge-base 以来上游改了 302 个文件，本地改了 57 个文件，双改 10 个文件。深度 fetch（depth=2000，1501 个提交）已完成并落在本地对象库，后续操作无需联网。数据面：迁移文件 `backend/internal/database/migrations.go` 仅上游侧修改（v43 → v47），本地该目录自 merge-base 零改动。

当前工作区处于 feature 分支 `feature/20261006/character-script-suggestions`（character-script-suggestions change 的 build 阶段），工作树有 Comet 工具态脏文件（AGENTS.md 注入块、归档 .comet.yaml、未跟踪 .zcode/ 与 openspec/）。集成不得触碰该分支与这些未提交产物。

## Goals / Non-Goals

**Goals:**

- 一次合并把上游 v1.6.1 全部提交引入集成分支，双方历史与提交语义完整保留
- 10 个双改文件逐个人工裁决，业务语义并存
- 合并后验证：`go test ./...`、`bun run build`、迁移升 v47
- 推送仅到 origin；upstream 配置为 fetch-only
- 未来可持续同步：合并后历史永久连通，下次同步直接 `fetch + merge`

**Non-Goals:**

- 不重写历史、不 rebase、不使用 `git reset --hard`
- 不处理 `character-script-suggestions` 的 18 个未验收提交（该 change 独立收尾）
- 不在本仓库规格中重复声明上游功能需求（以上游代码与文档为准）
- 不改动 Comet/OpenSpec 工具态产物

## Decisions

### D1：git merge 而非 format-patch + am -3

最初提案基于"历史不连通"的浅 fetch 误判。深 fetch 证实 merge-base 存在后，普通三方合并全面占优：

| 维度 | git merge | format-patch + am -3 |
| --- | --- | --- |
| 提交语义 | 合并提交 + 全部上游提交原样 | 46 个提交逐个重放，合并节点丢失 |
| 冲突处理 | 一次性裁决 10 个文件 | 可能中途停在每个冲突提交 |
| 后续同步 | fetch + merge，零导出成本 | 每次重导 patch 增量 |
| 历史连通性 | 永久连通 | 永不连通（假祖先） |

### D2：集成基于 main，集成分支命名 `sync/upstream-1.6.1`

从 main（`5f17fc92`）切出集成分支，不动 feature 分支。合并完成后经用户决定合入 main 的方式（默认 fast-forward 不可能，产生 merge commit）。

### D3：双改文件裁决基准

以 merge-tree 预演结果为准，逐文件按语义裁决：

- `.gitignore`：两行独立追加（本地与上游各加了忽略项），合并保留双方行
- 3 个文档 mdx（backend-database / features / pending-test）：条目级并存——本地条目 + 上游条目同时保留；`backend-database.mdx` 需补写 v47 表结构说明
- 4 个 web 文件（workspace-sidebar-nav、admin-shell、create/index、welcome/index）：上游多为新增入口/链接的独立 hunk，按 hunk 保留双方逻辑
- `user-data-sync.ts` / `use-appearance-store.ts`：上游在头部/常量区加字段，本地在逻辑区改动，预期 hunk 级自动可解

### D4：迁移无冲突但需行为验证

`migrations.go` 上游侧从 v43 连加 v44–v47；本地未动。合并后启动时自动执行迁移，需在验证阶段用现成数据目录启动后端确认 `schema_migrations` 推进到 47（或至少 `go test ./...` 覆盖迁移测试）。

## Risks / Trade-offs

- **网络不稳**：所有上游对象已在本地，后续 merge 不联网；fetch-only upstream remote 记录在案即可。
- **合并提交体积大**：302 个上游文件一次性进入，出问题时回退手段是放弃集成分支（未合入 main 前 main 不受影响），可接受。
- **双改文件语义冲突**：预演仅 4 处块，但 hunk 级自动合并可能在类型/导入层面引入编译错误——验证阶段以 `go test` / `bun run build` 兜底。
- **feature 分支后续合入**：character-script-suggestions 最终合回 main 时会跨越本次合并，届时冲突面另评，不在本 change 范围。

```

## openspec/changes/sync-upstream-161/tasks.md

- Source: openspec/changes/sync-upstream-161/tasks.md
- Lines: 1-27
- SHA256: a5fae9242125870615bcdd334b5834524117eef38a074ce122edb4b1c320ffe2

```md
# Tasks

## 1. 合并准备

- [ ] 1.1 添加 fetch-only upstream remote 并确认 fetch 配置（`git remote add upstream https://github.com/ddcat-ai/open-ai-canvas.git` + 仅 fetch 校验）
- [ ] 1.2 从 main（`5f17fc92`）切出集成分支 `sync/upstream-1.6.1`，确认工作树脏文件仅限工具态产物

## 2. 合并与冲突裁决

- [ ] 2.1 执行 `git merge upstream/main`（v1.6.1 `0b1d5073`），记录实际冲突文件清单
- [ ] 2.2 裁决 `.gitignore`：保留双方忽略条目
- [ ] 2.3 裁决 3 个文档 mdx（backend-database / features / pending-test）：条目级并存；backend-database.mdx 补 v47 表说明
- [ ] 2.4 裁决 4 个 web 文件（workspace-sidebar-nav / admin-shell / create/index / welcome/index）：hunk 级保留双方逻辑
- [ ] 2.5 裁决 `user-data-sync.ts` 与 `use-appearance-store.ts` 并完成合并提交

## 3. 验证

- [ ] 3.1 `cd backend && go test ./...` 通过
- [ ] 3.2 `cd web && bun run build` 通过
- [ ] 3.3 数据库迁移验证：现成数据目录启动后端或迁移测试确认 v47（upload_reservations / skill_curation / skill_curation_roots）
- [ ] 3.4 核对合并结果：双方提交历史完整、本地业务改动无丢失（抽查 c98c9b39、853c7d2c 对应文件）

## 4. 文档同步与交付

- [ ] 4.1 CHANGELOG 记录本次上游融合（v1.6.1 内容 + 合并事实）
- [ ] 4.2 按需同步 `docs/content/docs/` 专题文档差异描述
- [ ] 4.3 仅推送 origin：`git push origin sync/upstream-1.6.1`（或经用户确认后的合入 main 推送），远端核对 upstream 未收到推送

```

## openspec/changes/sync-upstream-161/specs/upstream-sync/spec.md

- Source: openspec/changes/sync-upstream-161/specs/upstream-sync/spec.md
- Lines: 1-46
- SHA256: 0126610789d643f3b495d7967695589122aab96b56620f779addeaf785fb8626

```md
# Spec Delta

## Purpose

定义本仓库与上游 `ddcat-ai/open-ai-canvas`（原始项目）的同步行为契约：在历史连通的前提下，如何以可追溯、可重复的方式融合上游更新，同时不破坏本地已有迭代，且任何提交只进入本仓库的 origin 远端。

## ADDED Requirements

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

```
