package repository

import (
	"encoding/json"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	assetrefs "yingce/backend/internal/assets"
	"yingce/backend/internal/model"
)

// LockAssetImageResources serializes persisted version references with resource deletion.
func (r *Repository) LockAssetImageResources(userID string, definition string, representations []model.AssetRepresentation) error {
	ids := map[string]struct{}{}
	var value struct {
		ReferenceImages []struct {
			ResourceID string `json:"resourceId"`
		} `json:"referenceImages"`
		ReferenceImage *struct {
			ResourceID string `json:"resourceId"`
		} `json:"referenceImage"`
	}
	if err := json.Unmarshal([]byte(definition), &value); err != nil {
		return err
	}
	for _, item := range value.ReferenceImages {
		if item.ResourceID != "" {
			ids[item.ResourceID] = struct{}{}
		}
	}
	if value.ReferenceImage != nil && value.ReferenceImage.ResourceID != "" {
		ids[value.ReferenceImage.ResourceID] = struct{}{}
	}
	for _, rep := range representations {
		if rep.ResourceID != "" {
			ids[rep.ResourceID] = struct{}{}
		}
	}
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		var resource model.Resource
		if err := r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ? AND kind = ? AND status = ?", id, userID, "image", model.ResourceStatusReady).First(&resource).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) CreateProjectEntity(asset *model.Asset, version *model.AssetVersion, link *model.ProjectAssetLink, reps []model.AssetRepresentation) error {
	return r.WithStoryboardProjectWrite(link.ProjectID, func(tx *Repository) error {
		if err := lockResourceLifecycle(tx.db); err != nil { return err }
		if _, err := tx.ProjectForUser(asset.UserID, link.ProjectID); err != nil {
			return err
		}
		if link.FolderID != "" {
			if _, err := tx.ProjectAssetFolder(link.ProjectID, link.FolderID); err != nil {
				return err
			}
		}
		if err := tx.LockAssetImageResources(asset.UserID, version.DefinitionJSON, reps); err != nil {
			return err
		}
		if err := tx.db.Create(asset).Error; err != nil {
			return err
		}
		if err := tx.db.Create(version).Error; err != nil {
			return err
		}
		if len(reps) > 0 {
			if err := tx.db.Create(&reps).Error; err != nil {
				return err
			}
		}
		if err := tx.db.Create(link).Error; err != nil {
			return err
		}
		return tx.BumpProjectRevision(link.ProjectID)
	})
}

// CommitAssetReferenceVersion keeps all current representations while changing only the definition.
func (r *Repository) CommitAssetReferenceVersion(projectID, expected string, asset *model.Asset, version *model.AssetVersion, reps []model.AssetRepresentation, voice *model.CharacterVoiceBinding) error {
	return r.WithStoryboardAssetWrite(projectID, asset.ID, func(owned *Repository) error {
	return owned.ProfileWriteTransaction(func(tx *Repository) error {
		if _, err := tx.LockProfileAsset(asset.UserID, asset.ID); err != nil { return err }
		if _, err := tx.ProjectForUser(asset.UserID, projectID); err != nil {
			return err
		}
		linked, err := tx.ProjectAssetLinked(projectID, asset.ID)
		if err != nil {
			return err
		}
		if !linked {
			return gorm.ErrRecordNotFound
		}
		return tx.SaveCharacterVersion(projectID, asset, version, reps, voice, expected)
	})
	})
}

func (r *Repository) LockImageCandidateTask(userID, taskID string) (*model.Task, error) {
	var task model.Task
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", taskID, userID).First(&task).Error
	return &task, err
}

func (r *Repository) UpdateImageCandidateResult(userID, taskID, result string) error {
    return r.resourceLifecycleTransaction(func(repo *Repository) error {
    if err := repo.requireLiveDocumentResources(userID, result); err != nil { return err }
	updated := repo.db.Model(&model.Task{}).Where("id = ? AND user_id = ? AND status = ?", taskID, userID, model.TaskStatusSucceeded).Updates(map[string]any{"result_json": result, "updated_at": time.Now()})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrTaskStateConflict
	}
	return nil
    })
}

// RequireNoOriginalImageReferences protects originals even on explicit material purges.
// It runs after resource row locks: a concurrent new reference either commits first
// and is visible here, or waits and fails when the resource has been removed.
func (r *Repository) RequireNoOriginalImageReferences(resourceIDs []string) error {
	if len(resourceIDs) == 0 {
		return nil
	}
	var tasks []model.Task
	if err := r.db.Select("input_json", "result_json").Where("operation = ?", "asset_image_profile_analysis").Find(&tasks).Error; err != nil { return err }
	wantedTasks := map[string]struct{}{}
	for _, id := range resourceIDs { wantedTasks[id] = struct{}{} }
	for _, task := range tasks {
		if assetrefs.DocumentReferences(task.InputJSON, wantedTasks) || assetrefs.DocumentReferences(task.ResultJSON, wantedTasks) { return ErrOriginalImageReferenced }
	}
	var versions []model.AssetVersion
	if err := r.db.Where("definition_json LIKE ? OR definition_json LIKE ? OR definition_json LIKE ?", "%\"referenceImages\"%", "%\"referenceImage\"%", "%\"_profileAnalysisAdoption\"%").Find(&versions).Error; err != nil {
		return err
	}
	wanted := map[string]struct{}{}
	for _, id := range resourceIDs {
		wanted[id] = struct{}{}
	}
	for _, version := range versions {
		var value struct {
			ReferenceImages json.RawMessage `json:"referenceImages"`
			ReferenceImage  json.RawMessage `json:"referenceImage"`
		}
		if err := json.Unmarshal([]byte(version.DefinitionJSON), &value); err != nil {
			return err
		}
		if assetrefs.DocumentReferences(version.DefinitionJSON, wanted) {
			return ErrOriginalImageReferenced
		}
	}
	return nil
}

var ErrOriginalImageReferenced = &originalImageReferenceError{}

type originalImageReferenceError struct{}

func (*originalImageReferenceError) Error() string {
	return "资源仍被资产版本的原始参考图引用"
}

func (r *Repository) ResourceByID(resourceID string) (*model.Resource, error) {
	var resource model.Resource
	if err := r.db.First(&resource, "id = ?", resourceID).Error; err != nil {
		return nil, err
	}
	return &resource, nil
}

func (r *Repository) AllAssetVersionsOrdered() ([]model.AssetVersion, error) {
	var versions []model.AssetVersion
	err := r.db.Order("id asc").Find(&versions).Error
	return versions, err
}

func (r *Repository) UpdateAssetVersionDefinition(versionID, definitionJSON string) error {
	return r.db.Model(&model.AssetVersion{}).Where("id = ?", versionID).Update("definition_json", definitionJSON).Error
}

func (r *Repository) LegacyReferencePayloadAssets() ([]model.Asset, error) {
	var assets []model.Asset
	err := r.db.Where("payload_json LIKE ?", "%referenceImage%").Order("id asc").Find(&assets).Error
	return assets, err
}

func (r *Repository) UpdateAssetPayloadJSON(assetID, payloadJSON string) error {
	return r.db.Model(&model.Asset{}).Where("id = ?", assetID).Update("payload_json", payloadJSON).Error
}

