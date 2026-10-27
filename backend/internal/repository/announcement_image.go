package repository

import (
	"errors"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

var ErrAnnouncementImageDraftUnavailable = errors.New("公告图片草稿已失效，请重新上传")
var ErrAnnouncementImageReferenced = errors.New("公告图片仍被引用，无法删除")

func (r *Repository) CreateAnnouncementImageDraft(draft *model.AnnouncementImageDraft) error {
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		var count int64
		if err := tx.db.Model(&model.Resource{}).Where("id = ? AND user_id = ? AND status = ?", draft.ResourceID, draft.UserID, model.ResourceStatusReady).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrAnnouncementImageDraftUnavailable
		}
		return tx.db.Create(draft).Error
	})
}

func (r *Repository) AnnouncementImageDraftForUser(userID string, resourceID string) (*model.AnnouncementImageDraft, error) {
	var draft model.AnnouncementImageDraft
	if err := r.db.First(&draft, "resource_id = ? AND user_id = ?", resourceID, userID).Error; err != nil {
		return nil, err
	}
	return &draft, nil
}

func (r *Repository) DeleteAnnouncementImageDraft(userID string, resourceID string) error {
	return r.db.Where("resource_id = ? AND user_id = ?", resourceID, userID).Delete(&model.AnnouncementImageDraft{}).Error
}

func (r *Repository) StaleAnnouncementImageDrafts(before time.Time, limit int) ([]model.AnnouncementImageDraft, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var drafts []model.AnnouncementImageDraft
	err := r.db.Where("created_at < ?", before).Order("created_at asc").Limit(limit).Find(&drafts).Error
	return drafts, err
}

func (r *Repository) CreateAnnouncementWithImage(announcement *model.Announcement) error {
	return r.resourceLifecycleTransaction(func(repo *Repository) error {
		tx := repo.db
		if err := consumeAnnouncementImageDraft(tx, announcement.CreatedBy, announcement.ImageResourceID); err != nil {
			return err
		}
		return tx.Create(announcement).Error
	})
}

func (r *Repository) UpdateAnnouncementWithImage(announcement *model.Announcement, draftUserID string, newDraftResourceID string, oldResource *model.Resource, deletionJob *model.ResourceDeletionJob) error {
	var resourceIDs []string
	if oldResource != nil {
		resourceIDs = []string{oldResource.ID}
	}
	return r.withStoryboardResourceWrite(nil, resourceIDs, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(repo *Repository) error {
			tx := repo.db
			if err := consumeAnnouncementImageDraft(tx, draftUserID, newDraftResourceID); err != nil {
				return err
			}
			result := tx.Model(&model.Announcement{}).Where("id = ?", announcement.ID).Updates(map[string]any{
				"title":             announcement.Title,
				"content":           announcement.Content,
				"image_resource_id": announcement.ImageResourceID,
				"level":             announcement.Level,
				"pinned":            announcement.Pinned,
				"status":            announcement.Status,
				"published_at":      announcement.PublishedAt,
				"closed_at":         announcement.ClosedAt,
				"updated_at":        announcement.UpdatedAt,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			if err := tx.Where("announcement_id = ?", announcement.ID).Delete(&model.UserAnnouncementRead{}).Error; err != nil {
				return err
			}
			if oldResource == nil {
				return nil
			}
			var referenceCount int64
			if err := tx.Model(&model.Announcement{}).Where("image_resource_id = ?", oldResource.ID).Count(&referenceCount).Error; err != nil {
				return err
			}
			if referenceCount > 0 {
				return ErrAnnouncementImageReferenced
			}
			if err := tx.Where("resource_id = ?", oldResource.ID).Delete(&model.AnnouncementImageDraft{}).Error; err != nil {
				return err
			}
			return repo.stageResourceDeletion(oldResource.UserID, nil, []string{oldResource.ID})
		})
	})
}

func (r *Repository) DiscardAnnouncementImageDraft(userID string, resource *model.Resource, deletionJob *model.ResourceDeletionJob) error {
	if resource == nil {
		return gorm.ErrRecordNotFound
	}
	return r.withStoryboardResourceWrite(nil, []string{resource.ID}, func(owned *Repository) error {
		return owned.resourceLifecycleTransaction(func(repo *Repository) error {
			tx := repo.db
			var draft model.AnnouncementImageDraft
			if err := tx.First(&draft, "resource_id = ? AND user_id = ?", resource.ID, userID).Error; err != nil {
				return err
			}
			var referenceCount int64
			if err := tx.Model(&model.Announcement{}).Where("image_resource_id = ?", resource.ID).Count(&referenceCount).Error; err != nil {
				return err
			}
			if referenceCount > 0 {
				return ErrAnnouncementImageReferenced
			}
			if err := tx.Delete(&draft).Error; err != nil {
				return err
			}
			return repo.stageResourceDeletion(userID, nil, []string{resource.ID})
		})
	})
}

func (r *Repository) AnnouncementImageReferenceCount(resourceID string) (int64, error) {
	var count int64
	err := r.db.Model(&model.Announcement{}).Where("image_resource_id = ?", resourceID).Count(&count).Error
	return count, err
}

func consumeAnnouncementImageDraft(tx *gorm.DB, userID string, resourceID string) error {
	if resourceID == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&model.Resource{}).Where("id = ? AND user_id = ? AND status = ?", resourceID, userID, model.ResourceStatusReady).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrAnnouncementImageDraftUnavailable
	}
	result := tx.Where("resource_id = ? AND user_id = ?", resourceID, userID).Delete(&model.AnnouncementImageDraft{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAnnouncementImageDraftUnavailable
	}
	return nil
}
