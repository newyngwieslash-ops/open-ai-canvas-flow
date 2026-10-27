package repository

import (
	"context"
	"yingce/backend/internal/model"
)

type resourcePublicationTokenKey struct{}

// SaveResourceProduction binds evidence, quota and the business update to one
// owner/lifecycle transaction. Recovery cannot interleave Check and Save.
func (r *Repository) SaveResourceProduction(row *model.Resource, operation *model.ResourceProducerOperation, limit, dailyLimit int64, reservationID string) error {
	if operation == nil || operation.Protocol != LocalGoProducerProtocol {
		return ErrResourceProducerUnsettled
	}
	bound := r.WithContext(context.WithValue(r.db.Statement.Context, resourcePublicationTokenKey{}, operation.ID))
	return bound.withStoryboardResourceWrite(nil, []string{row.ID}, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			var op model.ResourceProducerOperation
			if err := tx.db.First(&op, "id = ?", operation.ID).Error; err != nil {
				return err
			}
			current, err := tx.ResourceForUser(row.UserID, row.ID)
			if err != nil {
				return err
			}
			if current.Status != model.ResourceStatusPending || op.ResourceID != row.ID || op.UserID != row.UserID || op.Identity != operation.Identity || op.Identity != ResourcePhysicalIdentity(current, "original") || op.Identity != ResourcePhysicalIdentity(row, "original") || op.Protocol != LocalGoProducerProtocol || op.Fenced {
				return ErrResourceProducerUnsettled
			}
			if row.Status == model.ResourceStatusReady {
				if op.PhysicalState != "settled" || op.Outcome != "success" || op.Proof != "local-writer-returned" {
					return ErrResourceProducerUnsettled
				}
				return tx.SaveResourceWithinStorageLimit(row, limit, dailyLimit, reservationID)
			}
			if row.Status != model.ResourceStatusFailed {
				return ErrResourceProducerUnsettled
			}
			return tx.saveLiveResource(row)
		})
	})
}

func (r *Repository) requireResourcePublicationToken(current *model.Resource) error {
	if current.Status != model.ResourceStatusPending {
		return nil
	}
	var operations []model.ResourceProducerOperation
	result := r.db.Where("resource_id = ? AND variant = ? AND protocol = ?", current.ID, "original", LocalGoProducerProtocol).Find(&operations)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	token, _ := r.db.Statement.Context.Value(resourcePublicationTokenKey{}).(string)
	for _, operation := range operations {
		if token != "" && token == operation.ID && !operation.Fenced {
			return nil
		}
	}
	return ErrResourceProducerUnsettled
}
