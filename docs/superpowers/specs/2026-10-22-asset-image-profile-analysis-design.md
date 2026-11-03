---
comet_change: add-asset-image-profile-analysis
role: technical-design
canonical_spec: openspec
archived-with: 2026-10-26-add-asset-image-profile-analysis
status: final
---

# 资产参考图档案分析技术设计

## 最新范围裁定（2026-10-24，优先于历史附件）

用户已明确批准收敛本change：保留资产图片分析、字段审阅/选择采用、CAS/幂等、授权/恢复、历史baseline、媒体版本身份，以及全部已完成成果和安全阻断。当前5.2验收分析来源/任务/历史引用保护与保守删除；有引用拒绝，失败/部分完成保留资产、历史、未完成资源及持久删除意图，共享alias不受损。unknown、旧无证据、未结算playback/远端义务继续拒绝物理清理及最终业务记录清理，不要求本项完成每种生产者恢复或使所有对象最终可删。

完整恢复移至[add-resource-producer-full-recovery](../../openspec/changes/add-resource-producer-full-recovery/proposal.md)，远端结算移至[add-remote-storage-physical-settlement](../../openspec/changes/add-remote-storage-physical-settlement/proposal.md)，旧数据维护移至[add-legacy-resource-evidence-maintenance](../../openspec/changes/add-legacy-resource-evidence-maintenance/proposal.md)。P1/P2/P3-B、Windows C1/媒体补验、C2-A账本及设计/批准/审查记录保留；候选OS VM未创建、OS验证未运行，不启用新产品入口，不新增schema或操作业务库，不push。迁出不等于验收通过或授予后续环境、云端、旧数据操作权限。当前逐项合同及证据映射见[范围收敛记录](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/scope-convergence.md)；核心验收后可进入最终Verify，不再受下方历史“完整恢复未闭合所以不能Verify”的阶段性措辞约束。

### 最终集成审查修补

最终集成审查F1补齐同一CAS合同的普通同步入口：已有版本的资产在`UpsertAsset`现有owner/lifecycle事务内核对客户端head，缺失或不等于当前head即409 `project_asset_version_changed`，不允许另一个标签页仅编辑标题时把已采用主版本回退。复用原版本冲突sentinel和结构化错误，不新增同步框架/schema；同head元数据写入及未版本化素材行为保持。验收使用真实分析采用后旧快照写回、当前快照和普通素材正例，版本/收据/表现/项目状态均核对不被错误改写。本段是当前核心合同修补，不属于下列恢复历史。

媒体消费者补核发现的合法历史镜头产物登记同属现有溯源保留合同。已知历史结果沿原镜头版本归属校验，以stale且未选中登记，不替换当前选择或修改当前step/instance进度与输出；事务内仍校验owner/instance代际，ready/selected的陈旧提升、外来版本或已删镜头继续拒绝。原媒体版本/表现及任务溯源幂等保留，不新建恢复协议或绕过分镜并发栅栏。

### 历史恢复阶段记录（不作为当前5.2退出门槛）

C2-A真实OS验证正在按用户要求形成 [具名环境与权限审批稿](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-c2a-os-environment-design.md)：暂无现成VM，候选未创建/未绑定，本轮仅设计及独立审查，实施另批。C2-B、P4/P5、业务库及5.2未完成边界保持。

C2-A 私有验证代码及临时 SQLite / 独占 PostgreSQL 检查现已获用户批准，进展见 [账本实施记录](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-c2a-ledger-implementation.md)。真实 OS 环境、namespace/UID 映射与权限仍另批；不接产品、不改产品 schema、不操作业务库，5.2 保持未完成。此前“C2-A代码待批”表述为历史。

P3-C Windows C1 已获用户批准最小实施，包含三列 schema、临时库迁移、隔离测试及独立审查；范围与有限恢复合同见 [C1 方案](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3c-design.md)。正常产品入口不启用，业务库和编码器不操作，P4/P5 边界保持。下方仅设计/待审批表述保留为历史。

P3-C 的 [实施审批稿](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3c-design.md) 明确 Windows C1 有限控制、候选持久字段与隔离矩阵；仅设计、待审批，不改变已批准合同，不宣称全部控制者丢失恢复通过，P4/P5 边界保持。

隔离验证后的接入方案见 [P3 产品方案](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3-product-integration.md) 和 [P4/P5 验收前提](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p45-acceptance-prerequisites.md)。其中 P3-B 已批准最小实施与临时库验证，不新增 schema、不启用未经验证的 OS 控制、不操作业务库；P3-C 及 P4/P5 后续授权边界保留。附件不替代原验收合同，5.2 仍未完成。

用户于2026-10-22回复“好的，继续”，确认上一轮推荐方案。本文记录已确认技术设计，不表示实施或产品验证完成。唯一行为规范为 `docs/openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/specs/asset-image-profile-analysis/spec.md`。保留首版未提交实现和用户其他变更，不引入多参考、batch 或 Agent。

## 已确认阻塞解除修订（2026-10-23；删除验收按最新裁定收敛）

用户当时在收到具体设计后选择A。以下保留已批准实现依据，媒体身份与baseline合同不变；删除验收按顶部最新裁定及当前delta spec收敛，不再以全部恢复能力为本项门槛。修订优先于正文原有“无DDL”及生命周期待决措辞。历史详细约束见 `docs/openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/blocker-resolution-design.md`。

- 媒体表现取消全局TaskID/role唯一，保留version/role唯一及原TaskID溯源；新增前向索引迁移，同时修正注册/候选重放的目标和原采用版本判断。历史迁移不可改写。原“无DDL”调整为“profile草稿/收据无新增表列，允许此索引及两阶段删除必需的持久化迁移”。
- 专用详情增加可选baseline，仅投影对应分类历史字段并保留JSON类型，经全部对象授权及version归属检查；列表和通用输出不带baseline。缺失/损坏明确不可用，不假造当前值。任务清理后不重建草稿。
- 所有既有素材删除（含清空/强制清除）统一引用保护，有引用拒绝并返回来源。仅无引用且完整物理身份/变体及生产者结算满足安全条件时持久化资产级删除意图、进入删除中且禁止新增引用；保留资产/版本/表现及未完成资源，必要物理对象全部成功后才事务清理记录。失败/部分完成保留意图，不恢复ready；仅在安全条件满足时重试。未知写者、旧无证据、未结算playback/远端义务继续拒绝物理清理及最终记录清理，不承诺本项完成其恢复，不增加schema或提前删除Asset历史。
- 删除按完整存储身份处理共享别名及变体，worker复检引用、身份和lease；上传/转码迟到写须条件更新，不能复活删除中/已删资源。引用写路径也必须验证删除状态。SQLite先获取写锁并完整事务BUSY重试，PG依现有锁顺序，网络删除不占长事务。
- 仅授权实现及临时测试库迁移，不授权现有业务库迁移、真实删除、服务重启、收费模型或Git提交。新设计包单独风险审查，既有已关闭问题及复查轮次历史保留。

## 1. 边界与方案选择

在已有角色、场景、道具资产中明确选择一张已保存的原始参考图。角色草稿仅 appearance、physique、clothing、props；场景（真实 category 为 environment）、道具仅 description、prompt。未知留空，不猜姓名、性格、声音、关系，不创建资产，不自动覆盖。一次选择字段集并采用创建一个版本，草稿不是资产事实。

采用“专用项目 API + 现有文本任务”，复用队列、模型路由、账务和终态 fencing。前端直连识图再普通更新缺少服务端来源绑定、恢复及幂等，不采用；Agent 工具编排超出单次识图与人工采用边界，不采用。调用链保持 handler → service 别名 → app → repository/model，前端业务 API 只用 http。

## 2. 实现依据与兼容边界

- `backend/internal/app/project_reference_image.go` 将原图存于版本 `definition.referenceImage.resourceId`；成品 representation 不能冒充原图。资源须同用户、ready、image MIME 且有尺寸。
- `web/src/pages/projects/detail/assets.tsx` 和生成调用消费角色字段 string；后端 definition 实际为任意 JSON，历史 props 可能是数组。新草稿一律 string；未选历史字段保持原 JSON 类型和值，选中前明确提示“整个字段替换为文本”，不做存量迁移。
- 任务创建已有模型路由准入，但 `ValidateTaskCapability` 跳过 text；必须补本项实际路由后的视觉能力、图片字节预算复核，不能声称所有文本任务均无准入。
- `taskForOutput` 不校验 ResultJSON，metadata 也并非本项安全投影；通用任务输出不能直接充当安全草稿。
- AssetVersion 没有 MetadataJSON/SourceTaskID。Note 有长度约束且不是结构化收据位置；媒体 representation 不能承担文字采用事实。
- `prepareNextCharacterVersion` 会重写表现 TaskID、强制角色分类并重建 payload，不能直接套用于本项；复制媒体必须保留真实原媒体任务溯源。
- `SaveCharacterVersion` 已有主版本 CAS、版本号唯一约束及全部关联项目 revision 更新，本项复用合同而非只刷新当前项目。

## 3. 专用 API 与任务准入

所有路径带 `/api`，返回既有 `{code,data,msg,reason}` 信封，失败使用真实 HTTP 状态。新 DTO 严格拒绝未知键、重复键及多个 JSON 值，不依赖默认 JSON binder 忽略多余字段。

### 创建

`POST /api/projects/:id/assets/:assetId/profile-analyses`

请求为 `expectedVersionId`、`sourceResourceId`、`modelSelection`；模型选择只允许 `{kind:"logical",logicalModelId}` 或 `{kind:"system",channelId,modelKey}`。响应 data 为 `{analysis:ProfileAnalysisView}`。

体上限16 KiB；ID 非空、无首尾空白，version/resource/channel/logicalModel ID 最长36 ASCII字符，assetId 路径最长80，modelKey 最长240 Unicode字符。不接受 prompt、URL、dataURL、客户端 capability、metadata、current/none、批次数或结果字段。

服务端校验当前用户、活动项目、关联资产、主版本归属/category、expectedVersionId 及原图资源。sourceResourceId 必须等于基准 `definition.referenceImage.resourceId`，不自动换最新原图；最终任务创建和预扣事务内重查来源、取得资源锁，不在事务外临时锁一下。

首发只开放服务端可信目录的系统/逻辑视觉文本模型。复用 `resolveTaskModelSelection`，逻辑模型和实际物理路由均须支持图片；实际文本 ChannelModel 的 `Text.references.MaxImages >= 1`、MaxImageBytes 明确有效且容纳 Resource.Size，协议支持图片传输。缺失/0/未知失败关闭，worker 执行或重路由前再验证，不降级纯文本。浏览器 capability 和“OpenAI兼容”不是可信证据，自定义渠道需另行确认可信登记政策后才能开放。

内部固定 `type=canvas_text`、`operation=asset_image_profile_analysis`、`input.mode=text`。服务端生成提示词和一张 referenceImages，storageKey 为 `resource:<id>`，尺寸/MIME/bytes 来自 Resource。metadata 仅含服务端 operation 及 `profileAnalysis:{schemaVersion:1,projectId,assetId,category,baseVersionId,source:{resourceId},promptVersion:1}`。嵌套 resourceId 使用现有扫描器识别的键。

专用入口设置不可从 JSON 反序列化的内部 admission 标记，通用 POST /tasks 伪造 operation/metadata 也拒绝。禁止 text_replay、客户端 text-deltas 完成、tools/Agent、workflowStepId、自动绑定。保留完整密钥保护、配额、预扣和 worker 链路，不用临时残缺 Service 绕过准入。

创建本身不承诺幂等，断网先查恢复，不自动重发收费请求；明确“重新分析”才创建新任务。普通 retry API 拒绝对此 operation 原地重试，worker 租约恢复仍沿既有机制。

### 查询与取消

- `GET /api/projects/:id/assets/:assetId/profile-analyses?limit=20&beforeTaskId=...` 返回 `{analyses,nextCursor?}`，limit 1–50，默认20。
- `GET /api/projects/:id/assets/:assetId/profile-analyses/:taskId` 返回 `{analysis}`，失败、取消、旧基准也可读。
- 使用已有 `POST /api/tasks/:id/cancel` 逻辑取消，然后重新查询专用 view。

列表在 SQL LIMIT 前精确过滤 user/project/type/operation/metadata.assetId，包含 queued/running/succeeded/failed/cancelled。按 `(created_at DESC,id DESC)` 分页，游标也须属于同一查询范围；SQLite 用 json_valid/json_extract，PostgreSQL 使用合法 JSON 的 JSONB 路径，不以 LIKE 或“先抓100条再筛”代替。

ProfileAnalysisView 固定包含 taskId、projectId、assetId、category、baseVersionId、source:{resourceId}、status、stage、createdAt、updatedAt；服务端计算 canCancel、canAdopt、currentVersionId、blockedReason；可选 draft、adoption:{adoptedVersionId,selectedFields,adoptedAt}、error:{reason,message}。列表省略 draft 正文，详情提供。越权/不关联统一404；内部收据摘要不公开。

### 采用

`POST /api/projects/:id/assets/:assetId/profile-analyses/:taskId/adopt`

请求仅 `{expectedVersionId,selectedFields,idempotencyKey}`，响应 data 为 `{adoptedVersionId,replayed,asset:ProjectAssetSummary}`。体上限8 KiB，expectedVersionId 同创建约束。字段集非空、不重复、在分类白名单内；key 为16–64位 ASCII `[A-Za-z0-9_-]`。只接受字段名，拒绝值、definition、替换来源；值取服务端已验证草稿。空字段不能选，成功后不能补选另一字段集，需重新分析或人工编辑。

## 4. 草稿 schema、安全输出与终态

模型原文必须是单个 JSON 对象，精确包含角色四字段或视觉资产两字段，全部为 string。拒绝 Markdown fence、尾随内容、重复/额外键、null/number/array/object。trim 后空串为未知，UI 显示“未知／待确认”而不将文案写回字段；全空合法草稿可展示但不可采用。

新增安全预算为原文32 KiB UTF-8、每字段2,000 Unicode code points（Go rune，前端 Array.from），不沿用旧建议中的32,000字符。禁止 NUL/非法控制字符，允许普通换行；超长整体失败，不静默截断。提示词只描述可见内容，不把图中文字当指令、不补故事/精确身高/隐藏物、不伪造 confidence。白名单不能保证视觉事实，须人工审阅。

成功前解析 provider text，结果为 `{mode:"text",assetProfileAnalysis:{schemaVersion:1,assetId,category,baseVersionId,source:{resourceId},draft,adoption:null|AdoptionReceipt}}`。来源、category、taskId、收据由服务端绑定，模型不能控制。不保留第二份未经校验 text、reasoning、provider 原响应或 canvas_ops。

专用详情、任务中心摘要/详情、诊断和流使用 operation 专属白名单投影，key hash/digest 不公开；非法原文不作为错误正文。活动 UI 每2秒轮询，不显示未校验 token 流。provider 可消费上游 SSE，但本 operation 不持久化/广播原始 delta，不下发失败 TextDraft，`/tasks/:id/text-events` 不得绕过；其他文本任务 SSE 合同不变。

解析在成功提交前完成；安全结果、终态及必要来源检查纳入 `SaveTaskCompletionWithRegistration` 同事务。格式失败为 failed、资产不变，不能事务外先暴露可采用草稿。

仅本 operation 放宽“上游已提交不可取消”，queued/running 可逻辑取消并尽力中止上游，沿既有退款/费用待核对流程，不承诺供应商停止或退费。cancel/success 以条件更新赢家为准：取消先胜，迟到完成被 status/lease fencing 拦截；成功先胜，取消返回 task_state_changed 并刷新。重复取消已取消任务幂等，成功后不能取消。关闭弹窗既不是取消，也不是采用。

## 5. 无 DDL 收据与原子采用

### 收据和恢复边界

在既有 DefinitionJSON 新增服务端保留键 `_profileAnalysisAdoption`，Task.ResultJSON.assetProfileAnalysis.adoption 镜像同一收据，无新增表列。收据包含 schemaVersion:1、operation、taskId、taskCreatedAt、projectId、assetId、baseVersionId、adoptedVersionId、source:{resourceId}、selectedFields、idempotencyKeyHash、requestDigest、draftDigest、adoptedAt。

SHA-256 摘要；requestDigest 为规范编码的 schemaVersion/userId/projectId/assetId/taskId/expectedVersionId/排序selectedFields，draftDigest 绑定规范草稿。key 作用域为 `(userId,taskId)`；字段顺序不影响摘要，重复字段拒绝。不建设 batch 或无限采用数组。

普通 definition 写入不得新增、删除、改写保留键，由服务端保护并保留；无该键旧版本正常读。新版本可继承收据，但仅 `receipt.adoptedVersionId == AssetVersion.ID` 是原始采用记录。新任务采用写新收据，旧记录留历史。表单和提示词按白名单取字段，不将保留键送模型。

任务存在时恢复状态和草稿；清理后历史版本保留采用结果、来源及必要幂等凭据，不重建草稿、不要求历史收据 UNION 到任务列表或回退详情接口。任务详情缺失返回404并提示草稿不可恢复。采用接口遇到 task 缺失，授权后在目标资产历史版本查原始收据，只允许完全匹配重放，异参冲突，无收据404，不能首次采用。查询先按 asset_id 收窄，不新增复杂历史分页；性能不足需另行确认索引 DDL。

范围核对：proposal/spec 要求恢复、重放、历史来源保留，未要求任务删除后的列表 UNION/草稿重建。此处移除草稿未确认的复杂查询候选，保留已有 replay 合同；收据不依赖临时流/诊断保留。

### 单事务算法

新增专用 repository 提交能力，app 承担业务策略，不串联两个各自提交的 service。

1. 校验用户项目/资产关联。PostgreSQL 先锁 Asset FOR UPDATE，再锁 Task FOR UPDATE，重读权威状态；无 task 只走历史收据重放。
2. 先查收据，再检查基准。同 key hash + requestDigest 返回原 adoptedVersionId 及最新资产摘要；同键异参返回幂等冲突，已采用后换key/字段集返回已采用冲突，不追加字段或 revision。
3. 首次采用核验 owner/project/type/operation/category/asset/base/source、成功非取消、草稿 schema；要求 task.baseVersionId == request.expectedVersionId == asset.primaryVersionId，重新校验基准原图及资源归属/ready。
4. 按 resourceId 排序 SHARE 锁资源；深拷贝 definition，只替换 selectedFields 并增加收据。复制全部表现为新行ID，保留原媒体 TaskID、role/resource/mediaType/metadata，复制声音绑定，绝不把表现 TaskID 改为分析任务ID。
5. `WHERE id=? AND user_id=? AND primary_version_id=?` CAS 更新资产，创建版本/表现/声音。保留 title/kind/category/cover/tags/媒体payload；只更新 payload.primaryVersionId/updatedAt，entity 同步 data.definition，image 保留媒体data。保留版本 Prompt/Note；definition.prompt 不等于 AssetVersion.Prompt。所有关联项目 revision 增量一次。
6. 条件更新 succeeded task 的结果收据，任一步失败全回滚，不先标 adopted。提交后返回原 adoptedVersionId 与最新摘要，UI 不回退。

SQLite 不把 Locking 当行锁：首次读前对拥有 Asset 做无副作用条件自赋值 UPDATE（不改时间/revision）取得写锁，再读 task/版本。BUSY/BUSY_SNAPSHOT 整个短事务回滚并限量重试，耗尽503；PostgreSQL deadlock/serialization 同样整事务重试，不以进程mutex/单连接证明跨实例安全。事务中不调用模型。

锁顺序 asset → task（如有）→ sorted resources → 关联项目；受影响既有版本写路径也须先锁asset，避免原先资源先锁的逆序。删除路径不得反向加asset锁，关联项目按ID稳定顺序更新。

## 6. 资源生命周期和真实 UI

既有 resource_reference 扫描 task input/result 和历史 definition；新来源使用 source.resourceId，不只用扫描器不认识的 sourceResourceId。创建/完成/采用与删除锁后重查都要纳入来源，包含普通、脱离资源、管理端删除；事务外快照或原图历史保护不能替代任务来源锁。

不新设草稿 TTL：未采用草稿随 task 保存并受任务配额限制，清理后提示不可恢复；采用收据随版本保留。有效任务、草稿、历史来源引用期间不得删物理对象。现有删除队列不证明“物理失败不丢记录”，该已存在合同需对应资源实现测试证据，缺失则发布阻塞，不顺手全量重构。

修改真实 assets.tsx 及相邻角色/视觉卡片，增加“分析参考图”，复用 AppModal flush、ModelPicker、原图缩略图。当前只有一张原图，仍明确展示选择；缺少原图时先通过已有选择/保存接口保存，再基于新主版本分析，临时上传、current/none 不直接作为来源。不建独立HTML。

ModelPicker 使用 text + imageCount:1，独立构造 `{...config,model:selectedTextModel,textModel:selectedTextModel}` 并解析逻辑/系统模型选择，不误用 imageModel。默认有效 config.textModel，不取项目 defaultImageModel、不改全局偏好、不静默换付费模型。过滤无法可信验证的自定义/工作流模型并解释原因，服务端最终把关。

逐字段显示基准值、当前值（变化时）、建议值；默认全不选，未知禁用，历史数组/其他JSON只读按原类型展示，不能强转为 [object Object]。一次“采用所选N项”，结果不确定期间保持同 field-set/key，断网先GET、重试采用使用同请求。冲突保留草稿并禁用采用，刷新对照，不自动换基准或收费重跑。

查询key带 userId/projectId/assetId，详情加taskId；切账号/资产取消旧读取并忽略迟到结果，关闭仅停订阅。采用后刷新项目资产、角色详情、个人素材投影和分析详情，区分“服务端已保存，本地刷新失败”。不展示原始上游错误，检查焦点、滚动、明暗主题、reduced-motion。

## 7. 错误与验证策略

新增 reason 实施时同步 error-codes、HTTP API/OpenAPI；异步错误使用 view.error.reason，不解析msg。请求非法400 `asset_profile_analysis_invalid_request`；越权/不关联/无任务或收据404 `asset_profile_analysis_not_found`；旧版本409既有 `project_asset_version_changed`；来源变化409 `asset_profile_analysis_source_changed`；资源不可用422 `asset_profile_analysis_source_unavailable`；模型不支持/未知422 `asset_profile_analysis_model_unsupported` / `asset_profile_analysis_model_unknown`；非法结果422 `asset_profile_analysis_invalid_result`；不可采用409 `asset_profile_analysis_not_adoptable`；重放冲突409 `asset_profile_analysis_idempotency_conflict` / `asset_profile_analysis_already_adopted`；取消竞争409 `task_state_changed`；写忙耗尽503 `asset_profile_analysis_write_busy`。认证/配额/计费/目录失效/SSRF 沿既有错误，不转成功默认值。

实施阶段最小充分验收：

- DTO/schema：三分类、空/全空、UTF-8 32KiB及2,000rune边界、重复/未知键、尾随JSON、数组错误输出、控制字符；未选存量数组保留、选中显式替换。
- 准入：越权、旧版本、成品冒充原图、无ready/尺寸、通用operation伪造、replay/deltas旁路、未知/无图模型、超大小及重路由；拒绝前无上游调用/预扣。
- 终态和投影：成功/失败/超时、取消先胜/完成先胜、旧lease迟到、重复取消；所有详情/列表/SSE/诊断无原文/密钥/canvas_ops，拒绝原地retry。
- 原子性：字段子集、空选、其他定义/原图/title/category/kind/payload/所有表现/原媒体TaskID/声音/Prompt/Note保持、所有关联项目revision一次、各写点故障全回滚。
- SQLite文件WAL两连接/两服务、PostgreSQL两实例分别验证同key、异字段集、异task同base、人工编辑、删除及取消竞争；锁序、完整重试、CAS、唯一约束、task/版本收据原子性。不能用单连接内存库或SQLite代替PG验证。
- 生命周期：任务存在时恢复/分页，清理后历史结果及必要重放，继承收据不误判、保留键防注入、来源阻止删除、物理删除失败对应证据。
- 真实组件/路由：三分类入口、textModel同步、缺原图、未知禁选、类型差异/冲突、断网、关闭不取消、账号隔离、费用提示、明暗/键盘/滚动。

沿既有 project_reference_image、repository task lease、project-asset-image-modal 与 reference-image API 测试补专项。实施时执行相关 Go app/repository/handler 和 Bun 测试、必要 build；PG 要实际测试库。无收费授权只用受控 provider mock，不能宣称真实视觉质量、供应商取消或付费端到端通过。

## 8. 历史实施次序与批准记录

以下P1–P3/C2及P4/P5措辞记录对应阶段的范围与批准，不撤销已有成果或复查记录；其中“仍需完整恢复才能完成5.2”“不进入最终Verify”等退出限制已由顶部最新裁定替代。当前最终Verify按主功能与保守删除合同执行，未完成恢复工作在后续change保持未完成。

### P3剩余恢复：新设计获准，实施另批

用户已解除“暂保留当前范围”，批准先完成剩余恢复方案设计与独立审查。[剩余恢复审批稿](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-full-recovery-design.md)比较Windows与Linux证据，建议先验证C2-A单operation控制域能力；现有C1及媒体补验已交付，原句柄丢失unknown的缺口未被豁免。C2-B产品并发隔离/schema、正常启用、环境权限及P4/P5分别保留审批边界。本稿附件仍是待批准的实施范围，不改变5.2验收或将候选原语标为产品通过。

### P3待审批附件：ffmpeg控制与恢复

用户仅批准设计细化。[P3方案](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3-ffmpeg-design.md)列明候选平台控制、启动门/崩溃证据、隔离测试和验收标准；推荐先审批P3-A能力验证，结果明确后再审批P3-B产品接入。未证明全体写者停止时保留unknown，不变更普通MP4格式，不将P1 playback升级可信。当前不实施、不操作业务库；P4/P5未批准，5.2保持未完成。

### P2 已批准附件：本地跨进程锁

用户已明确批准按此最小范围实施P2并执行隔离测试。范围、临时库/真实子进程矩阵、平台与部署验收门见 [P2跨进程物理锁方案](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p2-lock-design.md)。复用已安装文件锁库与 P1 单表，仅新协议本地 Go 原件 writer 可凭锁及 fence 恢复；旧 P1、playback、远端和旧数据不升级为可信。公告和ReplaceAssets保持保守拒绝恢复，不提前修改任务5.2完成状态，不扩大P3–P5或授权业务操作。

### 已批准P1增量：生产者操作与物理结算

关联任务5.2，用户明确批准最小私有producer操作记录、业务失败与物理结算分离及临时库测试，细化合同见`docs/openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-recovery-design-refinement.md`。在物理写入前冻结完整对象身份，删除准入、物理执行前与最终完成均检查同身份全部未决操作。业务failed/ready不能代替物理停写证明，后次成功不能覆盖旧unknown，旧行不得默认回填settled。P1保留既有远端业务成功发布，但远端物理状态保留unknown；尚不实现完整新发布协议。必要前向迁移代码仅在临时库验证，不操作业务库。

本地跨进程控制、ffmpeg实际写入控制、新远端协议/provider限制和旧数据维护不在P1批准范围；P1通过不等于5.2或最终Verify通过。

顺序为合同/解析测试 → admission与资源事务/终态 → 收据/CAS/两DB并发 → 真实assets UI/恢复 → OpenAPI、http-api.mdx、backend-database.mdx JSON合同、features及pending-test。详细实施计划留给 Build，不在本阶段修改产品代码、勾选任务或提交。

上述“本阶段仅确认设计、Build配置待选”为初始阶段历史；当前配置沿用full/zh-CN/autonomous/tdd/standard/current，按收敛合同完成最终Verify。回退可以关闭分析入口，不删除采用版本/收据，也不退化为自动覆盖。
