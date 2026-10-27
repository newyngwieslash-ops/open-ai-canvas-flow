package repository

import (
	"context"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// This protocol is deliberately not selected by the product ffmpeg launcher.
// P3-C must supply a verified controller before the app may register it.
const ControlledPlaybackProtocol = "controlled-playback-v1"

type controlledPlaybackKey struct{}

// Supplied only by the app's private controller, not request data. A successful
// call returning this receipt proves closed launch and complete domain exit;
// Success separately reports the encoder result for the bound operation.
type ControlledPlaybackReceipt struct {
	Operation model.ResourceProducerOperation
	Success   bool
	ErrorText string
}

func (r *Repository) controlledPlaybackCompletionRow(op *model.ResourceProducerOperation) (*model.Resource, error) {
	if op == nil {
		return nil, ErrResourceProducerUnsettled
	}
	var current model.ResourceProducerOperation
	if err := r.db.First(&current, "id = ?", op.ID).Error; err != nil {
		return nil, err
	}
	selected := map[string]bool{op.ResourceID: true}
	if !samePlaybackOperation(&current, op) || current.Fenced || current.PhysicalState != "writing" || !r.controlledPlaybackAllowed(&current, selected, op.UserID) {
		return nil, ErrResourceProducerUnsettled
	}
	if current.ControlOwner != "" && (!validPlaybackControl(&current) || current.ControlPhase != "stopped") {
		return nil, ErrResourceProducerUnsettled
	}
	row, err := r.ResourceForUser(op.UserID, op.ResourceID)
	if err != nil {
		return nil, err
	}
	if row.Status != model.ResourceStatusReady || row.PlaybackStatus != model.PlaybackStatusProcessing || ResourcePhysicalIdentity(row, "playback") != op.Identity {
		return nil, ErrResourceProducerUnsettled
	}
	if err = r.requireResourceIdentityWritable(row); err != nil {
		return nil, err
	}
	if err = r.requireControlledPlaybackDomain(&current, selected, op.UserID); err != nil {
		return nil, err
	}
	return row, nil
}

func (r *Repository) PrepareControlledPlaybackCompletion(op *model.ResourceProducerOperation) (row *model.Resource, err error) {
	err = r.resourceLifecycleTransaction(func(tx *Repository) error { var e error; row, e = tx.controlledPlaybackCompletionRow(op); return e })
	return
}

func (r *Repository) CompleteControlledPlaybackProduction(op *model.ResourceProducerOperation, receipt ControlledPlaybackReceipt) error {
	if op == nil || !samePlaybackOperation(op, &receipt.Operation) {
		return ErrResourceProducerUnsettled
	}
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		row, err := tx.controlledPlaybackCompletionRow(op)
		if err != nil {
			return err
		}
		if op.ControlOwner != "" {
			current, e := tx.ControlledPlaybackOperation(op.ID)
			if e != nil || current.ControlPhase != "stopped" || receipt.Success != (current.ControlResult == "success") {
				return ErrResourceProducerUnsettled
			}
		}
		if tx.physicalLocks() == nil {
			return ErrResourceProducerUnsettled
		}
		for _, identity := range []string{ResourcePhysicalIdentity(row, "original"), op.Identity} {
			if err := tx.physicalLocks().Validate(identity); err != nil {
				return err
			}
		}
		outcome, status := "failed", model.PlaybackStatusFailed
		if receipt.Success {
			outcome, status = "success", model.PlaybackStatusReady
		}
		if err := tx.db.Model(&model.ResourceProducerOperation{}).Where("id = ?", op.ID).Updates(map[string]any{"physical_state": "settled", "proof": "controlled-domain-completed", "outcome": outcome}).Error; err != nil {
			return err
		}
		return tx.CompletePlaybackTranscode(op.UserID, op.ResourceID, status, receipt.ErrorText, op)
	})
}

func (r *Repository) requirePhysicalIdentityWritable(identity string) error {
	// ponytail: scan the existing ledger without a schema change; add a canonical
	// identity index if tombstone volume makes this scan expensive.
	var objects []model.ResourceDeletionObject
	if err := r.db.Where("status IN ?", []string{"pending", "completed"}).Find(&objects).Error; err != nil {
		return err
	}
	for _, object := range objects {
		row := model.Resource{Provider: object.Provider, Endpoint: object.Endpoint, Bucket: object.Bucket, ObjectKey: object.ObjectKey, PlaybackObjectKey: object.ObjectKey}
		if ResourcePhysicalIdentity(&row, object.Variant) == identity {
			return ErrResourceDeleting
		}
	}
	return nil
}

// WithControlledPlayback carries exact controller-owned tokens, never a global
// permission to recover legacy writers. The app has no public configuration path.
func (r *Repository) WithControlledPlayback(operations []model.ResourceProducerOperation) *Repository {
	tokens := make(map[string]model.ResourceProducerOperation, len(operations))
	for _, op := range operations {
		tokens[op.ID] = op
	}
	return r.WithContext(context.WithValue(r.db.Statement.Context, controlledPlaybackKey{}, tokens))
}

func samePlaybackOperation(a, b *model.ResourceProducerOperation) bool {
	return a.ID == b.ID && a.UserID == b.UserID && a.ResourceID == b.ResourceID && a.Identity == b.Identity && a.Protocol == b.Protocol && a.ControlOwner == b.ControlOwner && a.Variant == "playback" && b.Variant == "playback" && a.Provider == "local" && b.Provider == "local"
}

func (r *Repository) controlledPlaybackAllowed(op *model.ResourceProducerOperation, selected map[string]bool, userID string) bool {
	tokens, _ := r.db.Statement.Context.Value(controlledPlaybackKey{}).(map[string]model.ResourceProducerOperation)
	token, ok := tokens[op.ID]
	return ok && selected[op.ResourceID] && op.UserID == userID && op.Protocol == ControlledPlaybackProtocol && (op.PhysicalState == "reserved" || op.PhysicalState == "writing") && samePlaybackOperation(op, &token)
}

func (r *Repository) hasControlledPlayback(row *model.Resource) bool {
	tokens, _ := r.db.Statement.Context.Value(controlledPlaybackKey{}).(map[string]model.ResourceProducerOperation)
	identity := resourceProducerIdentity(row, "playback")
	for _, op := range tokens {
		if op.Protocol == ControlledPlaybackProtocol && op.ResourceID == row.ID && op.UserID == row.UserID && op.Provider == "local" && op.Variant == "playback" && op.Identity == identity.Identity {
			return true
		}
	}
	return false
}

func (r *Repository) requireControlledPlaybackDomain(op *model.ResourceProducerOperation, selected map[string]bool, userID string) error {
	var operations []model.ResourceProducerOperation
	if err := r.db.Where("identity = ?", op.Identity).Find(&operations).Error; err != nil {
		return err
	}
	for _, other := range operations {
		if other.UserID != userID || !selected[other.ResourceID] || other.PhysicalState != "settled" && !r.controlledPlaybackAllowed(&other, selected, userID) {
			return ErrResourceProducerUnsettled
		}
	}
	var rows []model.Resource
	if err := r.db.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		for _, variant := range []string{"original", "playback"} {
			candidate := resourceProducerIdentity(&row, variant)
			if candidate.ObjectKey != "" && candidate.Identity == op.Identity && (row.UserID != userID || !selected[row.ID]) {
				return ErrResourceProducerUnsettled
			}
		}
	}
	return nil
}

func (r *Repository) ClaimControlledPlaybackProduction(id string) (*model.ResourceProducerOperation, error) {
	var op *model.ResourceProducerOperation
	err := r.resourceLifecycleTransaction(func(tx *Repository) error {
		var err error
		op, err = tx.ClaimPlaybackProduction(id)
		if err != nil || op == nil {
			return err
		}
		if op.Provider != "local" {
			return gorm.ErrInvalidData
		}
		op.Protocol = ControlledPlaybackProtocol
		return tx.db.Model(op).Update("protocol", op.Protocol).Error
	})
	return op, err
}

// FinishPlaybackProduction closes the result/publication gap. Unknown physical
// outcome may report business failure, but can never publish ready.
func (r *Repository) FinishPlaybackProduction(op *model.ResourceProducerOperation, settled bool, status, errorText string) error {
	if op == nil || op.Variant != "playback" || status != model.PlaybackStatusReady && status != model.PlaybackStatusFailed || status == model.PlaybackStatusReady && !settled {
		return gorm.ErrInvalidData
	}
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		if err := tx.FinishResourceProduction(op, settled, status == model.PlaybackStatusReady, "ffmpeg-wait-returned"); err != nil {
			return err
		}
		return tx.CompletePlaybackTranscode(op.UserID, op.ResourceID, status, errorText, op)
	})
}

// ControlledPlaybackForDeletion validates the durable whole-intent barrier
// before the app requests stop, and again with locks before physical settlement.
func (r *Repository) ControlledPlaybackForDeletion(job *model.ResourceDeletionJob) (operations []model.ResourceProducerOperation, identities []string, err error) {
	err = r.withStoryboardDeletionJobWrite(job, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			var e error
			operations, identities, e = tx.controlledPlaybackForDeletion(job)
			return e
		})
	})
	return
}

func (r *Repository) controlledPlaybackForDeletion(job *model.ResourceDeletionJob) ([]model.ResourceProducerOperation, []string, error) {
	if err := r.requireDeletionLease(job); err != nil {
		return nil, nil, err
	}
	var object model.ResourceDeletionObject
	if err := r.db.First(&object, "id = ? AND status = ?", job.ID, "pending").Error; err != nil {
		return nil, nil, err
	}
	if deletionIdentity(job.Provider, job.Endpoint, job.Bucket, job.ObjectKey) != deletionIdentity(object.Provider, object.Endpoint, object.Bucket, object.ObjectKey) {
		return nil, nil, ErrResourceDeletionIdentityChanged
	}
	var intent model.ResourceDeletionIntent
	if err := r.db.First(&intent, "id = ? AND status = ?", object.IntentID, "pending").Error; err != nil {
		return nil, nil, err
	}
	if intent.UserID != job.UserID {
		return nil, nil, ErrResourceDeletionIdentityChanged
	}
	ids, err := deletionIDs(intent.ResourceIDsJSON)
	if err != nil {
		return nil, nil, err
	}
	assets, err := deletionIDs(intent.AssetIDsJSON)
	if err != nil {
		return nil, nil, err
	}
	if err := r.deletionReferences(intent.UserID, assets, ids); err != nil {
		return nil, nil, err
	}
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	var rows []model.Resource
	if err := r.db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) != len(ids) {
		return nil, nil, ErrResourceDeletionIdentityChanged
	}
	var result []model.ResourceProducerOperation
	var identities []string
	for _, row := range rows {
		if row.UserID != intent.UserID || row.Status != model.ResourceStatusDeleting {
			return nil, nil, ErrResourceDeletionIdentityChanged
		}
		if err := r.requireResourceProducers(&row, selected, intent.UserID); err != nil {
			return nil, nil, err
		}
		identities = append(identities, ResourcePhysicalIdentity(&row, "original"))
		if row.PlaybackObjectKey != "" {
			identities = append(identities, ResourcePhysicalIdentity(&row, "playback"))
		}
	}
	var pending []model.ResourceProducerOperation
	if err := r.db.Where("resource_id IN ? AND physical_state <> ?", ids, "settled").Find(&pending).Error; err != nil {
		return nil, nil, err
	}
	for _, op := range pending {
		if !op.Fenced || !r.controlledPlaybackAllowed(&op, selected, intent.UserID) {
			return nil, nil, ErrResourceProducerUnsettled
		}
		if err := r.requireControlledPlaybackDomain(&op, selected, intent.UserID); err != nil {
			return nil, nil, err
		}
		result = append(result, op)
	}
	return result, identities, nil
}

// Only controller-confirmed tokens are supplied here, after close-launch and
// whole-domain stop, with original and output OS locks held. No publication.
func (r *Repository) SettleControlledPlaybackDeletion(job *model.ResourceDeletionJob, stopped []model.ResourceProducerOperation) error {
	return r.withStoryboardDeletionJobWrite(job, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			pending, identities, err := tx.controlledPlaybackForDeletion(job)
			if err != nil {
				return err
			}
			if tx.physicalLocks() == nil {
				return ErrResourceProducerUnsettled
			}
			for _, identity := range identities {
				if err := tx.physicalLocks().Validate(identity); err != nil {
					return err
				}
			}
			proofs := map[string]model.ResourceProducerOperation{}
			for _, op := range stopped {
				proofs[op.ID] = op
			}
			for _, op := range pending {
				proof, ok := proofs[op.ID]
				if !ok || !samePlaybackOperation(&op, &proof) {
					return ErrResourceProducerUnsettled
				}
				if op.ControlOwner != "" && (!validPlaybackControl(&op) || op.ControlPhase != "stopped") {
					return ErrResourceProducerUnsettled
				}
				if err := tx.db.Model(&op).Updates(map[string]any{"physical_state": "settled", "proof": "controlled-domain-stopped", "outcome": "failed"}).Error; err != nil {
					return err
				}
				if err := tx.db.Model(&model.Resource{}).Where("id = ? AND user_id = ? AND status = ?", op.ResourceID, op.UserID, model.ResourceStatusDeleting).Updates(map[string]any{"playback_status": model.PlaybackStatusFailed, "playback_error": "deletion requested"}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}
