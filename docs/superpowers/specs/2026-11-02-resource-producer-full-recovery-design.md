---
comet_change: add-resource-producer-full-recovery
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-02-add-resource-producer-full-recovery
status: final
---

# 资源生产者全控制者丢失恢复与并发隔离技术设计文档

## 1. 背景与目标

现有 `c1PlaybackController` 实现了有限的 Windows C1 控制域与 `armed` 未启动恢复，但在控制者于 `attempted` 或 `closed` 阶段崩溃且失去内存句柄时，系统保守返回 `ErrResourceProducerUnsettled`。本设计在 `backend/internal/repository/resource_producer_full_recovery.go` 与 `backend/internal/app/resource_producer_full_recovery.go` 中实现：
1. **具名独占环境与持续保管合同门禁**（拒绝未绑定或 `CandidateOnly` 环境启动恢复）；
2. **全控制者退出后的原代际恢复校验**（绑定原 `GenerationID`、拒绝重建空域或同名空 Job 洗白、校验 `LateJoinSealed` 与 `AllWritersExited`）；
3. **多 Operation 并发隔离与业务结果/物理停止严格分离**（丢失编码回执时禁止凭可解码文件发布 `PlaybackStatusReady`）；
4. **跨平台证据不外推与 Windows 全句柄丢失保守阻断**（禁止用 Linux C2-A 证据替 Windows 签发恢复，Windows 全原句柄丢失且无持久终态时持续保持 `ErrResourceProducerUnsettled`）。

## 2. 核心数据结构

### 2.1 具名恢复环境合同 (`ProducerRecoveryEnvironmentContract`)
```go
type ProducerRecoveryEnvironmentContract struct {
	EnvironmentID        string `json:"environmentId"`
	Platform             string `json:"platform"` // "linux" | "windows"
	ImageSHA256          string `json:"imageSha256"`
	IsolatedRootPath     string `json:"isolatedRootPath"`
	AdminOwner           string `json:"adminOwner"`
	NamespaceUIDIsolated bool   `json:"namespaceUidIsolated"`
	NoNewPrivileges      bool   `json:"noNewPrivileges"`
	CustodyVerified      bool   `json:"custodyVerified"`
	AuthorizationTicket  string `json:"authorizationTicket"`
	CandidateOnly        bool   `json:"candidateOnly"`
}
```

### 2.2 原控制域代际恢复证明 (`ProducerDomainRecoveryProof`)
```go
type ProducerDomainRecoveryProof struct {
	Environment                   ProducerRecoveryEnvironmentContract `json:"environment"`
	OperationID                   string                              `json:"operationId"`
	ResourceID                    string                              `json:"resourceId"`
	UserID                        string                              `json:"userId"`
	ExpectedControlOwner          string                              `json:"expectedControlOwner"`
	OriginalGenerationID          string                              `json:"originalGenerationId"`
	ProofPlatform                 string                              `json:"proofPlatform"` // "linux" | "windows"
	AllControllersExited          bool                                `json:"allControllersExited"`
	BorrowedLegacyHandle          bool                                `json:"borrowedLegacyHandle"`
	RecreatedEmptyDomain          bool                                `json:"recreatedEmptyDomain"`
	SameNameJobAssumed            bool                                `json:"sameNameJobAssumed"`
	AllOriginalHandlesLost        bool                                `json:"allOriginalHandlesLost"`
	ParentFilesStructContinuous   bool                                `json:"parentFilesStructContinuous"`
	LateJoinSealed                bool                                `json:"lateJoinSealed"`
	AllWritersExited              bool                                `json:"allWritersExited"`
	VerifiedEncoderSuccessReceipt bool                                `json:"verifiedEncoderSuccessReceipt"`
	EncoderReceiptLost            bool                                `json:"encoderReceiptLost"`
	DecodableOutputFileExists     bool                                `json:"decodableOutputFileExists"`
}
```

## 3. 安全不变量与验证规则

1. **环境门槛 (`ValidateProducerRecoveryEnvironment`)**：
   - 若 `CandidateOnly == true` 或 `EnvironmentID == ""` 或 `ImageSHA256 == ""` 或 `IsolatedRootPath == ""` 或 `AdminOwner == ""` 或 `AuthorizationTicket == ""` 或 `!CustodyVerified` 或 `!NoNewPrivileges`，立即拒绝；
   - 若 `Platform == "linux"` 且 `!NamespaceUIDIsolated`，拒绝启动恢复。
2. **平台边界与全句柄丢失阻断**：
   - `ProofPlatform` 必须与 `Environment.Platform` 完全一致（拒绝将 `"linux"` 证据用于 `"windows"` 环境）；
   - 当 `Environment.Platform == "windows"` 且操作处于 `"attempted"` 或 `"closed"` 阶段且 `AllOriginalHandlesLost == true` 时，无论是否提供同名 Job 或监督者重启声明，一律返回 `repository.ErrResourceProducerUnsettled`，保留原 `writing` 状态与删除阻断。
3. **原代际恢复与反洗白校验**：
   - 若 `!AllControllersExited`、`BorrowedLegacyHandle`、`RecreatedEmptyDomain`、`SameNameJobAssumed`、`!ParentFilesStructContinuous`、`!LateJoinSealed` 或 `!AllWritersExited`，一律返回 `repository.ErrResourceProducerUnsettled`；
   - 必须在持有 `PlaybackControlGateIdentity(op.ID)`、`original` 物理锁与 `playback` 物理锁的前提下，核对数据库中的 `ResourceProducerOperation` 与 `ExpectedControlOwner`。
4. **并发隔离与结果/物理停止分离**：
   - 每个恢复操作仅按 `(OperationID, ResourceID, UserID, Identity)` 精确更新自身行，绝不修改同一资源或其他资源的并发 `ResourceProducerOperation`；
   - 若 `EncoderReceiptLost == true` 或 `!VerifiedEncoderSuccessReceipt`：即使 `DecodableOutputFileExists == true`，也只允许将 `ControlPhase` 推进至 `"stopped"`、`ControlResult` 记为 `"failed"`、`PhysicalState` 记为 `"settled"`、`Outcome` 记为 `"failed"`，并将 `Resource.PlaybackStatus` 更新为 `model.PlaybackStatusFailed`（绝不发布 `PlaybackStatusReady`）。
   - 仅当 `VerifiedEncoderSuccessReceipt == true && !EncoderReceiptLost` 时，才允许发布 `PlaybackStatusReady`。
