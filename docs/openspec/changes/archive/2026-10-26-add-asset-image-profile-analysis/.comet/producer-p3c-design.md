# P3-C 最小实施审批稿

追加授权及实际结果：用户随后明确允许下载ffmpeg，已用私有校验构建完成Windows C1真实媒体/HTTP和原生浏览器播放/seek，详见[媒体补验](producer-media-runtime-results.md)。系统工具未替换，正常入口未启用。下方“不下载/媒体缺工具”为该追加授权前历史；C2、独立监督服务及正常启用未因此获批。

最新授权：用户已批准本稿 C1 最小实施、三列 schema、临时库迁移、隔离测试与独立审查；不操作业务库、不启用正常产品入口、不下载或替换编码器，P4/P5 与 5.2 边界保持。以下“尚未批准”表述保留为设计审查时历史，实施授权以本段为准。

状态：仅设计，尚未批准实施。承接已通过限定验证和独立审查的 [P3-B](producer-p3b-implementation.md)，保留 full / zh-CN / autonomous / tdd / standard / current。任务 5.2 不勾选，Build 12/13、Verify pending、archived=false。本文件不修改原验收合同。

独立设计初审 C1-D1（未启动记录恢复入口）、C1-D2（无删除意图的成功结果恢复驱动）已集中补齐，唯一 1/1 定向复核 PASS，见 `.local/profile-p3c-design-review.md`。结论仅为 C1 有限方案可提交审批，不是运行验收；此前审查历史保留。

## 1. 选择与范围

| 路线 | 可交付能力 | 代价和缺口 |
| --- | --- | --- |
| **建议先批 C1：Windows 原句柄控制的隔离接入** | 将真实 Job 控制接到 P3-B 接口、持久封闭启动和结算；确定性证明丢失证据时保留 unknown | 无新常驻服务；全部句柄丢失且终态未提交时不自动接管，不构成完整 P3 |
| C2：Linux 委派 cgroup 的完整恢复候选 | 研究控制器重启后仍可识别并终止旧代际的控制域 | 当前环境无委派；需指定隔离环境、权限、目标目录和不可逃逸合同后另批，不用特权容器绕过 |
| Windows 独立持久监督服务 | 后端崩溃时原句柄可由监督服务保留 | 新进程、IPC、权限与部署生命周期；全部监督者丢失仍须处理，暂不增加该系统 |

推荐 C1 是有明确上限的实施包，不是把“全部控制者丢失仍 unknown”批准成最终产品行为。若用户希望本次即实现全丢失后自动恢复，应改选有可信持久控制域的方案并先完成环境与协议审批，不能把 C1 加码为隐含监督服务。

仅处理本次新登记的本地 playback ffmpeg，operation ID 就是唯一代际，不复用、不跨机器接管。保留旧 P1/P2、旧 processing、未知 thumbnail、远端 writer 的现有证据边界。只在测试组合根安装真实控制器，正常 Service 构造、环境变量、HTTP 和部署入口均不启用。无新依赖、无独立服务。P4/P5 仍按 [各自前提](producer-p45-acceptance-prerequisites.md) 推进。

## 2. 现有调用链与最小改动位置

已核对真实源码：`resource_store.go`、`task_media_storage.go` 及启动回填调用 `maybeStartPlaybackTranscode`，后者 `ClaimPlaybackProduction -> runPlaybackTranscode -> runH264Transcode -> FinishPlaybackProduction`，当前未安装真实控制器。`playbackProducerControl` 已有 Operations / CloseAndStop / Completion；删除 worker 已在持久屏障后停止、持物理锁后结算。复用这些接口，不另建删除引擎。

| 文件/责任 | 候选改动 |
| --- | --- |
| app/resource_producer_control.go 与一个 Windows 平台实现文件 | 原 Job 句柄、操作代际映射和真实 launch/close/stop/completion；受限启动和恢复接口仅内部可调用；其他平台返回能力不可用，不宣称支持 |
| app/video_transcode.go | 提取最少共享参数，测试安装控制器时走同一上传/回填启动链；原件和输出受 P2 锁保护；正常路径仍保持原协议，不全局替换 cmd.Run |
| repository/resource_producer_control.go | 持久单调启动门及终态回执的 CAS；精确 operation/token；沿用生命周期和引用检查；新 controller 必须通过可信能力验证后才能出现在 Operations 中 |
| model/models_resource_producer.go、database schema/migrations | 仅下节三列的候选增量及前向迁移；只在临时 SQLite / 独立 PG 运行 |
| app/repository/database 专项测试 | 真实进程、真实竞争和临时库迁移；沿用 P3-B 夹具和故障注入，fake controller 只作事务单测 |
| 数据库、存储专题及 pending-test | 区分 C1 有限能力、未验收项和产品默认未启用 |

不改 API、UI、provider 配置、P5 维护器或通用任务调度。审批后若发现必须增加其他持久字段、进程/服务或权限，应先更新方案再请求批准。

## 3. 最小持久合同（明确请求批准的 schema 增量）

P3-B 的“不新增 schema”不被本文件默默解除；C1 提议以下三列，须连同本稿明确批准后才写实现。复用 ResourceProducerOperation 现有 ID、Identity、UserID、ResourceID、Protocol、Fenced、PhysicalState、Outcome、Proof，无新表。

| 列 | 类型、默认和含义 |
| --- | --- |
| control_owner | varchar(36)，NOT NULL，默认空串；控制器每次启动产生的新随机实例 ID，仅绑定原句柄持有者，不是可转移授权或 PID |
| control_phase | varchar(24)，NOT NULL，默认空串；新协议只用 armed / attempted / closed / stopped；空串永不推导为未启动或已停止 |
| control_result | varchar(16)，NOT NULL，默认空串；仅 stopped 可写 success / failed / unknown，表示同次编码结果；未知结果不允许发布 ready |

不新增索引/唯一表，主键 operation ID 已满足精确 CAS。新受控记录必须 owner 非空且 phase=armed；迁移不填充旧记录、不改协议或物理状态。repository 拒绝未知枚举、倒退、owner 替换及非受控协议伪造字段；空默认旧行保持现有路径。schema 注册与正式前向迁移须使用同一字段定义，测试 fresh/upgrade/repeat 和迁移失败回滚，旧行逐列不变。不得把这些列用于记录 PID、Job 名称等可复用值来冒充终态证据。

状态单调：armed -> attempted -> closed -> stopped；armed 可直接 closed；编码正常退出也先 closed。attempted 在任何可能的 OS 创建前提交，表示“可能已启动”，不是运行成功；不对 attempted 自动重试 launch。stopped 只能由可信原域终态或在启动门下证明从未 attempted 的分支写入。control_result 与 stopped 同事务提交，已有物理结算/业务发布仍由 P3-B 事务完成，不能因新列绕过 fence/lease。

## 4. 控制顺序与锁

1. 新 claim 在同一生命周期事务绑定 operation ID、当前控制器 owner 和 armed，并复用 StartResourceProduction 完成 reserved→writing；该 writing 只表示准入，不证明已启动。返回同步后的 writing 快照，受控 run 分支不再次调用 Start；实际创建仍只能在门内 armed→attempted 后发生。事务失败不得启动。这样未启动崩溃也能经可信失败回执走现有要求 writing 的 completion，不放宽旧 reserved/无字段行。真实启动及封闭共用每 operation 的跨进程启动门。优先复用 P2 文件锁实现，以 SHA256(固定域前缀 + operation ID) 派生 64 位十六进制锁标识，与资源 identity 锁隔离；不删除锁文件。拿锁失败返回可重试错误，不以超时/锁空闲作为停写证据。
2. 启动先取原件/输出物理锁，再取启动门；核对精确代际、owner、armed、资源及墓碑。在短事务提交 attempted 后，在仍持启动门时执行原子 Job 关联的创建。不得 Start 后 Assign，不用 shell 拼命令，不启用 breakaway；句柄不可被写者继承或从外部打开后发起 launch。创建失败须以真实 API 结果判定是否根本未创建，含糊失败保留 unknown。命令/路径只由可信资源解析，验证目标位于本次隔离根、拒绝越界/链接替换。
3. 正常运行持原件/输出物理锁；原句柄由当前控制器保留。数据库持久化失败仍保留该句柄，不释放为“已完成”；异常清理也不能假定直接子进程 Wait 已覆盖全部写者。
4. 删除先按 P3-B 完成权限/引用/活 alias 核对并提交 fence/intent；停止不在数据库事务内。CloseAndStop 取得启动门后持久写 closed，再对同一原句柄停止并确认完整域退出。closed 提交失败不得返回停止成功；close 与 launch 串行，关闭后任何重复/迟到 launch 均拒绝。随后写 stopped/result，释放启动门，再等待物理锁进入原 worker 结算。禁止拿着启动门等待运行者所持物理锁，以免与正常完成的锁顺序形成环。
5. 正常完成持物理锁、取启动门，先 closed，再确认整个域退出，结合同次 ffmpeg exit 得到编码结果并持久 stopped；释放启动门和原有物理锁后调用 P3-B completion 通道重新取锁并原子发布。全域未停止或 result unknown 均不能发布 ready。关闭/停止成功并不等于编码成功。
6. 门内 DB 写为短事务，OS API 在事务之外；同一进程并发回调还须同步 map/句柄生命周期。停止只等待 OS 域退出，不等待需要同一启动门的正常完成回调。Job 句柄只有 stopped 已持久后才按正常完成关闭；失败/崩溃必须依靠下表保守恢复。多个 worker 可以重复停止，但只有本代际 owner 的真实句柄或已提交终态记录可提供证明；过期租约不能结算/删除。

Operations 不能枚举 DB 中所有 controlled 行当作能力：只暴露本控制器持有可信原句柄的精确操作，以及受信任写入面与单调 CAS 保证的已持久终态。不新增审计事件表，也不声称三列保存了完整历史。其他 owner 且 attempted/closed 无终态的操作一律不授予停止/结算能力。新的控制器不能凭相同 Job 名称、PID 或 operation ID 创建空 Job 接管。本包信任边界为受控测试账户、目录和后端写入面；任意管理员/同权限恶意进程篡改数据库或复制控制句柄不在已证明范围，不能从普通合成后代测试外推对恶意代码的隔离保证。

### 独立于删除的恢复入口

测试组合根安装控制器后、开始新 claim 和调用 Backfill 之前，显式运行一次内部受控操作恢复；以后每次受控操作失败重入可再次调用同一有界方法，不新增常驻服务。按 ID 游标分批读取新协议且 owner/phase 完整、尚未物理结算的记录，不经 PlaybackPendingVideos，也不恢复 ResetStuckPlaybackTranscodes 的旧重置行为。

- armed：先在启动门内重读精确操作和墓碑，原子 CAS armed→closed→stopped/failed。旧 owner 不改写，恢复者只封闭不夺取 launch 权。该事务可含连续状态检查与更新，不能留下“closed但未知是否曾attempted”的中间提交。待终态提交后才将它注册为 Operations 能力，因此无需先通过删除准入才能关闭未启动记录。
- attempted/closed 且无可信原句柄：只报告 unknown，不注册能力、不启动新 writer、不重写 failed/ready。持久 closed 本身不证明未启动。
- stopped：核验精确代际及字段合法性后提供终态能力。有删除 intent/fence 时走已有删除 worker；没有 intent 且仍符合当前 resource/processing/identity 条件时调用 completeControlledPlayback，Completion 从同一持久回执返回编码结果；result unknown 只可报告业务失败，不能 ready。并发 fence/新引用在 P3-B 事务内仍会拒绝发布；不忽略该拒绝、不强改业务状态。正常启动崩溃恢复必须覆盖这一分支，不能只测试删除 worker。

恢复扫描不启动 OS 进程；恢复所用终态来自受限 controller 写路径，不能接受用户 JSON、外部同名 Job 或测试文件作为回执。终态回执不可覆盖；重复扫描已结算记录直接跳过。

## 5. 崩溃与恢复判定

| 崩溃位置 | 新进程允许的动作与结果 |
| --- | --- |
| claim 未提交 | 不存在有效许可，不能启动；不能凭无行证明任意历史对象可删 |
| armed 已提交、attempted 未提交 | 持启动门和精确 CAS 封闭后，依“所有 launch 先 committed attempted”证明该新协议操作未启动；可保存 stopped/failed；不推广到旧空字段 |
| attempted 提交、创建前或创建中 | 可能已创建；原控制者可用原句柄处理，全部句柄丢失则 unknown，禁止重启同代际或新建同名域证明终态 |
| 已开始写、close 提交前 | 同上；已有 intent/fence 保持，不因进程消失或租约过期结算 |
| closed 提交、stop 前/中/后，stopped 未提交 | 原句柄存在可幂等停止；全部丢失仍 unknown，观察器预持句柄不得借给恢复者 |
| stopped/result 已提交、P3-B 结算前 | 核验不可再 launch 的持久记录与精确身份后重入；成功结果可按现有发布条件完成，fenced 仅结算不发布 |
| P3-B 已结算、remove/finalize 部分失败 | 复用已验证的 outbox/lease/物理成功再逻辑清理，不回退可写状态 |

记录矛盾、缺字段、未知协议、旧 owner 试图续跑、控制/物理锁或数据库不可用均保留对象与屏障。whole-domain exit 和不能再 launch 缺一不可；有限观察期无写入不能替代结构性封闭证明。

## 6. 隔离测试与可执行验收清单

本轮只定义测试，不运行新控制实现、迁移或 OS 实验。审批后的所有样本为合成数据，SQLite TempDir、独占低权限 PostgreSQL、独占测试目录、自产进程；不读取业务配置。每个测试记录源版本/hash、平台/文件系统/数据库/ffmpeg 版本、命令、断言与退出码，保留失败日志。清理只作用本次拥有的精确 PID/句柄、DB/schema/目录，测试前检查归属，测试后核对无残留进程。

| 必测组 | 通过标准 |
| --- | --- |
| 迁移与状态 | 临时 SQLite/PG fresh、旧表升级和重复执行；旧行不自动登记，非法状态/owner 变更拒绝，失败事务不留半条回执 |
| 启动门竞争 | 两个真实进程，以事件屏障制造 launch 先赢及 close 先赢；close 提交后 OS 创建调用计数为零，attempted 仅一次；实际锁争用可观测，不能只 sleep |
| 原子入组与后代 | 真正创建即关联 Job，子孙写者均在原域；无效句柄/权限/当前嵌套环境不满足时拒绝，不能降级为未受控启动；逃逸尝试若成功则该环境验收失败 |
| 停写与迟到请求 | 运行中删除、正常结束竞争、重复 close/stop、故意迟到 launch；真实写者退出且启动门永久封闭后才准许结算/remove，独立写入事件记录证明顺序 |
| 崩溃矩阵 | 上表各持久化/OS边界终止自产控制进程，另起进程调用实际恢复入口/worker；attempted或closed且stopped未提交时丢失全部句柄必须unknown并保留文件/记录；armed例外只能经门内未启动证明和原子封闭，stopped提交后可幂等完成；阴性结论不算完整崩溃恢复通过 |
| 恢复入口可达性 | 新进程先运行测试组合根恢复：armed且无句柄必须先持门原子封闭再登记能力；无intent的stopped success恢复到ready，失败/未知结果只到failed；并发fence只结算不发布；确认没有借用Backfill重置或测试直接改DB代替入口 |
| 权限和并发 | 跨用户、共享 alias、未选中同域对象、旧 lease、旧 owner、替换 token、受控/P2 混合批次；不得无授权停其他 writer；普通 P2 恢复不退化 |
| 故障 | closed/stopped 提交失败、锁获取/释放失败、stop/query 错误、超时、结果丢失、remove/finalize 失败；物理证据不齐绝不删除，原回执不被新结果覆盖 |
| 正常媒体 | 指定构建实际支持 libx264 后，H.265/MPEG4 各含/无音频四组走实际启动链；ffprobe、完整解码确认 H.264/AAC、偶数尺寸、yuv420p、普通 faststart MP4；不换 mpeg4 冒充产品格式 |
| 实际 HTTP | 合成账号/资源、临时库的真实 Gin 资源路由验证鉴权、GET、206 Content-Range/字节一致性、非法 Range；浏览器真实页面拖动/播放再记单独证据，不能用本地 seek 代替 |
| 回归与独立审查 | 重跑 P1/P2/P3-B 和正常 playback 专项、完整后端构建；复核原句柄信任、锁顺序、全部 launcher 与恢复流程，无阻断后交付；保留历史审查，不重置 |

媒体先验：现有 Windows 构建缺 libx264，C1 进程控制测试可用合成 writer，但媒体/HTTP播放不能因此通过。使用用户提供的已具备能力的可执行文件路径及 hash；实际受控启动链必须使用该路径，不能仍经 PATH 调用不合格构建而只在离线矩阵换工具。下载、替换、安装构建或引入特权环境不包含在 C1 自动执行范围。仅编译 Linux 分支或 skipped 测试不算 Linux 运行验收。

## 7. 可审批单位与最终门槛

**建议审批 C1：** 上述 Windows 隔离控制适配、三列最小 schema 与临时库迁移测试、真实自产进程故障测试、实际 service/repository/worker 接入测试及独立审查。允许删除本次合成测试文件/测试库对象和终止自产进程，不涉及业务数据。只在测试组合根安装控制器；正常服务不启用、不重启、不部署。既有工具不足时完成其他测试并明确缺项，不偷偷下载或更换编码器。

**不包含：** 业务库迁移/查询、真实素材删除、P4 云调用/能力限制、P5 inventory/回填/维护、独立监督服务、cgroup 权限变更、系统安装、push/合并/归档。

C1 包交付必须分别报告“Windows 原句柄能力”“全部句柄丢失保守拒绝”“媒体及真实路由”的实际结果。缺编码器可交付已完成代码和证据，但不能称整个 C1 验收通过。C1 即使全部清单达到其有限预期，仍不等于 P3-C 完整完成。

P3-C 完整完成还需：在明确的平台/部署边界证明全控制者丢失恢复（或经用户明确批准改变产品合同）、真实媒体/路由验收、独立审查，以及单独审批正常产品启用方式与无能力平台策略。若采用 C2，下一方案须具体列委派权限、写者不可迁出/重入保障、控制域代际的持续身份与清理规则，再验证；不能仅凭目录存在/消失或 boot ID 宣称证明。P4/P5 仍缺各自证据，5.2 与最终 Verify 均保持未完成。

本稿证据来自仓库源码及已冻结 [隔离结果](producer-p345-capability-results.md)、`.local/profile-p3-capability-report.md`，没有把候选 OS 合同描述为已实测能力。本轮不重新运行历史测试。
