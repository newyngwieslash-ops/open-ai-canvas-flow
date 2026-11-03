package repository

import (
	"strings"

	"yingce/backend/internal/model"
)

// RecoverControllerLostPlaybackOperation atomically transitions a crashed
// ControlledPlaybackProtocol operation from "attempted"/"closed" to "stopped"
// and "settled" under the launch gate and original/playback physical locks,
// separating physical settlement from business publication (never publishing
// PlaybackStatusReady when the encoder receipt was lost).
func (r *Repository) RecoverControllerLostPlaybackOperation(
	operationID string,
	resourceID string,
	userID string,
	expectedOwner string,
	publishReady bool,
	proof string,
	errorText string,
	auditEvent model.AdminAuditEvent,
) error {
	if strings.TrimSpace(operationID) == "" || strings.TrimSpace(resourceID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(expectedOwner) == "" || strings.TrimSpace(proof) == "" {
		return ErrResourceProducerUnsettled
	}
	return r.withStoryboardResourceWrite(nil, []string{resourceID}, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			locks := tx.physicalLocks()
			if locks == nil {
				return ErrResourceProducerUnsettled
			}
			if err := locks.Validate(PlaybackControlGateIdentity(operationID)); err != nil {
				return ErrResourceProducerUnsettled
			}

			current, err := tx.ControlledPlaybackOperation(operationID)
			if err != nil {
				return err
			}
			if current.ResourceID != resourceID || current.UserID != userID || current.ControlOwner != expectedOwner || current.Fenced {
				return ErrResourceProducerUnsettled
			}
			if current.PhysicalState != "writing" {
				return ErrResourceProducerUnsettled
			}

			row, err := tx.ResourceForUser(userID, resourceID)
			if err != nil {
				return err
			}
			if row.Status != model.ResourceStatusReady || row.PlaybackStatus != model.PlaybackStatusProcessing || ResourcePhysicalIdentity(row, "playback") != current.Identity {
				return ErrResourceProducerUnsettled
			}
			for _, identity := range []string{ResourcePhysicalIdentity(row, "original"), current.Identity} {
				if err := locks.Validate(identity); err != nil {
					return err
				}
			}
			if err := tx.requireResourceIdentityWritable(row); err != nil {
				return err
			}

			controlResult := "failed"
			outcome := "failed"
			targetPlaybackStatus := model.PlaybackStatusFailed
			if publishReady {
				controlResult = "success"
				outcome = "success"
				targetPlaybackStatus = model.PlaybackStatusReady
				errorText = ""
			} else if strings.TrimSpace(errorText) == "" {
				errorText = "recovered producer domain exited without verified encoder receipt"
			}

			if current.ControlPhase == "attempted" {
				if err := tx.TransitionPlaybackControl(current, "attempted", "closed", ""); err != nil {
					return err
				}
				current.ControlPhase = "closed"
			}
			if current.ControlPhase == "closed" {
				if err := tx.TransitionPlaybackControl(current, "closed", "stopped", controlResult); err != nil {
					return err
				}
				current.ControlPhase = "stopped"
				current.ControlResult = controlResult
			}
			if current.ControlPhase != "stopped" {
				return ErrResourceProducerUnsettled
			}

			updated := tx.db.Model(&model.ResourceProducerOperation{}).
				Where("id = ? AND resource_id = ? AND user_id = ? AND control_owner = ? AND physical_state = ? AND fenced = ?",
					current.ID, resourceID, userID, expectedOwner, "writing", false).
				Updates(map[string]any{
					"physical_state": "settled",
					"outcome":        outcome,
					"proof":          proof,
				})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrResourceProducerUnsettled
			}

			current.PhysicalState = "settled"
			current.Outcome = outcome
			current.Proof = proof
			if err := tx.CompletePlaybackTranscode(userID, resourceID, targetPlaybackStatus, errorText, current); err != nil {
				return err
			}

			if strings.TrimSpace(auditEvent.ID) != "" {
				if err := tx.db.Create(&auditEvent).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}
