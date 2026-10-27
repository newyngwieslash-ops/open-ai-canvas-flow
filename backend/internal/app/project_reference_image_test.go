package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/protocol"
	"yingce/backend/internal/repository"
)

func referenceTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()
	s, db, projectID, _ := seedProjectVisualAsset(t, model.AssetCategoryEnvironment)
	if err := db.AutoMigrate(&model.ProjectAssetFolder{}, &model.VoiceProfile{}, &model.ApiCallLog{}, &model.BillingOrder{}); err != nil {
		t.Fatal(err)
	}
	return s, db, projectID
}
func requireVersionConflict(t *testing.T, err error) {
	t.Helper()
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Reason != "project_asset_version_changed" {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestProjectImageManualWorkflowBindingIsolation(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint(nested), func(t *testing.T) {
			s, db, p := referenceTestService(t)
			if err := db.AutoMigrate(&model.WorkflowTemplateVersion{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.WorkflowStepTask{}, &model.ProductionTaskLink{}, &model.SystemSetting{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}); err != nil {
				t.Fatal(err)
			}
			asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "manual", Category: model.AssetCategoryProp})
			if err != nil {
				t.Fatal(err)
			}
			instance := model.WorkflowInstance{ID: "workflow", ProjectID: p}
			step := model.WorkflowStepInstance{ID: "step", WorkflowInstanceID: instance.ID, StepKey: "assets", Status: model.WorkflowStepStatusReady}
			if err := db.Create(&instance).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&step).Error; err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"mode": "image", "metadata": map[string]any{"operation": "project_asset_image", "adoptionMode": "manual", "projectAssetId": asset.ID, "projectAssetVersionId": asset.PrimaryVersionID, "referenceSource": "none"}}
			binding := input
			if nested {
				binding = input["metadata"].(map[string]any)
			}
			binding["workflowStepId"], binding["assetVersionId"], binding["resourceId"], binding["role"] = step.ID, asset.PrimaryVersionID, "image-1", "output"
			if err := s.prepareProjectImageTask("user-1", p, "canvas_image", input); err == nil {
				t.Error("manual admission accepted automatic binding metadata")
			}
			raw, _ := json.Marshal(input)
			task := model.Task{ID: "manual-workflow", UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: string(raw)}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.saveTaskCompletionWithinStorageQuota(&task, []byte(`{"images":[{"resourceId":"image-1","storageKey":"resource:image-1"}]}`), nil, false); err != nil {
				t.Fatal(err)
			}
			saved, err := s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if candidateFromResult(saved.ResultJSON) == nil {
				t.Fatal("manual candidate not saved")
			}
			if err := s.RegisterTaskOutputFromTask(*saved); err != nil {
				t.Fatal(err)
			}
			reps, err := s.repo.AssetRepresentations(asset.PrimaryVersionID)
			if err != nil || len(reps) != 0 {
				t.Fatalf("manual workflow bound output: %+v %v", reps, err)
			}
			var links int64
			if err := db.Model(&model.WorkflowStepTask{}).Count(&links).Error; err != nil || links != 0 {
				t.Fatalf("workflow advanced: %d %v", links, err)
			}
		})
	}
}

func TestProjectImageDeleteRechecksDirectUseAfterSnapshot(t *testing.T) {
	for _, purge := range []bool{false, true} {
		t.Run(fmt.Sprint(purge), func(t *testing.T) {
			s, db, p := referenceTestService(t)
			if err := db.AutoMigrate(&model.ResourceDeletionJob{}, &model.CanvasSnapshotResource{}, &model.CanvasSnapshot{}, &model.CanvasProject{}, &model.Announcement{}, &model.AnnouncementImageDraft{}, &model.ArkPrivateAssetBinding{}, &model.ShotAssetReference{}); err != nil {
				t.Fatal(err)
			}
			upload := model.Asset{ID: "upload", UserID: "user-1", PayloadJSON: `{"resourceId":"image-1"}`}
			if err := db.Create(&upload).Error; err != nil {
				t.Fatal(err)
			}
			// Deterministically model the committed creator between the deletion snapshot and row locks.
			snapshot, err := s.repo.OtherAssetResourceReferences("user-1", []string{upload.ID})
			if err != nil {
				t.Fatal(err)
			}
			for _, direct := range snapshot.Direct {
				if direct.ResourceID == "image-1" {
					t.Fatal("fixture already shared")
				}
			}
			asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "direct use", Category: model.AssetCategoryProp, ImageResourceID: "image-1"})
			if err != nil {
				t.Fatal(err)
			}
			resource, err := s.repo.ResourceForUser("user-1", "image-1")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.repo.DeleteAdminResources([]model.Resource{*resource}, nil, []model.AdminAuditEvent{{ID: "audit"}}); !errors.Is(err, repository.ErrAdminResourceStillReferenced) {
				t.Fatalf("admin missed committed direct use: %v", err)
			}
			if err := s.repo.DeleteDetachedResources([]model.Resource{*resource}, nil); !errors.Is(err, repository.ErrResourceCleanupStillReferenced) {
				t.Fatalf("orphan cleanup missed committed direct use: %v", err)
			}
			job := model.ResourceDeletionJob{ID: "delete-job", ResourceID: "image-1", UserID: "user-1", Status: model.ResourceDeletionStatusPending}
			if err := s.repo.DeleteAssetsAndResources("user-1", []string{upload.ID}, []string{"image-1"}, []model.ResourceDeletionJob{job}, purge); err != nil {
				t.Fatal(err)
			}
			if _, err := s.repo.ResourceForUser("user-1", "image-1"); err != nil {
				t.Fatalf("new direct-use resource deleted: %v", err)
			}
			if _, err := s.repo.AssetForUser("user-1", upload.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("upload not deleted: %v", err)
			}
			reps, err := s.repo.AssetRepresentations(asset.PrimaryVersionID)
			if err != nil || len(reps) != 1 {
				t.Fatalf("direct use lost: %+v %v", reps, err)
			}
			var jobs int64
			if err := db.Model(&model.ResourceDeletionJob{}).Count(&jobs).Error; err != nil || jobs != 0 {
				t.Fatalf("shared file queued: %d %v", jobs, err)
			}
		})
	}
}

func TestProjectImageActiveTargetBeforeLimit(t *testing.T) {
	s, db, p := referenceTestService(t)
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "target", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 120; i++ {
		if err := db.Create(&model.Task{ID: fmt.Sprintf("unrelated-active-%03d", i), UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: `{}`, CreatedAt: now.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	inputs := []string{
		`{"metadata":{"operation":"project_asset_image","projectAssetId":"` + asset.ID + `"}}`,
		`{"nodeId":"project-asset-image:` + asset.ID + `"}`,
	}
	for i, input := range inputs {
		if err := db.Create(&model.Task{ID: fmt.Sprintf("target-active-%d", i), UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusQueued, InputJSON: input, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{`{"nodeId":"project-asset-image:` + asset.ID + `-other"}`, `{"metadata":{"operation":"unrelated","projectAssetId":"` + asset.ID + `"}}`} {
		if err := db.Create(&model.Task{ID: newID(), UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: input, CreatedAt: now.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []model.Task{
		{ID: "other-owner-active", UserID: "user-2", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: inputs[1], CreatedAt: now.Add(time.Hour)},
		{ID: "other-project-active", UserID: "user-1", ProjectID: "other-project", Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: inputs[1], CreatedAt: now.Add(time.Hour)},
		{ID: "completed-target", UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusSucceeded, InputJSON: inputs[1], CreatedAt: now.Add(time.Hour)},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	options := TaskListOptions{ProjectID: p, ActiveOnly: true, AssetID: asset.ID, Limit: 100}
	tasks, err := s.TasksWithOptions("user-1", options)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("target active tasks=%+v err=%v", tasks, err)
	}
	if _, err := s.TasksWithOptions("user-2", options); err == nil {
		t.Fatal("foreign owner accepted")
	}
	options.ProjectID = "foreign-project"
	if _, err := s.TasksWithOptions("user-1", options); err == nil {
		t.Fatal("foreign project accepted")
	}
	if err := db.Create(&model.Project{ID: "owned-unlinked-project", UserID: "user-1", Status: model.ProjectStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	options.ProjectID = "owned-unlinked-project"
	if _, err := s.TasksWithOptions("user-1", options); err == nil {
		t.Fatal("unlinked project accepted")
	}
	options.ProjectID = ""
	if _, err := s.TasksWithOptions("user-1", options); err == nil {
		t.Fatal("missing project accepted")
	}
	character, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "active character"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{"metadata":{"operation":"character_turnaround","characterAssetId":"` + character.Asset.ID + `"}}`, `{"nodeId":"character-turnaround:` + character.Asset.ID + `"}`} {
		if err := db.Create(&model.Task{ID: newID(), UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: input, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	options.ProjectID, options.AssetID = p, character.Asset.ID
	if tasks, err := s.TasksWithOptions("user-1", options); err != nil || len(tasks) != 2 {
		t.Fatalf("character target tasks=%+v err=%v", tasks, err)
	}
}

func TestProjectImageManualTasksDoNotStarveAutoReconciliation(t *testing.T) {
	s, db, p := referenceTestService(t)
	detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "character"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		task := seedManualImageCandidate(t, s, db, p, detail.Asset, fmt.Sprintf("manual-%03d", i))
		if err := db.Model(&task).Update("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	task := model.Task{ID: "automatic", UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusSucceeded, InputJSON: `{"metadata":{"operation":"character_turnaround","characterAssetId":"` + detail.Asset.ID + `"}}`, ResultJSON: `{"images":[{"resourceId":"image-1","storageKey":"resource:image-1"}]}`, CreatedAt: time.Now()}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if !s.reconcileCharacterTurnaroundTasks("user-1", p) {
		t.Fatal("100 manual tasks starved automatic reconciliation")
	}
	reps, err := s.repo.AssetRepresentationsForTask(task.ID)
	if err != nil || len(reps) != 2 {
		t.Fatalf("automatic representations=%+v err=%v", reps, err)
	}
}

func TestProjectImageProviderBatchAdmissionUsesShippedProtocol(t *testing.T) {
	s, db, p := referenceTestService(t)
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "target", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../../plugin-packages/volcengine-ark-seedream/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := protocol.LoadManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"mode": "image", "prompt": "test", "config": map[string]any{"interfaceType": "volcengine-ark-image", "model": "doubao-seedream-5-0-260128", "count": "1"}, "metadata": map[string]any{"operation": "project_asset_image", "adoptionMode": "manual", "projectAssetId": asset.ID, "projectAssetVersionId": asset.PrimaryVersionID, "referenceSource": "none", "providerOptions": map[string]any{"volcengine-ark-image": map[string]any{"sequential_image_generation": "auto", "sequential_image_generation_options": map[string]any{"max_images": 4}, "seed": 42}}}}
	build := func() map[string]any {
		raw, _ := json.Marshal(input)
		var typed canvasGenerationInput
		if err := json.Unmarshal(raw, &typed); err != nil {
			t.Fatal(err)
		}
		spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: protocolRequestFromInput(typed)})
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(spec.Body)
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if body := build(); body["sequential_image_generation"] != "auto" {
		t.Fatalf("fixture did not exercise shipped batching: %+v", body)
	}
	_, err = s.CreateTask("user-1", CreateTaskRequest{ProjectID: p, Type: "canvas_image", Prompt: "test", Input: input})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Message != "项目资产仅支持单张输出" {
		t.Fatalf("batch was not rejected by admission: %v", err)
	}
	var count int64
	if err := db.Model(&model.Task{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("queued=%d err=%v", count, err)
	}
	options := input["metadata"].(map[string]any)["providerOptions"].(map[string]any)["volcengine-ark-image"].(map[string]any)
	options["sequential_image_generation"] = "disabled"
	options["sequential_image_generation_options"] = map[string]any{"max_images": 1}
	if err := s.prepareProjectImageTask("user-1", p, "canvas_image", input); err != nil {
		t.Fatal(err)
	}
	if body := build(); body["seed"] != float64(42) || body["sequential_image_generation"] != "disabled" {
		t.Fatalf("safe provider options lost: %+v", body)
	}
}

func TestProjectReferenceCreateAndDirectPortrait(t *testing.T) {
	s, db, p := referenceTestService(t)
	folder := model.ProjectAssetFolder{ID: "folder", ProjectID: p, Name: "角色", NameKey: "角色"}
	if err := db.Create(&folder).Error; err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"", "portrait", "turnaround_sheet"} {
		detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: " 角色 ", FolderID: &folder.ID, ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}, ImageResourceID: "image-1", ImageRole: role})
		if err != nil {
			t.Fatal(err)
		}
		if detail.Asset.FolderID != folder.ID || detail.Asset.ReferenceImage == nil || detail.Asset.ReferenceImage.ResourceID != "image-1" {
			t.Fatalf("missing reference/folder: %+v", detail)
		}
		want := 1
		if role == "turnaround_sheet" {
			want = 2
		}
		if len(detail.Character.Representations) != want {
			t.Fatalf("wrong portrait roles: %+v", detail.Character.Representations)
		}
		if role != "turnaround_sheet" && detail.Character.VisualStatus != "partial" {
			t.Fatal("portrait marked as turnaround")
		}
	}
	for _, category := range []model.AssetCategory{model.AssetCategoryEnvironment, model.AssetCategoryProp} {
		asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "实体", Category: category, FolderID: &folder.ID, ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}, ImageResourceID: "image-1"})
		if err != nil {
			t.Fatal(err)
		}
		if asset.MediaType != "image" || asset.Visual == nil || asset.Visual.Status != "ready" || asset.VersionCount != 1 || asset.FolderID != folder.ID {
			t.Fatalf("bad direct entity: %+v", asset)
		}
	}
	var before int64
	db.Model(&model.Asset{}).Count(&before)
	for _, req := range []CreateProjectAssetEntityRequest{
		{Name: " ", Category: model.AssetCategoryProp},
		{Name: "x", Category: model.AssetCategoryCharacter},
		{Name: "x", Category: model.AssetCategoryProp, ReferenceImage: &ProjectReferenceImage{ResourceID: "foreign-image"}},
		{Name: "x", Category: model.AssetCategoryProp, ReferenceImage: &ProjectReferenceImage{ResourceID: "pending-image"}},
		{Name: "x", Category: model.AssetCategoryProp, ReferenceImage: &ProjectReferenceImage{ResourceID: "video-1"}},
		{Name: "x", Category: model.AssetCategoryProp, Definition: map[string]any{"referenceImage": map[string]any{"resourceId": "foreign-image"}}},
	} {
		if _, err := s.CreateProjectAssetEntity("user-1", p, req); err == nil {
			t.Fatalf("invalid create accepted: %+v", req)
		}
	}
	badFolder := "not-project-folder"
	if _, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "x", FolderID: &badFolder}); err == nil {
		t.Fatal("invalid folder accepted")
	}
	var after int64
	db.Model(&model.Asset{}).Count(&after)
	if after != before {
		t.Fatal("failed creation left assets")
	}
}

func TestProjectReferenceCreateTransactionRollback(t *testing.T) {
	for _, category := range []model.AssetCategory{model.AssetCategoryCharacter, model.AssetCategoryEnvironment, model.AssetCategoryProp} {
		t.Run(string(category), func(t *testing.T) {
			s, db, p := referenceTestService(t)
			tables := []string{"assets", "asset_versions", "asset_representations", "project_asset_links"}
			before := map[string]int64{}
			for _, table := range tables {
				var count int64
				if err := db.Table(table).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				before[table] = count
			}
			project, err := s.repo.ProjectForUser("user-1", p)
			if err != nil {
				t.Fatal(err)
			}
			// Linking is after asset/version/representation inserts in the creation transaction.
			if err := db.Exec("CREATE TRIGGER fail_entity_link BEFORE INSERT ON project_asset_links BEGIN SELECT RAISE(ABORT, 'injected creation link failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			if category == model.AssetCategoryCharacter {
				_, err = s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "rollback", ImageResourceID: "image-1", ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}})
			} else {
				_, err = s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "rollback", Category: category, ImageResourceID: "image-1", ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}})
			}
			if err == nil || !strings.Contains(err.Error(), "injected creation link failure") {
				t.Fatalf("did not reach injected failure: %v", err)
			}
			for _, table := range tables {
				var count int64
				if err := db.Table(table).Count(&count).Error; err != nil || count != before[table] {
					t.Fatalf("%s rollback count=%d want=%d err=%v", table, count, before[table], err)
				}
			}
			after, err := s.repo.ProjectForUser("user-1", p)
			if err != nil || after.Revision != project.Revision {
				t.Fatalf("project revision changed: %+v %v", after, err)
			}
			if _, err := s.repo.ResourceForUser("user-1", "image-1"); err != nil {
				t.Fatal("creation rollback removed uploaded resource", err)
			}
		})
	}
}

func TestProjectReferenceRejectsInvalidMIMEAndDimensions(t *testing.T) {
	s, db, p := referenceTestService(t)
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "target", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []model.Resource{
		{ID: "bad-mime", MimeType: "application/pdf", Width: 100, Height: 100},
		{ID: "zero-width", MimeType: "image/png", Width: 0, Height: 100},
		{ID: "negative-height", MimeType: "image/png", Width: 100, Height: -1},
	} {
		t.Run(resource.ID, func(t *testing.T) {
			resource.UserID, resource.Kind, resource.Status = "user-1", "image", model.ResourceStatusReady
			if err := db.Create(&resource).Error; err != nil {
				t.Fatal(err)
			}
			_, err := s.SetProjectReferenceImage("user-1", p, asset.ID, SetProjectReferenceImageRequest{ResourceID: resource.ID, ExpectedVersionID: asset.PrimaryVersionID})
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != 400 {
				t.Fatalf("invalid image not explicitly rejected: %v", err)
			}
			versions, err := s.repo.AssetVersions(asset.ID)
			if err != nil || len(versions) != 1 {
				t.Fatalf("invalid reference changed versions: %+v %v", versions, err)
			}
		})
	}
}

func TestProjectReferencePreservesVersionsVoiceAndCAS(t *testing.T) {
	s, db, p := referenceTestService(t)
	detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "角色", ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}, ImageResourceID: "image-1"})
	if err != nil {
		t.Fatal(err)
	}
	profile := model.VoiceProfile{ID: "voice", UserID: "user-1", Status: "active"}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.CharacterVoiceBinding{ID: "binding", AssetVersionID: detail.Asset.PrimaryVersionID, VoiceProfileID: profile.ID}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	old := detail.Asset.PrimaryVersionID
	updated, err := s.UpdateProjectCharacter("user-1", p, detail.Asset.ID, UpdateProjectCharacterRequest{Name: "新名称", Definition: map[string]any{"description": "新设定"}, ExpectedVersionID: old})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Asset.ReferenceImage == nil || updated.Character.Voice == nil || len(updated.Character.Representations) != 1 {
		t.Fatalf("protected data lost: %+v", updated)
	}
	_, err = s.UpdateProjectCharacter("user-1", p, detail.Asset.ID, UpdateProjectCharacterRequest{Name: "旧修改", ExpectedVersionID: old})
	requireVersionConflict(t, err)
	_, err = s.ReplaceProjectCharacterRepresentations("user-1", p, detail.Asset.ID, ReplaceCharacterRepresentationsRequest{ExpectedVersionID: old, Representations: []CharacterRepresentationInput{{Role: "primary", ResourceID: "image-1"}}})
	requireVersionConflict(t, err)
	_, err = s.UpdateProjectCharacter("user-1", p, detail.Asset.ID, UpdateProjectCharacterRequest{Name: "非法", Definition: map[string]any{"referenceImage": nil}})
	if err == nil {
		t.Fatal("reference cleared through definition")
	}
	second := model.Resource{ID: "original-2", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 100, Height: 100}
	db.Create(&second)
	changed, err := s.SetProjectReferenceImage("user-1", p, detail.Asset.ID, SetProjectReferenceImageRequest{ResourceID: second.ID, ExpectedVersionID: updated.Asset.PrimaryVersionID})
	if err != nil {
		t.Fatal(err)
	}
	if changed.ReferenceImage.ResourceID != second.ID || changed.Character.Representations[0].ResourceID != "image-1" || changed.Character.Voice == nil {
		t.Fatalf("reference replaced output: %+v", changed)
	}
	previous, err := s.repo.AssetVersion(old)
	if err != nil || referenceImageFromDefinition(previous.DefinitionJSON).ResourceID != "image-1" {
		t.Fatal("historical original lost")
	}
	if err := s.repo.RequireNoOriginalImageReferences([]string{"image-1"}); !errors.Is(err, repository.ErrOriginalImageReferenced) {
		t.Fatalf("historical reference not protected: %v", err)
	}
	_, err = s.SetProjectReferenceImage("user-1", p, detail.Asset.ID, SetProjectReferenceImageRequest{ResourceID: "image-1", ExpectedVersionID: old})
	requireVersionConflict(t, err)
	for _, req := range []SetProjectReferenceImageRequest{{ResourceID: "", ExpectedVersionID: changed.PrimaryVersionID}, {ResourceID: "foreign-image", ExpectedVersionID: changed.PrimaryVersionID}, {ResourceID: "image-1"}} {
		if _, err := s.SetProjectReferenceImage("user-1", p, detail.Asset.ID, req); err == nil {
			t.Fatal("invalid reference update accepted")
		}
	}
}

func seedManualImageCandidate(t *testing.T, s *Service, db *gorm.DB, p string, asset ProjectAssetSummary, id string) model.Task {
	t.Helper()
	operation, target := "project_asset_image", "projectAssetId"
	if asset.Category == model.AssetCategoryCharacter {
		operation, target = "character_turnaround", "characterAssetId"
	}
	input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": operation, "adoptionMode": "manual", "projectAssetVersionId": asset.PrimaryVersionID, target: asset.ID, "referenceSource": "none"}})
	task := model.Task{ID: id, UserID: "user-1", ProjectID: p, Type: "canvas_image", Status: model.TaskStatusSucceeded, InputJSON: string(input), CreatedAt: time.Now()}
	result := map[string]interface{}{"images": []any{map[string]any{"resourceId": "image-1", "storageKey": "resource:image-1"}}}
	if _, err := s.finalizeCharacterTurnaroundTask(task, result); err != nil {
		t.Fatal(err)
	}
	if _, err := s.finalizeProjectAssetImageTask(task, result); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	task.ResultJSON = string(raw)
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	return task
}

func TestProjectImageCandidateManualAdoptionAndRecovery(t *testing.T) {
	for _, category := range []model.AssetCategory{model.AssetCategoryCharacter, model.AssetCategoryEnvironment, model.AssetCategoryProp} {
		t.Run(string(category), func(t *testing.T) {
			s, db, p := referenceTestService(t)
			var asset ProjectAssetSummary
			if category == model.AssetCategoryCharacter {
				detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "人物", ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}})
				if err != nil {
					t.Fatal(err)
				}
				asset = detail.Asset
				if err := db.Create(&model.VoiceProfile{ID: "adoption-voice", UserID: "user-1", Status: "active"}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.CharacterVoiceBinding{ID: "adoption-binding", AssetVersionID: asset.PrimaryVersionID, VoiceProfileID: "adoption-voice"}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				value, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "实体", Category: category, ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}})
				if err != nil {
					t.Fatal(err)
				}
				asset = value
			}
			task := seedManualImageCandidate(t, s, db, p, asset, "candidate-one")
			other := seedManualImageCandidate(t, s, db, p, asset, "candidate-two")
			if s.reconcileCharacterTurnaroundTasks("user-1", p) {
				t.Fatal("read recovery adopted manual task")
			}
			current, _ := s.repo.AssetForUser("user-1", asset.ID)
			if current.PrimaryVersionID != asset.PrimaryVersionID {
				t.Fatal("manual completion changed asset")
			}
			adopted, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID})
			if err != nil {
				t.Fatal(err)
			}
			if adopted.VersionCount != 2 || adopted.ReferenceImage.ResourceID != "image-1" {
				t.Fatalf("bad adoption: %+v", adopted)
			}
			replay, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID})
			if err != nil || replay.PrimaryVersionID != adopted.PrimaryVersionID || replay.VersionCount != 2 {
				t.Fatalf("replay: %+v %v", replay, err)
			}
			_, err = s.AdoptProjectImageCandidate("user-1", p, asset.ID, other.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: adopted.PrimaryVersionID})
			requireVersionConflict(t, err)
			persisted, _ := s.repo.Task(task.ID)
			candidate := candidateFromResult(persisted.ResultJSON)
			if candidate.State != "adopted" || candidate.AdoptedVersionID != adopted.PrimaryVersionID {
				t.Fatalf("candidate not persisted: %+v", candidate)
			}
			if category == model.AssetCategoryCharacter {
				if adopted.Character.VisualStatus != "ready" {
					t.Fatal("generated turnaround incomplete")
				}
				for _, versionID := range []string{asset.PrimaryVersionID, adopted.PrimaryVersionID} {
					voice, err := s.repo.CharacterVoiceBinding(versionID)
					if err != nil || voice.VoiceProfileID != "adoption-voice" {
						t.Fatalf("adoption lost voice on %s: %+v %v", versionID, voice, err)
					}
				}
				if adopted.Character.Voice == nil {
					t.Fatal("adopted summary lost voice")
				}
			}
		})
	}
}

func TestProjectImageCandidateRejectsInvalidAdoptionAndRollsBack(t *testing.T) {
	s, db, p := referenceTestService(t)
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "道具", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	task := seedManualImageCandidate(t, s, db, p, asset, "candidate")
	for _, tc := range []struct{ user, project, asset, task, version string }{{"user-2", p, asset.ID, task.ID, asset.PrimaryVersionID}, {"user-1", "foreign", asset.ID, task.ID, asset.PrimaryVersionID}, {"user-1", p, asset.ID, task.ID, ""}, {"user-1", p, asset.ID, "missing", asset.PrimaryVersionID}} {
		if _, err := s.AdoptProjectImageCandidate(tc.user, tc.project, tc.asset, tc.task, AdoptProjectImageCandidateRequest{ExpectedVersionID: tc.version}); err == nil {
			t.Fatal("invalid adoption accepted")
		}
	}
	db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailed)
	if _, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID}); err == nil {
		t.Fatal("failed task adopted")
	}
	db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusSucceeded)
	if err := db.Exec("CREATE TRIGGER fail_candidate_commit BEFORE UPDATE OF result_json ON tasks BEGIN SELECT RAISE(ABORT, 'injected candidate write failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID}); err == nil {
		t.Fatal("injected failure accepted")
	}
	versions, _ := s.repo.AssetVersions(asset.ID)
	if len(versions) != 1 {
		t.Fatalf("rollback left %d versions", len(versions))
	}
	reps, _ := s.repo.AssetRepresentationsForTask(task.ID)
	if len(reps) != 0 {
		t.Fatal("rollback left representations")
	}
}

func TestProjectImageCandidatePaginationBeyondRecentTasks(t *testing.T) {
	s, db, p := referenceTestService(t)
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "道具", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 125; i++ {
		candidate := AssetImageCandidate{AssetID: asset.ID, BaseVersionID: asset.PrimaryVersionID, State: "pending_confirmation"}
		result, _ := json.Marshal(map[string]any{"assetImageCandidate": candidate})
		task := model.Task{ID: fmt.Sprintf("candidate-%03d", i), UserID: "user-1", ProjectID: p, Status: model.TaskStatusSucceeded, Type: "canvas_image", InputJSON: `{"metadata":{"adoptionMode":"manual","operation":"project_asset_image"}}`, ResultJSON: string(result), CreatedAt: now}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 120; i++ {
		db.Create(&model.Task{ID: fmt.Sprintf("unrelated-%03d", i), UserID: "user-1", ProjectID: p, Status: model.TaskStatusSucceeded, CreatedAt: now.Add(time.Hour), ResultJSON: "{}"})
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := s.TasksWithOptions("user-1", TaskListOptions{ProjectID: p, AssetID: asset.ID, AssetImageCandidates: true, Limit: 50, BeforeTaskID: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range page {
			if seen[task.ID] || task.AssetImageCandidate == nil {
				t.Fatal("duplicate or missing candidate")
			}
			seen[task.ID] = true
		}
		if len(page) < 50 {
			break
		}
		cursor = page[len(page)-1].ID
	}
	if len(seen) != 125 {
		t.Fatalf("got %d candidates", len(seen))
	}
	for _, opts := range []TaskListOptions{{ProjectID: p, AssetImageCandidates: true, Limit: 50, BeforeTaskID: "unrelated-001"}, {ProjectID: p, AssetImageCandidates: true, ActiveOnly: true, Limit: 50}, {AssetImageCandidates: true, Limit: 50}} {
		if _, err := s.TasksWithOptions("user-1", opts); err == nil {
			t.Fatal("invalid candidate filter accepted")
		}
	}
}

func TestProjectImageCandidateForeignCursorAndAccountIsolation(t *testing.T) {
	s, db, p := referenceTestService(t)
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "target", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	own := seedManualImageCandidate(t, s, db, p, asset, "own-candidate")
	foreign := own
	foreign.ID, foreign.UserID = "foreign-candidate", "user-2"
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	options := TaskListOptions{ProjectID: p, AssetID: asset.ID, AssetImageCandidates: true, Limit: 50}
	page, err := s.TasksWithOptions("user-1", options)
	if err != nil || len(page) != 1 || page[0].ID != own.ID {
		t.Fatalf("account isolation failed: %+v %v", page, err)
	}
	options.BeforeTaskID = foreign.ID
	if page, err := s.TasksWithOptions("user-1", options); err == nil || len(page) != 0 {
		t.Fatalf("foreign cursor accepted: %+v %v", page, err)
	}
	options.BeforeTaskID = ""
	if page, err := s.TasksWithOptions("user-2", options); err == nil || len(page) != 0 {
		t.Fatalf("foreign project exposed: %+v %v", page, err)
	}
	if _, err := s.Task("user-2", own.ID); err == nil {
		t.Fatal("foreign candidate detail exposed")
	}
}

func TestProjectImageCandidateRejectsTargetAndForgedResource(t *testing.T) {
	for _, scenario := range []string{"target", "candidate-target", "foreign-resource", "missing-resource", "pending-resource"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, p := referenceTestService(t)
			asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "target", Category: model.AssetCategoryProp})
			if err != nil {
				t.Fatal(err)
			}
			other, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "other", Category: model.AssetCategoryProp})
			if err != nil {
				t.Fatal(err)
			}
			task := seedManualImageCandidate(t, s, db, p, asset, "candidate")
			target := asset
			var result map[string]any
			if err := json.Unmarshal([]byte(task.ResultJSON), &result); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "target":
				target = other
			case "candidate-target":
				result["assetImageCandidate"].(map[string]any)["assetId"] = other.ID
			default:
				resourceID := map[string]string{"foreign-resource": "foreign-image", "missing-resource": "missing", "pending-resource": "pending-image"}[scenario]
				result["images"] = []any{map[string]any{"resourceId": resourceID, "storageKey": "resource:" + resourceID}}
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&task).Update("result_json", string(raw)).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.AdoptProjectImageCandidate("user-1", p, target.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: target.PrimaryVersionID}); err == nil {
				t.Fatal("invalid candidate adopted")
			}
			for _, original := range []ProjectAssetSummary{asset, other} {
				versions, err := s.repo.AssetVersions(original.ID)
				if err != nil || len(versions) != 1 {
					t.Fatalf("rejection changed versions: %+v %v", versions, err)
				}
				current, err := s.repo.AssetForUser("user-1", original.ID)
				if err != nil || current.PrimaryVersionID != original.PrimaryVersionID {
					t.Fatalf("rejection changed primary: %+v %v", current, err)
				}
			}
			reps, err := s.repo.AssetRepresentationsForTask(task.ID)
			if err != nil || len(reps) != 0 {
				t.Fatalf("rejection left bindings: %+v %v", reps, err)
			}
		})
	}
}

func TestProjectImageCandidateCompletionRespectsLease(t *testing.T) {
	for _, category := range []model.AssetCategory{model.AssetCategoryCharacter, model.AssetCategoryProp} {
		for _, state := range []string{"running", "cancelled", "lost-lease"} {
			t.Run(string(category)+state, func(t *testing.T) {
				s, db, p := referenceTestService(t)
				if err := db.AutoMigrate(&model.SystemSetting{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}); err != nil {
					t.Fatal(err)
				}
				var asset ProjectAssetSummary
				if category == model.AssetCategoryCharacter {
					d, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "人物"})
					if err != nil {
						t.Fatal(err)
					}
					asset = d.Asset
				} else {
					d, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "道具", Category: category})
					if err != nil {
						t.Fatal(err)
					}
					asset = d
				}
				task := seedManualImageCandidate(t, s, db, p, asset, "completion-task")
				task.Status = model.TaskStatusRunning
				task.ResultJSON = ""
				stored := task
				if state == "cancelled" {
					stored.Status = model.TaskStatusCancelled
				}
				if state == "lost-lease" {
					stored.LeaseOwner = "other-worker"
				}
				if err := db.Save(&stored).Error; err != nil {
					t.Fatal(err)
				}
				err := s.saveTaskCompletionWithinStorageQuota(&task, []byte(`{"images":[{"resourceId":"image-1"}]}`), nil, false)
				if state == "running" {
					if err != nil {
						t.Fatal(err)
					}
					saved, _ := s.repo.Task(task.ID)
					if saved.Status != model.TaskStatusSucceeded || candidateFromResult(saved.ResultJSON) == nil {
						t.Fatal("candidate not atomically saved")
					}
				} else if !errors.Is(err, repository.ErrTaskStateConflict) {
					t.Fatalf("expected state conflict: %v", err)
				}
				current, _ := s.repo.AssetForUser("user-1", asset.ID)
				if current.PrimaryVersionID != asset.PrimaryVersionID {
					t.Fatal("completion adopted candidate")
				}
			})
		}
	}
}

func TestProjectReferenceDeleteGuardAndPreparedEditCAS(t *testing.T) {
	s, db, p := referenceTestService(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "角色", ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}})
	if err != nil {
		t.Fatal(err)
	}
	asset, _ := s.repo.AssetForUser("user-1", detail.Asset.ID)
	nextAsset, next, reps, voice, err := s.prepareNextCharacterVersion(asset, "过期编辑", "", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	task := seedManualImageCandidate(t, s, db, p, detail.Asset, "race-candidate")
	if _, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.SaveCharacterVersion(p, &nextAsset, &next, reps, voice, asset.PrimaryVersionID); !errors.Is(err, repository.ErrProjectAssetVersionChanged) {
		t.Fatalf("prepared edit overwrote adoption: %v", err)
	}
	resource, _ := s.repo.ResourceForUser("user-1", "image-1")
	if err := s.repo.DeleteDetachedResources([]model.Resource{*resource}, nil); !errors.Is(err, repository.ErrOriginalImageReferenced) {
		t.Fatalf("cleanup bypassed original: %v", err)
	}
	if err := s.repo.DeleteAssetsAndResources("user-1", []string{asset.ID}, []string{resource.ID}, nil, true); !errors.Is(err, repository.ErrOriginalImageReferenced) {
		t.Fatalf("purge bypassed original: %v", err)
	}
	if _, err := s.repo.ResourceForUser("user-1", resource.ID); err != nil {
		t.Fatal("guard deleted resource")
	}
}

func TestProjectImageTaskReferenceSourcesAndValidation(t *testing.T) {
	s, db, p := referenceTestService(t)
	db.Create(&model.Resource{ID: "current-image", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 100, Height: 100})
	asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "场景", Category: model.AssetCategoryEnvironment, ReferenceImage: &ProjectReferenceImage{ResourceID: "image-1"}, ImageResourceID: "current-image"})
	if err != nil {
		t.Fatal(err)
	}
	makeInput := func(source string) map[string]any {
		return map[string]any{"mode": "image", "config": map[string]any{"interfaceType": "openai-image", "model": "gpt-image-1", "count": "1"}, "metadata": map[string]any{"operation": "project_asset_image", "adoptionMode": "manual", "projectAssetId": asset.ID, "projectAssetVersionId": asset.PrimaryVersionID, "referenceSource": source}}
	}
	for _, source := range []string{"original", "current", "none"} {
		input := makeInput(source)
		if err := s.prepareProjectImageTask("user-1", p, "canvas_image", input); err != nil {
			t.Fatal(err)
		}
		refs := input["referenceImages"].([]any)
		if source == "none" {
			if len(refs) != 0 {
				t.Fatal("fabricated reference")
			}
		} else {
			want := "resource:image-1"
			if source == "current" {
				want = "resource:current-image"
			}
			if refs[0].(map[string]any)["storageKey"] != want {
				t.Fatal("wrong reference source")
			}
		}
	}
	for _, mutate := range []func(map[string]any){func(v map[string]any) { v["metadata"].(map[string]any)["adoptionMode"] = "typo" }, func(v map[string]any) { v["metadata"].(map[string]any)["referenceSource"] = "unknown" }, func(v map[string]any) { v["config"].(map[string]any)["count"] = "2" }, func(v map[string]any) { v["referenceImages"] = []any{map[string]any{}, map[string]any{}} }, func(v map[string]any) {
		v["referenceImages"] = []any{map[string]any{"storageKey": "resource:foreign-image"}}
	}} {
		input := makeInput("original")
		mutate(input)
		if err := s.prepareProjectImageTask("user-1", p, "canvas_image", input); err == nil {
			t.Fatal("invalid task input accepted")
		}
	}
	input := makeInput("original")
	if err := s.prepareProjectImageTask("user-1", p, "canvas_image", input); err != nil {
		t.Fatal(err)
	}
	input["config"].(map[string]any)["interfaceType"] = "unknown"
	if err := s.validateProjectImageReferenceCapability(input); err == nil {
		t.Fatal("unknown reference capability accepted")
	}
	input["config"].(map[string]any)["interfaceType"] = "openai-image"
	input["config"].(map[string]any)["model"] = "grok-imagine-image"
	if err := s.validateProjectImageReferenceCapability(input); err == nil {
		t.Fatal("text only capability accepted")
	}
}
