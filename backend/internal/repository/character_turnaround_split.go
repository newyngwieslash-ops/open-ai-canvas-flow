package repository

import (
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// FindTurnaroundSplitTaskByIdempotency finds an existing local turnaround split task for a user, project, and creation submission key.
func (r *Repository) FindTurnaroundSplitTaskByIdempotency(userID, projectID, submissionID string) (*model.Task, error) {
	var task model.Task
	if err := r.db.Where("user_id = ? AND project_id = ? AND operation = ? AND creation_submission_id = ?", userID, projectID, "character_turnaround_split", submissionID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// CommitCharacterTurnaroundSplit atomically merges representations, saves the new character AssetVersion,
// and records the idempotent Task receipt under the storyboard-owner lock.
func (r *Repository) CommitCharacterTurnaroundSplit(
	projectID string,
	asset *model.Asset,
	expectedVersionID string,
	nextVersion *model.AssetVersion,
	mergedReps []model.AssetRepresentation,
	nextVoice *model.CharacterVoiceBinding,
	taskRow *model.Task,
	now time.Time,
) error {
	return r.WithStoryboardAssetWrite(projectID, asset.ID, func(owned *Repository) error {
		return owned.ProfileWriteTransaction(func(repo *Repository) error {
			tx := repo.db
			if _, err := repo.LockProfileAsset(asset.UserID, asset.ID); err != nil {
				return err
			}
			if err := saveCharacterVersion(tx, asset, nextVersion, mergedReps, nextVoice, expectedVersionID); err != nil {
				return err
			}
			projectIDs := []string{}
			if err := tx.Model(&model.ProjectAssetLink{}).Where("asset_id = ?", asset.ID).Distinct("project_id").Order("project_id").Pluck("project_id", &projectIDs).Error; err != nil {
				return err
			}
			for _, linkedProjectID := range projectIDs {
				if err := tx.Model(&model.Project{}).Where("id = ?", linkedProjectID).
					Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": now}).Error; err != nil {
					return err
				}
			}
			return tx.Create(taskRow).Error
		})
	})
}
