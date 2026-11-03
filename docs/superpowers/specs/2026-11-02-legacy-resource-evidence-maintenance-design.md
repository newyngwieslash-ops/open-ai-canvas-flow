---
comet_change: add-legacy-resource-evidence-maintenance
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-02-add-legacy-resource-evidence-maintenance
status: final
---

# 旧资源可信证据与隔离维护技术设计文档

## 1. 背景与目标

在现有资源生命周期安全合同中：
1. `Repository.requireResourceProducers` 要求待删除资源的每个变体（`original` 及存在的 `playback`）以及所有共享同一物理身份的 alias 行都具备 `PhysicalState == "settled"` 的 `ResourceProducerOperation` 记录，否则返回 `ErrResourceProducerUnsettled`。
2. `Repository.PrepareResourceDeletion` 对 `ThumbnailStatus == "ready" || ThumbnailSize > 0` 的旧资源返回 `"thumbnail object identity unavailable; deletion retained for retry"`，防止未知位置的旧缩略图逃逸成孤儿文件或被误删。

本设计建立一套**可核验历史证据清单 + 隔离 Dry-Run 判定 + 逐项授权维护与真实删除入口验证**机制，确保：
- 仅在历史身份、归属、变体位置和全部历史 writer 停写证明完备且与当前快照一致时，才判定为 `eligible`；
- 伪造/自报 `trusted: true`、缺少 writer 停写证据、存在未知缩略图变体、快照过期漂移、跨用户归属不符、或存在未覆盖的共享物理 alias 时，一律判定为 `blocked` 或 `conflict`；
- 任何维护动作必须绑定精确的 `ApprovedResourceIDs`、`ExpectedDiffDigest`、备份确认、写入屏障确认与分项操作权限（`register_evidence` 与 `delete_after_evidence` 严格区分），并在隔离副本中通过真实删除入口验证。

## 2. 核心数据结构 (`backend/internal/app/legacy_resource_maintenance.go` & `backend/internal/repository/legacy_resource_maintenance.go`)

### 2.1 历史证据清单 (`LegacyResourceEvidenceManifest`)
```go
type LegacyResourceEvidenceManifest struct {
	ManifestID          string                        `json:"manifestId"`
	SourceType          string                        `json:"sourceType"`          // e.g. "authorized-sanitized-export"
	ReadOnlyAuthTicket  string                        `json:"readOnlyAuthTicket"`  // 精确只读授权凭据
	ExportedAt          time.Time                     `json:"exportedAt"`
	Trusted             bool                          `json:"trusted"`             // 自报标记，不能单独作为可信依据
	ProvenanceSignature string                        `json:"provenanceSignature"` // SHA-256 绑定来源与条目摘要
	Entries             []LegacyResourceEvidenceEntry `json:"entries"`
}

type LegacyResourceEvidenceEntry struct {
	ResourceID        string                         `json:"resourceId"`
	UserID            string                         `json:"userId"`
	ExpectedStatus    string                         `json:"expectedStatus"`
	Provider          string                         `json:"provider"`
	Endpoint          string                         `json:"endpoint"`
	Bucket            string                         `json:"bucket"`
	ObjectKey         string                         `json:"objectKey"`
	PlaybackObjectKey string                         `json:"playbackObjectKey"`
	PlaybackStatus    string                         `json:"playbackStatus"`
	ThumbnailStatus   string                         `json:"thumbnailStatus"`
	ThumbnailSize     int64                          `json:"thumbnailSize"`
	SnapshotDigest    string                         `json:"snapshotDigest"`
	Variants          []LegacyResourceVariantProof   `json:"variants"`
	WriterAttempts    []LegacyResourceWriterProof    `json:"writerAttempts"`
}

type LegacyResourceVariantProof struct {
	Variant        string `json:"variant"`        // "original" | "playback" | "thumbnail"
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint"`
	Bucket         string `json:"bucket"`
	ObjectKey      string `json:"objectKey"`
	Disposition    string `json:"disposition"`    // "verified-present" | "verified-absent" | "unknown"
	EvidenceSource string `json:"evidenceSource"` // e.g. "storage-inventory-v1", "thumbnail-absent-proof-v1"
}

type LegacyResourceWriterProof struct {
	AttemptID      string `json:"attemptId"`
	Variant        string `json:"variant"`        // "original" | "playback" | "thumbnail"
	Protocol       string `json:"protocol"`
	WriterID       string `json:"writerId"`
	TerminalState  string `json:"terminalState"`  // "settled" | "unknown" | "writing"
	Outcome        string `json:"outcome"`        // "success" | "failed"
	StopWriteProof string `json:"stopWriteProof"` // 具名客观停写证明，禁止为空或仅填 "operator-ack"
}
```

### 2.2 Dry-Run 评估输出 (`LegacyResourceMaintenancePlan`)
```go
type LegacyResourceEligibilityStatus string

const (
	LegacyResourceEligible LegacyResourceEligibilityStatus = "eligible"
	LegacyResourceBlocked  LegacyResourceEligibilityStatus = "blocked"
	LegacyResourceConflict LegacyResourceEligibilityStatus = "conflict"
)

type LegacyResourceItemEvaluation struct {
	ResourceID     string                          `json:"resourceId"`
	UserID         string                          `json:"userId"`
	Status         LegacyResourceEligibilityStatus `json:"status"`
	Reasons        []string                        `json:"reasons"`
	ExpectedActions []string                       `json:"expectedActions"`
	ItemDiffDigest string                          `json:"itemDiffDigest"`
}

type LegacyResourceMaintenancePlan struct {
	ManifestID     string                         `json:"manifestId"`
	Items          []LegacyResourceItemEvaluation `json:"items"`
	DiffDigest     string                         `json:"diffDigest"`
	EligibleCount  int                            `json:"eligibleCount"`
	BlockedCount   int                            `json:"blockedCount"`
	ConflictCount  int                            `json:"conflictCount"`
}
```

### 2.3 逐项维护授权 (`LegacyResourceMaintenanceApproval`)
```go
type LegacyResourceMaintenanceOperation string

const (
	LegacyMaintenanceOpRegisterEvidence    LegacyResourceMaintenanceOperation = "register_evidence"
	LegacyMaintenanceOpDeleteAfterEvidence LegacyResourceMaintenanceOperation = "delete_after_evidence"
)

type LegacyResourceMaintenanceApproval struct {
	ApprovalID            string                             `json:"approvalId"`
	ActorUserID           string                             `json:"actorUserId"`
	ManifestID            string                             `json:"manifestId"`
	Operation             LegacyResourceMaintenanceOperation `json:"operation"`
	ApprovedResourceIDs   []string                           `json:"approvedResourceIds"`
	ExpectedDiffDigest    string                             `json:"expectedDiffDigest"`
	BackupConfirmed       bool                               `json:"backupConfirmed"`
	RecoveryDrillVerified bool                               `json:"recoveryDrillVerified"`
	WriteBarrierConfirmed bool                               `json:"writeBarrierConfirmed"`
	MaintenanceWindow     string                             `json:"maintenanceWindow"`
}
```

## 3. 判定规则与安全不变量

1. **来源与完整性校验**：
   - `ManifestID`、`SourceType == "authorized-sanitized-export"`、`ReadOnlyAuthTicket` 均不能为空；
   - `ProvenanceSignature` 必须匹配基于规范化条目计算的 SHA-256 摘要；仅设置 `Trusted: true` 而签名不匹配或来源字段缺失时，返回 `untrusted_self_declaration` / `unverified_provenance` 并阻断。
2. **快照一致性与归属校验**：
   - 读取当前数据库中的 `model.Resource`，若记录不存在则判为 `blocked`（`resource_not_found`）；
   - 若 `row.UserID != entry.UserID` 判为 `conflict`（`cross_user_mismatch`）；
   - 若当前行的 `Status`、`Provider`、`Endpoint`、`Bucket`、`ObjectKey`、`PlaybackObjectKey`、`PlaybackStatus`、`ThumbnailStatus`、`ThumbnailSize` 或计算出的 `SnapshotDigest` 与 `entry` 不一致，判为 `conflict`（`snapshot_drift`）。
3. **变体与未知缩略图校验**：
   - `original` 变体必须在 `entry.Variants` 中存在且 `Disposition == "verified-present"`、身份与当前行完全一致；
   - 若 `row.PlaybackObjectKey != "" || row.PlaybackStatus != ""`，`playback` 变体必须存在且 `Disposition` 为 `verified-present` 或 `verified-absent`，且 `row.PlaybackStatus != model.PlaybackStatusProcessing`；
   - 若 `row.ThumbnailStatus == "ready" || row.ThumbnailSize > 0`，`entry.Variants` 必须提供 `thumbnail` 变体证明：
     - 若 `Disposition == "unknown"` 或缺失 `EvidenceSource` 或缺失对应 writer 停写证明，判为 `blocked`（`unknown_thumbnail_variant`）；
     - 仅当 `Disposition == "verified-absent"`（已证实从未物化或已在授权前核验不存在）且具备 `thumbnail` 变体停写证据时，才允许在维护事务中将 `ThumbnailStatus` 置为 `""`、`ThumbnailSize` 置为 `0`。
4. **全部历史 Writer 停写证明校验**：
   - 每个涉及变体必须至少有一条 `LegacyResourceWriterProof`；
   - 任何 attempt 的 `TerminalState != "settled"`、`StopWriteProof == ""`、或 `StopWriteProof == "operator-ack"` / `"process-exited"` / `"self-declared"` 等非客观停写证明，一律判为 `blocked`（`missing_writer_stop_proof` 或 `unsettled_writer_attempt`）；
   - 数据库中已有的 `ResourceProducerOperation` 若存在 `PhysicalState != "settled"` 且无法与已证实 `settled` 的历史 attempt 对应，或身份冲突，判为 `conflict`（`active_or_conflicting_producer_operation`）。
5. **共享物理 Alias 校验**：
   - 扫描数据库中所有共享同一 `original` 或 `playback` 物理身份的其他 `model.Resource`；
   - 若存在其他属于不同用户的 alias 行，或存在未在当前 manifest 中同批具备完整 `eligible` 证据且未在数据库中拥有 `settled` `ResourceProducerOperation` 的 alias 行，判为 `conflict`（`shared_alias_conflict`）。

## 4. 逐项授权执行与真实入口验证

- `ApplyLegacyResourceMaintenance(ctx, manifest, approval)`：
  1. 校验 `approval` 完整性：必须包含非空 `ApprovalID`、`ActorUserID`、`ManifestID == manifest.ManifestID`、`BackupConfirmed == true`、`RecoveryDrillVerified == true`、`WriteBarrierConfirmed == true`、`MaintenanceWindow != ""`，且 `Operation` 仅限 `register_evidence` 或 `delete_after_evidence`。
  2. 重新运行 `EvaluateLegacyResourceMaintenance(manifest)`，校验 `plan.DiffDigest == approval.ExpectedDiffDigest`，确保审批后到执行前无任何快照漂移。
  3. 校验 `approval.ApprovedResourceIDs` 非空，且其中每一项在最新 `plan` 中均为 `eligible`（任何 `blocked` 或 `conflict` 或未在 manifest 中的 ID 立即拒绝整个批次，不执行部分猜测）。
  4. 在 `ResourceLifecycleTransaction` 内逐个对批准资源：
     - 将 `unknown` / `legacy-untracked` 或缺失的 `ResourceProducerOperation` 登记/更新为 `PhysicalState: "settled"`，`Proof: "legacy-manifest:" + manifest.ManifestID + ":" + writer.StopWriteProof`；
     - 对已核验 `thumbnail` 为 `verified-absent` 的旧行，将 `ThumbnailStatus` 更新为 `""`、`ThumbnailSize` 更新为 `0`；
     - 写入 `model.AdminAuditEvent`（包含 `manifestId`、`approvalId`、`operation`、`resourceId`、`diffDigest`）。
  5. 若 `approval.Operation == LegacyMaintenanceOpRegisterEvidence`，到此完成并返回结果，**绝不触发删除**。
  6. 若 `approval.Operation == LegacyMaintenanceOpDeleteAfterEvidence`，对每个已补登记的资源按其真实所属用户调用 `s.DeleteUserResource(row.UserID, row.ID)` 并执行 `s.drainResourceDeletionJobs(len(approval.ApprovedResourceIDs) * 2)`；若资源仍被业务对象引用或物理删除失败，真实入口将按既有强校验合同阻断或保留记录以便重试，绝不伪造删除成功或静默清空记录。
