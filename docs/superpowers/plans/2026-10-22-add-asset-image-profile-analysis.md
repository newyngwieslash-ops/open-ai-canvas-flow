---
change: add-asset-image-profile-analysis
design-doc: docs/superpowers/specs/2026-10-22-asset-image-profile-analysis-design.md
base-ref: c9656bc9ce9aa805f61e2e6107bbbe8e279231da
archived-with: 2026-10-26-add-asset-image-profile-analysis
---

# 资产识图档案实施计划

## 最新范围与退出合同（2026-10-24）

用户已明确批准收敛本change：保留分析/审阅/选择采用、CAS/幂等、权限与恢复、历史baseline、媒体版本身份及全部已完成成果；5.2改为来源引用保护与保守删除验收。有引用拒绝并提供来源，物理失败/部分完成保留资产、历史、未完成资源与持久删除意图，共享alias保持安全。保留已实现P1/P2/P3-B等栅栏；任何unknown、旧无证据、未结算playback/远端义务继续拒绝物理清理及最终业务记录清理，不承诺本项完成所有生产者恢复或所有对象最终可删。

完整生产者恢复、远端结算、旧数据维护分别迁至[完整恢复change](../../openspec/changes/add-resource-producer-full-recovery/proposal.md)、[远端结算change](../../openspec/changes/add-remote-storage-physical-settlement/proposal.md)、[旧资源维护change](../../openspec/changes/add-legacy-resource-evidence-maintenance/proposal.md)，迁出不标为通过，不授予后续VM/OS/云端/旧库操作权限。P1–P5、C1、C2-A全部历史设计、批准和审查记录保留，不重置、不重做已关闭包，不启用新产品入口。本轮不新增schema，不操作业务库、不push或发起收费模型调用。

按[范围收敛及证据映射](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/scope-convergence.md)核证当前合同，保留5.2原ID与未勾标记，完成实际验收后由主会话经Runtime记录，再完成最终Verify及唯一集成审查。下方旧实施安排及“5.2未完成/不能进入Verify”均是对应历史阶段记录；当前退出门槛以上述最新合同及delta spec为准，移出能力不再阻塞本项。base-ref保持不变。

## 最终Verify范围内修补

最终Verify首轮发现的当前范围修补：旧测试fixture须补真实同步local生产者闭合，保持unknown拒绝和原引用/物理失败/并发断言；不得排除失败取得绿灯。唯一最终集成初审IMPORTANT F1由现有`UpsertAsset`事务内版本核对修复，真实profile adopt v2后普通旧payload/缺head应409且无写入，当前head及未版本化同步继续成功。修复仅既有核心CAS边界，保留此前审查历史，最终只做同一reviewer唯一1/1定向复核，不创建第二轮全量审查或扩大为同步架构改造。

最终调用方核对补发现：合法历史镜头任务的媒体登记被共享消费者的当前revision等值门一概拒绝。保留owner/instance/引用/删除和ready-selected陈旧竞争检查；仅将已知历史结果构造成stale且未选中，并允许其经过原镜头版本归属核对后幂等登记。不得取代当前选中产物、修改当前step/instance进度及输出或推进镜头版本。原真实失败及双库DirectRechecks作为修补证据，不将相邻脏树回滚为旧实现。

## 历史实施与批准记录

P3-C Windows C1 最新授权已明确：按已审查最小范围实施三列 schema、临时库迁移、隔离测试及独立审查。实现和验证记录见 [C1 实施记录](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3c-c1-implementation.md)。仍不启用正常产品入口、不操作业务库、不下载/替换编码器；P4/P5 与 5.2 边界保持。以下仅设计/待批准的表述为批准前历史。

C1 最小实现、可执行隔离矩阵及独立唯一复核现已交付；SQLite/独立PG/联合回归/后端full build通过，初审R1及PG升级测试缺口已关闭。后续不重做本包；媒体/浏览器、全控制者丢失的完整恢复、正常产品启用和P4/P5仍按实际缺口与授权推进，5.2不勾选。

P3-C 最新工作为设计审批：[最小范围与验收](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3c-design.md)。建议先 Windows C1 隔离接入；三列候选 schema、真实 OS 控制均待批准，当前不实施。P3-B 不重做，P4/P5 和 5.2 边界保持。

当前 5.2 后续：用户已批准 P3-B 产品事务接入与临时库测试，P3-C 平台控制仍待批准，见 [接入方案](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3-product-integration.md)；P4/P5 具名端点、旧对象证据及授权条件见 [验收前提](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p45-acceptance-prerequisites.md)。P3-B 不新增 schema、不启用未经验证的 OS 控制、不操作业务库；实现先记录真实 RED/GREEN，再独立审查，不重置既有审查历史。5.2 保持未完成。

<!-- comet-task-authority: docs/openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/tasks.md -->

P3-B已完成批准范围、临时库验证和唯一1/1独立定向复核，[最终证据](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/producer-p3b-implementation.md)。后续恢复不重做P3-B；P3-C/P4/P5仍按各自具体授权推进，5.2不勾选。

用户确认 autonomous、tdd、standard；保留 current 分支及首版未提交实现。只实现本项，其他三项独立推进。不提交、迁移生产数据、重启现有服务或调用收费模型。规范及正式设计为合同，以下不复制任务完成清单。

## 工作包 A：后端分析与采用（顺序实施，独立审查）

<!-- comet-task-ref:386e7408-e624-4b15-ba6d-93c40058bc12 -->
1.1：在现有项目资产 app/handler/service 层新增分析创建及 DTO；来源必须为基准版本原图，系统/逻辑视觉文本模型入队前和执行前校验，内部标记阻止通用任务伪造。复用任务队列、配额、预扣，不建立 Agent 通道。先写非法来源/权限/能力及合法请求测试并记录 RED，再实现 GREEN。

<!-- comet-task-ref:0234eed3-710b-4924-8048-a837fd2cb449 -->
1.2：纯解析器严格限制分类字段、重复键、类型、UTF-8字节与rune预算；任务成功结果和各公开投影只含安全草稿。原始流/失败草稿不可从任务详情/SSE/诊断绕过。先解析和泄漏回归，再接完成事务；同步 OpenAPI。

<!-- comet-task-ref:6673aac0-dcdb-419f-87f0-f0fb17ec1ec0 -->
1.3：目标过滤在LIMIT前，keyset恢复包含终态；本operation逻辑取消、拒绝原地重试、迟到完成状态栅栏。资源锁及删除复检覆盖任务来源，保留其他任务行为。使用受控provider测试，不联系真实上游。

<!-- comet-task-ref:212b116b-8fb2-4e87-a023-034e7d050c70 -->
2.1：服务端从草稿取selectedFields，单事务复制定义、媒体原任务溯源、声音及其他表现；同步所有关联项目revision及任务采用投影。保留键不可由普通写入口注入。依赖1.1–1.3安全结果。

<!-- comet-task-ref:c00e4076-1084-4bf2-a096-6bc4ff4c1659 -->
2.2：先重放后CAS，收据与请求摘要作用域固定，冲突不重发模型；历史原始采用收据提供必要重放而不重建草稿。用SQLite文件WAL两连接与故障注入验证；有可用PG测试环境才运行对应真实并发，缺环境必须报告未验证，不能勾完成。

<!-- comet-task-ref:6f4fbeb4-d541-44f6-aee6-746e5b51d89b -->
2.3：新增来源与收据被现有资源扫描识别；删除竞争与物理失败保留记录需实际断言。若旧Outbox语义与要求冲突，记录具体证据与最小修复边界，不顺手重构无关存储系统或伪称通过。

后端允许范围 backend/** 中本功能及直接调用链；排除用户 cloud_agent_user_errors*/outbound* 改动。使用独立 project_profile_analysis*.go/test 等承载协议，避免继续膨胀组合根。专项 `go -C backend test ./internal/app ./internal/repository ./internal/handler -run 'TestProjectProfileAnalysis|TestProfileAnalysis' -count=1`，必要旧参考图/任务回归及 `go -C backend build ./...`。测试名以实际新增为准，报告完整命令与结果。

## 工作包 B：真实前端（合同冻结后可与后端并行）

<!-- comet-task-ref:9b9fe1d3-192c-448e-b120-4c1a20790c9b -->
3.1：projects API DTO与http封装，页面私有分析弹窗及角色/视觉资产入口。textModel与model同步，服务端可信模型筛选；缺原图先复用保存原图流程。差异展示保留历史JSON类型，默认不选、未知禁选。先API与组件RED再实现。

<!-- comet-task-ref:8a2d9225-be08-407b-a0fd-a2a71f036336 -->
3.2：带user/project/asset scope的恢复/详情查询，2秒活动轮询；关闭不取消，显式取消说明费用不保证退还。采用重试保留原field-set/key，409不换基准、不重新分析；切账号忽略迟到结果。采用后刷新服务端与本地投影，区分已保存和刷新失败。

前端允许范围 web/src/pages/projects/detail、services/api/projects.ts、相邻测试；不改用户generation-error文件。专项Bun测试真实组件，`bun run --cwd web build`。不生成独立HTML，不更改全局模型偏好。

## 已批准阻塞解除增量（2026-10-23）

用户选择 A，所有删除入口均受业务引用保护；无引用后保留资产、历史、资源及持久删除意图，直到必要物理对象删除全部成功。允许必要前向迁移代码和隔离临时库测试，不授权操作现有业务数据。之前九项审查问题与取消诊断修复已关闭，保持原审查历史。

<!-- comet-task-ref:00ee168d-8df3-44f0-911a-37143f843b56 -->
5.1：媒体工作包独占 models_project.go、媒体索引迁移及注册/重放消费者。去掉全局 task/role 唯一、保留 version/role 唯一与溯源索引；目标资产锁内复检，候选采用稳定返回最初采用版本。测试先证明旧索引阻止合法复制及重放错误，再验证新旧库迁移、AutoMigrate 和并发。其他工作包不同时编辑 migrations.go/schema.go。

<!-- comet-task-ref:d2a5060f-e3d0-477d-b65f-38ad0f9b2fcd -->
5.3：基准工作包独占 profile analysis 详情/采用、前端弹窗与 API、OpenAPI，使用新专项测试文件。详情返回已授权历史版本的分类白名单字段，保留 JSON 类型与 missing/null，缺失或损坏禁止首次采用；列表与通用任务不返回基准。复用当前值刷新与持久重放，不允许历史基准替换当前值。

<!-- comet-task-ref:8b46534f-fd49-45f1-9bb5-977bdc972a33 -->
5.2（当前收敛验收）：核证既有删除入口/队列/worker与引用写入的安全行为，不继续扩建完整恢复协议。保留Asset/version/representation/未完成Resource及意图，覆盖分析来源、引用阻挡、失败/部分成功、共享物理alias/变体、租约复检和迟到业务写回拒绝；unknown、旧无证据、playback/远端未决必须拒绝物理清理及最终业务记录清理。P1/P2/P3-B等已完成栅栏保留，C1有限实现不启用正常产品入口；无证据对象允许持续阻断，不要求本change最终可删。网络I/O不持有数据库长事务。以真实实现/隔离测试及独立审查核证，API/数据/存储文档同步由主会话整合；不增加schema、不放松产品守卫或改业务数据以迎合缩小范围。旧fixture若不满足P1准入，须实际复现后最小补齐真实生产证据，保留原断言与unknown负例，不排除保留功能的失败回归。

删除5.2在独立初审发现新的实际写入竞态后，拆成同一任务的三个明确工作包（不新增产品范围）：核心删除/变体/物理身份墓碑由原删除包负责；引用生产者事务栅栏由独立实现包负责，沿canvas/project/普通任务/lease/style/voice/workflow/creation/tools/workbench/settings的真实写入口补校验，复用生命周期锁并保持通知上下文和锁序；前端包负责deleting类型/解析、已受理后保留状态、权威完成对账、管理列表和引用选择禁用。共享repo媒体文件待原媒体包完成后移交写入包。UI只显示真实删除中和自动重试说明，不伪造作业错误或物理重试按钮。新发现和修复统一进入5.2一次定向复查，不重置轮次。

三包均以当前未提交实现为基线，不回滚已有工作；只补未完成内容。新增风险分别独立审查，修复后定向复查最多一轮；最终集成审查仍留给 Verify。SQLite WAL 双连接为必要证据，PostgreSQL 不可用则明确未验证。

## 用户追加授权：四项删除安全缺陷修复

用户在上述删除复查阻塞后明确要求继续完成四项P1；本轮是该授权下的追加修复，不重置此前任何审查轮次，也不扩大为其余三个产品功能。仍关联任务5.2，沿用autonomous/TDD/standard/current。

- 核心包：按真实嵌套外观schema提取头像引用；冻结/登记迟到转码的派生播放身份，使生产者清理遵守同一共享对象与墓碑规则。负责resource_deletion_intent.go、video_transcode.go及必要资源状态方法；先以漏扫和共享别名破坏回归证明RED，再补实现。
- 引用包：盘点普通任务完成/结果/日志、Ark绑定、模板、镜头修订和raw Create/Save等真实发布路径，补事务准入或提供持续保护的测试证据；删除扫描覆盖工作台文档、创作批准画布及同一准入集合。每类验证writer-first拒删、delete-first拒写，不用测试数量替代覆盖矩阵。
- 共享文件串行移交；引用包不编辑核心包的删除意图/转码文件，核心包不编辑公共引用扫描/任务写入文件。保留另一会话storyboard所有者锁代码，不通过删除锁或削弱测试消除整合失败。
- 整合后由独立审查者针对这四项及引入风险做追加定向审查，保留原删除复查1/1历史；本轮最多再做一次修复后复查。生产者崩溃恢复、未知物化缩略图和既有分镜锁整合边界单独如实记录，不把四项修复等同整项产品验收。
- 仅临时库/临时文件验证，无业务删除、现有数据库迁移、既有服务重启、收费调用、提交或归档。

## 两项扩大回归整合修复

四项安全缺陷已全部独立关闭后，用户明确要求解决剩余两项扩大回归。先复现当前代码，补齐关联测试真实Project夹具；竞争测试按生产支持的owner外层→生命周期→行锁顺序进入，而非先开生命周期事务再申请新owner事务。保留真实独立连接、两种获胜顺序和失败无写入断言，不靠放宽锁协议或改成串行测试通过。

若实际生产调用方也存在同类倒序，需先定位最小入口再协调修改；不覆盖其他会话未完成的owner协议。专项重复/竞态检查、原四项加ReferenceAdmission扩大命令与后端build，并由独立审查者核对测试仍证明原安全性质。此为用户授权整合修复，保留四项已关闭审查及原历史；不代表自动完成所有产品验收或获得提交/业务操作授权。

## 集成、文档和交付

<!-- comet-task-ref:b4de8857-df1b-4634-b346-bd28692a811b -->
4.1：只对真实应用可控mock测试，先确认现有服务版本/用户数据隔离；不污染当前项目、不未经授权重启。浏览器能验证则检查主题、焦点、滚动及成功/取消/失败/冲突；不可安全接入mock时明确阻塞，组件测试不能替代浏览器验收。

<!-- comet-task-ref:c0ff2e2c-e4b9-46de-b51a-c1704573f1cf -->
4.2：后端包专项、前端专项/build及旧首版回归；API、数据库JSON、features、pending-test增量同步，保留首节用户存储修改。独立审查高风险后端及前端写路径，修复反馈一次定向复查；Verify负责唯一最终集成审查。

## 证据与退出

### 历史：P3仅设计，实施前审批

<!-- comet-task-ref:8b46534f-fd49-45f1-9bb5-977bdc972a33 -->
本轮交付 producer-p3-ffmpeg-design.md，独立设计初审及最多一次定向复核；不执行探针或产品测试。建议P3-A仅在.local专属临时环境验证Windows/Linux控制证据、普通MP4与临时账本竞争；工具/权限缺失如实记录，不擅自配置特权环境。P3-A及后续P3-B均须另获审批，P3-B具体文件/字段/部署条件依据实验结果再定。P4/P5不扩，5.2不勾选，不进入Guard或最终Verify。

### 历史：用户追加批准P2，本地Go原件物理锁

<!-- comet-task-ref:8b46534f-fd49-45f1-9bb5-977bdc972a33 -->
执行已审查的 producer-p2-lock-design.md：复用flock及P1单表，真实local入口标新protocol；事务外取得canonical锁，Start/Finish及同事务发布CAS；恢复与stage完整回滚，本地worker锁内Prepare/remove/Finish。公告与ReplaceAssets保持保守拒绝，不扩大P3–P5。先建真实子进程RED，再实现及Windows/Linux临时SQLite与隔离PG矩阵；环境缺失如实pending。产品与测试由同一实施者负责，环境验证另包只管理临时环境，主会话负责文档/Runtime；完成初审及最多一次定向复核，不重置设计或P1审查历史。完成P2仍不勾5.2，不提交或部署。

### 历史：用户追加批准P1，最小生产者操作与结算事实

<!-- comet-task-ref:8b46534f-fd49-45f1-9bb5-977bdc972a33 -->
本包对应5.2未完成子范围，用户明确批准最小producer操作记录、业务失败与物理结算分离及临时库测试。实现前核对所有实际writer及删除准入/最终完成/同identity别名路径；优先一份私有记录与现有生命周期事务，不采用P2跨进程锁、P3转码改造、P4远端协议或P5维护。先以真实受控writer结果未知仍可删除的行为建立RED，再接入持久记录、可信结算与unknown阻挡。普通成功返回不能自动消除SDK多请求未知义务，旧数据不得默认回填settled。临时新旧库迁移/幂等、调用写入失败保留、身份别名、后续重试不掩盖旧unknown、删除并发需专项证明。产品改动归同一实施者串行整合，主会话仅文档与Runtime；独立初审及最多一次修复后复查，不重置其他包历史。仅完成此包不勾5.2。

每个实施任务记录真实RED与GREEN，RED必须来自待实现行为而非加载错误。实现者不勾任务；主线程按唯一ID核对测试和独立审查后记录完成。PG/真实页面/物理删除失败证据不足的复合任务保持未完成。无用户提交授权不commit，不能为通过阶段检查虚构已提交或跳过验收。
