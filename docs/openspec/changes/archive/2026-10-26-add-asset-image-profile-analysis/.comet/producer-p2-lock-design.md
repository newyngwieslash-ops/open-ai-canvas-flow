# P2 本地 Go 生产者跨进程物理锁：已批准实施范围

状态：用户已明确批准“按此最小范围实施 P2 并执行隔离测试”。承接已完成 P1；Build 12/13，5.2 未完成，verify pending，未归档。本文件是正式设计第 8 节的实施附件，不覆盖 P3–P5 审批边界。以下验收矩阵是要求，实际结果另行记录，不预填通过。

独立设计审查已完成：初审两项缺口（发布原子性、遗漏的 stage 调用者）已修订，唯一一次定向复核 PASS，历史见 `.local/profile-producer-p2-design-review.md`。此结论仅表示可提交审批，不是运行验收或实施批准。

## 1. 最小目标与排除项

只覆盖同一主机、同一可信本地数据目录、遵守新协议的 Go 同步原件 writer：普通上传、失败重试和 task-media 写入。使新操作在写入前、写入中及写完尚未记账时进程退出后，可以凭同一 OS 排他锁取得和数据库 fence，安全终止该操作的后续写入权，再由既有删除流程处理。恢复不是发布成功；不能把部分文件或缺失成功凭据的文件标 ready。

不改媒体格式、不控制 ffmpeg、不推导子进程死亡、不做远端请求终态或 provider 限制、不接管旧无证据对象。不新增公开 API、管理页面、通用锁服务、后台全库恢复扫描、依赖版本升级或 schema 61。P1 的 protocol/proof/fenced/physical_state 字段足够区分本轮操作。

## 2. 代码事实和选择

- `app/resource_producer.go` 现为 Begin → Start → writer → Finish；具体 local writer 在 `resource_store.go` 同步打开、Copy、Close。task-media 复用 `storeResourceObject`，应在共同边界改一次。
- P1 `producer-accounting-v1` 不持跨进程锁，所有这种旧操作仍按 P1 证据规则处理；不能仅因升级后拿到锁就恢复它们。
- `repository/resource_deletion_intent.go` 的 stage 当前先拒绝 pending，再核对 settled；Prepare/Finish 复查完整意图。因此只给 writer 加锁不能解除崩溃 pending，也不能只在 worker 阶段恢复——必须有删除准入前的受保护恢复路径。
- 删除 worker 目前 Claim → Prepare → remove → Finish。P2 将本地物理执行区覆盖为取得锁 → Prepare → remove/retained → Finish → 解锁。租约过期不能替代物理锁。
- `go.mod` 已有 `github.com/gofrs/flock v0.8.1` 和 `golang.org/x/sys`。优先复用现有 flock 的排他 TryLock，不引入新库。每次获取创建独立 Flock 实例；不能共享一个实例让其“已锁”快路径把并发调用误当重入。依赖声明如需改为直接使用只改 indirect 属性，不升级版本。

## 3. 锁身份与支持范围

锁目录建议为数据根内固定的 `.resource-producer-locks/`；文件名复用 P1 canonical physical identity 的 SHA-256，不用 resourceID/userID。同对象跨资源、`x`/`./x`、Windows 大小写，以及资源/播放路径等价形式不能分到不同锁。不同物理 namespace 不错误合并。规范化逻辑只有一个来源，提取/导出已有纯函数，不复制到 app。

锁文件与目录不随对象删除，禁止正常清理器 unlink、rename 或重建。每次获取核对可信根、普通文件和固定路径；符号链接、junction/reparse、硬链接及运行中目录替换不作为受支持的别名机制。发现身份变化或不可确认时返回错误并保留记录。锁不能防御拥有数据目录写权限的外部恶意替换者，部署权限与不替换目录是明确前提，不能声称一次路径检查消除了 TOCTOU。

本轮验证目标是 Windows 本地 NTFS 与 Linux 本地文件系统；容器须共享同一实际数据根及锁目录。NFS/SMB、跨主机、多挂载映射和不支持可靠锁的介质不纳入已证明支持范围；错误时不能回退进程 mutex、Redis TTL 或“锁文件存在”。不静默改变既有远端 provider。部署到任何现有服务、调整挂载、重启或切换存储均另需授权。

OS 依据：Linux `flock` 是协作锁，关联打开的文件描述，网络文件系统行为依环境变化；Windows `LockFileEx` 的锁释放可能在进程退出后延迟，必须以真正获取成功为准，不能以 PID 消失为准。来源：[Linux man-pages](https://man7.org/linux/man-pages/man2/flock.2.html)、[Microsoft LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)。这些文档不证明本项目所有 writer 已遵守协议。

## 4. 新操作协议和顺序

新 protocol 建议 `local-go-lock-v1`，只由经过收窄的真实 local 原件入口在 Begin 原子创建时登记。任意 callback、仅 Provider=local、playback 或远端不得获得此标记。不要运行时把旧 P1 operation 升级为新协议；历史记录原样保留。

正常路径：

1. Begin 短事务提交 reserved。对重试保留所有旧尝试；旧 unknown 仍不被洗白。
2. 事务外取得对应 OS 锁。失败/取消不得触 writer；reserved 保留为可审计义务，不靠超时结算。
3. 锁内另开短事务重读 live operation、身份、fence、资源状态并执行 Start CAS；失败不得打开目标文件。
4. 退出事务后执行实际同步 Go writer，确保目标句柄关闭；锁持有至 Finish 持久化完成。Finish 失败保留 writing，不伪造 settled。
5. 发布成功仍要求本 operation 未 fenced 且业务写入确实完整成功。token、fenced、完整物理身份和成功证据的校验必须与 ready/failed 状态更新处于同一个 owner → lifecycle → row 短事务 CAS 内，不能拆成 Check→Save 两次事务；包括存储配额检查/发布失败后的写回。恢复先胜时旧快照更新必须失败，失败保存也不得复活 deleting/已删除行。只收窄新本地协议的提交路径，不泛化改远端发布协议。
6. 解锁/Close 错误向上传播，不将失败返回解释为已解锁。恢复者仍必须实际取得锁。

物理锁获取一律不在数据库事务或行锁内；数据库网络/磁盘阻塞不会被包装成超时停写证据。批量锁按 canonical identity 排序去重，使用非阻塞 TryLock；任一忙则释放本轮已取得的锁，返回当前冲突/重试路径，不无限等待、不部分 fence。物理锁内不能反向等待 `storageMu`；现有 storageMu 调用点须保持一致外层顺序或在进入物理区前释放，禁止递归重入。

## 5. 受限恢复和删除接入

只在既有授权删除请求/清理候选流程内按需恢复，不新增全库扫描。恢复与物理 remove 是两步：恢复持锁只记录停写事实，实际删除继续走引用、权限、共享对象、账本及 worker。

推荐两段准入：事务外读取候选身份并取得全部本地锁；进入原有 owner/lifecycle 短事务，重新构建权威资源/历史/别名集合并检查引用；集合新增或身份变化时整笔回滚、释放锁并有界重取，不在持有 DB 锁时追加物理锁。直接 repository 调用没有受保护恢复能力时继续保守拒绝，不因调用方传 bool 即跳过守卫。

仅当全部相关未决操作确属 `local-go-lock-v1` 的 original、身份匹配、没有 P1/旧/远端/playback 未决义务，且持有同一锁时，才允许在短事务 CAS：fenced=true，physical_state=settled，outcome=failed，proof=`local-lock-recovered`。同事务将对应崩溃 pending 资源改为失败以进入现有 stage；此失败只表示成功无法证明。所有引用/归属/完整集合检查及 stage 任一步失败，恢复和 stage 一并回滚。不得 fence 同物理身份的其他活资源以促成当前删除；存在这类别名的未决尝试则返回冲突。

reserved 但迟到的 writer 在恢复者释放锁后可能继续拿锁；其 Start 必须重读 fenced 并拒绝。writing 时进程仍活着且持锁则恢复失败；进程退出后只有取得锁才能恢复。写完、Finish 已 settled 而业务仍 pending 的窗口也须处理：凭锁和新协议 fence 后走失败/清理，不推断业务成功。已正常 ready 且 settled 的资源走既有删除合同。

删除 worker 在本地锁内重新 Prepare，不能使用锁外快照直接 remove；持锁至 Finish 或失败处理完成。同一 job 租约被另一 worker 接管时，新 worker仍需取得锁并复查 lease，旧 worker失去 lease不得写完成记录。仍保留幂等缺失、部分批次、活别名 retained 及墓碑保护。Playback 如有未决义务仍拒绝；给删除路径同名锁不等于解决 ffmpeg。

### 实施中的合同细化

锁能力实现放在 repository，以便事务内部验证真实能力并沿 GORM 上下文传递；获取仍仅在 app 的数据库事务外完成，事务内不得等待物理锁。空本地身份集不访问本地锁目录。

操作授权不使用 `created_at` 排序。Begin 在原事务内撤销先前已结算 P2 操作的发布权，同时保留其物理结算证据；旧 P1 unknown 不被升级，恢复产生的 fence 仍禁止旧 token 发布。

删除完成事务在全部记录更新后、提交前释放锁。释放错误令事务回滚并保留对象、意图、资源和作业；释放成功但提交失败时，先前已提交的 pending 墓碑继续阻止写入，closed 能力不能用于重试，后续 worker 必须重新取得锁。释放故障测试通过受控错误注入检验该合同，不声称制造真实内核解锁故障。

## 6. 最小预计改动面

| 范围 | 必要变更 |
| --- | --- |
| app/resource_producer.go、resource_store.go、task_media_storage.go | 共同真实 local 写入口、锁持有、协议标记、发布前 token 验证；保留远端行为 |
| app 内一个小锁 helper | 固定目录、独立锁实例、有界获取、批量释放和身份检查；不建通用锁框架 |
| repository/resource_producer.go | 复用身份算法，新协议准入/恢复 CAS；拒绝旧操作升级 |
| repository/resource_deletion_intent.go 及现有 stage 调用边界 | 权威集合二次核对、受保护恢复与 stage 同事务；不能直接在现有事务中等待物理锁 |
| app/resource_deletion_worker.go 与删除编排入口 | 本地 Prepare/remove/Finish 持锁，所有调用者接入审计 |
| 临时库/子进程测试、存储和数据库专题 | 新协议字段语义、支持介质、失败保留行为；不新增表 |

此为责任范围而非承诺固定文件数。当前 stage 调用者按以下策略处理，不能遗漏已在外层业务事务中的路径：

| 现有调用边界 | P2 最小处理 |
| --- | --- |
| resource_reference.go:DeleteAssetsAndResources（通用素材/资源删除） | service 事务外预取锁；原 owner/lifecycle 事务内验证锁覆盖并恢复+stage；直接 repository 调用无能力则拒绝恢复 |
| resource_cleanup.go（detached 清理）、admin_storage_delete.go | 对应 service 外层同样预取锁，复用同一合同；不得在已有事务内补锁 |
| announcement_image.go:UpdateAnnouncementWithImage / DiscardAnnouncementImageDraft | 本轮保留无恢复能力的保守拒绝；正常已 settled 删除仍经受锁保护的 worker，不为了清公告而自动接管 pending |
| repository_assets_canvases.go:ReplaceAssets | 本轮保留保守拒绝，不重构其外层替换事务为恢复流程；失败仍按既有事务整体回滚 |

实施前再枚举实际调用者以发现并发改动。上述保守路径不能描述为已完成崩溃恢复，也不得在 stage 内偷偷取 OS 锁。若要扩展这些入口的自动恢复，需补具体事务设计并审批。若需改变 P3–P5、公共产品策略或扩大 schema，同样停止审批，不借“接线”扩展范围。

## 7. 临时库和真实子进程测试计划

全部在测试创建的临时目录、SQLite WAL 文件和临时对象进行。用 Go test 子进程/管道屏障驱动真实 writer 和删除入口，不用同进程 mutex 或两个连接冒充跨进程；每个子进程独立打开 DB 与锁。deadline 只防测试挂死，不作停写证明。仅终止测试自己创建并确认 PID 的子进程。

| 场景 | 必须观察的断言 |
| --- | --- |
| 两进程/同进程不同 goroutine 争同一身份 | 只有一个实际进入物理区；不同身份可并行；不能共享 Flock 实例假重入 |
| reserved 后迟到启动，恢复先胜 | 恢复提交 fenced；旧进程拿锁后 Start 被拒，目标字节不变 |
| 持锁写中，删除先尝试 | 返回忙/冲突，原记录与字节保留；不以等待时间结算 |
| 写入部分字节后杀测试 writer | 新进程实际取锁后才可 fence/settle；不得 ready；经既有 worker 完成清理 |
| Close 后 Finish 前退出、Finish 后发布前退出 | 两个窗口各有断言，能安全清理但不自动发布；旧发布 CAS 失败 |
| 发布校验与实际更新间暂停 | 以真实事务屏障证明恢复不能插入同事务校验与更新之间；发布先胜/恢复先胜均无旧快照复活，包含失败写回 |
| 原子恢复/准入失败 | 结算 UPDATE、stage 插入或引用检查失败全部回滚，无一半 fence、一半删除；锁正常释放 |
| 别名与候选集合变化 | x/./x及Windows大小写同锁；活别名字节保留；候选新增需回滚重取，不能漏锁或取消别人的写入 |
| worker 退出/lease被接管 | 两worker不同时remove；新worker取得锁并复查，Finish失效lease拒绝；重试幂等 |
| 旧 P1/无记录/playback/远端 | 即使能取锁也不能恢复；后次新操作成功不洗白；P3–P5行为不扩大 |
| 锁设施异常 | 目录权限、锁文件身份变化、已发现替换/非法链接、获取或释放错误均保留记录；不能回退无锁路径 |
| 批量与锁顺序 | 反向资源输入排序后无死锁；忙时不部分提交；引用/部分对象失败/两阶段记录保留回归 |

Linux 和 Windows 均须真实执行跨进程矩阵，不能以交叉编译替代。PostgreSQL两独立进程的同样数据库竞争至少补准入/恢复/发布与删除先后、回滚和别名；只允许专门新建的隔离测试实例/数据库，环境不存在则标未验收，不能借用业务 DSN。NFS/SMB不在本轮验证结论内。

## 8. 验收与实施批准门

实施验收需要：关键反例真实 RED→GREEN；两平台跨进程和临时 SQLite 通过；隔离 PostgreSQL 关键事务矩阵通过；相关 P1/引用/别名/删除回归与完整后端构建通过；独立审查无未解决关键问题，standard 初审和最多一次定向复查历史保留。任何平台或矩阵缺环境则该项 pending，不用文档审阅代替运行证据。

部署验收另有前提：全部访问该数据目录的 writer/删除 worker 使用同一协议与锁目录；旧二进制混跑、旁路维护工具、不同锁目录不得被称作安全恢复。当前不获准重启/部署，因此即使临时环境通过也不宣称现有服务已具备该能力。

当前批准的只有上文 P2 最小实现及隔离测试；P3 ffmpeg、P4远端终态/provider能力、P5旧数据维护仍未批准。P2通过也不自动勾选5.2；完整必要对象与恢复验收、最终 Verify 和之后交付仍另按有效授权进行。
