---
comet_change: sync-upstream-161
role: technical-design
canonical_spec: openspec
archived-with: 2026-10-07-sync-upstream-161
status: final
---

# Design Doc: sync-upstream-161（上游 v1.6.1 融合）

## 背景与事实基础

- 历史连通：本地根提交 `aea35896` 即上游初始提交；`git merge-base main upstream/main` = `125864f6`。深度 fetch（depth=2000）已完成，全部上游对象在本地对象库，后续操作不依赖网络。
- 上游 main 头 `0b1d5073`（v1.6.1），自 merge-base 上游改 302 文件、本地改 57 文件、双改 10 文件。
- `backend/internal/database/migrations.go` 仅上游侧修改（v43 → v44–v47），本地 `backend/internal/database/` 自 merge-base 零改动 → 迁移无冲突。
- 当前工作树在 feature 分支 `feature/20261006/character-script-suggestions`，含 Comet 工具态脏文件；集成全程不触碰该分支与未提交产物。

## 方案选型（已确认：方案 A）

**方案 A — `git merge upstream/main`**：三方合并产生一个合并提交。本地 1490+ 提交与上游 46+ 提交语义原样保留；历史永久连通，未来同步零导出成本。

- 方案 B（format-patch + am -3）被否：为"历史不连通"设计；本项目 merge-base 存在，重放 46 个提交丢失合并语义且永不真连通。
- 方案 C（cherry-pick 内容）被否：与"全量融合"目标不符，只适合个别功能。

## 执行序列

```
main(5f17fc92) ──┐
                 ├─ git checkout -b sync/upstream-1.6.1 main
                 ├─ git merge upstream/main   ← 冲突 10 文件，逐个裁决
                 └─ 合并提交 → 验证 → 文档 → push origin
```

1. `git remote add upstream https://github.com/ddcat-ai/open-ai-canvas.git`（fetch-only，无 pushurl）。
2. `git checkout -b sync/upstream-1.6.1 main`。
3. `git merge upstream/main`（默认 merge commit message；`--no-edit`）。
4. 冲突文件按裁决表逐个处理后 `git add`，一次 `git commit` 完成 merge。
5. 验证 → 文档同步提交 → `git push origin sync/upstream-1.6.1`。

## 双改文件裁决表（已确认）

| 文件 | 本地改动 | 上游改动 | 裁决 |
| --- | --- | --- | --- |
| `.gitignore` | Comet 状态块 4 行 | `!docs/content/docs/backend/skill-curation.mdx` | 双方行并存 |
| `docs/content/docs/backend/backend-database.mdx` | `asset_representations` 表行 | "分片上传预留"+"可选技能策展"章节 | 双方并存 |
| `docs/content/docs/overview/features.mdx` | 资产图片绑定段 | "平台技能策展"章节 | 双方并存 |
| `docs/content/docs/progress/pending-test.mdx` | 场景道具绑定清单 | 副本消失/首尾帧等清单 | 双方并存 |
| `web/src/components/layout/workspace-sidebar-nav.tsx` | `displayBrandName` 品牌化 | 登录入口→游客模式 | 不同区域，hunk 并存 |
| `web/src/pages/admin/components/admin-shell.tsx` | 品牌名展示化 | 加"技能分类"导航项 | hunk 并存 |
| `web/src/pages/create/index.tsx` | 品牌名展示化 | CreationLoginDialog/用户会话状态 | hunk 并存 |
| `web/src/pages/welcome/index.tsx` | title 改动态品牌 effect | 删除硬编码 title 行 | **取本地动态 effect**（上游"标题不硬编码"意图已被本地以品牌动态化更强实现；本地 effect 同时服务 og:title 分享卡） |
| `web/src/services/user-data-sync.ts` | `acknowledgeRemoteAssets`+冲突检测 | import 区/生成资产默认值 | hunk 并存 |
| `web/src/stores/use-appearance-store.ts` | 品牌元数据展示化 | `redeemPurchaseUrl` 字段 | hunk 并存 |

裁决原则：业务文件"双方语义并存"，不整文件取单侧；工具态产物（AGENTS.md 注入块、`.zcode/`、`openspec/`、`docs/openspec/`）不参与融合。

## 边界与错误处理

- 合并若出现裁决表之外的冲突（merge-tree 预演是乐观估计）：逐文件重新核实双方意图后按同一原则裁决；语义交织且无法并存时记录到 verification report 并征询用户。
- 合并中途放弃：`git merge --abort`，main 与 feature 分支不受影响。
- 上游侧文件引用本地不存在的上游新模块（如 `creation-login-dialog`、`skill-curation.mdx`）：这些模块随合并一起进入，不单独处理。

## 测试策略

1. `cd backend && go test ./...`（覆盖迁移测试与新增技能策展/上传预留测试）。
2. `cd web && bun run build`（tsc + vite，兜住 hunk 合并的 import/类型错误）。
3. 迁移行为验证：以现成数据目录 `CANVAS_BACKEND_DATA_DIR=../.local/project-workbench-debug` 启动后端，日志确认 schema 迁移推进至 v47（或确认迁移测试覆盖 v45–v47 后以测试结果为准）。
4. 完整性抽查：`acknowledgeRemoteAssets` 仍在 user-data-sync.ts（c98c9b39）、Midjourney 创作配置仍在（853c7d2c）；`git log` 同时含双方提交。
5. 浏览器验收（welcome title）延后到用户侧，与 pending-test.mdx 既有清单一致。

## 回退

推送前：放弃集成分支即可。推送后：以 `git revert -m 1 <merge-commit>` 回退合并（不重写历史）。
