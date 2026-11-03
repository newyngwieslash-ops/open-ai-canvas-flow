package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type LegacyResourceSnapshotExportRequest struct {
	ReadOnlyAuthTicket string   `json:"readOnlyAuthTicket"`
	ResourceIDs        []string `json:"resourceIds"`
}

type LegacyResourceSnapshotItem struct {
	ResourceID        string `json:"resourceId"`
	UserID            string `json:"userId"`
	Status            string `json:"status"`
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	Bucket            string `json:"bucket"`
	ObjectKey         string `json:"objectKey"`
	PlaybackObjectKey string `json:"playbackObjectKey"`
	PlaybackStatus    string `json:"playbackStatus"`
	ThumbnailStatus   string `json:"thumbnailStatus"`
	ThumbnailSize     int64  `json:"thumbnailSize"`
	SnapshotDigest    string `json:"snapshotDigest"`
}

type LegacyResourceEvidenceManifest struct {
	ManifestID          string                        `json:"manifestId"`
	SourceType          string                        `json:"sourceType"`
	ReadOnlyAuthTicket  string                        `json:"readOnlyAuthTicket"`
	ExportedAt          time.Time                     `json:"exportedAt"`
	Trusted             bool                          `json:"trusted"`
	ProvenanceSignature string                        `json:"provenanceSignature"`
	Entries             []LegacyResourceEvidenceEntry `json:"entries"`
}

type LegacyResourceEvidenceEntry struct {
	ResourceID        string                       `json:"resourceId"`
	UserID            string                       `json:"userId"`
	ExpectedStatus    string                       `json:"expectedStatus"`
	Provider          string                       `json:"provider"`
	Endpoint          string                       `json:"endpoint"`
	Bucket            string                       `json:"bucket"`
	ObjectKey         string                       `json:"objectKey"`
	PlaybackObjectKey string                       `json:"playbackObjectKey"`
	PlaybackStatus    string                       `json:"playbackStatus"`
	ThumbnailStatus   string                       `json:"thumbnailStatus"`
	ThumbnailSize     int64                        `json:"thumbnailSize"`
	SnapshotDigest    string                       `json:"snapshotDigest"`
	Variants          []LegacyResourceVariantProof `json:"variants"`
	WriterAttempts    []LegacyResourceWriterProof  `json:"writerAttempts"`
}

type LegacyResourceVariantProof struct {
	Variant        string `json:"variant"`
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint"`
	Bucket         string `json:"bucket"`
	ObjectKey      string `json:"objectKey"`
	Disposition    string `json:"disposition"`
	EvidenceSource string `json:"evidenceSource"`
}

type LegacyResourceWriterProof struct {
	AttemptID      string `json:"attemptId"`
	Variant        string `json:"variant"`
	Protocol       string `json:"protocol"`
	WriterID       string `json:"writerId"`
	TerminalState  string `json:"terminalState"`
	Outcome        string `json:"outcome"`
	StopWriteProof string `json:"stopWriteProof"`
}

type LegacyResourceEligibilityStatus string

const (
	LegacyResourceEligible LegacyResourceEligibilityStatus = "eligible"
	LegacyResourceBlocked  LegacyResourceEligibilityStatus = "blocked"
	LegacyResourceConflict LegacyResourceEligibilityStatus = "conflict"
)

type LegacyResourceItemEvaluation struct {
	ResourceID      string                          `json:"resourceId"`
	UserID          string                          `json:"userId"`
	Status          LegacyResourceEligibilityStatus `json:"status"`
	Reasons         []string                        `json:"reasons"`
	ExpectedActions []string                        `json:"expectedActions"`
	ItemDiffDigest  string                          `json:"itemDiffDigest"`
}

type LegacyResourceMaintenancePlan struct {
	ManifestID    string                         `json:"manifestId"`
	Items         []LegacyResourceItemEvaluation `json:"items"`
	DiffDigest    string                         `json:"diffDigest"`
	EligibleCount int                            `json:"eligibleCount"`
	BlockedCount  int                            `json:"blockedCount"`
	ConflictCount int                            `json:"conflictCount"`
}

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

type LegacyResourceMaintenanceResult struct {
	ManifestID            string                       `json:"manifestId"`
	ApprovalID            string                       `json:"approvalId"`
	Operation             string                       `json:"operation"`
	RegisteredResourceIDs []string                     `json:"registeredResourceIds"`
	DeletedResourceIDs    []string                     `json:"deletedResourceIds"`
	BlockedDeletes        []AdminResourceDeleteBlocked `json:"blockedDeletes"`
}

func computeLegacyManifestSignature(manifest LegacyResourceEvidenceManifest) string {
	copyManifest := manifest
	copyManifest.ProvenanceSignature = ""
	copyManifest.ExportedAt = copyManifest.ExportedAt.UTC().Truncate(time.Second)
	raw, _ := json.Marshal(copyManifest)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SignLegacyResourceEvidenceManifest binds the manifest provenance fields and
// all entries into a deterministic SHA-256 signature for verifiable transport.
func SignLegacyResourceEvidenceManifest(manifest LegacyResourceEvidenceManifest) LegacyResourceEvidenceManifest {
	manifest.ExportedAt = manifest.ExportedAt.UTC().Truncate(time.Second)
	manifest.ProvenanceSignature = computeLegacyManifestSignature(manifest)
	return manifest
}

func isObjectiveStopWriteProof(proof string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(proof))
	if cleaned == "" || len(cleaned) < 12 {
		return false
	}
	switch cleaned {
	case "operator-ack", "operator_ack", "manual-confirm", "process-exited", "self-declared", "trusted", "none", "unknown":
		return false
	}
	return true
}

// ExportLegacyResourceSnapshot performs a bounded read-only snapshot export only
// when an administrator supplies an explicit read-only authorization ticket and
// an explicit list of candidate resource IDs.
func (s *Service) ExportLegacyResourceSnapshot(actor *model.User, req LegacyResourceSnapshotExportRequest) ([]LegacyResourceSnapshotItem, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ReadOnlyAuthTicket) == "" {
		return nil, BadAuthRequest("导出旧资源快照必须提供精确只读授权凭据 readOnlyAuthTicket")
	}
	ids, err := normalizeAdminResourceDeleteIDs(req.ResourceIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.AdminResourcesByIDs(ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]model.Resource, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]LegacyResourceSnapshotItem, 0, len(ids))
	for _, id := range ids {
		row, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, LegacyResourceSnapshotItem{
			ResourceID:        row.ID,
			UserID:            row.UserID,
			Status:            string(row.Status),
			Provider:          normalizedResourceProvider(row.Provider),
			Endpoint:          row.Endpoint,
			Bucket:            row.Bucket,
			ObjectKey:         row.ObjectKey,
			PlaybackObjectKey: row.PlaybackObjectKey,
			PlaybackStatus:    row.PlaybackStatus,
			ThumbnailStatus:   row.ThumbnailStatus,
			ThumbnailSize:     row.ThumbnailSize,
			SnapshotDigest:    repository.LegacyResourceSnapshotDigest(&row),
		})
	}
	return out, nil
}

// EvaluateLegacyResourceMaintenance executes an isolated read-only dry-run over
// the manifest entries, classifying each resource as eligible, blocked, or
// conflict with machine-readable reasons and expected action diffs.
func (s *Service) EvaluateLegacyResourceMaintenance(actor *model.User, manifest LegacyResourceEvidenceManifest) (*LegacyResourceMaintenancePlan, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifest.ManifestID) == "" || len(manifest.Entries) == 0 {
		return nil, BadAuthRequest("维护清单 manifestId 和 entries 不能为空")
	}

	provenanceReasons := []string{}
	if strings.TrimSpace(manifest.SourceType) != "authorized-sanitized-export" || strings.TrimSpace(manifest.ReadOnlyAuthTicket) == "" {
		provenanceReasons = append(provenanceReasons, "unverified_provenance")
	}
	expectedSig := computeLegacyManifestSignature(manifest)
	if strings.TrimSpace(manifest.ProvenanceSignature) == "" || manifest.ProvenanceSignature != expectedSig {
		if manifest.Trusted {
			provenanceReasons = append(provenanceReasons, "untrusted_self_declaration")
		}
		provenanceReasons = append(provenanceReasons, "unverified_provenance")
	}

	candidateIDs := make([]string, 0, len(manifest.Entries))
	entryByID := make(map[string]LegacyResourceEvidenceEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		id := strings.TrimSpace(entry.ResourceID)
		if id != "" {
			candidateIDs = append(candidateIDs, id)
			entryByID[id] = entry
		}
	}
	rows, err := s.repo.AdminResourcesByIDs(candidateIDs)
	if err != nil {
		return nil, err
	}
	rowByID := make(map[string]model.Resource, len(rows))
	for _, row := range rows {
		rowByID[row.ID] = row
	}

	// First pass: evaluate each entry's own provenance, snapshot parity, variants, and writers.
	prelimEligible := make(map[string]bool, len(manifest.Entries))
	evals := make([]LegacyResourceItemEvaluation, 0, len(manifest.Entries))

	for _, entry := range manifest.Entries {
		eval := LegacyResourceItemEvaluation{
			ResourceID:      entry.ResourceID,
			UserID:          entry.UserID,
			Status:          LegacyResourceEligible,
			Reasons:         []string{},
			ExpectedActions: []string{},
		}
		addReason := func(level LegacyResourceEligibilityStatus, code string) {
			if !slices.Contains(eval.Reasons, code) {
				eval.Reasons = append(eval.Reasons, code)
			}
			if level == LegacyResourceConflict {
				eval.Status = LegacyResourceConflict
			} else if level == LegacyResourceBlocked && eval.Status == LegacyResourceEligible {
				eval.Status = LegacyResourceBlocked
			}
		}

		for _, reason := range provenanceReasons {
			addReason(LegacyResourceBlocked, reason)
		}

		row, exists := rowByID[entry.ResourceID]
		if !exists {
			addReason(LegacyResourceBlocked, "resource_not_found")
			evals = append(evals, eval)
			continue
		}

		if strings.TrimSpace(entry.UserID) != row.UserID {
			addReason(LegacyResourceConflict, "cross_user_mismatch")
		}

		expectedRow := model.Resource{
			ID:                entry.ResourceID,
			UserID:            entry.UserID,
			Status:            model.ResourceStatus(entry.ExpectedStatus),
			Provider:          entry.Provider,
			Endpoint:          entry.Endpoint,
			Bucket:            entry.Bucket,
			ObjectKey:         entry.ObjectKey,
			PlaybackObjectKey: entry.PlaybackObjectKey,
			PlaybackStatus:    entry.PlaybackStatus,
			ThumbnailStatus:   entry.ThumbnailStatus,
			ThumbnailSize:     entry.ThumbnailSize,
		}
		currentDigest := repository.LegacyResourceSnapshotDigest(&row)
		// Allow idempotent re-evaluation if the only difference is an already-cleared verified-absent thumbnail.
		drifted := false
		if strings.TrimSpace(entry.SnapshotDigest) == "" || currentDigest != entry.SnapshotDigest || repository.LegacyResourceSnapshotDigest(&expectedRow) != entry.SnapshotDigest {
			if row.ThumbnailStatus == "" && row.ThumbnailSize == 0 && (entry.ThumbnailStatus == "ready" || entry.ThumbnailSize > 0) {
				reconstructed := row
				reconstructed.ThumbnailStatus = entry.ThumbnailStatus
				reconstructed.ThumbnailSize = entry.ThumbnailSize
				if repository.LegacyResourceSnapshotDigest(&reconstructed) != entry.SnapshotDigest {
					drifted = true
				}
			} else {
				drifted = true
			}
		}
		if drifted {
			addReason(LegacyResourceConflict, "snapshot_drift")
		}

		if row.Status == model.ResourceStatusPending || row.Status == model.ResourceStatusDeleting || row.PlaybackStatus == model.PlaybackStatusProcessing {
			addReason(LegacyResourceBlocked, "resource_lifecycle_in_flight")
		}

		variantMap := map[string]LegacyResourceVariantProof{}
		for _, v := range entry.Variants {
			variantMap[v.Variant] = v
		}
		writersByVariant := map[string][]LegacyResourceWriterProof{}
		for _, w := range entry.WriterAttempts {
			writersByVariant[w.Variant] = append(writersByVariant[w.Variant], w)
		}

		// Check original variant proof & writers.
		origProof, hasOrig := variantMap["original"]
		if !hasOrig || origProof.Disposition != "verified-present" || strings.TrimSpace(origProof.EvidenceSource) == "" ||
			normalizedResourceProvider(origProof.Provider) != normalizedResourceProvider(row.Provider) ||
			origProof.Endpoint != row.Endpoint || origProof.Bucket != row.Bucket || origProof.ObjectKey != row.ObjectKey {
			addReason(LegacyResourceBlocked, "missing_or_mismatched_original_variant_proof")
		}
		origWriters := writersByVariant["original"]
		if len(origWriters) == 0 {
			addReason(LegacyResourceBlocked, "missing_writer_stop_proof")
		}
		for _, w := range origWriters {
			if strings.TrimSpace(w.WriterID) == "" || strings.TrimSpace(w.Protocol) == "" || !isObjectiveStopWriteProof(w.StopWriteProof) {
				addReason(LegacyResourceBlocked, "missing_writer_stop_proof")
			}
			if w.TerminalState != "settled" {
				addReason(LegacyResourceBlocked, "unsettled_writer_attempt")
			}
		}
		eval.ExpectedActions = append(eval.ExpectedActions, "register_settled_producer:original")

		// Check playback variant if present on row.
		if strings.TrimSpace(row.PlaybackObjectKey) != "" {
			playProof, hasPlay := variantMap["playback"]
			if !hasPlay || playProof.Disposition != "verified-present" || strings.TrimSpace(playProof.EvidenceSource) == "" || playProof.ObjectKey != row.PlaybackObjectKey {
				addReason(LegacyResourceBlocked, "missing_or_mismatched_playback_variant_proof")
			}
			playWriters := writersByVariant["playback"]
			if len(playWriters) == 0 {
				addReason(LegacyResourceBlocked, "missing_writer_stop_proof")
			}
			for _, w := range playWriters {
				if strings.TrimSpace(w.WriterID) == "" || strings.TrimSpace(w.Protocol) == "" || !isObjectiveStopWriteProof(w.StopWriteProof) {
					addReason(LegacyResourceBlocked, "missing_writer_stop_proof")
				}
				if w.TerminalState != "settled" {
					addReason(LegacyResourceBlocked, "unsettled_writer_attempt")
				}
			}
			eval.ExpectedActions = append(eval.ExpectedActions, "register_settled_producer:playback")
		}

		// Check legacy thumbnail state.
		if row.ThumbnailStatus == "ready" || row.ThumbnailSize > 0 || entry.ThumbnailStatus == "ready" || entry.ThumbnailSize > 0 {
			thumbProof, hasThumb := variantMap["thumbnail"]
			if !hasThumb || thumbProof.Disposition != "verified-absent" || strings.TrimSpace(thumbProof.EvidenceSource) == "" {
				addReason(LegacyResourceBlocked, "unknown_thumbnail_variant")
			}
			thumbWriters := writersByVariant["thumbnail"]
			if len(thumbWriters) == 0 {
				addReason(LegacyResourceBlocked, "unknown_thumbnail_variant")
			}
			for _, w := range thumbWriters {
				if strings.TrimSpace(w.WriterID) == "" || !isObjectiveStopWriteProof(w.StopWriteProof) || w.TerminalState != "settled" {
					addReason(LegacyResourceBlocked, "unknown_thumbnail_variant")
				}
			}
			eval.ExpectedActions = append(eval.ExpectedActions, "clear_verified_absent_thumbnail_metadata")
		}

		// Check existing DB producer operations for active/conflicting state.
		existingOps, err := s.repo.LegacyResourceProducerOperations(&row)
		if err != nil {
			return nil, err
		}
		origCanonical := repository.LegacyResourceProducerIdentity(&row, "original")
		playCanonical := repository.LegacyResourceProducerIdentity(&row, "playback")
		for _, op := range existingOps {
			if op.ResourceID == row.ID {
				expectedIdentity := origCanonical.Identity
				if op.Variant == "playback" {
					expectedIdentity = playCanonical.Identity
				}
				if op.Identity != expectedIdentity {
					addReason(LegacyResourceConflict, "active_or_conflicting_producer_operation")
				}
			}
			if op.PhysicalState == "writing" || op.PhysicalState == "reserved" {
				addReason(LegacyResourceConflict, "active_or_conflicting_producer_operation")
			}
		}

		if eval.Status == LegacyResourceEligible {
			prelimEligible[row.ID] = true
		}
		evals = append(evals, eval)
	}

	// Second pass: verify physical alias safety across all database resources.
	plan := &LegacyResourceMaintenancePlan{
		ManifestID: manifest.ManifestID,
		Items:      make([]LegacyResourceItemEvaluation, 0, len(evals)),
	}
	for i := range evals {
		eval := evals[i]
		if row, exists := rowByID[eval.ResourceID]; exists {
			aliases, err := s.repo.LegacyResourcePhysicalAliases(&row)
			if err != nil {
				return nil, err
			}
			for _, alias := range aliases {
				if alias.UserID != row.UserID {
					if !slices.Contains(eval.Reasons, "shared_alias_conflict") {
						eval.Reasons = append(eval.Reasons, "shared_alias_conflict")
					}
					eval.Status = LegacyResourceConflict
					continue
				}
				if !prelimEligible[alias.ID] && !s.repo.LegacyResourceHasSettledCoverage(&alias) {
					if !slices.Contains(eval.Reasons, "shared_alias_conflict") {
						eval.Reasons = append(eval.Reasons, "shared_alias_conflict")
					}
					eval.Status = LegacyResourceConflict
				}
			}
		}
		sort.Strings(eval.Reasons)
		sort.Strings(eval.ExpectedActions)
		itemSum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s",
			eval.ResourceID, eval.UserID, eval.Status,
			strings.Join(eval.Reasons, ","),
			strings.Join(eval.ExpectedActions, ","),
		)))
		eval.ItemDiffDigest = hex.EncodeToString(itemSum[:])
		switch eval.Status {
		case LegacyResourceEligible:
			plan.EligibleCount++
		case LegacyResourceBlocked:
			plan.BlockedCount++
		case LegacyResourceConflict:
			plan.ConflictCount++
		}
		plan.Items = append(plan.Items, eval)
	}

	planRaw, _ := json.Marshal(plan.Items)
	planSum := sha256.Sum256(planRaw)
	plan.DiffDigest = hex.EncodeToString(planSum[:])
	return plan, nil
}

// ApplyLegacyResourceMaintenance enforces per-item approval, backup/recovery/write-barrier
// confirmations, and exact diff digest matching before registering settled evidence or
// invoking the real resource deletion entrypoint (`DeleteAdminResources` + worker drain).
func (s *Service) ApplyLegacyResourceMaintenance(
	actor *model.User,
	manifest LegacyResourceEvidenceManifest,
	approval LegacyResourceMaintenanceApproval,
) (*LegacyResourceMaintenanceResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(approval.ApprovalID) == "" || strings.TrimSpace(approval.ManifestID) != strings.TrimSpace(manifest.ManifestID) {
		return nil, BadAuthRequest("维护授权 approvalId 与 manifestId 必须精确匹配")
	}
	if !approval.BackupConfirmed || !approval.RecoveryDrillVerified || !approval.WriteBarrierConfirmed || strings.TrimSpace(approval.MaintenanceWindow) == "" {
		return nil, BadAuthRequest("执行旧资源维护前必须确认备份恢复演练、写入屏障与维护窗口")
	}
	if approval.Operation != LegacyMaintenanceOpRegisterEvidence && approval.Operation != LegacyMaintenanceOpDeleteAfterEvidence {
		return nil, BadAuthRequest("未知的维护操作类型，必须明确指定 register_evidence 或 delete_after_evidence")
	}
	approvedIDs, err := normalizeAdminResourceDeleteIDs(approval.ApprovedResourceIDs)
	if err != nil {
		return nil, err
	}

	plan, err := s.EvaluateLegacyResourceMaintenance(actor, manifest)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(approval.ExpectedDiffDigest) == "" || approval.ExpectedDiffDigest != plan.DiffDigest {
		return nil, BadAuthRequest("当前快照或评估差异与审批摘要不一致，已拒绝执行")
	}

	evalByID := make(map[string]LegacyResourceItemEvaluation, len(plan.Items))
	for _, item := range plan.Items {
		evalByID[item.ResourceID] = item
	}
	entryByID := make(map[string]LegacyResourceEvidenceEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entryByID[entry.ResourceID] = entry
	}

	for _, id := range approvedIDs {
		eval, ok := evalByID[id]
		if !ok || eval.Status != LegacyResourceEligible {
			return nil, BadAuthRequest(fmt.Sprintf("资源 %s 未通过隔离资格校验（状态：%s），已整批阻断", id, eval.Status))
		}
	}

	registered := make([]string, 0, len(approvedIDs))
	for _, id := range approvedIDs {
		entry := entryByID[id]
		expectedRow := &model.Resource{
			ID:                entry.ResourceID,
			UserID:            entry.UserID,
			Status:            model.ResourceStatus(entry.ExpectedStatus),
			Provider:          entry.Provider,
			Endpoint:          entry.Endpoint,
			Bucket:            entry.Bucket,
			ObjectKey:         entry.ObjectKey,
			PlaybackObjectKey: entry.PlaybackObjectKey,
			PlaybackStatus:    entry.PlaybackStatus,
			ThumbnailStatus:   entry.ThumbnailStatus,
			ThumbnailSize:     entry.ThumbnailSize,
		}

		writersByVariant := map[string]LegacyResourceWriterProof{}
		for _, w := range entry.WriterAttempts {
			writersByVariant[w.Variant] = w
		}

		settledOps := []model.ResourceProducerOperation{}
		origWriter := writersByVariant["original"]
		origOp := repository.LegacyResourceProducerIdentity(expectedRow, "original")
		origOp.Protocol = origWriter.Protocol
		origOp.PhysicalState = "settled"
		origOp.Outcome = origWriter.Outcome
		origOp.Proof = fmt.Sprintf("legacy-manifest:%s:%s:%s", manifest.ManifestID, origWriter.AttemptID, origWriter.StopWriteProof)
		settledOps = append(settledOps, origOp)

		if strings.TrimSpace(expectedRow.PlaybackObjectKey) != "" {
			playWriter := writersByVariant["playback"]
			playOp := repository.LegacyResourceProducerIdentity(expectedRow, "playback")
			playOp.Protocol = playWriter.Protocol
			playOp.PhysicalState = "settled"
			playOp.Outcome = playWriter.Outcome
			playOp.Proof = fmt.Sprintf("legacy-manifest:%s:%s:%s", manifest.ManifestID, playWriter.AttemptID, playWriter.StopWriteProof)
			settledOps = append(settledOps, playOp)
		}

		clearThumb := entry.ThumbnailStatus == "ready" || entry.ThumbnailSize > 0
		auditEvent, err := newAdminAuditEvent(
			actor,
			"resource.legacy_maintenance."+string(approval.Operation),
			"resource",
			entry.ResourceID,
			"执行已授权的旧资源历史证据维护",
			map[string]any{
				"manifestId":        manifest.ManifestID,
				"approvalId":        approval.ApprovalID,
				"operation":         approval.Operation,
				"snapshotDigest":    entry.SnapshotDigest,
				"diffDigest":        plan.DiffDigest,
				"maintenanceWindow": approval.MaintenanceWindow,
				"clearedThumbnail":  clearThumb,
			},
		)
		if err != nil {
			return nil, err
		}

		if err := s.repo.RegisterLegacyResourceEvidence(
			expectedRow,
			entry.SnapshotDigest,
			settledOps,
			clearThumb,
			*auditEvent,
		); err != nil {
			return nil, err
		}
		registered = append(registered, entry.ResourceID)
	}

	result := &LegacyResourceMaintenanceResult{
		ManifestID:            manifest.ManifestID,
		ApprovalID:            approval.ApprovalID,
		Operation:             string(approval.Operation),
		RegisteredResourceIDs: registered,
		DeletedResourceIDs:    []string{},
		BlockedDeletes:        []AdminResourceDeleteBlocked{},
	}

	if approval.Operation == LegacyMaintenanceOpRegisterEvidence {
		return result, nil
	}

	// For delete_after_evidence, invoke the real DeleteAdminResources entrypoint
	// per approved resource so each resource receives an isolated ResourceDeletionIntent
	// (preventing a physical failure on one object from holding another independent
	// resource's finalization), then synchronously drain the queued deletion jobs.
	for _, resID := range registered {
		delRes, err := s.DeleteAdminResources(actor, AdminResourceDeleteRequest{
			ResourceIDs: []string{resID},
		})
		if err != nil {
			return nil, err
		}
		result.DeletedResourceIDs = append(result.DeletedResourceIDs, delRes.Deleted...)
		result.BlockedDeletes = append(result.BlockedDeletes, delRes.Blocked...)
	}
	for deadline := time.Now().Add(2 * time.Second); s.ActiveWorkerTasks() > 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	s.drainResourceDeletionJobs(len(registered) * 2)
	return result, nil
}
