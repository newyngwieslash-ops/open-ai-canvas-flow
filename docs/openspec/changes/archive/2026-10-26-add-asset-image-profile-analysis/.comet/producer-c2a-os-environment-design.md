# C2-A 真实 OS 验证：具名环境与实施审批稿

状态：仅设计，实施未批准。用户已确认没有现成 VM，先按独占 VM 候选细化。`yingce-c2a-os-a1` 是拟建环境名称，不是已存在或已验证的机器。本轮不创建 VM、不启用虚拟化、不配置 namespace/权限、不执行探针、进程实验或数据库检查。

独立设计审查：初审两项集中修订后，唯一 **1/1 定向复核 PASS**，剩余 CRITICAL/IMPORTANT/MINOR 均为0。受审正文 SHA256 为 `7c5516a04b639ee89af468d4363948e98137e16ec741d625c601eb935a17f2eb`；此后仅补审查元数据。OS能力仍为 NOT RUN，实际绑定与实施审批尚未完成。

关联任务5.2，当前12/13、Build、Verify pending、archived=false。沿用 full / zh-CN / autonomous / tdd / standard / current。已交付的 [账本子包](producer-c2a-ledger-implementation.md) 与审查历史保持；本稿不扩大到 C2-B、P4/P5、正常产品入口或业务库。

## 1. 选择与最小边界

| 路线 | 实际代价及边界 | 决定 |
| --- | --- | --- |
| 新建独占 Linux VM `yingce-c2a-os-a1` | 一台无业务数据、无共享宿主目录、可离线运行的试验机；权限变化只落在该 VM | 推荐候选；平台、离线镜像和路径尚未绑定，不能据名称执行 |
| 改造现有 WSL/共享 Linux | 现有证据只表明 WSL UID1000 无可写委派；改造涉及现有系统及管理生命周期 | 不作为本次最小实施项，不自动提权或修改它 |
| 特权容器或普通命名 Job/监督进程 | 前者扩大宿主权限且仍需域/namespace证明，后者不解决全部句柄丢失 | 不用作替代通过路径 |

只验证一个活跃合成 operation、一个专属 W 身份和该 VM boot 内的恢复。多个控制进程用于竞争与崩溃测试；不是多个独立媒体任务共用 W 的产品安全证明。无新常驻服务、通用特权代理、sudo规则、产品字段或业务迁移。

后续最小代码位于新的忽略目录 `.local/profile-c2a-os/`：复用已冻结账本子包作为隔离测试快照，新增受限 bootstrap、Linux 控制域适配器、合成进程 fixture、环境验证器及测试。不修改原账本七源、后端 Service/model/migration、部署或编码器安装。仅当具体实施获批后编写；本稿不是代码交付。

## 2. 执行前必须绑定的环境清单

下列字段目前没有真实值，属于执行门槛，不能由候选名称、默认路径或一条概括批准代填：

- 负责创建/保管的操作者；已有虚拟化平台与本次新 VM 的实际标识、存储绝对路径。没有平台则另批安装，不自动启用 Hyper-V、重启 Windows 或改已有虚拟机。
- 用户提供或另行明确批准的离线 Linux 镜像路径、来源及 SHA256；禁止自动下载镜像、拉容器或安装系统软件。
- 实际 guest machine-id、boot-id、初始 user/mount/cgroup namespace 身份、内核版本/构建标识与 cgroup2 挂载。源码核对以 Linux v6.12 为参考，不能把版本号当能力通过。
- 已有离线 Go1.25、C编译器（SQLite CGO及最小原生bootstrap复用）、ffmpeg/ffprobe 路径/版本/hash；实际支持 libx264、所需解码器及 AAC。缺工具停止，不安装或替换。
- VM 无业务磁盘/共享目录/宿主设备或管理 socket，试验期间无外网；不把 Windows 仓库挂入 guest。源码/合成fixture通过明确批准的离线导入方式按 hash 复制。
- 以下 cgroup/数据目录的实际 dev/ino、所有权/权限、管理者及持续保管配置；初始 VM namespace 允许至少本轮所需的四个 sibling userns，不能为绕过失败自动改全局 sysctl/LSM。

绑定后生成不可复用环境代际 ID（随机 UUID），由 VM 管理员保存只读 manifest；C 从受保护路径核对期望 hash，普通 JSON 自报 `trusted=true` 不构成授权。发生 boot、控制根或管理合同变化，原代际失效，保留 unknown；不复用环境 ID 重建空域。

## 3. 身份、namespace 和 bootstrap

### 固定 UID/GID 映射

以下是待批准 VM 内的专属数字身份；实施前核对无已有账号、文件或无关进程占用。H仅在该新VM建立固定测试账号/组 `yingce-c2a-c`（200000）和 `yingce-c2a-w`（200001），无登录口令、nologin、无额外组；C的home在私有control目录，W无可写home。这样PG的非root系统身份和peer认证也有明确来源。冲突时停止并修订具名清单，不能借用现有账号；不在Windows/其他主机创建账号。

| 角色 | VM 初始 namespace 的身份 | 每次 C 调用的新 userns 内身份 |
| --- | --- | --- |
| 环境管理员 H | 该专属 VM 的管理员；不映射进 C/W namespace | 不进入运行中的 C/W 角色 |
| 控制者 C | UID/GID 200000 | UID/GID 0 |
| 写者 W | UID/GID 200001 | UID/GID 1 |

uid_map 和 gid_map 各精确两段：`0 200000 1`、`1 200001 1`。不映射 VM UID0，不授予真实宿主身份的 setuid 能力。C/W 的 real/effective/saved/fs IDs 均须实测符合映射，补充组均为空。

只新建 user namespace；不增加 PID、mount、network 或 cgroup namespace，也不 remount。它们仍是独占 VM 的 namespace，实际标识记录到环境清单。恢复进程可从 VM 初始 namespace 创建**新的 sibling userns**，使用同一精确映射与环境代际；不重入旧 userns，不保留其 namespace FD 或安排 keeper。

这是对原宽泛“同一环境”的具体定义：稳定的是 VM boot、受保护根、VM kuid/kgid 及管理合同，不是每个控制者私有 userns 的 inode。各次 userns 身份单独记录。新 C 通过对原 cgroup 文件的已授权访问恢复，不能把普通 signal 权限等同于该能力；跨 sibling userns 的真实停止是必过实验。

### 短生命周期 bootstrap 顺序

1. H 只创建本次精确子进程及映射。子进程先在 VM 初始 namespace 清组并降 real/effective/saved/fs UID/GID 到200000，确认无 VM 初始 namespace 能力，再创建新 userns。不得让未映射的 VM root 凭据进入普通测试代码。
2. 子进程在映射握手上阻塞。H 只向其尚未退出的已核对直接子进程写上述两段 map；使用固定父子关系及进程身份防 PID复用，不接受用户输入的任意目标PID。保留 `setgroups=allow`，由有父 namespace 权限的 H 写 gid_map；不走 `deny` 后用 `NoSetGroups` 保留组的降级。
3. bootstrap B 是尚未 exec Go 的短暂角色：新 userns 初始会有该 namespace 的全能力集，这一点必须披露。B 仅在自己的 userns 设置 `user.max_user_namespaces=0`，阻止 W 再建子 userns；初始 VM namespace 的值不得改变。该步骤使用 B 瞬时的 namespaced `CAP_SYS_RESOURCE`，不是 C 的运行权限。其他 namespace 限额不批量改动。
4. B 清空补充组和 ambient/inheritable，设置 NNP=1，并用 capset 将本执行线程 P/E 降为 `CAP_SETUID|CAP_SETGID`（0xc0），随后 exec 固定 hash 的 C 程序。首选不修改 bounding set，省去额外 SETPCAP 操作；Bnd如实记录，可非零。不得设置 NOROOT/NO_SETUID_FIXUP/KEEP_CAPS 等改变预期凭据降权规则。
5. Go C 的每个实际线程必须 NNP1、P/E=0xc0、I/A=0；不存在任意 goroutine 设位后推定所有线程有效的做法。NNP在本次 Go runtime 启动前已设置；C不持 SYS_ADMIN、SYS_RESOURCE、SETPCAP、CHOWN、DAC_OVERRIDE、SYS_PTRACE 等能力。
6. C 用已有 Go1.25 `UseCgroupFD/CgroupFD` 创建即入原域，`Credential` 明确设 W UID/GID=1、清空补充组，AmbientCaps为空。W exec 后 P/E/I/A四集为0、NNP1，Bnd只记录不虚报清零。W及后代不得新建userns或改回局部限额。

NNP不会禁止使用已有 SETUID/GID，也不会替代组清理。UID0 exec 与 NNP/原 permitted 的组合须按实际内核、securebits和普通无filecap可执行文件验证；不根据 capset 调用成功推定 Go 之后仍符合要求。任一权限失败不添加新cap补救。

H/B是待审批的环境工作，不是已存在的工具或受审组件。优先复用已有工具；无法完整表达时才写一个无网络、无通用命令/任意路径入口的短生命周期、单线程原生bootstrap。它在短暂全能力阶段不启动Go runtime，不安装setuid位/filecap，不提供RPC，不常驻。H保留的是环境管理员权限，不声称已被技术限制为仅几个cap；批准须限定本VM及列明操作，不能转为宿主泛化管理员授权。具体工具版本或bootstrap源码/二进制另经实施审查，不把未经执行核对的unshare/setpriv命令当既成入口。

## 4. cgroup、文件和数据库权限

### 控制树

固定候选路径为 `/sys/fs/cgroup/yingce-c2a-os-a1`（D），其子项：

- `controllers/`：仅 C/bootstrap 进程源组，恢复 C 也从这里运行。
- `work/`：空的受保护控制根 R；其唯一活跃叶子 `op-<immutable UUID>` 承载该 operation 的所有创建者后代。

H在现有 cgroup2 挂载内一次建立精确子树并核对归属；不改全局根、不开启资源 controller、不建threaded域。D/R/门父目录不可随应用C退出被服务管理器删除、迁移或重建。必须检查实际管理配置和操作者合同；目录存在或 `Delegate=yes` 本身不是连续性证明。

C拥有D/R和所需控制文件的权限：能够在R创建叶子、核对身份、写叶子的cgroup.kill、读events/procs，并具有源/目标与**共同祖先D/cgroup.procs**所需迁移权限。每次bootstrap由H在握手时放入controllers，C不从D外任意源拉进程。只授予新D子树内的确切文件，记录最终所有权/权限；不递归chown整棵系统cgroup。W不能遍历/写控制根、procs/threads/kill/subtree_control，也不能从任何别名或继承FD绕过。C自己不进入将被终止的work叶子。

新 sibling C对原域写 kill 是候选能力：Linux v6.12参考实现通过文件授权执行组终止，不借用旧 C句柄；目标内核仍须实测。kill返回成功不等于终态，必须同一原域 `populated=0`、所有可能创建已封闭、身份/保管未断裂。未知子域/归属或根/域替换时保留 unknown，不终止、不清理或补建。

### 数据与可写面

候选数据根 `/var/lib/yingce-c2a-os-a1` 仅新VM本地盘。H保管环境manifest；`control/` 为C:200000、0700，ledger/gate/日志及私有数据库socket在内。W不得读写这些对象或数据库凭据。

输入为H封存的合成只读文件。每个预列测试UUID的输出父目录由C拥有、0711；预建 `playback.mp4` 由C拥有、GID200001、0660。W可读写目标字节，但不拥有它，不能chmod、换名、删除或创建额外同目录文件；C可读取、ffprobe和最终清理，不需 CHOWN/DAC_OVERRIDE。该固定目标方案仅限本实验，不宣称已解决 C2-B 的一般输出沙箱。

H在这台新VM内使W对 `/tmp`、`/var/tmp`、`/dev/shm` 及其他普通持久写入口无写权限，并提供只读/不可用的HOME/TMPDIR；只保留固定媒体目标、必要的标准输出/事件管道和null设备。必须枚举真实可写挂载、socket、路径及FD；任一未知可写逃逸则环境失败，不自动开放临时目录。无需在宿主或共享系统改这些目录。ffmpeg faststart若需要额外文件且现有固定目标不能满足，本候选失败，先报告最小新需求再审批。

可写面还必须显式核对不经过路径DAC的SysV共享内存/消息队列/信号量、POSIX mqueue、abstract Unix socket及FD传递。普通进程私有内存与可共享或退出后仍存续的对象分开记录；不能把收紧 `/dev/shm` 当作禁止SysV IPC。未受审的创建/传递入口或不能精确归属、收尾的共享对象使候选失败；不自动新增namespace、修改全局sysctl、安装seccomp或扩大W白名单。

W不得继承控制目录/cgroup/ledger/gate/数据库FD；普通stdout/stderr及事件pipe有明确白名单，所有控制FD close-on-exec。检查实际exec后FD，不只看文件权限；预exec短窗口另外验证，不能把CLOEXEC说成fork时已经关闭。

### 临时数据库

SQLite为C私有目录内真实WAL文件。Linux PG如执行全矩阵，另建本VM专属临时集群，以VM非root账号yingce-c2a-c在initial userns运行；只监听C私有0700目录的filesystem Unix socket，`listen_addresses`为空，不留默认/tmp或abstract socket。低权限登录role/database为 `profile_c2a_os_test`，NOSUPERUSER/NOCREATEDB/NOCREATEROLE/NOREPLICATION及独占schema，建表前核对目录/systemID/身份。测试连接采用精确peer映射 `yingce-c2a-c → profile_c2a_os_test`，管理连接只用于本新集群准备/收尾，未知role/database/连接方式拒绝。W无法遍历socket父目录。不能复用先前Windows的loopback trust方案给真实W使用，否则W可冒充C修改账本。

旧私有ledger的Windows DSN白名单不能直接绕过；未来仅在新隔离快照增加具名Linux Unix-socket fixture和同等防误连校验。既有七源及证明保留；无业务库DSN、无真实key、无远端provider调用。

## 5. OS动作的启动门与状态顺序

账本审查已证明：PG会话失效时不能依赖数据库锁撤销外部动作。因此实际launch/close/kill/cleanup还需一个独立、不可替换的**本地进程生命周期门**；不得把现有DB guard直接当作OS门。

最小候选是Linux传统 `fcntl(F_SETLK/F_SETLKW)` 全文件进程锁，加同一C进程内mutex。每个C只打开一个强引用gate FD；父目录C私有，整个环境代际不删、不换名、不重建gate。传统锁随C进程死亡释放，不由fork子进程继承；不选会随子进程保留open-file-description的flock/OFD锁来推定全部C死亡即可取门。

已知上限必须在实现中以 `ponytail:` 注释说明：同进程任何另一FD关闭相同inode会释放传统锁，线程也共享该锁。故唯一打开面、禁止dup/二次open、强引用不被finalizer关闭、stat用既有FD、close仅在mutex内释放后；这不是通用锁库。需要突破单例/独占程序假设时停止，另审更完整门协议，不扩成产品抽象。

另一项硬前提是锁owner的文件描述符表（`files_struct`）连续性：从取得gate到结束全部可能创建及停止/空域处理，所有可launch的父C线程必须始终共享取得该锁的同一张表。禁止父侧 `unshare(CLONE_FILES)`、`close_range(..., CLOSE_RANGE_UNSHARE)`、exec替换，以及任何分离/替换父表或提前unlock/close的路径；保留相同FD数字/inode或相同tgid不足以证明owner未变。固定Go/native源码及依赖的实际调用链必须核对这些动作，无法证明即候选失败，不加seccomp、ptrace、BPF或新cap补救。普通Go runtime以 `CLONE_THREAD|CLONE_FILES` 新建共享原表的线程不因此被禁；创建出的独立子进程按预期有自己的FD表并exec，与被禁的父侧表分离/exec不同，不能让新进程以CLONE_FILES共享父表。

获得OS门后重读精确代际/token/fence/phase和完整环境/域身份；OS门跨短DB提交、实际创建/kill/空域确认及cleanup保持，DB事务不跨OS调用。顺序仍为claim+reserved先于mkdir，附着先于attempted提交，提交attempted后才可能clone；持久closed先于kill，原域空且后续创建被封闭后才stopped，持久stopped后才rmdir；释放OS门后再结算。遵守 [原方案第4节](producer-full-recovery-design.md) 的未启动原子absent/attach+stop、unknown、失败重入和fenced不ready合同。

所有C进程死亡后，新C从同一原gate inode取锁、重开原R/叶子并核对。观察者不持gate/旧域FD/旧namespace FD，不向恢复者借句柄。环境H/观察者可以存续，但不充当活的旧协议控制者；故障用例结束后的应急清理由H单独记录，不能算恢复程序成功。

父C在clone3中死亡是核心实证门槛：核对与目标内核构建对应的fork/cgroup发布、原锁owner父表持续共享、files_struct最后引用退出和传统锁移除顺序，验证Go实际创建进程的flags未引入CLONE_FILES，父侧无上述分离/替换表路径。最小实际窗口包括native clone3/VFORK：子进程在exec前首个受控事件处暂停，父仍在clone syscall等待，确认子已入原域后杀全部C并立即恢复；另覆盖调用前和返回后，以及Go真实launch/exec链。源码排序依据与实际窗口共同构成候选证明，不能把用户态事件称为定位了copy_process内部每条指令。

没有证据的内部区间仍明确未直接注入。若上述源码与实际窗口不能排除“门已释放而原clone后来才入域”，候选不通过，另报精确内核注入所需方案/权限；本包不预先授权ptrace/BPF/kprobe、内核补丁、替换内核或新增调试权限。不能仅凭“不继承fcntl锁”或随机SIGKILL/一段时间无写入推定安全。

## 6. 隔离测试与验收

以下全部是待批准后执行的计划，当前结果为 NOT RUN。事件/pipe/数据库或内核可核对的屏障控制交错，超时只防挂死。保留失败/skip与源码、工具、镜像和二进制hash；无法触达的内核细粒度窗口明确说明替代依据，不伪称逐指令覆盖。

| 编号 | 场景与最小断言 |
| --- | --- |
| E1 | 核对实际VM/boot/挂载/管理合同/专属ID无占用；无委派、缺clone3/cgroup.kill、seccomp/LSM拒绝均在媒体写入前失败，无普通exec回退 |
| E2 | map精确两段、setgroups allow、C/W实际全部IDs与空组；deny、错误映射或组残留必须拒绝，不设NoSetGroups绕过 |
| E3 | Go C所有线程NNP1、P/E0xc0、I/A0；W exec四集0、NNP1、普通二进制无fcap/setuid。NNP0/额外cap/FD泄露拒绝 |
| E4 | 仅新C userns的max_user_namespaces=0，初始VM值前后不变；C/W改回失败，W的unshare/clone/clone3 NEWUSER拒绝，新 sibling恢复C仍能建立。不得以NNP自动阻断namespace为依据 |
| E5 | W写控制文件/迁移/建子域/读ledger/开PGsocket/改gate/额外路径或继承FD逃逸均被拒绝；固定目标rw正常，chmod/unlink/rename失败。显式枚举SysV shm/msg/sem、POSIX mqueue、abstract Unix socket/FD传递；未受审入口或不能精确收尾的共享对象即失败，不以目录DAC替代 |
| G1 | 两个真实C进程及同进程goroutine竞争OS门；actual EACCES/EAGAIN或确定等待后才提交/clone。gate替换/重复打开/提前Close负检；子进程不能因继承锁阻塞新恢复者。固定父C调用链证明同一files_struct持续共享；独立最小反例/负检揭示父侧unshare-files或CLOSE_RANGE_UNSHARE即使保留同FD仍可能提前失锁，并拒绝该路径，不把反例fixture当允许的launcher |
| G2 | 暂停原C的DB连接并精确终止自身PG backend；另C仍不能越过实际OS门。原C应停止新动作/保留义务，释放或死亡后新C重读DB恢复。模型PG guard通过不替代此项 |
| C1 | claim前后、reserved后mkdir前、mkdir后附着前、附着后attempted前、attempted后clone前/中/后杀全部C；旧gate/域/namespace FD无人保留，新C恢复。无域原子absent且OS创建/删除计数0；有可信空域原子attach+stop；不产生closed+reserved |
| C2 | 两真实launcher与close双方向；closed提交后无新创建。按第5节核对对应目标源码、父表owner连续性及父侧无分离/exec/提前解锁路径，并留native VFORK等待/子exec前、调用前后与Go真实创建证据；C死亡后所有在途子进程已在原域或创建失败，不能出现stopped/cleanup之后的迟到写入；父表前提未证或无法排除内核在途窗口则不通过，不默加调试权限 |
| C3 | W父死子活、孙进程、fork与kill竞争；恢复C在新sibling userns凭原域执行kill，全部实际成员退出且populated0后才stopped。普通kill权限拒绝不作为cgroup.kill失败/成功的替代 |
| I1 | 旧boot/错误环境/根/域/源不明EEXIST/假binding/未知子域均拒绝，不补造域；破坏性替换用无在途写者的专属fixture，原域无法证实时保留隔离/义务 |
| F1 | 附着/closed/stopped/cleaned提交失败、锁/kill/query/权限错误、rmdir后提交失败均保留义务并可重入；只清理持久stopped后的精确空叶子，清理失败不settled |
| F2 | 丢失exit结果写unknown，不用ffprobe补造success；fenced+success可物理结算但首次/重复ready均false；absent永不ready |
| D1 | 相同Linux源码快照运行真实SQLite WAL及新PG Unix-socket矩阵，保留双进程BUSY/55P03、会话断连、门释放和原子回滚等已知反例，不以Windows旧结果冒充本平台 |
| M1 | 本环境真实ffmpeg处理H.265/MPEG4各含/无音频四组，输出H.264/AAC、偶数尺寸、yuv420p、普通faststart MP4；完整解码通过，固定预建目标权限不被放宽 |
| Z1 | 所有C/W/后代精确退出，按持久终态只清理本次域/样本/临时库；表/schema/其他client及自产进程/端口检查。核对本次SysV shm/msg/sem、POSIX mqueue和共享socket/FD对象均可精确归属并收尾，无残留；未知对象保留失败，不宽删全VM IPC。环境保留/销毁按审批，未决义务保留，不执行宽泛kill/rm |

单例固定W、VM内单boot是公开测试上限。全部必测项实际通过、没有用skip替代能力、相同hash独立审查PASS且收尾完成，才可称“C2-A该具名环境OS能力验证通过”。任一必测无法执行、身份/保管前提不成立、发现逃逸或迟到写入，结果为未完成/候选失败，不缩小验收合同。

即使通过，也不是C2-B产品接入、Windows完整恢复、真实Gin页面或5.2完成；后续仍须各自原合同与批准。当前不运行Guard、最终Verify、归档、合并或push。

## 7. 下一审批单位与不可执行门槛

建议先审本稿候选，再绑定第2节的实际平台、离线镜像、VM路径和管理员。没有这些事实时，不请求泛化管理员权限，不把“同意方案”当成允许选购资源、启用虚拟化、下载/安装或重启宿主。

绑定材料和独立设计审查通过后，再提交一个范围明确的实施批准：仅该新VM及新私有目录，H的精确配置/映射、B瞬时userns能力与局部sysctl、最小OS适配/fixture、临时库及第6节实验、精确收尾。任何缺工具、需要更宽权限/目录/namespace/管理器修改的情况回到审批，不能默加。C2-B、P4/P5、业务库、正常入口及5.2的边界不随本批准改变。

## 8. 依据和设计审查

这是基于官方原语的候选推导，未完成运行验证。来源中的通用保证不自动证明本机权限、管理器保管或内核窗口。

- [Linux cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html)：控制文件、共同祖先访问及组终止语义。
- [user namespaces](https://man7.org/linux/man-pages/man7/user_namespaces.7.html)、[user namespace sysctls](https://docs.kernel.org/admin-guide/sysctl/user.html)：map/setgroups及局部namespace计数；源码核对见研究稿。
- [NNP](https://docs.kernel.org/userspace-api/no_new_privs.html)、[capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html)：继承、capset及exec边界。
- [fcntl locks](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html)：进程锁、fork和关闭语义。
- [v6.12 file.c](https://github.com/torvalds/linux/blob/v6.12/fs/file.c)、[locks.c](https://raw.githubusercontent.com/torvalds/linux/v6.12/fs/locks.c)、[unshare](https://man7.org/linux/man-pages/man2/unshare.2.html)：父files_struct所有权连续性及分离表反例；[shmget](https://man7.org/linux/man-pages/man2/shmget.2.html)：独立于目录DAC的SysV IPC创建面。
- [Go1.25 Linux exec](https://raw.githubusercontent.com/golang/go/go1.25.0/src/syscall/exec_linux.go)：创建即入组和Credential执行链。
- [Linux v6.12 user_namespace.c](https://raw.githubusercontent.com/torvalds/linux/v6.12/kernel/user_namespace.c)、[ucount.c](https://raw.githubusercontent.com/torvalds/linux/v6.12/kernel/ucount.c)、[commoncap.c](https://raw.githubusercontent.com/torvalds/linux/v6.12/security/commoncap.c)：bootstrap、局部限额及NNP/UID0 exec推导。
- [v6.12 fork.c](https://raw.githubusercontent.com/torvalds/linux/v6.12/kernel/fork.c)、[cgroup.c](https://raw.githubusercontent.com/torvalds/linux/v6.12/kernel/cgroup/cgroup.c)：在途创建与组终止的版本参照；实际内核须核对对应完整调用链，不能把不同revision的研究片段拼成目标构建已证。
- [PostgreSQL连接配置](https://www.postgresql.org/docs/14/runtime-config-connection.html)、[peer认证](https://www.postgresql.org/docs/14/auth-peer.html)：新集群仅私有filesystem socket及精确映射。

研究稿 `.local/profile-c2a-os-env-research.md`；独立正式设计审查及唯一1/1定向复核见 `.local/profile-c2a-os-env-design-review.md`，结果仅候选设计PASS。已关闭父files_struct连续性和非路径IPC枚举发现；没有运行实验。既有账本子包的唯一复核不重开；本环境方案是新的设计审查工作包，复核额度已用完。
