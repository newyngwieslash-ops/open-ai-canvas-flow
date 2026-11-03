# P3 ffmpeg 生产者控制与崩溃恢复：待审批方案

最新状态：用户已批准P3/P4/P5隔离能力验证及独立审查，当前执行P3-A；P3-B产品接入仍依据实验结果另行明确。下文“待批”描述是设计提交时历史，最新范围以rulings.md首节为准。当前 Classic Build 12/13，5.2 未完成，verify pending，archived=false。沿用 full / zh-CN / autonomous / tdd / standard / current；不重置既有审查历史。

独立设计初审提出“kill不可随数据库回滚”的一项问题，已在第5.6节及测试矩阵修订；唯一一次定向复核PASS，仅表示P3-A审批稿可提交。报告 `.local/profile-producer-p3-design-review.md` 保留完整历史，不代表P3-B已获准或通过运行验收。

## 1. 现状与必须补足的证据

`app/video_transcode.go` 的上传就绪和启动回填都调用 `maybeStartPlaybackTranscode`；`ClaimPlaybackProduction` 原子保留 playback 身份并登记 P1 operation，然后 goroutine 经 `StartResourceProduction` 调用 `runH264Transcode`。当前 ffmpeg 直接写 `playback/<resourceID>.mp4`，使用 libx264/AAC、yuv420p、偶数尺寸及 `+faststart`。`cmd.Run` 返回后才记录 `ffmpeg-wait-returned`，再调用 `CompletePlaybackTranscode`。

正常返回证据不能补齐父进程崩溃的窗口。父锁释放、进程号不存在、取消请求或发出 kill 都不等于所有实际写者已退出；panic 仍须保留 unknown。`ResetStuckPlaybackTranscodes` 当前是保守 no-op，不能按时间重置 processing。P2 恢复仅识别 `local-go-lock-v1` original，不能把它的可取锁事实直接用于 P1 playback。

发布还需补强：现有 `CompletePlaybackTranscode` 以资源状态和 key 检查终态，没有 operation token 参数。P3-B 必须将成功结算与 token、身份、fence、processing 状态校验放入同一事务，不能仅增加进程 kill。

## 2. 推荐分段与最小边界

**推荐先批准 P3-A：只实现可丢弃的隔离能力验证程序和报告；P3-B 在结果明确后再次审批。** 原因是目前没有两平台可共同采用且已证明的停写控制，直接承诺产品恢复会把 OS 假设当成证据。

| 包 | 最小范围 | 不包含 |
| --- | --- | --- |
| P3-A（本次待批） | 临时目录中的真实父/监督/ffmpeg/恢复进程；启动门、整个写者集合退出证明、普通MP4格式探针；候选数据库协议用临时SQLite与新建隔离PG做屏障试验；产出运行命令、版本和失败窗口报告 | 不修改产品Go代码、依赖、schema、API、默认转码行为、Compose或服务启动配置；不操作业务库 |
| P3-B（另批） | 依据P3-A已证明的一条平台路线，接入新playback协议、原子发布、受限删除恢复和worker；补正式专项及文档 | 不接管旧P1/旧对象；不新增后台全库扫描、自动重转码、公开取消API；不扩P4/P5 |

P3-A代码仅放 Git 忽略的 `.local` 专属目录，复用已有 Go/ffmpeg/ffprobe/依赖，不下载、不安装、不启动既有服务。新建隔离PG及测试子进程可列入审批范围；不存在工具或权限则报告缺项。需要新 Linux cgroup 委派、特权容器、内核设置或安装工具时，先交具体环境需求再批准，不能把能力验证授权解释为管理员配置授权。

## 3. 候选比较与停止条件

| 路线 | 判断 |
| --- | --- |
| 父Go持P2锁，退出时kill子进程 | 排除为完整恢复方案：锁先释放与子进程真正停止之间存在窗口 |
| 仅PDEATHSIG、PID/进程组或Wait单个直接子进程 | 可作辅助，不能独自证明整个写者集合停止；需涵盖后代、创建窗口和身份重用 |
| ffmpeg→pipe→Go写最终对象 | 不作为默认方案：保持当前普通MP4 seek/faststart未证明；不默换fragmented MP4 |
| 私有staging后复制/rename | 不能单独解决：残留进程仍可能写staging，必须保留其清理义务；本次不引入新对象布局 |
| 平台进程容器+监督控制 | P3-A主候选，能保留既有ffmpeg参数/输出路径；需分别证明不可逃逸、启动登记原子性和崩溃后可恢复的终止证据 |
| 子进程继承实际OS锁 | Linux备选探针；不能根据继承FD推断ffmpeg始终保留它，也不能将Linux结论照搬Windows |

主候选失败时不自动实施备选、更换媒体格式或增加特权服务。记录失败窗口后提交新的具体方案。P3-A交付可以是“候选不成立/环境不具备”；这不是P3功能验收通过。

## 4. 两平台能力验证

### Windows

以每operation独立 Job Object 约束监督器/ffmpeg及后代，禁breakaway；优先验证 `PROC_THREAD_ATTRIBUTE_JOB_LIST` 创建时关联，避免普通Start之后再Assign造成未受控运行窗口。不采用“先运行再登记”。检查父级Job/嵌套限制及失败返回，关联失败必须使子进程不能写目标。

`KILL_ON_JOB_CLOSE` 只作为终止触发，不作为已停止凭据。存活控制者须从同一个可信Job句柄核验整个集合归零；通知只作唤醒，重新查询实际状态。恢复进程必须能证明所观察对象就是该operation的容器并封闭后续加入。Job名不存在、Open失败、权限拒绝、句柄丢失一律unknown；不能以“再创建同名空Job”洗白。

**关键待证明项：所有监督进程都崩溃后，是否仍能恢复可信Job身份并获得完整停写证据。** P3-A必须强杀父与监督器验证该窗口；证明不了则Windows自动崩溃恢复不通过，不能仅凭正常取消通过批准P3-B。句柄继承不能泄漏到非参与进程，Kill-on-close不能被隐含引用无限延迟。

### Linux

主候选为每operation独立cgroup v2：在任何可写代码执行前加入，所有后代留在同一受控子树；关闭新的启动/迁移进入通道后，以容器终止加 `cgroup.events populated=0` 形成候选停写凭据。只发 `cgroup.kill` 不算完成。验证容器身份、挂载/namespace、权限、父死子活和监督器崩溃；缺失/被替换的容器不能当作空容器。

这条路线可能需要额外委派，与当前默认运行条件不等价。P3-A仅在已有且明确可用的隔离委派中试验；不为通过测试修改宿主cgroup或启动特权容器。恢复者不能杀同父cgroup中其他任务，必须按operation独占范围控制。

Linux备选小探针检验实际ffmpeg进程持有同一open-file-description的flock能力，以及实际构建是否关闭继承FD。`fd:`协议的regular-file seek支持不自动证明faststart重开成功；须运行实际MP4探针。任何继承失败、进程仍可写但锁可取得的反例，立即否定该候选。

## 5. P3-B 拟议合同（不是实施批准）

1. 复用P1 operation和P2 canonical physical identity，不把原P1 playback改标可信。新协议暂命名 `local-ffmpeg-controlled-v1`，只有具体受控launcher可登记，普通调用方不能传布尔值声称受控。
2. Claim提交reserved和冻结playback身份；取P2原件/副本所需锁时统一排序且不持数据库事务。源读取与别名删除也纳入锁集合，避免转码期间原件被另一入口删除。短事务遵守既有owner/lifecycle/row顺序。
3. 准备控制容器并建立可恢复身份，先持久登记、后允许写者运行；启动许可与fence必须互斥。恢复者封闭许可后，迟到launcher即使持旧快照也不能创建或释放新写者。登记失败、未知Start结果、崩溃窗口均保留未决。
4. 正常完成同时要求：完整写者集合终止、ffmpeg成功退出、普通MP4输出验证和文件写句柄关闭。物理settled与业务ready分离，完整成功证据丢失时只允许失败清理，不自动恢复ready。
5. ready/failed发布在同一事务验证operation token、协议、identity、fenced、实际成功/失败证据及当前processing状态；旧token不得覆盖新尝试。保留P1 unknown，不用新成功洗白历史义务。
6. 事务外获取/查询控制句柄不等于发送终止。任何kill之前必须在短事务内确认当前用户授权、全部引用和alias归属及operation集合，并持久封闭迟到启动；未决活别名不属于本次删除时不得终止。必须证明这道已提交屏障也阻止新的冲突引用/别名，否则不能发kill。随后在事务外控制终止并取得可信证据，再在短事务内重查完整集合，原子settle失败与stage。DB回滚不能撤销已发生的kill；后续失败必须保留先前屏障/恢复义务，既不释放旧token也不伪称整个动作回滚。当前P1/P2的fence不自动证明已覆盖该屏障：P3-A需验证最小状态机及是否需增加持久状态；未证明前禁止P3-B接入。无控制能力的repository调用、公告/ReplaceAssets继续保守拒绝恢复。
7. worker仍使用P2本地锁、Prepare/remove/Finish复查及租约合同；全部必要对象完成前保留资源/历史/意图。控制容器清理失败不能丢弃其恢复身份；删除幂等不意味着清除未决生产者。恢复必须能在DB提交失败后重入，不以已消失的容器推导成功。
8. 不开启启动全库恢复：只在现有授权删除/重试边界处理有新协议证据的目标。原 `ResetStuckPlaybackTranscodes` 保持保守，不自动重转码或接管旧行。

需持久化的最小语义：operation关联的平台控制种类/协议版本、不可混淆的容器身份与启动代际、启动许可封闭及可信终止证据。PID、时间戳、可复用名字均不足。P3-A先确定这些字段是否能由operation ID和已有字段可靠派生；不能则P3-B明确给出最小schema变更及临时迁移测试再审批，不预承诺零迁移。禁止把秘密/任意命令行塞入proof字符串。

## 6. 隔离测试矩阵与验收标准

所有竞争用管道/事件/数据库屏障控制，sleep和超时仅防挂死。只终止已确认归属的测试PID/Job/cgroup，不按进程名批杀。测试对象、数据库、锁和媒体均为测试自产；测试完停止自建服务，保留报告。

| 场景 | 必须观察 |
| --- | --- |
| Claim前后、容器创建、登记提交、放行写入各窗口杀父 | 未登记不能写；reserved迟到启动被fence拒绝；不产生无法归属的写者 |
| 真实ffmpeg写中杀父，保留子进程/后代 | 恢复不能仅取父锁就删除；完整集合终止前保持账本/字节保护 |
| 监督器、恢复者相继崩溃 | 可重新识别同一容器并重入；证据丢失则unknown，不冒认完成 |
| 容器终止与迟到spawn竞争 | 不能在观察空集合后再启动旧代际writer；启动门封闭有确定性证明 |
| fork后代/句柄泄漏/Job关联失败/cgroup迁移失败 | 逃逸或遗漏写者使候选失败；不能靠只Wait主ffmpeg通过 |
| Windows句柄/Job名、Linux容器身份缺失或重用 | 不查错对象、不kill无关进程、不把新空容器当旧操作证明 |
| 写完但Finish前、Finish后发布前退出 | 可清理而不能凭可解码文件补造ready；旧成功/失败发布均被拒 |
| DB登记/结算/发布/stage/Finish失败 | SQLite及隔离PG回滚一致，保留恢复义务；下次重试仍能证明身份 |
| kill前授权/活alias复查、kill后stage提交失败 | 不先误停其他资源；kill前屏障提交失败不得终止；kill后失败保留已提交屏障与恢复义务，不能声称DB回滚恢复进程；新增引用/迟到启动均被屏障约束 |
| 相同identity、活别名、引用新增、租约接管和部分批次 | 不误杀/误删其他资源；完整集合变化回滚；两个worker不同时remove |
| 真实媒体格式 | H.265与MPEG-4 Part2输入，含音频/无音频；保持H.264/AAC、yuv420p、偶数尺寸、普通MP4且moov先于mdat；ffprobe/完整解码和Range读取验证 |
| 权限/不支持平台/旧P1/远端/旧thumbnail | 明确不支持或unknown；不降级无锁、不升级旧证据、不触P4/P5 |

**P3-A交付标准**：两平台分别记录OS/内核、Go/ffmpeg构建、运行命令、真实子进程证据、精确通过/失败/skip，绘出启动门和崩溃窗口；辅助写者反例与真实ffmpeg格式验证缺一不可。SQLite/PG候选账本试验证明状态机，不声称测试了尚未接入的产品实现。缺环境不能记PASS；不强求为“全绿”扩权限。对拟推荐平台路线进行一次独立设计复核，并给出P3-B准确文件/字段/部署前置条件。

**P3-B验收标准**：需另获实施授权，真实反例RED→GREEN；Windows/Linux实际控制与产品调用链、临时SQLite/隔离PG竞争矩阵通过；回归P1/P2、普通MP4播放与Range、删除引用/alias/租约/失败保留；完整后端构建与独立审查通过。缺任一关键平台终止/恢复证明，P3不能宣布完成。即使P3通过，P4/P5及5.2整体仍须独立满足，不越Guard。

## 7. 官方依据与证据限制

- [FFmpeg protocols](https://ffmpeg.org/ffmpeg-protocols.html)：pipe与regular-file fd的seek差别；本项目安装版本/faststart行为仍须实测。
- [Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)：子进程关联、breakaway、终止、查询与通知限制；不从名称缺失推导停止。
- [UpdateProcThreadAttribute](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute)：创建时Job列表能力，需核对目标系统支持与嵌套约束。
- [Linux cgroup v2](https://docs.kernel.org/admin-guide/cgroup-v2.html)：进程归属、委派、kill及populated观察；启动门封闭与身份绑定是本方案额外必须证明的条件。
- [PR_SET_PDEATHSIG](https://man7.org/linux/man-pages/man2/PR_SET_PDEATHSIG.2const.html)：关联父线程且存在设置窗口，不作为完整进程树终态证据。

本轮仅源码调查、官方资料核对和设计文档；没有执行P3测试、创建进程控制容器或修改产品实现。P3-A也须用户明确批准后才开始。P4 provider协议/能力限制、P5旧数据维护、业务库操作、部署/重启、付费调用和push均不在批准请求内。
