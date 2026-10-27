package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// Separate pools and physical connections exercise cross-instance locking;
// neither a shared single-connection pool nor an in-memory database would do so.
func profileConcurrencyDatabases(t *testing.T, dialect string) [2]*gorm.DB {
	t.Helper()
	options := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var dialector gorm.Dialector
	if dialect == "sqlite" {
		path := filepath.ToSlash(filepath.Join(t.TempDir(), "profile.db"))
		dialector = sqlite.Open("file:" + path + "?_journal_mode=WAL&_busy_timeout=5000")
	} else {
		dsn := os.Getenv("CANVAS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured; PostgreSQL concurrency is unverified")
		}
		parsed, err := url.Parse(dsn)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
			t.Fatal("CANVAS_TEST_POSTGRES_DSN must be a PostgreSQL URL for an isolated test database")
		}
		admin, err := gorm.Open(postgres.Open(dsn), options)
		if err != nil {
			t.Fatal(err)
		}
		adminSQL, err := admin.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = adminSQL.Close() })
		schema := fmt.Sprintf("profile_concurrency_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
				t.Error(err)
			}
		})
		query := parsed.Query()
		query.Set("search_path", schema)
		query.Set("lock_timeout", "10000")
		query.Set("statement_timeout", "15000")
		parsed.RawQuery = query.Encode()
		dialector = postgres.Open(parsed.String())
	}
	var databases [2]*gorm.DB
	for i := range databases {
		db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
		databases[i] = db
		if dialect == "sqlite" {
			var mode string
			if err := db.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil || mode != "wal" {
				t.Fatalf("file database must use WAL: mode=%q err=%v", mode, err)
			}
		}
	}
	first, _ := databases[0].DB()
	second, _ := databases[1].DB()
	if first == second {
		t.Fatal("concurrency requires separate pools")
	}
	if dialect == "postgres" {
		var pids [2]int
		for i, db := range databases {
			if err := db.Raw("SELECT pg_backend_pid()").Scan(&pids[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		if pids[0] == pids[1] {
			t.Fatal("concurrency requires separate PostgreSQL sessions")
		}
	}
	if err := databases[0].AutoMigrate(
		&model.Project{}, &model.Asset{}, &model.AssetVersion{}, &model.ProjectAssetLink{},
		&model.ProjectAssetFolder{}, &model.AssetRepresentation{}, &model.CharacterVoiceBinding{},
		&model.Shot{}, &model.ShotAssetReference{}, &model.Task{}, &model.Resource{},
	); err != nil {
		t.Fatal(err)
	}
	return databases
}

// Rendezvous before the first owner coordination lock, not a downstream row
// lock: waiting for both row-lock arrivals would deadlock behind the owner fence.
// No database writes are mocked.
func profileConcurrencyBarrier(t *testing.T, databases [2]*gorm.DB, firstLocker int) {
	t.Helper()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	firstLocked := make(chan struct{})
	var lockOnce sync.Once
	for index, db := range databases {
		var once sync.Once
		wait := func(tx *gorm.DB) {
			statement := tx.Statement.SQL.String()
			if !strings.Contains(statement, "pg_advisory_xact_lock") && !strings.Contains(statement, "UPDATE assets SET id = id WHERE 1 = 0") { return }
			once.Do(func() {
				arrived <- struct{}{}
				if len(arrived) == len(databases) {
					releaseOnce.Do(func() { close(release) })
				}
				select {
				case <-release:
				case <-time.After(10 * time.Second):
					tx.AddError(errors.New("both services did not reach the asset lock rendezvous"))
				}
				if firstLocker >= 0 && index != firstLocker {
					select {
					case <-firstLocked:
					case <-time.After(10 * time.Second):
						tx.AddError(errors.New("first writer did not acquire its real asset lock"))
					}
				}
			})
		}
		if index == firstLocker {
			acquired := func(tx *gorm.DB) {
				statement := tx.Statement.SQL.String()
				if tx.Error != nil || (!strings.Contains(statement, "pg_advisory_xact_lock") && !strings.Contains(statement, "UPDATE assets SET id = id WHERE 1 = 0")) { return }
				lockOnce.Do(func() { close(firstLocked) })
			}
			err := db.Callback().Raw().After("gorm:raw").Register("profile_concurrency_acquired", acquired)
			if err != nil {
				t.Fatal(err)
			}
		}
		err := db.Callback().Raw().Before("gorm:raw").Register("profile_concurrency_lock", wait)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProfileAnalysisRealConcurrency(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, scenario := range []string{"same_key", "same_key_different_fields", "different_key_same_task", "different_tasks_same_base", "manual_reference_edit", "manual_reference_edit_wins"} {
				t.Run(scenario, func(t *testing.T) {
					manualRace := scenario == "manual_reference_edit" || scenario == "manual_reference_edit_wins"
					databases := profileConcurrencyDatabases(t, dialect)
					db := databases[0]
					services := [2]*Service{{repo: repository.New(db)}, {repo: repository.New(databases[1])}}
					project := model.Project{ID: newID(), UserID: "profile-user", Name: "concurrent profile", Status: model.ProjectStatusActive}
					resource := model.Resource{ID: newID(), UserID: project.UserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 32, Height: 32, Size: 100}
					for _, row := range []any{&project, &resource} {
						if err := db.Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					asset, err := services[0].CreateProjectAssetEntity(project.UserID, project.ID, CreateProjectAssetEntityRequest{
						Name: "profile", Category: model.AssetCategoryProp,
						Definition:     map[string]any{"description": "old", "prompt": "old prompt", "extra": []any{"keep", 2}},
						ReferenceImage: &ProjectReferenceImage{ResourceID: resource.ID},
					})
					if err != nil {
						t.Fatal(err)
					}
					// Both linked projects must increment once, not again for replay/the loser.
					other := model.Project{ID: newID(), UserID: project.UserID, Name: "also linked", Status: model.ProjectStatusActive}
					for _, row := range []any{&other, &model.ProjectAssetLink{ID: newID(), ProjectID: other.ID, AssetID: asset.ID}} {
						if err := db.Create(row).Error; err != nil {
							t.Fatal(err)
						}
					}
					var beforeProjects []model.Project
					if err := db.Order("id").Find(&beforeProjects).Error; err != nil {
						t.Fatal(err)
					}
					base, err := services[0].repo.AssetVersion(asset.PrimaryVersionID)
					if err != nil {
						t.Fatal(err)
					}
					binding := ProfileAnalysisBinding{SchemaVersion: 1, ProjectID: project.ID, AssetID: asset.ID, Category: asset.Category, BaseVersionID: base.ID, Source: ProjectReferenceImage{ResourceID: resource.ID}, PromptVersion: 1}
					input, _ := json.Marshal(map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}})
					result, _ := json.Marshal(map[string]any{"mode": "text", "assetProfileAnalysis": ProfileAnalysisResult{SchemaVersion: 1, AssetID: asset.ID, Category: asset.Category, BaseVersionID: base.ID, Source: binding.Source, Draft: map[string]string{"description": "new", "prompt": "suggestion"}}})
					task := model.Task{ID: newID(), UserID: project.UserID, ProjectID: project.ID, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusSucceeded, InputJSON: string(input), ResultJSON: string(result), CreatedAt: time.Now(), UpdatedAt: time.Now()}
					if err := db.Create(&task).Error; err != nil {
						t.Fatal(err)
					}
					tasks := [2]model.Task{task, task}
					requests := [2]AdoptProfileAnalysisRequest{
						{ExpectedVersionID: base.ID, SelectedFields: []string{"description", "prompt"}, IdempotencyKey: "concurrent-key-0001"},
						{ExpectedVersionID: base.ID, SelectedFields: []string{"prompt", "description"}, IdempotencyKey: "concurrent-key-0001"},
					}
					manualResource := resource
					manualResource.ID = newID()
					wantReason := ""
					switch scenario {
					case "manual_reference_edit", "manual_reference_edit_wins":
						if err := db.Create(&manualResource).Error; err != nil {
							t.Fatal(err)
						}
						wantReason = "project_asset_version_changed"
					case "same_key_different_fields":
						requests[0].SelectedFields, requests[1].SelectedFields = []string{"description"}, []string{"prompt"}
						wantReason = "asset_profile_analysis_idempotency_conflict"
					case "different_key_same_task":
						requests[1].IdempotencyKey = "concurrent-key-0002"
						wantReason = "asset_profile_analysis_already_adopted"
					case "different_tasks_same_base":
						tasks[1].ID = newID()
						if err := db.Create(&tasks[1]).Error; err != nil {
							t.Fatal(err)
						}
						wantReason = "project_asset_version_changed"
					}
					firstLocker := -1
					if manualRace {
						firstLocker = 0
					}
					if scenario == "manual_reference_edit_wins" {
						firstLocker = 1
					}
					profileConcurrencyBarrier(t, databases, firstLocker)
					var responses [2]AdoptProfileAnalysisResponse
					var errs [2]error
					var workers sync.WaitGroup
					for i := range services {
						workers.Add(1)
						go func(i int) {
							defer workers.Done()
							if manualRace && i == 1 {
								// The real manual API reaches SaveCharacterVersion through
								// CommitAssetReferenceVersion, using the same old base CAS.
								var edited ProjectAssetSummary
								edited, errs[i] = services[i].SetProjectReferenceImage(project.UserID, project.ID, asset.ID, SetProjectReferenceImageRequest{ExpectedVersionID: base.ID, ResourceID: manualResource.ID})
								responses[i] = AdoptProfileAnalysisResponse{AdoptedVersionID: edited.PrimaryVersionID, Asset: edited}
								return
							}
							responses[i], errs[i] = services[i].AdoptProjectProfileAnalysis(project.UserID, project.ID, asset.ID, tasks[i].ID, requests[i])
						}(i)
					}
					workers.Wait()
					winner, writes, replays, conflicts := -1, 0, 0, 0
					for i, err := range errs {
						if err != nil {
							var appErr *AppError
							if wantReason == "" || !errors.As(err, &appErr) || appErr.Status != 409 || string(appErr.Reason) != wantReason {
								t.Errorf("request %d: want conflict %q, got %v", i, wantReason, err)
							}
							conflicts++
						} else if responses[i].Replayed {
							replays++
						} else {
							writes++
							winner = i
						}
					}
					if writes != 1 || (wantReason == "" && (replays != 1 || conflicts != 0)) || (wantReason != "" && (replays != 0 || conflicts != 1)) {
						t.Fatalf("want exactly one commit: writes=%d replays=%d conflicts=%d responses=%+v", writes, replays, conflicts, responses)
					}
					adoptedID := responses[winner].AdoptedVersionID
					if adoptedID == "" || adoptedID == base.ID {
						t.Fatalf("invalid adopted version %q", adoptedID)
					}
					if wantReason == "" && responses[1-winner].AdoptedVersionID != adoptedID {
						t.Fatal("same-key replay returned a different version")
					}
					versions, err := services[0].repo.AssetVersions(asset.ID)
					if err != nil || len(versions) != 2 {
						t.Fatalf("loser/replay left extra versions: count=%d err=%v", len(versions), err)
					}
					storedBase, err := services[0].repo.AssetVersion(base.ID)
					if err != nil || !reflect.DeepEqual(storedBase, base) {
						t.Fatalf("historical base changed: %v", err)
					}
					latest, err := services[0].repo.AssetForUser(project.UserID, asset.ID)
					if err != nil || latest.PrimaryVersionID != adoptedID {
						t.Fatalf("asset CAS not committed with receipt: %+v %v", latest, err)
					}
					adopted, err := services[0].repo.AssetVersion(adoptedID)
					if err != nil {
						t.Fatal(err)
					}
					var definition map[string]json.RawMessage
					if err := json.Unmarshal([]byte(adopted.DefinitionJSON), &definition); err != nil {
						t.Fatal(err)
					}
					manualWon := manualRace && winner == 1
					var receipt ProfileAdoptionReceipt
					if manualWon {
						if _, exists := definition[profileAdoptionKey]; exists {
							t.Fatal("rejected adoption left a partial version receipt")
						}
					} else if err := json.Unmarshal(definition[profileAdoptionKey], &receipt); err != nil || receipt.TaskID != tasks[winner].ID || receipt.AdoptedVersionID != adoptedID || receipt.BaseVersionID != base.ID {
						t.Fatalf("version receipt not atomic with winner: %+v %v", receipt, err)
					}
					var baseDefinition map[string]json.RawMessage
					_ = json.Unmarshal([]byte(base.DefinitionJSON), &baseDefinition)
					for key, value := range baseDefinition {
						expected := string(value)
						for _, field := range requests[winner].SelectedFields {
							if field == key && !manualWon {
								if key == "description" {
									expected = `"new"`
								} else {
									expected = `"suggestion"`
								}
							}
						}
						if manualWon && key == "referenceImage" {
							raw, _ := json.Marshal(ProjectReferenceImage{ResourceID: manualResource.ID})
							expected = string(raw)
						}
						if string(definition[key]) != expected {
							t.Errorf("field %s: got %s want %s", key, definition[key], expected)
						}
					}
					for i, original := range tasks {
						stored, err := services[0].repo.Task(original.ID)
						if err != nil {
							t.Fatal(err)
						}
						if manualWon || (scenario == "different_tasks_same_base" && i != winner) {
							if stored.ResultJSON != original.ResultJSON || stored.Status != original.Status || stored.InputJSON != original.InputJSON {
								t.Error("losing task mutated despite rejected base CAS")
							}
							continue
						}
						parsed, err := validatedProfileResult(*stored)
						if err != nil || !reflect.DeepEqual(parsed.Adoption, &receipt) {
							t.Fatalf("task and version receipts differ: %+v %v", parsed, err)
						}
					}
					for _, before := range beforeProjects {
						var after model.Project
						if err := db.First(&after, "id = ?", before.ID).Error; err != nil {
							t.Fatal(err)
						}
						if after.Revision != before.Revision+1 {
							t.Errorf("project %s revision = %d, want %d", after.ID, after.Revision, before.Revision+1)
						}
					}
				})
			}
		})
	}
}
