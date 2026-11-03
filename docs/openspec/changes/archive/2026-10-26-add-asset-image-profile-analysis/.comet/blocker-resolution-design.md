# 阻塞解除设计草案

状态：用户已明确选择方案A，批准统一严格引用保护和两阶段删除，连同媒体版本内唯一、专用详情baseline修订实施。具体持久化实现由执行者依据现有模型选择最小可靠方案；需要保存资产级删除意图和物理对象完成状态，允许必要schema迁移代码及临时库测试，不授权操作现有业务库。此前九项问题及取消诊断均关闭、标准复查1/1历史保持；此次设计修订作为新工作包独立审查，不重置原发现轮次。

## 1. 取消诊断：现有合同内修复

recordTaskDiagnostic在status仍queued/running时收到可信phase=cancellation/code=task_cancelled，应保留取消诊断；不得提前改变status影响CancelTaskIfStatus CAS。增加真实API取消后的持久化diagnostic断言，RED观察invalid_result，最小修改判断后GREEN（10.062s）。保留原typed error的账务/取消判定，安全文本仍固定。新增独立审查单独记录，不改前轮次数。

## 2. 媒体身份：推荐版本内唯一，保留原任务溯源

备选：继续阻止有媒体资产采用不满足目标；改写媒体TaskID破坏溯源，不采用。推荐移除全局UNIQUE(task_id,role)，保留UNIQUE(asset_version_id,role)及任务查询索引。模型tag与新增前向迁移同时调整；已有57版本，实施前核对最新迁移编号，不能改历史checksum。临时SQLite/真实可用PG分别测试旧库升级、新库、重复迁移、再次AutoMigrate不会重建全局唯一。

必须同时修正依赖全局唯一的消费者：原图任务注册在目标asset锁内重查task/resource归属，工作流按目标version/role注册并校验重放；图片候选采用优先校验持久化adoptedVersionId，不能把后续复制表现当作原采用版本；回退查找须确定性且验证目标/资源。既有task/resource“是否注册”存在性查询可保留。并发注册仍需数据库CAS和版本内唯一兜底，不能只删除索引。

此处放宽的是无DDL约束，仍不新增profile表列。只写迁移和临时库测试；不运行现有业务数据库迁移、不重启服务。存在复制记录后不能直接回旧schema或重建全局索引。

## 3. 历史基准：推荐扩展专用详情，不建通用历史API

ProfileAnalysisView详情增加可选baseline对象；在用户/项目/asset/task授权后读取binding.baseVersionId并验证version.AssetID。仅投影角色appearance/physique/clothing/props，其他两类description/prompt，保留原JSON类型及missing/null差异。不返回referenceImage、内部receipt、其他definition。不在列表/通用task/diagnostic输出baseline。

详情无法读取/损坏基准则baseline缺失且不可采用、明确原因，不能以当前definition替代。UI优先真实服务器基准，同时单独显示最新current与task.source。task已清理仍404，仅已有收据支持exact replay，不重建草稿。测试跨资产伪造、数组/null/missing、重新打开旧基准和保留键不泄漏。

## 4. 删除生命周期：已批准方案A

已经确认的技术事实：三个outbox入口（detached/admin/asset）都在物理删除前删Resource；asset入口还先删Asset、版本和表现；显式purge还故意忽略普通task/canvas引用。仅将Resource设deleting无法满足“保留素材及历史”的更强要求，也不能默默保留强制purge语义同时声称符合仓库规则。

### 方案A（推荐）：所有素材删除统一为严格两阶段

有项目、画布、任务等业务引用则拒绝，返回来源，包括原“清空/强制清除”入口；无引用后短事务锁定Asset/Resource，设删除中并保留资产、历史与资源记录，禁止新增引用/读取为ready。outbox执行并在必要物理对象全部成功后原子清理相关记录；失败保留记录与作业，用户可见删除中/重试状态。此方案改变现有purge公开行为，且需要明确资产级删除意图/归组的持久化办法，不能假装只增加一个Resource状态即可。

共享物理对象按provider/endpoint/bucket/key精确归组；任何保留alias存在则不删物理对象，删除中aliases不能互相永久阻塞。worker重新核对引用/identity，legacy job安全失败，lease条件更新防迟到worker。多对象允许部分已物理完成但记录保留待重试，不回ready；thumbnail/playback等变体需逐一纳入成功条件。SaveResource、upload completion、playback completion须条件写，避免旧worker复活deleting或已移除行。网络删除不放入长数据库事务；引用写路径也必须锁和检查ready。SQLite需要先取写锁、完整事务BUSY重试，不能声称SELECT FOR UPDATE生效。

### 方案B：只修资源记录保留，资产删除另拆

在已有outbox引入Resource deleting、状态栅栏、共享对象归组完成和失败保留；保留现有Asset purge策略并单独建立后续变更。改动较小，但不满足仓库完整素材删除约束，本次2.3与发布仍然阻塞，不能当作风险已被用户接受。

用户已选择A，确认“原清空/强制清除也受业务引用保护，并延后删除资产历史记录”的行为变化。方案B仅作为未采用方案保留。按A落实详细状态/数据合同和实施，不涉及执行真实删除或生产迁移。

## 验证与范围

正向替换现有两条已知缺陷复现测试；媒体溯源/原采用重放/迁移幂等、物理失败保留完整记录/部分批次/alias/旧worker/late reference、详情白名单均需真实专项。PostgreSQL环境不可用时明确未验证，不用SQLite代替。保留全部其他会话改动，不收费、不提交、不归档，不启动后续三项来绕过第一项阻塞。
