# 资产图片分析最终 Verify

> 归档公开版仅以具名占位符脱敏临时实例标识；同名占位符指同一原实例。原始值保留在文内引用的本地证据中，历史失败、跳过、未完成状态及验证结论不变。

状态：**PASS**。完整实际检查、唯一最终独立复核及临时库收尾通过；Comet Verify Guard实际exit0，已核定`verify_result=pass`。

## 范围与用户裁定

用户明确批准收敛当前change：完整生产者恢复、远端物理结算、旧对象维护迁到三个后续Comet changes，保留既有成果和unknown等安全阻断。当前合同为最新proposal、delta spec、技术设计、tasks及[范围映射](../../openspec/changes/archive/2026-10-26-add-asset-image-profile-analysis/.comet/scope-convergence.md)。原13个任务ID、base-ref与审查历史保持；移出不表示那些能力已实现。

后续提案分别为[生产者完整恢复](../../openspec/changes/add-resource-producer-full-recovery/proposal.md)、[远端物理结算](../../openspec/changes/add-remote-storage-physical-settlement/proposal.md)、[旧对象证据维护](../../openspec/changes/add-legacy-resource-evidence-maintenance/proposal.md)，仅open待办，未进入实施。无业务库、真实云费用/删除、既有服务重启或push操作。

## 完整性、正确性与一致性

| 现行规格要求 | 实际实现与核证入口 | 最终状态 |
| --- | --- | --- |
| 分析来源与模型准入 | project_profile_analysis、专用handler/service别名、任务执行/重路由；ProfileAnalysis/ContractMatrix准入与来源竞争 | 实际检查PASS |
| 可见字段白名单与不确定性 | schema解析、预算/重复键/类型拒绝、安全任务投影及原始流不公开 | 实际检查PASS |
| 字段审阅与原子采用 | adopt、repository事务/保留键/原媒体溯源、真实弹窗/API | 实际检查PASS |
| CAS与采用幂等 | 收据/摘要、SQLite WAL和独占PG双连接并发、owner嵌套与回滚，含F1普通同步保护 | 实际检查PASS |
| 历史基准安全读取 | 专用baseline授权/白名单/JSON类型/missing-null/缺失拒绝，通用出口不泄露 | 实际检查PASS |
| 媒体表现版本身份 | 前向索引迁移、注册/原采用重放与跨版本TaskID保留，含F2历史镜头登记 | 实际检查PASS |
| 来源保护与保守删除 | 引用/alias/租约/身份复检、失败和部分完成保留、P1/P2/P3-B与unknown阻断；旧fixture真实准入证据修订 | 实际检查PASS |
| 取消恢复与资源生命周期 | 取消/完成CAS、逻辑取消账务/诊断、按权限恢复及分析来源删除竞争 | 实际检查PASS |

## 证据与工作区

现行delta spec为8项Requirement、27项Scenario。以下分组覆盖每项场景；实际通过与否以最终矩阵及独审结果更新上表，不将测试存在当作通过：

| 场景组（数量） | 场景及可核验入口 |
| --- | --- |
| 来源/模型（3） | 合法参考图、伪造来源/无能力、可信目录/专用入口：AdmissionAndStrictDTO、ContractMatrixAdmission/LogicalReroute/ExecutionRechecks、API专项。 |
| 草稿安全（4） | 部分不可见、注入/非法结构、类型/长度、原始流禁公开：DraftSchema、ContractMatrixSchema/AllUnknownNotAdoptable、GenericOutputNeverLeaksRaw、ProfileReview投影/持久化/诊断。 |
| 人工采用（4） | 部分字段、伪造采用、旧JSON类型/媒体溯源、保留键：AdoptReplayAndHistoricalRecovery、AdoptionPreservesCompleteState/AdoptionRollbackEveryWrite、ReservedReceipt及真实modal专项。 |
| CAS/幂等（3） | 并发编辑、响应丢失重放、普通同步旧快照：RealConcurrency、ProfileOwner/NestedRetry双库及SyncRejectsStaleAssetHead/AllowsCurrentHeadAndUnversionedMetadata。 |
| 历史基准（1） | 重新打开真实历史baseline及不可读禁止首次采用：BaselineHistoricalProjection/UnavailableBlocksFirstAdoption/Ownership/MissingDoesNotBreakExactReceiptReplay/SourceUnavailableBlocksView。 |
| 媒体身份（2） | 跨版本复制原TaskID、合法历史镜头结果登记：MediaIdentity迁移/并发注册/各消费者重放、原RegisterTaskOutputFromTask历史例、HistoricalWorkflowOutputPreservesProgress及DirectRechecks双库。 |
| 保守删除（7） | 失败/部分完成、alias/迟到引用、P1结果未知、旧无证据、P2本地中断、未闭合范围阻断、后续承接：Deletion/ReferenceAdmission/Worker/Admin/Detached及P1/P2/P3B矩阵；三个后续proposal/spec/tasks严格验证，全部实施任务未完成。 |
| 取消/生命周期（3） | 取消迟到结果、失败/删除竞争、终态赢家：CancelAndBypasses、CompletionWins/CancelCompletionRace、WorkerKeepsBillingAndCancellationSemantics及SourceDeletionAdmissionRace双库。 |

- 输入归属及测试映射：`.local/profile-scope-verification-map.md`。巨大dirty tree含相邻参考图/分镜/同步及用户错误提示/出站配置；按函数/hunk纳入本change实际影响，保留其余改动，不宽暂存/提交/重置。
- 历史后端2313冻结项在范围修改前mismatch0；主分析前端21项直接输入无语义漂移，request.ts仅CRLF差异且Git无diff。浏览器4.1可保留限定既有证据，不能称本轮新浏览器执行。
- 4.1实际真实React/登录态受控mock证据：`.local/profile-browser-mock/acceptance-codex-resume.md`及state-codex-final.json，覆盖三分类点击、采用/刷新恢复/取消/失败/403/409、明暗/窄屏/滚动、键盘与退场回焦点。非独立HTML；不证明真实付费上游或当前业务部署已升级。
- 11项旧删除fixture实际RED：`.local/profile-scope-fixtures-red.log`，app45.116s，缺producer证据被保护拒绝；修订后的原11项加部分重试共12顶层GREEN58.947s，远端unknown/独立HTTP helper双库19.659s，global引用17.785s，均0fail/0skip。四源manifest `a0ca614fb2edf7e92c26793acad4855fe95ac8465a977e89e72ec0d053ec8ecc`，不删除历史失败或放宽生产保护。
- 原handler筛选无匹配测试，不能记为HTTP路由运行通过；app服务边界与真实页面mock证据分开陈述，最终集成审查核对剩余必要接口覆盖。

## 实际结果与失败修补历史

### 首轮失败与范围内修补（保留）

- 任务13/13及Build Guard已通过，完整前后端构建check `5f097b2b-ef7b-4f38-93c2-6eedbade8e42` exit0/full/reused=false，输入快照前后一致；前端Vite11.92s，既有chunk等警告保留。首次PS裸`--`被吞的CLI usage exit70未执行子程序，参数数组保留后正式构建成功，失败记录未覆盖。
- 首轮正式Verify `f4d83e89-d622-4d38-b6b4-0a544b9dcd0b` exit1/full，核心189父子PASS、20 Test fail事件（含父节点）、0skip；3个顶层在缺producer证据时未到原引用/物理失败断言。日志`.local/profile-scope-verify-core-first-failure.jsonl`保留。按IMPORTANT验收缺口自动`verify-fail`回Build，失败次数1，未豁免。
- 三组核心fixture适配保留双库/两获胜顺序/唯一来源/精确引用错误与真实local目录失败；GREEN32.278s，物理失败用例显式t.TempDir后补验3.387s。3源manifest `90387eca160b6218c0d79be8a2a3d5c22dae248f121b3b14e1743f50c97d5566`，不改生产保护。
- Build扩大安全诊断check `116380a4-f696-45f8-a71b-209bcbe79517` 为增量失败，183父子PASS、25 fail事件含package、0skip。日志`.local/profile-scope-safety-first-diagnostic.jsonl`保留；旧admin/worker/引用/竞争/P1repair夹具及一个缺projects表的folder用例需适配。运行中核心测试源和自产lock文件变化，Runtime未接受为稳定输入结果；只用于根因调查，不作最终验收。
- 唯一最终集成初审`.local/profile-scope-final-integration-review.md`为REQUEST_CHANGES：IMPORTANT F1，旧标签页普通素材同步可以回退真实采用后的head。已在原UpsertAsset事务补最小guard，返回409/`project_asset_version_changed`且保留原sentinel。真实采用后经Upsert/Replace提交旧/缺head四场景RED→GREEN；正常head/无版本元数据对照通过，共6父子pass/0fail/0skip。SQLite WAL和独占PG的两入口owner锁回归7父子pass/0skip。报告`.local/profile-scope-sync-head-fix.md`。唯一1/1定向复核尚未使用。
- 调用方附加检查中可选浏览器测试因未配置runner跳过1项，不作浏览器通过证据。一项媒体注册旧测试报`unsupported data`；仅去除本轮四行修补的源码overlay与修前hash完全一致且原样失败，linked-canvas对照通过。此baseline只证明不是F1新回归，不能作为整个change的豁免。
- 后续只读补核将该失败确认为范围内媒体消费者集成问题F2：原测试与HEAD相同，HEAD无新增当前shot revision一刀切门；当前dirty共享storyboard/媒体消费者会拒绝合法历史r1结果。现有MediaIdentity专项未覆盖该artifact分支。已在Build修补为stale未选登记并保留当前step/instance进度及输出，原ready/selected陈旧提升及owner/instance栅栏不放松，空/不存在/外来镜头版本均拒绝。原历史例、新七子例、当前linked对照、媒体身份与F1冻结联合专项全部通过，DirectRechecks双库通过/0skip；源码前后hash相同，`.local/profile-scope-historical-output-frozen-{app,repository}.jsonl`保留。未修改当前登记的既有project revision通知行为；最终Runtime与唯一定向复核仍待完成。
- 六安全测试源manifest `80d2a7e56e7bc2db23870ebd7f69f529199bd50eebae75ece6fac3e027aae99d`已冻结。39顶层完整首跑另发现共用项目表依赖及failed空key无证据的旧删除预期，保留真实RED后两项分别补验GREEN；PG五组双连接含package45PASS/0skip。所有16修改case及两个共用fixture消费者均纳入最终Safety正则，未排除已知失败取得绿灯。详见`.local/profile-scope-safety-fixture-fix.md`。
- 源树零字节lock已精确核实由本轮核心fixture旧空dataDir产生，修正为t.TempDir后仅删除该一文件，未递归清理；记录`.local/profile-scope-owned-lock-cleanup.json`。

### 冻结输入后的正式检查

- 最终完整构建check `3c2a62fe-4ea1-49a4-b5a9-0d9e40aea95c`：exit0/full/reusable=false，执行前后输入均`bc628e12e96c0fdf98b5285731780d4e6576e599d560db806fac896782572307`，changedDuringExecution为空。后端全量build及前端tsc/Vite通过，Vite9.87s，既有chunk等警告保留。结果`.local/profile-scope-build-check-final.json`。
- 最终Build Guard exit0，正式进入Verify；原13项ID和revision保持，13/13，full，历史verify_failures=1、verify_result=pending、archived=false，分支不变。正式报告路径已绑定，当前运行冻结输入的最终完整Verify矩阵。
- 同一最终独审者已完成唯一1/1定向代码复核，独立核F1/F2、各新fixture和新增风险，当前未关闭CRITICAL/IMPORTANT为0；13个fixture与五源hash均一致，仍等待ROOT最终实际矩阵，未签整体PASS。未重新发起全量审查或重置旧记录。
- 正式Verify `8710e44a-2ec5-4559-877e-5228e9f8cf23`实际exit1/full/输入无变化：核心55顶层、236父子pass；安全100顶层、245父子pass，四包均通过（app564.464s），全部状态事件0fail/0skip。但验证入口合并native输出时把一条中文SQL Output错解成GBK并吞掉JSON反斜线，完整解析失败，前端未执行。因此整条check失败，原始坏行与结果保存在`.local/profile-scope-final-safety-invalid-json.json`、`profile-scope-verify-check-encoding-failure.json`及原日志副本，不把部分Go通过改成最终通过。按流程正式verify-fail回Build，失败次数2。
- 编码修复只在`.local`验证入口：UTF8、原生stdout独立重定向，stderr另存；仍严格解析全部JSON且要求零skip。原出错Admin rollback用例最小补验exit0/1pass/0skip，原中文精确保留、stderr0。日志`.local/profile-scope-encoding-probe.log`及`profile-scope-verify-encoding.jsonl`。产品、测试、规格及最终独审修补源均未再改，等待正式重验。
- 编码修复后的完整构建check `c3772053-1c9b-447e-86d5-2ba8832694e3` exit0/full，前后仍为同一`bc628e12…`源码输入，无变化；Build Guard再次通过，Verify失败历史次数2保留。最终验证入口将两组前端先执行，场景及失败条件不变；因前次安全矩阵实际564.464s，为避免接近600s上限，Go单矩阵限时900s，Runtime总限时1500000ms，不重试隐藏失败、不排除场景。最终wrapper SHA256 `A90C9C19B0CFF5DA69385CFA56232D067BDCAA8D224D9E43C328563AD0D74128`，非本地可复用证据。
- 本次前端实际结果：主功能7文件84pass/0fail/0skip，547expect，40.54s；安全相关5文件49pass/3skip/0fail，144expect，4.65s。日志`.local/profile-scope-verify-web-{core,safety}.log`。三项skip为“未acknowledge旧基线的冲突隔离聚合错误”“entity冲突时其余素材/画布继续推送”“dirty entity跳过通用覆盖、仅对齐基线”。独审已逐一核对HEAD同名body原本就是skip且本轮未改，属于通用entity同步交互限制，不能计为通过或作为当前CAS豁免；当前要求的真实采用后旧/缺head单条/批量拒绝由F1四叶和模态409/恢复实际通过证明。没有借skip放宽当前27场景，也不把旧注释当人类接受偏差。

### 最新完整Runtime结果

最终check `b76785c8-8ee2-48a3-87e9-56507740b295`实际exit0/full/reusable=false，前后输入均`bc628e12e96c0fdf98b5285731780d4e6576e599d560db806fac896782572307`、changedDuringExecution为空。全部Go JSON逐行严格解析成功，core/safety的stderr均0字节，保留前两轮真实失败历史。原始结果`.local/profile-scope-verify-check-final.json`，Runtime原日志为本change `.comet/checks/b76785c8-8ee2-48a3-87e9-56507740b295.log`。

| 实际检查 | 结果 |
| --- | --- |
| 核心Go app/repository/database专项 | 55顶层、236父子pass，0fail/0skip；app61.978s、repository33.128s、database33.344s。包括SQLite WAL/专属PG并发及迁移、F1/F2全部新增用例。 |
| 保守安全Go app/repository/database/storage专项 | 100顶层、245父子pass，0fail/0skip；app561.885s、repository10.353s、database1.361s、storage0.894s。保留unknown负例及所有已知失败分支。 |
| 前端主功能7文件 | 84pass/0fail/0skip、547expect，40.54s。 |
| 前端安全相关5文件 | 49pass/3既有skip/0fail、144expect，4.65s。三项限制与独立范围判断见上文，不计通过。 |
| 专属PG前后核对 | systemID `<FINAL_VERIFY_PG_SYSTEM_ID>`、loopback端口 `<FINAL_VERIFY_PG_PORT>`、专属非超级角色/库一致；public_tables/test_schemas/other_clients均0。只用临时SQLite和任务独占PG，无业务库。 |

当前8要求、27场景实际证据完整，proposal目标与delta/技术设计一致；原任务13/13及所有ID、base-ref保留。没有以迁出能力当通过，也没有当前范围验收豁免。未跑后端全套，不能称全库全绿。两项旧Windows媒体恢复目录Sync环境失败的HEAD对照历史保留，未通过放宽产品Sync消除限制；完整OS恢复、真实云结算和旧数据维护保持后续待办，受控mock不证明真实收费模型或现有业务部署成功。

同一最终独审者唯一1/1定向复核最终**PASS**，F1/F2均CLOSED，未关闭CRITICAL=0、IMPORTANT=0，无新增可行动阻塞；报告`.local/profile-scope-final-integration-review.md`保留初审及所有失败记录。独审严格解析最终core1435行/safety14923行，核实际结果与Runtime原日志SHA一致，13个fixture及五修补源hash再次一致。三项既有通用同步skip作为未验证限制保留，不计通过，不构成当前范围的验收豁免。

临时PG已精确收尾：停止前核原data/systemID `<FINAL_VERIFY_PG_SYSTEM_ID>`、专属role/db、PID `<FINAL_VERIFY_PG_PID>`、仅loopback端口 `<FINAL_VERIFY_PG_PORT>`及public/schema/其他client均0；仅smart stop该自建实例，exit0。后续pg_ctl status3，PID/postmaster.pid/listener均消失，端口独占bind成功；离线controldata仍为原systemID且state=shut down。证据`.local/profile-scope-env/postgres-stopped.json`及`profile-scope-pg-environment.md`。启动session `<FINAL_VERIFY_START_SESSION>`已终态，其exit1来自正常停库后启动脚本尾部读取已移除pid文件；独立shutdown exit0与全部停止证据保留，不把wrapper尾部错误掩盖为exit0。未操作其他服务、业务库或真实对象。

最终`comet guard add-asset-image-profile-analysis verify --apply`实际exit0。权威状态：13/13、原revision与所有任务ID保持，`verify_result=pass`、`verified_at=2026-10-24`、`phase=archive`、`archived=false`、`branch_status=pending`；下一步明确为confirm-archive。状态由Guard更新，未手动写pass。结果`.local/profile-scope-verify-guard-final.json`。本次到最终Verify完成为止，归档及本地合并仍待有效具体授权。

## 结论

**PASS**。当前收敛合同的8要求/27场景、13项任务、完整实际检查及唯一独立集成审查均满足，Comet Guard已正式通过。完整生产者恢复、远端物理结算和旧数据维护由三个后续open change承接，实施任务继续未完成并保留原授权边界。原分支/HEAD与混合工作区、审查和失败历史保留，暂存区为空；本次未归档、本地合并、提交或push，未操作业务库/真实云对象/既有服务，未调用收费模型。
