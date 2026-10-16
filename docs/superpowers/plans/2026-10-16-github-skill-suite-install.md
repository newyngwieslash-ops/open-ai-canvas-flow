---
change: github-skill-suite-install
design-doc: docs/superpowers/specs/2026-10-16-github-skill-suite-install-design.md
base-ref: 9ce50f27ce93b22955edce69033a19d5674d7b8e
archived-with: 2026-10-16-github-skill-suite-install
---

<!-- comet-task-authority: docs/openspec/changes/github-skill-suite-install/tasks.md -->

# GitHub 技能套装安装实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 每个 Task 标注了对应 `docs/openspec/changes/github-skill-suite-install/tasks.md` 的复选项，验收通过后同步勾选。

**Goal:** GitHub 技能安装填子目录 X 时，自动把"仓库根一级、含 SKILL.md、被入口（或已纳入技能）SKILL.md 正文引用"的兄弟技能目录按传递闭包并入最终技能包，套装型仓库（1 总控 + N 子技能）安装后总控的相对引用可达。需求与验收场景以 delta spec（`docs/openspec/changes/github-skill-suite-install/specs/skill-package-suite-install/spec.md`）为准。

**Architecture:** 改动全部收敛在 `backend/internal/skills/skill_packages_archive.go`：新增 `suiteSiblingDirs` 在内存 `raw` map 上做 token 化引用扫描与队列式闭包；`normalizeSkillArchiveRoot` 的 subdir 分支在 X 压平之后、根识别之前按原相对路径并入兄弟目录文件。入口契约（根级 `SKILL.md`）、根识别、ZIP / Markdown 路径、20MB / 8MB / 512 文件上限全部零改动（技术依据见设计文档"实证结论"表 #1–#6 与"技术方案"节）。

**Tech Stack:** Go 1.25 标准库（`strings` / `unicode` / `sort`，无新依赖）、既有 `kernel.BadAuthRequest` 错误路径、`go test`、一次性端到端验证用真实 GitHub 归档。

## Global Constraints

以下逐条来自 Design Doc 与 delta spec，每个 Task 隐含遵守：

- token 分隔符表**不含 `/`**：新建 `suiteRefTokenSeparator`（`unicode.IsSpace` + `,，、。;；:：\|()（）[]【】{}<>"'“”‘’!！?？+*&\``）；不复用 `cloudAgentSkillTokenSeparator`——其表含 `/`，会破坏 `dir/` 前缀形态（实证 #1）。
- 匹配双形态：`token == dir || strings.HasPrefix(token, dir+"/")`；精确大小写，不做折叠（GitHub 目录名大小写敏感，实证仓库全小写；YAGNI）。
- 闭包必须 visited 防环（实证 #4：仓库存在自引用 `sd-xuanhuan-engine/SKILL.md`）。
- 根识别循环（`normalizeSkillArchiveRoot` 中 `skillRoots` 计算，`skill_packages_archive.go:213-229`）**零改动**（实证 #5：兄弟 SKILL.md 在任何 map 迭代序下收敛到单根 `''`）。
- ZIP / Markdown 路径（`subdir == ""`）不进入改造分支，行为不变；不填 subdir 时仓库含多个 SKILL.md 仍被拒绝。
- 并入兄弟目录文件时**不覆盖已有键**（`if _, ok := filtered[p]; !ok`，设计文档"防御边界"）；入口目录自身（`dir == normalized`）不并入，保证 spec"入口提升与唯一性"场景（`director/SKILL.md` 不在包内重复保留）。
- 产品上限对并入后的最终集合生效：20MB 总量（`finalizeSkillArchive`，`skill_packages_archive.go:277`）、单文件 8MB（解压循环对全部条目生效，`:138-155`）、512 文件（`:241-243`）。预期零改动，仅验证。
- 不改 `EntryPath` 契约、`validateSkillPackageSnapshot`、bundle、Agent 卡片路径；不改前端、数据库、存量已安装技能数据。
- 不做 Markdown 链接语法解析、不区分引用语境；不做"多技能列表选择"安装体验。
- 无 API 路径、JSON 结构、数据表、SSE、部署变化，不涉及 `http-api.mdx` / `backend-database.mdx`；仅按 tasks.md 3.2 更新 `docs/content/docs/progress/pending-test.mdx`。
- 工作区当前有他人未提交改动（`web/`、`docs/content/docs/progress/pending-test.mdx` 等，基线 HEAD `9ce50f27`，分支 `codex/welcome-ui-hallmark`）：不覆盖、不清理、不 `git add -A` / `git reset --hard` / `git checkout --`；每次提交只 add 本次涉及文件。
- 验证命令按范围最小充分：`cd backend && go test ./internal/skills/`、`cd backend && go build ./...`；文档站 `cd docs && bun run types:check`。

## 文件职责与现状（File Structure）

| 文件 | 职责与本次改动 |
| --- | --- |
| `backend/internal/skills/skill_packages_archive.go` | 归档归一化。`archiveFromZip`（:111）解压到 `raw` map 后调 `normalizeSkillArchiveRoot`（:172，wrapper 剥离 :177-194 → subdir 过滤 :196-212【本次改造点】→ 根识别 :213-229【不动】→ 根内容提升 :229-240）；`finalizeSkillArchive`（:261）对最终文件集合做上限校验【不动】。新增 `suiteRefTokenSeparator`、`suiteSiblingDirs` 两个包内函数。 |
| `backend/internal/skills/skill_packages_test.go` | 既有测试与 `skillZip(t, files map[string]string) []byte` helper（:294）。新增套装用例同文件。 |
| `backend/internal/skills/skill_packages_github.go` | GitHub 安装唯一调用点 `archiveFromZip(data, spec.Subdir)`（:86），零改动。 |
| `backend/internal/skills/skill_packages.go` | ZIP 上传路径 `archiveFromZip(data, "")`（:204）与上限常量（:28-33），零改动。 |
| `docs/content/docs/progress/pending-test.mdx` | Task 5 追加待测小节。注意：该文件当前已有他人未提交修改，只追加、不改写。 |
| `backend/internal/skills/suite_e2e_temp_test.go` | Task 4 一次性端到端临时测试，验证后删除，不入库。 |
| `.local/cache/suite-e2e/` | Task 4 真实归档与对照输出存放处（Git 忽略目录）。 |

## Task 与 tasks.md 对照

| 计划任务 | tasks.md 复选项 |
| --- | --- |
| Task 1 | 1.1（闭包扫描函数）、2.3 的词边界部分 |
| Task 2 | 1.2（subdir 分支接入）、2.1、2.2、2.3 的排除部分 |
| Task 3 | 1.3（上限确认）、2.4（回归） |
| Task 4 | 2.5（真实仓库端到端） |
| Task 5 | 3.2（pending-test.mdx） |
| Task 6 | 3.1（全量验证）、3.3（提交推送） |

---

### Task 1：套装兄弟目录闭包扫描 `suiteSiblingDirs`（tasks.md 1.1 + 2.3 词边界）


<!-- comet-task-ref:1-1 --> <!-- comet-task-ref:2-3 -->

**Files:**
- Modify: `backend/internal/skills/skill_packages_archive.go`（文件末尾新增两个包内函数）
- Test: `backend/internal/skills/skill_packages_test.go`（新增 `TestSuiteSiblingDirs`）

**Interfaces:**
- Produces:
  - `suiteRefTokenSeparator(r rune) bool`：引用 token 分隔判定，`unicode.IsSpace(r) || strings.ContainsRune(",，、。;；:：\\|()（）[]【】{}<>\"'“”‘’!！?？+*&\`", r)`——**表中不含 `/`**。
  - `suiteSiblingDirs(raw map[string][]byte, entryText string) []string`：从剥壳后 raw 视图收集被 entryText 按引用关系传递命中的"根一级含 `dir/SKILL.md`"目录名，返回**字典序排序**切片（并入顺序确定，内容哈希稳定）。注意：本函数不知道入口目录是谁，入口目录自身的排除由 Task 2 的调用点负责。


在 `skill_packages_test.go` 新增 `TestSuiteSiblingDirs`，表驱动覆盖以下用例（输入 raw / entryText / 期望返回）：

| 用例 | raw（map[string]string） | entryText | 期望返回 |
| --- | --- | --- | --- |
| 裸 token + `dir/` 前缀双形态 | `{"a/SKILL.md":"x","b/SKILL.md":"y","b/lib.js":"z"}` | `` "使用 `b/SKILL.md`，另见 b/lib.js。" `` | `["b"]`（反引号在分隔符表内被剥掉；`b/lib.js` 整 token 以 `dir/` 前缀命中） |
| 词边界 | `{"sd-script-engine/SKILL.md":"x"}` | `"不要混淆 sd-script-engine-x/SKILL.md。"` | `[]`（token `sd-script-engine-x/SKILL.md` 不等于 `sd-script-engine` 且不以 `sd-script-engine/` 开头，实证 #3） |
| 防环/互引 | `{"a/SKILL.md":"引 b/SKILL.md","b/SKILL.md":"回引 a/SKILL.md"}` | `"b/SKILL.md"` | `["a","b"]` 且有限时间内返回（visited 防环） |
| 自引用 | `{"sd-x/SKILL.md":"参见 sd-x/SKILL.md 自身。"}` | `"sd-x/SKILL.md"` | `["sd-x"]`，有限时间内返回 |
| 排序输出 | `{"c/SKILL.md":"x","a/SKILL.md":"y"}` | `"c/SKILL.md a/SKILL.md"` | `["a","c"]`（字典序） |
| 空正文 | `{"a/SKILL.md":"x"}` | `""` | `[]`（设计文档"防御边界"：入口无 SKILL.md 时闭包为空集） |
| 无 SKILL.md 目录不入索引 | `{"a/SKILL.md":"见 examples/readme.md","examples/readme.md":"素材"}` | `"见 examples/readme.md"` | `[]`（`examples/` 不含根一级 `SKILL.md`，不进 skillDirs 索引） |


Run: `cd backend && go test ./internal/skills/ -run TestSuiteSiblingDirs -v`
Expected: FAIL，编译错误 `undefined: suiteSiblingDirs`


在 `skill_packages_archive.go` 末尾新增，技术要点与 Design Doc"新增函数：suiteSiblingDirs"节逐条一致：

1. `suiteRefTokenSeparator`：按 **Interfaces** 中的精确分隔符表实现（含全角标点与反引号，**不含 `/`**）。
2. `suiteSiblingDirs`：
   - skillDirs 索引：一次遍历 `raw`，对每个路径取第一个 `/` 之前的段为目录名，仅当 remainder 恰为 `SKILL.md`（即根一级 `dir/SKILL.md`）时纳入索引；子层级 `a/b/SKILL.md` 不入索引（闭包只发生在仓库根一级目录之间，Design Doc Non-Goals）。
   - `visited` 集合 + `pending` 正文队列：队列初始仅含 `entryText`；出队正文用 `strings.FieldsFunc(text, suiteRefTokenSeparator)` 切 token；对每个 token 检查双形态命中（**Interfaces** 中的表达式）；命中且未 visited → 标记 visited、记入结果、把 `raw[dir+"/SKILL.md"]` 正文入队；队列空即固定点。
   - 返回前 `sort.Strings` 排序。
3. 不新增错误路径、不修改任何现有函数。


Run: `cd backend && go test ./internal/skills/ -run TestSuiteSiblingDirs -v`
Expected: PASS，全部子用例通过


Run: `cd backend && go test ./internal/skills/`
Expected: PASS（含既有 `TestArchiveFromZip*` 全部通过）


```bash
git add backend/internal/skills/skill_packages_archive.go backend/internal/skills/skill_packages_test.go
git commit -m "feat(skills): 技能包安装 - 新增套装兄弟目录闭包扫描 suiteSiblingDirs"
```

---

### Task 2：`normalizeSkillArchiveRoot` subdir 分支接入闭包（tasks.md 1.2 + 2.1 + 2.2 + 2.3）


<!-- comet-task-ref:1-2 --> <!-- comet-task-ref:2-1 --> <!-- comet-task-ref:2-2 --> <!-- comet-task-ref:2-3 -->

**Files:**
- Modify: `backend/internal/skills/skill_packages_archive.go:196-212`（subdir 分支，在 `if len(filtered) == 0` 报错之后、`raw = filtered` 赋值之前插入）
- Test: `backend/internal/skills/skill_packages_test.go`（新增 `TestArchiveFromZipSuiteInstall`）

**Interfaces:**
- Consumes: Task 1 的 `suiteSiblingDirs(raw map[string][]byte, entryText string) []string`。
- Produces: `normalizeSkillArchiveRoot` subdir 分支的新语义——`filtered`（X 压平结果）构建后，以 `string(filtered["SKILL.md"])` 为入口正文调 `suiteSiblingDirs(raw, entryText)`（注意传**剥壳后的 raw 全仓视图**，不是 filtered），对返回的每个 `dir`：跳过 `dir == normalized`（入口目录自身不并入，spec"入口提升与唯一性"），遍历 `raw` 中 `dir+"/"` 前缀路径按**原相对路径**并入 `filtered`，已有键不覆盖。函数签名、报错路径（子目录不存在或为空）、根识别、提升逻辑均不变。


新增 `TestArchiveFromZipSuiteInstall`，表驱动覆盖以下用例（对应用例均满足 metadata 可解析：入口 SKILL.md 含 `name` / `description` frontmatter 或可推断标题与描述）：

| 用例 | ZIP 文件（map[string]string） | subdir | 期望（`archive.Files` 键集及关键断言） |
| --- | --- | --- | --- |
| 套装闭包（spec 场景"套装仓库总控安装引用可达"） | `director/SKILL.md`（正文含 `` `engine-a/SKILL.md` ``）、`director/refs/camera.md`、`engine-a/SKILL.md`、`engine-a/scripts/check.js` | `director` | 键集恰为 `{SKILL.md, refs/camera.md, engine-a/SKILL.md, engine-a/scripts/check.js}`；`Files["SKILL.md"]` 内容等于 director 原文（入口提升）；**无任何 `director/` 前缀键**（压平 + 入口唯一性） |
| 传递闭包（spec 场景"传递闭包"） | `director/SKILL.md`（引 `engine-a/SKILL.md`）、`engine-a/SKILL.md`（引 `` `engine-b/SKILL.md` ``）、`engine-a/e.md`、`engine-b/SKILL.md`、`engine-b/f.md` | `director` | 键集恰为 2（入口压平）+ 2（engine-a 原路径）+ 2（engine-b 原路径）共 6 个；engine-a、engine-b 内容全部在包 |
| 排除：无 SKILL.md 的兄弟目录 | `director/SKILL.md`（正文提及 `examples/a.md`）、`director/x.md`、`examples/a.md`（**无** `examples/SKILL.md`）、`unused-skill/SKILL.md`（正文从未提及） | `director` | 键集恰为 `{SKILL.md, x.md}`；无 `examples/` 前缀键（spec 场景"无 SKILL.md 的兄弟目录不纳入"，实证 #6 storyboard 双条件）；无 `unused-skill/` 前缀键（spec 场景"未被引用的兄弟技能目录不纳入"） |
| 入口自引用不并入（spec 场景"入口提升与唯一性"） | `director/SKILL.md`（正文含 `director/references/style.md` 与 `` `engine-a/SKILL.md` ``）、`director/references/style.md`、`engine-a/SKILL.md` | `director` | 键集恰为 `{SKILL.md, references/style.md, engine-a/SKILL.md}`；无任何 `director/` 前缀键（入口目录自身在闭包命中但不并入） |
| 同名键不覆盖（设计文档"防御边界"） | `pkg/SKILL.md`（引 `sub/SKILL.md`）、`pkg/sub/x.md`（内容 `X内容`）、`sub/SKILL.md`（内容 `# Sub`）、`sub/x.md`（内容 `Sub内容`） | `pkg` | `Files["sub/x.md"] == "X内容"`（X 压平产物优先，后并入者不覆盖已有键）；`Files["sub/SKILL.md"] == "# Sub"` |
| 防环（archiveFromZip 级） | `a/SKILL.md`（引 `b/SKILL.md` 与 `a/SKILL.md` 自身）、`b/SKILL.md`（回引 `a/SKILL.md`） | `a` | 有限时间内返回；键集恰为 `{SKILL.md, b/SKILL.md}`；无 `a/` 前缀键 |


Run: `cd backend && go test ./internal/skills/ -run TestArchiveFromZipSuiteInstall -v`
Expected: FAIL——当前 subdir 分支硬裁剪，兄弟目录期望键缺失（如 `engine-a/SKILL.md` 不在包内）


在 `skill_packages_archive.go` 的 subdir 分支内、`if len(filtered) == 0 { return ... }`（:208-210）之后、`raw = filtered`（:211）之前插入闭包并入，要点：

1. `entryText := string(filtered["SKILL.md"])`（X 压平后的入口正文；X 无 SKILL.md 时为空串 → 闭包返回空集 → 后续根识别报"缺少 SKILL.md"，与现状一致，不新增处理）。
2. `suite := suiteSiblingDirs(raw, entryText)`——第二个参数传剥壳后的 `raw`（兄弟目录在 raw 中，不在 filtered 中），与设计文档数据流图一致。
3. 对 `suite` 中每个 `dir`：`dir == normalized` 时跳过（入口目录自身不并入）；否则 `prefix := dir + "/"`，遍历 `raw`，对 `strings.HasPrefix(p, prefix)` 的路径执行 `if _, ok := filtered[p]; !ok { filtered[p] = content }`（原相对路径、不覆盖已有键）。
4. X 压平逻辑、报错路径、根识别、根内容提升全部不动。


Run: `cd backend && go test ./internal/skills/ -run TestArchiveFromZipSuiteInstall -v`
Expected: PASS，全部子用例通过


Run: `cd backend && go test ./internal/skills/`
Expected: PASS——特别确认 `TestArchiveFromZipNormalizesWrapperAndNestedFiles`（wrapper 剥离 + 嵌套层级保留）、`TestArchiveFromZipIgnoresOversizedUnrelatedAssets`（examples 剔除与 20MB 边界）不受影响


```bash
git add backend/internal/skills/skill_packages_archive.go backend/internal/skills/skill_packages_test.go
git commit -m "feat(skills): 技能包安装 - 子目录安装并入被引用兄弟技能目录闭包"
```

---

### Task 3：产品上限对最终包生效 + 既有回归确认（tasks.md 1.3 + 2.4）


<!-- comet-task-ref:1-3 --> <!-- comet-task-ref:2-4 -->

**Files:**
- Test: `backend/internal/skills/skill_packages_test.go`（新增 `TestArchiveFromZipSuitePackageLimit`；既有用例只跑不改）
- 无实现改动（1.3 预期零改动，验证即可）

**Interfaces:**
- Consumes: Task 2 完成后的 subdir 闭包行为。
- Produces: 无（仅测试与验证记录）。


新增 `TestArchiveFromZipSuitePackageLimit`：

- 输入：`director/SKILL.md`（frontmatter 合法，正文引用 `heavy-a/SKILL.md`、`heavy-b/SKILL.md`、`heavy-c/SKILL.md`）+ 三个兄弟目录，各含一个 `SKILL.md`（几字节）与一个大文件 `strings.Repeat("x", 7*1024*1024)`（3×7MB = 21MB > `maxSkillPackageBytes` 20MB；单文件均 < 8MB，不触发解压循环的单文件护栏）。
- 调用：`archiveFromZip(data, "director")`。
- 期望：返回非 nil 错误，错误信息为"技能包内容不能超过 20MB"（`finalizeSkillArchive` :277-279 对含兄弟目录的最终集合生效），不静默截断。


Run: `cd backend && go test ./internal/skills/ -run TestArchiveFromZipSuitePackageLimit -v`
Expected: FAIL（Task 2 已实现闭包并入，本用例应直接 PASS 才对——若 PASS，说明 20MB 已对最终集合生效；若 FAIL 或错误信息不符，按失败原因回查 Task 2 实现并修复后重跑）

说明：这是"确认上限生效（预期零改动，验证即可）"的固化用例，不是新行为开发；以 PASS 为验收。


Run: `cd backend && go test ./internal/skills/ -run TestArchiveFromZip -v`
Expected: PASS，逐项确认——
- `TestArchiveFromZipRejectsTraversalAndMultipleSkills` 的 `multiple` 用例：不填 subdir 多 SKILL.md 仍拒绝（spec 场景"不填子目录仍拒绝多技能"）；
- `TestArchiveFromZipIgnoresOversizedUnrelatedAssets`：examples 等无关素材剔除行为不变、技能本体 20MB 边界不变；
- `TestArchiveFromZipNormalizesWrapperAndNestedFiles`：单技能 ZIP 归一化结果与现状一致（spec 场景"单技能仓库安装不变"）。


Run: `cd backend && go test ./internal/skills/`
Expected: PASS

代码复核结论（记录到交付说明，不改代码）：单文件 8MB 在 `archiveFromZip` 解压循环对**全部条目**生效（`skill_packages_archive.go:138-155`，含兄弟目录文件）；512 文件在 `:241-243` 对提升后最终 `files` 生效；20MB 在 `finalizeSkillArchive :277-279` 对最终集合生效（Task 3 Step 1 用例已固化）。


```bash
git add backend/internal/skills/skill_packages_test.go
git commit -m "test(skills): 技能包安装 - 套装包产品上限与既有回归固化用例"
```

---

### Task 4：真实仓库端到端一次性验证（tasks.md 2.5）


<!-- comet-task-ref:2-5 -->

**Files:**
- Create（临时，**不入库**，验证后删除）: `backend/internal/skills/suite_e2e_temp_test.go`
- Data（Git 忽略）: `.local/cache/suite-e2e/manju.zip`、`.local/cache/suite-e2e/storyboard.zip`、对照输出 `.local/cache/suite-e2e/paths-{base,new}-{manju,storyboard}.txt`

**Interfaces:**
- Consumes: Task 2 后的 `archiveFromZip(data, subdir)` 行为。
- Produces: 无入库产物；结论写入交付说明与 Task 5 的 pending-test 记录。


先确认两个仓库的默认分支名（`curl -s https://api.github.com/repos/<owner>/<repo>` 读 `default_branch`，下例假设为 `main`，以实际为准），再下载：

```bash
mkdir -p .local/cache/suite-e2e
curl -L -o .local/cache/suite-e2e/manju.zip https://codeload.github.com/lixiaoxiao9888-create/manju-laoli-skill/zip/refs/heads/main
curl -L -o .local/cache/suite-e2e/storyboard.zip https://codeload.github.com/kevinchin12/storyboard-director/zip/refs/heads/main
```

Expected: 两个 zip 文件生成且非空（manju 全仓约 3.5MB 量级）。网络不可达时向用户说明并请其手动放置归档，不得用伪造归档冒充端到端结论。


`backend/internal/skills/suite_e2e_temp_test.go`（package `skills`，`go test` 工作目录为包目录，归档相对路径 `../../../../.local/cache/suite-e2e/<name>.zip`），两个测试函数：

- `TestSuiteE2ETempManju`：读 `manju.zip` → `archiveFromZip(data, "short-drama-director")` → 断言：
  - 无错误；`Files["SKILL.md"]` 存在且无 `short-drama-director/` 前缀键；
  - 按键收集 `sd-` 前缀的根一级目录名，恰好 12 个，且每个 `sd-*-engine/SKILL.md` 都在包内（13 个技能目录全量：入口总控 + 12 兄弟）；
  - `archive.TotalBytes <= maxSkillPackageBytes`。
  - 副作用：把 `sort.Strings` 后的全部键逐行写入 `.local/cache/suite-e2e/paths-new-manju.txt`（绝对路径由测试内通过向上回溯仓库根拼出，或直接用环境变量传入）。
- `TestSuiteE2ETempStoryboard`：读 `storyboard.zip` → 子目录先按归档实际内容确认（列出 zip 内 `SKILL.md` 所在目录，取根一级总控目录名；storyboard 把子技能嵌在自己目录内，子技能 `SKILL.md` 非根一级、不会进闭包索引）→ 断言：无 `examples/` 前缀键（实证 #6：`examples/` 根一级、44 文件、无 SKILL.md，被双条件排除）；键清单与总字节写入 `.local/cache/suite-e2e/paths-new-storyboard.txt`。


```bash
git worktree add .local/suite-e2e-base 9ce50f27ce93b22955edce69033a19d5674d7b8e
cp backend/internal/skills/suite_e2e_temp_test.go .local/suite-e2e-base/backend/internal/skills/
cd .local/suite-e2e-base/backend && go test ./internal/skills/ -run TestSuiteE2ETemp -v
cd ../../.. && git worktree remove .local/suite-e2e-base
```

说明：worktree 检出 base-ref 独立目录，不触碰当前脏工作区；输出文件名中 `new` 由测试内固定，基线跑完后手工改名为 `paths-base-*.txt`（或测试内读环境变量区分）。worktree 不可用（如磁盘/权限问题）时的替代：在改动合入前先跑一次本测试留存基线输出，再实施 Task 1–3 后重跑对比。


Run: `cd backend && go test ./internal/skills/ -run TestSuiteE2ETemp -v`
Expected:
- manju：全部断言通过（12 个 `sd-*` 目录 + 根入口 SKILL.md，无 `short-drama-director/` 前缀键）；
- storyboard：`diff .local/cache/suite-e2e/paths-base-storyboard.txt .local/cache/suite-e2e/paths-new-storyboard.txt` 输出为空（文件清单与总字节和 base-ref 完全一致，即"同步结果与现状一致"）。


```bash
rm backend/internal/skills/suite_e2e_temp_test.go
git status --short backend/internal/skills/
```

Expected: `git status` 显示 `backend/internal/skills/` 下仅剩本次两个正式文件的改动（或已提交后的干净状态）；`.local/cache/suite-e2e/` 留在本地（Git 忽略），不提交。

本任务不产生提交。

---

### Task 5：更新待测文档（tasks.md 3.2）


<!-- comet-task-ref:3-2 -->

**Files:**
- Modify: `docs/content/docs/progress/pending-test.mdx`（追加小节；该文件已有他人未提交修改，只追加、不改写既有内容）


在文件现有小节之后按既有体例新增：

```markdown
## GitHub 技能套装安装

- 已实现：GitHub 子目录安装时自动纳入被引用的兄弟技能目录（根一级 + 含 SKILL.md + 被入口或已纳入技能正文引用，按引用传递闭包，visited 防环）；兄弟目录文件保持仓库内原相对路径；入口仍提升到根 `SKILL.md`，读取链路与既有校验零改动；20MB / 8MB / 512 文件上限对并入后的最终包生效。
- 已验证：`go test ./internal/skills/` 与 `go build ./...` 通过；manju-laoli 真实归档填 `short-drama-director` 端到端装出 13 个技能目录全量包；storyboard 真实归档安装结果与改动前逐键一致。
- [ ] 用户验收：浏览器实际安装 `lixiaoxiao9888-create/manju-laoli-skill` 子目录 `short-drama-director`，确认安装成功、技能包文件浏览含 12 个 `sd-*-engine` 目录、Agent 渐进披露读取兄弟 `SKILL.md` 正文正常。
- [ ] 用户验收：确认不填子目录安装多技能仓库仍被拒绝、ZIP 上传与 Markdown 上传行为不变。
```


Run: `cd docs && bun run types:check`
Expected: 无类型/MDX 错误


pending-test.mdx 的提交并入 Task 6 统一处理（该文件含他人未提交改动，见 Task 6 Step 4）。

---

### Task 6：最终验证与提交推送（tasks.md 3.1 + 3.3）


<!-- comet-task-ref:3-1 --> <!-- comet-task-ref:3-3 -->

**Files:**
- 本次涉及：`backend/internal/skills/skill_packages_archive.go`、`backend/internal/skills/skill_packages_test.go`、`docs/content/docs/progress/pending-test.mdx`（其余工作区改动一律不动）


```bash
cd backend && go test ./internal/skills/ -count=1
cd backend && go build ./...
```

Expected: 测试全 PASS；build 无输出、退出码 0。


Run: `git status --short`
Expected: 本次改动仅上述三个文件（Task 4 临时文件已删除）；他人未提交改动原样保留。


Run: `git branch --show-current`
Expected: `codex/welcome-ui-hallmark`（基线分支；若执行时分支已变，以实际分支为准并推送该分支）。


先 `git diff docs/content/docs/progress/pending-test.mdx` 逐块核对：若该文件 diff 除本次追加的"GitHub 技能套装安装"小节外还含**非本次任务**的改动，不得静默夹带——默认只提交后端两个文件并把 pending-test.mdx 留在工作区（交付说明注明原因），仅当用户明确同意时才一并提交；若该文件当前改动全部属于本次任务则一并提交。

```bash
git add backend/internal/skills/skill_packages_archive.go backend/internal/skills/skill_packages_test.go
git add docs/content/docs/progress/pending-test.mdx   # 仅在 Step 4 检查通过/用户同意时
git commit -m "feat(skills): 技能包安装 - GitHub 子目录套装安装纳入被引用兄弟技能目录"
```


```bash
git push origin <Step 3 确认的分支名>
```

Expected: 推送成功；交付说明中如实记录测试结果、端到端对照结论与未覆盖边界。

---

## 计划自审

- **Spec 覆盖**：delta spec 8 个场景逐条有归属——引用可达 / 传递闭包 / 双条件排除（Task 2 用例表）；入口提升与唯一性（Task 2 套装闭包用例 + 入口自引用用例）；超限明确报错（Task 3 Step 1）；不填子目录拒绝多技能 / 单技能不变（Task 3 Step 3 既有回归）。tasks.md 11 个复选项全部映射（见对照表），无遗漏。
- **技术要点一致性**：分隔符表不含 `/`、双形态匹配、visited 防环、根识别零改动、ZIP/Markdown 路径不变、不覆盖已有键、20MB 对最终集合生效，全部落在 Global Constraints 与对应 Task 的实现要点中，表述与 Design Doc 一致；`suiteSiblingDirs` 签名与设计文档完全相同，入口目录自身排除放在调用点（设计文档签名不携带入口目录信息，spec"入口唯一性"场景要求该排除）。
- **占位符检查**：无 TBD / "适当处理"类步骤；测试用例以输入/期望表给出可逐字落地的规格，实现以要点清单限定边界（本计划按任务要求不内嵌实现代码）。
- **类型一致性**：`suiteSiblingDirs(raw map[string][]byte, entryText string) []string` 在 Task 1 定义、Task 2 消费，名称与签名一致；错误信息引用与代码现状（:209/:227/:242/:277-279）一致。
- **风险边界**：漏引（变量拼接）与假阳性纳入按设计文档"风险与边界"接受，不在本计划处理；端到端依赖 GitHub 网络可达，不可达时走手动放置归档路径并在交付中如实说明。
