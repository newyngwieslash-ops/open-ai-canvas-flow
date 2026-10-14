# 画布同步修复验证

变更：fix-canvas-sync-reconciliation。用户确认在 sync/upstream-1.6.3 原分支处理，并在新增历史读取接口时明确选择“沿用当前修复流程完成”。

## 结果

本轮代码、专项测试、构建与本地服务部署已完成；原 Edge 登录会话中的实际缓存恢复仍待验收，不宣告用户画布已经恢复，不归档。此前用户刷新后仍报“缺少已确认基线”的实际验收失败保留在记录中。

| 检查 | 证据 | 结果 |
| --- | --- | --- |
| 完整性 | tasks.md 5/5 完成，覆盖原竞态、journal、持久化、历史恢复及本地部署 | PASS |
| 设计一致性 | 优先现有基线，最后按旧版本读取回执，核对 ID、revision、哈希后复用三方合并 | PASS |
| 前端专项 | 四个相关测试文件：57 pass，0 fail，196 assertions | PASS |
| 前端构建 | bun run --cwd web build，TypeScript 与 Vite 成功 | PASS；已有大包提示 |
| 后端域测试 | go test ./internal/canvas -count=1 | PASS |
| HTTP 专项 | 历史版本、增量路由、ETag、gzip 专项测试 | PASS |
| 后端构建与启动 | Go 1.25，go build -ldflags '-s -w'；本地 8178 服务启动 | PASS |
| 实际服务入口 | /api/health 返回 200；通过前端 15178 代理请求历史版本接口，未登录返回 401 | PASS；不等同登录验收 |
| 实际数据只读检查 | 原画布 revision 354、76 节点、79 连线；229–354 共 126 份完整回执，ID、版本和数组字段均有效 | PASS |
| 部署数据保护 | SQLite backup API 备份完整性为 ok；重启前后画布和任务数量、1 条 waiting_approval 均未变化 | PASS |
| 用户实际缓存恢复 | 当前工具不能访问用户 Edge 登录会话 | 待用户刷新确认 |
| 审查策略 | hotfix 的 review_mode=off；已人工核对调用链与差异 | 按配置跳过自动审查 |

## 根因及失败证据

前序测试证明保存期间改标题会误发删除 Agent 节点/连线，并复现纯悬空连线请求重试阻塞。修复使用原请求投影、远端完整文档和当前本地内容三方合并；原 mutationId 优先重放，读取、合并和持久化完成后才清理 journal。

会话重建回归最初为 13 pass、2 fail，均在 Agent 刷新抛出“缺少画布合并基线”。已确认快照此前只驻留内存，且打开、刷新恢复逻辑不一致。修复按账号持久化确认快照，统一入口，助手审批仅等待当前画布保存。

用户刷新后仍报错，证明持久化只能防止未来丢失，不能重建旧客户端早已丢失的快照。新回归覆盖打开、刷新和保存三条路径：缺少本地完整基线，但服务端保留旧版本完整回执。实施历史读取前结果为 24 pass、3 fail，均因缺少基线失败；实施后通过。另覆盖错误哈希、错误版本、错误画布、缺失回执、权限拒绝、断网和同字段冲突，均保留本地编辑且不发送覆盖写入。

后端复用现有 mutation receipt，不新增表或迁移。先检查现存画布归属，再按用户、画布、版本查询；不恢复已删除画布，不重放生成。损坏或摘要式旧回执不能作为基线。HTTP 测试覆盖登录、跨账号、未来版本、非法版本及 no-store 响应。

## 运行命令

- 前端：`bun test --cwd web test/user-data-canvas-sync.test.ts test/user-data-asset-sync.test.ts test/canvas-operations-build.test.ts test/agent-canvas-patch.test.ts`；`bun run --cwd web build`。
- 后端（backend 目录，Go 1.25）：`go test ./internal/canvas -count=1`；`go test ./internal/handler -run 'Test(CanvasRevisionRecoveryHTTP|UserDataIncrementalRoutes|CanvasProjectResponse|AcceptsGzip)' -count=1`。
- 本地部署：`go build -ldflags '-s -w' -o ../.local/canvas-recovery.exe ./cmd/server`。沿用原数据目录，CANVAS_AUTO_MIGRATE=false，监听 127.0.0.1:8178。

## 扩展检查及环境问题

- 完整 handler 包测试有一项公告媒体 Range 失败：TestAppearanceUpdateAnnouncementMediaRangeAndDisabledAccess 期待 206，实际 500，错误为 fs.PathError。使用 Go -overlay 将所有未提交 Go 修改替换为 HEAD 原始源码后，该用例仍以同样原因失败，确认不是本次改动引入。隔离覆盖文件和日志均在 Git 忽略目录，工作区未回滚；本次不修复公告媒体模块。
- 初次历史 HTTP 测试的清理回调返回类型错误已修正，专项重新通过。后端未取得有效 RED，不能把环境或编译失败当作缺陷复现；有效 RED 来自前端真实同步服务测试。
- 默认带调试信息的 Windows 构建不能启动。检查 PE 发现旧 MinGW 生成的调试段地址超出映像范围；按本机 go run 使用的无调试符号方式构建后成功启动。未修改产品代码或引入构建依赖。仅将实际启动的无调试构建记为部署成功。

## 验收与范围

已核对 proposal、design 和 tasks：从共享恢复函数覆盖打开、Agent 刷新、增量、保存及全量同步；保持乐观冲突、账号切换和原请求幂等保护。无独立技术设计文档或 delta spec，本次 hotfix 恢复既有数据保护合同；新增只读接口已同步 OpenAPI 与 HTTP 专题。

最终验收需在原 Edge 画布刷新，观察缺少基线提示是否消失，核对本地编辑、已有生成结果和有效连线。哈希或历史不匹配时保留编辑，不能强制覆盖。这里的接口 401 与完整历史记录存在不能证明浏览器缓存已经恢复。

AGENTS.md、后端 Agent/尺寸修改、重启脚本、其他文档与历史 Comet 元数据为既有无关改动，保留且不纳入提交。pending-test.mdx 仅提交本任务段落。数据库、日志、备份、构建文件和临时测试覆盖均不提交。
