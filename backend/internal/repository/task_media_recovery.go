package repository

import (
	"encoding/base64"
	"gorm.io/gorm"
	"strings"
	"time"

	"yingce/backend/internal/model"
)

// The existing task lease fences every checkpoint write, including after cancellation.
func (r *Repository) SaveTaskMediaCheckpoint(task *model.Task, encrypted, stage string, resourceIDs []string) error {
	encrypted, err := taskMediaCheckpointProjection(encrypted, resourceIDs)
	if err != nil {
		return err
	}
	err = r.resourceLifecycleTransaction(func(repo *Repository) error {
		if err := repo.replaceTaskMediaReferences(task.UserID, task.ID, resourceIDs); err != nil {
			return err
		}
		result := taskLeaseWriter(repo.db.Model(&model.Task{}), task.LeaseOwner).
			Where("id = ? AND user_id = ? AND status = ? AND route_run = ?", task.ID, task.UserID, model.TaskStatusRunning, task.RouteRun).
			Updates(map[string]any{"media_recovery_json": encrypted, "media_stage": stage, "stage": "作品已生成，正在保存", "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTaskStateConflict
		}
		return nil
	})
	if err == nil {
		task.MediaRecoveryJSON, task.MediaStage = encrypted, stage
	}
	return err
}

// Manual recovery requeues the same task and order, never a new generation.
func (r *Repository) RequeueTaskMediaRecovery(task *model.Task, encrypted string, resourceIDs []string) error {
	encrypted, err := taskMediaCheckpointProjection(encrypted, resourceIDs)
	if err != nil {
		return err
	}
	return r.resourceLifecycleTransaction(func(repo *Repository) error {
		if err := repo.replaceTaskMediaReferences(task.UserID, task.ID, resourceIDs); err != nil {
			return err
		}
		now := time.Now()
		result := repo.db.Model(&model.Task{}).
			Where("id = ? AND user_id = ? AND status = ? AND media_recovery_json = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", task.ID, task.UserID, model.TaskStatusFailed, task.MediaRecoveryJSON, now).
			Updates(map[string]any{"status": model.TaskStatusQueued, "stage": "等待恢复作品保存", "error": "", "completed_at": nil, "next_poll_at": nil, "lease_owner": "", "lease_expires_at": nil, "media_recovery_json": encrypted, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTaskStateConflict
		}
		return nil
	})
}

func (r *Repository) requireUnchangedMediaCheckpoint(task *model.Task) error {
	if task.MediaRecoveryJSON == "" {
		return nil
	}
	current, err := r.TaskForUser(task.UserID, task.ID)
	if err != nil {
		return err
	}
	if current.MediaRecoveryJSON != task.MediaRecoveryJSON {
		return gorm.ErrInvalidData
	}
	return nil
}

const taskMediaReferencePrefix = "task-media:"
const taskMediaEmptyPrefix = "task-media:empty:v1:"

// TaskMediaCheckpointCiphertext unwraps only the server-owned known-empty
// projection. The ciphertext bytes and encryption format remain unchanged.
func TaskMediaCheckpointCiphertext(value string) (string, bool, error) {
	if !strings.HasPrefix(value, taskMediaReferencePrefix) {
		return value, false, nil
	}
	if !strings.HasPrefix(value, taskMediaEmptyPrefix) {
		return "", false, gorm.ErrInvalidData
	}
	ciphertext := strings.TrimPrefix(value, taskMediaEmptyPrefix)
	if !validTaskMediaCiphertext(ciphertext) {
		return "", false, gorm.ErrInvalidData
	}
	return ciphertext, true, nil
}

func validTaskMediaCiphertext(value string) bool {
	const prefix = "enc:v1:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	encoded := strings.TrimPrefix(value, prefix)
	payload, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	// AES-GCM uses a 12-byte nonce and a 16-byte authentication tag.
	return err == nil && len(payload) > 28 && base64.RawStdEncoding.EncodeToString(payload) == encoded
}

func taskMediaCheckpointProjection(encrypted string, resourceIDs []string) (string, error) {
	// Callers supply raw ciphertext, never an already asserted projection.
	if strings.HasPrefix(encrypted, taskMediaReferencePrefix) {
		return "", gorm.ErrInvalidData
	}
	if len(uniqueSortedStrings(resourceIDs)) != 0 {
		return encrypted, nil
	}
	if !validTaskMediaCiphertext(encrypted) {
		return "", gorm.ErrInvalidData
	}
	return taskMediaEmptyPrefix + encrypted, nil
}

func (r *Repository) replaceTaskMediaReferences(userID, taskID string, resourceIDs []string) error {
	if userID == "" || taskID == "" {
		return gorm.ErrInvalidData
	}
	if err := r.requireReferenceResources(userID, false, resourceIDs); err != nil {
		return err
	}
	owner := taskMediaReferencePrefix + taskID
	if err := r.db.Where("user_id = ? AND owner_id = ?", userID, owner).Delete(&model.CloudAgentResourceLease{}).Error; err != nil {
		return err
	}
	for _, id := range uniqueSortedStrings(resourceIDs) {
		lease := model.CloudAgentResourceLease{UserID: userID, RunID: owner, OwnerID: owner, ResourceID: id}
		if err := r.db.Create(&lease).Error; err != nil {
			return err
		}
	}
	return nil
}
