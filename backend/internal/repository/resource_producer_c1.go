package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// A different hash domain prevents a launch gate from aliasing a physical lock.
func PlaybackControlGateIdentity(id string) string {
	sum := sha256.Sum256([]byte("playback-control-gate-v1\x00" + id))
	return hex.EncodeToString(sum[:])
}

func validPlaybackControl(op *model.ResourceProducerOperation) bool {
	if op == nil || op.Protocol != ControlledPlaybackProtocol || op.Provider != "local" || op.Variant != "playback" || strings.TrimSpace(op.ControlOwner) == "" || len(op.ControlOwner) > 36 {
		return false
	}
	switch op.ControlPhase {
	case "armed", "attempted", "closed":
		return op.ControlResult == ""
	case "stopped":
		return op.ControlResult == "success" || op.ControlResult == "failed" || op.ControlResult == "unknown"
	}
	return false
}

// Claim, armed registration and reserved -> writing commit together. No OS call
// can precede the later attempted CAS, so an abandoned armed row is recoverable.
func (r *Repository) ClaimWindowsControlledPlayback(id, owner string) (op *model.ResourceProducerOperation, err error) {
	if strings.TrimSpace(owner) == "" || len(owner) > 36 {
		return nil, gorm.ErrInvalidData
	}
	err = r.resourceLifecycleTransaction(func(tx *Repository) error {
		var e error
		op, e = tx.ClaimControlledPlaybackProduction(id)
		if e != nil || op == nil {
			return e
		}
		if e = tx.StartResourceProduction(op); e != nil {
			return e
		}
		op.ControlOwner, op.ControlPhase, op.PhysicalState = owner, "armed", "writing"
		return tx.db.Model(op).Updates(map[string]any{"control_owner": owner, "control_phase": "armed"}).Error
	})
	return
}

func (r *Repository) ControlledPlaybackOperation(id string) (*model.ResourceProducerOperation, error) {
	var op model.ResourceProducerOperation
	if err := r.db.First(&op, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if !validPlaybackControl(&op) {
		return nil, ErrResourceProducerUnsettled
	}
	return &op, nil
}

func (r *Repository) UnsettledPlaybackControls(after string, limit int) ([]model.ResourceProducerOperation, error) {
	if limit < 1 || limit > 100 {
		return nil, gorm.ErrInvalidData
	}
	var rows []model.ResourceProducerOperation
	err := r.db.Where("protocol = ? AND control_owner <> ? AND id > ? AND physical_state <> ?", ControlledPlaybackProtocol, "", after, "settled").Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *Repository) lockedPlaybackControl(op *model.ResourceProducerOperation) (*model.ResourceProducerOperation, error) {
	if op == nil || r.physicalLocks().Validate(PlaybackControlGateIdentity(op.ID)) != nil {
		return nil, ErrResourceProducerUnsettled
	}
	current, err := r.ControlledPlaybackOperation(op.ID)
	if err != nil {
		return nil, err
	}
	if !samePlaybackOperation(current, op) || current.ControlOwner != op.ControlOwner {
		return nil, ErrResourceProducerUnsettled
	}
	return current, nil
}

// Only the trusted app controller calls this with the persistent launch gate.
// Every state is monotonic; stopped/result is immutable and owner never changes.
func (r *Repository) TransitionPlaybackControl(op *model.ResourceProducerOperation, from, to, result string) error {
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		current, err := tx.lockedPlaybackControl(op)
		if err != nil {
			return err
		}
		if current.ControlPhase == "stopped" && to == "stopped" && current.ControlResult == result {
			return nil
		}
		allowed := from == "armed" && (to == "attempted" || to == "closed") || from == "attempted" && to == "closed" || from == "closed" && to == "stopped"
		candidate := *current
		candidate.ControlPhase, candidate.ControlResult = to, result
		if !allowed || current.ControlPhase != from || !validPlaybackControl(&candidate) {
			return ErrResourceProducerUnsettled
		}
		if to == "attempted" {
			if current.Fenced || current.PhysicalState != "writing" {
				return ErrResourceProducerUnsettled
			}
			row, e := tx.ResourceForUser(current.UserID, current.ResourceID)
			if e != nil {
				return e
			}
			if row.Status != model.ResourceStatusReady || row.PlaybackStatus != model.PlaybackStatusProcessing || ResourcePhysicalIdentity(row, "playback") != current.Identity {
				return ErrResourceProducerUnsettled
			}
			if e := tx.requireResourceIdentityWritable(row); e != nil {
				return e
			}
		}
		updated := tx.db.Model(&model.ResourceProducerOperation{}).Where("id = ? AND control_owner = ? AND control_phase = ? AND control_result = ?", current.ID, current.ControlOwner, from, "").Updates(map[string]any{"control_phase": to, "control_result": result})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrResourceProducerUnsettled
		}
		return nil
	})
}

// The gate plus armed proves no OS creation was attempted under this protocol.
// Both close and terminal receipt commit atomically: no ambiguous closed window.
func (r *Repository) RecoverUnstartedPlaybackControl(op *model.ResourceProducerOperation) error {
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		current, err := tx.lockedPlaybackControl(op)
		if err != nil {
			return err
		}
		if current.ControlPhase != "armed" {
			return ErrResourceProducerUnsettled
		}
		if err := tx.TransitionPlaybackControl(current, "armed", "closed", ""); err != nil {
			return err
		}
		return tx.TransitionPlaybackControl(current, "closed", "stopped", "failed")
	})
}
