# Tasks

## 1. 合并准备

- [x] 1.1 添加 fetch-only upstream remote 并确认 fetch 配置（`git remote add upstream https://github.com/ddcat-ai/open-ai-canvas.git` + 仅 fetch 校验）
- [x] 1.2 从 main（`5f17fc92`）切出集成分支 `sync/upstream-1.6.1`，确认工作树脏文件仅限工具态产物

## 2. 合并与冲突裁决

- [x] 2.1 执行 `git merge upstream/main`（v1.6.1 `0b1d5073`），记录实际冲突文件清单
- [x] 2.2 裁决 `.gitignore`：保留双方忽略条目
- [x] 2.3 裁决 3 个文档 mdx（backend-database / features / pending-test）：条目级并存；backend-database.mdx 补 v47 表说明
- [x] 2.4 裁决 4 个 web 文件（workspace-sidebar-nav / admin-shell / create/index / welcome/index）：hunk 级保留双方逻辑
- [x] 2.5 裁决 `user-data-sync.ts` 与 `use-appearance-store.ts` 并完成合并提交

## 3. 验证

- [x] 3.1 `cd backend && go test ./...` 通过
- [x] 3.2 `cd web && bun run build` 通过
- [x] 3.3 数据库迁移验证：现成数据目录启动后端或迁移测试确认 v47（upload_reservations / skill_curation / skill_curation_roots）
- [x] 3.4 核对合并结果：双方提交历史完整、本地业务改动无丢失（抽查 c98c9b39、853c7d2c 对应文件）

## 4. 文档同步与交付

- [x] 4.1 CHANGELOG 记录本次上游融合（v1.6.1 内容 + 合并事实）
- [x] 4.2 按需同步 `docs/content/docs/` 专题文档差异描述
- [x] 4.3 仅推送 origin：`git push origin sync/upstream-1.6.1`（或经用户确认后的合入 main 推送），远端核对 upstream 未收到推送
