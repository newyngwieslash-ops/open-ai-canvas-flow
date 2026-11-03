package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"yingce/backend/internal/model"
)

func profileBaselineJSON(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProfileAnalysisBaselineHistoricalProjection(t *testing.T) {
	for _, category := range []model.AssetCategory{model.AssetCategoryCharacter, model.AssetCategoryEnvironment, model.AssetCategoryProp} {
		t.Run(string(category), func(t *testing.T) {
			s, db, p, asset, task := profileAdoptFixture(t)
			raw := `{"appearance":null,"physique":false,"props":["sword",2],"description":{"n":9007199254740993},"prompt":null,"name":"PRIVATE_NAME","voice":"PRIVATE_VOICE","referenceImage":{"resourceId":"image-1"},"_profileAnalysisAdoption":{"requestDigest":"PRIVATE_RECEIPT"}}`
			if err := db.Model(&model.AssetVersion{}).Where("id = ?", asset.PrimaryVersionID).Update("definition_json", raw).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.AssetVersion{ID: "new-current", AssetID: asset.ID, Version: 2, DefinitionJSON: `{"description":"CURRENT_NOT_BASELINE"}`}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Asset{}).Where("id = ?", asset.ID).Updates(map[string]any{"category": category, "primary_version_id": "new-current"}).Error; err != nil {
				t.Fatal(err)
			}
			b, _ := profileBinding(task.InputJSON)
			b.Category = category
			input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": b}})
			task.InputJSON, task.Status = string(input), model.TaskStatusRunning
			if err := db.Save(&task).Error; err != nil {
				t.Fatal(err)
			}
			detail, err := s.ProjectProfileAnalysis("user-1", p, asset.ID, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			envelope := profileBaselineJSON(t, detail)
			if len(envelope["baseline"]) == 0 {
				t.Fatal("detail omitted authorized historical baseline")
			}
			var baseline struct {
				VersionID string                     `json:"versionId"`
				Fields    map[string]json.RawMessage `json:"fields"`
			}
			if err := json.Unmarshal(envelope["baseline"], &baseline); err != nil {
				t.Fatal(err)
			}
			if baseline.VersionID != asset.PrimaryVersionID || detail.CurrentVersionID != "new-current" {
				t.Fatal("baseline substituted current version")
			}
			if category == model.AssetCategoryCharacter {
				if len(baseline.Fields) != 3 || string(baseline.Fields["appearance"]) != "null" || string(baseline.Fields["physique"]) != "false" || string(baseline.Fields["props"]) != `["sword",2]` {
					t.Fatalf("lost historical JSON types/missing: %s", envelope["baseline"])
				}
				if _, exists := baseline.Fields["clothing"]; exists {
					t.Fatal("missing became present")
				}
			} else if len(baseline.Fields) != 2 || string(baseline.Fields["description"]) != `{"n":9007199254740993}` || string(baseline.Fields["prompt"]) != "null" {
				t.Fatalf("lost object/number/null: %s", envelope["baseline"])
			}
			for _, secret := range []string{"PRIVATE", "referenceImage", "_profileAnalysisAdoption"} {
				if strings.Contains(string(envelope["baseline"]), secret) {
					t.Fatal("baseline leaked unrelated definition")
				}
			}
			list, err := s.ProjectProfileAnalyses("user-1", p, asset.ID, "", 20)
			if err != nil || len(list.Analyses) != 1 {
				t.Fatalf("list: %+v %v", list, err)
			}
			listed := profileBaselineJSON(t, list.Analyses[0])
			if _, exists := listed["baseline"]; exists {
				t.Fatal("list exposed baseline")
			}
			generic, _ := json.Marshal(taskForOutput(task))
			if strings.Contains(string(generic), "baseline") || strings.Contains(string(generic), "PRIVATE") {
				t.Fatal("generic task exposed baseline/receipt")
			}
		})
	}
}

func TestProfileAnalysisBaselineUnavailableBlocksFirstAdoption(t *testing.T) {
	for _, raw := range []string{"missing", `{`, `null`, `[]`, `{"description":"a","description":"b","referenceImage":{"resourceId":"image-1"}}`} {
		t.Run(raw, func(t *testing.T) {
			s, db, p, asset, task := profileAdoptFixture(t)
			if raw == "missing" {
				if err := db.Delete(&model.AssetVersion{}, "id = ?", asset.PrimaryVersionID).Error; err != nil {
					t.Fatal(err)
				}
			} else if err := db.Model(&model.AssetVersion{}).Where("id = ?", asset.PrimaryVersionID).Update("definition_json", raw).Error; err != nil {
				t.Fatal(err)
			}
			detail, err := s.ProjectProfileAnalysis("user-1", p, asset.ID, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if detail.CanAdopt || detail.BlockedReason != "asset_profile_analysis_baseline_unavailable" {
				t.Fatalf("unavailable baseline is adoptable/not explained: %+v", detail)
			}
			if _, ok := profileBaselineJSON(t, detail)["baseline"]; ok {
				t.Fatal("invented unavailable baseline")
			}
			_, err = s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"})
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Reason != "asset_profile_analysis_baseline_unavailable" {
				t.Fatalf("first adoption not safely rejected: %v", err)
			}
			current, _ := s.repo.AssetForUser("user-1", asset.ID)
			saved, _ := s.repo.Task(task.ID)
			result, _ := validatedProfileResult(*saved)
			if current.PrimaryVersionID != asset.PrimaryVersionID || result.Adoption != nil {
				t.Fatal("unavailable baseline wrote adoption")
			}
		})
	}
}

func TestProfileAnalysisBaselineOwnership(t *testing.T) {
	for _, target := range []string{"user", "task-user", "task-project", "binding-asset", "binding-category", "version-asset", "project-link"} {
		t.Run(target, func(t *testing.T) {
			s, db, p, asset, task := profileAdoptFixture(t)
			user := "user-1"
			var err error
			switch target {
			case "user":
				user = "other"
			case "task-user":
				err = db.Model(&model.Task{}).Where("id = ?", task.ID).Update("user_id", "other").Error
			case "task-project":
				err = db.Model(&model.Task{}).Where("id = ?", task.ID).Update("project_id", "other").Error
			case "version-asset":
				err = db.Model(&model.AssetVersion{}).Where("id = ?", asset.PrimaryVersionID).Update("asset_id", "foreign").Error
			case "project-link":
				err = db.Exec("DELETE FROM project_asset_links WHERE asset_id = ?", asset.ID).Error
			default:
				b, _ := profileBinding(task.InputJSON)
				if target == "binding-asset" {
					b.AssetID = "foreign"
				} else {
					b.Category = model.AssetCategoryCharacter
				}
				input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": b}})
				err = db.Model(&model.Task{}).Where("id = ?", task.ID).Update("input_json", string(input)).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ProjectProfileAnalysis(user, p, asset.ID, task.ID)
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != 404 {
				t.Fatalf("detail ownership not rejected: %v", err)
			}
			_, err = s.AdoptProjectProfileAnalysis(user, p, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"})
			if !errors.As(err, &appErr) || appErr.Status != 404 {
				t.Fatalf("adoption ownership not rejected: %v", err)
			}
		})
	}
}

func TestProfileAnalysisBaselineMissingDoesNotBreakExactReceiptReplay(t *testing.T) {
	s, db, p, asset, task := profileAdoptFixture(t)
	req := AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"}
	first, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.AssetVersion{}, "id = ?", asset.PrimaryVersionID).Error; err != nil {
		t.Fatal(err)
	}
	for _, cleaned := range []bool{false, true} {
		if cleaned {
			if err := db.Delete(&model.Task{}, "id = ?", task.ID).Error; err != nil {
				t.Fatal(err)
			}
		}
		replay, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req)
		if err != nil || !replay.Replayed || replay.AdoptedVersionID != first.AdoptedVersionID {
			t.Fatalf("exact replay failed after baseline removal (cleaned=%v): %+v %v", cleaned, replay, err)
		}
	}
	_, err = s.ProjectProfileAnalysis("user-1", p, asset.ID, task.ID)
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != 404 {
		t.Fatalf("missing task must remain 404: %v", err)
	}
	req.SelectedFields = []string{"prompt"}
	if _, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req); err == nil {
		t.Fatal("nonexact durable replay accepted")
	}
}

func TestProfileAnalysisBaselineSourceUnavailableBlocksView(t *testing.T) {
	for _, status := range []string{"missing", "deleting", "deleted"} {
		t.Run(status, func(t *testing.T) {
			s, db, p, asset, task := profileAdoptFixture(t)
			if status == "missing" {
				if err := db.Delete(&model.Resource{}, "id = ?", "image-1").Error; err != nil {
					t.Fatal(err)
				}
			} else if err := db.Model(&model.Resource{}).Where("id = ?", "image-1").Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			view, err := s.ProjectProfileAnalysis("user-1", p, asset.ID, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if view.CanAdopt || view.BlockedReason != "asset_profile_analysis_source_unavailable" {
				t.Fatalf("missing/deleting source not blocked: %+v", view)
			}
		})
	}
}
