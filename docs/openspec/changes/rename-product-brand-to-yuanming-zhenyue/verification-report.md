# Verification Report — `rename-product-brand-to-yuanming-zhenyue`

## 1. 验证概览

- **Change**: `rename-product-brand-to-yuanming-zhenyue`
- **Branch**: `codex/welcome-ui-hallmark`
- **Verify Mode**: `full`
- **Verify Result**: `pass`

## 2. 自动化测试、类型检查、Lint 与生产构建证据

### 2.1 前端验证（工作目录 `web/`）

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `bun test test/brand-display.test.ts test/appearance-bootstrap.test.ts test/appearance-refresh.test.ts test/canvas-appearance.test.ts test/auth-registration-agreement.test.ts test/welcome-story.test.ts` | PASS（`55 pass, 0 fail`, `319 expect() calls`） | 覆盖 UI 品牌映射、外观启动与刷新规范化、旧版权/SEO 模板迁移、自定义品牌保留、画布自定义外观、注册服务协议标题及欢迎页品牌消费 |
| `node --test test/welcome-title.test.mjs` | PASS（`2 pass, 0 fail`） | 覆盖欢迎页标题解析与回退 |
| `bun run typecheck` | PASS（`tsc --noEmit` 退出 `0`） | 全量 TypeScript 类型检查通过 |
| `bun run lint` | PASS（退出 `0`） | UI 退场规则检查通过 |
| `bun run build` | PASS（`✓ built in 5.84s`，退出 `0`） | 多入口生产构建（`index.html`、`welcome/index.html`）通过 |

### 2.2 后端验证（工作目录 `backend/`）

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `go test ./internal/auth ./internal/skills ./internal/protocol -count=1` | PASS（`ok yingce/backend/internal/auth`, `ok yingce/backend/internal/skills`, `ok yingce/backend/internal/protocol`） | 覆盖默认品牌身份、注册协议默认标题与旧值规范化、邮件继承型发件人、内置技能种子与已安装官方技能刷新、105 个官方插件归档与协议解析 |
| `go test ./internal/app -run "TestAppearance|TestDefaultAppearance|TestNormalizeAppearance|TestLive2DAppearance|TestCloudAgentIdentity|TestProtocolPlugin|TestOfficialProtocol|TestBuiltin" -count=1`（配 `CANVAS_SETTINGS_ENCRYPTION_KEY`） | PASS（`ok yingce/backend/internal/app`） | 覆盖外观默认身份、存量旧模板规范化、恢复默认、Live2D 外观、云端 Agent 身份、官方协议插件元数据更新与用户插件保留 |
| `go test ./internal/handler -run "TestAppearance" -count=1`（配 64 位十六进制 `CANVAS_SETTINGS_ENCRYPTION_KEY`） | PASS（`ok yingce/backend/internal/handler`） | 覆盖外观与更新公告路由注册及 Range/权限响应 |
| `go test ./internal/app ./internal/repository ./internal/handler -count=1`（Windows 宿主全包说明） | 环境已知限制（非本次引入） | Windows 下 `settings.go#publishSettingsKey` 对只读目录句柄调用 `dir.Sync()` 触发 `Access is denied`、`admin_system_performance_test.go` 依赖 Unix socket Redis、`repository` 的 `TestGmailMailboxRegistrationConcurrent` 在 Windows TempDir 清理时受 SQLite 句柄释放时序影响（见 `CHANGELOG.md` 既有说明） |

### 2.3 品牌扫描与归档完整性验证

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `python scripts/audit-product-brand.py --self-test` | PASS（退出 `0`） | 扫描器自检通过（覆盖源码、文件名、ZIP 归档、精确行例外、ignored docs、排除目录、非文本记录与幂等性） |
| `python scripts/audit-product-brand.py --root . --json` | PASS（退出 `0`，`product = 0`） | 仓库全量文本、文件名、维护文档与 105 个 `.yingce-plugin` 归档扫描 `product` 违规为 `0` |
| `git diff --check` | PASS（退出 `0`） | 无空白错误或冲突标记，LF/CRLF 行尾与原文件保持一致 |

## 3. 隔离环境真实浏览器与媒体核查证据

在隔离环境（前端 `http://127.0.0.1:3010` / `http://localhost:3010`，后端 `http://127.0.0.1:28110`，独立 SQLite 数据目录 `.local/project-workbench-debug/brand-preview-task10-20261016`）完成真实路由捕获与验证：

- **登录页（`http://localhost:3010/login`）**：1280×720 浅色真实页面截图更新至 `assets/login.png`（本地原图 `.local/cache/task10/login-full.jpg`），左上角品牌字标与底部版权均显示“源铭振跃”，英文副标为中性“AI 创作工作台”。
- **创作首页与工作区侧栏（`http://127.0.0.1:3010/`）**：1440×1000 浅色真实页面截图更新至 `assets/create.png`（本地原图 `.local/cache/task10/create-full.jpg`），侧栏顶部品牌与工作区入口统一显示“源铭振跃”。
- **管理后台设置页（`http://127.0.0.1:3010/admin/settings/drawing-engine`）**：1440×1000 浅色（`assets/tldraw-license-key-settings.png` / `.local/cache/task10/drawing-source.jpg`）与深色主题（`.local/cache/task10/drawing-dark.jpg`）真实页面核验通过，管理端侧栏顶部品牌块显示“源铭振跃”，未填入任何真实密钥。
- **欢迎页（`/welcome`）**：1440px 桌面（`.local/cache/task10/welcome-desktop.jpg`，`scrollWidth=1440`）与 390×844 窄屏（`.local/cache/task10/welcome-mobile.jpg`，`scrollWidth=375`）真实页面核验通过，Tab 聚焦“前往工作台介绍”具备 `:focus-visible` 轮廓、Enter 跳转 `#workbench` 正常；已批准的 `fantasy-dawn.png`、`paper-grain.svg` 与暗色 `workbench-create-branded.jpg` 原字节完整保留。
- **视频与海报媒体**：本地 `web/public/welcome/charge/sequence.mp4`（7 秒，每秒采样 `.local/cache/task10/sequence-contact.jpg`）与默认登录背景视频/海报（`.local/cache/task10/default-video-whole-contact.jpg`）抽样核查未见目标旧品牌。

## 4. 未覆盖边界与诚实披露

- 未在生产环境或用户正式开发库上执行恢复默认操作，未发送真实 SMTP 邮件，未触发付费模型生成。
- 320px、375px、414px、768px 四个附加断点下的登录/注册/找回密码、后台外观设置页、预演工作台与助手面板未在本轮逐一重跑实时浏览器交互，保留在 `docs/content/docs/progress/pending-test.mdx` 待用户在目标环境最终确认。
- 默认远程登录视频的 AAC 音轨与全部 4,325 帧未逐帧人工审阅；`assets/wx.jpg`（含原上游微信群名称）与 `assets/qq.jpg` 作为上游历史社群邀请图原样保留并在 `brand-media-audit.md` 中明确标注。

## 5. 残留终审与回滚方案

- 全部旧品牌命中均已分类为：
  1. 源码精确旧默认识别与无损规范化入口（`category: exception`）；
  2. 旧配置迁移与自定义品牌保留回归测试（`category: exception`）；
  3. 稳定技术标识与真实上游链接（`category: exception`）；
  4. 普通摄影术语“摄影策略”（`category: non_brand`）；
  5. 上游许可证与历史归档/调研/变更日志（`category: exception`）。
- **回滚方案**：如需回退本次品牌替换，仅对本分支本次变更提交范围执行 `git revert`，不动用 `git reset --hard`，不删除任何数据库、资源文件或欢迎页既有未提交素材。
