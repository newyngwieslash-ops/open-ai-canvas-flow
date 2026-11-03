package repository

import (
	"time"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ClaimNextResourceDeletionJob(owner string, leaseDuration time.Duration) (*model.ResourceDeletionJob, error) {
	owner = owner + ":" + deletionID()
	var job model.ResourceDeletionJob
	now := time.Now()
	err := r.resourceLifecycleTransaction(func(repo *Repository) error {
		tx := repo.db
		available := "(status = ? OR (status = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?))) AND next_attempt_at <= ?"
		query := tx.Where(available, model.ResourceDeletionStatusPending, model.ResourceDeletionStatusProcessing, now, now).
			Order("next_attempt_at asc, created_at asc").Limit(1)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		result := query.Find(&job)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		claim := tx.Model(&model.ResourceDeletionJob{}).Where("id = ?", job.ID)
		if r.Dialect() != "postgres" {
			claim = claim.Where(available, model.ResourceDeletionStatusPending, model.ResourceDeletionStatusProcessing, now, now)
		}
		updated := claim.Updates(map[string]any{
			"status":           model.ResourceDeletionStatusProcessing,
			"attempts":         gorm.Expr("attempts + ?", 1),
			"lease_owner":      owner,
			"lease_expires_at": now.Add(leaseDuration),
			"updated_at":       now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			job = model.ResourceDeletionJob{}
			return nil
		}
		return tx.First(&job, "id = ?", job.ID).Error
	})
	if err != nil || job.ID == "" {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) CompleteResourceDeletionJob(id string, owner string) error {
	// Completion requires the claimed generation and retained intent, not just an owner.
	return ErrResourceDeletionLeaseLost
}

func (r *Repository) RetryResourceDeletionJob(id string, owner string, lastError string, nextAttemptAt time.Time) error {
	result := r.db.Model(&model.ResourceDeletionJob{}).
		Where("id = ? AND lease_owner = ? AND status = ? AND lease_expires_at > ?", id, owner, model.ResourceDeletionStatusProcessing, time.Now()).
		Updates(map[string]any{
			"status":           model.ResourceDeletionStatusPending,
			"last_error":       lastError,
			"next_attempt_at":  nextAttemptAt,
			"lease_owner":      "",
			"lease_expires_at": nil,
			"updated_at":       time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrResourceDeletionLeaseLost
	}
	return nil
}
