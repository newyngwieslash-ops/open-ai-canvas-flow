# 生产者恢复设计细化：提交审批版

最新状态：P1/P2已获批准并完成各自限定实施与隔离验证，详见verification-report.md。用户随后批准P3/P4/P5隔离能力验证及独立审查，结果见[producer-p345-capability-results.md](producer-p345-capability-results.md)。P3产品接入、真实provider变更和业务旧数据维护不从实验授权推定。Build保持12/13，5.2未完成，verify pending。下文各段保留早期候选与当时批准范围，不覆盖最新授权；完整目标协议不等于已全部实现。

## 1. 目标与已完成边界

目标仍是必要物理对象全部完成后才清业务记录，并能在有可信证据时恢复中断删除。逻辑发布被拒绝不等于物理writer停止；业务failed也不等于可删除。

现已完成写前CAS、显式delete-marker拒绝、既有intent/object/job与引用/alias/墓碑保护。这些继续复用。新设计不再预设两张通用表、全量staging或改变MP4格式；也不新增thumbnail生成器、公开管理API或全桶扫描。

## 2. 最小证据模型

建议用一份私有producer操作记录表达一次真实物化尝试；具体DDL在实现批准后依据调用约束确定。只在一次操作确实可能生成多个必要对象时增加子对象记录；优先复用现有删除对象身份合同，不能复制两套完成状态。

| 维度 | 最小语义 | 禁止推论 |
| --- | --- | --- |
| 身份 | operation ID、resource/user、variant、provider/endpoint/bucket/namespace/key，必要时version/session | storageSettingID不能代替冻结的存储位置 |
| 发布权 | generation/token、fenced标记 | fenced不代表已停写 |
| 物理状态 | reserved、writing、unknown、settled | failed、超时、取消、TTL不自动变settled |
| 结算依据 | protocol版本、proof类别、精确operation与对象、可信终态 | 单个HTTP status/ETag不证明所有重试已结束 |
| 清理义务 | 对应intent/object关系、所有已开始尝试 | 新尝试成功不覆盖旧unknown义务 |

fenced与physical state必须正交：例如fenced+writing等待停写，fenced+unknown等待证据，fenced+settled才可能执行清理。业务结果仍由现有Resource/任务状态承载；不把这些状态合并成一个顺序枚举。

旧记录默认unknown而非自动settled；没有新协议记录的旧writer不因新锁可取得而变可信。任何数据补登记仅是候选设计，不执行业务迁移或维护。

## 3. 允许的转移与删除条件

| 事件 | 条件及持久动作 | 失败/崩溃后 |
| --- | --- | --- |
| 准入 | 短事务检查资源、alias、墓碑；冻结身份与token，reserved提交 | 无记录不得发起I/O |
| 开始写 | 取得对应实际写能力控制；再核对live token与删除状态，转writing | 未证明没有发出请求则unknown |
| 正常完成 | 完整物理终态、全部SDK重试结束、证据匹配操作，settled | DB写失败保留未决记录，可凭可信证据恢复 |
| 逻辑发布 | 同一短事务核对未fenced、资源仍可发布、settled与身份匹配，且有完整写入成功及内容/元数据校验证据 | 失败不得删掉清理义务；仅停写不能发布ready |
| 删除获胜 | 现有lifecycle事务阻止新准入并fence有关操作 | 不等待网络I/O、不伪造settled |
| 恢复 | 取得真实物理控制或供应商终态证据后CAS结算 | 缺证据保留unknown并报告原因 |
| 最终完成 | 全部必要尝试无潜在后续物化；对象清理成功或由合法活alias保留；原Finish复检 | 任何失败保留资产、历史、资源和意图 |

同一物理身份的所有资源/attempt必须一起核对，不能只查当前resource。SDK透明重试也是潜在物化请求：必须可枚举/受同一终态保证约束，否则成功响应也不能消除较早未知请求。

settled只描述停写事实，必须区分完整成功、未启动、失败/部分物化。恢复者凭锁接管并fence/settle只解除清理阻塞，不恢复ready；成功证据缺失时不能通过可解码、文件存在或大小相同来补造。

## 4. 本地：保留对象布局的优先候选

P1实施完成后，用户仅批准 P2 进一步设计。具体待审批附件见 [producer-p2-lock-design.md](producer-p2-lock-design.md)，包含新协议边界、恢复准入、锁顺序和真实多进程验收；未批准实施，不改变下列P3–P5边界。

普通Go文件writer可先保留现有final identity，不强制staging。锁键以规范化、冻结的实际namespace和物理身份生成，不能用不同resource ID为同一对象生成不同锁。锁文件固定、不由清理器删除重建；部署必须证明同一存储介质上的跨进程互斥。

推荐顺序：准入短事务提交 → 取得物理锁 → 锁内另开短事务复查token/fence并标writing → 关闭事务 → 打开/写/关闭文件 → 锁内记录settled及可选发布 → 释放锁。任何等待物理锁的代码不得持有数据库事务/行锁；锁内数据库短事务沿原owner/lifecycle/row顺序。避免一方DB→等待物理锁、另一方物理锁→等待DB的死锁。

恢复者取得同一物理锁后，先在短事务fence旧代际再结算；旧writer随后拿锁必须重查live token，不能依据先前快照写入。删除worker清理时同样遵守控制，不能在已settled但尚可启动旧writer的窗口释放保护。

这仅适用于全部写路径遵守该协议的新操作。需审计普通上传、失败重试、task-media恢复、playback及维护工具；旧二进制混跑不满足此假设。发布/回滚期间的新旧writer共存是部署前置问题，必须另获部署授权后处理，不能隐含重启服务。

### ffmpeg单独留门

目前ffmpeg持有目标路径，父Go进程锁释放不证明子进程停止。候选比较：

| 路线 | 优点 | 未解决项 |
| --- | --- | --- |
| 实际子进程/监督器持有同一写入控制 | 可保留seek/faststart格式 | Windows/Linux继承、进程树崩溃、锁交接必须分别证明 |
| Go持有唯一目标文件写能力，ffmpeg经受控通道输出 | 残留子进程不能直接写目标 | 普通MP4需要seek；不能直接替换fragmented MP4并宣称等价 |

两者尚未选定。先做隔离能力验证方案，再提交准确格式/部署影响；不授权媒体格式改变。文件可解码、PID不在、panic或父进程退出均不作终止凭据。

## 5. 远端：按具体端点闭合，不能用统一adapter掩盖缺证据

| 端点类别 | 当前证据 | 待证明的门槛 | 本次提议 |
| --- | --- | --- | --- |
| AWS S3指定桶类型 | Abort可能留在途parts，需核清；不是完整提交屏障 | init响应丢失、并发Complete/Abort、SDK重试、全部版本/parts归属 | 仅设计候选session协议，尚不能批准实现为可靠恢复 |
| OSS / COS | 既有单PUT无持久操作终态；显式marker已拒绝 | 逐家session终止/并发提交合同、未知ID恢复与版本清理 | 不沿用AWS假设，逐家提交证明 |
| Kodo | deadline是在上传完成生成文件时校验；不是简单的开始授权时间 | 已接受Complete的排序、丢失init ID发现、重签/重试终态 | 修正旧草案描述，不以TTL作为恢复凭据 |
| 任意S3-compatible | 名称不能证明一致性或终止语义 | 具名产品/版本/配置的官方保证及隔离验证 | 保留未证明状态，不静默禁用 |

AWS依据：[AbortMultipartUpload](https://docs.aws.amazon.com/AmazonS3/latest/API/API_AbortMultipartUpload.html)；七牛依据：[上传策略](https://developer.qiniu.com/kodo/1206/put-policy)。其他调查与来源见`.local/profile-final-two-producer-feasibility.md`，其未决项不提升为供应商能力已证实。

每家实施前必须交付：精确对象/session/version归属；最后允许开始请求的屏障；已接收请求不会在结算后物化的证明；自动重试与响应丢失处理；权限最小集合；配置变化后仍能回到原位置的凭据解析方式。凭据撤销/不可用时保留阻塞，不回退当前桶或其他账户。

只增加multipart、扫描空session列表、HEAD404或DELETE204均不闭合“扫描结束后旧init才落地”的窗口。没有供应商保证时算法/测试不能制造保证。永久barrier、仅逻辑删除以及限制既有provider均另需产品批准，本方案未选择这些替代合同。

## 6. 旧数据与未知thumbnail

准确结论是“所查代码/字段/文档不足以可信恢复身份”，不是绝对不可恢复。未查询业务库，不断言真实旧问题行存在或不存在。

需要维护方提供最小非敏感证据摘要：部署过的writer版本、可恢复身份规则/manifest来源、是否仍有旧writer、受影响数量与存储范围。若这些材料不存在，继续标记未决。任何inventory、停服务、kill、补登记或真实删除另行授权；本设计不新增维护入口。不得用文件名/大小相似度猜归属，也不默认旧failed可删。

## 7. 分段实施候选与审批范围

| 包 | 候选交付 | 实施前条件 |
| --- | --- | --- |
| P1 | 业务失败/物理结算分离；新操作最小记录；unknown拒绝finalize | 明确批准持久合同及错误行为，临时迁移测试；不改旧行可信性 |
| P2 | 本地Go writer跨进程控制和迟到启动fence | Linux/Windows、共享介质、旧二进制共存策略的隔离验证计划获批 |
| P3 | ffmpeg实际写能力控制 | 选定可证明方案，明确媒体格式与部署影响后单独批准 |
| P4 | 单个具名远端adapter | 该端点通过第5节证据门并批准必要权限/行为；不能一次批准所有兼容端点 |
| P5 | 旧对象补登记/接管 | 有可信身份与停写证据并另获精确维护授权 |

用户已批准先实施P1，P2–P5仍各自待批。P1采用单份私有操作记录及现有生命周期事务，写前冻结身份；已正常返回的本地同步writer可结算，远端调用即使业务返回成功也保留物理unknown（尚无全部SDK请求终态证明）。P1保留既有远端业务成功发布行为，只新增物理未决删除保护；第3节完整可信发布协议仍是后续目标，不宣称P1已满足。旧无记录资源包括ready不自动补可信结算记录。只有临时测试fixture中已知实际对象可显式建立合成证据，不增加生产默认可信分支。P1部分交付不能完成5.2，也不实现跨进程接管或旧数据修复。

## 8. 必须留下的验证矩阵

采用确定性屏障控制次序，避免sleep/TTL模拟证明：

- SQLite双连接与PostgreSQL双实例：准入/删除、发布/删除两个先后；跨resource同identity；完整回滚和迟到token。
- 真正跨进程：持锁不能删除；writer开始前被fence；写中崩溃；写完DB回写前崩溃；旧进程迟到启动；锁文件替换/共享介质异常拒绝。
- 部分写后崩溃：恢复者取得锁并fence/settle后只能进入清理，不能发布ready；完整成功但成功证据丢失也不得自动发布。
- 受控HTTP反例：请求已接受未物化，客户端断开，HEAD404/DELETE204后再提交；系统仍不得finalize。SDK重试形成两次请求时不能只跟踪最后一次。
- 每家候选协议：init ID丢失、列表前后迟到init、part/Complete/Abort竞争、成功body丢失、已删当前版本仍有旧payload、权限撤销和配置变更。
- 普通MP4 seek/faststart与范围读取回归；子进程能存活时无法继续写目标；否则该路线不通过。
- 旧unknown不被默认迁移为settled，真实identity不足仍拒绝，可信资料不能跨用户/namespace误登记；现有alias/引用/历史/部分批次回归保持。

本轮仅文档与静态设计审查，没有新运行验收。mock证明实现算法，官方合同证明外部语义，隔离provider验证需另获授权；三者不可互相替代。全部5.2验收成立后仍须最终Verify。
