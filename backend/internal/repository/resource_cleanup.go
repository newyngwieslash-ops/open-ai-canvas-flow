package repository

import (
	"errors"

	"yingce/backend/internal/model"
)

var ErrResourceCleanupSetChanged = errors.New("待清理的资源已变化，请刷新后重试")
var ErrResourceCleanupStillReferenced = errors.New("资源仍被引用，暂不清理")

// DeleteDetachedResources stages cleanup; physical failures retain all records.
func (r *Repository) DeleteDetachedResources(resources []model.Resource, deletionJobs []model.ResourceDeletionJob) error {
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		ids = append(ids, resource.ID)
	}
	return r.withStoryboardResourceWrite(nil, ids, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(tx *Repository) error {
			byUser := map[string][]string{}
			for _, resource := range resources {
				byUser[resource.UserID] = append(byUser[resource.UserID], resource.ID)
			}
			for userID, ids := range byUser {
				if err := tx.stageResourceDeletion(userID, nil, ids); err != nil {
					return err
				}
			}
			return nil
		})
	})
}
