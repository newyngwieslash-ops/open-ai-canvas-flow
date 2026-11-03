# P3 剩余恢复方案：最小审批稿

追加批准：用户已批准 C2-A 验证代码及临时 SQLite / 独占 PostgreSQL 检查，源码限 `.local`；真实 OS 实验仍须第3节具名环境与权限另批。下文“待批/仅设计”为本稿初始审批历史，不阻止本次账本验证实施，不扩大至产品接入或 OS 能力运行。

已交付 [C2-A账本验证子包](producer-c2a-ledger-implementation.md)：最终同源双库矩阵和独立唯一 1/1 复核 PASS。没有 OS adapter 或真实环境/进程能力证明，不能把数据库门修复延伸为 OS 动作互斥；完整 C2-A、C2-B、正常入口及5.2仍未完成。

用户随后要求实施前细化 [C2-A真实OS具名环境方案](producer-c2a-os-environment-design.md)，并确认暂无现成VM。新稿把第3节的宽泛环境合同具体化为待创建独占VM、精确UID/GID映射与分阶段权限、私有文件/数据库及独立OS门；当前只设计和独立审查，不代表环境已创建/绑定或已批准实施。运行时C/W权限与原恢复状态合同保持，bootstrap局部namespace限额等新增环境动作须明示审批。

状态：用户已解除此前保留，批准先完成设计和独立审查，实施另批。本稿关联任务5.2；沿用full/zh-CN/autonomous/tdd/standard/current。已交付的P1/P2/P3-B/C1和真实媒体证据保留。Build12/13、5.2未完成、Verify pending、archived=false，本稿不改变验收合同或平台支持范围。

## 1. 结论和候选比较

建议下一步只审批 **C2-A：Linux持久控制域的隔离能力验证**，先验证原语、权限和全部控制者崩溃后的恢复。当前环境没有可用委派，须先提供本稿规定的独占环境合同；审批代码不等于审批环境配置。能力证明成立后，再提交C2-B产品接入和正常启用的准确范围。不先实现一个已知不足的Windows监督服务。

| 候选 | 实际价值与代价 | 当前结论 |
| --- | --- | --- |
| 普通Windows命名Job加一个或多个监督者 | 活监督者可保留原句柄；引入IPC、身份、服务生命周期 | 全部句柄丢失仍不能证明旧域终态，不可作为完整恢复实施包 |
| Linux C2持久委派域 | cgroup可独立于应用控制进程存在；标准库支持创建时入组；需要受保护环境、角色分离及连续保管 | 可以提出有明确失败条件的隔离验证，尚未证明可接产品 |
| 永久Windows内核对象等新原语 | 可能改变零句柄后的可寻址性 | Job/API支持、额外权限及安全生命周期未证，留作另案研究，不在本次最小范围 |

Windows官方对象模型明确：临时对象在句柄数为零时名称可先移除，指针引用仍可使本体存活。因此Job“最终销毁”的描述不能倒推Open失败等于原成员已退出。加监督者、服务自动重启或重建同名空Job均不补足该窗口。[Object生命周期](https://learn.microsoft.com/en-us/windows-hardware/drivers/kernel/life-cycle-of-an-object)、[Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)

Linux官方允许受委派者在其子树内迁移；kill处理树内进程及并发fork/migration，populated描述当前活进程。它们不自动提供每operation隔离、持续身份或禁止未来进入。本稿以下条款是额外设计义务，不是官方已验证本项目。[cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html)

当前C1仍按已验合同处理：armed可经启动门证明从未attempted并原子封闭；attempted/closed无原句柄且无持久终态保持unknown。C2成功不能写成Windows成功，也不批准限制Windows能力或豁免最终P3/5.2。

## 2. C2-A 最小实施边界（代码/临时库已批，OS另批）

仅在忽略的专属临时目录中实现验证程序、测试账本、合成writer和报告。复用已安装Go1.25、现有Linux编码器及数据库依赖；不引入产品controller、HTTP入口、常驻服务、生产model/migration或部署配置。

验证使用一个明确独占的环境代际、一个活跃合成operation和一个专属writer身份。它证明该代际内控制者丢失恢复的候选能力；不证明多operation共用UID安全，不把单例实验当产品并发验收。辅助writer负责确定性fork/权限/崩溃窗口，真实ffmpeg四种输入另测；两者互不替代。

只清理本次合成文件、临时库对象及精确拥有的控制域/子进程。无业务库、真实媒体删除、云调用、系统安装、镜像拉取、系统权限修改或既有服务重启。没有满足前提的环境时，只交付可审查代码与明确缺项，不运行替代实验或记PASS。

## 3. 实验环境必须先提供的合同

| 项目 | 最小要求与核对方式 | 不满足时 |
| --- | --- | --- |
| 独占范围 | 提供非秘密环境ID、kernel/Go/编码器版本、专属cgroup v2根、测试数据根及责任人；所有路径精确限定，不借业务挂载 | 不启动writer |
| 持续保管 | 委派根及每operation域在控制者全部退出后继续存在；外部管理器不自动删除、迁移或重新创建；根被重建/换挂载必须换环境代际 | 无法核对连续性则unknown，不能重建空目录接管 |
| 控制角色C | 建议实验中采用非initial user namespace内的UID0，映射到非特权宿主身份；仅P/E的CAP_SETUID/GID、Inh/Amb为0，无SYS_ADMIN等其他能力。精确UID/GID映射及授予机制须具名批准，不向宿主后端授予任意setuid能力 | 这是待审批的实验角色，不是默认启用root；不得用initial user namespace或privileged容器替代 |
| writer角色W | A中为本次独占且不同于C的UID/GID；exec后Eff/Prm/Inh/Amb能力均为0，无控制面写权限、无特权FD、无C的补充组 | 拒绝该环境，不声称Credential已自动解决 |
| 禁止重新提权 | 在Go启动前由受审环境设置NoNewPrivs=1，使实际clone线程及后代继承；核对实际线程/子进程状态。Go1.25 SysProcAttr没有该字段 | 不在exec后才补设，不偷偷增加launcher或权限 |
| 能力边界 | 子进程CapBnd可继承非零；必须如实记录。NNP1和零permitted/inheritable/ambient等约束共同防止exec获权；不声称Bnd已清零 | 若环境要求清Bnd，另审额外机制，不隐含CAP_SETPCAP |
| 创建与文件边界 | clone3/CLONE_INTO_CGROUP在目标环境可用，无先运行再迁入回退；C/W仅访问本次临时媒体、账本及控制根，writer不得读账本/启动门或控制其他域 | 任何额外权限/沙箱组件另批 |
| 生命周期 | 单例A可用固定W；无未决writer时才回收环境。观察者不持原cgroup FD、不持启动门、不向恢复者借能力 | 不用活观察者伪造“全部控制者丢失” |

NNP不会阻止控制者使用已经持有的SETUID权限；角色切换、清理补充组和子进程实际能力必须分别核对。已安装Go1.25标准库有UseCgroupFD/CgroupFD及Credential，可优先复用，无需假定新依赖；本轮仅阅读源码，目标seccomp/内核/权限仍待实际验证。[NNP](https://docs.kernel.org/userspace-api/no_new_privs.html)、[用户命名空间](https://man7.org/linux/man-pages/man7/user_namespaces.7.html)、[能力集](https://man7.org/linux/man-pages/man7/capabilities.7.html)

选择隔离命名空间内UID0仅为避免非零C在exec前后保留P/E而清空Inh/Amb所需的额外launcher机制；不取得宿主root或扩大映射。C进入Go运行时的实际权限、securebits、NNP及每个执行线程必须实测，不能从容器参数推定。W在exec前设置全部UID/GID及清空补充组，固定执行无setuid/file-capability的已核验ffmpeg；exec后四个能力集为0仍是硬门槛。测试媒体目录与文件权限由具名fixture准备，不隐含增加CHOWN、DAC_OVERRIDE或SETPCAP；无安全可用fixture则拒绝执行。

当前WSL调查只有“已运行UID1000无可写委派”的证据，不能把该环境填成可执行。环境合同中的授权字段须在实施前明确，不自行chown/remount、改systemd或创建特权容器。

## 4. 临时账本、控制域身份与状态

C2-A用私有临时SQLite/独占PG的验证表，沿用operation ID/token、物理identity、fenced、armed/attempted/closed/stopped及独立编码结果语义。临时实验表不是业务迁移，也不是已经接入的产品协议。

binding为严格内部类型：版本、环境ID、boot ID、根dev/ino、域dev/ino及分配阶段。相对域路径只由不可复用的operation ID派生，不接收任意路径/PID/凭据或trusted布尔值。分配阶段仅reserved/attached/absent：reserved只允许armed且域dev/ino为空；attached具有实际域身份；absent只允许stopped/failed且域dev/ino为空，表示本次从未启动且无待清理控制域。absent只能由下述门内原子恢复产生，绝不表示旧物理媒体不存在。附着CAS只发生一次，attached及absent之后绑定字段不可修改。解析限制2KiB、白名单及类型/数值范围，拒绝重复键、未知版本、未知字段和尾随JSON；仅DisallowUnknownFields不充分。记录完整性由可信写入面与不可覆盖CAS保护；不把普通JSON自报当可信证据。

这些值仅用于核对，**任何单个inode、boot ID、路径存在/不存在都不是证明**。正向恢复同时依赖：受保护独占祖先的连续保管合同、每代际仅分配一次且不重建、域绑定在任何可能创建前已提交、所有入口共用持久启动门。旧boot、根替换、域替换、未知创建来源、无法证明连续性均保留unknown，不按当前根猜原位置。

顺序定义：

1. claim在短事务保存armed及计划binding（域尚未分配），之后才在启动门内创建精确域；记录实际域身份并CAS附着，之后才可提交attempted。mkdir前必须已有本代际的分配预留；任何绑定缺失不得启动。
2. **未启动原子恢复**单独处理armed，不能套用下述一般closed路径。先持启动门重读精确token、原预留、fence及phase，验证受保护祖先连续性和“所有launch先committed attempted”的未启动证明；不创建新域。armed+reserved且预留路径不存在时，在同一短事务CAS为stopped/failed+absent，无任何OS创建/删除调用。armed+reserved且本次mkdir已成功但未附着时，仅当不可复用预留、独占根、创建者唯一及空叶子归属均可证明，取其实际身份，在同一事务附着binding并直接CAS为stopped/failed+attached；不留下closed+reserved。armed+attached则重核原域身份、空叶子，直接原子转stopped/failed。未知EEXIST、非空/未知子域、矛盾身份或不能证明的分配保持原行和义务，不能补建空域或借用当前目录。事务失败保留armed及原预留/原域，下次从相同入口重入；提交前不rmdir。此例外不适用于attempted/closed。
3. attempted在实际clone前提交。实际创建使用本次已核对的cgroup FD；所有可能创建的调用都在同一启动门内，失败不得回退为普通exec。writer后代留在该操作域，控制面能力不泄露。
4. 对已经attempted的附着域，close先在门内持久closed，阻止所有旧launcher。再对同一可信域kill并等populated=0；返回kill成功、父进程死亡或租约到期均不够。closed只允许attached，不接受reserved/absent。此时不等待需要启动门的正常完成回调，也不在DB事务内操作OS。
5. 同域确已空且不能再进入后，持久stopped和结果。编码exit0与完整成功来源仍在才可success；结果丢失写unknown，物理停止与业务失败分离，不凭可解码文件恢复ready。
6. stopped+attached已提交后才允许清理精确空域；清理失败保留该行及义务，不提前physical settled。stopped/failed+absent无需控制域清理，恢复者只核对既有可信未启动回执和预留路径仍为空；若出现新对象则视为连续保管违约而保留义务，不删除它。成功清理或合格absent分支释放门后，再取P2物理锁重入P3-B结算/发布或删除worker。崩溃于rmdir后由不可覆盖stopped回执重入，不依赖重新创建域；absent分支不能发布ready。

域限定为操作叶子，writer不能创建子域；不做宽范围递归cgroup清理。出现未登记子域或其他归属进程时不终止、不删除。并发gate/CAS、绑定附着及先预留后创建的实际细节必须在A中通过确定性反例验证，否则C2候选失败，不进入B。

## 5. C2-B 产品接入候选（不属于A）

仅A证据成立后再形成可实施范围。建议独立协议`cgroup-playback-v1`，不复用或放宽C1的`controlled-playback-v1`。新控制者可在核对持续域后恢复原代际，不能改写原ControlOwner或把C1无句柄记录升级可信。

最少持久候选是一列`control_domain TEXT NOT NULL DEFAULT ''`，仅新协议解析上述有版本、严格字段/长度限制的内部binding。旧行保留空串、不回填；原三列和旧C1行为不变。实际字段、分配状态表示及迁移要以A验证为依据重新列明并批准，不能把本段当schema实施授权。

复用位置为app的playbackProducerControl/completeControlledPlayback、video_transcode的同一启动链、P3-B的准入/停止屏障/物理锁/receipt/outbox。repository只增加新协议的具体绑定校验和单调CAS；当前同名C1校验、ClaimWindows和扫描不能直接套用。共享准入、settle与completion须显式识别两种协议，拒绝未知协议及伪造receipt，不做“任意非空protocol都可信”的泛化。

产品并发还有独立门槛：同一W身份可以读写其他同身份文件或影响其他进程，C≠W不能证明每operation只写本目标。B必须另给每operation身份/文件沙箱、共享原件访问、ptrace/信号、输出权限撤回、UID复用与未决代际保留方案，并验证SQLite/PG并发和全部调用方。A单例通过不替代这些条件；不未经验证增加输入副本/新staging义务、改媒体格式或承诺通用FD协议可用。

正常Service/环境变量/部署入口的启用属于后续独立批准；须有具名目标平台、权限、回滚及无能力平台策略。C2不自动关闭Windows转码、不改变provider限制，也不接管旧记录或P5未知thumbnail。

## 6. 隔离测试和验收标准

所有竞争用事件/pipe/数据库屏障控制；超时仅防挂死，不以观察期无写入作终态证明。保留源码/二进制/工具hash、版本、命令、退出码、失败和skip；观察者只记录独立oracle，不给恢复者原域能力。

| 组 | C2-A必须给出的实际证据 |
| --- | --- |
| 环境负检 | 无委派、clone3拒绝、错误映射、NNP0、writer能力/补充组残留、控制FD泄露均在写入前失败，无降级 |
| 创建/登记窗口 | claim前后、预留后mkdir前、mkdir后绑定前、绑定后attempted前、attempted后clone前、中、后依次杀全部控制者；reserved无域原子转stopped/failed+absent且OS创建/删除计数均零，已mkdir未附着原子附着并stopped、提交后才清理；逐写点失败及重复恢复不产生非法closed+reserved，不补造域；已可能启动必须使用原域 |
| 全部控制者丢失 | 先确认全部控制角色进程终止且未保留FD/门锁；新进程从实际持久绑定恢复，先closed再停止，全部实际writer退出后才stopped/清理 |
| 迟到启动与fork | 两个真实launcher与close竞争；closed提交后没有OS创建；父死子活、孙进程写入、fork/kill竞争均不漏写者；逃逸或控制面写入成功即候选失败 |
| 身份与归属 | 假绑定、错误环境/boot/根/域、不属于本代际预留的EEXIST、已可能启动后的域缺失/替换、未知子域、其他用户/未选alias一律拒绝；不能新建空域洗白。armed本次分配崩溃的受限重入与拒绝旧对象分别测试 |
| 持久化及清理故障 | closed/stopped/binding CAS失败、锁/权限/kill/query错误、结果丢失、stopped后rmdir及P3-B提交失败均保留义务；重复恢复不改写终态，不发布迟到ready |
| 数据库 | 临时SQLite文件WAL与独占低权限PG，两真实进程；观察SQLITE_BUSY/55P03或实际等待，覆盖launch先赢/close先赢、回滚及重入；fakeStop不计OS证据 |
| 媒体 | 该环境实际ffmpeg处理H.265/MPEG4各含/无音频，保持H.264/AAC、偶数尺寸、yuv420p和普通faststart MP4，完整解码；结果丢失不以ffprobe补造success |
| 实验收尾 | 仅停止本次精确域/进程；在持久终态后清理域与自产对象，确认无残留，外部环境保留或撤销按其原批准合同；无业务挂载与既有服务操作 |

A的交付可以是“候选不成立/环境不满足”，这种结果不是P3或5.2通过。A通过也不等于B产品集成通过；B还必须用真实新controller走上传/回填/删除及真实Gin媒体URL、Range/鉴权/播放seek，跑P1/P2/P3-B/C1回归、临时迁移与并发矩阵、后端构建和独立审查。旧媒体通过只作为旧路径证据，不能替代新controller。

## 7. 审批与最终边界

当前已批准设计、独立审查及 C2-A 私有验证代码与临时 SQLite/独占PG检查；账本验证子包已交付并通过独立复核。真实OS/合成写者实验仍以第3节具名环境合同另获明确批准为前提，不在缺环境时申请笼统管理员权限；没有OS adapter或真实能力验收通过声明。C2-B、正常产品启用、Windows新内核方案、P4实际端点/费用/权限、P5可信旧manifest/维护分别保留。

本稿完成不是整个剩余恢复实现完成。要勾5.2，仍需原平台/部署边界内完整恢复与所有物理义务满足；Windows缺口、P4所有attempt终态和P5旧对象证据不能被C2实验排除。13/13之后仍须最终集成Verify及有效归档/交付授权。本轮不运行Guard、不归档、提交、合并或push，不操作业务库。

## 8. 本轮证据

实际读取当前C1/P3-B源码、项目Go1.25标准库及官方原语说明；研究稿为 `.local/profile-full-recovery-windows-design-research.md`、`.local/profile-full-recovery-linux-design-research.md`。这是设计证据，未执行新的OS探针、C2测试、schema迁移或产品实现。Runtime冷恢复要求的现有后端构建另行记录，不作为C2能力通过。

Runtime冷恢复后`comet check rerun`返回exit0、reused=true、tier=full，重新接受原完整构建证据`568be78e-ef06-4457-9ac2-c0b4d41fba2a`；本轮没有新执行go build或C2专项，不把复用写成新能力通过。

正式独立初审R-D1指出reserved无域记录缺少合法恢复终态，已集中补上原子未启动分支及absent组合、分配归属/清理/失败重入和矩阵；唯一1/1定向复核PASS，剩余IMPORTANT/CRITICAL为0，仅限设计包。审查源码稿SHA256 `014ae6f1554786c2460a01d73ef8a849bfb859824694b27b1a48042efbdb7053`；报告 `.local/profile-full-recovery-design-review.md`。既有C1和媒体包审查历史及复核次数不重置。此段为设计审查历史；后续代码/临时库授权与账本子包证据见本稿开头及第7节，真实OS具名环境仍另批。
