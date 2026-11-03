package repository

import (
	"errors"

	"yingce/backend/internal/model"
)

var ErrAdminResourceDeleteChanged = errors.New("待删除的资源已变化，请刷新后重试")
var ErrAdminResourceStillReferenced = errors.New("资源仍被引用，无法删除")
var ErrAdminResourceAuditMismatch = errors.New("资源删除记录不一致，请刷新后重试")

func (r *Repository) AdminResourcesByIDs(ids []string) ([]model.Resource, error) {
	if len(ids) == 0 {
		return []model.Resource{}, nil
	}
	var resources []model.Resource
	err := r.db.Where("id IN ?", ids).Find(&resources).Error
	return resources, err
}

func (r *Repository) AnnouncementResourceReferences(resourceIDs []string) ([]ResourceDirectReference, error) {
	if len(resourceIDs) == 0 {
		return []ResourceDirectReference{}, nil
	}
	var announcements []model.Announcement
	if err := r.db.Where("image_resource_id IN ?", resourceIDs).Find(&announcements).Error; err != nil {
		return nil, err
	}
	result := make([]ResourceDirectReference, 0, len(announcements))
	for _, announcement := range announcements {
		result = append(result, ResourceDirectReference{Kind: "公告", ID: announcement.ID, Title: announcement.Title, ResourceID: announcement.ImageResourceID})
	}
	return result, nil
}

func (r *Repository) DeleteAdminResources(resources []model.Resource, deletionJobs []model.ResourceDeletionJob, audits []model.AdminAuditEvent) error {
	if len(resources) == 0 {
		return nil
	}
	if len(audits) != len(resources) {
		return ErrAdminResourceAuditMismatch
	}
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
			return tx.db.Create(&audits).Error
		})
	})
}
