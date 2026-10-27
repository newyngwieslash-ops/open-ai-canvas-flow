package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

var ErrProfileSourceUnavailable = errors.New("profile analysis source unavailable")
var ErrProfileSourceChanged = errors.New("profile analysis source changed")
var ErrProfileWriteBusy = errors.New("profile analysis write busy")

// LockProfileAsset is the first lock in all version writes; SQLite must obtain
// its writer reservation before any reads, not upgrade a stale read snapshot.
func (r *Repository) LockProfileAsset(userID, assetID string) (*model.Asset, error) {
	if r.Dialect() == "sqlite" {
		result := r.db.Model(&model.Asset{}).Where("id = ? AND user_id = ?", assetID, userID).UpdateColumn("primary_version_id", gorm.Expr("primary_version_id"))
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	var asset model.Asset
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", assetID, userID).First(&asset).Error
	if err == nil && asset.Status == model.AssetVersionStatusDeleting {
		return nil, ErrProjectAssetVersionChanged
	}
	return &asset, err
}
func (r *Repository) ProfileWriteTransaction(fn func(*Repository) error) error {
	// A savepoint cannot refresh the outer snapshot. Preserve its repository
	// context/hooks and return raw errors so only the transaction owner retries.
	if _, nested := r.db.Statement.ConnPool.(gorm.TxCommitter); nested {
		// Owner-coordinated callers now own the outer transaction. Preserve the
		// existing lifecycle fence before entering their profile row-lock code.
		if _, coordinated := r.db.Statement.Context.Value(storyboardOwnersContextKey{}).([]string); coordinated {
			if err := lockResourceLifecycle(r.db); err != nil { return err }
		}
		return fn(r)
	}
	for attempt := 0; attempt < 4; attempt++ {
		err := r.Transaction(func(tx *Repository) error {
			// Lifecycle serialization precedes all asset/resource row locks.
			if err := lockResourceLifecycle(tx.db); err != nil {
				return err
			}
			return fn(tx)
		})
		if err == nil {
			return nil
		}
		text := strings.ToLower(err.Error())
		retry := strings.Contains(text, "database is locked") || strings.Contains(text, "database table is locked") || strings.Contains(text, "busy_snapshot") || strings.Contains(text, "sqlstate 40001") || strings.Contains(text, "sqlstate 40p01")
		if !retry {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 15 * time.Millisecond)
	}
	return ErrProfileWriteBusy
}

// RequireProfileSource is called INSIDE creation/precharge and completion transactions.
// Completion deliberately validates the historical base, not the current head.
func (r *Repository) RequireProfileSource(task *model.Task, current bool) error {
	if task.Operation != "asset_image_profile_analysis" {
		return nil
	}
	var input struct {
		Metadata struct {
			Operation string `json:"operation"`
			Binding   struct {
				ProjectID     string              `json:"projectId"`
				AssetID       string              `json:"assetId"`
				BaseVersionID string              `json:"baseVersionId"`
				Category      model.AssetCategory `json:"category"`
				Source        struct {
					ResourceID string `json:"resourceId"`
				} `json:"source"`
			} `json:"profileAnalysis"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(task.InputJSON), &input) != nil || input.Metadata.Operation != task.Operation {
		return ErrProfileSourceChanged
	}
	b := input.Metadata.Binding
	var asset *model.Asset
	var err error
	if current { asset, err = r.LockProfileAsset(task.UserID, b.AssetID) } else { asset, err = r.AssetForUser(task.UserID, b.AssetID) }
	if err != nil { return err }
	if b.ProjectID != task.ProjectID || b.Category != asset.Category {
		return ErrProfileSourceChanged
	}
	if _, err := r.ProjectForUser(task.UserID, b.ProjectID); err != nil {
		return err
	}
	linked, err := r.ProjectAssetLinked(b.ProjectID, b.AssetID)
	if err != nil {
		return err
	}
	if !linked {
		return gorm.ErrRecordNotFound
	}
	if current && asset.PrimaryVersionID != b.BaseVersionID {
		return ErrProjectAssetVersionChanged
	}
	version, err := r.AssetVersion(b.BaseVersionID)
	if err != nil {
		return err
	}
	var definition struct {
		ReferenceImage struct {
			ResourceID string `json:"resourceId"`
		} `json:"referenceImage"`
	}
	if version.AssetID != asset.ID || json.Unmarshal([]byte(version.DefinitionJSON), &definition) != nil || definition.ReferenceImage.ResourceID != b.Source.ResourceID || b.Source.ResourceID == "" {
		return ErrProfileSourceChanged
	}
	var resource model.Resource
	err = r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ? AND status = ? AND kind = ?", b.Source.ResourceID, task.UserID, model.ResourceStatusReady, "image").First(&resource).Error
	if err != nil || resource.Width <= 0 || resource.Height <= 0 || resource.Size <= 0 || !strings.HasPrefix(resource.MimeType, "image/") {
		return ErrProfileSourceUnavailable
	}
	return nil
}

// CommitProfileAnalysisVersion runs on the caller's locked transaction repository.
func (r *Repository) CommitProfileAnalysisVersion(projectID, expected string, asset *model.Asset, version *model.AssetVersion, reps []model.AssetRepresentation, voice *model.CharacterVoiceBinding, task *model.Task, result string) error {
	if err := r.SaveCharacterVersion(projectID, asset, version, reps, voice, expected); err != nil {
		return err
	}
	return r.UpdateImageCandidateResult(task.UserID, task.ID, result)
}

func (r *Repository) ProfileAnalysisTasks(userID, projectID, assetID, before string, limit int) ([]model.Task, error) {
	query := func() *gorm.DB {
		return r.db.Model(&model.Task{}).Where("user_id = ? AND project_id = ? AND type = ? AND operation = ?", userID, projectID, "canvas_text", "asset_image_profile_analysis").Where(r.taskInputJSONField("metadata", "profileAnalysis", "assetId")+" = ?", assetID)
	}
	q := query()
	if before != "" {
		var cursor model.Task
		if err := query().Where("id = ?", before).First(&cursor).Error; err != nil {
			return nil, err
		}
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	tasks := []model.Task{}
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}
