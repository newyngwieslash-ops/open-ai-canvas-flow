# P4/P5 完成验收所需的最小输入与授权

用户已批准 P3/P4/P5 隔离能力验证及独立审查；本文件不重新请求这份批准。已完成的本地 HTTP、临时 SQLite/文件实验继续有效，证据见 `.local/profile-p45-capability-report.md`。本文件仅列实验之后仍缺少的条件；不执行云调用、业务库操作或维护。

P4/P5 的目标仍是：全部必要物理对象清理完成（或有合法活 alias 保留）之后才清业务记录；每个可能继续物化的 writer/attempt 都必须有停写依据。实验通过并不证明真实供应商终态；获得账户权限也不能补足供应商的语义保证。5.2 保持未完成。

## 1. P4：先选实际端点，再准备真实隔离验证

不要求同时开通四家账户。维护方先提供当前实际支持/使用的端点清单，可按一家具名端点逐个闭合。每个验收结论必须限定产品、版本、桶类型与配置；AWS 的结论不能外推到任意 S3-compatible。未覆盖部分仍列未验收；若要排除这些部分或改变原合同，须单独作产品裁定，不能由实验者默认缩小 5.2。

以下均为非秘密摘要；不在聊天、报告或日志粘贴访问密钥、签名 URL、会话令牌。实际鉴权材料通过批准的本地凭据注入方式供测试进程使用，报告只记录凭据引用名与权限摘要。

| 最小输入 | 内容与用途 | 不足时 |
| --- | --- | --- |
| 具名端点 | provider、厂商/服务版本、区域、API endpoint、桶类型；兼容服务附其官方合同 | 不推定 AWS 等价 |
| 测试归属 | 专用测试账户/项目的非秘密别名、独占桶或明确隔离的前缀、责任人 | 不借用业务桶/默认配置 |
| 对象模式 | 版本控制启用/暂停/未启用、对象锁/保留策略、生命周期、复制、加密要求及是否允许清理所有测试版本 | 无法删除的版本/副本不得称清理完成；不擅自关闭保护 |
| 测试边界 | 精确 bucket/namespace/key 前缀、允许的操作和最大对象数、单对象/总字节、最大尝试次数与截止时间 | 不做全桶扫描或无限重试 |
| 费用 | 总费用上限及币种、请求/存储/流量预算，预算耗尽时保留的证据与清理安排 | 无付费调用授权则不启动真实云实验 |
| 可验证合同 | 指定 API/版本的启动、终止、重试、提交排序与列举语义官方链接；未公开保证可由厂商提供可审查说明 | 增加权限或重复试验不能替代缺失语义 |
| 凭据边界 | 测试身份、权限摘要、凭据引用及有效期；谁可撤销/恢复；配置变更时怎样定位原账户/位置 | 凭据失效保留阻塞，不换当前桶重试 |

### 按能力申请最小 API 权限

下表是所需能力/API，不是可直接部署的统一 IAM 策略。各家 action 名称、资源级约束须按选定服务核验后生成并审查；不授予管理员权限。某些 list API 只能桶级授权时，应优先专用测试桶，并限制实验只处理本次 manifest 的精确对象。

| 实验能力 | 需要允许的 API 能力 | 限制 |
| --- | --- | --- |
| 正常及失败上传 | 本次测试对象 PUT/表单上传；选定方案需要时 Create/InitiateMultipartUpload、UploadPart、Complete | 仅合成媒体；无真实用户 payload |
| 恢复未知会话 | 列举未完成上传（含全部分页）、列指定会话 parts、查询精确对象/版本 | 仅在官方支持时使用；Kodo 未知 uploadId 的完整发现能力尚待证，不能以猜测接口替代 |
| 结算与清理 | AbortMultipartUpload；删除本次生成的精确 key、必要版本和残片 | 无版本清理权限则版本模式验收 pending；不可清同前缀非本次对象 |
| 观察配置 | 读取选定桶的版本/保留策略等影响测试的配置，或由管理员提供可信快照 | 不自动改 bucket policy/版本模式/生命周期 |
| 权限失败测试 | 对本次测试身份临时撤销指定权限并恢复，或使用预制受限身份 | 只影响测试身份；变更与恢复另有明确授权 |
| SDK 重试/网络故障 | 测试进程内故障代理与 SDK attempt 观测；不记录 Authorization/签名查询串 | 不改生产网络；不能以客户端断开证明服务端停止 |

最小建议先用一个专用测试桶及一组独占随机前缀，执行与候选 adapter 对应的模式。若版本桶是实际支持范围，则必须另有版本桶覆盖；不以无版本桶通过替代。配置变更测试优先切换测试配置引用至第二个自有测试位置，不改业务配置。

### P4 必须拿到的证据

1. **准入与所有 attempt**：写前 durable 操作记录绑定精确身份；SDK 重试、重新签发、迟到启动都在同一可核对证据链。只跟踪最后一次成功响应不合格。
2. **init 响应丢失**：证明服务端可能已创建、客户端未拿到 ID 的会话仍能恢复，且扫描之后不会有旧 init 新出现。完整分页和空列表本身不是启动屏障；需供应商终态合同或可证明的协议设计。
3. **提交/终止竞争**：覆盖 part、Complete、Abort 并发、成功 body 丢失、重复请求及完整响应解析。恢复完成后已接收请求不能再物化；客户端 context 取消、进程死亡、TTL、HEAD404、DELETE204 都不能单独作证。
4. **物理穷尽**：匹配本次所有会话、版本、parts及必要派生物；delete marker 不算 payload 删除。处理同 key 多 attempt、多 session、共享 alias，不能误删其他归属对象。
5. **恢复安全**：权限不足、配置切换、证据不完整或写证据落库失败时保持未决；临时 SQLite 与隔离 PG 验证事务回滚、重复恢复、worker lease、墓碑与迟到发布。真实云测试不能替代这些产品层验证。
6. **实际产品接入**：对最终 adapter 实现独立审查，并在指定端点运行正常成功、故障恢复、拒绝路径及清理核账。仅实验模型或 HTTP helper 测试不能签发产品完成结论。

有些次序无法在黑盒云端确定性制造；必须明确哪些由故障注入实测、哪些由正式合同支撑、哪些仍未知。有限次未观察到迟到写并不证明其不可能发生。

现有公开依据说明为什么还需要这些条件：AWS [Abort](https://docs.aws.amazon.com/AmazonS3/latest/API/API_AbortMultipartUpload.html) 允许在途 parts 仍成功，[Complete](https://docs.aws.amazon.com/AmazonS3/latest/API/API_CompleteMultipartUpload.html) 要读完整 body；OSS [Complete](https://www.alibabacloud.com/help/en/oss/developer-reference/completemultipartupload) 明确断开后继续处理；COS [Abort](https://www.tencentcloud.com/document/product/436/7740) 有更强的 parts 请求失败表述，但仍要覆盖未知 init 与已完成对象；Kodo [Init](https://developer.qiniu.com/kodo/6365/initialize-multipartupload)、[Abort](https://developer.qiniu.com/kodo/6367/abort-multipart-upload)、[Complete](https://developer.qiniu.com/kodo/6368/complete-multipart-upload) 的 ID 生命周期不单独提供全部历史 attempt 证明。本轮未新增浏览核查，以上沿用已冻结实验查证来源。

## 2. P5：先建立可信旧对象证据，再谈接管

不默认实际业务存在问题行，也不默认没有。当前临时测试只证明无证据 ready/failed/pending、未知 thumbnail、跨用户/namespace alias 会保留；没有实现可信旧 manifest 接管工具。

最小输入优先由维护方提供已授权的非秘密摘要或脱敏导出。若需执行者读取业务库/存储 inventory，则需新增精确只读授权；先在隔离副本验证，不直接修改原始系统。

| 证据项 | 最小字段/来源 | 不可替代依据 |
| --- | --- | --- |
| manifest 来源 | 生成者/维护责任人、生成方式和版本、部署版本映射、来源记录引用及完整性校验、导出快照边界 | 任意手写 JSON、自报 trusted=true 不能自动可信；不强制新建签名体系，须有可核验来源 |
| 业务归属 | resource ID、owner scope、variant、相关资产/引用/alias、导出版本或并发校验值 | 文件名相似、大小/MIME、同前缀不能证明归属 |
| 物理身份 | provider/endpoint/bucket/namespace/key；必要的 version/session/part 清单；本地根与规范化路径的可信映射 | 当前 storageSettingID、当前默认桶不能替代历史位置 |
| 历史生产者 | 所有可能写该对象的 writer/二进制版本、操作/attempt 与重试来源、已知派生规则及部署区间 | 当前无生成器不证明过去无字节 |
| 停写来源 | 每个 writer 的可信终止/不可再启动证明；远端已接收请求的终态；已实行的准入屏障 | 旧 ready/failed、PID 消失、锁空闲、租约过期、运维口头“应该停了”不充分 |
| 完整性范围 | 证据覆盖哪些对象/时间/尝试，哪些未知；可信 inventory 或历史清单与物化规则的对应关系 | 不完整清单不能写成对象不存在证明 |

本地旧 writer 未遵守新锁时，拿到 P2 锁不产生接管权；若需要停服务、终止旧进程或冻结写入才能建立控制，应列出精确实例与影响后单独批准。远端旧模糊 PUT 不因本地进程全部停止而已结算。未知 thumbnail 不从 original/playback 的规则类推；只有可复核历史规则或可信精确 manifest 才能补身份。

### P5 最小分步验收

1. **只读摘要/导出**：限定表/列/资源 ID/时间范围与存储对象范围，排除密钥和用户内容；记录快照边界、行数、完整性和未知项。无授权时由维护方提供，不自行查询。
2. **隔离副本 dry-run**：只生成逐行判定 `eligible / blocked / conflict`、依据、将新增的最小证据与预计清理对象清单；不写业务库、不删除原存储。对可信证据正例、缺字段、来源伪造、跨用户/namespace、同物理alias、未知writer、重复执行、过期快照全部有断言。
3. **隔离执行验证**：在可丢弃副本和合成对象执行接管/删除与失败恢复；使用实际产品入口验证权限、引用、事务、部分失败与审计，不能只测试一个接受 manifest 的纯函数。证明失败不会默认补 settled 或清掉业务记录。
4. **live 维护（若确实需要）**：用户审阅具体 dry-run 差异后，另批准精确资源/对象清单、备份恢复方案、维护窗口、写入屏障、执行人及权限。临执行再次核对身份、停写证据、归属与引用/alias，快照变化即阻塞；数据库成功不等于物理完成。

批准 live 补登记不自动批准真实物理删除、停服务、kill 或业务库迁移；这些动作必须在具体维护批准中分别明确。没有可信身份/停写来源的旧行继续 blocked；不能让维护工具以“管理员确认”替代客观证明。

## 3. 现在无需重问与确需新增的授权

| 状态 | 内容 |
| --- | --- |
| 已批准，可继续 | P3/P4/P5 的隔离能力验证与独立审查；无真实凭据的本地 HTTP、临时库/文件和合成样本实验 |
| 新增：真实云实验 | 选定端点/测试账户/桶/前缀、精确 API 能力、费用与资源上限、凭据注入、测试对象创建及精确清理、测试身份权限故障注入 |
| 新增：业务只读取证（如需要） | 精确只读查询/导出范围、脱敏规则、目标隔离副本与数据保存/销毁规则；由维护方提供已有授权导出时无需再授权执行者查原库 |
| 新增：live 维护（如需要） | 审阅后的资源/对象名单及 dry-run 差异，补登记、真实删除、服务/进程控制、迁移分别列明；不自动推定已授权 |
| 产品裁定（仅当原合同无法达成） | 指定端点能力限制、旧数据处置范围改变、永久屏障/仅逻辑删除等，必须显式修改合同；不能通过勾选任务代替 |

本文件不把“有账户”与“允许实施”当充分条件。产品接入的具体实现应建立在语义证据及实验结果上，继续受 Comet 独立审查和验收门约束；不提前声明 P4/P5 或整个 change 完成。

## 4. 可填写的无秘密样板

```yaml
remote_validation:
  provider_product_version: "待填：具名产品/版本"
  endpoint_region_bucket_type: "待填"
  account_alias: "待填：非秘密测试账户别名"
  bucket_namespace_prefix: "待填：专用测试范围"
  versioning_retention_replication: "待填：配置摘要"
  credential_reference: "待填：注入引用名，不填写值"
  permitted_api_capabilities: []
  object_count_bytes_attempt_limits: "待填"
  budget_currency_and_ceiling: "待填"
  deadline_and_cleanup_owner: "待填"
  exact_fixture_cleanup_authorized: false
  vendor_terminal_contract_references: []
legacy_evidence:
  source_owner_and_export_reference: "待填"
  resource_scope_and_snapshot_revision: "待填：脱敏范围"
  identity_manifest_and_integrity_reference: "待填"
  historical_writer_versions: []
  stop_and_no_restart_evidence_references: []
  unknown_items: []
  authorized_readonly_export_scope: "未授权/维护方提供/待填具体范围"
  isolated_copy_location_reference: "待填：非敏感引用"
  live_maintenance_authorized: false
```

样板字段完整只是准备就绪，不代表证据可信或终态已证明；须经核查、真实实现验证及独立审查。完成所有原要求后仍需最终 Verify，当前不改变 12/13、5.2 未完成与 verify pending。
