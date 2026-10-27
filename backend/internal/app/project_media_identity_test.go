package app

import (
	"encoding/json"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestMediaIdentityConcurrentRegistrationSQLiteWAL(t *testing.T) {
	for _, mode := range []string{"candidate", "visual", "character"} {
		t.Run(mode, func(t *testing.T) {
			s, db, p := referenceTestService(t)
			var asset ProjectAssetSummary
			if mode == "character" {
				detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "character"})
				if err != nil {
					t.Fatal(err)
				}
				asset = detail.Asset
			} else {
				var err error
				asset, err = s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "prop", Category: model.AssetCategoryProp})
				if err != nil {
					t.Fatal(err)
				}
			}
			var task model.Task
			if mode == "candidate" {
				task = seedManualImageCandidate(t, s, db, p, asset, "candidate")
			}
			file := filepath.Join(t.TempDir(), "registration.db")
			if err := db.Exec("VACUUM INTO '" + strings.ReplaceAll(filepath.ToSlash(file), "'", "''") + "'").Error; err != nil {
				t.Fatal(err)
			}
			services := make([]*Service, 2)
			for i := range services {
				conn, err := gorm.Open(sqlite.Open(filepath.ToSlash(file)+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{})
				if err != nil {
					t.Fatal(err)
				}
				sqlDB, _ := conn.DB()
				sqlDB.SetMaxOpenConns(1)
				t.Cleanup(func() { sqlDB.Close() })
				var journal string
				if err := conn.Raw("PRAGMA journal_mode").Scan(&journal).Error; err != nil || journal != "wal" {
					t.Fatalf("not WAL: %s %v", journal, err)
				}
				services[i] = &Service{repo: repository.New(conn)}
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			for _, service := range services {
				wg.Add(1)
				go func(s *Service) {
					defer wg.Done()
					<-start
					var err error
					switch mode {
					case "candidate":
						_, err = s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID})
					case "visual":
						_, err = s.bindProjectAssetImage("user-1", p, asset.ID, BindProjectAssetImageRequest{ResourceID: "image-1", ExpectedVersionID: asset.PrimaryVersionID}, "automatic")
					case "character":
						_, err = s.finalizeCharacterTurnaroundTask(model.Task{ID: "automatic", UserID: "user-1", ProjectID: p, InputJSON: `{"metadata":{"operation":"character_turnaround","characterAssetId":"` + asset.ID + `"}}`}, map[string]interface{}{"images": []any{map[string]any{"resourceId": "image-1"}}})
					}
					errs <- err
				}(service)
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			versions, err := services[0].repo.AssetVersions(asset.ID)
			if err != nil || len(versions) != 2 {
				t.Fatalf("registration not exactly once: %d %v", len(versions), err)
			}
		})
	}
}

func TestMediaIdentityConcurrentRegistrationPostgres(t *testing.T) {
	for _, mode := range []string{"candidate", "visual", "character"} {
		t.Run(mode, func(t *testing.T) {
			dbs := profileConcurrencyDatabases(t, "postgres")
			db := dbs[0]
			if err := db.AutoMigrate(&model.ProjectUnit{}, &model.ProjectAssetCandidate{}, &model.VoiceProfile{}); err != nil {
				t.Fatal(err)
			}
			p, _ := seedWorkflowProject(t, db)
			s := &Service{repo: repository.New(db)}
			if err := db.Create(&model.Resource{ID: "image-1", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Width: 800, Height: 600, MimeType: "image/png", Size: 100}).Error; err != nil {
				t.Fatal(err)
			}
			var asset ProjectAssetSummary
			if mode == "character" {
				d, err := s.CreateProjectCharacter("user-1", p.ID, CreateProjectCharacterRequest{Name: "character"})
				if err != nil {
					t.Fatal(err)
				}
				asset = d.Asset
			} else {
				var err error
				asset, err = s.CreateProjectAssetEntity("user-1", p.ID, CreateProjectAssetEntityRequest{Name: "prop", Category: model.AssetCategoryProp})
				if err != nil {
					t.Fatal(err)
				}
			}
			var task model.Task
			if mode == "candidate" {
				task = seedManualImageCandidate(t, s, db, p.ID, asset, "candidate")
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			for _, conn := range dbs {
				go func(conn *gorm.DB) {
					<-start
					writer := &Service{repo: repository.New(conn)}
					var err error
					switch mode {
					case "candidate":
						_, err = writer.AdoptProjectImageCandidate("user-1", p.ID, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID})
					case "visual":
						_, err = writer.bindProjectAssetImage("user-1", p.ID, asset.ID, BindProjectAssetImageRequest{ResourceID: "image-1", ExpectedVersionID: asset.PrimaryVersionID}, "automatic")
					case "character":
						_, err = writer.finalizeCharacterTurnaroundTask(model.Task{ID: "automatic", UserID: "user-1", ProjectID: p.ID, InputJSON: `{"metadata":{"operation":"character_turnaround","characterAssetId":"` + asset.ID + `"}}`}, map[string]interface{}{"images": []any{map[string]any{"resourceId": "image-1"}}})
					}
					errs <- err
				}(conn)
			}
			close(start)
			for i := 0; i < 2; i++ {
				if err := <-errs; err != nil {
					t.Fatal(err)
				}
			}
			versions, err := s.repo.AssetVersions(asset.ID)
			if err != nil || len(versions) != 2 {
				t.Fatalf("registration not exactly once: %d %v", len(versions), err)
			}
		})
	}
}

func TestMediaIdentityGeneratedWorkflowReplayIgnoresCopies(t *testing.T) {
	s, db := newProjectWorkflowV2TestService(t)
	p, u := seedWorkflowProject(t, db)
	shot, err := s.CreateProjectShot("user-1", p.ID, CreateProjectShotRequest{UnitID: u.ID, Title: "shot", Description: "image"})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "generated-task", UserID: "user-1", ProjectID: p.ID}
	if err := db.Create(&model.Resource{ID: "image", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady}).Error; err != nil {
		t.Fatal(err)
	}
	original, err := s.ensureGeneratedProjectAsset(task, p.ID, shot.ID, "image", "image")
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.repo.AssetVersion(original)
	if err != nil {
		t.Fatal(err)
	}
	copied := *version
	copied.ID = "copy"
	copied.Version++
	if err := db.Create(&copied).Error; err != nil {
		t.Fatal(err)
	}
	// Equal timestamps and different IDs must not make the copy a replay target.
	for _, rep := range []model.AssetRepresentation{{ID: "a-copy", TaskID: task.ID, AssetVersionID: copied.ID, ResourceID: "image", MediaType: "image", Role: "output"}, {ID: "z-original", TaskID: task.ID, AssetVersionID: original, ResourceID: "image", MediaType: "image", Role: "output"}} {
		if err := db.Create(&rep).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ensureGeneratedProjectAsset(task, p.ID, shot.ID, "image", "image")
	if err != nil || got != original {
		t.Fatalf("generated replay selected copy: %s want %s: %v", got, original, err)
	}
}

func TestMediaIdentityRejectsDeletingAsset(t *testing.T) {
	s, db, p, asset := seedProjectVisualAsset(t, model.AssetCategoryProp)
	if err := db.Model(&model.Asset{}).Where("id = ?", asset.ID).Update("status", model.AssetVersionStatusDeleting).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.bindProjectAssetImage("user-1", p, asset.ID, BindProjectAssetImageRequest{ResourceID: "image-1", ExpectedVersionID: asset.PrimaryVersionID}, "late-task"); err == nil {
		t.Fatal("registration resurrected deleting asset")
	}
}

func TestMediaIdentityCandidateReplayAfterCopies(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "receipt"
		if legacy {
			name = "legacyFallback"
		}
		t.Run(name, func(t *testing.T) {
			s, db, p := referenceTestService(t)
			asset, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "prop", Category: model.AssetCategoryProp})
			if err != nil {
				t.Fatal(err)
			}
			task := seedManualImageCandidate(t, s, db, p, asset, "candidate")
			adopted, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID})
			if err != nil {
				t.Fatal(err)
			}
			// Isolate replay from the separately tested schema upgrade.
			if err := db.Exec("DROP INDEX IF EXISTS idx_asset_representations_task_role").Error; err != nil {
				t.Fatal(err)
			}
			original, _ := s.repo.AssetVersion(adopted.PrimaryVersionID)
			copied := *original
			copied.ID = "copied-version"
			copied.Version++
			copied.CreatedAt = time.Now().Add(time.Second)
			if err := db.Create(&copied).Error; err != nil {
				t.Fatal(err)
			}
			reps, _ := s.repo.AssetRepresentations(original.ID)
			for _, rep := range reps {
				rep.ID = "copied-" + rep.ID
				rep.AssetVersionID = copied.ID
				rep.CreatedAt = copied.CreatedAt
				if err := db.Create(&rep).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Model(&model.Asset{}).Where("id = ?", asset.ID).Update("primary_version_id", copied.ID).Error; err != nil {
				t.Fatal(err)
			}
			if legacy {
				stored, _ := s.repo.Task(task.ID)
				var result map[string]any
				json.Unmarshal([]byte(stored.ResultJSON), &result)
				candidate := candidateFromResult(stored.ResultJSON)
				candidate.AdoptedVersionID = ""
				result["assetImageCandidate"] = candidate
				raw, _ := json.Marshal(result)
				if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("result_json", string(raw)).Error; err != nil {
					t.Fatal(err)
				}
			}
			replay, err := s.AdoptProjectImageCandidate("user-1", p, asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: asset.PrimaryVersionID})
			if err != nil {
				t.Fatal(err)
			}
			stored, _ := s.repo.Task(task.ID)
			if got := candidateFromResult(stored.ResultJSON).AdoptedVersionID; got != original.ID {
				t.Fatalf("replay moved original adoption to copy: got %s want %s", got, original.ID)
			}
			if replay.PrimaryVersionID != copied.ID || replay.VersionCount != 3 {
				t.Fatalf("replay changed current asset: %+v", replay)
			}
		})
	}
}

func TestMediaIdentityCharacterCopyPreservesProvenance(t *testing.T) {
	s, db, p := referenceTestService(t)
	detail, err := s.CreateProjectCharacter("user-1", p, CreateProjectCharacterRequest{Name: "character"})
	if err != nil {
		t.Fatal(err)
	}
	task := seedManualImageCandidate(t, s, db, p, detail.Asset, "candidate")
	adopted, err := s.AdoptProjectImageCandidate("user-1", p, detail.Asset.ID, task.ID, AdoptProjectImageCandidateRequest{ExpectedVersionID: detail.Asset.PrimaryVersionID})
	if err != nil {
		t.Fatal(err)
	}
	asset, _ := s.repo.AssetForUser("user-1", adopted.ID)
	next, err := s.createNextCharacterVersion(p, asset, "renamed", "", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	reps, err := s.repo.AssetRepresentations(next.ID)
	if err != nil || len(reps) != 2 {
		t.Fatalf("copied representations: %+v %v", reps, err)
	}
	for _, rep := range reps {
		if rep.TaskID != task.ID {
			t.Fatalf("copy lost original media task: %q", rep.TaskID)
		}
	}
}

func TestMediaIdentityWorkflowTargetsVersionAndRejectsReplayMismatch(t *testing.T) {
	s, db := newProjectWorkflowV2TestService(t)
	p, u := seedWorkflowProject(t, db)
	if err := s.EnsureBuiltinProjectWorkflowTemplate(); err != nil {
		t.Fatal(err)
	}
	workflow, err := s.CreateUnitWorkflow("user-1", p.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{ID: "asset", UserID: "user-1", PrimaryVersionID: "v1"}
	for _, row := range []any{&asset, &model.AssetVersion{ID: "v1", AssetID: asset.ID, Version: 1}, &model.AssetVersion{ID: "v2", AssetID: asset.ID, Version: 2}, &model.ProjectAssetLink{ID: "link", ProjectID: p.ID, AssetID: asset.ID}, &model.Resource{ID: "image", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady}, &model.Resource{ID: "other-image", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady}, &model.Task{ID: "task", UserID: "user-1", ProjectID: p.ID, Status: model.TaskStatusSucceeded}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	req := RegisterTaskOutputRequest{TaskID: "task", AssetVersionID: "v1", ResourceID: "image", MediaType: "image", Role: "output"}
	if _, err := s.RegisterTaskOutput("user-1", p.ID, workflow.Steps[0].ID, req); err != nil {
		t.Fatal(err)
	}
	req.AssetVersionID = "v2"
	if _, err := s.RegisterTaskOutput("user-1", p.ID, workflow.Steps[0].ID, req); err != nil {
		t.Fatal(err)
	}
	reps, err := s.repo.AssetRepresentations("v2")
	if err != nil || len(reps) != 1 {
		t.Fatalf("target version registration missing: %+v %v", reps, err)
	}
	req.ResourceID = "other-image"
	if _, err := s.RegisterTaskOutput("user-1", p.ID, workflow.Steps[0].ID, req); err == nil {
		t.Fatal("mismatched workflow replay accepted")
	}
}

func TestMediaIdentityRegistrationRejectsWrongTargetAndResource(t *testing.T) {
	s, _, p, asset := seedProjectVisualAsset(t, model.AssetCategoryProp)
	req := BindProjectAssetImageRequest{ResourceID: "image-1", ExpectedVersionID: asset.PrimaryVersionID}
	if _, err := s.bindProjectAssetImage("user-1", p, asset.ID, req, "media-task"); err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProjectAssetEntity("user-1", p, CreateProjectAssetEntityRequest{Name: "other", Category: model.AssetCategoryProp})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.bindProjectAssetImage("user-1", p, other.ID, BindProjectAssetImageRequest{ResourceID: "image-1", ExpectedVersionID: other.PrimaryVersionID}, "media-task"); err == nil {
		t.Fatal("task registered to different asset accepted as replay")
	}
	req.ResourceID = "foreign-image"
	if _, err := s.bindProjectAssetImage("user-1", p, asset.ID, req, "media-task"); err == nil {
		t.Fatal("different resource accepted as replay")
	}
}
