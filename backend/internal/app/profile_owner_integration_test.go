package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// File WAL with independent observer pool: rollback checks never read the writer's
// transaction or rely on a shared in-memory connection.
func profileOwnerFixture(t *testing.T, dialect string, category model.AssetCategory) (*Service, *gorm.DB, *gorm.DB, model.Project, *model.Asset) {
	t.Helper()
	dbs := profileConcurrencyDatabases(t, dialect)
	db := dbs[0]
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.CanvasProject{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}, &model.ApiCallLog{}, &model.VoiceProfile{}); err != nil {
		t.Fatal(err)
	}
	s := &Service{repo: repository.New(db)}
	project := model.Project{ID: newID(), UserID: "owner-user", Name: "owner transaction", Status: model.ProjectStatusActive}
	resource := model.Resource{ID: "owner-image", UserID: project.UserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 32, Height: 32, Size: 100}
	for _, row := range []any{&project, &resource} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	asset, err := s.createProjectImageEntity(project.UserID, project.ID, category, "owner asset", map[string]any{"description": "old", "appearance": "old"}, &ProjectReferenceImage{ResourceID: resource.ID}, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.First(&project, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	return s, db, dbs[1], project, asset
}

func profileOwnerFaults() []struct {
	name string
	err  error
} {
	return []struct {
		name string
		err  error
	}{
		{"busy", sqlite3.Error{Code: sqlite3.ErrBusy}},
		{"busy_snapshot", sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: sqlite3.ErrBusySnapshot}},
		{"locked", sqlite3.Error{Code: sqlite3.ErrLocked}},
		{"serialization", &pgconn.PgError{Code: "40001"}},
		{"deadlock", &pgconn.PgError{Code: "40P01"}},
	}
}

func profileOwnerObserve(t *testing.T, db *gorm.DB, project model.Project, asset *model.Asset, task model.Task, versions, reps int64, succeeded bool) {
	t.Helper()
	var actual model.Asset
	if err := db.First(&actual, "id = ?", asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.AssetVersion{}).Where("asset_id = ?", asset.ID).Count(&count).Error; err != nil || count != versions {
		t.Fatalf("versions=%d want=%d err=%v", count, versions, err)
	}
	if err := db.Model(&model.AssetRepresentation{}).Where("task_id = ?", task.ID).Count(&count).Error; err != nil || count != reps {
		t.Fatalf("representations=%d want=%d err=%v", count, reps, err)
	}
	var p model.Project
	if err := db.First(&p, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	delta := int64(0)
	if succeeded {
		delta = 1
	}
	if p.Revision != project.Revision+delta {
		t.Fatalf("revision=%d want=%d", p.Revision, project.Revision+delta)
	}
	if !succeeded && (actual.PrimaryVersionID != asset.PrimaryVersionID || actual.PayloadJSON != asset.PayloadJSON || stored.Status != task.Status || stored.ResultJSON != task.ResultJSON) {
		t.Fatalf("rollback leaked asset/task: asset=%+v task=%+v", actual, stored)
	}
	if succeeded && (actual.PrimaryVersionID == asset.PrimaryVersionID || stored.Status != model.TaskStatusSucceeded) {
		t.Fatalf("success not committed: asset=%+v task=%+v", actual, stored)
	}
}

func TestProfileOwnerAdoptFullTransactionRetry(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) { profileOwnerAdoptFullTransactionRetry(t, dialect) })
	}
}

func profileOwnerAdoptFullTransactionRetry(t *testing.T, dialect string) {
	for _, fault := range profileOwnerFaults() {
		t.Run(fault.name, func(t *testing.T) {
			s, db, observer, p, asset := profileOwnerFixture(t, dialect, model.AssetCategoryProp)
			binding := ProfileAnalysisBinding{1, p.ID, asset.ID, asset.Category, asset.PrimaryVersionID, ProjectReferenceImage{"owner-image"}, 1}
			input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}})
			result, _ := json.Marshal(map[string]any{"mode": "text", "assetProfileAnalysis": ProfileAnalysisResult{SchemaVersion: 1, AssetID: asset.ID, Category: asset.Category, BaseVersionID: asset.PrimaryVersionID, Source: binding.Source, Draft: map[string]string{"description": "new", "prompt": ""}}})
			task := model.Task{ID: newID(), UserID: p.UserID, ProjectID: p.ID, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusSucceeded, InputJSON: string(input), ResultJSON: string(result), CreatedAt: time.Now()}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			ownerCalls, updates := 0, 0
			if err := db.Callback().Raw().Before("gorm:raw").Register("profile_owner_attempt", func(tx *gorm.DB) {
				if strings.Contains(tx.Statement.SQL.String(), "UPDATE assets SET id = id WHERE 1 = 0") || strings.Contains(tx.Statement.SQL.String(), "SET LOCAL lock_timeout") {
					ownerCalls++
					if ownerCalls > 1 {
						profileOwnerObserve(t, observer, p, asset, task, 1, 0, false)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().After("gorm:update").Register("profile_owner_adopt_fault", func(tx *gorm.DB) {
				if tx.Error == nil && tx.Statement.Table == "tasks" && strings.Contains(tx.Statement.SQL.String(), "result_json") {
					updates++
					if updates == 1 {
						tx.AddError(fault.err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			out, err := s.AdoptProjectProfileAnalysis(p.UserID, p.ID, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "owner-retry-key-0001"})
			if err != nil {
				t.Fatalf("full owner transaction did not retry: calls=%d updates=%d err=%v", ownerCalls, updates, err)
			}
			if ownerCalls != 2 || updates != 2 || out.Replayed {
				t.Fatalf("retry scope calls=%d updates=%d response=%+v", ownerCalls, updates, out)
			}
			profileOwnerObserve(t, observer, p, asset, task, 2, 0, true)
			stored, err := s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			verified, err := validatedProfileResult(*stored)
			if err != nil || verified.Adoption == nil || verified.Adoption.AdoptedVersionID != out.AdoptedVersionID {
				t.Fatalf("receipt lost: %+v %v", verified, err)
			}
		})
	}
}

func TestProfileOwnerCompletionCallbackAtomic(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) { profileOwnerCompletionCallbackAtomic(t, dialect) })
	}
}

func profileOwnerCompletionCallbackAtomic(t *testing.T, dialect string) {
	for _, operation := range []string{"project_asset_image", "character_turnaround"} {
		for _, mode := range []string{"success", "rollback", "retry"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				category := model.AssetCategoryProp
				if operation == "character_turnaround" {
					category = model.AssetCategoryCharacter
				}
				s, db, observer, p, asset := profileOwnerFixture(t, dialect, category)
				metadata := map[string]any{"operation": operation, "projectAssetId": asset.ID, "projectAssetVersionId": asset.PrimaryVersionID, "characterAssetId": asset.ID}
				input, _ := json.Marshal(map[string]any{"metadata": metadata})
				task := model.Task{ID: newID(), UserID: p.UserID, ProjectID: p.ID, Type: "canvas_image", Status: model.TaskStatusRunning, InputJSON: string(input), LeaseOwner: "worker", LeaseExpiresAt: ptr(time.Now().Add(time.Minute))}
				if err := db.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
				original := task
				ownerCalls, versionWrites, faults := 0, 0, 0
				failure := errors.New("owner completion fault")
				if err := db.Callback().Raw().Before("gorm:raw").Register("profile_owner_completion_attempt", func(tx *gorm.DB) {
					if strings.Contains(tx.Statement.SQL.String(), "UPDATE assets SET id = id WHERE 1 = 0") || strings.Contains(tx.Statement.SQL.String(), "SET LOCAL lock_timeout") {
						ownerCalls++
						if ownerCalls > 1 {
							profileOwnerObserve(t, observer, p, asset, original, 1, 0, false)
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				// Fail after the real registration has written the version and representations,
				// not by substituting a direct registration call for the completion callback.
				if err := db.Callback().Create().After("gorm:create").Register("profile_owner_completion_fault", func(tx *gorm.DB) {
					if tx.Error == nil && tx.Statement.Table == "asset_versions" {
						versionWrites++
					}
					if tx.Error == nil && tx.Statement.Table == "asset_representations" && operation == "character_turnaround" && mode != "success" {
						faults++
						if mode == "rollback" {
							tx.AddError(failure)
						} else if faults == 1 {
							tx.AddError(sqlite3.Error{Code: sqlite3.ErrBusy})
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				if err := db.Callback().Update().After("gorm:update").Register("profile_owner_completion_result_fault", func(tx *gorm.DB) {
					values, mapUpdate := tx.Statement.Dest.(map[string]interface{})
					encoded, _ := values["result_json"].(string)
					if tx.Error == nil && mapUpdate && operation == "project_asset_image" && mode != "success" && tx.Statement.Table == "tasks" && strings.Contains(encoded, "projectAssetImageStatus") {
						faults++
						if mode == "rollback" {
							tx.AddError(failure)
						} else if faults == 1 {
							tx.AddError(sqlite3.Error{Code: sqlite3.ErrBusy})
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				err := s.saveTaskCompletionWithinStorageQuota(&task, []byte(`{"images":[{"resourceId":"owner-image"}]}`), nil, false)
				if mode == "rollback" {
					if !errors.Is(err, failure) || faults != 1 {
						t.Fatalf("wrong failure: calls=%d faults=%d err=%v", ownerCalls, faults, err)
					}
					profileOwnerObserve(t, observer, p, asset, original, 1, 0, false)
					if task.Status != original.Status || task.ResultJSON != original.ResultJSON {
						t.Fatal("failed completion published in-memory success")
					}
					return
				}
				if err != nil {
					t.Fatalf("real completion callback failed: calls=%d versions=%d faults=%d err=%v", ownerCalls, versionWrites, faults, err)
				}
				expected := 1
				if mode == "retry" {
					expected = 2
				}
				if ownerCalls != expected || versionWrites != expected {
					t.Fatalf("not whole outer retry: owners=%d versions=%d want=%d", ownerCalls, versionWrites, expected)
				}
				reps := int64(1)
				if operation == "character_turnaround" {
					reps = 2
				}
				profileOwnerObserve(t, observer, p, asset, original, 2, reps, true)
				stored, err := s.repo.Task(task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if task.ResultJSON != stored.ResultJSON || task.Status != model.TaskStatusSucceeded {
					t.Fatal("memory/database terminal state diverged")
				}
				if operation == "project_asset_image" && !strings.Contains(stored.ResultJSON, `"projectAssetImageStatus":"bound"`) {
					t.Fatal("registration result lost on retry")
				}
				t.Logf("真实完成回调 %s/%s owner=%d versionWrites=%d", operation, mode, ownerCalls, versionWrites)
			})
		}
	}
}

func TestProfileOwnerAdoptRetryExhaustion(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) { profileOwnerAdoptRetryExhaustion(t, dialect) })
	}
}

func profileOwnerAdoptRetryExhaustion(t *testing.T, dialect string) {
	s, db, observer, p, asset := profileOwnerFixture(t, dialect, model.AssetCategoryProp)
	task := model.Task{ID: newID(), UserID: p.UserID, ProjectID: p.ID, Status: model.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := s.repo.WithStoryboardAssetWrite(p.ID, asset.ID, func(tx *repository.Repository) error {
		calls++
		if err := tx.BumpProjectRevision(p.ID); err != nil {
			return err
		}
		return fmt.Errorf("write fault: %w", sqlite3.Error{Code: sqlite3.ErrBusy})
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != 503 || !appErr.Retryable || calls < 2 || calls > 4 {
		t.Fatalf("retry exhaustion: calls=%d err=%v", calls, err)
	}
	profileOwnerObserve(t, observer, p, asset, task, 1, 0, false)
}
