# P3 产品接入候选方案

P3-C 最新最小范围、三列候选持久合同、隔离测试与分级验收见 [P3-C 审批稿](producer-p3c-design.md)。本次仅设计，C1/C2 均未批准实施；P3-B 已交付，后续不重复实施。

状态：用户已批准本文件 P3-B 最小实施、临时库测试及独立审查；P3-C 仍为候选。不是产品能力验收。5.2 保持未完成，12/13、Build、verify pending、archived=false。不授权业务库操作、部署变更或真实删除。下文“可单独批准”保留原审批单位的完整定义。

P3-B现已交付并通过限定验证及独立复核，见[实施证据](producer-p3b-implementation.md)。本文仍保留B/C联合目标以说明尚未交付的C条件，不能把B的测试替身当真实OS控制。

## 结论与证据

复用 P1 操作记录、P2 规范化物理身份/锁和现有 ResourceDeletionIntent/Object/outbox，不新增通用 gate 表。分为 P3-B（产品事务接入，隔离测试）和 P3-C（经能力验证的 OS 控制接入）。B 的 fake controller 测试不能代替 C；仅 B 完成不能验收 P3 或 5.2。

证据见 [隔离结果](producer-p345-capability-results.md) 和 `.local/profile-p3-capability-report.md`、`.local/profile-p3-ledger-report.md`。Windows 同一 Job 在 Terminate 后仍可启动迟到写者，全控制句柄丢失后同名 Job 也不能证明旧代际退出。Linux 当前容器无 cgroup 委派，继承锁 FD 仅验证了特定 ffmpeg 构建；父进程退出或父锁释放不证明全部写者停止。因此不能直接把 cmd.Run 改成 kill+wait 就声明崩溃恢复完成。

## 最小产品范围

仅为新登记的本地 playback ffmpeg 操作引入受控协议。旧操作、未知 thumbnail、远端 writer 仍按现有未决证据阻止删除；不得自动升级旧记录。启动入口包括上传后 maybeStartPlaybackTranscode 和 BackfillPlaybackTranscodes；删除、替换、公告清理等所有共享删除入口必须保持同一安全合同，不能只改 UI 对应入口。

| 位置 | 最小改动及复用 |
| --- | --- |
| `backend/internal/app/video_transcode.go`、`resource_producer.go` | 把实际启动交给具名控制能力；原件读取和 playback 写入均受控；正常结果发布携带 operation/token，并与状态更新原子校验 |
| `backend/internal/model/models_resource_producer.go`、database schema/migrations（仅 C 候选） | B 不修改 schema，沿用已有 operation ID/token 和状态；C 若需要控制代际身份与启动阶段持久字段，必须先列具体字段、默认值、约束及迁移测试再审批，不存 PID 作为终态证明或存凭据 |
| `backend/internal/repository/resource_producer*.go` | 复用 Begin/Start，增加只接受可信停止证据的 fenced 物理结算通道；不放宽现有 Finish 的发布许可 |
| `resource_deletion_intent.go`、`resource_reference_admission.go` | 新受限准入分支先建立停止屏障；所有创建/保存身份检查统一到现有 canonical identity；复用真实权限、全引用重建、owner/lifecycle 锁 |
| `backend/internal/app/resource_deletion_worker.go` | 新协议先处理停止请求，确认停写后取得物理锁，再执行原有 Prepare/remove/Finish；普通 P2 路径不改变为无锁删除 |
| 数据库/存储专题、pending-test | 记录新增合同和未覆盖能力，不宣称旧数据恢复或 Windows 编码器可用 |

B 不引入新的后台服务、依赖或 schema 迁移，仅用已有 operation/token、状态和墓碑实现事务逻辑及受限控制接口的隔离测试；真实控制器尚不存在时不得把新未结算准入分支接通到产品入口。C 如需要独立监督进程、持久字段或部署权限，属于明确的单独实施范围；不能隐藏在 B 中。以下是 B/C 联合目标顺序，其中真实 OS 启动/停止及新代际持久化仅属 C。

## 事务及控制顺序

1. **登记与启动**：沿用 owner/lifecycle 顺序，核对用户、资源 ready/processing、canonical identity 和墓碑。创建唯一 operation 与控制 incarnation；在实际 OS 启动前持久记录启动阶段。每次重试使用新的 operation，不能以复用文件名掩盖未知旧写者。
2. **启动封闭合同**：同一控制能力必须串行处理真实 launch 与 close-launch；持久屏障提交后控制器再次检查许可，且许可检查到 OS 加入控制域之间不能与 close-launch 交错。仅数据库检查后释放锁再 CreateProcess 是不合格实现。恢复时对“已允许但尚未确认加入”的窗口一律按可能已启动处理；不可因无 PID 改成未启动。
3. **提交停止屏障**：在短事务中从当前用户、资产全部版本/表现和真实引用源重新构造对象集合，并按 canonical identity 跨用户重建全部 alias/operation。停止前必须确认控制域涉及的全部可写目标均在此次选择和授权内；有未选中或未授权活 alias 共享该域即拒绝停止，不得先 kill 再补权限核对。原件只读输入的共享关系与 playback 可写目标分别判断：原件共享不授权终止其他用户的 writer，也不得连带删除保留的输入。仅允许已登记的新受控协议进入未结算分支；任何不能证明可安全接管的未知对象仍拒绝。原子创建现有 pending intent/object/job、标记资源/资产 deleting 并 fence 全部相关操作。不能先等待 writer 持有的物理锁，不能在事务中执行 OS kill。
4. **阻止新增引用与别名**：deleting 状态挡同资源引用；pending/completed 对象墓碑必须按规范化物理身份挡所有新别名、重新登记和发布。当前 requireResourceIdentityWritable 的原始 key 比较不足，应在共享 helper 复用 canonical 算法，覆盖 namespace、`./` 与 Windows 大小写等价。实施前枚举全部调用方；helper 存在不等于覆盖已证明。
5. **停止与恢复**：提交后由控制器封闭所有 launch，再停止整个已验证的控制域并收集同代际终态证据。任务重试和多个 worker 可重复请求，必须幂等；丢失/矛盾证据保留 unknown 与屏障。工作进程崩溃后按持久 intent 重入，不依赖内存回调。任何 OS 操作都在 DB 事务之外。
6. **物理结算**：控制域终止且不能再接收写者后，按 canonical identity 排序取得原件和 playback 锁；固定锁顺序避免多对象死锁。重新核对 operation/incarnation、intent、租约、权限、完整引用/别名和全部操作证据，再将该操作物理结算。fenced 操作可报告物理终止，不能发布 ready。证据不齐继续 pending/unknown，不回滚屏障为可写。
7. **发布与删除互斥**：正常结束也必须以 operation/token 在一个事务中完成物理结算与 playback ready 更新；替换当前 Finish 后另行 CompletePlaybackTranscode 的无 token 窗口。若已 fence，仅结算、不发布。Prepare/Finish/finalize 继续检查整个 intent 的生产者证据；全部物理删除成功才删除逻辑记录。

现有 stageResourceDeletion 在 pending/unsettled 检查处提前返回，新分支不能只是删掉检查。现有 Prepare 只接受 pending intent，身份屏障只认 pending/completed object，最小方案沿用这些状态，以未 settled 的操作阻止 remove；无需凭空添加一个无人识别的 intent 状态。SettleDeletingPlayback 不能独立充当停写证据，必须绑定上述受限结算事务。

## OS 控制能力与未解决门槛

| 平台 | 候选接入 | 启用前必须证明 |
| --- | --- | --- |
| Windows | 原子加入 Job 的启动方式，控制器独占 launch/close-launch 顺序，保留可信原 Job 句柄 | close 后所有启动入口不可再加入；后代、控制器崩溃、全部控制者崩溃的恢复策略；同名 Job/PID/active=0 均不可单独证明旧代际终态。全部句柄丢失仍未解决时只能 unknown，不能宣称完整恢复 |
| Linux | 受限委派 cgroup v2，控制域按 operation/代际隔离并禁止逃逸或重新进入 | 实际委派环境中的创建、全子树终止、population 归零、启动封闭及恢复；目录存在/不存在和 boot ID 单独均不构成完整证明。继承 FD 不作为通用替代 |

产品默认如何处理不具备能力的平台（保持旧行为但禁止未知删除、拒绝新转码、或部署新的受控运行环境）需要明确产品裁定，不能通过测试 skip 静默限制能力。在 C 证据齐全前不得自动启用新协议。Windows 还需具有 libx264 的明确 ffmpeg 构建及复验；不能换 mpeg4 降低原有格式合同。环境安装、特权委派和既有服务重启分别需要授权。

## 隔离测试及验收标准

B 实施遵循 TDD，使用真实 repository/service 入口、临时 SQLite 和独立 PostgreSQL；fake controller 只用于确定性停写/错误注入，不作为 OS 证据。

| 场景 | 必须观察到的结果 |
| --- | --- |
| 启动 vs 删除，两进程真实事务争用 | 确认驱动层锁冲突/等待，分别验证启动先赢、屏障先赢；屏障后实际 launch 不被允许 |
| 多 identity、同 key 别名、跨用户和全部引用源 | 用户权限真实校验；完整集合一致、锁排序固定；新增别名/引用/发布均被共享屏障拒绝，不靠 Authorized=true |
| 删除在 reservation/launch 间、写入中、刚完成时 | 无写者逃离控制域；无 remove 与 writer 重叠；迟到 ready 永不复活资源 |
| fence 后正常退出、停止失败、证据丢失 | 只允许有证据的物理结算；失败/unknown 保留资源、文件及屏障，不把业务失败当终态 |
| 每个持久化/OS 边界崩溃，worker 租约过期或重复执行 | 新进程幂等恢复；旧 worker 不能凭过期租约删除；全部操作结算前所有相关原件/playback 保留 |
| C 真实控制器 | 使用真实 ffmpeg/后代，证明关闭 launch、全部停写及崩溃恢复；同时监控真实删除入口和输出写入，不能仅 fakeStop/父 Wait |
| 格式与真实路由 | H.265/MPEG4、含音频/无音频转为既定 H.264/AAC、偶数尺寸 yuv420p、普通 faststart MP4；真实应用资源 URL 的 Range/拖动与完整解码通过；本地 seek 重组不算 HTTP 验收 |

B、C 都须有独立审查。控制者全部丢失、平台主路线或真实路由仍缺证据时明确保留未通过项；不得以受限支持表替换原任务验收。真实业务服务/账号/数据不在隔离测试范围内。

## 下一步最小批准单位

可单独批准 **P3-B 产品事务接入及临时库测试**：仅既有 operation/token/canonical guard/停止屏障与 worker 顺序，控制接口通过测试替身验证，不新增持久字段或迁移，不接通无真实控制能力的未结算准入，新 OS 协议不启用。该批准不包含业务迁移、业务删除、C 部署或 provider 限制。其结果是可审查的安全接入底座，仍不是 P3 完成。

P3-C 应先指定一个实际目标平台，批准其隔离控制环境验证；Linux 明确委派权限及测试目录，Windows 明确可信句柄生命周期/全控制者丢失策略及编码器构建来源。只有证据闭合后才能批准相应产品启用。P4/P5 所需条件和授权见 [最小验收前提](producer-p45-acceptance-prerequisites.md)。
