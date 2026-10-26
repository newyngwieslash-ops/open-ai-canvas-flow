# C2-A 私有账本与临时数据库检查

> 归档公开版仅以具名占位符脱敏临时实例标识；同名占位符指同一原实例。原始值保留在文内引用的本地证据中，历史失败、跳过、未完成状态及验证结论不变。

本包实施依据 [剩余恢复设计](producer-full-recovery-design.md) 第4、6节。用户已批准私有验证代码及临时 SQLite / 独占 PostgreSQL 检查；真实 OS 环境另批。关联任务5.2，仍未完成。配置 full / zh-CN / autonomous / tdd / standard / current，既有包和审查历史保留。

## 范围及证据解释

源码仅在忽略目录 `.local/profile-c2a-ledger/`，环境及冻结日志在 `.local/profile-c2a-env/`。不接产品，不修改产品 schema/model/migration/依赖，不操作业务库。数据库辅助子进程只证明 gate/CAS 的数据库竞争；模型观察、清理计数和结果来源夹具只证明账本约束，不能证明 Linux cgroup 持续保管、真实进程停止或 Windows 全句柄丢失恢复。

真实 cgroup、writer、ffmpeg 和 namespace/UID/权限实验未获批准，本包不执行。C2-B 产品接入、正常入口、P4/P5及最终5.2验收边界保持。

## 验收覆盖与交付

账本子包已完成 TDD、集中修复、最终冻结同源双库矩阵及唯一 1/1 定向复核 PASS。初审两项 IMPORTANT 均关闭，历史与失败反例保留。真实结果如下，不替代完整 C2-A 的 OS 环境与能力验收：

| 组 | 必须验证 |
| --- | --- |
| binding | 2KiB限制、严格字段/类型/数值、重复/未知键、缺字段/尾随、版本、操作派生路径 |
| 单调状态 | reserved/attached/absent组合、不可覆盖身份、token/fence、claim/attach/attempted/closed/stopped |
| 未启动恢复 | 无域原子absent、有归属空域原子attach+stop、失败回滚/重入、未知来源拒绝 |
| 结算纪律 | closed先提交、stopped先提交才cleanup、失败保留义务、结果丢失unknown、absent禁止ready |
| 临时数据库 | SQLite WAL及专属低权限PG、两个真实测试进程、launch赢/close赢、真实锁冲突/等待 |
| 独立审查 | 初审两项 IMPORTANT 集中修复，唯一 1/1 定向复核 PASS；原反例不改断言变 GREEN，剩余 CRITICAL/IMPORTANT/MINOR 均0 |
| 收尾 | 新PG身份/public/schema/client核对干净，精确停止成功；PID文件消失、同归属status无服务、端口可独占绑定，OS能力NOT RUN |

实现包含严格 binding 解析、具体单调 CAS、未启动原子恢复、故障回滚与模型清理/结算。两库每个竞争方向都启动两个实际数据库辅助进程，由事件握手控制；SQLite 实际返回 `SQLITE_BUSY`，PostgreSQL 实际返回 `55P03`，并核对先提交状态和迟到操作结果。精确终止本次持门数据库 helper 后，新连接获取释放的门并恢复账本；该测试不等于全部 OS 控制者丢失实验。

可编译拒绝骨架对合法输入/转换产生真实 RED；损坏持久 binding 与终态覆盖的新增反例也真实 RED，随后全部 GREEN。未把初始编译失败或依赖诊断错误算作行为 RED。

初版7文件源清单 `.local/profile-c2a-ledger/source-manifest.json` SHA256 `44dba646829cfb2a07a6b7f08661c4aebee73c154e81ccde9bd95443d30f1a18`。实现者全矩阵 exit0/2.481s；环境执行者核对清单/current/copy mismatch0 后，从独立冻结副本运行 SQLite 专项 exit0/1.575s、SQLite+PG全矩阵 exit0/2.614s，均无 FAIL/SKIP。PG仅本次新独占 loopback 集群与低权限角色，测试前后身份一致、public表0、专属schema0、其他client0。

初审两项 IMPORTANT 已集中修复并冻结 fix1：fenced success 可结算但初次及幂等返回都不 ready；PG 使用同连接 session advisory gate 与账本短事务，SQLite 用 `Conn.Raw` 锁住本地门连接覆盖账本提交，避免一次存活检查后的失锁窗口。门连接禁止 idle 返池，释放失败丢弃连接。新增双库反例从真实 RED 到 GREEN，覆盖旧 holder 在竞争者持门后不得提交 attempted/终态/cleaned、PG 在实际 UPDATE 触发器内精确断开自身门 backend、SQLite 连接关闭窗口及释放失败。fix1 清单 `.local/profile-c2a-ledger/source-manifest-fix1.json` SHA256 `c0778a5cfa8b3a271ca1da9565b1baaccfed6508f6a04b478df5b5fd970939e5`。环境执行者最终同 hash 冻结复跑：SQLite 专项 exit0/1.887s，全矩阵 exit0/12.573s，无 FAIL/SKIP；7源 current/copy/manifest mismatch0，PG 后验身份相同、public表/schema/其他client均0。

原独立审查者完成唯一 1/1 定向复核，限定本子包 PASS。最终7源 hash mismatch0，原两反例不改断言变 GREEN；独立双库修复专项 exit0/5.232s，无 FAIL/SKIP，覆盖此次发现、修复及新风险。报告 `.local/profile-c2a-review.md`，初审和原始失败保留，不重置此前审查历史或追加第二轮复核。

根会话核对既有后端与媒体 2313 文件，全部 hash 与既有冻结证据一致（`.local/profile-c2a-product-full-boundary.json`）；此次实现只在私有目录。

独审后再次核对本次新实例目录/systemID/loopback端口与原PID，public表/schema/其他client均0；只对本次目录执行 `pg_ctl fast stop`，exit0/server stopped。同归属 status exit3、postmaster.pid不存在、loopback端口 `<C2A_PG_PORT>`独占绑定通过，原启动句柄亦已terminal。证据 `.local/profile-c2a-env/postgres-stopped.json`，数据与日志保留，未操作既有服务。

实施报告 `.local/profile-c2a-implementation.md`；环境报告 `.local/profile-c2a-environment.md`；新包审查报告 `.local/profile-c2a-review.md`。没有 OS adapter、具名环境验证器或实际域/进程 fixture；默认拒绝函数只表示授权边界，不计作真实环境负检通过。最终账本子包通过也不满足完整 C2-A 或 P3/5.2 验收，不勾5.2，不运行Guard或最终Verify。
