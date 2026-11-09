package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/prompts"
)

func TestGenerationInheritsStoredProjectStyleWithoutClientSnapshot(t *testing.T) {
	s, db := newProjectWorkflowV2TestService(t)
	project, _ := seedWorkflowProject(t, db)
	profile := styleProfileDocument{
		SchemaVersion: 1, PresetID: "guoman-2d", Title: "国漫 2D",
		Prompt: "国漫 2D，稳定线稿、赛璐璐明暗；禁止真人照片。",
		Source: "builtin", Revision: 1,
		Assets: []styleProfileAsset{},
	}
	if err := db.Model(&project).Updates(map[string]any{"style_preset_id": profile.PresetID, "style_profile_json": prompts.MustStyleProfileJSON(profile)}).Error; err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []string{"storyboard", "action_board", "video"} {
		t.Run(artifact, func(t *testing.T) {
			input := canvasGenerationInput{
				Mode: "image", Prompt: "古风写实，女子织布。",
				Config:   providerConfig{Model: "test-model"},
				Metadata: map[string]any{"source": "short-drama-workflow", "artifactType": artifact},
			}
			if artifact == "video" {
				input.Mode = "video"
			}
			if err := s.applyGenerationStyleProfile(project.UserID, project.ID, &input); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(input.Prompt, profile.Prompt) {
				t.Fatalf("project style missing from model prompt: %s", input.Prompt)
			}
			if artifact == "action_board" && !strings.Contains(input.Prompt, "黑白") {
				t.Fatal("action board must retain monochrome presentation with project medium")
			}
		})
	}
	input := canvasGenerationInput{Mode: "image", Prompt: "女子织布。"}
	if err := s.applyGenerationStyleProfile(project.UserID, project.ID, &input); err != nil || !strings.Contains(input.Prompt, profile.Prompt) {
		t.Fatalf("nil metadata must not bypass project style: %v, %s", err, input.Prompt)
	}
	input.Prompt = "独立画布内容"
	if err := s.applyGenerationStyleProfile(project.UserID, "", &input); err != nil || input.Prompt != "独立画布内容" {
		t.Fatalf("unscoped canvas changed: %v, %s", err, input.Prompt)
	}
}

func TestGenerationCannotBypassStoredStrictStyleAtProviderDispatch(t *testing.T) {
	s, db := newProjectWorkflowV2TestService(t)
	project, _ := seedWorkflowProject(t, db)
	profile := styleProfileDocument{
		SchemaVersion: 1, PresetID: "strict-style", Title: "国漫 2D", Prompt: "二维绘画",
		Source: "user", Revision: 1, ExecutionPolicy: "strict-assets",
		Assets: []styleProfileAsset{{ID: "ref", Kind: "reference", Title: "风格参考", Provider: "project", Status: "validated", ReferenceResourceIDs: []string{"ref-1"}}},
	}
	if err := db.Model(&model.Project{}).Where("id = ?", project.ID).Updates(map[string]any{"style_preset_id": profile.PresetID, "style_profile_json": prompts.MustStyleProfileJSON(profile)}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, protocol string }{{"image", "openai-image"}, {"video", "newapi-video"}, {"image", "runninghub-workflow-image"}} {
		t.Run(tc.mode+"/"+tc.protocol, func(t *testing.T) {
			input := canvasGenerationInput{Mode: tc.mode, Prompt: "女子织布。", Config: providerConfig{Model: "test-model", InterfaceType: tc.protocol, BaseURL: "https://8.8.8.8/v1"}}
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.processCanvasGenerationTask(context.Background(), project.UserID, project.ID, "canvas_"+tc.mode, "", string(raw))
			if err == nil || !strings.Contains(err.Error(), "无法完整执行项目画风") {
				t.Fatalf("missing snapshot bypassed style validation: %v", err)
			}
		})
	}
}
