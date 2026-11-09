package app

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"os"
	"reflect"
	"strings"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func generatedFixture(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	service, source := previewFixture(t)
	if os.Getenv("TASK4_TEST_DIALECT") != "postgres" {
		return service, source
	}
	db := profileConcurrencyDatabases(t, "postgres")[0]
	models := []any{&model.Project{}, &model.ProjectUnit{}, &model.Shot{}, &model.ShotRevision{}, &model.ShotAssetReference{}, &model.ShotArtifact{}, &model.ProjectAssetLink{}, &model.Asset{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.Resource{}, &model.Task{}}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	for _, entry := range models {
		rows := reflect.New(reflect.SliceOf(reflect.TypeOf(entry).Elem())).Interface()
		if err := source.Find(rows).Error; err != nil {
			t.Fatal(err)
		}
		if reflect.ValueOf(rows).Elem().Len() > 0 {
			if err := db.Create(rows).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	return &Service{repo: repository.New(db)}, db
}

func TestGeneratedStoryboardCanBeSavedAfterTaskCompletion(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.CanvasProject{}, &model.TaskLog{}, &model.Result{}, &model.ApiCallLog{}, &model.TaskTextDelta{}, &model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID: "completed-storyboard", UserID: "u", ProjectID: "p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusRunning,
		InputJSON: `{"mode":"text","metadata":{"source":"short-drama-chapter-storyboard","chapterId":"unit"},"config":{"apiKey":"private-test-key"},"chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","summary":"large catalog","allowedAssetIds":["a"],"totalCount":3,"sentCount":1,"scope":{"kind":"selected","assetIds":["a"]}}}`,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	result := []byte(`{"text":"{\"rows\":[{\"plotDescription\":\"A character enters\",\"durationSeconds\":6,\"projectAssetRefs\":[{\"assetId\":\"a\",\"category\":\"character\"}]}]}"}`)
	if err := s.saveTaskCompletionWithinStorageQuota(&task, result, nil, false); err != nil {
		t.Fatal(err)
	}
	stored, err := s.repo.TaskForUser("u", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusSucceeded || strings.Contains(stored.InputJSON, "private-test-key") || strings.Contains(stored.InputJSON, `"config"`) {
		t.Fatal("task must finish without retaining provider credentials")
	}
	// Public reads and storage maintenance both reapply the same compaction.
	if err := db.Model(stored).Update("input_json", publicTaskInputJSON(stored.InputJSON)).Error; err != nil {
		t.Fatal(err)
	}
	resolution, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: task.ID})
	if err != nil {
		t.Fatalf("completed storyboard must remain saveable: %v", err)
	}
	if len(resolution.Rows) != 1 || !reflect.DeepEqual(resolution.Rows[0].AssetVersionIDs, []string{"v"}) {
		t.Fatalf("original allowed asset scope was lost: %+v", resolution.Rows)
	}
	shots, err := s.ReplaceProjectUnitShots("u", "p", "unit", ReplaceProjectUnitShotsRequest{
		ExpectedShotIDs:  []string{"s"},
		GeneratedBinding: &GeneratedBindingInput{TaskID: task.ID, Baseline: resolution.Baseline, RowIndices: []int{0}},
		Shots:            []ReplaceProjectUnitShotInput{{CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头01", Description: "A character enters", DurationMs: 6000, Revision: ShotRevisionInput{PlotDescription: "A character enters", DurationMs: 6000}}, AssetVersionIDs: []string{"v"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 || shots[0].UnitID != "unit" || shots[0].Description != "A character enters" {
		t.Fatalf("completed result was not persisted as a chapter shot: %+v", shots)
	}
}

func TestStoryboardAssetGeneratedWorkspacePreservesOriginalReference(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.CanvasProject{}, &model.CanvasUnitLink{}, &model.ProjectAssetCandidate{}, &model.CharacterVoiceBinding{}, &model.VoiceProfile{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ShotAssetReference{}).Where("id = ?", "r-a").Update("asset_version_id", "v").Error; err != nil {
		t.Fatal(err)
	}
	workspace, err := s.ProjectUnitWorkspace("u", "p", "unit")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(workspace.ShotReferences)
	var refs []map[string]any
	if err := json.Unmarshal(raw, &refs); err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref["id"] == "r-a" {
			bound := ref["referencedVersion"].(map[string]any)
			image, _ := bound["referenceImage"].(map[string]any)
			if image["resourceId"] != "original" {
				t.Fatalf("persisted original reference omitted: %s", raw)
			}
			return
		}
	}
	t.Fatal("missing reference")
}

func TestStoryboardAssetGeneratedThreeCategoriesAndTampering(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Asset{}).Where("id = ?", "empty").Update("primary_version_id", "ve").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AssetVersion{ID: "ve", AssetID: "empty", Version: 1, Status: "draft", DefinitionJSON: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "generated", UserID: "u", ProjectID: "p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusSucceeded, InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a","b","empty"]}}`, ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"},{"assetId":"empty","category":"environment"},{"assetId":"b","category":"prop"}]}]}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	resolution, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Rows) != 1 || len(resolution.Rows[0].AssetVersionIDs) != 3 {
		t.Fatalf("three categories not resolved: %+v", resolution)
	}
	req := ReplaceProjectUnitShotsRequest{ExpectedShotIDs: []string{"s"}, GeneratedBinding: &GeneratedBindingInput{TaskID: "generated", Baseline: resolution.Baseline, RowIndices: []int{0}}, Shots: []ReplaceProjectUnitShotInput{{CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头01", Description: "镜头 1", DurationMs: 6000, Revision: ShotRevisionInput{PlotDescription: "镜头 1", DurationMs: 6000}}, AssetVersionIDs: []string{"v", "ve", "vb"}}}}
	for _, mode := range []string{"tampered_version", "out_of_range", "duplicate_row", "missing_catalog", "version_changed", "invalid_id_with_valid_name"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(req)
			var bad ReplaceProjectUnitShotsRequest
			_ = json.Unmarshal(raw, &bad)
			switch mode {
			case "tampered_version":
				bad.Shots[0].AssetVersionIDs = []string{"v-old"}
			case "out_of_range":
				bad.GeneratedBinding.RowIndices = []int{1}
			case "duplicate_row":
				bad.Shots = append(bad.Shots, bad.Shots[0])
				bad.GeneratedBinding.RowIndices = []int{0, 0}
			case "missing_catalog":
				bad.GeneratedBinding.Baseline.Catalog = nil
			case "version_changed":
				if err := db.Model(&model.Asset{}).Where("id = ?", "a").UpdateColumn("primary_version_id", "v-old").Error; err != nil {
					t.Fatal(err)
				}
				defer db.Model(&model.Asset{}).Where("id = ?", "a").UpdateColumn("primary_version_id", "v")
			case "invalid_id_with_valid_name":
				if err := db.Model(&task).Update("result_json", `{"rows":[{"projectAssetRefs":[{"assetId":"invented","category":"character","name":"阿青"}]}]}`).Error; err != nil {
					t.Fatal(err)
				}
				defer db.Model(&task).Update("result_json", `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"},{"assetId":"empty","category":"environment"},{"assetId":"b","category":"prop"}]}]}`)
				fresh, e := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "generated"})
				if e != nil {
					t.Fatal(e)
				}
				bad.GeneratedBinding.Baseline = fresh.Baseline
				bad.Shots[0].AssetVersionIDs = []string{}
			}
			before := previewState(t, db)
			if _, err := s.ReplaceProjectUnitShots("u", "p", "unit", bad); err == nil {
				t.Fatal("accepted tampered generated save", mode)
			}
			if previewState(t, db) != before {
				t.Fatal("rejection mutated database")
			}
		})
	}
	shots, err := s.ReplaceProjectUnitShots("u", "p", "unit", req)
	if err != nil {
		t.Fatal(err)
	}
	var refs []model.ShotAssetReference
	if err := db.Where("shot_id = ?", shots[0].ID).Order("asset_version_id").Find(&refs).Error; err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0].AssetVersionID != "v" || refs[1].AssetVersionID != "vb" || refs[2].AssetVersionID != "ve" {
		t.Fatalf("wrong persisted versions: %+v", refs)
	}
}

func TestStoryboardAssetGeneratedManualReplaceRemainsIndependent(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	req := ReplaceProjectUnitShotsRequest{ExpectedShotIDs: []string{"s"}, Shots: []ReplaceProjectUnitShotInput{{CreateProjectShotRequest: CreateProjectShotRequest{Title: "manual", Description: "manual script", DurationMs: 1000}}}}
	shots, err := s.ReplaceProjectUnitShots("u", "p", "unit", req)
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 || shots[0].Description != "manual script" {
		t.Fatalf("manual replace changed contract: %+v", shots)
	}
}

func TestStoryboardAssetGeneratedRechecksAfterScopeLock(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "generated", UserID: "u", ProjectID: "p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusSucceeded, InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a"]}}`, ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"}]}]}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	result, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	before := previewState(t, db)
	changed := false
	if err := db.Callback().Query().After("gorm:query").Register("generated-change-at-lock", func(tx *gorm.DB) {
		if _, locked := tx.Statement.Clauses["FOR"]; !locked || tx.Statement.Table != "shots" || changed {
			return
		}
		changed = true
		if err := tx.Session(&gorm.Session{NewDB: true}).Model(&model.Task{}).Where("id = ?", "generated").Update("result_json", `{"rows":[]}`).Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	req := ReplaceProjectUnitShotsRequest{ExpectedShotIDs: []string{"s"}, GeneratedBinding: &GeneratedBindingInput{TaskID: "generated", Baseline: result.Baseline, RowIndices: []int{0}}, Shots: []ReplaceProjectUnitShotInput{{CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头01", Description: "镜头 1", DurationMs: 6000, Revision: ShotRevisionInput{PlotDescription: "镜头 1", DurationMs: 6000}}}}}
	if _, err := s.ReplaceProjectUnitShots("u", "p", "unit", req); err == nil {
		t.Fatal("generated result changed at scope lock but old shots were replaced")
	}
	if !changed {
		t.Fatal("scope lock barrier was not exercised")
	}
	if previewState(t, db) != before {
		t.Fatal("failed locked recheck did not roll back all writes")
	}
}

func TestStoryboardAssetGeneratedRejectsUntrustedTaskBeforeReplacement(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	before := previewState(t, db)
	var req ReplaceProjectUnitShotsRequest
	if err := json.Unmarshal([]byte(`{"shots":[{"title":"new","description":"new shot","durationMs":1000}],"expectedShotIds":["s"],"generatedBinding":{"taskId":"not-a-real-task","baseline":{},"rowIndices":[0]}}`), &req); err != nil {
		t.Fatal(err)
	}
	_, err := s.ReplaceProjectUnitShots("u", "p", "unit", req)
	if err == nil {
		t.Fatal("generated replacement accepted a nonexistent task and deleted old shots")
	}
	if after := previewState(t, db); after != before {
		t.Fatal("rejected generated replacement mutated persisted state")
	}
}

func TestStoryboardAssetGeneratedRejectsInvalidBindingBeforeReplacement(t *testing.T) {
	for _, mode := range []string{"wrong_owner", "wrong_project", "wrong_operation", "wrong_unit", "unfinished", "missing_baseline", "fake_allowed_ids"} {
		t.Run(mode, func(t *testing.T) {
			s, db := generatedFixture(t)
			if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
				t.Fatal(err)
			}
			task := model.Task{ID: "generated", UserID: "u", ProjectID: "p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusSucceeded, InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a"],"scope":{"kind":"all","assetIds":[]}}}`, ResultJSON: `{"text":"{\"rows\":[{\"plotDescription\":\"new shot\",\"projectAssetRefs\":[{\"assetId\":\"a\",\"category\":\"character\"}]}]}"}`}
			switch mode {
			case "wrong_owner":
				task.UserID = "other"
			case "wrong_project":
				task.ProjectID = "other"
			case "wrong_operation":
				task.Operation = "other"
			case "wrong_unit":
				task.InputJSON = `{"chapterAssetContext":{"projectId":"p","unitId":"other-unit","operation":"storyboard"}}`
			case "unfinished":
				task.Status = model.TaskStatusRunning
			}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			before := previewState(t, db)
			var req ReplaceProjectUnitShotsRequest
			if err := json.Unmarshal([]byte(`{"shots":[{"title":"new","description":"new shot","durationMs":1000}],"expectedShotIds":["s"],"generatedBinding":{"taskId":"generated","baseline":{},"allowedIds":["a"],"rowIndices":[0]}}`), &req); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReplaceProjectUnitShots("u", "p", "unit", req); err == nil {
				t.Fatal("accepted invalid generated binding", mode)
			}
			if after := previewState(t, db); before != after {
				t.Fatal("invalid generated save changed old data")
			}
		})
	}
}

func TestStoryboardAssetGeneratedNotFoundMapping(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{ID: "foreign-p", UserID: "foreign", Name: "secret-project"}).Error; err != nil {
		t.Fatal(err)
	}
	validTask := model.Task{
		ID:         "generated-ok",
		UserID:     "u",
		ProjectID:  "p",
		Type:       "canvas_text",
		Operation:  "storyboard",
		Status:     model.TaskStatusSucceeded,
		InputJSON:  `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a"]}}`,
		ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"}]}]}`,
	}
	foreignTask := model.Task{
		ID:         "generated-foreign",
		UserID:     "foreign",
		ProjectID:  "foreign-p",
		Type:       "canvas_text",
		Operation:  "storyboard",
		Status:     model.TaskStatusSucceeded,
		InputJSON:  `{"mode":"text","chapterAssetContext":{"projectId":"foreign-p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a"]}}`,
		ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"}]}]}`,
	}
	if err := db.Create(&[]model.Task{validTask, foreignTask}).Error; err != nil {
		t.Fatal(err)
	}

	resolveCases := []struct {
		name      string
		userID    string
		projectID string
		unitID    string
		taskID    string
	}{
		{name: "missing_project", userID: "u", projectID: "missing-p", unitID: "unit", taskID: "generated-ok"},
		{name: "foreign_project", userID: "u", projectID: "foreign-p", unitID: "unit", taskID: "generated-ok"},
		{name: "missing_unit", userID: "u", projectID: "p", unitID: "missing-unit", taskID: "generated-ok"},
		{name: "missing_task", userID: "u", projectID: "p", unitID: "unit", taskID: "missing-task"},
		{name: "foreign_task", userID: "u", projectID: "p", unitID: "unit", taskID: "generated-foreign"},
	}
	for _, tc := range resolveCases {
		t.Run("resolve_"+tc.name, func(t *testing.T) {
			_, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), tc.userID, tc.projectID, tc.unitID, GeneratedBindingRequest{TaskID: tc.taskID})
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != 404 {
				t.Fatalf("expected *AppError with status 404 for %s, got err=%T (%v)", tc.name, err, err)
			}
		})
	}

	resolved, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "generated-ok"})
	if err != nil {
		t.Fatal(err)
	}
	before := previewState(t, db)
	replaceCases := []struct {
		name      string
		userID    string
		projectID string
		unitID    string
		taskID    string
	}{
		{name: "missing_project", userID: "u", projectID: "missing-p", unitID: "unit", taskID: "generated-ok"},
		{name: "foreign_project", userID: "u", projectID: "foreign-p", unitID: "unit", taskID: "generated-ok"},
		{name: "missing_unit", userID: "u", projectID: "p", unitID: "missing-unit", taskID: "generated-ok"},
		{name: "missing_task", userID: "u", projectID: "p", unitID: "unit", taskID: "missing-task"},
		{name: "foreign_task", userID: "u", projectID: "p", unitID: "unit", taskID: "generated-foreign"},
	}
	for _, tc := range replaceCases {
		t.Run("replace_"+tc.name, func(t *testing.T) {
			req := ReplaceProjectUnitShotsRequest{
				ExpectedShotIDs:  []string{"s"},
				GeneratedBinding: &GeneratedBindingInput{TaskID: tc.taskID, Baseline: resolved.Baseline, RowIndices: []int{0}},
				Shots: []ReplaceProjectUnitShotInput{{
					CreateProjectShotRequest: CreateProjectShotRequest{Title: "镜头01", Description: "镜头 1", DurationMs: 6000},
					AssetVersionIDs:          []string{"v1"},
				}},
			}
			_, err := s.ReplaceProjectUnitShots(tc.userID, tc.projectID, tc.unitID, req)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != 404 {
				t.Fatalf("expected *AppError with status 404 for %s, got err=%T (%v)", tc.name, err, err)
			}
			if after := previewState(t, db); after != before {
				t.Fatalf("failed replace (%s) mutated persisted state", tc.name)
			}
		})
	}
}

func TestStoryboardAssetGeneratedWarnsOnMissingRefsAndMissingEnvironment(t *testing.T) {
	s, db := generatedFixture(t)
	if err := db.AutoMigrate(&model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Asset{}).Where("id = ?", "empty").Update("primary_version_id", "ve").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AssetVersion{ID: "ve", AssetID: "empty", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"original"}}`}).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID:        "generated-env-check",
		UserID:    "u",
		ProjectID: "p",
		Type:      "canvas_text",
		Operation: "storyboard",
		Status:    model.TaskStatusSucceeded,
		InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a","empty"]}}`,
		ResultJSON: `{"rows":[` +
			`{"plotDescription":"missing field"},` +
			`{"plotDescription":"only character","projectAssetRefs":[{"assetId":"a","category":"character"}]},` +
			`{"plotDescription":"with environment","projectAssetRefs":[{"assetId":"a","category":"character"},{"assetId":"empty","category":"environment"}]}` +
			`]}`,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	resolved, err := s.ResolveGeneratedProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", GeneratedBindingRequest{TaskID: "generated-env-check"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(resolved.Rows))
	}
	if len(resolved.Rows[0].Issues) != 1 || resolved.Rows[0].Issues[0].Kind != "missing_asset_refs" {
		t.Fatalf("expected missing_asset_refs on row 0, got %+v", resolved.Rows[0].Issues)
	}
	if len(resolved.Rows[1].Issues) != 1 || resolved.Rows[1].Issues[0].Kind != "missing_environment" {
		t.Fatalf("expected missing_environment on row 1, got %+v", resolved.Rows[1].Issues)
	}
	if len(resolved.Rows[2].Issues) != 0 {
		t.Fatalf("expected 0 issues on row 2 with environment bound, got %+v", resolved.Rows[2].Issues)
	}
}
