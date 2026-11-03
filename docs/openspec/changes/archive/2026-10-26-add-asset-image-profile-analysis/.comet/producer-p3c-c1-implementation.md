# P3-C Windows C1 实施与验证

> 归档公开版仅以具名占位符脱敏临时实例标识；同名占位符指同一原实例。原始值保留在文内引用的本地证据中，历史失败、跳过、未完成状态及验证结论不变。

补充授权与最新媒体证据：[真实媒体补验](producer-media-runtime-results.md)已完成Windows C1四组真实上传/受控转码/HTTP及原生浏览器播放/seek，使用用户批准下载的私有ffmpeg9.0.2，未替换系统工具。原C1十二文件未改；下方“编码器缺失/媒体未验收/不下载”为此前实施阶段历史。完整恢复、正常入口、P4/P5及5.2仍未完成，不把新增媒体通过外推为完整P3-C通过。

状态：C1 最小实现、可运行的隔离验证及独立审查已交付；媒体/浏览器及完整 P3-C 验收仍未完成。依据 [已审查范围](producer-p3c-design.md)，仅三列持久状态、临时库迁移、测试组合根的 Windows 受控启动及恢复、隔离测试与独立审查。正常产品入口不启用，不操作业务库，不下载/替换编码器，不扩大 P4/P5 授权。Build 12/13、5.2 未完成、Verify pending、archived=false。

## 最终结果

实现三列及前向迁移61；Windows 以原子 JOB_LIST 创建、不可复用的操作代际和持久启动门控制新 playback 写者。可信原域可幂等停止、结算及恢复；全部控制句柄丢失且终态未提交保持 unknown。正常 Service、HTTP、环境配置与部署入口均未安装控制器，无新依赖或独立服务。

| 验证 | 最终证据与结果 |
| --- | --- |
| 源码 | fix1 实现 manifest `a7bb6dd36c868feb2115e1adf7283b00544fbef26c62fb84bb4a4573c3d56a55`；全 backend 2309 文件快照 manifest `b7b5a3d44e95f2593aec5f16f890ebebea89e431166c258f9499b51261c4cd9b`；冻结时 source/copy/hash 一致 |
| SQLite C1/迁移/P1迁移 | `profile-p3c-c1-env/matrix-sqlite-fix1.log`，app21.012s、database2.348s，exit0，无FAIL/SKIP |
| 独占 PostgreSQL C1/迁移 | `profile-p3c-c1-env/matrix-postgres-fix1.log`，app123.095s、database31.858s，exit0，无FAIL/SKIP；fresh及upgrade/repeat/rollback真实PG通过，混合命令中的P1Migration仍为SQLite，不计PG覆盖 |
| P1/P2/P3-B/C1/普通playback回归 | `profile-p3c-c1-regression-fix1.log`，app68.164s，通过 |
| 真实认证资源HTTP | `profile-p3c-c1-env/http-sqlite.log` handler5.933s，GET/区间/后缀/416/匿名/跨用户六例通过；合成字节，非编码或浏览器播放 |
| 独立审查 | `.local/profile-p3c-c1-review.md`；初审C1-R1实际RED/GREEN后唯一1/1定向复核PASS，独立app13.405s/database1.677s，无剩余阻断；既有历史不重置 |
| Runtime完整构建 | `358bb5d2-a3ce-4f21-a195-d1eff5a29005`，`go build ./...`（backend，mod readonly），exit0、reused=false、tier=full |

初次PG升级0A000及错误GOROOT环境日志均保留，最终结论来自修复冻结复验，不删除失败记录。自产PG身份核验一致、测试schema/public表/其他client均0后已精确停止；同归属pg_ctl确认server stopped、pidfile不存在、端口 `<C1_PG_PORT>`重新独占绑定成功。最终2309文件source/copy/manifest mismatch=0。详见 `.local/profile-p3c-c1-environment.md`、`postgres-stopped.json`；不将仅sandbox内pg_ctl status作为终止证明。

未执行后端全量测试、本轮Linux控制、真实编码成功或浏览器播放。Stop/Query权限不足为真实Windows API故障；deadline仅为wrapper边界注入，硬链接仅验证启动前替换拒绝，不外推任意恶意指令窗口。当前ffmpeg缺libx264，因此不宣称C1全验收清单或完整P3-C通过；P4/P5、正常启用、5.2和最终Verify均未解除。

以下保留开发、初审失败与集中修复历史，其中“待补/进行中”为当时状态，最终结论以上述冻结证据为准。

## 当前证据

- 迁移实际 RED：`.local/profile-p3c-c1-red.log`，`TestProducerC1MigrationColumns` 因缺少 control_owner 失败，日志 package database 2.018s（子例 0.30s）。
- 实现与专项日志由 `.local/profile-p3c-c1-implementation.md` 记录；冻结版本结果待补，不将开发中测试当最终证据。
- 独立低权限 PostgreSQL 环境见 `.local/profile-p3c-c1-environment.md`；仅本轮自产实例，冻结后才运行最终矩阵，退出前需停止并核对。
- 新增真实认证资源路由的合成字节测试。首轮 `.local/profile-p3c-c1-http.log` 仅 whole 子例因既有 Windows settings-key 首次发布的目录 Sync 失败，其余五例通过。已按仓库已有夹具预置临时合成密钥后复验，`.local/profile-p3c-c1-http-green.log` package handler 5.315s、六个子例通过，原失败日志保留。该测试只证明真实路由字节/鉴权合同，不是 C1 真实编码或浏览器播放验收。
- 实现独立审查 `.local/profile-p3c-c1-review.md` 已在候选冻结后进入初审；此前 P3-C 设计初审/唯一复核 PASS 不是本次实现审查。

## 候选冻结（最终审查和环境矩阵前）

实现者已冻结 12 个文件，清单 `.local/profile-p3c-c1-source-manifest.json`；主会话逐项核对 mismatch=0，go.mod/go.sum 与 P3-B 冻结基线相同。根新增的 handler 路由专项另列，不混入该清单。

- 迁移 fresh/upgrade/repeat/rollback/旧行保持：`.local/profile-p3c-c1-migration-green.log`，database 2.420s。
- 原子 claim 与无门 CAS 拒绝：`.local/profile-p3c-c1-state-red.log` app 3.326s 实际失败到 `state-green.log` app 3.364s 通过。
- Windows 候选完整专项：`.local/profile-p3c-c1-candidate.log` app 12.395s；随后状态拒绝/回滚及 escape 观测修正的专项另有记录，以环境同快照复验为最终矩阵。
- P1/P2/P3-B/C1 及普通 playback 联合回归：`.local/profile-p3c-c1-regression.log` app 49.093s 通过。
- 专属环境初次 setup 使用 Go1.25 二进制但继承旧 GOROOT 失败，保留 `profile-p3c-c1-env/matrix-sqlite.log`；修正测试工具链后用同冻结源码复验，不能将 setup 失败误记为产品通过。

真实 Job/后代/逃逸拒绝、删除与迟到启动、无 intent 结果恢复、五个控制进程崩溃窗口、持久提交失败的原句柄保留均有专项；真实媒体与浏览器播放未验收。独立初审和 SQLite/PG 环境结果尚待收齐，本节不是最终通过声明。

## 初轮矩阵与集中修复输入

初轮完整 backend 冻结快照为 2309 文件，manifest `55fbf5ab1c9a14316b759194901473f8fb86a537391360013d7522d57843a502`，source/copy/manifest mismatch=0。

- 同快照 SQLite app 17.225s、database 1.526s 通过，无 skip；独立合成字节 HTTP handler 5.933s 通过。
- 同快照 PostgreSQL app 全部 C1 专项 84.066s 通过；database fresh 通过，但 upgrade/repeat/rollback 最终读旧行触发 `SQLSTATE 0A000: cached plan must not change result type`。日志 `profile-p3c-c1-env/matrix-postgres.log` 保留，整体矩阵失败；不能写作 PG 验收通过。
- 独立实现初审 NEEDS_REVISION：C1-R1 为同 owner 仍持原 Job 句柄时，recover 直接将 attempted/closed 留 unknown，不能重试暂时失败的持久终态。已有故障专项直接再次 CloseAndStop，未证明真正恢复入口能推进。修复须走同 owner 的可信原域重试，冷控制器仍保持 unknown，并补无 intent 的恢复入口测试。
- 审查者独立运行 C1/迁移/合成路由专项 app 16.391s、database 1.794s、handler 6.457s，通过；此为 SQLite 结果，不能替代 PG 失败或关闭 C1-R1。

实现者正在一次性调查和集中修复 C1-R1/PG 问题，并补最小故障/路径/顺序证据。此前设计及 P3-B 审查历史保持，唯一实现定向复核尚未消费；待 fix1 冻结后复验，不对中间状态宣称通过。

## 集中修复版 fix1

12 文件清单 `.local/profile-p3c-c1-source-manifest-fix1.json`，manifest `a7bb6dd36c868feb2115e1adf7283b00544fbef26c62fb84bb4a4573c3d56a55`；主会话再次核对 mismatch=0。修复不增加 schema 或依赖。

- C1-R1：`.local/profile-p3c-c1-r1-red.log` app 4.706s 真实失败到 `r1-green.log` app 4.687s 通过。当前 owner 且仍持精确可信原域时，恢复入口重试原停止/终态处理；新控制器缺句柄仍 unknown。两种提交故障解除后经真实 recover 的无 intent 结算均通过。
- PG：`pg-diagnostic-red.log` database 6.099s 保存列序和 0A000；改为测试 before/after 的 QueryFields 显式全模型列序后 `pg-diagnostic-green.log` database 8.541s 通过，仍 DeepEqual 全字段。未更改生产缓存/迁移或跳过旧行断言，最终完整 PG 复验另列。
- 故障扩展：`fault-expanded-green.log` app 7.625s。真实 Windows Job 的 Terminate/Query 权限不足保留未决；deadline 为 wrapper 错误边界注入，不宣称实测自然 OS 停止超时；真实硬链接替换在创建前拒绝；父子写者实际写入本次 playback 目标，独立事件记录顺序为屏障、停写、删除。
- 本地全 C1 + C1 迁移及 P1 迁移：`fix1-local.log` app 16.082s、database 1.828s 通过。最终环境矩阵、唯一复核和 Runtime build 进行中，尚不据此宣称整个 C1 验收通过。

## 保留验收边界

现有 Windows 编码器缺 libx264；不以 mpeg4、合成字节或历史 Linux 媒体结果替代。全部可信句柄丢失且终态未提交仍 unknown，必须保留记录/对象/屏障，不能声称完整崩溃恢复通过。C1 的有限实现、实际进程能力、媒体及真实路由分别报告；缺必要证据不宣称整个 C1 验收通过，不勾 5.2，不运行 Guard 或最终 Verify。
