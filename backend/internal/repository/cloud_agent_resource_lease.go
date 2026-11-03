package repository

import (
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

func (r *Repository) UpsertCloudAgentResourceLeases(userID, runID, ownerID string, resourceIDs []string, expiresAt time.Time) error {
	if strings.HasPrefix(ownerID, taskMediaReferencePrefix) || strings.HasPrefix(runID, taskMediaReferencePrefix) {
		return gorm.ErrInvalidData
	}
	return r.resourceLifecycleTransaction(func(repo *Repository) error {
		if err := repo.requireReferenceResources(userID, false, resourceIDs); err != nil {
			return err
		}
		return repo.upsertCloudAgentResourceLeases(userID, runID, ownerID, resourceIDs, expiresAt)
	})
}

func (r *Repository) upsertCloudAgentResourceLeases(userID, runID, ownerID string, resourceIDs []string, expiresAt time.Time) error {
	resourceIDs = uniqueSortedStrings(resourceIDs)
	for _, id := range resourceIDs {
		lease := model.CloudAgentResourceLease{UserID: userID, RunID: runID, OwnerID: ownerID, ResourceID: id, ExpiresAt: expiresAt}
		if err := r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "owner_id"}, {Name: "resource_id"}}, DoUpdates: clause.AssignmentColumns([]string{"run_id", "expires_at"})}).Create(&lease).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReplaceCloudAgentResourceLeases makes an approval owner's resource set
// authoritative after the user changes generation settings.
func (r *Repository) ReplaceCloudAgentResourceLeases(userID, runID, ownerID string, resourceIDs []string, expiresAt time.Time) error {
	if strings.HasPrefix(ownerID, taskMediaReferencePrefix) || strings.HasPrefix(runID, taskMediaReferencePrefix) {
		return gorm.ErrInvalidData
	}
	return r.resourceLifecycleTransaction(func(repo *Repository) error {
		if err := repo.requireReferenceResources(userID, false, resourceIDs); err != nil {
			return err
		}
		tx := repo.db
		if err := tx.Where("user_id = ? AND owner_id = ?", userID, ownerID).Delete(&model.CloudAgentResourceLease{}).Error; err != nil {
			return err
		}
		for _, id := range uniqueSortedStrings(resourceIDs) {
			lease := model.CloudAgentResourceLease{UserID: userID, RunID: runID, OwnerID: ownerID, ResourceID: id, ExpiresAt: expiresAt}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "owner_id"}, {Name: "resource_id"}}, DoUpdates: clause.AssignmentColumns([]string{"run_id", "expires_at"})}).Create(&lease).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) ReleaseCloudAgentResourceLeases(userID, ownerID string) error {
	if strings.HasPrefix(ownerID, taskMediaReferencePrefix) {
		return gorm.ErrInvalidData
	}
	return r.db.Where("user_id = ? AND owner_id = ?", userID, ownerID).Delete(&model.CloudAgentResourceLease{}).Error
}

func (r *Repository) ReleaseCloudAgentResourceLeasesByRun(userID, runID string) error {
	if strings.HasPrefix(runID, taskMediaReferencePrefix) {
		return gorm.ErrInvalidData
	}
	return r.db.Where("user_id = ? AND run_id = ?", userID, runID).Delete(&model.CloudAgentResourceLease{}).Error
}

func (r *Repository) TransferCloudAgentResourceLeases(userID, fromOwner, toOwner, runID string, expiresAt time.Time) error {
	if strings.HasPrefix(fromOwner, taskMediaReferencePrefix) || strings.HasPrefix(toOwner, taskMediaReferencePrefix) || strings.HasPrefix(runID, taskMediaReferencePrefix) {
		return gorm.ErrInvalidData
	}
	if fromOwner == "" || toOwner == "" || fromOwner == toOwner {
		return nil
	}
	return r.resourceLifecycleTransaction(func(repo *Repository) error {
		tx := repo.db
		var leases []model.CloudAgentResourceLease
		if err := tx.Where("user_id = ? AND owner_id = ?", userID, fromOwner).Find(&leases).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND owner_id = ?", userID, toOwner).Delete(&model.CloudAgentResourceLease{}).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(leases))
		for _, lease := range leases {
			ids = append(ids, lease.ResourceID)
		}
		if err := repo.requireReferenceResources(userID, false, ids); err != nil {
			return err
		}
		for _, lease := range leases {
			lease.OwnerID, lease.RunID, lease.ExpiresAt = toOwner, runID, expiresAt
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "owner_id"}, {Name: "resource_id"}}, DoUpdates: clause.AssignmentColumns([]string{"run_id", "expires_at"})}).Create(&lease).Error; err != nil {
				return err
			}
		}
		return tx.Where("user_id = ? AND owner_id = ?", userID, fromOwner).Delete(&model.CloudAgentResourceLease{}).Error
	})
}

func (r *Repository) ReleaseExpiredCloudAgentResourceLeases(now time.Time) error {
	return r.db.Where("expires_at <= ? AND (owner_id NOT LIKE ? OR NOT EXISTS (SELECT 1 FROM tasks WHERE tasks.user_id = cloud_agent_resource_leases.user_id AND cloud_agent_resource_leases.owner_id = ? || tasks.id AND tasks.media_recovery_json <> ''))", now, taskMediaReferencePrefix+"%", taskMediaReferencePrefix).Delete(&model.CloudAgentResourceLease{}).Error
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (r *Repository) CloudAgentCanvasMutationChain(userID, runID, canvasID string) ([]model.CloudAgentCanvasMutation, error) {
	var records []model.CloudAgentCanvasMutation
	err := r.db.Where("user_id = ? AND run_id = ? AND canvas_id = ? AND status IN ?", userID, runID, canvasID, []string{"applied", "not_undoable"}).Order("created_at, id").Find(&records).Error
	return records, err
}

// LatestCloudAgentCanvasMutationForCanvas is used to distinguish a current-run
// write from a later Agent run write. Manual canvas edits have no mutation row;
// comparing the row's after hash with the live document then rejects them too.
func (r *Repository) LatestCloudAgentCanvasMutationForCanvas(userID, canvasID string) (*model.CloudAgentCanvasMutation, error) {
	var mutation model.CloudAgentCanvasMutation
	err := r.db.Where("user_id = ? AND canvas_id = ? AND status IN ?", userID, canvasID, []string{"applied", "not_undoable"}).Order("created_at DESC, id DESC").First(&mutation).Error
	return &mutation, err
}
