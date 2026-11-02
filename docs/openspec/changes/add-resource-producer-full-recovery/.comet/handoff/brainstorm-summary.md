# Brainstorm Summary: add-resource-producer-full-recovery

## Core Problem
在 `ControlledPlaybackProtocol`（及本地生产者控制域）中，当原控制者进程在 `attempted` 或 `closed` 阶段崩溃且内存中无原控制句柄时：
1. 不能在未绑定具名独占环境、镜像哈希、UID/namespace 权限与持续保管合同前默认启用恢复入口或签发通过结论；
2. 新控制者恢复时必须校验持久化的原控制域代际（`GenerationID`）、启动门封闭证明（`LateJoinSealed`）、父进程 `files_struct` 连续性与全部写者退出证明（`AllWritersExited`），绝不能通过重建同名空域/空 Job 洗白旧写者；
3. 多个并发 `ResourceProducerOperation` 必须严格隔离各自身份、文件与控制域，且当物理停止已证明但编码器成功回执丢失时，绝不能凭磁盘上存在可解码文件补造 `PlaybackStatusReady`；
4. 平台证明严格隔离：Linux C2-A 的 `cgroup/namespace/files_struct` 证据不得外推至 Windows；Windows 在全部原句柄与持久终态均丢失时必须继续保持 `unknown` 并阻断删除（`ErrResourceProducerUnsettled`）。

## Approved Architecture & Decisions
1. **具名环境与持续保管门槛 (`ProducerRecoveryEnvironmentContract`)**：
   - 校验平台（`linux` | `windows`）、独占环境 ID、镜像 SHA-256、安全路径、管理员、UID/namespace 隔离配置、持续保管证明（`CustodyVerified`）与显式授权票据；若 `CandidateOnly: true` 或任一前提缺失，拒绝启用恢复并保持未执行状态。
2. **全控制者丢失的原代际恢复 (`RecoverControllerLostProducerOperation`)**：
   - 在 `PlaybackControlGateIdentity(op.ID)` 与资源物理锁保护下，校验原操作 `ID`、`ResourceID`、`UserID`、`Identity`、`ControlOwner` 及原代际绑定证明；
   - 拒绝 `RecreatedEmptyDomain`、`BorrowedOldHandle`、`SupervisorRestartAssumed` 或跨平台外推证明（例如在 `windows` 上提交 `linux` 控制域证明）；
   - 在 Windows 全原句柄丢失且无持久终态（`ControlPhase` 为 `attempted` 或 `closed` 且 `AllOriginalHandlesLost: true`）时，保持 `unknown` 并返回 `ErrResourceProducerUnsettled`。
3. **并发操作隔离与结果/物理结算分离 (`SettleRecoveredProducerOperation`)**：
   - 每个 `operation` 仅能推进自身绑定的 `(ResourceID, Variant, Identity)`，不影响同用户或跨用户的其他并发 `operation`；
   - 当物理停止证明完备（`AllWritersExited && LateJoinSealed`）但编码成功回执丢失（`EncoderReceiptLost: true`），即使磁盘上存在可解码输出文件（`DecodableOutputFileExists: true`），也只能将 `PhysicalState` 置为 `settled`、`Outcome` 置为 `failed`、并将资源的 `PlaybackStatus` 置为 `failed`，严禁发布 `PlaybackStatusReady`。
