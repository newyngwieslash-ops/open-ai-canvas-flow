package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"yingce/backend/internal/model"
)

func TestStoryboardAssetGeneratedFix1SevenVersions(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	refs := []map[string]string{}
	for i := 0; i < 7; i++ {
		id, v := fmt.Sprintf("extra-%d", i), fmt.Sprintf("extra-v-%d", i)
		for _, entry := range []any{&model.Asset{ID: id, UserID: "u", Title: id, Category: model.AssetCategoryProp, Status: "draft", PrimaryVersionID: v}, &model.AssetVersion{ID: v, AssetID: id, Version: 1, Status: "draft", DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: fmt.Sprintf("extra-link-%d", i), ProjectID: "p", AssetID: id}} {
			if err := db.Create(entry).Error; err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, id)
		refs = append(refs, map[string]string{"assetId": id, "category": "prop"})
	}
	input, _ := json.Marshal(map[string]any{"mode": "text", "chapterAssetContext": map[string]any{"projectId": "p", "unitId": "unit", "operation": "storyboard", "allowedAssetIds": ids}})
	output, _ := json.Marshal(map[string]any{"rows": []any{map[string]any{"plotDescription": "door", "projectAssetRefs": refs}}})
	if err := db.Create(&model.Task{ID: "seven", UserID: "u", ProjectID: "p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusSucceeded, InputJSON: string(input), ResultJSON: string(output)}).Error; err != nil {
		t.Fatal(err)
	}
	resolution, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "seven"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Rows[0].AssetVersionIDs) != 7 {
		t.Fatalf("legal set truncated: %v", resolution.Rows[0].AssetVersionIDs)
	}
	for _, candidate := range resolution.Rows[0].Candidates {
		if candidate.DefaultSelected {
			t.Fatal("over-capacity row must not preselect any version")
		}
	}
	req := ReplaceProjectUnitShotsRequest{ExpectedShotIDs: []string{"s"}, GeneratedBinding: &GeneratedBindingInput{TaskID: "seven", Baseline: resolution.Baseline, RowIndices: []int{0}}, Shots: []ReplaceProjectUnitShotInput{{CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头01", Description: "door", DurationMs: 6000, Revision: ShotRevisionInput{PlotDescription: "door", DurationMs: 6000, ActionBeats: []map[string]any{}}}, AssetVersionIDs: []string{"extra-v-6"}}}}
	shots, err := s.ReplaceProjectUnitShots("u", "p", "unit", req)
	if err != nil {
		t.Fatal(err)
	}
	var saved []model.ShotAssetReference
	if err := db.Where("shot_id = ?", shots[0].ID).Find(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].AssetVersionID != "extra-v-6" {
		t.Fatalf("seventh choice lost: %+v", saved)
	}
}

func TestStoryboardAssetGeneratedFix1BindsScript(t *testing.T) {
	for _, mode := range []string{"swap_content", "duration", "dialogue", "image_prompt", "video_prompt", "action", "beats", "normalized", "normalized_duration_array", "selected_prompt"} {
		t.Run(mode, func(t *testing.T) {
			s, db := generatedFixture(t)
			if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
				t.Fatal(err)
			}
			task := model.Task{ID: "script", UserID: "u", ProjectID: "p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusSucceeded, InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":[]}}`, ResultJSON: `{"rows":[{"plotDescription":{"text":" A "},"durationSeconds":"2.25","dialogue":["甲",{"content":"乙"}],"shotSize":true,"camera":12,"motion":" pan ","narrativeIntent":" intent ","timeBeats":{"description":" beat "},"imageGenerationPrompt":" image ","videoMotionPrompt":" video ","negativePrompt":" blur ","continuityOut":" end "},{"plotDescription":"B"}]}`}
			if mode == "normalized_duration_array" {
				task.ResultJSON = strings.Replace(task.ResultJSON, `"durationSeconds":"2.25"`, `"durationSeconds":[2.25]`, 1)
			}
			if mode == "selected_prompt" {
				task.InputJSON = `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a","b"]}}`
				task.ResultJSON = `{"rows":[{"plotDescription":"A","durationSeconds":2.25,"dialogue":"甲、乙","shotSize":true,"camera":12,"motion":"pan","narrativeIntent":"intent","timeBeats":"beat","imageGenerationPrompt":"image","videoMotionPrompt":"video","negativePrompt":"blur","continuityOut":"end","projectAssetRefs":[{"assetId":"a","category":"character"},{"assetId":"b","category":"prop"}]},{"plotDescription":"B"}]}`
			}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			resolution, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "script"})
			if err != nil {
				t.Fatal(err)
			}
			req := ReplaceProjectUnitShotsRequest{ExpectedShotIDs: []string{"s"}, GeneratedBinding: &GeneratedBindingInput{TaskID: "script", Baseline: resolution.Baseline, RowIndices: []int{0, 1}}, Shots: []ReplaceProjectUnitShotInput{
				{CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头01", Description: "A", DurationMs: 2250, Revision: ShotRevisionInput{PlotDescription: "A", DurationMs: 2250, Dialogue: "甲、乙", ShotSize: "true", CameraAngle: "12", CameraMovement: "pan", Action: "叙事意图：intent\n时间节拍：beat", ActionBeats: []map[string]any{{"description": "beat"}}, ImagePrompt: "image", VideoPrompt: "video", NegativePrompt: "blur", ContinuityNotes: "end"}}},
				{CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头02", Description: "B", DurationMs: 6000, Revision: ShotRevisionInput{PlotDescription: "B", DurationMs: 6000, ActionBeats: []map[string]any{}}}},
			}}
			switch mode {
			case "swap_content":
				req.Shots[0].Description = "B"
				req.Shots[0].Revision.PlotDescription = "B"
				req.Shots[1].Description = "A"
				req.Shots[1].Revision.PlotDescription = "A"
			case "duration":
				req.Shots[0].Revision.DurationMs = 9000
			case "dialogue":
				req.Shots[0].Revision.Dialogue = "changed"
			case "image_prompt":
				req.Shots[0].Revision.ImagePrompt = "changed"
			case "video_prompt":
				req.Shots[0].Revision.VideoPrompt = "changed"
			case "action":
				req.Shots[0].Revision.Action = "changed"
			case "beats":
				req.Shots[0].Revision.ActionBeats = []map[string]any{{"description": "changed"}}
			}
			if mode == "selected_prompt" {
				req.Shots[0].AssetVersionIDs = []string{"v", "vb"}
				req.Shots[0].Revision.VideoPrompt = "video\n\n【角色参考】\n阿青：@[asset:a]"
			}
			before := previewState(t, db)
			shots, err := s.ReplaceProjectUnitShots("u", "p", "unit", req)
			if mode == "normalized" || mode == "normalized_duration_array" || mode == "selected_prompt" {
				if err != nil {
					t.Fatal(err)
				}
				if len(shots) != 2 || shots[0].Description != "A" || shots[0].DurationMs != 2250 {
					t.Fatalf("normalized script lost: %+v", shots)
				}
				return
			}
			if err == nil {
				t.Fatalf("accepted %s script unrelated to its task row", mode)
			}
			if previewState(t, db) != before {
				t.Fatal("script rejection changed old complete state")
			}
		})
	}
}
