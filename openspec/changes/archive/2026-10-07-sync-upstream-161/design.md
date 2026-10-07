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
