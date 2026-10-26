# 当前范围收敛与验收映射

## 最新人类裁定

2026-10-24用户明确同意：“收敛本 change 范围，把完整生产者恢复、远端存储结算和旧数据维护拆到后续 change，保留现有安全阻断和已完成成果。按 Comet 更新范围与验收合同，完成资产图片分析主功能的最终 Verify，不操作业务库、不 push。”本记录是这一裁定的合同映射，不是缺失能力的通过证明。当前任务5.2的ID、base-ref、其余任务完成标记及全部独立审查历史保持。

## 当前保留的合同

核心合同不变：三类资产从已保存参考图分析；服务端视觉模型准入与来源授权；可见字段schema、安全投影；人工逐字段审阅/采用；CAS与原子幂等；取消/恢复；历史baseline真实类型；媒体版本内身份及原媒体任务溯源。真实付费视觉质量、供应商取消与费用不在无付费授权的受控mock验收内。

5.2改为“分析来源保护与保守素材删除”，保留所有已完成成果，不回退严格引用保护或提前删除资产历史：

1. 所有既有素材删除入口含purge、管理端和闲置清理检查业务引用，分析任务/草稿及历史来源同样受保护，有引用拒绝并提供来源。
2. 只有完整身份、必要变体、引用及生产者结算证据满足现有安全条件时才允许物理清理；已受理的删除意图保留资产、历史及未完成资源，物理失败/部分完成不宣称整批完成，不恢复ready，不提前清理最终业务记录。
3. 共享alias、worker身份/引用/租约复检，以及P1操作登记与业务/物理状态分离、P2限定本地锁恢复、P3-B发布令牌与事务栅栏均保留；后次成功不得洗掉旧unknown，迟到Start/发布/业务写回不得复活删除中或已删资源。
4. unknown、旧对象无可信记录、未结算playback/远端义务继续拒绝物理清理和最终业务记录清理。保守阻断是当前合同的预期结果；不将其描述为完整恢复或保证所有对象最终可删，不因范围缩小放松证据门。
5. 保留Windows C1有限实现、媒体补验、C2-A私有账本/环境设计与原批准审查记录；不启用正常产品入口、不新增schema、不实施新的OS控制或维护，不操作业务库、既有服务或收费模型，不push。

## 迁出工作与承接落点

| 后续change | 未完成工作 | 继续保留的边界 |
| --- | --- | --- |
| [add-resource-producer-full-recovery](../../../add-resource-producer-full-recovery/proposal.md) | Linux C2-A具名VM/namespace/UID/权限与真实OS验证，C2-B正常产品接入/并发隔离，Windows全部原句柄与控制者丢失后的完整恢复 | VM候选未创建、未绑定，OS验收NOT RUN；当前设计PASS不等于能力PASS；实际环境与产品实施仍需具体审批 |
| [add-remote-storage-physical-settlement](../../../add-remote-storage-physical-settlement/proposal.md) | 具名provider端点的全部attempt、session、版本、parts及迟到写入可靠结算与产品接入 | 本地HTTP/SDK反例不替代真实云端；凭据、测试桶、权限、预算及操作分别确认，不默认改变provider能力 |
| [add-legacy-resource-evidence-maintenance](../../../add-legacy-resource-evidence-maintenance/proposal.md) | 历史manifest、未知缩略图归属、可信停写来源、隔离副本dry-run与精确维护 | 不扫描业务库、不猜key、不回填settled、不真实删除；维护对象与具体操作仍须审批 |

这三项按Comet建立独立change，只承接尚未完成的验收；已完成代码、设计及测试留在原位置并由新proposal引用，避免重复实施或重置原审查轮次。创建后续change不代表批准其执行。

## 当前验收证据与核对点

下表指向真实实现和既有记录，供最终Verify核对当前源码及实际运行结果。这里不新运行测试、不把测试函数存在写成通过，也不将旧失败/skip删掉；历史成功证据以其报告范围及冻结输入为限。

现有扩大筛选曾有11项旧生命周期fixture缺少P1生产者证据而失败，这些仍属于保留安全功能的回归，不能以完整恢复迁出为由排除。最终Verify须在临时库实际核对，必要时最小修订fixture使其满足真实生产准入，保留原失败/重试/共享对象等业务断言及unknown负例；不得批量伪造settled或放松生产守卫。尚未完成该核对时，不宣称5.2或最终Verify通过。

| 当前要求 | 实现/专项核对点 | 既有记录及最终Verify责任 |
| --- | --- | --- |
| 合法分析、schema/准入/投影、取消、采用/CAS/授权/baseline/媒体身份 | app/project_profile_analysis*.go与专项、repository/project_profile_analysis.go，真实前端profile-analysis组件/API专项 | [verification-report.md](verification-report.md)、原1–3/5.1/5.3任务证据；核心字段与并发要求不变，最终Verify按现状核证 |
| 分析来源的引用保护与竞争 | `TestProfileSourceDeletionAdmissionRace`；resource_reference及reference_admission调用链 | 原SQLite/PG来源竞争及owner专项记录保留；不能用普通历史引用代替本项来源测试 |
| 所有入口含purge的引用阻挡、失败与部分完成保留记录 | app/resource_delete.go、resource_deletion_worker.go、repository/resource_deletion_intent.go；`TestDeletionLifecyclePurgeProtectsCanvas`、`TestDeletionLifecycleFailureRetainsWholeAsset`、`TestDeletionLifecyclePartialObjectsRetryPreservesRecords` | 原四项补修与扩大回归记录；最终核对必须使用满足当前producer证据合同的fixture，不能将旧无operation的成功删除断言当通过 |
| 共享alias保护及迟到发布/引用阻断 | `TestProducerP1SettledCanonicalAliasRetainsBytes`、`TestProducerP3BCanonicalTombstone`、`TestProducerP3BAssetHistoryAndReferenceAdmission`；P2发布/恢复回滚专项 | [P3-B实施记录](producer-p3b-implementation.md)、P1/P2原记录；受控控制器只证明事务性质，不冒充OS停写证明 |
| unknown/旧无证据/playback/远端未决持续拒绝 | `TestProducerP1LegacyRowsHaveNoStopEvidence`、`TestProducerP1AcceptedRequestCommitsAfterFailure`、`TestProducerP1DeletionRechecksInterruptedOperation`、`TestProducerP2UnsupportedObligationsStayUnresolved`、`TestProducerP3BRejectUnownedDomainAndNoControl` | P1/P2/P3-B限定隔离与独立审查记录；必须断言未错误清物理对象/记录且未伪造settled，不要求将这些对象成功删除 |
| 真实应用界面与构建 | 真实React路由、受控mock的三分类入口/成功/恢复/取消/安全失败/403/409，明暗/焦点/窄屏/滚动；相关Go/Bun专项及构建 | 原4.1/4.2证据保留，不替代真实付费上游、现有服务部署或业务库迁移；最终Verify记录当前复用或重验依据及限制 |

## 完成判断

5.2只在当前收敛合同的真实证据与独立审查满足后，经Comet按原ID验收；本文件不自行勾选。迁出项保持“未完成/后续验收”，不标pass。13/13仍须最终Verify和集成审查，未通过不得归档。归档/本地合并按届时有效授权处理，本次无push或业务库操作授权。历史文档中“完整恢复未完成所以禁止最终Verify”的旧阶段退出限制由本次明确裁定替代，安全栅栏及真实失败记录不替代、不删除。
