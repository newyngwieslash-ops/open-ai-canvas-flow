package repository

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
	"yingce/backend/internal/model"
)

// taskInputJSONField only accepts internal constant paths; filter values are parameters.
func (r *Repository) taskInputJSONField(path ...string) string {
	if r.Dialect() == "postgres" {
		return "NULLIF(input_json, '')::jsonb #>> '{" + strings.Join(path, ",") + "}'"
	}
	return "json_extract(CASE WHEN json_valid(input_json) THEN input_json ELSE '{}' END, '$." + strings.Join(path, ".") + "')"
}

// ActiveProjectImageTasks filters before LIMIT after the service validates the linked asset.
func (r *Repository) ActiveProjectImageTasks(userID, projectID, assetID string, category model.AssetCategory, limit int) ([]model.Task, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	operation, target, node := "project_asset_image", "projectAssetId", "project-asset-image:"
	if category == model.AssetCategoryCharacter {
		operation, target, node = "character_turnaround", "characterAssetId", "character-turnaround:"
	}
	tasks := []model.Task{}
	err := r.db.Where("user_id = ? AND project_id = ? AND type = ? AND status IN ?", userID, projectID, "canvas_image", []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning}).
		Where("("+r.taskInputJSONField("metadata", "operation")+" = ? AND "+r.taskInputJSONField("metadata", target)+" = ?) OR "+r.taskInputJSONField("nodeId")+" = ?", operation, assetID, node+assetID).
		Order("created_at DESC, id DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ImageCandidateTasks filters before LIMIT; the cursor remains stable when a candidate is adopted.
func (r *Repository) ImageCandidateTasks(userID, projectID, assetID, beforeTaskID string, limit int) ([]model.Task, error) {
	field := func(path string) string {
		if r.Dialect() == "postgres" {
			return "NULLIF(result_json, '')::jsonb #>> '{assetImageCandidate," + path + "}'"
		}
		return "json_extract(CASE WHEN json_valid(result_json) THEN result_json ELSE '{}' END, '$.assetImageCandidate." + path + "')"
	}
	metadata := func(path string) string {
		if r.Dialect() == "postgres" {
			return "NULLIF(input_json, '')::jsonb #>> '{metadata," + path + "}'"
		}
		return "json_extract(CASE WHEN json_valid(input_json) THEN input_json ELSE '{}' END, '$.metadata." + path + "')"
	}
	query := func() *gorm.DB {
		q := r.db.Model(&model.Task{}).Where("user_id = ? AND project_id = ? AND status = ?", userID, projectID, model.TaskStatusSucceeded).
			Where(field("state")+" IN ?", []string{"pending_confirmation", "adopted"}).Where(field("baseVersionId")+" <> ''").
			Where("type = ?", "canvas_image").Where(metadata("adoptionMode")+" = ?", "manual").
			Where(metadata("operation")+" IN ?", []string{"character_turnaround", "project_asset_image"})
		if assetID != "" {
			q = q.Where(field("assetId")+" = ?", assetID)
		} else {
			q = q.Where(field("assetId") + " <> ''")
		}
		return q
	}
	q := query()
	if beforeTaskID != "" {
		var cursor model.Task
		if err := query().Select("id", "created_at").Where("id = ?", beforeTaskID).First(&cursor).Error; err != nil {
			return nil, fmt.Errorf("invalid image candidate cursor: %w", err)
		}
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	tasks := []model.Task{}
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}
