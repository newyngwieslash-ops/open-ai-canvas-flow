package repository

import (
	"fmt"
	"strings"

	"yingce/backend/internal/model"
)

// SettleRemoteProducerObligation atomically verifies the remote resource row's
// physical identity and records settled producer evidence only when all
// attempts and post-barrier multipart upload listings have been proven complete.
func (r *Repository) SettleRemoteProducerObligation(
	resourceID string,
	userID string,
	variant string,
	provider string,
	endpoint string,
	bucket string,
	objectKey string,
	outcome string,
	proof string,
	auditEvent model.AdminAuditEvent,
) error {
	if strings.TrimSpace(resourceID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(proof) == "" {
		return ErrResourceProducerUnsettled
	}
	return r.withStoryboardResourceWrite(nil, []string{resourceID}, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			var row model.Resource
			if err := tx.db.First(&row, "id = ? AND user_id = ?", resourceID, userID).Error; err != nil {
				return err
			}
			if deletionProvider(row.Provider) == "local" {
				return fmt.Errorf("remote settlement requires non-local provider")
			}
			if deletionIdentity(row.Provider, row.Endpoint, row.Bucket, row.ObjectKey) != deletionIdentity(provider, endpoint, bucket, objectKey) {
				return ErrResourceDeletionIdentityChanged
			}
			if row.Status == model.ResourceStatusDeleting {
				return ErrResourceDeleting
			}
			canonical := resourceProducerIdentity(&row, variant)
			if canonical.ObjectKey == "" {
				return ErrResourceProducerUnsettled
			}

			var existing []model.ResourceProducerOperation
			if err := tx.db.Where("resource_id = ? AND variant = ?", row.ID, variant).Find(&existing).Error; err != nil {
				return err
			}
			matched := false
			for _, op := range existing {
				if op.Identity != canonical.Identity {
					return ErrResourceDeletionIdentityChanged
				}
				if op.Fenced {
					return ErrResourceDeleting
				}
				if err := tx.db.Model(&model.ResourceProducerOperation{}).
					Where("id = ? AND fenced = ?", op.ID, false).
					Updates(map[string]any{
						"physical_state": "settled",
						"outcome":        outcome,
						"proof":          proof,
					}).Error; err != nil {
					return err
				}
				matched = true
			}
			if !matched {
				record := canonical
				record.ID = deletionID()
				record.Protocol = "remote-settlement-v1"
				record.PhysicalState = "settled"
				record.Outcome = outcome
				record.Proof = proof
				if err := tx.db.Create(&record).Error; err != nil {
					return err
				}
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

// RemoteResourceDeletionStatus inspects the current state of a remote resource
// and any pending ResourceDeletionObject / ResourceDeletionJob for re-entrant
// deletion verification.
func (r *Repository) RemoteResourceDeletionStatus(resourceID string) (row *model.Resource, retained bool, finalized bool, err error) {
	var current model.Resource
	findErr := r.db.First(&current, "id = ?", resourceID).Error
	if findErr == nil {
		row = &current
	}
	var objects []model.ResourceDeletionObject
	if err := r.db.Order("id desc").Find(&objects).Error; err != nil {
		return nil, false, false, err
	}
	for _, obj := range objects {
		ids, parseErr := deletionIDs(obj.ResourceIDsJSON)
		if parseErr != nil {
			continue
		}
		for _, id := range ids {
			if id == resourceID {
				if obj.Status == "retained" {
					retained = true
				}
				break
			}
		}
	}
	if row == nil {
		return nil, retained, true, nil
	}
	return row, retained, false, nil
}
