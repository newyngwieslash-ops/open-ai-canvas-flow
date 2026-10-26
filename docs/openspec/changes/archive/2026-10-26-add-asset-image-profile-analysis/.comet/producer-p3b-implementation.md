# P3-B 产品事务接入记录

用户已批准最小实施、临时库测试和独立审查。范围依据 [已审查方案](producer-p3-product-integration.md)。P3-B实现及集中修复已冻结，最终临时库测试与构建通过，唯一1/1独立定向复核PASS。本结论仅限P3-B，不完成5.2。

## 范围

复用既有 operation/token、规范化身份、删除意图及 worker，补原子 playback 发布与受限停止屏障。测试控制器仅证明产品事务顺序，不能提供 OS 停写证明。默认产品路径不启用未经验证的新控制能力；不新增 schema、迁移或依赖，不操作业务库。P3-C、P4/P5 边界保留。

## 证据

- RED：`.local/profile-p3b-red.log`，实际 app 测试复现 canonical 同路径别名绕过墓碑及无 token 发布 playback 两项失败，exit1、3.621s。
- 初轮实现与专项结果：`.local/profile-p3b-implementation.md`，SQLite专项10.689s、P1/P2/P3及正常播放联合回归31.428s通过；环境代理冻结快照SQLite11.015s通过。不是最终修复版本证据。
- 隔离 PostgreSQL：`.local/profile-p3b-environment.md`，本轮独立测试实例和低权限测试角色；未使用业务配置，最终需停止自产实例。
- 独立初审：`.local/profile-p3b-review.md`，NEEDS_REVISION。B1控制器存在时纯P2恢复回归、B2受控正常成功发布通道、B3持久失败/真实进程重入测试集中修复；独立初轮SQLite15.972s通过不替代这些缺口。唯一1/1定向复核尚未消费，此前设计和实验审查不重置。
- 初轮隔离PG app101.133s失败：start竞争场景未捕获driver错误，stage观察55P03通过。旧日志`.local/profile-p3b-env/env-postgres.log`保留；已定位产品owner事务的SET LOCAL超时覆盖测试设置导致与context超时竞速，修订仅限隔离测试观测，待新冻结复验。

任务 5.2 保持未完成；本包通过亦不等于完整 ffmpeg 崩溃恢复、真实远端终态或旧数据接管验收。当前 Build 12/13、verify pending、archived=false；不越过 Guard。

## 集中修复后的构建

最终冻结联合回归 `.local/profile-p3b-review-fix-regression-final.log` app32.300s通过；后端 Runtime full build `bb4a12bc-e9ff-4658-b692-4f74e06cadbf` exit0、reused=false，命令 `go build ./...`（cwd backend，mod readonly）。这只证明构建，不替代独立定向复核，不运行Guard或最终Verify。

最终矩阵：`.local/profile-p3b-env/matrix-sqlite-fix1.log` app24.944s，`matrix-postgres-fix1.log` app146.347s，无FAIL/SKIP；PG定向14.152s，start/stage两方向均实际55P03。2301文件当前源码与冻结快照一致，manifest `0598ac16eb64969e690e29816c1998edf96f18bc2c9f62c83f6d18d49661715e`。自产PG已停止，测试数据和首轮失败日志保留；未操作业务库或已有服务。

B1增加空控制能力及P2/P3混合批次回归，保留普通P2恢复；B2以可信控制器返回的精确操作回执区分停止和编码结果，原子发布，调用前和锁内均核验；B3覆盖结算回滚、删除/完成失败重试、第二操作停止失败、停止中租约接管，以及屏障/停止/结算三个窗口的真实自产子进程退出，由不同子进程运行worker恢复。测试替身不构成真实OS恢复证明。

## 最终独立复核与边界

`.local/profile-p3b-review.md`的唯一1/1定向复核关闭B1–B3及PG证据缺口，无新增阻断；审查者独立SQLite专项9.578s通过，包括三个真实子进程退出/新进程恢复场景。settled-crash为直接repository结算后的持久边界，不声称所有OS/worker指令位置均已覆盖。普通ffmpeg Wait不授予受控协议的整域结算权，产品没有安装控制器。模型、schema/migrations及go.mod/go.sum六项与本轮基线内容一致；未运行后端全量测试或本轮Linux/真实ffmpeg矩阵。P3-C、P4/P5、业务维护与真实服务启用均保留原授权边界。
