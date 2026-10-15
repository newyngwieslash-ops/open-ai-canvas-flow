---
change: rename-product-brand-to-yuanming-zhenyue
design-doc: docs/superpowers/specs/2026-10-15-product-brand-identity-design.md
base-ref: efc5126d8b5d9fed6fac62fe8d9417028d214ef8
---

# 源铭振跃产品品牌实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将项目自有品牌展示统一为“源铭振跃”，覆盖存量配置、系统通信、内置内容、实际插件包和视觉素材。

**Architecture:** 修改真实默认值及已有规范化入口，保留当前外观配置、认证、技能和插件职责边界。复用官方内容启动同步，精确处理旧默认值；不增加通用品牌服务，不做全库 replace。

**Tech Stack:** React/TypeScript、Bun、Go/GORM、现有插件协议与 ZIP、Python 标准库、真实浏览器。

## Global Constraints

- 产品自有展示名为“源铭振跃”；可见英文旧品牌使用新中文品牌或中性功能说明。
- 不改 API 路径、JSON 结构、Cookie、存储键、环境变量、Go module/import、插件协议/扩展名/稳定 ID、部署服务或实际上游来源。
- 保留其他自定义品牌、Agent、邮件署名、自由文案、Logo/媒体/主题/备案/公告、密钥和用户创作数据。
- GET 配置读取不新增持久写入；保存失败向上抛错，不恢复默认、不清库、不清缓存。
- 不增加依赖；手工源码改动使用 apply_patch；机械目录移动、生成归档和浏览器截图使用对应工具。
- 保留当前欢迎页、暖色奇幻图片及未提交修改，不以 git add .、reset、checkout 清理工作区。
- 不推送、不部署，不发送真实邮件，不用生成界面图代替运行验收。
- 行为逻辑按已确认的 tdd/direct 模式执行；tdd 时先证明有意义的回归失败，再实现。纯文案、素材和文档不编写镜像测试。

## 文件职责与执行顺序

`backend/internal/app/appearance.go` 和 `appearance_canvas.go` 负责站点与 Agent 外观规范化；`backend/internal/auth/host.go`、`email.go` 和现有注册逻辑负责默认通信身份。前端在 `use-appearance-store.ts`、`brand-display.ts`、`agent-appearance.ts` 统一默认和读取，调用方只消费结果。

`backend/builtin/`、`backend/internal/prompts/` 和 `plugin-packages/` 是官方内容源，实际 `.yingce-plugin` 同步更新。已有 `EnsureBuiltinSkills`、`UpsertBuiltinSkills` 和 `bootstrapBuiltInPlugins` 保持稳定 ID 和现有更新职责。

新增 `scripts/audit-product-brand.py` 只负责审计，含 `--self-test` 最小检查；仅在确认没有可复用打包入口时新增 `scripts/repack-branded-plugins.py`，只负责保留现有归档结构的机械打包。两者不接入运行时、不引入依赖。

下列任务与 OpenSpec 的 14 个复选框逐一对应，执行顺序允许为减少共享入口往返而先完成 2.2，再收敛 2.1。每项通过自身验收后，才勾选对应 tasks.md；提交只包含该项所属补丁，避免夹带前序 UI 修改。

### Task 1：审计与保护基线（1.1）

**Files:** 创建 `scripts/audit-product-brand.py`；更新 change 下 `brand-inventory.md`。现有基线快照为 `.local/cache/brand-workspace-before.json`，包含 12 个先前文件的 SHA-256。

**Interfaces:** 审计命令 `python scripts/audit-product-brand.py --root . --json` 输出发现数组；内部 `audit_root(root: pathlib.Path) -> list[dict]` 返回同一结构。每项包含 path、entry（归档时）、line、text、category、reason；category 为 product、exception 或 non_brand。未经批准的 product 发现使进程非零退出；`--self-test` 不扫描或修改真实项目。

- [x] 先用临时目录构造普通源文件、名称含旧品牌的文件、ZIP 文本条目及非品牌“摄影策略”，检查前三类会报告、最后一类单独分类；添加精确例外条目的最小自检。测试内使用标准库 `TemporaryDirectory`、`ZipFile` 和 `assert`。

```python
with tempfile.TemporaryDirectory() as root:
    source = pathlib.Path(root) / 'copy.txt'
    source.write_text('影策', encoding='utf-8')
    with zipfile.ZipFile(pathlib.Path(root) / 'sample.yingce-plugin', 'w') as package:
        package.writestr('README.md', '影策团队')
    (pathlib.Path(root) / '影策.txt').write_text('', encoding='utf-8')
    (pathlib.Path(root) / 'ordinary.txt').write_text('摄影策略', encoding='utf-8')
    findings = audit_root(pathlib.Path(root))
    assert any(item['path'] == 'copy.txt' and item['category'] == 'product' for item in findings)
    assert any(item['entry'] == 'README.md' and item['category'] == 'product' for item in findings)
    assert any(item['path'] == '影策.txt' and item['category'] == 'product' for item in findings)
    assert not any(item['path'] == 'ordinary.txt' and item['category'] == 'product' for item in findings)
```

- [x] 用 `git ls-files --cached --others --exclude-standard` 建立输入集；排除 Git/依赖/构建/本地缓存和明确的用户参考输入，不给产品目录整目录豁免。按 UTF-8 读取文本，ZIP 按条目读取；记录所有 105 包及非文本条目，不宣称二进制文本扫描覆盖了图片像素。
- [x] 给旧值回归、迁移识别和历史证据添加精确到文件与命中内容的例外；同文件的其他命中仍报告。保存基线、未检查素材和真实引用。
- [x] 运行以下命令。self-test 应通过；实施前审计应报告旧品牌而非假通过。提交扫描与基线说明，不提交本地快照。

```powershell
python scripts/audit-product-brand.py --self-test
python scripts/audit-product-brand.py --root . --json
```

### Task 2：运行实例配置盘点（1.2）

**Files:** 更新 `brand-inventory.md`；必要完整备份只保存到 Git 忽略、权限受保护的 `.local/`，不把敏感值写入报告。

**Interfaces:** 消费现有公开 `{data:{appearance}}`、管理 `{setting}` 接口。保持 `/public/appearance`、`/admin/settings/appearance` 与既有更新 payload 不变。

- [x] 只读检查运行实例；3000 端口公开 API 已返回旧品牌，8080 直连不可用不意味着需要重启。仅输出品牌、SEO、版权、Agent 字段。

```powershell
$response = Invoke-RestMethod 'http://127.0.0.1:3000/api/public/appearance'
$response.data.appearance | Select-Object brandName,brandSlug,seoTitle,seoDescription,footerCopyright
```

- [x] 用现有受权会话只读查看外观、Agent、邮件和注册设置；无权限则记录具体未覆盖项，不创建账号、不提取 Cookie 或假定已通过。记录自定义字段和配置归属。
- [x] 若需要显式保存旧全局设置，先完整备份；使用现有受保护 PATCH 并保留所有字段，不执行 DELETE 恢复默认。保存失败保留原值及错误，不手工改库。
- [x] 对照工作区基线和 Git diff，确认先前 UI、上传图及工具目录仍在；提交脱敏盘点说明。

### Task 3：站点默认与前端读取（2.1）

**Files:** 修改 `web/src/stores/use-appearance-store.ts`、`web/src/lib/brand-display.ts`、`backend/internal/app/appearance.go`、`backend/internal/auth/host.go`；更新 `web/test/brand-display.test.ts`、`appearance-bootstrap.test.ts`、`appearance-refresh.test.ts`。

**Interfaces:** 保留 `normalizePublicAppearance(value?: Partial<PublicAppearance> | null): PublicAppearance`、`displayBrandName(value: string): string`、`displayBrandTitle(title: string, brandName: string): string` 和现有 store 消费合同；不新增页面品牌 helper。

- [x] 补充默认身份和失败降级的断言；保留自定义品牌和自定义 SEO 测试，测试先前显示映射可以收敛但不改变调用合同。

```typescript
expect(DEFAULT_PUBLIC_APPEARANCE.brandName).toBe('源铭振跃');
expect(normalizePublicAppearance({}).seoTitle).toBe('源铭振跃');
expect(normalizePublicAppearance({ brandName: 'HIMA Studio', seoTitle: '定制标题' }).seoTitle).toBe('定制标题');
expect(displayBrandName('HIMA Studio')).toBe('HIMA Studio');
```

- [x] 在原入口更新默认名、默认 SEO/版权和 `brandStudioLabel` 的可见英文副标；保持 `brandSlug: open-ai-canvas`。处理旧值的逻辑与 Task 4 保持同一字段规则，调用方不再叠加新的映射。
- [x] 运行专项，初始、成功加载、失败刷新、元数据和自定义配置均应通过；提交默认与读取补丁。

```powershell
Set-Location web
bun test test/brand-display.test.ts test/appearance-bootstrap.test.ts test/appearance-refresh.test.ts
```

### Task 4：存量外观、Agent、邮件和协议（2.2）

**Files:** 修改 `backend/internal/app/appearance.go`、`appearance_canvas.go`、`backend/internal/auth/email.go`；更新 `appearance_test.go`、`backend/internal/auth/password_reset_test.go`、`registration_agreement_test.go`；前端 Agent 默认在 `web/src/lib/canvas/agent-appearance.ts`。

**Interfaces:** 复用 `readAppearance`、`UpdateAppearance`、`normalizeCanvasAppearance`、`resolveEmailSender`。邮件公开继承标记与实际发送用同一个判定，必要时在 email.go 中定义 `isInheritedEmailSender(name string) bool`，只消除这两个调用点的真实重复。

- [x] 在现有外观 fixture 中加入存量配置测试；用 `db.Create(&model.SystemSetting{Key: appearanceSettingKey, ValueJSON: ...})` 写隔离测试库，验证公开/管理结果均为新名，数据库原 JSON 在 GET 后不变，再经合法保存重读。

```go
func TestAppearanceMigratesLegacyBrand(t *testing.T) {
    svc, db, _, _ := newAppearanceTestService(t)
    raw := `{"brandName":"影策","seoTitle":"影策","seoDescription":"影策，面向 AI 影视与短剧创作的工作台。","footerCopyright":"© 2026 影策. All rights reserved."}`
    if err := db.Create(&model.SystemSetting{Key: appearanceSettingKey, ValueJSON: raw}).Error; err != nil { t.Fatal(err) }
    got, err := svc.Appearance()
    if err != nil { t.Fatal(err) }
    if got.BrandName != "源铭振跃" || got.SEOTitle != "源铭振跃" { t.Fatalf("unexpected identity: %q / %q", got.BrandName, got.SEOTitle) }
    var stored model.SystemSetting
    if err := db.First(&stored, "key = ?", appearanceSettingKey).Error; err != nil { t.Fatal(err) }
    if stored.ValueJSON != raw { t.Fatal("GET mutated stored appearance") }
}
```

- [x] 扩展同一 fixture 覆盖其他品牌/Agent、旧名派生默认值、无关完整字段、连续规范化、保存和服务重新构造读取。默认模板只按明确字段精确识别；不替换自由正文，不使用全库 replace。
- [x] 在 email.go 增加共享继承判定，并给空名、旧默认名、新默认名和自定义名分别断言；旧默认发件名在自定义站点上仍继承站点名。默认协议沿 `defaultAgreementTitle` 使用站点身份，自定义条款保持。

```go
func isInheritedEmailSender(name string) bool {
    return name == "" || name == DefaultBrandName || name == "影策"
}
```

```go
func TestSenderNameLegacyInheritance(t *testing.T) {
    for _, name := range []string{"", "影策", "源铭振跃"} {
        got := resolveEmailSender(EmailSettingValue{FromName: name}, "星河工作室")
        if got.FromName != "星河工作室" { t.Fatalf("sender %q did not inherit", name) }
    }
    if got := resolveEmailSender(EmailSettingValue{FromName: "业务通知"}, "星河工作室"); got.FromName != "业务通知" { t.Fatal("custom sender changed") }
}
```

- [x] 用隔离发送器复核注册/找回密码邮件，不发送真实邮件；运行以下命令。提交共享逻辑与回归。

```powershell
Set-Location backend
go test ./internal/app -run 'TestAppearance|TestUpdateAppearance|TestResetAppearance' -count=1
go test ./internal/auth -run 'TestSenderName|TestRegistrationAgreement' -count=1
```

### Task 5：真实功能入口的品牌文案（2.3）

**Files:** 更新 `web/src/lib/creation/creative-agent-tools.ts`、`web/src/components/canvas/previs/canvas-previs-workbench.tsx`、`web/src/lib/timeline/ai-command-schema.ts`、`web/src/lib/plugins/builtin/media-conversion.ts`、`web/src/lib/plugins/builtin/editor/editor-shell.tsx`、`web/src/lib/plugins/builtin/editor/editor-subtitle-tools.tsx`、`web/src/lib/sidebar-checkin.ts`、`web/src/components/canvas/canvas-agent-skill-library-modal.tsx` 和 `web/src/pages/admin/settings/appearance-settings-page.tsx`。

- [x] 用下列命令确认实际路径与上下文；普通“摄影策略”等词不改，自有提示词/示例品牌改名，用户内容不动。

```powershell
rg -n '影策|YINGCE STUDIO' web/src --max-columns 200 --max-columns-preview
rg -n 'displayBrandName|displayBrandTitle|SOURCE_UI_BRAND' web/src
```

- [x] 在真实组件和自有文案源头替换，保持工具名、参数、命令和 CSS 选择器；复用 Task 1 扫描验收，不为每条文案写源码字符串镜像测试。
- [x] 定点检查预演、助手、技能库与恢复默认提示的真实组件源码并运行前端类型检查，记录为静态证据后提交；真实路由、交互及提示验收统一在 Task13 执行，不以源码阅读代替，不执行真实重置。

```powershell
Set-Location web
bun run typecheck
```

### Task 6：API 与外观专题同步（2.4）

**Files:** 修改 `backend/internal/handler/openapi.yaml` 的展示标题、`docs/content/docs/backend/canvas-agent-appearance.mdx` 以及审计定位到的自有外观/认证说明；更新 `web/test/canvas-appearance.test.ts` 和已有默认品牌预期。

- [ ] 修改标题和品牌说明，不更改 OpenAPI paths、schema、操作 ID；文档明确存量默认值识别与自定义配置保留边界。
- [ ] 运行 Task 4 的后端专项及前端 canvas/appearance 专项，确认旧值只作为回归输入保留；提交说明及对应断言。

```powershell
Set-Location web
bun test test/canvas-appearance.test.ts test/appearance-bootstrap.test.ts test/auth-registration-agreement.test.ts
```

### Task 7：官方内容源与中文技能目录（3.1）

**Files:** 修改 `backend/builtin/影策短剧制作工作流/` 并改名为 `源铭振跃短剧制作工作流/`、`backend/internal/prompts/agent-media-policy.md`、`backend/internal/skills/skills_builtin.go` 中自有作者回退、`plugin-packages/` 自有元数据与文档。

- [ ] 固定所有中文目录引用及嵌入规则，检查 `skills_yingshi_builtin_test.go`、`skills_builtin_showcase_test.go`；只移动已解析且位于 backend/builtin 内的这个技能目录。
- [ ] 改标题、作者、自述和品牌说明；保留 `yingce-system`、`yingce-short-drama-workflow`、协议与供应商参数，保留 OpenAI 等真实原作者。manifest 的内嵌 documentation 与旁边 README 同步更新，不打印整个巨大 JSON。
- [ ] 在既有加载测试中检查稳定 skillId 不变且自有展示新名；运行技能包测试并提交内容源补丁。

```powershell
Set-Location backend
go test ./internal/skills -count=1
```

### Task 8：已安装官方内容更新与用户保护（3.2）

**Files:** 复用 `backend/internal/skills/skills_builtin.go`、`backend/internal/repository/skills.go` 和 `backend/internal/app/protocol_plugins.go`；在相应已有测试中扩展更新场景，若没有合适插件 fixture 则创建 `backend/internal/app/protocol_plugins_brand_test.go`。

- [ ] 测试两次加载同一官方技能稳定 ID：旧内容先安装，更新后名字/作者改变但用户加入、收藏与固定版本关系不被重置。
- [ ] 用临时插件目录及缓存注册表先加载旧官方元数据并禁用，再加载新包；断言同 ID、作者更新、禁用状态保留。另放独立自定义 ID，断言其 raw manifest 不变。复用既有运行时构造，测试不污染实际 registry。
- [ ] 现有更新流程满足断言则不改运行时；若发现遗漏，只在共享启动入口修复品牌元数据更新，不增加全量迁移或覆盖用户自由编辑。运行专项并提交测试与必要最小补丁。

```powershell
Set-Location backend
go test ./internal/skills ./internal/repository -run 'Builtin|Skill' -count=1
go test ./internal/app -run 'Plugin.*Brand|Brand.*Plugin' -count=1
```

### Task 9：实际插件包重建和解包检查（3.3）

**Files:** 更新受影响的 `plugin-packages/*.yingce-plugin`；无可复用打包入口时创建 `scripts/repack-branded-plugins.py`。新增打包脚本只接受项目内 package root，默认检查差异，显式 `--write` 才重建。

- [ ] 列出全部 105 包和对应源目录，识别 94 个旧品牌包及其他因内嵌文档同步受影响的包；不能因为包名无中文就跳过。
- [ ] 标准库 zip 重建保持原条目路径、可执行权限、未改文件原始字节和现有 manifest 格式，仅用已更新源文件替换同路径条目；内嵌二进制不得丢失。使用临时文件构建并完整校验后以 `os.replace` 原子替换。已有 SHA-256 缓存随实际新包更新，不手填假摘要。

```python
# 对已经核对过根目录和源映射的每个原归档执行；replacement 只含允许更新的条目。
with zipfile.ZipFile(original) as old, zipfile.ZipFile(temporary, 'w') as new:
    for info in old.infolist():
        new.writestr(info, replacement.get(info.filename, old.read(info.filename)))
with zipfile.ZipFile(temporary) as check:
    assert check.testzip() is None
os.replace(temporary, original)
```

- [ ] 解包对照更新源清单、内嵌说明、稳定 ID、协议和参数，审计全部包。复用宿主 Parse/Inspect 和官方协议测试；支付包内多平台二进制保持原字节。提交实际归档及打包工具，不提交缓存 registry。

```powershell
python scripts/audit-product-brand.py --root . --json
Set-Location backend
go test ./internal/protocol -count=1
```

### Task 10：产品截图、SVG 与视频（4.1）

**Files:** 重截 `assets/login.png`、`assets/create.png`、`assets/tldraw-license-key-settings.png`；处理原 `web/public/welcome/workbench-create.webp`，保持现有 welcome 已引用的新截图有效；更新素材核查表及实际引用。

- [ ] 按 `brand-inventory.md` 逐项检查产品素材。neutral `logo.svg` 不重画；backend/huabu 的“无限画布”不是目标旧名，不顺带改名。保留项目创作画面、第三方头像、Logo 和水印。
- [ ] 在更新后的真实应用，用干净会话重截三张已确认有旧品牌的页面；设置页不展示真实密钥。原 WebP 若仍被实际引用则用真实新截图替换；无引用且重复时可在检查引用后移除，不能借此删除用户上传参考图。
- [ ] 实际检查默认视频/海报和本地品牌相关视频的可访问内容与代表性帧，记录时段与范围；无法访问或不能完整确认的项标为未验证，不能视为通过。媒体编辑如需生图，使用 imagegen 并先读技能；产品截图始终直接来自真实路由。
- [ ] 复查欢迎页与宽度、滚动、焦点；提交仅本次替换的产品素材和引用，保留 fantasy-dawn.png、paper-grain.svg 的已批准设计。

### Task 11：文档、脚本及可见文件名（4.2）

**Files:** 按扫描精确更新 `README.md`、`CONTEXT.md`、`AGENTS.md`、`CHANGELOG.md` 当前产品展示、维护文档/注释和 `scripts/` 输出。历史研究稿与用户参考按明确例外记录，不将其旧限制当作本次授权边界。

- [ ] 对每个命中先判定品牌、普通词、机器标识或真实出处；产品当前可见品牌改名，来源链接、许可证和真实作者保留。
- [ ] 改名文件时同步每个真实引用，使用 `rg -n --fixed-strings` 检查旧路径；不改变脚本行为或依赖锁文件。
- [ ] 运行扫描与聚焦 diff 检查，逐项核对例外无产品输出；提交文档及脚本显示补丁。

```powershell
python scripts/audit-product-brand.py --root . --json
git diff --check
```

### Task 12：功能、待测与边界说明（4.3）

**Files:** 更新 `docs/content/docs/overview/features.mdx`、`docs/content/docs/progress/pending-test.mdx`、受影响专题及 `brand-inventory.md`。

- [ ] 明确真实默认身份、存量规范化、官方内容刷新和技术标识保留边界；不声称部署或用户确认已完成。
- [ ] 保留此前 pending-test 中的 UI 工作记录；旧 screenshot 未完成项在实际重截后更新，不单凭新源码删除待测项。
- [ ] 对照代码、包内结果和视觉记录逐条审阅，更新最终例外和未验证项，提交对应说明。

### Task 13：综合验证（5.1）

**Files:** change 下新增 `verification-report.md`；只记录实际执行的命令和浏览器证据，不提交构建产物和账号数据。

- [ ] 运行前端专项、类型检查/lint/build 和相关 Go 包；以下命令从标明目录分别执行，不用 npm/pnpm 重装现有 Bun 依赖。

```powershell
# 工作目录 web
bun test test/brand-display.test.ts test/appearance-bootstrap.test.ts test/appearance-refresh.test.ts test/canvas-appearance.test.ts test/auth-registration-agreement.test.ts test/welcome-story.test.ts
node --test test/welcome-title.test.mjs
bun run typecheck
bun run lint
bun run build
# 工作目录 backend
go test ./internal/app ./internal/auth ./internal/skills ./internal/repository ./internal/protocol ./internal/handler -count=1
```

- [ ] 通过允许的浏览器工具操作真实页面，检查登录/注册/找回密码、创作/侧栏、后台外观、预演、助手与欢迎页，覆盖明暗主题和 320/375/414/768/1440 宽度；不发送邮件、不生成付费任务、不执行恢复默认。
- [ ] 记录实际 URL、主题、宽度、截图和关键交互结果；权限或环境导致无法访问的路由单独列出。源码阅读不得标作浏览器通过。
- [ ] 按所选 review_mode 完成 Comet 要求的代码审查并处理关键问题；新失败先按 systematic-debugging 定位，三次同类失败停止盲试。记录证据，提交验证报告。

### Task 14：残留终审与交付（5.2）

**Files:** 更新审计、验证报告及 tasks.md；保留所有必要迁移输入与回归例外的具体位置和用途。

- [ ] 重跑扫描和包内检查，人工复核全部例外和素材表。不是零命中就算通过：需解释每个旧名残留，未检查项继续标为未完成。
- [ ] 比较稳定 ID、接口/存储/环境变量、用户配置和前序 UI 差异；核对旧数据仍能读取。无本次理由的差异撤回本次补丁，不能覆盖用户文件。
- [ ] 如实列出修改、命令结果、截图、未覆盖边界和回滚方法。回滚仅撤本次提交及有备份的设置写入，不删除库、资源、参考图或之前 UI 成果。
- [ ] 所有 14 项任务满足验收后才勾选；登记真实 build 证据并运行 Comet build guard，随后按工具路由进入 verify，不能因计划写完就宣称替换完成。

## 实施配置确认点

计划已具备实施输入，尚未选择 workspace、build_mode 或 tdd_mode。按 comet-build 第 2 步统一收集：继续/暂停、当前分支/新分支/worktree、子代理/本会话执行、tdd/direct、off/standard/thorough 审查。当前分支为 `codex/welcome-ui-hallmark`，可用新分支候选为 `codex/rebrand-yuanming-zhenyue`；worktree 不自动携带未提交 UI，必须先妥善准备已批准输入。用户完整选择后才写入配置并开始源码实现。

## 计划自审

14 项任务逐项覆盖 OpenSpec 1.1–5.2；配置/邮件共享入口、包内内容、视觉核查、技术合同和用户数据保护均有验收路径。新增函数名仅有 email.go 内两个真实调用点共用的继承判定，扫描 CLI 与机械打包职责已定义；没有新品牌框架、依赖或需求扩展。纯文案不增加镜像测试；行为变更保留有意义的 RED/GREEN 检查。无法实际访问的路径不得在交付中写为通过。
