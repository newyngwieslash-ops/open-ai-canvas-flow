package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"

	"strings"
	"testing"
	"time"
	"yingce/backend/internal/database"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func profileMatrixJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func profileMatrixReason(t *testing.T, err error, status int, reason string) {
	t.Helper()
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != status || string(appErr.Reason) != reason {
		t.Fatalf("want %d/%s, got %v", status, reason, err)
	}
}

func TestProfileContractMatrixSchema(t *testing.T) {
	for _, category := range []model.AssetCategory{model.AssetCategoryCharacter, model.AssetCategoryEnvironment, model.AssetCategoryProp} {
		t.Run(string(category), func(t *testing.T) {
			fields := profileFields(category)
			base := map[string]any{}
			for _, field := range fields {
				base[field] = ""
			}
			empty := profileMatrixJSON(t, base)
			t.Run("all_unknown", func(t *testing.T) {
				draft, err := parseProfileDraft(empty, category)
				if err != nil || len(draft) != len(fields) {
					t.Fatalf("all-empty draft: %v %v", draft, err)
				}
				for _, value := range draft {
					if value != "" {
						t.Fatal(draft)
					}
				}
			})
			base[fields[0]] = "界😀"
			multibyte := profileMatrixJSON(t, base)
			base[fields[0]] = ""
			for _, size := range []int{(32 << 10) - 1, 32 << 10, (32 << 10) + 1} {
				t.Run(fmt.Sprintf("utf8_bytes_%d", size), func(t *testing.T) {
					raw := multibyte + strings.Repeat(" ", size-len(multibyte))
					_, err := parseProfileDraft(raw, category)
					if (err == nil) != (size <= 32<<10) {
						t.Fatalf("%d raw bytes: %v", len(raw), err)
					}
				})
			}
			for _, field := range fields {
				t.Run(field, func(t *testing.T) {
					for _, size := range []int{1999, 2000, 2001} {
						value := strings.Repeat("界😀", size/2) + strings.Repeat("界", size%2)
						base[field] = value
						draft, err := parseProfileDraft(profileMatrixJSON(t, base), category)
						if (err == nil) != (size <= 2000) {
							t.Fatalf("%s %d codepoints: %v", field, size, err)
						}
						if err == nil && draft[field] != value {
							t.Fatal("accepted text truncated or rewritten")
						}
					}
					for _, value := range []any{[]any{}, []any{"visible"}, map[string]any{}, map[string]any{"text": "visible"}, nil, 1, true} {
						base[field] = value
						if _, err := parseProfileDraft(profileMatrixJSON(t, base), category); err == nil {
							t.Fatalf("accepted %s as %T", field, value)
						}
					}
					base[field] = "\t visible\n\r"
					draft, err := parseProfileDraft(profileMatrixJSON(t, base), category)
					if err != nil || draft[field] != "visible" {
						t.Fatalf("normal whitespace: %v %v", draft, err)
					}
					base[field] = ""
				})
			}
			for _, raw := range []string{"[]", "null", "```json\n" + empty + "\n```", empty + " false", empty[:len(empty)-1] + `,"unexpected":"x"}`, string([]byte{'{', '"', 0xff, '"', ':', '"', '"', '}'})} {
				if _, err := parseProfileDraft(raw, category); err == nil {
					t.Fatalf("accepted invalid envelope %q", raw)
				}
			}
		})
	}
}

func TestProfileContractMatrixAdmission(t *testing.T) {
	for _, scenario := range []string{"valid_exact_size", "missing_model", "unknown_bytes", "no_images", "actual_size_over_budget", "custom_selection", "not_ready", "no_dimensions", "foreign_resource"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, projectID, asset := profileCreationFixture(t)
			capability := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
			capability.Text.References.MaxImages, capability.Text.References.MaxImageBytes = 1, 100
			req := CreateProfileAnalysisRequest{asset.PrimaryVersionID, "profile-image", ProfileAnalysisModelSelection{Kind: "system", ChannelID: "channel", ModelKey: "text-test"}}
			switch scenario {
			case "missing_model":
				req.ModelSelection.ModelKey = "missing"
			case "unknown_bytes":
				capability.Text.References.MaxImageBytes = 0
			case "no_images":
				capability.Text.References.MaxImages = 0
			case "actual_size_over_budget":
				capability.Text.References.MaxImageBytes = 99
			case "custom_selection":
				req.ModelSelection.Kind = "custom"
			case "not_ready":
				if err := db.Model(&model.Resource{}).Where("id = ?", req.SourceResourceID).Update("status", "deleting").Error; err != nil {
					t.Fatal(err)
				}
			case "no_dimensions":
				if err := db.Model(&model.Resource{}).Where("id = ?", req.SourceResourceID).Update("width", 0).Error; err != nil {
					t.Fatal(err)
				}
			case "foreign_resource":
				if err := db.Model(&model.Resource{}).Where("id = ?", req.SourceResourceID).Update("user_id", "other").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Model(&model.ChannelModel{}).Where("id = ?", "cm").Update("capability_config_json", mustEncodeModelCapabilityConfig(t, capability)).Error; err != nil {
				t.Fatal(err)
			}
			view, err := s.CreateProjectProfileAnalysis("user", projectID, asset.ID, req)
			if scenario == "valid_exact_size" {
				if err != nil {
					t.Fatal(err)
				}
				stored, err := s.repo.Task(view.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				binding, err := profileBinding(stored.InputJSON)
				if err != nil || binding.Source.ResourceID != req.SourceResourceID || binding.BaseVersionID != asset.PrimaryVersionID || view.Status != model.TaskStatusQueued {
					t.Fatalf("binding: %+v %v", binding, err)
				}
				if latest, err := s.repo.AssetForUser("user", asset.ID); err != nil || latest.PrimaryVersionID != asset.PrimaryVersionID {
					t.Fatalf("creation modified asset: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid request entered queue")
			}
			for _, table := range []string{"tasks", "billing_orders", "credit_ledger_entries", "route_attempts", "api_call_logs"} {
				var count int64
				if err := db.Table(table).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("rejected admission left %d %s", count, table)
				}
			}
			account, err := s.repo.CreditAccount("user")
			if err != nil || account.AvailableMicrocredits != 10000 || account.ReservedMicrocredits != 0 {
				t.Fatalf("rejected admission debited: %+v %v", account, err)
			}
		})
	}
}

func TestProfileContractMatrixLogicalReroute(t *testing.T) {
	for _, scenario := range []string{"no_images", "unknown_bytes", "over_budget"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, projectID, asset := profileCreationFixture(t)
			capability := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
			capability.Text.References.MaxImages, capability.Text.References.MaxImageBytes = 1, 100
			spec, err := CapabilitySpecFromModelCapabilityConfig(capability, "text")
			if err != nil {
				t.Fatal(err)
			}
			logical := model.LogicalModel{ID: "matrix-logical", Code: "matrix-logical", Capability: "text", Enabled: true, ActiveRevisionID: "matrix-revision", PricePolicy: "unified", BillingMode: "fixed_request"}
			revision := model.LogicalModelRevision{ID: logical.ActiveRevisionID, LogicalModelID: logical.ID, Version: 1, CapabilitySpecJSON: profileMatrixJSON(t, spec), DefaultOptionsJSON: `{}`}
			first := model.LogicalModelRoute{ID: "matrix-first", LogicalModelRevisionID: revision.ID, ChannelModelID: "cm", Enabled: true, Weight: 1}
			secondModel := model.ChannelModel{ID: "matrix-second-model", ChannelID: "channel", ModelKey: "matrix-second", Capability: "text", Protocol: model.ChannelInterfaceChatCompletion, Enabled: true, PriceConfigured: true, BillingMode: "fixed_request"}
			switch scenario {
			case "no_images":
				capability.Text.References.MaxImages = 0
			case "unknown_bytes":
				capability.Text.References.MaxImageBytes = 0
			case "over_budget":
				capability.Text.References.MaxImageBytes = 99
			}
			secondModel.CapabilityConfigJSON = mustEncodeModelCapabilityConfig(t, capability)
			second := model.LogicalModelRoute{ID: "matrix-second", LogicalModelRevisionID: revision.ID, ChannelModelID: secondModel.ID, Enabled: true, Weight: 1}
			for _, row := range []any{&logical, &revision, &first, &secondModel, &second} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			binding := ProfileAnalysisBinding{1, projectID, asset.ID, asset.Category, asset.PrimaryVersionID, ProjectReferenceImage{"profile-image"}, 1}
			input := map[string]any{"mode": "text", "prompt": "visible only", "config": map[string]any{"channelId": "channel", "model": "text-test"}, "referenceImages": []any{map[string]any{"storageKey": "resource:profile-image", "bytes": 1}}, "metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}}
			task := model.Task{ID: newID(), UserID: "user", ProjectID: projectID, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusRunning, LogicalModelID: logical.ID, LogicalModelRevisionID: revision.ID, RouteID: first.ID, ChannelModelID: "cm", InputJSON: profileMatrixJSON(t, input)}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			before := task
			_, err = s.switchTaskToNextRoute(&task, []model.RouteAttempt{{RouteID: first.ID}})
			if err == nil {
				t.Error("reroute accepted physically unusable vision model")
			}
			if scenario == "unknown_bytes" {
				profileMatrixReason(t, err, 422, "asset_profile_analysis_model_unknown")
			}
			if scenario == "over_budget" {
				profileMatrixReason(t, err, 422, "asset_profile_analysis_model_unsupported")
			}
			stored, readErr := s.repo.Task(task.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if stored.InputJSON != before.InputJSON || stored.RouteID != before.RouteID {
				t.Error("rejected reroute persisted another model")
			}
			var attempts int64
			if err := db.Model(&model.RouteAttempt{}).Count(&attempts).Error; err != nil {
				t.Fatal(err)
			}
			if attempts != 0 {
				t.Errorf("rejected route created %d dispatch attempts", attempts)
			}
			for _, table := range []string{"billing_orders", "credit_ledger_entries", "api_call_logs"} {
				var count int64
				if err := db.Table(table).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("rejected reroute left %d %s", count, table)
				}
			}
			account, readErr := s.repo.CreditAccount("user")
			if readErr != nil || account.AvailableMicrocredits != 10000 || account.ReservedMicrocredits != 0 {
				t.Fatalf("rejected reroute debited: %+v %v", account, readErr)
			}
		})
	}
}

func TestProfileContractMatrixExecutionRechecks(t *testing.T) {
	for _, scenario := range []string{"no_images", "unknown_bytes", "actual_size_changed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			s, db, projectID, asset := profileCreationFixture(t)
			if err := db.Model(&model.ModelChannel{}).Where("id = ?", "channel").Update("base_url", server.URL).Error; err != nil {
				t.Fatal(err)
			}
			view, err := s.CreateProjectProfileAnalysis("user", projectID, asset.ID, CreateProfileAnalysisRequest{asset.PrimaryVersionID, "profile-image", ProfileAnalysisModelSelection{Kind: "system", ChannelID: "channel", ModelKey: "text-test"}})
			if err != nil {
				t.Fatal(err)
			}
			task, err := s.repo.Task(view.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			capability := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
			capability.Text.References.MaxImages, capability.Text.References.MaxImageBytes = 1, 100
			reason := "asset_profile_analysis_model_unsupported"
			switch scenario {
			case "no_images":
				capability.Text.References.MaxImages = 0
			case "unknown_bytes":
				capability.Text.References.MaxImageBytes = 0
				reason = "asset_profile_analysis_model_unknown"
			case "actual_size_changed":
				if err := db.Model(&model.Resource{}).Where("id = ?", "profile-image").Update("size", 101).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Model(&model.ChannelModel{}).Where("id = ?", "cm").Update("capability_config_json", mustEncodeModelCapabilityConfig(t, capability)).Error; err != nil {
				t.Fatal(err)
			}
			_, _, err = s.processTask(context.Background(), *task)
			profileMatrixReason(t, err, 422, reason)
			if calls.Load() != 0 {
				t.Fatal("changed model/resource reached provider")
			}
		})
	}
}

func TestProfileContractMatrixAllUnknownNotAdoptable(t *testing.T) {
	s, db, projectID, asset, task := profileAdoptFixture(t)
	var result map[string]any
	if err := json.Unmarshal([]byte(task.ResultJSON), &result); err != nil {
		t.Fatal(err)
	}
	result["assetProfileAnalysis"].(map[string]any)["draft"] = map[string]string{"description": "", "prompt": ""}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("result_json", profileMatrixJSON(t, result)).Error; err != nil {
		t.Fatal(err)
	}
	before := profileMatrixSnapshot(t, db)
	view, err := s.ProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID)
	if err != nil || view.CanAdopt || view.Draft["description"] != "" || len(view.Draft) != 2 {
		t.Fatalf("unknown view: %+v %v", view, err)
	}
	_, err = s.AdoptProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "matrix-unknown-key-01"})
	profileMatrixReason(t, err, 409, "asset_profile_analysis_not_adoptable")
	if !reflect.DeepEqual(before, profileMatrixSnapshot(t, db)) {
		t.Fatal("unknown adoption mutated database")
	}
}

func TestProfileContractMatrixCompletionWins(t *testing.T) {
	s, db, projectID, asset, task := profileAdoptFixture(t)
	result := task.ResultJSON
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"status": model.TaskStatusRunning, "result_json": ""}).Error; err != nil {
		t.Fatal(err)
	}
	task.Status, task.ResultJSON = model.TaskStatusRunning, ""
	if err := s.saveTaskCompletionWithinStorageQuota(&task, []byte(result), nil, false); err != nil {
		t.Fatal(err)
	}
	_, err := s.CancelTask(context.Background(), "user-1", task.ID)
	profileMatrixReason(t, err, 409, "task_state_changed")
	view, err := s.ProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID)
	if err != nil || view.Status != model.TaskStatusSucceeded || !view.CanAdopt || view.CanCancel {
		t.Fatalf("completed winner view: %+v %v", view, err)
	}
	stored, err := s.repo.Task(task.ID)
	if err != nil || stored.Status != model.TaskStatusSucceeded || stored.ResultJSON != task.ResultJSON {
		t.Fatalf("cancel rewrote completed winner: %+v %v", stored, err)
	}
}

func TestProfileContractMatrixCancelCompletionRace(t *testing.T) {
	for iteration := 0; iteration < 4; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			databases := profileConcurrencyDatabases(t, "sqlite")
			db := databases[0]
			if err := db.AutoMigrate(database.Models()...); err != nil {
				t.Fatal(err)
			}
			services := [2]*Service{{repo: repository.New(db)}, {repo: repository.New(databases[1])}}
			project := model.Project{ID: newID(), UserID: "race-user", Name: "profile race", Status: model.ProjectStatusActive}
			resource := model.Resource{ID: newID(), UserID: project.UserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 32, Height: 32, Size: 100}
			for _, row := range []any{&project, &resource} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			asset, err := services[0].CreateProjectAssetEntity(project.UserID, project.ID, CreateProjectAssetEntityRequest{Name: "race asset", Category: model.AssetCategoryProp, ReferenceImage: &ProjectReferenceImage{resource.ID}})
			if err != nil {
				t.Fatal(err)
			}
			binding := ProfileAnalysisBinding{1, project.ID, asset.ID, asset.Category, asset.PrimaryVersionID, ProjectReferenceImage{resource.ID}, 1}
			task := model.Task{ID: newID(), UserID: project.UserID, ProjectID: project.ID, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusRunning, InputJSON: profileMatrixJSON(t, map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}})}
			result := profileMatrixJSON(t, map[string]any{"mode": "text", "assetProfileAnalysis": ProfileAnalysisResult{SchemaVersion: 1, AssetID: asset.ID, Category: asset.Category, BaseVersionID: asset.PrimaryVersionID, Source: binding.Source, Draft: map[string]string{"description": "visible", "prompt": ""}}})
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			taskID := task.ID
			start := make(chan struct{})
			var ready, done sync.WaitGroup
			ready.Add(2)
			done.Add(2)
			var completeErr, cancelErr error
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				completeErr = services[0].saveTaskCompletionWithinStorageQuota(&task, []byte(result), nil, false)
			}()
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				_, cancelErr = services[1].CancelTask(context.Background(), project.UserID, taskID)
			}()
			ready.Wait()
			close(start)
			done.Wait()
			stored, err := services[0].repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			view, err := services[0].ProjectProfileAnalysis(project.UserID, project.ID, asset.ID, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch stored.Status {
			case model.TaskStatusSucceeded:
				if completeErr != nil {
					t.Fatalf("completed winner failed: %v", completeErr)
				}
				profileMatrixReason(t, cancelErr, 409, "task_state_changed")
				if !view.CanAdopt || view.CanCancel {
					t.Fatalf("wrong completed race view: %+v", view)
				}
			case model.TaskStatusCancelled:
				if cancelErr != nil || !errors.Is(completeErr, repository.ErrTaskStateConflict) {
					t.Fatalf("cancel winner: cancel=%v completion=%v", cancelErr, completeErr)
				}
				if view.CanAdopt || view.CanCancel || view.Draft != nil || stored.ResultJSON != "" {
					t.Fatalf("late race result exposed: %+v", view)
				}
				if _, err := services[1].CancelTask(context.Background(), project.UserID, task.ID); err != nil {
					t.Fatalf("repeat cancellation: %v", err)
				}
			default:
				t.Fatalf("race did not settle: %+v", stored)
			}
			versions, err := services[0].repo.AssetVersions(asset.ID)
			if err != nil || len(versions) != 1 {
				t.Fatalf("race wrote asset versions: %d %v", len(versions), err)
			}
		})
	}
}

func TestProfileContractMatrixRecoveryPagination(t *testing.T) {
	s, db, projectID, asset, original := profileAdoptFixture(t)
	if err := db.Delete(&model.Task{}, "id = ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Truncate(time.Second)
	statuses := []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusRunning, model.TaskStatusSucceeded, model.TaskStatusFailed, model.TaskStatusCancelled}
	for i, status := range statuses {
		task := original
		task.ID, task.Status, task.CreatedAt, task.UpdatedAt = fmt.Sprintf("matrix-task-%d", i), status, stamp, stamp
		if status != model.TaskStatusSucceeded {
			task.ResultJSON = ""
		}
		task.Error, task.TextDraft = "RAW_SECRET", "RAW_SECRET"
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []string{"user", "project", "asset", "operation", "type"} {
		task := original
		task.ID, task.CreatedAt = "matrix-foreign-"+scope, stamp.Add(time.Minute)
		switch scope {
		case "user":
			task.UserID = "other"
		case "project":
			task.ProjectID = "other"
		case "asset":
			task.InputJSON = strings.ReplaceAll(task.InputJSON, asset.ID, "another-asset")
		case "operation":
			task.Operation = "another"
		case "type":
			task.Type = "canvas_image"
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.ProjectProfileAnalyses("user-1", projectID, asset.ID, task.ID, 2); err == nil {
			t.Fatalf("accepted %s foreign cursor", scope)
		}
	}
	var got []string
	cursor := ""
	for page := 0; page < 3; page++ {
		list, err := s.ProjectProfileAnalyses("user-1", projectID, asset.ID, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, view := range list.Analyses {
			got = append(got, view.TaskID)
			if view.Draft != nil || view.Baseline != nil {
				t.Fatal("list exposed full draft/baseline")
			}
			detail, err := s.ProjectProfileAnalysis("user-1", projectID, asset.ID, view.TaskID)
			if err != nil || detail.Status != view.Status {
				t.Fatalf("cannot recover %s: %+v %v", view.TaskID, detail, err)
			}
			if strings.Contains(profileMatrixJSON(t, detail), "RAW_SECRET") {
				t.Fatal("recovery exposed provider secret")
			}
			if view.Status == model.TaskStatusFailed && detail.Error == nil {
				t.Fatal("failed recovery lacks safe error")
			}
		}
		cursor = list.NextCursor
		if (cursor != "") != (page < 2) {
			t.Fatalf("page %d cursor %q", page, cursor)
		}
	}
	want := []string{"matrix-task-4", "matrix-task-3", "matrix-task-2", "matrix-task-1", "matrix-task-0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pagination: %v want %v", got, want)
	}
	if _, err := s.ProjectProfileAnalyses("other", projectID, asset.ID, "", 2); err == nil {
		t.Fatal("cross-user recovery admitted")
	}
}

// Whole-row snapshots include unknown JSON, media provenance and voice bindings.
func profileMatrixRichFixture(t *testing.T, kind string) (*Service, *gorm.DB, string, ProjectAssetSummary, model.Task) {
	t.Helper()
	s, db, projectID, asset, task := profileAdoptFixture(t)
	var owner model.Asset
	if err := db.First(&owner, "id = ?", asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(owner.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	payload["tags"], payload["cover"], payload["custom"] = []any{"keep", 2}, map[string]any{"resourceId": "image-1"}, map[string]any{"null": nil, "number": 9007199254740993}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	data["other"], data["resourceId"], data["media"] = []any{true, nil, map[string]any{"n": 7}}, "image-1", map[string]any{"crop": []any{1, 2, 3, 4}}
	payload["data"] = data
	if err := db.Model(&owner).Updates(map[string]any{"kind": kind, "title": "unchanged title", "payload_json": profileMatrixJSON(t, payload)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AssetVersion{}).Where("id = ?", asset.PrimaryVersionID).Updates(map[string]any{"prompt": "immutable media generation prompt", "note": "keep version note"}).Error; err != nil {
		t.Fatal(err)
	}
	voice := model.CharacterVoiceBinding{ID: newID(), AssetVersionID: asset.PrimaryVersionID, VoiceProfileID: "matrix-voice", Instructions: "keep instructions", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	for _, row := range []any{&model.VoiceProfile{ID: voice.VoiceProfileID, UserID: "user-1", Name: "voice", Status: "active"}, &voice, &model.AssetRepresentation{ID: newID(), AssetVersionID: asset.PrimaryVersionID, TaskID: "original-primary-task", ResourceID: "image-1", MediaType: "image", Role: "primary", MetadataJSON: `{"crop":[1,2],"keep":null}`}, &model.AssetRepresentation{ID: newID(), AssetVersionID: asset.PrimaryVersionID, TaskID: "original-audio-task", ResourceID: "image-1", MediaType: "audio", Role: "preview", MetadataJSON: `{"gain":0.3}`}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return s, db, projectID, asset, task
}

func profileMatrixSnapshot(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range []string{"assets", "asset_versions", "asset_representations", "character_voice_bindings", "voice_profiles", "tasks", "projects", "project_asset_links"} {
		var rows []map[string]any
		if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		out[table] = profileMatrixJSON(t, rows)
	}
	return out
}

func TestProfileContractMatrixAdoptionPreservesCompleteState(t *testing.T) {
	for _, kind := range []string{"entity", "image"} {
		t.Run(kind, func(t *testing.T) {
			s, _, projectID, asset, task := profileMatrixRichFixture(t, kind)
			before, _ := s.repo.AssetForUser("user-1", asset.ID)
			base, _ := s.repo.AssetVersion(asset.PrimaryVersionID)
			reps, _ := s.repo.AssetRepresentations(base.ID)
			voice, _ := s.repo.CharacterVoiceBinding(base.ID)
			result, err := s.AdoptProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID, AdoptProfileAnalysisRequest{base.ID, []string{"description"}, "matrix-adopt-key-0001"})
			if err != nil {
				t.Fatal(err)
			}
			after, _ := s.repo.AssetForUser("user-1", asset.ID)
			if after.ID != before.ID || after.UserID != before.UserID || after.Kind != before.Kind || after.Category != before.Category || after.Status != before.Status || after.Title != before.Title || after.FolderID != before.FolderID || !after.CreatedAt.Equal(before.CreatedAt) {
				t.Fatalf("asset identity changed: %+v", after)
			}
			next, _ := s.repo.AssetVersion(result.AdoptedVersionID)
			if next.Prompt != base.Prompt || next.Note != base.Note || next.Status != base.Status || next.AssetID != base.AssetID || next.Version != base.Version+1 {
				t.Fatalf("version fields changed: %+v", next)
			}
			var oldDefinition, newDefinition map[string]json.RawMessage
			_ = json.Unmarshal([]byte(base.DefinitionJSON), &oldDefinition)
			_ = json.Unmarshal([]byte(next.DefinitionJSON), &newDefinition)
			for key, value := range oldDefinition {
				if key == "description" {
					continue
				}
				if string(newDefinition[key]) != string(value) {
					t.Errorf("unselected JSON changed: %s", key)
				}
			}
			if string(newDefinition["description"]) != `"new"` || len(newDefinition) != len(oldDefinition)+1 {
				t.Fatalf("unexpected definition writes: %s", next.DefinitionJSON)
			}
			var oldPayload, newPayload map[string]json.RawMessage
			_ = json.Unmarshal([]byte(before.PayloadJSON), &oldPayload)
			_ = json.Unmarshal([]byte(after.PayloadJSON), &newPayload)
			delete(oldPayload, "primaryVersionId")
			delete(newPayload, "primaryVersionId")
			delete(oldPayload, "updatedAt")
			delete(newPayload, "updatedAt")
			if kind == "entity" {
				var oldData, newData map[string]json.RawMessage
				_ = json.Unmarshal(oldPayload["data"], &oldData)
				_ = json.Unmarshal(newPayload["data"], &newData)
				if string(newData["definition"]) != next.DefinitionJSON {
					t.Fatal("entity definition not synchronized")
				}
				delete(oldData, "definition")
				delete(newData, "definition")
				if !reflect.DeepEqual(oldData, newData) {
					t.Fatal("entity other data changed")
				}
				delete(oldPayload, "data")
				delete(newPayload, "data")
			}
			if !reflect.DeepEqual(oldPayload, newPayload) {
				t.Fatalf("payload changed beyond permitted keys: %s", after.PayloadJSON)
			}
			copied, _ := s.repo.AssetRepresentations(next.ID)
			if len(copied) != len(reps) {
				t.Fatal("lost representations")
			}
			for i, rep := range reps {
				copy := copied[i]
				if copy.ID == rep.ID {
					t.Fatal("representation not copied")
				}
				copy.ID, copy.AssetVersionID, copy.CreatedAt = rep.ID, rep.AssetVersionID, rep.CreatedAt
				if !reflect.DeepEqual(copy, rep) {
					t.Fatalf("media provenance changed: %+v vs %+v", copy, rep)
				}
			}
			newVoice, err := s.repo.CharacterVoiceBinding(next.ID)
			if err != nil || newVoice.ID == voice.ID {
				t.Fatalf("voice not copied: %+v %v", newVoice, err)
			}
			newVoice.ID, newVoice.AssetVersionID = voice.ID, voice.AssetVersionID
			if !reflect.DeepEqual(newVoice, voice) {
				t.Fatalf("voice changed: %+v %+v", newVoice, voice)
			}
			persistedBase, _ := s.repo.AssetVersion(base.ID)
			if !reflect.DeepEqual(persistedBase, base) {
				t.Fatal("historical base changed")
			}
		})
	}
}

func TestProfileContractMatrixAdoptionRollbackEveryWrite(t *testing.T) {
	for _, point := range []string{"asset_cas", "version", "representations", "voice", "project_revision", "task_receipt"} {
		t.Run(point, func(t *testing.T) {
			s, db, projectID, asset, task := profileMatrixRichFixture(t, "entity")
			before := profileMatrixSnapshot(t, db)
			injected := errors.New("matrix write failure")
			fired := false
			fail := func(tx *gorm.DB) {
				if fired {
					return
				}
				table := tx.Statement.Table
				match := (point == "version" && table == "asset_versions") || (point == "representations" && table == "asset_representations") || (point == "voice" && table == "character_voice_bindings") || (point == "project_revision" && table == "projects") || (point == "task_receipt" && table == "tasks")
				if point == "asset_cas" && table == "assets" {
					if values, ok := tx.Statement.Dest.(map[string]any); ok {
						_, match = values["payload_json"]
					}
				}
				if match {
					fired = true
					tx.AddError(injected)
				}
			}
			if err := db.Callback().Create().Before("gorm:create").Register("profile_matrix_failure", fail); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Before("gorm:update").Register("profile_matrix_failure", fail); err != nil {
				t.Fatal(err)
			}
			_, err := s.AdoptProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "matrix-adopt-key-0001"})
			if !fired || !errors.Is(err, injected) {
				t.Fatalf("write point not exercised: fired=%v err=%v", fired, err)
			}
			if after := profileMatrixSnapshot(t, db); !reflect.DeepEqual(before, after) {
				for table, want := range before {
					if after[table] != want {
						t.Errorf("failed %s left partial %s", point, table)
					}
				}
			}
		})
	}
}

func TestProfileContractMatrixIsolatedSourceProtection(t *testing.T) {
	for _, source := range []string{"task_input", "task_draft", "historical_receipt"} {
		t.Run(source, func(t *testing.T) {
			s, db, _, _ := profileCreationFixture(t)
			// Remove all reference-image and payload links so they cannot mask a miss.
			if err := db.Model(&model.Asset{}).Where("user_id = ?", "user").Update("payload_json", `{}`).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.AssetVersion{}).Where("1=1").Update("definition_json", `{}`).Error; err != nil {
				t.Fatal(err)
			}
			raw := `{"source":{"resourceId":"profile-image"}}`
			var documentID string
			if source == "historical_receipt" {
				var version model.AssetVersion
				if err := db.First(&version).Error; err != nil {
					t.Fatal(err)
				}
				documentID = version.ID
				if err := db.Model(&version).Update("definition_json", `{"_profileAnalysisAdoption":`+raw+`}`).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				task := model.Task{ID: newID(), UserID: "user", Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusRunning, InputJSON: `{}`, ResultJSON: `{}`}
				if source == "task_input" {
					task.InputJSON = `{"metadata":{"operation":"asset_image_profile_analysis","profileAnalysis":` + raw + `}}`
				} else {
					task.Status = model.TaskStatusSucceeded
					task.ResultJSON = `{"mode":"text","assetProfileAnalysis":` + raw + `}`
				}
				documentID = task.ID
				if err := db.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
			}
			resource, err := s.repo.Resource("profile-image")
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := s.repo.ResourceReferenceSnapshot("user", "", []string{resource.ID})
			if err != nil {
				t.Fatal(err)
			}
			refs := adminResourceReferences(snapshot, []model.Resource{*resource})[resource.ID]
			if len(refs) != 1 || refs[0].ID != documentID {
				t.Fatalf("source not isolated: %+v", refs)
			}
			if err := s.repo.DeleteDetachedResources([]model.Resource{*resource}, nil); !errors.Is(err, repository.ErrOriginalImageReferenced) {
				t.Fatalf("isolated %s source not protected: %v", source, err)
			}
			if source == "historical_receipt" {
				if err := db.Model(&model.AssetVersion{}).Where("id = ?", documentID).Update("definition_json", `{}`).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Delete(&model.Task{}, "id = ?", documentID).Error; err != nil {
					t.Fatal(err)
				}
			}
			// No worker runs and no physical object is touched.
			if err := s.repo.DeleteDetachedResources([]model.Resource{*resource}, nil); err != nil {
				t.Fatalf("reference removal did not permit cleanup: %v", err)
			}
		})
	}
}
