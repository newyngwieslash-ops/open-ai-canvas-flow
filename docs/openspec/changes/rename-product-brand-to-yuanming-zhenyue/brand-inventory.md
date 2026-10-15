# 品牌排查基线

## 已完成的只读检查

- 文本扫描：377 个文件、592 个命中行；排除 Git 元数据、node_modules、构建目录、.local 和 .taste-skill。
- 分布：plugin-packages 189，backend 128，docs 27，web 20，scripts 5，assets 2，.github 1，其余为 README、CHANGELOG、CONTEXT、AGENTS 与原 UI 品牌方案各 1。
- 压缩包扫描：105 个 .yingce-plugin 中，94 个包的文本条目仍包含中文旧品牌。扫描解包读取文本，没有修改压缩包。
- 新欢迎页已显示新名，但原 workbench-create.webp 仍含旧品牌像素，当前页面已改用新截图；需排查其他引用后再决定替换或清理。
- web/public/logo.svg 是中性几何图形，不含中文品牌文字；不能把替换文字误当成必须重画图形。

## 关键入口与调用链

| 入口 | 当前发现 | 后续验证 |
| --- | --- | --- |
| web/src/lib/brand-display.ts | 只映射旧默认值，原 appearance 未变 | 与真实新默认身份统一，保留自定义品牌 |
| web/src/stores/use-appearance-store.ts | 默认品牌、SEO、版权和 YINGCE STUDIO 仍为旧身份 | 初始/成功/失败降级、元数据与重置 |
| backend/internal/app/appearance.go | defaultAppearanceBrandName 为旧名；readAppearance 中已保存 BrandName 覆盖默认值 | 存量配置、默认投影及无关字段保留 |
| backend/internal/auth/host.go | auth.DefaultBrandName 为旧名 | 默认协议、验证码/重置邮件 |
| backend/internal/auth/email.go | FromName 等于旧默认值时继承站点名 | 默认值改变后旧发件人仍正确继承 |
| backend/internal/app/appearance_canvas.go、web/src/lib/canvas/agent-appearance.ts | Agent 默认名仍是旧名 | 新/旧配置及自定义 Agent |
| web/src/components/canvas/previs/canvas-previs-workbench.tsx | 直接硬编码旧字标 | 真实预演入口 |
| backend/builtin/影策短剧制作工作流 | 目录、标题、作者含旧名；skillId 为稳定技术身份 | 名称与引用同步、稳定 ID 不变 |
| plugin-packages | 作者、说明和内嵌契约文档存在旧名 | 源清单/文档/分发包一致 |
| backend/internal/handler/openapi.yaml | API 文档标题仍为旧品牌 | 元数据更新，接口合同不变 |

## 进入实施前的检查清单

- 检查所有中文命中是否确属品牌，避免误改普通语言中的“摄影策略”等组合。
- 按引用关系检查产品截图、SVG/HTML、预置视频及生成/内嵌文档；保留第三方 Logo 与个人头像。
- 检查运行实例公开外观与需要管理员读取的设置，只输出品牌字段，不泄露密钥或个人信息。
- 盘点当前配置中包含旧名的全局品牌文案；按明确字段处理，保留其他配置和用户创作内容。
- 按项目现有打包方式重建插件，核对清单、文档、版本/摘要和解包结果；不得只改源码后宣称包内已完成。
- 残留分类：必要旧数据识别/回归输入、稳定机器标识、真实第三方出处、用户参考与历史审计；每项记录位置、原因和是否产品可见。

## 工作区归属

既有欢迎页 index.tsx/story.ts/welcome.css、相关测试、credits.html、pending-test.mdx 及新图像来自当前聊天已获授权的 UI 工作，作为既有输入保留，不借改名重做版式。`.taste-skill/` 和根目录用户上传参考图保持原样。新品牌变更文档遵循项目配置的 docs/openspec 布局；无关 Comet change 不变。

## 计划阶段补充的视觉检查

以下为直接查看仓库原图得到的结果，不代表改后页面验收通过，也没有修改图片。

| 素材 | 看到的品牌情况 | 实施动作 |
| --- | --- | --- |
| assets/login.png | 左上有旧中文字标，左下为 YINGCE STUDIO | 从改后真实登录页重截 |
| assets/create.png | 主标题下有旧品牌副标 | 从改后真实创作页重截 |
| assets/tldraw-license-key-settings.png | 管理后台左上有旧中文字标，密钥输入已遮蔽 | 从改后真实绘图设置页重截，不展示真实密钥 |
| assets/backend.png、assets/huabu.png | 页面字标为“无限画布”，未见目标旧品牌 | 记录检查结果，不因本次任务重写其他名称 |
| assets/canvas.png、assets/huabu-info.png | 本次直接查看未见目标旧品牌；有创作素材及第三方水印 | 保留创作内容与真实出处 |
| assets/project-detail.png、web/public/welcome/workbench-project.webp | 本次直接查看未见目标旧品牌 | 不需为改名重做截图 |
| web/public/welcome/workbench-canvas.webp | 本次直接查看未见目标旧品牌 | 保留创作画面 |

原 workbench-create.webp、其替代截图、其他品牌相关素材和外部视频仍需纳入实施后的最终核查；不由上述局部检查推断所有视觉素材通过。

## Task1 可复跑扫描基线

上面的人工检查和视觉表保留为历史证据。下面的自动扫描覆盖范围更大，计数不能与前面的人工检索直接相减。

基线旧品牌检索词：`影策`、`YINGCE STUDIO`、`Open AI Canvas`。

命令（仓库根目录，Python 3.9+ 和 Git，均无新增依赖）：

```powershell
python scripts/audit-product-brand.py --self-test
python scripts/audit-product-brand.py --root . --json
```

- 自检只使用临时 Git 仓库、临时文本与 ZIP，不读取或改写当前产品。真实扫描只读，不替换品牌、不重打包、不访问运行配置或网络。
- 输入先由 `git ls-files --cached --others --exclude-standard -z` 建立，再补入被现有 ignore 规则隐藏的 `docs/` 文档。当前维护文档、源码、插件目录均没有整目录品牌豁免。
- Git、依赖、构建、缓存、Agent 状态目录、其他 worktree 和两个明确的根目录用户参考文件不属于产品输入。目录符号链接和文件符号链接不跟随；含独立 `.git` 的其他 checkout 不扫描。
- 返回发现数组；字段为 `path`、`entry`、`line`、`text`、`category`、`reason`。行号从 1 开始；0 表示路径、二进制或包覆盖记录，普通文件的 entry 为 null。保留原始文本；JSON 字符串的 Unicode 转义仅用于匹配，定位仍指向原物理行与包条目。
- product 表示仍需审查的旧名候选或读取失败，存在即退出 1；exception 区分精确旧值回归、默认值识别、历史证据、稳定技术 token / 实际引用，以及未检查二进制和包覆盖记录；non_brand 单列普通摄影用语。一个物理行可因不同理由产生多条发现，记录数不是唯一命中行数。
- 例外表使用精确文件和完整行内容，同文件其他行继续报告；技术 token 按命中区间分类，同一行其他品牌继续报告。未逐项确认的测试、历史文档和协议标识保守列为 product，不能据此批量替换。
- 技术例外精确覆盖 `yingce.plugin/v1`、`yingce.plugin/v2`、`yingce.plugin/package-v1`、`yingce-system`、`yingce-short-drama-workflow` 和 `brandSlug/open-ai-canvas` 等明确字段值；相似但不同的 token 与同一行其他可见品牌仍报告。
- 完整扫描数组（全部包覆盖、非文本对象和引用）保存在 `.local/cache/product-brand-task1-baseline.json`；证据摘要为 `.superpowers/sdd/2026-10-15-product-brand-identity/task-1-report.md`。二者与本地保护快照不纳入提交。

本次最终扫描统计：4,824 条记录，涉及 1,967 个路径；product 1,272、exception 3,542、non_brand 10。105 个包全部读取，包含 318 个文本条目、35 个非文本条目；全输入共 546 个未检查非文本对象，读取错误 0。100 个包仍有保守 product 候选（包含未分类旧值，不等于全部存在可见中文旧名）。12 个保护文件的 SHA-256 全部保持一致。基线退出 1 为预期结果，不代表扫描器失败。

### 待人工关注

- 所有 105 个分发包逐个打开、逐个读取非目录条目，包含没有品牌命中的包；不提取到磁盘，也不把整个插件目录当作例外。
- UTF-8 解码失败或含 NUL 的对象只记为未检查二进制。图片像素、视频、字体、音频及外部视频不属于文本扫描覆盖；前面的视觉检查表仍是唯一已有视觉证据。
- 稳定协议、模块、环境变量、服务名、Cookie、存储键和真正上游来源不得因为命中就改动。JSON 仅解码双引号字符串，不是 JavaScript、HTML 或其他语言的通用转义解析器。
- 扫描器自检数据、例外配置自身和未精确确认的历史记录可能仍产生保守候选，需按 reason 和实际调用语义处理，不能将候选数当作产品可见缺陷数。
- `.local/cache/brand-workspace-before.json` 的 12 个 SHA-256 作为既有 UI 保护依据；扫描完成后逐一核对，不提交该快照。

## Task2 运行实例配置盘点（OpenSpec 1.2）

本节是一次只读快照。运行接口值、运行进程证据与源码规则分开记录；没有保存配置、重置外观、重启服务、发送邮件或读取业务数据库。目标展示名“源铭振跃”尚未在本任务应用。

### 已读取的运行证据

`GET http://127.0.0.1:3000/api/public/appearance` 返回 HTTP 200、`code=0`，形状为 `{data:{appearance}}`。

| 品牌字段 | 实际返回值 |
| --- | --- |
| `brandName` | `影策` |
| `brandSlug` | `open-ai-canvas`（稳定技术标识，保持） |
| `seoTitle` | `影策` |
| `seoDescription` | `影策，面向 AI 影视与短剧创作的工作台。` |
| `seoKeywords` | 空字符串 |
| `footerCopyright` | `© 2026 影策. All rights reserved.` |
| `schemaVersion` / `configured` / `revision` | `10` / `false` / `builtin` |

| Agent 展示字段 | 实际返回值 |
| --- | --- |
| `canvas.agentName` | `影策` |
| `canvas.launcherLabel` | `Agent` |
| `canvas.panelTitle` | `画布助手` |
| `canvas.welcomeTitle` | `在这里，和{agentName}让灵感，慢慢成形` |
| `canvas.welcomeDescription` | `从一个想法开始，和{agentName}一起创作。` |
| `canvas.inputPlaceholder` | `输入操作指导；用 @ 引用画布节点，用 / 或 、 引用 Skills` |
| `canvas.avatarType` / `canvas.avatarHeight` | `orb` / `220` |
| Live2D 资源 / 入口是否存在 | `false` / `false` |

其他字段仅记录非敏感结构和布尔值：`logoConfigured`、`darkLogoConfigured`、`authVideoConfigured`、`authVideoPosterConfigured` 均为 `false`，但四类公开资源 URL 均非空；`skinId=classic`；`icpFilingEnabled=false`，备案号为空；Hero 标题/说明均不含旧中文品牌。`updates` 结构为 `enabled/currentVersion/releases`，实际为 `enabled=false`、当前版本为空、`releases` 长度 `0`。不记录资源 URL、主题明细、备案内容或自由文案全文。

结合 `appearance.go#readAppearance/publicAppearanceSetting` 的静态实现，`configured=false/revision=builtin` 对应未找到已保存的 appearance 记录，当前品牌、Agent、SEO 和版权符合内置默认投影；这不是“已保存的自定义旧品牌”证据，也不证明资源已经播放或页面验收通过。

`GET /api/auth/settings` 返回 HTTP 200、`code=0`：`firstUser=false`、`registrationEnabled=false`、`emailEnabled=false`、`emailCodeRequired=false`；公开 `agreementTitle=影策服务协议`，协议正文缺省/空。此接口没有协议继承标记，不能仅凭标题等于默认值认定后台原始标题未配置。

### 管理访问边界与未覆盖原值

CUA 发现的可用浏览器只有应用内浏览器和 MCP Apps，均无现有标签页；没有可复用的已授权会话证据。以下请求为不携带认证凭据的 GET，均返回 HTTP `401`、业务 `code=401`、`reason=unauthorized`，没有 `data.setting`：

| 接口 | 精确未覆盖项 |
| --- | --- |
| `/api/admin/settings/appearance` | 管理 `{setting}` 原值、资源 ID、主题、自由文案、自定义 Agent、公告保存内容及管理元信息 |
| `/api/admin/settings/email` | 实际 `fromName`、`fromNameInherited`、自定义邮件署名归属；公开 `emailEnabled=false` 不能证明未保存发件人 |
| `/api/admin/settings/registration` | 实际 `agreementTitle/agreementContent`，是否空标题继承或显式保存同名协议 |

没有提取 Cookie、创建/重置账号、查询数据库或绕过权限；上述未知项不填默认值，不标为已通过。官方技能、工具、插件的已安装版本/摘要和用户关系也未通过授权接口读取。

### 当前服务和数据归属

| 证据层级 | 发现与边界 |
| --- | --- |
| 运行：前端 | 3000 监听进程为 Node；`/` 含 Vite client。读取运行模块 `/src/pages/welcome/story.ts` 的 source map，`sourcesContent` 与当前工作区文件逐字一致，启动命令也引用当前 checkout。未操作页面或生成仿制页面。 |
| 运行：实际代理 | 仅白名单读取 Vite 转换后 `/src/services/api/request.ts` 的公开环境字段：`VITE_API_PROXY_TARGET=http://127.0.0.1:28080`，`VITE_CANVAS_BACKEND_URL` 未配置。没有输出其他环境字段或 URL 参数。 |
| 运行：后端 | 28080 由 `server.exe` 监听，其父链含 Go 和 PowerShell；启动 PowerShell 引用当前 checkout，包含 `CANVAS_BACKEND_DATA_DIR`、`.local/project-workbench-debug` 和 28080。直连 28080 的公开外观返回 HTTP 200、旧品牌、`configured=false/revision=builtin`，与 3000 的对应字段一致。进程证据只输出名称和归属布尔值。 |
| 运行：8080 | 由 Docker Desktop 后端进程监听，`/api/public/appearance` 返回 404，根路径返回 200；不是本次可用的外观 API。不能把“直连不可用”误写成 TCP 未监听，也不能因此重启或更换 3000 的代理。 |
| 静态：代理 | `web/vite.config.ts` 使用进程环境 `VITE_API_PROXY_TARGET`，缺省才取 8080；`request.ts` 使用 `VITE_CANVAS_BACKEND_URL || /api`。当前运行覆盖值优先于源码默认端口。 |
| 静态：数据 | `backend/cmd/server/main.go#run` 从 `CANVAS_BACKEND_DATA_DIR` 取资源/运行目录，缺省 `data`；数据库由 `CANVAS_DATABASE_DRIVER`、`DATABASE_URL` 决定。启动父链支持当前 `.local/project-workbench-debug` 归属，但未读取子进程完整环境、DSN、数据库文件或表；实际驱动和继承环境仍未覆盖，不能断言 SQLite 或某个 Compose。 |
| 静态：其他脚本 | 本地遗留修复脚本指向 8178；这只是脚本配置，不能覆盖当前 28080 的运行证据。仓库 `docker-compose.local.yml` 的 `/data` 及默认 3000 映射同样不是当前宿主机后端的运行证明。 |

后续需要保存设置时，应沿已确认的 3000 → 28080 实例重新核对归属和管理员会话；不能把另一个 8080 服务、`backend/data` 或遗留 8178 启动脚本当成本实例。当前 Go 进程与工作区源码是否完全同一编译版本未作验证。

### 默认、自定义及合法保存边界（静态依据）

| 范围 | 源码依据与归属规则 |
| --- | --- |
| 全局外观 | `backend/internal/app/appearance.go#readAppearance` 缺记录才取默认，存在记录时解码保存值；品牌空值回退默认，非空自定义品牌保留。SEO/版权只在对应值为空时从站点名投影。GET 路径未见 `SaveSystemSetting`；未以查库验证实际零写入。 |
| Agent | `appearance_canvas.go#defaultCanvasAppearance` 默认名为旧品牌，其他字段为展示文案和模板；保存的 `canvas` 与品牌名是不同字段。非空自定义 Agent 和自由文案不能因默认旧名命中而批量替换。公开值符合当前默认；管理原值未取得。 |
| 邮件 | `backend/internal/auth/host.go` 的 `DefaultBrandName=影策`。`email.go#publicEmailSetting` 在原 `FromName` 为空或等于该默认值时设 `fromNameInherited=true`；`resolveEmailSender` 才把展示/发送署名解析为站点名；其他自定义署名保留。管理读取结果不是完整敏感邮件配置的可公开备份。运行继承标记未知，未发送测试邮件。 |
| 注册协议 | `registration.go#RegistrationAgreement/defaultAgreementTitle` 去除外层书名号后，只有空标题才跟随站点品牌；非空同名旧标题仍可能是显式配置，不能当默认迁移。更新请求的协议字段为指针：未提交保留原文，明确空值才清空。运行公开正文为空，后台原始归属未知。 |
| 外观 PATCH | `handler/appearance.go` 先读取管理当前值，再以其 `AppearanceSetting` 为底绑定 PATCH；`app/appearance.go#UpdateAppearance` 校验、保存、审计，错误返回调用方。使用既有 `/public/appearance`、`/admin/settings/appearance` 与既有 payload，不改变合同。若后续确需显式保存旧全局配置，先完整备份到 Git 忽略且访问受保护的 `.local/`，保留全部字段；本次不写配置，因此未生成完整敏感备份。 |
| 邮件/注册 PATCH | `handler/finance.go` 经 `currentUser`，service 再校验管理员；邮件写入还涉及原密码保留/加密，不可仅凭发件人字段重建整份设置。保存失败应上抛，不重置、不清库、不使用 DELETE 恢复默认。实际写入、失败回滚和备份恢复均未执行。 |

### 官方内容更新归属（静态依据，未触发）

- `backend/cmd/server/main.go#run` 启动会调用内置工作流、技能、技能包和工具初始化；本任务未启动或重启进程，未触发这些更新。
- `internal/skills/skills_builtin.go#EnsureBuiltinSkills` 以稳定技能 ID 更新目录，比较内容 hash 和当前版本 ID 后决定是否追加 builtin 文件版本；用户关系另表保留。`repository/skills.go#UpsertBuiltinSkills` 按 ID 更新指定目录字段，不能由此声称用户数据已运行验证或所有 ID 冲突都有保护。
- `internal/tools/tools_seed.go#EnsureBuiltinTools` 通过稳定工具 ID 幂等同步；`repository/tools.go#UpsertBuiltinTools` 在事务中拒绝与非 builtin 工具 ID 冲突，不应重建用户工具。
- `internal/app/protocol_plugins.go#bootstrapBuiltInPlugins` 从官方 `.yingce-plugin` 包/内置清单刷新 registry 与包摘要，保留既有 enabled；非内置来源的其他记录保留，移除已不在发行来源中的内置记录。registry、包缓存位于该 runtime 的 dataDir 下。改源文件不代表已安装包已更新，更不能改扩展名、稳定 ID、协议 token 或实际上游。
- `internal/app/project_workflow.go#EnsureBuiltinProjectWorkflowTemplate` 同一模板 key/version 已存在即返回；仅改展示源码不保证存量模板更新，不能重建用户工作流来完成改名。
- 公开 `updates.enabled=false` 只描述管理员自定义公告；`web/src/components/layout/app-changelog-dialog.tsx` 此时展示构建注入的 `__APP_CHANGELOG__`，由 Vite 读取 `CHANGELOG.md`，与技能/插件内容更新、后端发布更新是不同链路。未覆盖实际弹窗、已安装内容或升级执行结果。

### 工作区保护与验证范围

开始时分支为 `codex/welcome-ui-hallmark`，索引为空。对 `.local/cache/brand-workspace-before.json` 的 12 个文件逐个检查存在性及 SHA-256：首次全部一致；提交前再次核对，结果记入 Task2 scratch 报告。既有 8 个 tracked 修改、`.taste-skill/`、用户参考图及 3 个欢迎页素材均保留。工具目录存在，但不在这 12 个文件的哈希清单内，未声称逐文件字节校验。

本任务仅追加此盘点文档并提交；scratch 报告位于 `.superpowers/sdd/2026-10-15-product-brand-identity/task-2-report.md`，不提交报告或 `.local/` 快照。RED/GREEN 不适用：没有产品逻辑改动，仅做只读盘点和文档编辑；没有添加镜像测试，没有运行全库测试、构建或品牌扫描器。管理原值、真实邮件、写入/恢复、UI 交互、官方内容实际升级仍未覆盖。
