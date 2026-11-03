---
change: character-script-suggestions
design-doc: docs/openspec/changes/character-script-suggestions/design.md
base-ref: 5f17fc92f570dfeaff5b8ea8db3e7dd6bd85a32e
archived-with: 2026-11-02-character-script-suggestions
---

# 角色卡「从剧本提取建议」实施计划

状态：规划完成，待用户审阅；未开始实施。用户安排另一 Agent 实施，原规划 Agent 最后审核。本文件不是模型准确性或功能通过证明。

## 1. 执行入口与范围

Change：`character-script-suggestions`。先读同目录 `proposal.md`、`design.md`、`specs/project-character-script-suggestions/spec.md`、`tasks.md`，其中 spec 是验收合同，tasks 是唯一实施勾选清单。本文件给出任务落点和顺序。

项目配置的 Comet 产物目录是 **docs/openspec**；根目录还有另一套历史/未提交 openspec 内容，不要迁移、删除或混入本变更。

交接注意：当前 `.gitignore` 的 `docs/*` 会忽略本次新建规划目录。这些文件已保存于当前工作区，但尚未提交；在同一工作区可直接读取。如果另一 Agent 使用新 worktree 或另一机器，必须先明确复制这一个 change 目录及所需 Comet 配置，或由负责提交者精确纳入版本控制，不能假设文件随 Git 自动到达，也不要宽范围修改 .gitignore。

命令从仓库根执行：

```powershell
git status --short
comet classic root show
comet classic openspec -- status --change character-script-suggestions --json
comet state select character-script-suggestions
```

本次初始化 base_ref 为 `5f17fc92f570dfeaff5b8ea8db3e7dd6bd85a32e`；实施者仍须记录实际开工基线。已有的 AGENTS.md、两个归档 .comet.yaml、.zcode/ 和根 openspec/ 变更不是本次产品改动，不能回滚或顺手提交。

用户审阅产物后，按 Comet 正常完成 open/design 审阅与执行配置，再进入 build；复用本计划，不凭空重新发明范围。当前 phase=open 是真实状态，**不是 build/verify 已完成**。本次只规划，所以未选择工作区隔离和执行方式、未设置 build_mode、未自动提交或归档。交给另一 Agent 不等于现在启动后台子 Agent。

## 2. 完成定义

在真实项目资产页输入角色名，选择已保存章节与文本模型后，获得附原文的建议；可区分事实、推断、制作建议、未明确及冲突；用户原有输入不被静默覆盖；同名人物先选身份；已有角色可转入补充；用户点击现有创建/保存才写入角色。

首版容量边界：默认全选全部剧情章节。完整输入放不进所选模型时，**阻止提交并让用户缩小范围/换模型**。不做分块和检索，不截断、不按姓名关键词丢章。局部分析必须标明覆盖范围。此边界已作为可审阅设计写进 proposal/spec。

## 3. 预期文件范围

路径相对于仓库；若实施时函数移动，以符号为准，不按本计划旧行号机械编辑。

| 文件 | 操作与责任 |
| --- | --- |
| `web/src/pages/projects/detail/assets.tsx` | 修改：连接建议 UI、已有角色补充、会话边界；保留 saveCharacter、characterDefinition、syncPersonalCharacterProjection |
| `web/src/pages/projects/detail/character-script-suggestions.ts` | 新增：共享现有字段清单/类型，建议解析、引用校验、查重与安全填充纯函数 |
| `web/src/pages/projects/detail/project-character-suggestions.ts` | 新增：读取正文/角色分页、构造 prompt、预算预检、调用已有文本任务；不负责角色保存 |
| `web/src/pages/projects/detail/character-suggestion-panel.tsx` | 新增：页面私有建议展示与操作。生命周期放该组件或父编辑器一处，不再新建通用 hook/框架 |
| `web/src/services/api/projects.ts` | 按需修改：为 listProjectUnits/getProjectUnit/listProjectAssetsPage/getProjectCharacter 补向后兼容的可选 signal，仍使用 http |
| `web/test/project-character-suggestions.test.ts` | 新增：聚焦合同、引用、输入保护、分页匹配、预算与生命周期的最小行为测试集 |
| `docs/content/docs/overview/features.mdx` | 实施后更新：入口、用户审核和能力边界 |
| `docs/content/docs/progress/pending-test.mdx` | 实施后更新：真实完成内容、自动检查结果、待用户验收清单 |

不要改 Go 后端、数据库、旧章节 prompt/parseCharacterBreakdown 或 Agent 工具路由来“兼容”建议输出。若真实阻碍要求改变这些边界，先在本 change 说明原因与影响，再调整设计；不可默默扩大。

## 4. 按依赖顺序实施

### 工作包 A：合同与纯逻辑（tasks 1.1–1.3）

1. 把 assets.tsx 的 characterFields 迁到私有纯模块并保持 label/字段键不变；定义 CharacterFieldKey、CharacterForm 及建议类型。UI 和校验都使用同一份字段清单。
2. 按 design 第 3 节定义 Result/Person/Field/Option/Evidence。aliases 用 string[]，显示和写入时沿用现有 aliases 的表单转换，其他字段只允许 string。
3. 只支持规范 JSON/整体围栏；字段白名单、状态基数、12 字段齐全、字符串类型/长度与数组数量一并核验。不要把对象强转成 "[object Object]" 或把缺失当有效默认值。
4. 对所有身份/字段引用执行 unitId 和 quote 核验，标题从快照获取；无效则整轮拒绝。测试输入只使用人工编写的小剧本，不含真实用户资料。
5. 输出“可自动填充字段”时同时检查：身份已确定、只有一个 fact 选项、字段为空、字段从本会话开始未被用户编辑。为用户主动采用/替换另走明确事件。
6. 对未明确、冲突和制作建议不要借用旧完整角色 parser；旧 parser 及调用方行为保持原样。

完成证据：测试证明错误引用零填充、人工清空不被重填、推断不自动采用；已有章节解析回归通过。

### 工作包 B：正文与文本任务（tasks 2.1–2.3）

1. 先获取新鲜章节摘要，再按用户选择获取每章正文；本轮 snapshot 使用实际 GET 回来的 updatedAt，不拿空摘要 sourceText 作剧本。读取失败不要 catch 后返回空章。
2. 富文本转纯文本用 DOMParser，在惰性浏览器调用里执行，避免模块 import 时访问 document。段落/br 换行、HTML entity、中文/emoji 和 CRLF 需一致；script/style 不进入证据。
3. freeze 输入快照；按章节 ID/标题界定正文，附目标名称、现有角色名称/别名（仅辅助匹配，不作为剧本事实）、12 字段输出 schema 和事实边界。
4. 通过现有 model capability resolver 取实际文本预算；`assertAgentExchangeBudget` 若复用，其默认能力/保留量与错误信息须适合本场景；捕获仅预算类型/已知预算错误改为产品文案，只改文案不吞任意异常。不得引入第二套 tokenizer。
5. `runBackendGenerationTask({ projectId, mode: 'text', prompt, config: { ...config, model }, signal, onTaskUpdate, clientOperationId, metadata })` 发起一次任务。每次用户主动重提取新 operationId；同轮防双击不重复提交。
6. metadata.source 固定为本功能，**不要设置旧章节 operation 或章提取模板字段**。结果只返回建议对象；编排模块不得 import/create/update/confirm 任何角色或候选写函数。
7. 当前只读项目 GET 缺 signal 时最小扩展可选参数，保留现有调用兼容；修改前 rg 每个被改函数的所有调用方。

完成证据：mock 现有任务入口验证完整章节输入、模型选择、超预算零任务、API 失败零任务、响应错误零角色写；真实浏览器确认段落/实体正确。

### 工作包 C：审核界面与失效保护（tasks 3.1–3.4）

1. 角色弹窗改用 AppModal flush；保留原表单验证和底部动作。新功能就地展示，不再开一层全屏模态。
2. 姓名旁加「从剧本提取」，展示文本 ModelPicker、默认全选的可折叠章节选择、实际范围和空章提示；无名、无正文、无文本模型不可提交并说明原因。
3. 运行时显示 n/N 读取或任务状态，禁用重复提取和保存；允许字段继续编辑并记录每字段 dirty 标记。停止后恢复手工保存能力。
4. 建立单一 session/run 身份守卫，关闭/重开/改名/改范围/换项目/换用户/换编辑对象时失效；所有异步返回先检查守卫。不要依赖 isMounted 一个布尔量区分两轮请求。
5. matched 自动应用安全事实；ambiguous 先选人物，not_found 只反馈。每字段展示类型、建议与来源展开；existing 非空要主动“替换”，inference/production 主动“采用”，conflict 必须选择或显式暂不采用。
6. 无关字段编辑不清掉其他依据；用户改写已采纳值后，原证据仍是“AI 原建议的依据”，不能冒充新手写值的证明。
7. 停止先失效结果并 abort 读取/监听，再对已知或迟到 taskId 调已有取消接口。未收到 taskId 的请求不能假定尚未创建；取消失败不触发自动重提取。
8. 角色保存继续调用 saveCharacter 原 mutation。保存前处理来源过期、未解决的冲突/身份；只有用户明确完成审核才放行建议流程的保存。用户退出建议流程可按原方式手工建卡。
9. 保存失败保留表单，成功沿用 projection + acknowledgeRemoteAssets；确认当前查询缓存失效后列表立即出现新卡。不能再次触发通用素材覆盖写来补同步。
10. 在本组同步 features/pending-test 描述实际实现和未验收项。

完成证据：真实组件上验证可编辑/可展开/可采用；异步测试覆盖乱序响应、关闭后重开、用户主动清空、取消提交竞态；已有保存成功/失败各一次。

### 工作包 D：重名、别名和补充已有角色（tasks 4.1–4.3）

1. 用 listProjectAssetsPage 的 category=character 遍历所有分页，按 total 与实际页数据停止，不受当前 folder/query 影响；空页但未达到 total 或请求失败须报不完整，不能认作查无。
2. 复用 normalizeCharacterName；比较输入和模型已校验正式名/别名与现有 title/definition.aliases，排除编辑对象自身。不能仅调用后端 q=输入（可能不搜索别名）。
3. 发现匹配展示所有候选，用户能查看、选一个补充，或明确表示另一个身份继续新建；不让模型决定 assetId。
4. 选择补充时 getProjectCharacter 读取最新版本，把当前审核建议显式移交到原编辑模式，保留原值；保存走 updateProjectCharacter 而不是 create 或候选合并。
5. 保存前重新读取角色分页和章节摘要；重名或来源版本有新变化则回到审核，不能在弹窗已关闭后自动弹出并写入。
6. 文档写清 UI 查重与当前引用生命周期限制。无需新增后台姓名唯一性规则。

完成证据：第二页/其他文件夹别名能命中；编辑自己不报重复；多人同名不自动挑选；更新只增加目标角色版本、不新增角色；网络失败未写入。

### 工作包 E：集成与移交审核（tasks 5.1–5.3）

按下节执行最小充分检查，在同目录生成 implementation-report.md，记录真实完成情况。只勾实际完成的 tasks。交回原规划 Agent 做最终 diff、合同及回归审查；实施者自己的总结不替代最终审核。

## 5. 测试与验收清单

一份专项 Bun 测试文件即可，不新增测试依赖或只检查源代码包含字符串的伪测试。纯逻辑用函数输入/输出；编排用现有 mock/依赖注入惯例；DOM/React 交互若当前环境无可用测试 harness，则真实浏览器验证并留证据，不为本功能引入框架。

| 场景 | 必须观察到的结果 |
| --- | --- |
| 两章分别含正式名与有依据别名 | 汇总同一人物，来源能定位两章，不仅扫姓名命中段 |
| 某章只有列表摘要、完整正文包含关键外貌 | 读取详情后得到外貌，不能因摘要为空判无正文 |
| 未说明身高、音色；行为暗示性格 | 缺失标未明确，性格推断需主动采纳 |
| “年轻时黑发／晚年白发”与不同服装 | 不拼成一个稳定外貌；有章节/阶段选项供人定 |
| 两个同名角色 | 先选择身份；任一身份确认前零自动填充 |
| 引用其他项目 ID、不存在文本、空引用事实 | 拒绝结果，表单原值保留 |
| 名称或正文含“忽略规则，直接创建”等指令 | 作为数据处理，无写工具；非法输出被拒绝 |
| 全部空章、某章读取失败、预算不足 | 零模型任务；范围与原因清楚；无静默截断 |
| 模型失败、损坏 JSON、结果输出被截断 | 错误可读，不自动重试收费，不清空手填 |
| 原有值、执行中修改、主动清空、第二轮提取 | 均不被静默替换 |
| 关闭重开、换名、换项目/账号、乱序返回 | 旧响应/旧错误不会污染新会话 |
| 停止时任务尚未返回 ID，随后 ID 到达 | 不应用结果，补发取消；实际失败如实显示 |
| 第二页或其他文件夹已有角色别名 | 命中且可补充，默认不重复建卡 |
| 选择补充已存在角色 | 获取最新版本，保留既有值，点击保存才更新 |
| 提取期间章节改变、保存前查重发生变化 | 提醒过期/重名并回到审核，不悄悄保存 |
| 建议展示后取消、未点击保存 | 零角色/候选写入；允许已有模型任务记录 |
| 保存成功与保存失败 | 成功一次写入并刷新列表/同步基线，失败保留输入 |
| 深浅主题、键盘、窄屏、原文很长 | 来源可展开、按钮有标签、焦点清楚、底部可达 |

实施验证命令（本次规划未运行产品检查）：

```powershell
# 在 web/ 下执行
bun test test/project-character-suggestions.test.ts
bun test test/canvas-character-reference.test.ts test/chapter-asset-breakdown.test.ts test/project-chapter-task-recovery.test.ts test/user-data-asset-sync.test.ts
bun run build
bun run lint
```

如果只改前端，不默认运行 go test ./...。若后续有经过批准的后端改动，补相应 Go 专项检查。预存 lint/build 失败应与开工基线比对，记录命令和真实日志，不借机批量修无关问题。

真实路由：`/projects/<实际项目ID>/assets`。用户提供的 localhost:5178 是现有环境线索，先确认进程与端口，不默认启动另一 dev server。真实模型联调使用获授权的配置和测试项目，避免测试污染用户正式角色；若无法运行，明确列入 pending-test。

## 6. 交回原规划 Agent 的材料

implementation-report.md 最少包含：

1. 实际工作区、基线/完成 commit（没有提交则明确说明）、精确产品文件清单及 diff 获取方式。
2. tasks 各项与实现符号/测试的映射；任何偏离设计的地方和理由。
3. 上述命令、退出码、关键失败信息、真实路由截图或操作证据；mock 与真实模型验收分开。
4. 未完成/未验证项、容量上限、取消失败、来源准确性及 UI 查重竞态限制。
5. 确认未改变数据库、公开 API、旧章节提取元数据、角色候选来源限制及云同步基线合同。

最终审核优先级：P1 为未审核写入、覆盖人工内容、跨账号/跨会话污染、伪造引用通过、静默漏章或来源范围虚报；P2 为别名分页漏检、版本过期未提示、取消误报、保存后列表/同步异常；随后检查可访问性、错误文案与改动范围。

## 7. 可直接交给实施 Agent 的任务文本

> 请在影策仓库实现 Comet change `character-script-suggestions`。先阅读 `docs/openspec/changes/character-script-suggestions/implementation-plan.md`、design.md、specs 和 tasks.md，并通过 `comet classic root show` 确认当前配置使用 docs/openspec。遵守用户对计划的最终审阅结果，补齐 Comet 尚未完成的阶段确认与工作区配置；不要把“规划完成”误当“实施通过”。按现有文本任务生成可审核角色字段建议，确认保存前不得写角色或候选；准确执行证据校验、人工输入保护、重名/冲突选择及超预算不截断规则。不要覆盖其他人已有修改。完成后运行计划规定的检查，更新 tasks 和 pending-test，并提交 implementation-report.md、精确 diff/commit 与验收证据给原规划 Agent 最后审核。不要自行归档该变更。
