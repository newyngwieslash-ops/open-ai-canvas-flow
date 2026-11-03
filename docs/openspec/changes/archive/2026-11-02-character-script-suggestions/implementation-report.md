# 角色剧本建议（character-script-suggestions）实施报告

状态：**实施完成，等待原规划 Agent 最终审核**。本报告由实施 Agent 产出，不替代最终 diff、合同与回归审核；未经最终审核不标记整个需求验收通过、不归档 change。

## 1. 实际工作区、基线/完成 commit 与产品文件清单

- 工作区：`E:\AI_Vedio_Things\new-toonflow\open-ai-canvas-flow`
- 分支：`feature/20261006/character-script-suggestions`
- 基线 commit：`5f17fc92`
- 完成 commit：`c5289461`（`git rev-parse HEAD` @ 报告生成时；本报告与 tasks.md 勾选提交后以最终 commit 为准）
- 提交序列（`git log 5f17fc92..HEAD --oneline`，13 个提交，Tasks 1–4 各含实现/修复/勾选）：

```
c5289461 chore(openspec): 角色剧本建议 - 勾选任务 4.1-4.3
35ebd7d9 fix(assets): 角色剧本建议 - 修复查重与补充模式评审发现
b4f8bbee feat(assets): 角色剧本建议 - 项目重名查重与补充已有角色
3838e350 chore(openspec): 角色剧本建议 - 勾选任务 3.1-3.4
4b692077 fix(assets): 角色剧本建议 - 修复二轮评审发现
42cb1d50 fix(assets): 角色剧本建议 - 修复审核界面评审发现
8f64b9aa feat(assets): 角色剧本建议 - 弹窗审核界面与会话生命周期
dd6acc4a chore(openspec): 角色剧本建议 - 勾选任务 2.1-2.3
36af8aad fix(assets): 角色剧本建议 - 修复评审发现的守卫失效与接口缺口
636b43fc feat(assets): 角色剧本建议 - 正文读取、预算预检与文本任务编排
b948d3db chore(openspec): 角色剧本建议 - 勾选任务 1.1-1.3
6a63d49c refactor(assets): 角色剧本建议 - 收紧建议字段类型并补 aliases 转换测试
5383bfee feat(assets): 角色剧本建议 - 建议合同与纯逻辑模块
```

- 产品文件清单（`git diff --stat 5f17fc92..HEAD -- web/ docs/content/`，9 个文件，+4392/−21）：

| 文件 | 变更 |
| --- | --- |
| `web/src/pages/projects/detail/character-script-suggestions.ts` | 新增（381 行）：建议合同类型、严格解析、引用核验、重名匹配、安全自动填充决策、12 字段清单单一来源 |
| `web/src/pages/projects/detail/project-character-suggestions.ts` | 新增（402 行）：正文逐章读取与快照冻结、富文本转纯文本、prompt 构造、预算预检、文本任务编排、跨轮守卫、分页查重遍历 |
| `web/src/pages/projects/detail/character-suggestion-session.ts` | 新增（543 行）：会话守卫状态机、运行身份比对、审核采纳决策、保存门禁、停止序列控制器、运行通知通道、保存前复查与补充移交纯函数 |
| `web/src/pages/projects/detail/character-suggestion-panel.tsx` | 新增（952 行）：建议审核面板（AppModal 内嵌、模型/章节范围选择、逐字段审核、查重候选 UI、补充移交接线） |
| `web/src/pages/projects/detail/assets.tsx` | 修改（+126/−21）：CharacterEditorModal 改 AppModal flush、面板挂载、requestSave 接 reviewGate + preSaveCheck + openRef 防护、supplementCharacter |
| `web/src/services/api/projects.ts` | 修改（+16/−16）：4 个 GET 函数补可选 `signal`（唯一 API 层变更，见 §5） |
| `web/test/project-character-suggestions.test.ts` | 新增（1963 行，160 个用例）：上述全部纯逻辑的行为测试 |
| `docs/content/docs/overview/features.mdx` | +10：功能说明同步 |
| `docs/content/docs/progress/pending-test.mdx` | +20：浏览器/真实模型待验收清单（见 §4） |

另含 `docs/openspec/changes/character-script-suggestions/tasks.md`（勾选记录，gitignored，`git add -f` 提交）。

diff 获取方式：

```bash
git diff 5f17fc92..c5289461 -- web/ docs/content/          # 全部产品变更
git diff 5f17fc92..c5289461 -- web/src/services/api/projects.ts   # 唯一 API 层变更
git log 5f17fc92..HEAD --oneline                            # 提交序列
```

## 2. OpenSpec tasks 与实现符号/测试的映射及设计偏离

| OpenSpec task | 主要实现符号 | 测试（`web/test/project-character-suggestions.test.ts`，共 160 项全通过） |
| --- | --- | --- |
| 1.1 建议合同与解析 | `characterFields`/`characterFieldKeys`/`CharacterForm`/`SuggestedField` 判别联合/`parseCharacterSuggestions`（严格 JSON、状态基数、白名单、长度上限、fact/inference 必须有引用） | 字段清单迁移 2 项、解析/合同校验 17 项、aliases 转换 3 项 |
| 1.2 引用核验与事实规则 | `verifySuggestionEvidence`（unitId 必须在快照内、quote 归一 CRLF 后逐字匹配、标题取本地快照）、三视图字段伪装 fact 降级 production | 引用核验 8 项（含不存在章节/伪造原文/无依据事实/阶段差异不自动填充） |
| 1.3 只填未编辑空字段 | `computeSafeAutoFillFields`（matched+fact 唯一选项+空值+未 dirty）、`findMatchingExistingCharacters` | 安全填充 5 项、重名匹配 6 项；旧解析回归 `canvas-character-reference.test.ts` 10 项通过 |
| 2.1 正文读取与快照 | `extractCharacterSuggestions`（逐章顺序 GET、摘要只取 id/kind/title/position、空章进 `excludedUnits`、任一章失败整轮抛错）、`convertRichTextToPlainText`（惰性 DOMParser）、快照 `{projectId, createdAt, units[{unitId,title,updatedAt,text}]}` | 多章顺序/跳过非剧情 1、空章排除 1、读取失败终止 1、取消信号中止 1、prompt 与纯文本转换 4 |
| 2.2 模型任务编排 | `buildCharacterSuggestionPrompt`（JSON.stringify 边界嵌入正文/角色名，内嵌"忽略剧本内指令"）、`checkCharacterSuggestionBudget`（window−maxOutput−overhead 口径，超预算零提交）、`defaultCharacterSuggestionDependencies.runTask`（mode:'text'）、metadata 仅 `domainProjectId/source/operationId/characterName/unitIds`、`createRunSession` 跨轮守卫 | 预算 2（超预算零提交、中文口径）、任务 5+（signal 同引用、operationId 每轮新值、无旧章节字段、解析失败/伪造引用整轮抛错、成功前零角色写操作）、跨轮隔离 1、prompt 白名单单一来源 1 |
| 2.3 容量限制与快照语义交接 | 见 §4 未完成/边界项与本报告 | 交接记录：首版阻止提交不分块（升级路径 `ponytail:` 注释标注于 `project-character-suggestions.ts`）；快照为逐章 GET 时刻，非数据库级一致性；错误不触发自动付费重试（编排层零重试逻辑） |
| 3.1 审核界面 | `CharacterSuggestionPanel`：AppModal flush 内嵌、ModelPicker、章节默认全选/可折叠多选、实际范围与空章提示、逐字段建议行（依据展开/冲突选择/身份歧义选人）、not_found 反馈、n/N 进度与任务状态（aria-live） | 纯逻辑（采纳决策/门禁/守卫）已测；组件接线由 tsc/build/lint + 审读证明（Bun 无 DOM/React harness，不伪造组件测试），真实渲染见 §4 浏览器验收 |
| 3.2 会话守卫与停止 | `createSuggestionSessionGuard`/`canUseSuggestionResult`/`createStopController`（失效→abort→已知 taskId 取消→迟到 taskId 补取消单轨防重）、`createRunningChannel`、`beginRunInheritingDirty` | 守卫状态机 5、运行身份 7（关闭重开/改名/改范围/换项目/换用户/换编辑对象拒绝、换模型不拒绝）、停止序列 5+迟到取消共享防重 1、运行通道 4、dirty 继承 3 |
| 3.3 保存门禁与写路径复用 | `decideSaveGate`/`isReviewResolved`/`applyAdoptionToDraft`/`markDirtyFieldsFromValues`/`observeProgrammaticWrites`；保存仍走原 `saveCharacter`（创建/更新复用），失败保留表单 | 保存门禁 4+运行中禁用 2、单字段审核 6、采纳应用 5、证据归属 2、程序化写入不误标 |
| 3.4 文档同步 | `features.mdx`、`pending-test.mdx`（推断需人工采纳、证据仅本会话有效；未把未跑的真实模型场景写为通过） | — |
| 4.1 项目内查重 | `collectProjectCharacters`（category=character、不传 folderId/query、pageSize=80 对齐后端上限、按 total/hasMore 停止、200 页兜底、失败返回 complete:false）、`findProjectCharacterDuplicates`（复用 normalizeCharacterName、excludeCharacterId 排除自身） | 分页遍历 6（第二页/其他文件夹/查询失败/空页未达 total）、查重编排 5（别名跨页命中/自身排除/多匹配/失败不完整） |
| 4.2 补充已有角色 | `supplementHandoffIdentity`/`supplementResumeSnapshot`/`shouldApplySupplementFetch`（关窗丢弃）/补充模式移交时序（`getProjectCharacter` 读最新版本 → `openCharacterEditor` 既有值原样保留 → 建议走常规采纳 → 保存走 `updateProjectCharacter`，无自动合并/覆盖） | 移交身份 3、快照移交 5、GET 应用判定 4、保存前复查门禁 8、来源过期判定 4 |
| 4.3 查重边界文档 | `features.mdx`/`pending-test.mdx` 写明 UI 查重不保证跨会话原子唯一性、后端无姓名唯一约束 | — |
| 5.1 全量自动验证 | 本报告 §3（全部真实执行，exit 0） | — |
| 5.2 浏览器/真实模型验收 | **未执行**（无浏览器/dev server/授权模型配置）；`pending-test.mdx` 已完整列出待验收项，见 §4 | — |
| 5.3 本报告 | 实施者报告交原规划 Agent 审核后，方可归档 | — |

### 设计偏离及理由

1. **Task 2 预算预检在编排模块内实现，未复用 `assertAgentExchangeBudget` 本体**：该函数错误文案面向 Agent 工具会话、签名吃 `ResponseInputMessage[]/tools`，与本功能单 prompt 场景不合。改为模块内用其同一估算口径（ASCII/4 + 非 ASCII ×1.5）与同一 inputBudget 公式（contextWindow − maxOutput − overhead，`agent-context-budget.ts:11-27`）做预检，能力数据取 `modelCapabilityConfigFor`，错误文案为本场景产品文案。已知边缘偏差：`inputBudget` 原实现有 `Math.max` 下限保护，模块内口径缺该下限（台账 M8，见 §4 剩余风险）。
2. **Task 3 停止控制器注入 abort 函数**：Bun 的 `AbortSignal` 实例无 `.abort()` 方法，直接调用会 TypeError。改为调用方注入 `abort: () => controller.abort()`，测试与生产走同一路径。
3. **Task 4 保存前复查的来源新鲜度基准是弹窗期间 React Query 缓存的章节摘要（staleTime 30s）**：非强制 refetch，30s 内可能非毫秒级最新。属可接受竞态，已计入"不保证跨会话原子唯一性"边界；如需更强新鲜度可改为复查时强制 refetch units（首版未做）。Fix Round 1 已让补充模式经 `supplementResumeSnapshot` 恢复快照，来源过期拦截对补充模式同样生效。
4. **Task 2 富文本转纯文本的默认实现按块级 p/div 分段**，`querySelectorAll("p, div")` 对嵌套块级元素会重复取文本（台账 M9，模型输入质量问题）；真实浏览器 DOMParser 转换行为本身需浏览器验收（§4）。
5. **各任务评审 Minor 修复的取舍**：M8/M9/M18–M21 等低危项未逐一修复（详见 §4 台账摘要），交最终审核triage；Task 2/3/4 的 Critical/Important findings 已全部修复并重评审通过（各自 fix round 记录见 `.superpowers/sdd/task-{2,3,4}-report.md`）。

## 3. 验证命令、退出码与关键输出（5.1，全部真实执行）

执行时间 2026-10-06，均在 `web/` 目录下、工作区为本次变更后的 HEAD 状态。

| # | 命令 | 退出码 | 关键输出 |
| --- | --- | --- | --- |
| 1 | `bun test test/project-character-suggestions.test.ts` | 0 | `160 pass / 0 fail / 373 expect() calls`（专项全量） |
| 2 | `bun test test/canvas-character-reference.test.ts test/chapter-asset-breakdown.test.ts test/project-chapter-task-recovery.test.ts test/user-data-asset-sync.test.ts` | 0 | `22 pass / 0 fail / 33 expect() calls`（4 文件回归） |
| 3 | `bun run build`（`tsc --noEmit && vite build`） | 0 | `✓ built in 5.63s`；仅有 chunk >500kB 体积警告 |
| 4 | `bun run lint`（`eslint src test`） | 0 | 无输出（0 error） |

预存失败比对：

- chunk >500kB 警告：基线 `5f17fc92` 构建即存在（Task 1 报告及台账 M7 记录），本次构建仍为唯一构建异常，未新增其他警告/错误。
- 本次验证未发现任何非预存失败，未触发修复授权（无本任务新增修复轮）。

### mock 与真实模型验收的明确区分

- **mock/自动化验证（已执行，如上表）**：模型响应、章节正文、任务回调全部为测试内注入的假实现。这证明合同解析、引用核验、编排守卫、门禁与查重逻辑的行为正确性；**不构成模型输出质量或真实模型协议兼容性的证据**。
- **真实模型验收（未执行，未验收）**：本环境无获授权的真实模型配置，未运行任何真实模型提取。`pending-test.mdx` 已写明"本任务未运行真实模型提取，未将任何 mock 结果写为模型准确性证据"，待验收项见 §4。

## 4. 未完成/未验证项与剩余风险

### 4.1 浏览器/真实模型待验收清单（5.2 未勾选的原因）

本实施环境无浏览器、无 dev server、无获授权真实模型配置，OpenSpec task 5.2 的双语义中浏览器部分未完成，**保持未勾选**。以下为 `pending-test.mdx`「仍待真实环境验收」的完整清单（逐项待用户在真实项目资产路由验收）：

1. 弹窗内展开「从剧本提取」：模型选择、章节默认全选/增删、实际范围与空章提示展示正确；无名/无模型/无章节时不可提交且原因可读。
2. 真实模型提取返回后：逐字段建议、来源原文展开、冲突选择、身份歧义列表（选定人物后字段行出现且可采纳/替换/暂不采用）、not_found 反馈渲染正确；matched 安全事实自动填充空字段并显示「已自动填充」。
3. 提取运行中：n/N 读取进度与「模型分析中：任务状态」展示，重复提取与保存按钮被禁用；字段编辑（含清空）被记录 dirty，结果不覆盖已编辑字段；重提取不回写已清空字段的 AI 值。
4. 停止提取：本地结果丢弃、后端任务取消成功/失败如实显示；停止后迟到的任务 ID 只触发一次取消请求；关闭弹窗后迟到响应不污染表单；重新打开可再次提取。
5. 重名查重（含第 2 页/其他文件夹角色）：候选提示、「查看」只读摘要、「补充此角色」切换编辑模式且既有值保留、「仍要新建」确认文案持续显示（同输入重扫不清除、不触发额外重扫）；补充模式下修改来源章节后保存被拦截；补充 GET 期间关闭弹窗后不会自动重开；保存前新重名或来源被他人修改时拦截并提示回审核。
6. 深浅主题、键盘导航（展开/选择/采用/停止/保存焦点可达）、窄屏内容区滚动且底部保存可达。
7. 真实模型合同验收：解析拒绝（伪造引用/非法 JSON/超预算）在界面上的错误文案与重试路径；真实模型往返端到端（n/N 进度、任务状态、取消竞态）。

### 4.2 首版边界（设计内限制，非遗漏）

- **容量上限**：预算预检按全量输入计算，超预算阻止提交（零任务、零扣费），**不做分块与检索**；升级路径（按章节分块提取再聚合）已用 `ponytail:` 注释标注。
- **取消失败仅显示**：取消请求失败只发 `cancel-failed` 事件如实提示（任务可在任务中心查看），不自动重试、不自动重提取。
- **来源快照非数据库级一致性**：快照为逐章 GET 各自时刻冻结，不同章节间存在时间差；保存前过期比对基于快照 updatedAt 与当前摘要的差异。
- **UI 查重竞态限制**：查重为前端 UI 提示，遍历与复查之间、多会话并发创建之间存在竞态，**不保证跨会话原子唯一性**；后端未新增姓名唯一约束（features.mdx 与 pending-test.mdx 已写明）。

### 4.3 评审 Minor findings 台账摘要（M1–M21，triage 状态）

以下来自 `.superpowers/sdd/progress.md` 各任务评审记录，均经 reviewer 判定为低危/无害，交最终审核一并 triage：

- M1 严格性不对称：option/evidence/顶层未知键静默容忍（运行时白名单覆盖人物/字段两级，option/evidence 级未拒）。**未修复，待 triage**。
- M2 裸 ``` 围栏被接受（`(?:json)?` 正则放宽）。**未修复，待 triage**。
- M3 aliases 静态类型未按 design 严格区分（运行时校验严格，断言不绕过）。**未修复，待 triage**。
- M4 测试死代码 FIELD_STATUSES。**未清理，待 triage**。
- M5 两份 CharacterForm 定义（模块导出 vs assets.tsx 本地）。**Task 2/3 接入后仍并存，待 triage 收敛**。
- M6 模块文件缺末尾换行。**未清理，待 triage**。
- M7 build chunk 警告为仓库既有问题。**预存，不处理**。
- M8 inputBudget 口径缺 Math.max 下限（与 `agent-context-budget.ts:11-18` 边缘偏差）。**未修复，待 triage**。
- M9 `querySelectorAll("p, div")` 嵌套块级元素文本重复（模型输入质量）。**未修复，待 triage**。
- M10 Task 2 fix 报告不实一处：恒真断言测试未删（test:801-804 仍在）+ 守卫测试注释与实际场景不符。**如实移交，待 triage**。
- M11 跨轮守卫测试只覆盖 :260 检查点，:308 响应检查点无行为测试；`defaultCharacterSuggestionDependencies.createRunSession` 未被测试。**覆盖缺口，待 triage**。
- M12 `useRef(createRunningChannel())` 每渲染求值（标准 React useRef 提示，无正确性影响）。**未修复，待 triage**。
- M13 `CharacterSuggestionPanelApi.runningChannel` 死字段（无害溯源文档）。**未清理，待 triage**。
- M14 api 替换存活测试部分复述自身 setup（通知计数断言承载真实价值）。**待 triage**。
- M15 `needsExplicitConfirm`/`invalidateSuggestionSession` 仅测试调用（API 面轻微死代码）。**待 triage**。
- M16 旧 knownTaskIdRef 跨轮污染窗口（reviewer 判定无害）。**待 triage**。
- M17 自动填充 tag 依赖接线。**已修复（Fix Round 1 SuggestionFieldRow autoFilled Tag），浏览器端显示效果待验收**。
- M18 重开同目标编辑器 + 陈旧 GET resolve 极窄窗口可能覆盖少量编辑（同目标语义可辩护）。**未修复，待 triage**。
- M19 resume effect eslint-disable exhaustive-deps（projectId 稳定，仅记录）。**待 triage**。
- M20 查重扫描未传 signal（seq 丢弃响应兜底）。**未修复，待 triage**。
- M21 `requestSave` onSave 内 validateFields 微秒级关窗窗口（既有模式，TypeError 非静默错写）。**既有模式，待 triage**。

### 4.4 真实模型验收发现与修复（2026-10-07）

- **发现**：真实模型验收（deepseek-v4.1-flash，任务 `db3f3a09a06eaf196e9316170869f275`，见 `.local/project-workbench-debug/open_ai_canvas.db`）返回中 `fields.aliases.options[].value` 为字符串 `"玛尔虎"` 而非字符串数组，严格解析按合同报「第 1 个人物不合法：aliases 必须是字符串数组」整轮拒绝；响应其余部分（逐字引用、fact/production basis、人物级 aliases 数组）全部合规。
- **根因**：prompt 规则 6 prose 写明「aliases 的 value 是字符串数组」，但输出 schema 示例把所有字段（含 aliases）的 options[].value 演示为字符串占位 `{"value":"..."}`；模型跟随示例而非 prose 规则。
- **修复**：仅改 `buildCharacterSuggestionPrompt`（`web/src/pages/projects/detail/project-character-suggestions.ts`，prompt 文本）——规则 6 后新增一行数组/字符串双示例：`fields.aliases 的 options[].value 必须是字符串数组（如 ["玛尔虎","老虎"]）；其他字段 options[].value 是单个字符串（如 "羌寨猎户首领"）`。解析/校验合同未放宽，无 lenient coercion，未改 session/UI/后端。
- **防漂移**：`web/test/project-character-suggestions.test.ts` 漂移锁测试（「prompt 字段白名单与 characterFieldKeys 单一来源一致」）新增示例字面量断言（TDD：先 RED 后 GREEN）。
- **验证**：专项 `bun test test/project-character-suggestions.test.ts` 187 pass / 0 fail；`bun run build`、`bun run lint` 退出码 0（仅预存 chunk >500kB 警告，M7）。修复后的 prompt 待真实模型重新验收（pending-test.mdx「真实模型合同验收」项已注明）。

## 5. 确认未改变的部分（附证据）

| 确认项 | 证据 |
| --- | --- |
| 数据库 / Go 后端 | `git diff --stat 5f17fc92..HEAD` 仅含 `web/` 与 `docs/` 路径，零后端文件；无新表、无迁移 |
| 公开 API（`web/src/services/api/*`） | 唯一变更为 `projects.ts` 4 个 GET 函数补**可选** `signal`（`listProjectUnits`/`getProjectUnit`/`getProjectCharacter` 的 `config?` 参数、`listProjectAssetsPage` 的可选 options 字段）；`git diff` 显示全部为纯可选扩展，现有调用方零参兼容，`tsc --noEmit` 通过证明无调用方破坏 |
| 旧章节提取元数据合同 | `rg "chapterId|operation|promptTemplateOperation|chapterOperation"` 在三个新增模块中零命中（仅 `operationId`/`clientOperationId`/`onTaskUpdate`）；新任务 metadata 仅含 `domainProjectId/source/operationId/characterName/unitIds`，未混入旧章节任务字段；`canvas-character-reference.ts` 及旧章节提取代码未改动 |
| 角色候选来源限制 | 查重仅遍历 `category=character` 的项目角色分页（不传 folderId/query），候选 `assetId` 永远由用户点击决定，模型/代码不自动决定；未接入素材库、其他分类或其他项目的角色来源 |
| 云同步基线合同 | `saveCharacter` 保存链路、`syncPersonalCharacterProjection` + `acknowledgeRemoteAssets` 投影认领链路（assets.tsx:857-879）未改动；`user-data-asset-sync.test.ts` 5 项回归通过；`web/src/services/user-data-sync/*` 零变更 |
| 旧章节解析回归 | `canvas-character-reference.test.ts` 10 项通过，该模块未修改 |

---

**给最终审核 Agent 的优先级提示**（自计划 §6）：P1 检查未审核写入路径（补充模式保存、采纳/替换写表单）、人工内容保护（dirty 语义、无静默覆盖）、跨账号/跨会话污染（守卫身份比对）、伪造引用通过（逐字核验）、静默漏章或来源范围虚报（excludedUnits/空章提示）；P2 检查别名分页漏检（pageSize=80 分页遍历）、版本过期未提示（staleTime 30s 基准）、取消误报（单轨防重）、保存后列表/个人素材同步；随后检查可访问性、错误文案与改动范围。M1–M21 的 triage 决定也请一并在最终审核中给出。
