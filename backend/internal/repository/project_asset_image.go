package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

// ErrProjectAssetVersionChanged prevents a late image from replacing newer work.
var ErrProjectAssetVersionChanged = errors.New("project asset version changed")

type ProjectAssetImageReference struct {
	AssetID    string
	ResourceID string
}

// ProjectAssetImageReferences returns only server-owned historical image bindings.
func (r *Repository) ProjectAssetImageReferences(userID string, assetIDs []string) ([]ProjectAssetImageReference, error) {
	references := []ProjectAssetImageReference{}
	if len(assetIDs) == 0 {
		return references, nil
	}
	err := r.db.Table("asset_representations").
		Select("asset_versions.asset_id, asset_representations.resource_id").
		Joins("JOIN asset_versions ON asset_versions.id = asset_representations.asset_version_id").
		Joins("JOIN assets ON assets.id = asset_versions.asset_id").
		Where("assets.user_id = ? AND assets.id IN ? AND assets.category IN ? AND asset_representations.media_type = ?", userID, assetIDs, []model.AssetCategory{model.AssetCategoryEnvironment, model.AssetCategoryProp}, "image").
		Scan(&references).Error
	return references, err
}

// RegisteredMediaVersion validates replay identity rather than treating a task/role
// match as globally unique. A persisted version wins; legacy records choose the
// earliest asset version, never a later provenance copy.
func (r *Repository) RegisteredMediaVersion(assetID, taskID, resourceID, versionID string, roles []string) (string, error) {
	var reps []model.AssetRepresentation
	query := r.db.Where("task_id = ? AND role IN ?", taskID, roles)
	if versionID != "" {
		query = query.Where("asset_version_id = ?", versionID)
	}
	if err := query.Find(&reps).Error; err != nil {
		return "", err
	}
	versions := map[string]*model.AssetVersion{}
	found := map[string]map[string]bool{}
	for _, rep := range reps {
		version, err := r.AssetVersion(rep.AssetVersionID)
		if err != nil {
			return "", err
		}
		if version.AssetID != assetID || rep.ResourceID != resourceID || rep.MediaType != "image" {
			return "", gorm.ErrInvalidData
		}
		versions[version.ID] = version
		if found[version.ID] == nil {
			found[version.ID] = map[string]bool{}
		}
		found[version.ID][rep.Role] = true
	}
	var earliest *model.AssetVersion
	for id, version := range versions {
		if len(found[id]) != len(roles) {
			return "", gorm.ErrInvalidData
		}
		if earliest == nil || version.Version < earliest.Version || (version.Version == earliest.Version && version.ID < earliest.ID) {
			earliest = version
		}
	}
	if earliest != nil {
		return earliest.ID, nil
	}
	if versionID != "" {
		return "", gorm.ErrInvalidData
	}
	return "", nil
}

// CommitProjectAssetImage atomically promotes one image version, retaining historical references.
func (r *Repository) CommitProjectAssetImage(projectID string, expectedVersionID string, asset *model.Asset, version *model.AssetVersion, representation *model.AssetRepresentation) (bool, error) {
	applied := false
	err := r.WithStoryboardAssetWrite(projectID, asset.ID, func(owned *Repository) error {
		return owned.ProfileWriteTransaction(func(repo *Repository) error {
			applied = false
			tx := repo.db
			if _, err := repo.LockProfileAsset(asset.UserID, asset.ID); err != nil {
				return err
			}
			existing, err := repo.RegisteredMediaVersion(asset.ID, representation.TaskID, representation.ResourceID, "", []string{"primary"})
			if err != nil {
				return err
			}
			if existing != "" {
				return nil
			}
			if _, err := New(tx).ProjectForUser(asset.UserID, projectID); err != nil {
				return err
			}
			if err := New(tx).LockAssetImageResources(asset.UserID, version.DefinitionJSON, []model.AssetRepresentation{*representation}); err != nil {
				return err
			}
			// 与物理清理竞争时持有资源行锁，提交引用前不得删除该资源。
			var resource model.Resource
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
				Where("id = ? AND user_id = ? AND kind = ? AND status = ?", representation.ResourceID, asset.UserID, "image", model.ResourceStatusReady).
				First(&resource).Error; err != nil {
				return err
			}
			result := tx.Model(&model.Asset{}).
				Where("id = ? AND user_id = ? AND primary_version_id = ? AND category = ?", asset.ID, asset.UserID, expectedVersionID, asset.Category).
				Where("id IN (?)", tx.Model(&model.ProjectAssetLink{}).Select("asset_id").Where("project_id = ?", projectID)).
				Updates(map[string]any{"kind": asset.Kind, "payload_json": asset.PayloadJSON, "primary_version_id": version.ID, "status": asset.Status, "updated_at": asset.UpdatedAt})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrProjectAssetVersionChanged
			}
			if err := tx.Create(version).Error; err != nil {
				return err
			}
			if err := tx.Create(representation).Error; err != nil {
				return err
			}
			// 资产可能被多个项目引用，所有关联项目都需要刷新。
			if err := tx.Model(&model.Project{}).
				Where("id IN (?)", tx.Model(&model.ProjectAssetLink{}).Select("project_id").Where("asset_id = ?", asset.ID)).
				Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": time.Now()}).Error; err != nil {
				return err
			}
			applied = true
			return nil
		})
	})
	return applied, err
}
