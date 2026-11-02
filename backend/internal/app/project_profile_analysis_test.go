package app

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func profileAdoptFixture(t *testing.T) (*Service, *gorm.DB, string, ProjectAssetSummary, model.Task) {
	t.Helper()
	s, db, p := referenceTestService(t)
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}); err != nil {
		t.Fatal(err)
	}
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "profile", Category: model.AssetCategoryProp, Definition: map[string]any{"description": "old", "prompt": []any{"legacy", 2}, "extra": map[string]any{"n": 7}}, ReferenceImages: []ProjectReferenceImage{{"image-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	binding := ProfileAnalysisBinding{1, p, asset.ID, model.AssetCategoryProp, asset.PrimaryVersionID, ProjectReferenceImage{"image-1"}, 1}
	input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}})
	result, _ := json.Marshal(map[string]any{"mode": "text", "assetProfileAnalysis": ProfileAnalysisResult{SchemaVersion: 1, AssetID: asset.ID, Category: model.AssetCategoryProp, BaseVersionID: asset.PrimaryVersionID, Source: binding.Source, Draft: map[string]string{"description": "new", "prompt": "suggestion"}}})
	task := model.Task{ID: newID(), UserID: "user-1", ProjectID: p, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusSucceeded, InputJSON: string(input), ResultJSON: string(result), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	return s, db, p, asset, task
}
func TestProfileAnalysisAdoptReplayAndHistoricalRecovery(t *testing.T) {
	s, db, p, asset, task := profileAdoptFixture(t)
	req := AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"}
	result, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.repo.AssetVersion(result.AdoptedVersionID)
	if err != nil {
		t.Fatal(err)
	}
	var def map[string]any
	_ = json.Unmarshal([]byte(version.DefinitionJSON), &def)
	if def["description"] != "new" {
		t.Fatal(def)
	}
	if _, ok := def["prompt"].([]any); !ok {
		t.Fatalf("unselected historical JSON type changed: %v", def)
	}
	replay, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req)
	if err != nil || !replay.Replayed || replay.AdoptedVersionID != result.AdoptedVersionID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	req.SelectedFields = []string{"prompt"}
	if _, err = s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req); err == nil {
		t.Fatal("different fields replayed")
	}
	req.SelectedFields = []string{"description"}
	if err := db.Delete(&model.Task{}, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	replay, err = s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, req)
	if err != nil || !replay.Replayed || replay.AdoptedVersionID != result.AdoptedVersionID {
		t.Fatalf("historical replay: %+v %v", replay, err)
	}
}
func TestProfileAnalysisMediaCopiesPreserveTaskProvenance(t *testing.T) {
	s, db, p, asset, task := profileAdoptFixture(t)
	rep := model.AssetRepresentation{ID: newID(), TaskID: "original-media-task", AssetVersionID: asset.PrimaryVersionID, ResourceID: "image-1", MediaType: "image", Role: "primary", MetadataJSON: `{"kept":true}`}
	if err := db.Create(&rep).Error; err != nil {
		t.Fatal(err)
	}
	request := AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"}
	result, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, request)
	if err != nil {
		t.Fatalf("media-bearing adoption: %v", err)
	}
	var copied model.AssetRepresentation
	if err := db.Where("asset_version_id = ? AND role = ?", result.AdoptedVersionID, rep.Role).First(&copied).Error; err != nil {
		t.Fatal(err)
	}
	if copied.ID == rep.ID || copied.TaskID != rep.TaskID || copied.ResourceID != rep.ResourceID || copied.MediaType != rep.MediaType || copied.MetadataJSON != rep.MetadataJSON {
		t.Fatalf("copied media lost provenance: %+v", copied)
	}
	var original model.AssetRepresentation
	if err := db.First(&original, "id = ?", rep.ID).Error; err != nil || original.AssetVersionID != asset.PrimaryVersionID || original.TaskID != rep.TaskID {
		t.Fatalf("original media changed: %+v %v", original, err)
	}
	replay, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, request)
	if err != nil || !replay.Replayed || replay.AdoptedVersionID != result.AdoptedVersionID {
		t.Fatalf("media adoption replay: %+v %v", replay, err)
	}
	var count int64
	if err := db.Model(&model.AssetRepresentation{}).Where("task_id = ?", rep.TaskID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("replay duplicated media: count=%d error=%v", count, err)
	}
}
func TestProfileAnalysisCancelAndBypasses(t *testing.T) {
	s, db, p, asset, task := profileAdoptFixture(t)
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"status": model.TaskStatusRunning, "stage": "调用生成模型", "result_json": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendTaskTextDelta("user-1", task.ID, "RAW"); err == nil {
		t.Fatal("accepted raw delta")
	}
	if _, err := s.CompleteTextReplayTask("user-1", task.ID, "RAW"); err == nil {
		t.Fatal("accepted replay completion")
	}
	if _, err := s.TaskTextReplay("user-1", task.ID, 0); err == nil {
		t.Fatal("exposed text replay")
	}
	cancelled, err := s.CancelTask(context.Background(), "user-1", task.ID)
	if err != nil || cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("cancel: %v %v", cancelled, err)
	}
	var persisted model.Task
	if err := db.First(&persisted, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	var diagnostic model.TaskExecutionDiagnostic
	if err := json.Unmarshal([]byte(persisted.ExecutionDiagnosticJSON), &diagnostic); err != nil {
		t.Fatal(err)
	}
	if diagnostic.Code != "task_cancelled" || diagnostic.Phase != "cancellation" || diagnostic.SafeMessage != "任务已取消" {
		t.Fatalf("explicit cancellation diagnostic: %+v", diagnostic)
	}
	if _, err := s.CancelTask(context.Background(), "user-1", task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryTask("user-1", task.ID); err == nil {
		t.Fatal("accepted retry")
	}
	task.Status = model.TaskStatusRunning
	if err := s.saveTaskCompletionWithinStorageQuota(&task, []byte(task.ResultJSON), nil, false); !errors.Is(err, repository.ErrTaskStateConflict) {
		t.Fatalf("late result fence: %v", err)
	}
	if _, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"}); err == nil {
		t.Fatal("adopted cancelled task")
	}
}

func TestProfileAnalysisPhysicalDeleteFailureRetainsResourceRecord(t *testing.T) {
	s, db, _ := referenceTestService(t)
	s.dataDir = t.TempDir()
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "delete-proof", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "directory-physical-failure"}
	path := filepath.Join(s.dataDir, "resources", resource.ObjectKey)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, s, &resource)
	job := model.ResourceDeletionJob{ID: "delete-proof-job", ResourceID: resource.ID, UserID: resource.UserID, Provider: resource.Provider, ObjectKey: resource.ObjectKey, Status: model.ResourceDeletionStatusPending}
	if err := s.repo.DeleteDetachedResources([]model.Resource{resource}, []model.ResourceDeletionJob{job}); err != nil {
		t.Fatal(err)
	}
	s.drainResourceDeletionJobs(1)
	job = model.ResourceDeletionJob{}
	if err := db.Where("resource_id = ?", resource.ID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.LastError == "" {
		t.Fatalf("physical failure not exercised: %+v", job)
	}
	var retained model.Resource
	if err := db.First(&retained, "id = ?", resource.ID).Error; err != nil {
		t.Fatalf("physical failure lost resource record: %v", err)
	}
	if retained.Status != "deleting" || retained.ObjectKey != resource.ObjectKey || retained.Provider != resource.Provider {
		t.Fatalf("failed deletion must remain retryable without becoming ready: %+v", retained)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("failed physical object was lost: %v %v", info, err)
	}
}

func TestProfileAnalysisAdmissionAndStrictDTO(t *testing.T) {
	for _, raw := range []string{`{"expectedVersionId":"a","expectedVersionId":"b"}`, `{"prompt":"injection"}`, `{} {}`, `{"modelSelection":{"kind":"system","capability":true}}`} {
		var req CreateProfileAnalysisRequest
		if StrictProfileAnalysisJSON([]byte(raw), &req) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	s, _, p, asset, _ := profileAdoptFixture(t)
	for _, input := range []map[string]any{{"metadata": map[string]any{"operation": profileAnalysisOperation}}, {"metadata": map[string]any{"profileAnalysis": map[string]any{}}}, {"replay": true}} {
		if _, err := s.CreateTask("user-1", CreateTaskRequest{Operation: profileAnalysisOperation, Type: "canvas_text", Prompt: "x", Input: input}); err == nil {
			t.Fatal("generic spoof admitted")
		}
	}
	req := CreateProfileAnalysisRequest{asset.PrimaryVersionID, "image-1", ProfileAnalysisModelSelection{Kind: "system", ChannelID: "missing", ModelKey: "unknown"}}
	if _, err := s.CreateProjectProfileAnalysis("user-2", p, asset.ID, req); err == nil {
		t.Fatal("cross-user source admitted")
	}
	req.ExpectedVersionID = "stale"
	if _, err := s.CreateProjectProfileAnalysis("user-1", p, asset.ID, req); err == nil {
		t.Fatal("stale base admitted")
	}
	req.ExpectedVersionID = asset.PrimaryVersionID
	req.SourceResourceID = "foreign-image"
	if _, err := s.CreateProjectProfileAnalysis("user-1", p, asset.ID, req); err == nil {
		t.Fatal("different source admitted")
	}
}

func TestProfileAnalysisDraftSchema(t *testing.T) {
	for _, raw := range []string{`{"description":"x","prompt":"y","name":"bad"}`, `{"description":"x","description":"y","prompt":"z"}`, `{"description":null,"prompt":"x"}`, `{"description":"x"}`, `{"description":"\u0000","prompt":"x"}`, `{"description":"` + strings.Repeat("界", 2001) + `","prompt":"x"}`, `{"description":"x","prompt":"y"} {}`} {
		if _, err := parseProfileDraft(raw, model.AssetCategoryProp); err == nil {
			t.Errorf("accepted invalid draft %.100s", raw)
		}
	}
	draft, err := parseProfileDraft(`{"description":"  x  ","prompt":""}`, model.AssetCategoryEnvironment)
	if err != nil || draft["description"] != "x" {
		t.Errorf("trim failed: %v %v", draft, err)
	}
}

func TestProfileAnalysisDedicatedCompletionValidates(t *testing.T) {
	s, db, p := referenceTestService(t)
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}); err != nil {
		t.Fatal(err)
	}
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "profile", Category: model.AssetCategoryProp, ReferenceImages: []ProjectReferenceImage{{"image-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	binding := ProfileAnalysisBinding{1, p, asset.ID, model.AssetCategoryProp, asset.PrimaryVersionID, ProjectReferenceImage{"image-1"}, 1}
	input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}})
	task := model.Task{ID: "profile-task", UserID: "user-1", ProjectID: p, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusRunning, InputJSON: string(input)}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.saveTaskCompletionWithinStorageQuota(&task, []byte(`{"text":"RAW_SECRET"}`), nil, false); err == nil {
		t.Fatal("completion admitted unvalidated provider result")
	}
}

func TestProfileAnalysisReservedReceipt(t *testing.T) {
	if _, err := preserveReferenceDefinition(`{}`, `{"_profileAnalysisAdoption":{"taskId":"forged"}}`); err == nil {
		t.Fatal("accepted forged adoption receipt")
	}
	raw, err := preserveReferenceDefinition(`{"_profileAnalysisAdoption":{"taskId":"trusted"}}`, `{}`)
	if err != nil || !strings.Contains(raw, "trusted") {
		t.Fatalf("lost reserved receipt: %s %v", raw, err)
	}
}

func profileCreationFixture(t *testing.T) (*Service, *gorm.DB, string, ProjectAssetSummary) {
	t.Helper()
	s, db, _, _ := creationTestService(t)
	s.activeCancels = make(map[string]context.CancelFunc)
	capability := DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceChatCompletion), "text-test")
	capability.Text.References.MaxImages = 1
	capability.Text.References.MaxImageBytes = 1024 * 1024
	if err := db.Model(&model.ChannelModel{}).Where("id = ?", "cm").Update("capability_config_json", mustEncodeModelCapabilityConfig(t, capability)).Error; err != nil {
		t.Fatal(err)
	}
	p := model.Project{ID: newID(), UserID: "user", Name: "profile", Status: model.ProjectStatusActive}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "profile-image", UserID: "user", Provider: "local", ObjectKey: "profile-image.png", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 32, Height: 32, Size: 100}
	path := filepath.Join(s.dataDir, "resources", r.ObjectKey)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", int(r.Size))), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, s, &r)
	a, err := s.CreateProjectAssetEntity("user", p.ID, CreateProjectAssetEntityRequest{Name: "profile", Category: model.AssetCategoryProp, ReferenceImages: []ProjectReferenceImage{{r.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, db, p.ID, a
}

// Only the external model call is controlled; worker, routing, billing and logs are real.
type profileFailingProvider struct {
	*Service
	cause error
}

func (p *profileFailingProvider) processTask(context.Context, model.Task) (map[string]interface{}, []map[string]interface{}, error) {
	return nil, nil, p.cause
}

func TestProfileReviewWorkerKeepsBillingAndCancellationSemantics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		status model.TaskStatus
	}{
		{"timeout", context.DeadlineExceeded, model.TaskStatusFailed},
		{"524", providerHTTPError{StatusCode: 524, Body: "PROFILE_SENTINEL"}, model.TaskStatusFailed},
		{"cancel", context.Canceled, model.TaskStatusCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, p, asset := profileCreationFixture(t)
			view, err := s.CreateProjectProfileAnalysis("user", p, asset.ID, CreateProfileAnalysisRequest{asset.PrimaryVersionID, "profile-image", ProfileAnalysisModelSelection{Kind: "system", ChannelID: "channel", ModelKey: "text-test"}})
			if err != nil {
				t.Fatal(err)
			}
			s.taskRouteExecutor = &taskRouteExecutor{port: &profileFailingProvider{s, tc.cause}}
			err = s.taskWorker().processNextTask()
			if tc.status == model.TaskStatusFailed && !errors.Is(err, tc.cause) {
				t.Errorf("worker lost original typed error: %v", err)
			}
			task, readErr := s.repo.Task(view.TaskID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if task.Status != tc.status {
				t.Errorf("status = %s, want %s", task.Status, tc.status)
			}
			order, readErr := s.repo.BillingOrder(task.BillingOrderID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if order.Status != model.BillingStatusUncertain {
				t.Errorf("billing = %s, want uncertain (never refund)", order.Status)
			}
			var successes int64
			if err := db.Model(&model.ApiCallLog{}).Where("task_id = ? AND status = ?", task.ID, model.ApiCallStatusSucceeded).Count(&successes).Error; err != nil {
				t.Fatal(err)
			}
			if successes != 0 {
				t.Fatal("fixture unexpectedly has successful call evidence")
			}
			raw, _ := json.Marshal(task)
			if strings.Contains(string(raw), "PROFILE_SENTINEL") {
				t.Error("persisted task leaked provider content")
			}
		})
	}
}

func TestProfileReviewAdoptionPublicProjections(t *testing.T) {
	for _, surface := range []string{"batch", "version"} {
		t.Run(surface, func(t *testing.T) {
			s, _, p, asset, task := profileAdoptFixture(t)
			adopted, err := s.AdoptProjectProfileAnalysis("user-1", p, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "abcdefghijklmnop"})
			if err != nil {
				t.Fatal(err)
			}
			var output any
			versionID := adopted.AdoptedVersionID
			if surface == "batch" {
				output, err = s.UserAssetsByIDs("user-1", []string{asset.ID})
			} else {
				var version model.AssetVersion
				version, err = s.CreateProjectAssetVersion("user-1", p, asset.ID, CreateAssetVersionRequest{DefinitionJSON: `{"description":"manual"}`})
				output, versionID = version, version.ID
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := s.repo.AssetVersion(versionID)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stored.DefinitionJSON, "idempotencyKeyHash") || !strings.Contains(stored.DefinitionJSON, "requestDigest") {
				t.Fatal("durable receipt lost")
			}
			raw, _ := json.Marshal(output)
			for _, key := range []string{"idempotencyKeyHash", "requestDigest", "draftDigest"} {
				if strings.Contains(string(raw), key) {
					t.Errorf("%s leaked internal %s", surface, key)
				}
			}
			if !strings.Contains(string(raw), "description") {
				t.Fatal("public definition removed")
			}
		})
	}
}

func TestProfileReviewPersistenceAllowlist(t *testing.T) {
	s, db, _, _ := profileCreationFixture(t)
	task := model.Task{ID: newID(), UserID: "user", Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusRunning, InputJSON: `{}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.WithValue(req.Context(), providerAnalyticsKey{}, providerAnalyticsContext{Service: s, UserID: "user", TaskID: task.ID, Operation: profileAnalysisOperation, Capability: "text"}))
	recordProviderRequest(req, time.Now(), 200, []byte(`{"error":{"code":"PROFILE_SENTINEL","message":"PROFILE_SENTINEL"}}`), nil)
	if err := s.LogAPICall(model.ApiCallLog{UserID: "user", TaskID: task.ID, Operation: profileAnalysisOperation, Status: model.ApiCallStatusFailed, ErrorCode: "PROFILE_SENTINEL", Error: "PROFILE_SENTINEL", RequestBody: "PROFILE_SENTINEL", ResponseBody: "PROFILE_SENTINEL"}); err != nil {
		t.Fatal(err)
	}
	attempt := &model.RouteAttempt{ID: newID(), TaskID: task.ID, Status: "selected"}
	if err := s.repo.CreateRouteAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	s.finishTaskRouteAttempt(attempt, &task, providerHTTPError{StatusCode: 524, Body: "PROFILE_SENTINEL"})
	if attempt.DispatchState != "submission_unknown" || attempt.FailureCode != "upstream_524" {
		t.Fatalf("route semantics changed: %+v", attempt)
	}
	var logs []model.ApiCallLog
	if err := db.Where("task_id = ?", task.ID).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs not exercised: %d", len(logs))
	}
	for _, call := range logs {
		raw, _ := json.Marshal(call)
		if strings.Contains(string(raw), "PROFILE_SENTINEL") {
			t.Error("API call persisted upstream sentinel")
		}
		raw, _ = json.Marshal(sanitizeDiagnosticAPICall(call))
		if strings.Contains(string(raw), "PROFILE_SENTINEL") {
			t.Error("diagnostic exposed upstream sentinel")
		}
	}
	var saved model.RouteAttempt
	if err := db.First(&saved, "id = ?", attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.FailureMessage, "PROFILE_SENTINEL") {
		t.Error("route persisted upstream sentinel")
	}
	if err := s.log("user", task.ID, "error", "任务处理失败", "PROFILE_SENTINEL"); err != nil {
		t.Fatal(err)
	}
	var taskLogs []model.TaskLog
	if err := db.Where("task_id = ?", task.ID).Find(&taskLogs).Error; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(taskLogs)
	if strings.Contains(string(raw), "PROFILE_SENTINEL") {
		t.Error("task log persisted upstream sentinel")
	}
}

func TestProfileReviewDiagnosticProjectsStoredUpstreamCode(t *testing.T) {
	raw, _ := json.Marshal(sanitizeDiagnosticAPICall(model.ApiCallLog{Operation: profileAnalysisOperation, Status: model.ApiCallStatusFailed, ErrorCode: "PROFILE_SENTINEL", Error: "PROFILE_SENTINEL"}))
	if strings.Contains(string(raw), "PROFILE_SENTINEL") {
		t.Fatal("diagnostic exposed previously stored upstream content")
	}
}

func TestProfileReviewTransactionPreservesAdmissionErrors(t *testing.T) {
	for _, err := range []error{
		&AppError{Status: 401, Code: 401, Message: "auth"},
		&AppError{Status: 403, Code: 403, Message: "quota"},
		&AppError{Status: 402, Code: 402, Message: "billing"},
		repository.ErrInsufficientCredits,
		repository.ErrActiveTaskLimit,
		errors.New("unexpected database failure"),
	} {
		if profileTransactionError(err) != err {
			t.Errorf("overrode unrelated admission error: %v", err)
		}
	}
}

func TestProfileReviewCreationRechecksMapWithoutDebit(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		status    int
		reason    string
	}{
		{"version", "UPDATE assets SET primary_version_id = 'changed' WHERE id = ?", 409, "project_asset_version_changed"},
		{"source", "UPDATE asset_versions SET definition_json = '{}' WHERE asset_id = ?", 409, "asset_profile_analysis_source_changed"},
		{"unavailable", "UPDATE resources SET status = 'deleted' WHERE id = 'profile-image' AND ? <> ''", 422, "asset_profile_analysis_source_unavailable"},
		{"notfound", "DELETE FROM project_asset_links WHERE asset_id = ?", 404, "asset_profile_analysis_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, p, asset := profileCreationFixture(t)
			fired := false
			if err := db.Callback().Update().Before("gorm:update").Register("profile_review_race", func(tx *gorm.DB) {
				if fired || tx.Statement.Table != "assets" {
					return
				}
				fired = true
				if err := tx.Session(&gorm.Session{NewDB: true}).Exec(tc.sql, asset.ID).Error; err != nil {
					tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			_, err := s.CreateProjectProfileAnalysis("user", p, asset.ID, CreateProfileAnalysisRequest{asset.PrimaryVersionID, "profile-image", ProfileAnalysisModelSelection{Kind: "system", ChannelID: "channel", ModelKey: "text-test"}})
			if !fired {
				t.Fatalf("transaction recheck not exercised: %v", err)
			}
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != tc.status || string(appErr.Reason) != tc.reason {
				t.Errorf("expected %d/%s, got %v", tc.status, tc.reason, err)
			}
			for _, table := range []string{"tasks", "billing_orders", "credit_ledger_entries"} {
				var count int64
				if err := db.Table(table).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Errorf("race left %d rows in %s", count, table)
				}
			}
			account, err := s.repo.CreditAccount("user")
			if err != nil {
				t.Fatal(err)
			}
			if account.AvailableMicrocredits != 10000 || account.ReservedMicrocredits != 0 {
				t.Fatalf("race debited credits: %+v", account)
			}
		})
	}
}

func TestProfileAnalysisGenericOutputNeverLeaksRaw(t *testing.T) {
	task := model.Task{Operation: "asset_image_profile_analysis", Status: model.TaskStatusFailed, InputJSON: `{"metadata":{"secret":"RAW_SECRET"}}`, ResultJSON: `{"text":"RAW_SECRET","reasoning":"RAW_SECRET"}`, TextDraft: "RAW_SECRET", Error: "RAW_SECRET"}
	raw, _ := json.Marshal(taskForOutput(task))
	if strings.Contains(string(raw), "RAW_SECRET") {
		t.Fatal("generic task output leaked unvalidated provider content")
	}
}
