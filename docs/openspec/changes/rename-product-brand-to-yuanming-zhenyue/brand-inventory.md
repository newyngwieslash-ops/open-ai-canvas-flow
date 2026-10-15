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
