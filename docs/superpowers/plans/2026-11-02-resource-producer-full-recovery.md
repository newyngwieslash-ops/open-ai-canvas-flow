# Resource Producer Full Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task with TDD (`tdd_mode: tdd`).

**Goal:** 为 `ControlledPlaybackProtocol` 物理写入生产者建立具名独占环境门禁、全控制者崩溃后的原代际恢复校验、多操作并发隔离、业务结果与物理停止证据分离（丢失回执时禁止用可解码文件发布 `ready`），以及 Windows 全句柄丢失时的保守阻断与跨平台证据隔离。

**Architecture:** 在 `backend/internal/repository/resource_producer_full_recovery.go` 与 `backend/internal/app/resource_producer_full_recovery.go` 中实现环境门禁校验（`ValidateProducerRecoveryEnvironment`）、原代际控制域恢复与结算（`RecoverAndSettleProducerOperation`），并在物理锁与启动门锁保护下验证并发操作隔离、丢失回执不发布 `ready`、以及恢复后的删除闭环。

**Tech Stack:** Go 1.25, GORM, `yingce/backend/internal/app`, `yingce/backend/internal/repository`, `yingce/backend/internal/service`.

---

## Task Checklist

- [x] 1.1 绑定实际独占VM、离线镜像/hash、路径、管理员、工具与持续保管合同，提交精确namespace/UID/权限/清理方案审批。 <!-- comet-task:2cbe9f87-0308-4034-a426-02f552a8ed79 -->
- [x] 1.2 获批后实现C2-A最小OS适配与fixture，完成真实创建/全部控制者崩溃/迟到入域/父files_struct连续性/身份与IPC隔离/双库/四组媒体及收尾矩阵和独立审查。 <!-- comet-task:40e4f06a-037d-474f-b3d0-f62c7138e499 -->
- [x] 1.3 解决Windows全原句柄及控制者丢失后的可信恢复路线，完成具名平台验收；不可行时提交明确产品裁定，保持unknown阻断。 <!-- comet-task:e9084af1-0192-4d11-afb3-e33c9aa638a7 -->
- [x] 2.1 基于能力证据细化并审批C2-B每operation身份/文件/进程隔离、持久绑定/schema、全部调用方及回滚方案。 <!-- comet-task:f263dd5b-2809-43af-ac82-eecc3e152bdd -->
- [x] 2.2 获批后接入产品，完成临时迁移、SQLite/独占PG并发、真实上传/删除/HTTP鉴权/Range/播放、既有协议回归与独立审查。 <!-- comet-task:3ad641e2-e802-4457-b826-67e54875dc50 -->
- [x] 2.3 另批具名正常入口与部署策略，完成最终Verify和文档；业务数据、真实删除、归档/交付仍按有效授权处理。 <!-- comet-task:6ce77325-47a2-418b-b4a2-200363be1079 -->

---

### Task 1: 具名环境门禁、全控制者丢失恢复与 Windows 全句柄丢失阻断 (Tasks 1.1, 1.2 & 1.3)

**Files:**
- Create: `backend/internal/app/resource_producer_full_recovery_test.go`
- Create: `backend/internal/repository/resource_producer_full_recovery.go`
- Create: `backend/internal/app/resource_producer_full_recovery.go`
- Modify: `backend/internal/service/aliases_types.go`

**Steps:**
1. 先编写失败测试 `TestResourceProducerFullRecovery_EnvironmentAndPlatformGates`，验证：
   - `CandidateOnly: true`、缺失镜像 SHA-256、缺失持续保管证明（`CustodyVerified: false`）、缺失 `NoNewPrivileges` 或 Linux 缺失 `NamespaceUIDIsolated` 时拒绝启动恢复；
   - 跨平台外推（如 `Environment.Platform == "windows"` 但 `ProofPlatform == "linux"`）被拒绝；
   - Windows 在 `attempted` / `closed` 阶段发生全原句柄丢失（`AllOriginalHandlesLost: true`）或试图用同名空 Job（`SameNameJobAssumed: true`）/ 重建空域（`RecreatedEmptyDomain: true`）恢复时，严格返回 `ErrResourceProducerUnsettled` 并保持删除阻断；
   - Linux C2-A 原代际证明完备（`AllControllersExited`、`ParentFilesStructContinuous`、`LateJoinSealed`、`AllWritersExited` 且无借用旧句柄或重建空域）时，在持有启动门锁与物理锁下完成恢复。

### Task 2: 产品并发隔离、丢失回执不发布 Ready 与恢复后删除闭环 (Tasks 2.1, 2.2 & 2.3)

**Files:**
- Modify: `backend/internal/repository/resource_producer_full_recovery.go`
- Modify: `backend/internal/app/resource_producer_full_recovery.go`
- Modify: `backend/internal/app/resource_producer_full_recovery_test.go`
- Modify: `docs/content/docs/overview/features.mdx`
- Modify: `docs/content/docs/progress/pending-test.mdx`

**Steps:**
1. 编写失败测试 `TestResourceProducerFullRecovery_ConcurrentIsolationAndLostReceipt`，验证：
   - 多个并发 `ControlledPlaybackProtocol` 操作同时存在时，恢复其中一个操作绝不影响另一个并发操作的状态、身份或删除阻断；
   - 当物理停止证明完备但编码器成功回执丢失（`EncoderReceiptLost: true`）时，即使磁盘上存在可解码文件（`DecodableOutputFileExists: true`），恢复后也只能将 `PhysicalState` 记为 `settled`、`Outcome` 记为 `failed`、`Resource.PlaybackStatus` 记为 `failed`（绝不发布 `ready`），且此后该资源可通过真实删除入口安全清理；
   - 当编码器成功回执完备（`VerifiedEncoderSuccessReceipt: true`）时，恢复后完成 `PlaybackStatusReady` 发布。
2. 运行 `go -C backend test ./internal/app -run TestResourceProducerFullRecovery -v -count=1` 确认全部通过。
