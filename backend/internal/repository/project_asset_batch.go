package repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

var (
	ErrBatchItemAlreadyDispatched = errors.New("batch item already dispatched or stopped")
)

func (r *Repository) FindProjectAssetGenerationBatchByIdempotency(userID, projectID, idempotencyKey string) (*model.ProjectAssetGenerationBatch, []model.ProjectAssetGenerationBatchItem, error) {
	var batch model.ProjectAssetGenerationBatch
	err := r.db.Where("user_id = ? AND project_id = ? AND idempotency_key = ?", userID, projectID, idempotencyKey).First(&batch).Error
	if err != nil {
		return nil, nil, err
	}
	var items []model.ProjectAssetGenerationBatchItem
	if err := r.db.Where("batch_id = ?", batch.ID).Order("position asc").Find(&items).Error; err != nil {
		return nil, nil, err
	}
	return &batch, items, nil
}

func (r *Repository) CreateProjectAssetGenerationBatch(batch *model.ProjectAssetGenerationBatch, items []model.ProjectAssetGenerationBatchItem) (*model.ProjectAssetGenerationBatch, []model.ProjectAssetGenerationBatchItem, bool, error) {
	var existingBatch *model.ProjectAssetGenerationBatch
	var existingItems []model.ProjectAssetGenerationBatchItem
	reused := false

	err := r.db.Transaction(func(tx *gorm.DB) error {
		var current model.ProjectAssetGenerationBatch
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND project_id = ? AND idempotency_key = ?", batch.UserID, batch.ProjectID, batch.IdempotencyKey).
			First(&current).Error
		if err == nil {
			reused = true
			existingBatch = &current
			return tx.Where("batch_id = ?", current.ID).Order("position asc").Find(&existingItems).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(batch).Error; err != nil {
			if isUniqueConstraintError(err) {
				var raced model.ProjectAssetGenerationBatch
				if findErr := tx.Where("user_id = ? AND project_id = ? AND idempotency_key = ?", batch.UserID, batch.ProjectID, batch.IdempotencyKey).First(&raced).Error; findErr == nil {
					reused = true
					existingBatch = &raced
					return tx.Where("batch_id = ?", raced.ID).Order("position asc").Find(&existingItems).Error
				}
			}
			return err
		}
		if len(items) > 0 {
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		existingBatch = batch
		existingItems = items
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	return existingBatch, existingItems, reused, nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key")
}

func (r *Repository) ProjectAssetGenerationBatchWithItems(userID, projectID, batchID string) (*model.ProjectAssetGenerationBatch, []model.ProjectAssetGenerationBatchItem, error) {
	var batch model.ProjectAssetGenerationBatch
	if err := r.db.Where("id = ? AND user_id = ? AND project_id = ?", batchID, userID, projectID).First(&batch).Error; err != nil {
		return nil, nil, err
	}
	var items []model.ProjectAssetGenerationBatchItem
	if err := r.db.Where("batch_id = ?", batch.ID).Order("position asc").Find(&items).Error; err != nil {
		return nil, nil, err
	}
	return &batch, items, nil
}

func (r *Repository) ListProjectAssetGenerationBatches(userID, projectID string, limit int) ([]model.ProjectAssetGenerationBatch, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var batches []model.ProjectAssetGenerationBatch
	err := r.db.Where("user_id = ? AND project_id = ?", userID, projectID).Order("created_at desc").Limit(limit).Find(&batches).Error
	return batches, err
}

func (r *Repository) TaskByCreationSubmissionID(userID, submissionKey string) (*model.Task, error) {
	if strings.TrimSpace(submissionKey) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var task model.Task
	if err := r.db.Where("user_id = ? AND creation_submission_id = ?", userID, submissionKey).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *Repository) CanDispatchProjectAssetBatchItem(batchID, itemID string, attempt int) (bool, error) {
	var allowed bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var batch model.ProjectAssetGenerationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batchID).First(&batch).Error; err != nil {
			return err
		}
		if batch.StopRequestedAt != nil || batch.Status == "stopped" {
			allowed = false
			return nil
		}
		var item model.ProjectAssetGenerationBatchItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND batch_id = ?", itemID, batchID).First(&item).Error; err != nil {
			return err
		}
		if item.Status != "pending" || item.TaskID != "" || item.Attempt != attempt {
			allowed = false
			return nil
		}
		allowed = true
		return nil
	})
	return allowed, err
}

func (r *Repository) BindProjectAssetBatchItemTask(batchID, itemID string, attempt int, taskID, status, errorReason, errorMessage string) error {
	now := time.Now()
	return r.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"status":        status,
			"error_reason":  errorReason,
			"error_message": errorMessage,
			"updated_at":    now,
		}
		if taskID != "" {
			updates["task_id"] = taskID
		}
		res := tx.Model(&model.ProjectAssetGenerationBatchItem{}).
			Where("id = ? AND batch_id = ? AND attempt = ? AND (status = ? OR task_id = ?)", itemID, batchID, attempt, "pending", taskID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrBatchItemAlreadyDispatched
		}
		return tx.Model(&model.ProjectAssetGenerationBatch{}).
			Where("id = ?", batchID).
			Update("updated_at", now).Error
	})
}

func (r *Repository) UpdateProjectAssetBatchAndItems(batch *model.ProjectAssetGenerationBatch, items []model.ProjectAssetGenerationBatchItem) error {
	now := time.Now()
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ProjectAssetGenerationBatch{}).
			Where("id = ?", batch.ID).
			Updates(map[string]any{
				"status":     batch.Status,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		for i := range items {
			item := &items[i]
			if err := tx.Model(&model.ProjectAssetGenerationBatchItem{}).
				Where("id = ? AND batch_id = ?", item.ID, batch.ID).
				Updates(map[string]any{
					"status":        item.Status,
					"task_id":       item.TaskID,
					"error_reason":  item.ErrorReason,
					"error_message": item.ErrorMessage,
					"updated_at":    now,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) StopUnsubmittedProjectAssetBatchItems(userID, projectID, batchID string) (*model.ProjectAssetGenerationBatch, []model.ProjectAssetGenerationBatchItem, error) {
	now := time.Now()
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var batch model.ProjectAssetGenerationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ? AND project_id = ?", batchID, userID, projectID).
			First(&batch).Error; err != nil {
			return err
		}
		if batch.StopRequestedAt == nil {
			if err := tx.Model(&model.ProjectAssetGenerationBatch{}).
				Where("id = ?", batch.ID).
				Updates(map[string]any{
					"stop_requested_at": &now,
					"updated_at":        now,
				}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.ProjectAssetGenerationBatchItem{}).
			Where("batch_id = ? AND status = ? AND (task_id = '' OR task_id IS NULL)", batch.ID, "pending").
			Updates(map[string]any{
				"status":        "stopped",
				"error_reason":  "batch_stopped_before_submit",
				"error_message": "已停止未提交的生成项",
				"updated_at":    now,
			}).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return r.ProjectAssetGenerationBatchWithItems(userID, projectID, batchID)
}

func (r *Repository) PrepareRetryProjectAssetBatchItem(userID, projectID, batchID, itemID string, expectedAttempt int, nextSubmissionKey, nextHistoryJSON string) (*model.ProjectAssetGenerationBatchItem, error) {
	now := time.Now()
	var updatedItem model.ProjectAssetGenerationBatchItem
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var batch model.ProjectAssetGenerationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ? AND project_id = ?", batchID, userID, projectID).
			First(&batch).Error; err != nil {
			return err
		}
		var item model.ProjectAssetGenerationBatchItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND batch_id = ?", itemID, batchID).
			First(&item).Error; err != nil {
			return err
		}
		if item.Attempt != expectedAttempt || item.Status != "failed" {
			return ErrTaskStateConflict
		}
		nextAttempt := item.Attempt + 1
		if err := tx.Model(&model.ProjectAssetGenerationBatchItem{}).
			Where("id = ? AND batch_id = ? AND attempt = ?", item.ID, batch.ID, expectedAttempt).
			Updates(map[string]any{
				"status":               "pending",
				"task_id":              "",
				"attempt":              nextAttempt,
				"submission_key":       nextSubmissionKey,
				"attempt_history_json": nextHistoryJSON,
				"error_reason":         "",
				"error_message":        "",
				"updated_at":           now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProjectAssetGenerationBatch{}).
			Where("id = ?", batch.ID).
			Updates(map[string]any{
				"stop_requested_at": nil,
				"status":            "running",
				"updated_at":        now,
			}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND batch_id = ?", item.ID, batch.ID).First(&updatedItem).Error
	})
	if err != nil {
		return nil, err
	}
	return &updatedItem, nil
}
