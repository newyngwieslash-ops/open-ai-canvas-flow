package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/platform"
	"yingce/backend/internal/repository"
)

func batchTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	svc, db, projectID := referenceTestService(t)
	svc.dataDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(svc.dataDir, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(&model.SystemSetting{Key: "feature_availability", ValueJSON: `{"customChannelsEnabled":true}`}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&model.Resource{ID: "image-out-1", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Width: 800, Height: 600, Size: 100, MimeType: "image/png", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	return svc, db, projectID
}

func batchQueueTestRequest(t *testing.T, svc *Service, projectID string, count int) CreateProjectAssetBatchRequest {
	t.Helper()
	req := PreflightProjectAssetBatchRequest{
		Provider: "custom", Model: "gemini-2.5-flash-image",
		Config: map[string]any{
			"interfaceType": "gemini-image", "baseUrl": "https://api.example.com/v1",
			"apiKey": "sk-batch-queue-test", "model": "gemini-2.5-flash-image",
		},
	}
	for i := 0; i < count; i++ {
		asset, err := svc.CreateProjectAssetEntity("user-1", projectID, CreateProjectAssetEntityRequest{
			Category: model.AssetCategoryProp, Name: fmt.Sprintf("queued prop %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		req.Items = append(req.Items, ProjectAssetBatchItemInput{
			AssetID: asset.ID, ExpectedVersionID: asset.PrimaryVersionID,
			ReferenceSource: "none", Prompt: asset.Title, Operation: "project_asset_image",
		})
	}
	preflight, err := svc.PreflightProjectAssetBatch("user-1", projectID, req)
	if err != nil {
		t.Fatal(err)
	}
	return CreateProjectAssetBatchRequest{
		IdempotencyKey: newID(), PreflightDigest: preflight.PreflightDigest,
		PreflightProjectAssetBatchRequest: req,
	}
}

func TestProjectAssetBatchQueuesAboveActiveLimit(t *testing.T) {
	svc, db, projectID := batchTestService(t)
	req := batchQueueTestRequest(t, svc, projectID, 23)
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, req)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, item := range batch.Items {
		counts[item.Status]++
		if item.Status == "pending" && (item.TaskID != "" || item.CanRetry || item.Attempt != 1) {
			t.Fatalf("waiting item was submitted or treated as a retry: %+v", item)
		}
	}
	if counts["running"] != 5 || counts["pending"] != 18 || counts["failed"] != 0 {
		t.Fatalf("capacity must defer remaining items, got %v", counts)
	}
	var tasks int64
	if err := db.Model(&model.Task{}).Count(&tasks).Error; err != nil || tasks != 5 {
		t.Fatalf("active limit was bypassed: tasks=%d err=%v", tasks, err)
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(encoded, &response); err != nil || response["totalCount"] != float64(23) {
		t.Fatalf("batch response missing totalCount: %v err=%v", response["totalCount"], err)
	}
}

func TestProjectAssetBatchPreflightAtCapacity(t *testing.T) {
	svc, _, projectID := batchTestService(t)
	req := batchQueueTestRequest(t, svc, projectID, 1)
	for i := 0; i < 5; i++ {
		if _, err := svc.CreateProjectAssetBatch("user-1", projectID, req); err != nil {
			t.Fatal(err)
		}
		req.IdempotencyKey = newID()
	}
	if _, err := svc.PreflightProjectAssetBatch("user-1", projectID, req.PreflightProjectAssetBatchRequest); err != nil {
		t.Fatalf("preflight must remain available while waiting for capacity: %v", err)
	}
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, req)
	if err != nil || batch.Items[0].Status != "pending" {
		t.Fatalf("full queue must accept a waiting batch: batch=%+v err=%v", batch, err)
	}
}

type batchImageExecutionProbe struct {
	*Service
	calls atomic.Int32
}

func (p *batchImageExecutionProbe) processTask(_ context.Context, task model.Task) (map[string]interface{}, []map[string]interface{}, error) {
	p.calls.Add(1)
	input, err := p.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		return nil, nil, err
	}
	var parsed canvasGenerationInput
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, nil, err
	}
	if parsed.Config.APIKey != "sk-batch-queue-test" {
		return nil, nil, errors.New("queued task lost its credentials after restart")
	}
	return map[string]interface{}{"images": []map[string]interface{}{{"resourceId": "image-out-1", "storageKey": "resource:image-out-1"}}}, nil, nil
}

func TestProjectAssetBatchWorkerContinuesAfterRestart(t *testing.T) {
	// The concurrent worker needs the same WAL locking behavior as the local server.
	svc, db := newMediaRecoveryTestService(t)
	project, _ := seedWorkflowProject(t, db)
	projectID := project.ID
	if err := db.Save(&model.SystemSetting{Key: "feature_availability", ValueJSON: `{"customChannelsEnabled":true}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Resource{ID: "image-out-1", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Width: 800, Height: 600, Size: 100, MimeType: "image/png"}).Error; err != nil {
		t.Fatal(err)
	}
	req := batchQueueTestRequest(t, svc, projectID, 23)
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, req)
	if err != nil {
		t.Fatal(err)
	}
	_, items, err := svc.repo.ProjectAssetGenerationBatchWithItems("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.Contains(item.InputSnapshotJSON, "sk-batch-queue-test") {
			t.Fatal("batch snapshot leaked plaintext credentials")
		}
	}

	// No original request or browser polling is available to the new worker.
	t.Setenv("REDIS_URL", "")
	coordinator, err := platform.NewCoordinator("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Service{
		repo: svc.repo, dataDir: svc.dataDir, coordinator: coordinator,
		activeCancels: make(map[string]context.CancelFunc), taskDispatcherWake: make(chan struct{}, 1),
	}
	probe := &batchImageExecutionProbe{Service: restarted}
	restarted.taskRouteExecutor = &taskRouteExecutor{port: probe}
	ctx, _ := restarted.backgroundWorkers().Start()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restarted.StopWorker(stopCtx); err != nil {
			t.Error(err)
		}
	})
	restarted.taskWorker().start(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var succeeded int64
		if err := db.Model(&model.Task{}).Where("status = ?", model.TaskStatusSucceeded).Count(&succeeded).Error; err != nil {
			t.Fatal(err)
		}
		active, err := svc.repo.ActiveTaskCountForUser("user-1")
		if err != nil || active > 5 {
			t.Fatalf("worker exceeded user capacity: active=%d err=%v", active, err)
		}
		if succeeded == 23 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not drain the batch after restart: succeeded=%d providerCalls=%d", succeeded, probe.calls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	view, err := restarted.GetProjectAssetBatch("user-1", projectID, batch.ID)
	if err != nil || view.Status != "completed" || probe.calls.Load() != 23 {
		t.Fatalf("batch did not complete exactly once: batch=%+v calls=%d err=%v", view, probe.calls.Load(), err)
	}
	for _, item := range view.Items {
		if item.Attempt != 1 || item.Candidate == nil {
			t.Fatalf("queue wait must preserve the attempt and manual candidate: %+v", item)
		}
	}
}

func TestProjectAssetBatchStopWinsOverPreparedDispatch(t *testing.T) {
	svc, db, projectID := batchTestService(t)
	req := batchQueueTestRequest(t, svc, projectID, 6)
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, req)
	if err != nil {
		t.Fatal(err)
	}
	_, prepared, err := svc.evaluateProjectAssetBatch("user-1", projectID, req.PreflightProjectAssetBatchRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, items, err := svc.repo.ProjectAssetGenerationBatchWithItems("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StopProjectAssetBatch("user-1", projectID, batch.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("user_id = ?", "user-1").Update("status", model.TaskStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	// A dispatcher may have prepared its request just before the stop committed.
	taskReq := prepared[5].TaskReq
	taskReq.creationSubmissionID = items[5].SubmissionKey
	if _, err := svc.CreateTask("user-1", taskReq); !errors.Is(err, repository.ErrBatchItemAlreadyDispatched) {
		t.Fatalf("stopped item was admitted after stop: %v", err)
	}
	var tasks int64
	if err := db.Model(&model.Task{}).Count(&tasks).Error; err != nil || tasks != 5 {
		t.Fatalf("stopped item created a task: tasks=%d err=%v", tasks, err)
	}
}

func TestProjectAssetBatchStaleViewCannotRestoreStoppedItems(t *testing.T) {
	svc, db, projectID := batchTestService(t)
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, batchQueueTestRequest(t, svc, projectID, 6))
	if err != nil {
		t.Fatal(err)
	}
	staleBatch, staleItems, err := svc.repo.ProjectAssetGenerationBatchWithItems("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StopProjectAssetBatch("user-1", projectID, batch.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", staleItems[0].TaskID).Update("status", model.TaskStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.reconcileAndBuildBatchView("user-1", staleBatch, staleItems); err != nil {
		t.Fatal(err)
	}
	_, items, err := svc.repo.ProjectAssetGenerationBatchWithItems("user-1", projectID, batch.ID)
	if err != nil || items[5].Status != "stopped" {
		t.Fatalf("stale view resurrected stopped work: status=%s err=%v", items[5].Status, err)
	}
}

func TestProjectAssetBatchBillableQueueReservesOnlyAdmittedTasks(t *testing.T) {
	svc, db, projectID := batchTestService(t)
	req := batchQueueTestRequest(t, svc, projectID, 6)
	profile := mustEncodeModelCapabilityConfig(t, DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceGeminiImage), req.Model))
	for _, row := range []any{
		&model.ModelChannel{ID: "batch-channel", Scope: model.ChannelScopeSystem, Enabled: true},
		&model.ChannelModel{ID: "batch-model", ChannelID: "batch-channel", ModelKey: req.Model, Capability: "image", Protocol: model.ChannelInterfaceGeminiImage, CapabilityConfigJSON: profile, Enabled: true},
		&model.ChannelModelPriceTier{ID: "batch-price", ChannelModelID: "batch-model", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true},
		&model.CreditAccount{UserID: "user-1", AvailableMicrocredits: 10000},
	} {
		if err := db.Save(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	req.Provider = "system"
	req.Config = map[string]any{"channelId": "batch-channel", "model": req.Model}
	preflight, err := svc.PreflightProjectAssetBatch("user-1", projectID, req.PreflightProjectAssetBatchRequest)
	if err != nil {
		t.Fatal(err)
	}
	req.PreflightDigest = preflight.PreflightDigest
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, req)
	if err != nil {
		t.Fatal(err)
	}
	before := captureTaskBillingAccounting(t, db, "user-1")
	if before.tasks != 5 || before.billingOrders != 5 || before.availableMicrocredits != 9500 || batch.Items[5].Status != "pending" {
		t.Fatalf("waiting work reserved credits or bypassed capacity: %+v status=%s", before, batch.Items[5].Status)
	}
	_, prepared, err := svc.evaluateProjectAssetBatch("user-1", projectID, req.PreflightProjectAssetBatchRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, items, err := svc.repo.ProjectAssetGenerationBatchWithItems("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StopProjectAssetBatch("user-1", projectID, batch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelProjectAssetBatchTasks(context.Background(), "user-1", projectID, batch.ID); err != nil {
		t.Fatal(err)
	}
	before = captureTaskBillingAccounting(t, db, "user-1")
	taskReq := prepared[5].TaskReq
	taskReq.creationSubmissionID = items[5].SubmissionKey
	if _, err := svc.CreateTask("user-1", taskReq); !errors.Is(err, repository.ErrBatchItemAlreadyDispatched) {
		t.Fatalf("stopped billable task was admitted: %v", err)
	}
	if after := captureTaskBillingAccounting(t, db, "user-1"); after != before {
		t.Fatalf("stop rejection changed billing: before=%+v after=%+v", before, after)
	}
}

func TestProjectAssetBatchRetryWaitsForCapacity(t *testing.T) {
	svc, db, projectID := batchTestService(t)
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, batchQueueTestRequest(t, svc, projectID, 6))
	if err != nil {
		t.Fatal(err)
	}
	first := batch.Items[0]
	if err := db.Model(&model.Task{}).Where("id = ?", first.TaskID).Updates(map[string]any{"status": model.TaskStatusFailed, "error": "upstream failed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Task{ID: "other-active-task", UserID: "user-1", Status: model.TaskStatusQueued}).Error; err != nil {
		t.Fatal(err)
	}
	retried, err := svc.RetryProjectAssetBatchItem("user-1", projectID, batch.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	item := retried.Items[0]
	if item.Status != "pending" || item.TaskID != "" || item.Attempt != 2 || len(item.AttemptHistory) != 1 {
		t.Fatalf("retry should wait without another failed attempt: %+v", item)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", "other-active-task").Update("status", model.TaskStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.dispatchPendingProjectAssetBatchItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	resumed, err := svc.GetProjectAssetBatch("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	item = resumed.Items[0]
	if item.Status != "running" || item.TaskID == "" || item.TaskID == first.TaskID || item.Attempt != 2 || resumed.Items[5].Status != "pending" {
		t.Fatalf("retry did not resume once capacity returned: %+v", item)
	}
	task, err := svc.repo.Task(item.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	input, err := svc.decryptTaskInputJSON(task.InputJSON)
	if err != nil || !strings.Contains(input, "sk-batch-queue-test") {
		t.Fatalf("queued retry lost its execution credentials: %v", err)
	}
}

func TestProjectAssetBatchMigrationAndIdempotency(t *testing.T) {
	svc, _, projectID := batchTestService(t)

	charAsset, err := svc.CreateProjectCharacter("user-1", projectID, CreateProjectCharacterRequest{
		Name:            "主角A",
		ReferenceImages: []ProjectReferenceImage{{ResourceID: "image-1"}, {ResourceID: "image-2"}},
	})
	if err != nil {
		t.Fatalf("create character: %v", err)
	}
	envAsset, err := svc.CreateProjectAssetEntity("user-1", projectID, CreateProjectAssetEntityRequest{
		Category:        model.AssetCategoryEnvironment,
		Name:            "古城街道",
		Definition:      map[string]any{"description": "雨夜青石板古街"},
		ReferenceImages: []ProjectReferenceImage{{ResourceID: "image-1"}},
	})
	if err != nil {
		t.Fatalf("create env asset: %v", err)
	}

	// Duplicate asset in batch must be rejected
	_, err = svc.PreflightProjectAssetBatch("user-1", projectID, PreflightProjectAssetBatchRequest{
		Provider: "custom",
		Model:    "gemini-2.5-flash-image",
		Config: map[string]any{
			"interfaceType": "gemini-image",
			"baseUrl":       "https://api.example.com/v1",
			"apiKey":        "sk-secret-do-not-persist-plaintext",
			"model":         "gemini-2.5-flash-image",
		},
		Items: []ProjectAssetBatchItemInput{
			{AssetID: charAsset.Asset.ID, ExpectedVersionID: charAsset.Asset.PrimaryVersionID, ReferenceSource: "original", Prompt: "角色立绘"},
			{AssetID: charAsset.Asset.ID, ExpectedVersionID: charAsset.Asset.PrimaryVersionID, ReferenceSource: "original", Prompt: "重复角色"},
		},
	})
	if err == nil {
		t.Fatalf("expected duplicate assetId to be rejected")
	}

	preflightReq := PreflightProjectAssetBatchRequest{
		Provider: "custom",
		Model:    "gemini-2.5-flash-image",
		Config: map[string]any{
			"interfaceType": "gemini-image",
			"baseUrl":       "https://api.example.com/v1",
			"apiKey":        "sk-secret-do-not-persist-plaintext",
			"model":         "gemini-2.5-flash-image",
		},
		Items: []ProjectAssetBatchItemInput{
			{
				AssetID:           charAsset.Asset.ID,
				ExpectedVersionID: charAsset.Asset.PrimaryVersionID,
				ReferenceSource:   "original",
				Prompt:            "角色三视图",
				Operation:         "character_turnaround",
			},
			{
				AssetID:           envAsset.ID,
				ExpectedVersionID: envAsset.PrimaryVersionID,
				ReferenceSource:   "original",
				Prompt:            "古街全景",
				Operation:         "project_asset_image",
			},
		},
	}

	preflight, err := svc.PreflightProjectAssetBatch("user-1", projectID, preflightReq)
	if err != nil {
		t.Fatalf("preflight batch: %v", err)
	}
	if preflight.PreflightDigest == "" || len(preflight.Items) != 2 {
		t.Fatalf("unexpected preflight response: %+v", preflight)
	}
	if preflight.PriceUnknownCount != 2 || !preflight.Items[0].PriceUnknown {
		t.Fatalf("expected custom channel items to mark priceUnknown=true, got %+v", preflight)
	}

	createReq := CreateProjectAssetBatchRequest{
		IdempotencyKey:                    "idem-batch-1",
		PreflightDigest:                   preflight.PreflightDigest,
		PreflightProjectAssetBatchRequest: preflightReq,
	}
	batch1, err := svc.CreateProjectAssetBatch("user-1", projectID, createReq)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if len(batch1.Items) != 2 || batch1.Items[0].TaskID == "" || batch1.Items[1].TaskID == "" {
		t.Fatalf("expected 2 dispatched items with taskIds, got %+v", batch1)
	}

	// Verify snapshots in DB never store plaintext apiKey
	rawBatch, rawItems, err := svc.repo.ProjectAssetGenerationBatchWithItems("user-1", projectID, batch1.ID)
	if err != nil {
		t.Fatalf("load raw batch: %v", err)
	}
	if strings.Contains(rawBatch.ConfigSummaryJSON, "sk-secret-do-not-persist-plaintext") {
		t.Fatalf("batch ConfigSummaryJSON leaked plaintext apiKey: %s", rawBatch.ConfigSummaryJSON)
	}
	for _, rawItem := range rawItems {
		if strings.Contains(rawItem.InputSnapshotJSON, "sk-secret-do-not-persist-plaintext") {
			t.Fatalf("item InputSnapshotJSON leaked plaintext apiKey: %s", rawItem.InputSnapshotJSON)
		}
	}

	// Replay with identical idempotencyKey + request returns exact same batch and taskIds
	batchReplay, err := svc.CreateProjectAssetBatch("user-1", projectID, createReq)
	if err != nil {
		t.Fatalf("replay batch: %v", err)
	}
	if batchReplay.ID != batch1.ID || batchReplay.Items[0].TaskID != batch1.Items[0].TaskID || batchReplay.Items[1].TaskID != batch1.Items[1].TaskID {
		t.Fatalf("expected idempotent replay to return identical batch and taskIds, got %+v vs %+v", batchReplay, batch1)
	}

	// Replay with same idempotencyKey but different request payload returns 409 Conflict
	conflictReq := createReq
	conflictReq.Items = append([]ProjectAssetBatchItemInput(nil), createReq.Items...)
	conflictReq.Items[0].Prompt = "被篡改的提示词"
	_, err = svc.CreateProjectAssetBatch("user-1", projectID, conflictReq)
	if err == nil || errorReasonOf(err) != "batch_idempotency_conflict" {
		t.Fatalf("expected batch_idempotency_conflict, got %v (reason=%s)", err, errorReasonOf(err))
	}
}

func errorReasonOf(err error) string {
	if err == nil {
		return ""
	}
	if appErr, ok := err.(*AppError); ok {
		return string(appErr.Reason)
	}
	return ""
}

func TestProjectAssetBatchPreflightAndStaleRejection(t *testing.T) {
	svc, _, projectID := batchTestService(t)

	propAsset, err := svc.CreateProjectAssetEntity("user-1", projectID, CreateProjectAssetEntityRequest{
		Category:        model.AssetCategoryProp,
		Name:            "青铜剑",
		Definition:      map[string]any{"description": "古朴青铜剑"},
		ReferenceImages: []ProjectReferenceImage{{ResourceID: "image-1"}, {ResourceID: "image-2"}},
	})
	if err != nil {
		t.Fatalf("create prop: %v", err)
	}

	// Model only supports 1 reference image -> preflight must reject
	_, err = svc.PreflightProjectAssetBatch("user-1", projectID, PreflightProjectAssetBatchRequest{
		Provider: "custom",
		Model:    "grok-2-image",
		Config: map[string]any{
			"interfaceType": "grok-image",
			"baseUrl":       "https://api.example.com/v1",
			"apiKey":        "sk-test",
			"model":         "grok-2-image",
			"capabilityConfig": map[string]any{
				"image": map[string]any{
					"references": map[string]any{"enabled": true, "maxImages": 1},
				},
			},
		},
		Items: []ProjectAssetBatchItemInput{
			{
				AssetID:           propAsset.ID,
				ExpectedVersionID: propAsset.PrimaryVersionID,
				ReferenceSource:   "original",
				Prompt:            "青铜剑特写",
				Operation:         "project_asset_image",
			},
		},
	})
	if err == nil {
		t.Fatalf("expected preflight to reject when reference count exceeds model maxImages")
	}

	// Valid preflight with compatible model
	preflightReq := PreflightProjectAssetBatchRequest{
		Provider: "custom",
		Model:    "gemini-2.5-flash-image",
		Config: map[string]any{
			"interfaceType": "gemini-image",
			"baseUrl":       "https://api.example.com/v1",
			"apiKey":        "sk-test",
			"model":         "gemini-2.5-flash-image",
		},
		Items: []ProjectAssetBatchItemInput{
			{
				AssetID:           propAsset.ID,
				ExpectedVersionID: propAsset.PrimaryVersionID,
				ReferenceSource:   "original",
				Prompt:            "青铜剑特写",
				Operation:         "project_asset_image",
			},
		},
	}
	preflight, err := svc.PreflightProjectAssetBatch("user-1", projectID, preflightReq)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}

	// Evolve asset version after preflight -> submission with old preflight/version must fail with 409
	_, err = svc.SetProjectReferenceImages("user-1", projectID, propAsset.ID, SetProjectReferenceImagesRequest{
		ReferenceImages:   []ProjectReferenceImage{{ResourceID: "image-1"}},
		ExpectedVersionID: propAsset.PrimaryVersionID,
	})
	if err != nil {
		t.Fatalf("update references: %v", err)
	}

	_, err = svc.CreateProjectAssetBatch("user-1", projectID, CreateProjectAssetBatchRequest{
		IdempotencyKey:                    "idem-stale-1",
		PreflightDigest:                   preflight.PreflightDigest,
		PreflightProjectAssetBatchRequest: preflightReq,
	})
	if err == nil {
		t.Fatalf("expected stale version/preflight to be rejected on submission")
	}
}

func TestProjectAssetBatchStopCancelRetryAndResourceProtection(t *testing.T) {
	svc, db, projectID := batchTestService(t)

	asset1, err := svc.CreateProjectAssetEntity("user-1", projectID, CreateProjectAssetEntityRequest{
		Category:        model.AssetCategoryProp,
		Name:            "玉佩",
		Definition:      map[string]any{"description": "白玉龙纹玉佩"},
		ReferenceImages: []ProjectReferenceImage{{ResourceID: "image-1"}},
	})
	if err != nil {
		t.Fatalf("create asset1: %v", err)
	}
	asset2, err := svc.CreateProjectAssetEntity("user-1", projectID, CreateProjectAssetEntityRequest{
		Category:        model.AssetCategoryEnvironment,
		Name:            "大殿",
		Definition:      map[string]any{"description": "金碧辉煌大殿"},
		ReferenceImages: []ProjectReferenceImage{{ResourceID: "image-2"}},
	})
	if err != nil {
		t.Fatalf("create asset2: %v", err)
	}
	asset3, err := svc.CreateProjectAssetEntity("user-1", projectID, CreateProjectAssetEntityRequest{
		Category:   model.AssetCategoryProp,
		Name:       "长枪",
		Definition: map[string]any{"description": "银甲长枪"},
	})
	if err != nil {
		t.Fatalf("create asset3: %v", err)
	}

	preflightReq := PreflightProjectAssetBatchRequest{
		Provider: "custom",
		Model:    "gemini-2.5-flash-image",
		Config: map[string]any{
			"interfaceType": "gemini-image",
			"baseUrl":       "https://api.example.com/v1",
			"apiKey":        "sk-test",
			"model":         "gemini-2.5-flash-image",
		},
		Items: []ProjectAssetBatchItemInput{
			{AssetID: asset1.ID, ExpectedVersionID: asset1.PrimaryVersionID, ReferenceSource: "original", Prompt: "玉佩", Operation: "project_asset_image"},
			{AssetID: asset2.ID, ExpectedVersionID: asset2.PrimaryVersionID, ReferenceSource: "original", Prompt: "大殿", Operation: "project_asset_image"},
			{AssetID: asset3.ID, ExpectedVersionID: asset3.PrimaryVersionID, ReferenceSource: "none", Prompt: "长枪", Operation: "project_asset_image"},
		},
	}
	preflight, err := svc.PreflightProjectAssetBatch("user-1", projectID, preflightReq)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}

	// Pause dispatch after 2 items to simulate 1 unsubmitted pending item when Stop is called
	svc.batchDispatchLimitForTest = 2
	batch, err := svc.CreateProjectAssetBatch("user-1", projectID, CreateProjectAssetBatchRequest{
		IdempotencyKey:                    "idem-stop-cancel",
		PreflightDigest:                   preflight.PreflightDigest,
		PreflightProjectAssetBatchRequest: preflightReq,
	})
	svc.batchDispatchLimitForTest = 0
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if batch.Items[0].Status != "running" || batch.Items[1].Status != "running" || batch.Items[2].Status != "pending" {
		t.Fatalf("expected 2 running + 1 pending item, got %+v", batch.Items)
	}

	// Resource referenced by active/pending batch item is protected from deletion
	if err := svc.repo.RequireNoOriginalImageReferences([]string{"image-1"}); err == nil {
		t.Fatalf("expected resource referenced by active batch item/version to be protected")
	}

	// Stop unsubmitted items: only item[2] becomes stopped, item[0] & item[1] keep their taskIds
	stoppedBatch, err := svc.StopProjectAssetBatch("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatalf("stop batch: %v", err)
	}
	if stoppedBatch.Items[2].Status != "stopped" || stoppedBatch.Items[2].TaskID != "" {
		t.Fatalf("expected item[2] to be stopped without taskId, got %+v", stoppedBatch.Items[2])
	}
	if stoppedBatch.Items[0].TaskID == "" || stoppedBatch.Items[1].TaskID == "" {
		t.Fatalf("expected item[0] and item[1] to retain taskIds after stop")
	}

	// Complete item[0] as succeeded candidate, fail item[1]
	task0, err := svc.repo.Task(stoppedBatch.Items[0].TaskID)
	if err != nil {
		t.Fatalf("get task0: %v", err)
	}
	task0.Status = model.TaskStatusRunning
	if err := db.Save(task0).Error; err != nil {
		t.Fatalf("update task0 running: %v", err)
	}
	// Real backend-saved media keeps its checkpoint after success.
	if err := svc.saveMediaCheckpoint(task0, &mediaCheckpoint{Mode: "image", Items: []mediaCheckpointItem{{ResourceID: "image-out-1"}}}, "register"); err != nil {
		t.Fatalf("save task0 media checkpoint: %v", err)
	}
	if err := svc.saveTaskCompletionWithinStorageQuota(task0, []byte(`{"images":[{"resourceId":"image-out-1","storageKey":"resource:image-out-1"}]}`), nil, false); err != nil {
		t.Fatalf("finalize manual candidate for task0: %v", err)
	}
	if _, err := svc.repo.AssetForUser("user-1", generationMediaAssetID(task0.ID, 0)); err == nil {
		t.Fatalf("project asset image result must stay a candidate, not become a separate material asset")
	}

	task1, err := svc.repo.Task(stoppedBatch.Items[1].TaskID)
	if err != nil {
		t.Fatalf("get task1: %v", err)
	}
	task1.Status = model.TaskStatusFailed
	task1.Error = "上游生成超时"
	if err := db.Save(task1).Error; err != nil {
		t.Fatalf("update task1: %v", err)
	}

	refreshed, err := svc.GetProjectAssetBatch("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if refreshed.Status != "partial_failed" {
		t.Fatalf("expected batch status partial_failed, got %s", refreshed.Status)
	}
	if refreshed.Items[0].Status != "succeeded" || refreshed.Items[0].Candidate == nil || refreshed.Items[0].Candidate.State != "pending_confirmation" {
		t.Fatalf("expected item[0] succeeded with pending_confirmation candidate, got %+v", refreshed.Items[0])
	}
	if refreshed.Items[1].Status != "failed" || !refreshed.Items[1].CanRetry {
		t.Fatalf("expected item[1] failed and retryable, got %+v", refreshed.Items[1])
	}

	// Retry item[0] (succeeded) must be rejected
	if _, err := svc.RetryProjectAssetBatchItem("user-1", projectID, batch.ID, refreshed.Items[0].ID); err == nil {
		t.Fatalf("expected retrying succeeded item to be rejected")
	}

	// Retry item[1] (confirmed failed) must increment attempt, preserve old taskId in history, and create new task
	oldTask1ID := refreshed.Items[1].TaskID
	retried, err := svc.RetryProjectAssetBatchItem("user-1", projectID, batch.ID, refreshed.Items[1].ID)
	if err != nil {
		t.Fatalf("retry item[1]: %v", err)
	}
	if retried.Items[1].Attempt != 2 || retried.Items[1].TaskID == "" || retried.Items[1].TaskID == oldTask1ID {
		t.Fatalf("expected item[1] attempt=2 with new taskId, got %+v", retried.Items[1])
	}
	if len(retried.Items[1].AttemptHistory) != 1 || retried.Items[1].AttemptHistory[0].TaskID != oldTask1ID {
		t.Fatalf("expected attempt history to preserve old taskId %s, got %+v", oldTask1ID, retried.Items[1].AttemptHistory)
	}

	// Cancel active tasks: item[1]'s new running task gets cancelled while item[0]'s succeeded candidate is preserved
	afterCancel, err := svc.CancelProjectAssetBatchTasks(context.Background(), "user-1", projectID, batch.ID)
	if err != nil {
		t.Fatalf("cancel batch tasks: %v", err)
	}
	if afterCancel.Items[0].Status != "succeeded" || afterCancel.Items[0].Candidate == nil {
		t.Fatalf("expected succeeded item[0] candidate to remain intact after cancel-tasks, got %+v", afterCancel.Items[0])
	}
	if afterCancel.Items[1].Status != "cancelled" && afterCancel.Items[1].Status != "cancel_requested" {
		t.Fatalf("expected item[1] status cancelled or cancel_requested, got %s", afterCancel.Items[1].Status)
	}

	// Adopt item[0] candidate via existing single-candidate adoption API
	adoptedAsset, err := svc.AdoptProjectImageCandidate("user-1", projectID, asset1.ID, afterCancel.Items[0].TaskID, AdoptProjectImageCandidateRequest{
		ExpectedVersionID: asset1.PrimaryVersionID,
	})
	if err != nil {
		t.Fatalf("adopt item[0] candidate: %v", err)
	}
	if adoptedAsset.StorageKey != "resource:image-out-1" {
		t.Fatalf("expected adopted asset storageKey resource:image-out-1, got %+v", adoptedAsset)
	}

	// Mark an item task with MediaRecoveryJSON checkpoint -> status becomes reconcile_required and retry is forbidden
	taskRetry, err := svc.repo.Task(afterCancel.Items[1].TaskID)
	if err != nil {
		t.Fatalf("get retried task: %v", err)
	}
	taskRetry.Status = model.TaskStatusFailed
	taskRetry.MediaStage = "download_failed"
	taskRetry.MediaRecoveryJSON = "encrypted-checkpoint"
	if err := db.Save(taskRetry).Error; err != nil {
		t.Fatalf("update retried task: %v", err)
	}
	reconcileBatch, err := svc.GetProjectAssetBatch("user-1", projectID, batch.ID)
	if err != nil {
		t.Fatalf("get reconcile batch: %v", err)
	}
	if reconcileBatch.Items[1].Status != "reconcile_required" || reconcileBatch.Items[1].CanRetry {
		t.Fatalf("expected item[1] with media recovery checkpoint to be reconcile_required and non-retryable, got %+v", reconcileBatch.Items[1])
	}
	if _, err := svc.RetryProjectAssetBatchItem("user-1", projectID, batch.ID, reconcileBatch.Items[1].ID); err == nil {
		t.Fatalf("expected retrying reconcile_required item to be rejected")
	}
}
