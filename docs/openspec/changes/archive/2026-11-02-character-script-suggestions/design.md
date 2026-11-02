---
comet_change: character-script-suggestions
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-02-character-script-suggestions
status: final
---

# Design

## Context

动机与产品范围见 proposal.md。本文件是完整技术路线草案，implementation-plan.md 给出逐步交接任务；用户当前只要求规划，未实施、未验证产品功能。

现状依据（路径均相对于仓库）：

| 位置 | 已确认事实 | 对方案的约束 |
| --- | --- | --- |
| `web/src/pages/projects/detail/assets.tsx` | `CharacterEditorModal` 为手填；`saveCharacter` 复用创建/更新；`syncPersonalCharacterProjection` 认领云同步基线 | 保留保存与投影链路，建议只进入表单 |
| `web/src/pages/projects/detail/project-chapter-ai.ts` | `extractChapterAssets` 使用章节模板及文本任务 | 复用任务服务，不能把单人建议伪装为章节批量任务 |
| `backend/internal/repository/repository_projects.go` | `ProjectUnitSummaries` 显式未 SELECT source_text | `listProjectUnits` 后必须 `getProjectUnit` 读取正文 |
| `web/src/lib/canvas/canvas-character-reference.ts` | `parseCharacterBreakdown` 强制 role、多个设定和声音字段；导出 `normalizeCharacterName` | 不调用/放宽旧解析器；复用姓名规范化 |
| `backend/internal/app/project_shot.go` | 角色候选仅接受章节提取来源且需 unitId | 不走 candidates 写路径，不伪造来源 |
| `web/src/services/api/generation-task.ts` | `runBackendGenerationTask` 支持文本、signal、onTaskUpdate、clientOperationId | 直接复用，不另写 fetch/模型代理 |
| `web/src/services/api/task-center.ts` | AbortSignal 只停止监听；有独立 `cancelGenerationTask` | 正确区分监听停止和后端取消 |
| `backend/internal/app/project_character.go` | 创建检查归属及字段，但当前没有按姓名强制唯一 | 本次查重为 UI 辅助，不能声称数据库强去重 |

## Goals / Non-Goals

**Goals:** 一个可审核的本地建议会话；可信来源快照；严格解析；现有角色写入不变；新建及用户主动转入的已有角色编辑均能使用。

**Non-Goals:** 见 proposal；尤其不建设向量库、工具循环、多 Agent、全新审批服务、数据库迁移或通用表单框架。不以执行次数多来代替可验证的依据。

## Decisions

### 1. 选择一次结构化文本任务

| 方案 | 结论 |
| --- | --- |
| 批量章节提取后按姓名过滤 | 不采用：无字段证据契约、只覆盖单章、会产生资产候选副作用 |
| 自主 Agent 搜索与工具循环 | 不采用：任务输入输出明确，增加权限、状态与重复写入风险 |
| 完整选定正文 + 一次结构化文本任务 | 采用：现有任务通道足够；响应严格校验后只回填草稿 |

在页面私有 `project-character-suggestions.ts` 组织项目读取、prompt、任务调用，纯类型/校验/填充决策放 `character-script-suggestions.ts`，建议区域放一个页面私有组件。不要把规则堆进通用 API request 或 Agent 工具目录。

prompt 与 JSON 输出合同在该功能模块同源定义。通过通用 text operation 调用；不借用 `chapter_character_breakdown`、`CharacterExtract` 或 `ChapterAssetsExtract` 标记，以免章节恢复误关联。metadata 仅包含 `domainProjectId`、`source: project-character-script-suggestions`、本轮 operationId 等定位信息，正文只进入正常任务输入，不重复放 metadata。

### 2. 正文范围、格式与预算

- 打开弹窗加载项目章节摘要；默认选 `kind === chapter || kind === episode`，按 position 排序，不根据 wordCount 直接丢章。
- 用户触发时逐个读取所选 `getProjectUnit`，首版顺序读取，显示 n/N；任一失败终止本轮。空正文显示排除列表。
- 使用浏览器原生 DOMParser 解析富文本，去掉 script/style，保留段落和换行、解码实体，使用同一份纯文本生成 prompt 和校验证据。不要沿用章节页私有 regex stripHtml（会粘连段落、不完整解码）。不跨页重构旧逻辑，不执行原文 HTML。
- 冻结 `{projectId, unitId, title, updatedAt, text}` 快照。显示「基于本次读取的已保存版本」，不保证不同 GET 属于数据库同一时刻快照；保存前比较当前章节摘要 updatedAt，变化则保留用户输入、标记来源过期并要求重提取或明确确认沿用旧快照。
- 所有被选非空章都进入 prompt，不能仅按名字字符串命中来筛选，否则称谓、别名、代词会漏掉。
- 按当前实际渠道/模型的文本能力和输出预留校验预算。优先复用 `assertAgentExchangeBudget` 与模型能力解析；使用准确的 system + prompt + schema 计算输入，不通过 `budgetAgentHistory` 删除正文来凑预算。现有文本生成函数本身没有该全量正文预算保护。
- 超预算直接阻止模型任务，提供章节多选和模型切换，不自动分块或静默截断。这是首版明确上限，用 `ponytail:` 注释写明升级路径为分块证据提取再汇总。
- 正文和角色名均为不可信任务数据，使用 JSON 序列化边界；提示模型忽略剧本内指令，只依据输入整理人物。无需外网检索。

### 3. 模型输出合同与证据验证

字段键直接来自现有角色表单：role、aliases、appearance、physique、clothing、personality、props、consistencyPrompt、multiViewPrompt、voiceLanguage、voiceAge、voiceTimbre。不要复制两套可漂移的字段列表；提取为页面私有导出后 UI 和解析复用。

建议合同（实际 TypeScript 类型由实施 Agent 落地）：

```ts
type Evidence = { unitId: string; quote: string };
type Option = {
  value: string; // aliases 在该字段使用 string[]，其他字段使用 string
  basis: 'fact' | 'inference' | 'production';
  evidence: Evidence[];
};
type SuggestedField = {
  status: 'suggested' | 'unspecified' | 'conflict';
  options: Option[];
};
type Person = {
  name: string;
  aliases: string[];
  identitySummary: string;
  identityEvidence: Evidence[];
  fields: Record<CharacterFieldKey, SuggestedField>;
};
type Result = {
  status: 'matched' | 'not_found' | 'ambiguous';
  people: Person[];
};
```

该代码为合同示意；实现必须为 aliases 提供严格区分类型，不能用强制断言绕过校验。候选身份引用能说明输入名/称谓与人物的关联；有可能改写正式姓名时须向用户展示并主动采纳，不自动替换姓名框。

- 接受一个 JSON 对象，可去除整体 Markdown JSON 围栏后 JSON.parse；不写复杂的宽松纠错或强制字符串化。
- matched 必须恰好一人，ambiguous 至少两人，not_found 必须空数组；姓名非空、字段白名单、12 个字段齐全、枚举/数组/字符串/数量限制严格校验。多候选只返回与输入有关的人物；不得静默截断超限结果。
- 每个人物至少一个有效身份引用；fact/inference 必须有至少一个有效引用。production 可不引原文，但必须明确标为制作建议且不能伪装成缺失的身高、音色等事实。
- 引用 unitId 必须在本轮快照中，quote 非空且能在对应 text 中精确匹配（只统一 CRLF/LF，不模糊匹配）。章节标题由本地快照提供，不信任模型标题、URL 或 HTML。
- suggested 恰好一个选项；unspecified 的 options 必须为空；conflict 至少两个有依据的选项。三视图/一致性约束默认为 production，派生性格标 inference；遇到伪装成 fact 的制作字段拒绝或明确按制作建议处理，不自动当事实采纳。
- 任一合同/引用错误拒绝整轮，不部分填表、不自动付费重试。错误保留表单，提供重试入口。
- 原文存在不等于推断正确；界面使用「剧本依据」「AI 推断」「制作建议」，不显示未经校准的百分比置信度或“已验证准确”。

### 4. 填充与并发保护

本地状态只覆盖 `idle → loading-sources → extracting → review/error`。每轮捕获 projectId、用户 scope、editor asset ID、输入姓名、model、scope IDs、operationId 及字段编辑标记。

- 每次打开/关闭、项目或账号切换、目标姓名变化、范围变化和重提取均使旧 operationId 失效。signal 用于读取与等待；所有 await 后应用结果及报错前再检查会话。
- 无歧义且有有效引用的唯一 fact 只填未编辑的空字段；用户主动清空也算编辑，不能以“现在为空”覆盖。旧 AI 自动填入的值也不在重试时静默覆盖，旁列新建议由用户替换。
- 推断、制作建议只呈现「采用」；已有值对应「替换为建议」。冲突必须选择、手动编辑或显式“暂不采用”；ambiguous 在身份选择前一律不填。
- 未明确字段显示状态，保存时维持空值，不把「正文未明确」写成实际设定值。
- 运行中禁用创建/保存；停止后可继续手填。成功提取后仍须点击原保存按钮才写角色。
- 关闭/停止：先使会话失效、abort 本地监听；已知 taskId 时调用 cancelGenerationTask。若停止发生于提交窗口，在 onTaskUpdate 收到迟到 taskId 后补取消。取消失败如实显示，不宣称上游已停或退款。
- 保存成功、失败、列表缓存失效以及云端素材基线认领沿用现有链路；若当前列表不能即时刷新，在现有 onSuccess 最小补 `assets` 查询失效，不再写第二套保存流程。

### 5. 重名匹配与已有角色补充

按 category=character 拉取项目资产全部分页，不传当前 folderId/query。复用 normalizeCharacterName，对输入、已验证的正式姓名/别名，与已有角色 title/definition.aliases 比较；不是读取当前页的 characterAssets。更新时排除自身。

查询失败不能解释为“无重复”；建议流程要求重试，用户仍可退出建议会话并手工操作。保存前重新查询，降低跨会话漏查机会；匹配是提示，不是数据库唯一约束。

匹配提示提供查看、补充已有角色及明确另建不同身份的选择。补充先 getProjectCharacter 加载最新版本，通过已有编辑器保存；保存已有人工值，保留建议供主动采纳。不自动触发合并、版本写入或候选确认。切换目标角色要失效旧请求；只有用户已选身份的已校验建议能显式传到新编辑会话。

### 6. UI 与可访问性

将该角色弹窗收敛为 `AppModal flush`，不扩大改其他弹窗。姓名旁放次操作「从剧本提取」，沿用 ModelPicker；范围选择可折叠，默认全选。顶部说明「AI 建议，确认保存前不会创建角色」。字段下显示标签、建议、采用/替换以及可展开引用。冲突和身份歧义优先展示。

复用组件 token 和现有 Tabs/Input/Button；原文按纯文本渲染；错误与进度使用 aria-live，标签不只依靠颜色；键盘可访问展开、选择和采用，窄屏只在内容区滚动且底部保存可达。不要添加独立 HTML 验证页面。

## Risks / Trade-offs

- 模型漏检人物、语义误判 → 人工审核、显示真实分析范围和来源；不承诺绝对准确。
- 超长篇无法一次分析 → 预算前置拦截，显式选择部分章节/更大模型；首版不实现分块汇总。
- 读取阶段剧本变化 → 快照版本提示，保存前检查并要求重提取或明确沿用。
- 多会话同时创建 → UI 查重仍有竞态，首版不增加业务唯一约束；不宣称彻底防重。
- 关闭后取消失败 → 本地结果必失效，后端取消状态如实报告；任务记录可在现有任务中心查看。
- 证据仅保留当前建议会话 → 保存后角色沿用原 definition，不承诺永久来源追溯；需要审计历史时再设计版本化证据持久化。

## Migration Plan

无数据库和接口迁移。只发布前端代码与文档；回滚本功能入口及页面私有模块即可，已有角色数据保持原契约。不得改变旧章节任务 metadata、解析器及后台候选限制。

## Open Questions

无阻碍编写计划的产品问题。执行 Agent 开工前需确认实际可用文本模型及浏览器联调条件；无法执行真实模型验收时必须记录未验收，不将 mock 测试作为模型准确性证明。
