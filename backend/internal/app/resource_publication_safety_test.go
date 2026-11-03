package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/protocol"
	"yingce/backend/internal/repository"
)

func TestResourcePublicationTaskFamilies(t *testing.T) {
	for _, family := range []string{"completion", "completion-result", "registration-result", "raw-task-create", "raw-task-save", "raw-result-create", "raw-result-save", "raw-log-create", "raw-log-save", "service-log", "route-input", "text-replay", "candidate-result"} {
		for _, order := range []string{"delete-first", "writer-first"} {
			t.Run(family+"/"+order, func(t *testing.T) {
				db, other := deletionConcurrencyDBs(t)
				repo, writer := repository.New(db), repository.New(other)
				resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
				task := model.Task{ID: "publication-task", UserID: "user-1", Status: model.TaskStatusRunning, InputJSON: `{}`, ResultJSON: `{}`, RouteID: "before"}
				if family == "text-replay" {
					task.Status = model.TaskStatusTextReplay
				}
				if family == "candidate-result" {
					task.Status = model.TaskStatusSucceeded
				}
				for _, row := range []any{&resource, &task} {
					if err := db.Create(row).Error; err != nil {
						t.Fatal(err)
					}
				}
				raw := `{"nested":{"resourceId":"publication"}}`
				publish := func() error {
					switch family {
					case "completion":
						v := task
						v.Status = model.TaskStatusSucceeded
						v.ResultJSON = raw
						return writer.SaveTaskCompletion(&v, task.Status, nil)
					case "completion-result":
						v := task
						v.Status = model.TaskStatusSucceeded
						return writer.SaveTaskCompletion(&v, task.Status, []model.Result{{ID: "result", UserID: "user-1", TaskID: task.ID, URL: "/api/resources/publication"}})
					case "registration-result":
						v := task
						v.Status = model.TaskStatusSucceeded
						return writer.SaveTaskCompletionWithRegistration(&v, task.Status, nil, func(tx *repository.Repository) error {
							v.ResultJSON = raw
							return tx.Create(&model.TaskLog{ID: "registration-side-effect", UserID: "user-1", TaskID: task.ID, Payload: `{}`})
						})
					case "raw-task-create":
						return writer.Create(&model.Task{ID: "new-task", UserID: "user-1", InputJSON: raw})
					case "raw-task-save":
						v := task
						v.ResultJSON = raw
						return writer.Save(&v)
					case "raw-result-create":
						return writer.Create(&model.Result{ID: "result", UserID: "user-1", TaskID: task.ID, Payload: raw})
					case "raw-result-save":
						return writer.Save(&model.Result{ID: "result", UserID: "user-1", TaskID: task.ID, Payload: raw})
					case "raw-log-create":
						return writer.Create(&model.TaskLog{ID: "log", UserID: "user-1", TaskID: task.ID, Payload: raw})
					case "raw-log-save":
						return writer.Save(&model.TaskLog{ID: "log", UserID: "user-1", TaskID: task.ID, Payload: raw})
					case "service-log":
						svc := New(writer, t.TempDir())
						return svc.log("user-1", task.ID, "info", "publication", raw)
					case "route-input":
						return writer.SwitchTaskLogicalRoute(task.ID, "before", "after", raw, "", "", "", nil, model.BillingCostSnapshot{})
					case "candidate-result":
						return writer.UpdateImageCandidateResult("user-1", task.ID, raw)
					case "text-replay":
						_, err := writer.CompleteTextReplayTask("user-1", task.ID, raw, time.Now())
						return err
					}
					return fmt.Errorf("unknown family")
				}
				if order == "delete-first" {
					preparePublicationDeletion(t, repo, resource)
					before := publicationRows(t, db)
					err := publish()
					if !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, repository.ErrResourceDeleting) {
						t.Errorf("want unavailable reference rejection, got %v", err)
					}
					if after := publicationRows(t, db); before != after {
						t.Error("rejected publication changed durable rows")
					}
				} else {
					if err := publish(); err != nil {
						t.Fatal(err)
					}
					if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
						t.Fatal("writer-first reference omitted by deletion admission")
					}
					stored, err := repo.Resource(resource.ID)
					if err != nil || stored.Status != model.ResourceStatusReady {
						t.Fatalf("referenced resource changed: %+v %v", stored, err)
					}
				}
			})
		}
	}
}

func TestResourcePublicationEscapedIDs(t *testing.T) {
	for _, family := range []string{"task-input", "task-result", "task-log"} {
		for _, count := range []int{1, 100, 101} {
			for _, order := range []string{"delete-first", "writer-first"} {
				t.Run(fmt.Sprintf("%s/%d/%s", family, count, order), func(t *testing.T) {
					db, other := deletionConcurrencyDBs(t)
					repo, writer := repository.New(db), repository.New(other)
					resources := make([]model.Resource, count)
					for i := range resources {
						id := fmt.Sprintf("unreferenced-%d", i)
						if i == 0 {
							id = "publication"
						}
						resources[i] = model.Resource{ID: id, UserID: "user-1", Provider: "local", ObjectKey: id, Status: model.ResourceStatusReady}
					}
					if err := db.Create(&resources).Error; err != nil {
						t.Fatal(err)
					}
					task := model.Task{ID: "escaped-task", UserID: "user-1", Status: model.TaskStatusRunning, InputJSON: `{}`, ResultJSON: `{}`}
					if err := db.Create(&task).Error; err != nil {
						t.Fatal(err)
					}
					publish := func() error {
						raw := `{"resourceId":"\u0070ublication"}`
						switch family {
						case "task-input":
							task.InputJSON = raw
							return writer.Save(&task)
						case "task-result":
							task.ResultJSON, task.Status = raw, model.TaskStatusSucceeded
							return writer.SaveTaskCompletion(&task, model.TaskStatusRunning, nil)
						default:
							return writer.Create(&model.TaskLog{ID: "escaped-log", UserID: task.UserID, TaskID: task.ID, Payload: raw})
						}
					}
					if order == "delete-first" {
						preparePublicationDeletion(t, repo, resources[0])
						before := publicationRows(t, db)
						if err := publish(); !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, repository.ErrResourceDeleting) {
							t.Fatalf("late escaped reference admitted: %v", err)
						}
						if publicationRows(t, db) != before {
							t.Fatal("rejected write changed rows")
						}
					} else {
						if err := publish(); err != nil {
							t.Fatal(err)
						}
						if err := repo.DeleteDetachedResources(resources, nil); err == nil {
							t.Fatal("escaped reference omitted from deletion admission")
						}
						stored, err := repo.Resource(resources[0].ID)
						if err != nil || stored.Status != model.ResourceStatusReady {
							t.Fatalf("referenced resource changed: %+v %v", stored, err)
						}
					}
				})
			}
		}
	}
}

func TestResourcePublicationDocumentParity(t *testing.T) {
	for _, family := range []string{"workbench-capability", "workbench-guide", "workbench-disabled-cover", "preset", "creation-canvas", "voice-compatible", "template", "shot-revision", "shot-replace", "builtin-tool", "ark-create", "ark-save"} {
		for _, order := range []string{"delete-first", "writer-first"} {
			t.Run(family+"/"+order, func(t *testing.T) {
				db, other := deletionConcurrencyDBs(t)
				repo, writer := repository.New(db), repository.New(other)
				resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
				project := model.Project{ID: "project", UserID: "user-1", Name: "project"}
				for _, row := range []any{&resource, &project} {
					if err := db.Create(row).Error; err != nil {
						t.Fatal(err)
					}
				}
				raw := `{"nested":{"resourceId":"publication"}}`
				table := ""
				publish := func() error {
					switch family {
					case "workbench-capability":
						table = "workbench_tools"
						return writer.CreateWorkbenchTool(&model.WorkbenchTool{ID: "tool", Name: "tool", CapabilityJSON: raw})
					case "workbench-guide":
						table = "workbench_tools"
						return writer.CreateWorkbenchTool(&model.WorkbenchTool{ID: "tool", Name: "tool", GuideJSON: raw})
					case "workbench-disabled-cover":
						table = "workbench_tools"
						return writer.CreateWorkbenchTool(&model.WorkbenchTool{ID: "tool", Name: "tool", Enabled: false, CoverResourceID: resource.ID})
					case "preset":
						table = "workbench_presets"
						return writer.CreateWorkbenchPreset(&model.WorkbenchPreset{ID: "preset", ToolID: "tool", Name: "preset", DefaultParamsJSON: raw})
					case "creation-canvas":
						table = "creation_runs"
						return writer.CreateCreationRun(&model.CreationRun{ID: "run", UserID: "user-1", ClientKey: "run", ApprovedCanvasJSON: raw})
					case "voice-compatible":
						table = "voice_profiles"
						return writer.CreateVoiceProfile(&model.VoiceProfile{ID: "voice", UserID: "user-1", CompatibleModelsJSON: raw})
					case "template":
						table = "workflow_template_versions"
						return writer.CreateWorkflowTemplateVersion(&model.WorkflowTemplateVersion{ID: "template", TemplateKey: "template", DefinitionJSON: raw})
					case "shot-revision":
						table = "shot_revisions"
						return writer.SaveShotWithRevision(&model.Shot{ID: "shot", ProjectID: project.ID}, &model.ShotRevision{ID: "revision", ShotID: "shot", ActionBeatsJSON: raw}, true)
					case "shot-replace":
						table = "shot_revisions"
						return writer.ReplaceProjectUnitShots(project.ID, "unit", []model.Shot{{ID: "shot", ProjectID: project.ID, UnitID: "unit"}}, []model.ShotRevision{{ID: "revision", ShotID: "shot", ActionBeatsJSON: raw}}, nil, nil)
					case "builtin-tool":
						table = "tools"
						return writer.UpsertBuiltinTools([]model.Tool{{ID: 42, Source: "builtin", Cover: "/api/resources/publication"}})
					case "ark-create":
						table = "ark_private_asset_bindings"
						_, err := writer.CreateArkPrivateAssetBinding(&model.ArkPrivateAssetBinding{ID: "ark", UserID: "user-1", ResourceID: resource.ID, ProjectName: "project"})
						return err
					case "ark-save":
						table = "ark_private_asset_bindings"
						return writer.SaveArkPrivateAssetBinding(&model.ArkPrivateAssetBinding{ID: "ark", UserID: "user-1", ResourceID: resource.ID, ProjectName: "project"})
					}
					return fmt.Errorf("unknown")
				}
				if order == "delete-first" {
					preparePublicationDeletion(t, repo, resource)
					err := publish()
					if !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, repository.ErrResourceDeleting) {
						t.Errorf("want unavailable rejection, got %v", err)
					}
					var count int64
					if err := db.Table(table).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Errorf("rejected publication left %d rows", count)
					}
				} else {
					if err := publish(); err != nil {
						t.Fatal(err)
					}
					if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
						t.Fatal("writer-first document omitted by deletion scanner")
					}
				}
			})
		}
	}
}

func TestResourcePublicationTruncatedLogRetainsReference(t *testing.T) {
	for _, order := range []string{"delete-first", "writer-first"} {
		t.Run(order, func(t *testing.T) {
			db, other := deletionConcurrencyDBs(t)
			repo, writer := repository.New(db), repository.New(other)
			resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
			if err := db.Create(&resource).Error; err != nil {
				t.Fatal(err)
			}
			raw := `{"message":"` + strings.Repeat("x", taskLogPayloadLimit+100) + `","source":{"resourceId":"publication"}}`
			svc := New(writer, t.TempDir())
			if order == "delete-first" {
				preparePublicationDeletion(t, repo, resource)
				if err := svc.log("user-1", "", "info", "large", raw); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("late truncated log: %v", err)
				}
			} else {
				if err := svc.log("user-1", "", "info", "large", raw); err != nil {
					t.Fatal(err)
				}
				logs, err := writer.TaskLogs("user-1", "")
				if err != nil || len(logs) != 1 {
					t.Fatalf("logs %v %v", logs, err)
				}
				if !json.Valid([]byte(logs[0].Payload)) {
					t.Fatal("truncation invalidated JSON")
				}
				if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
					t.Fatal("truncation erased durable reference")
				}
			}
		})
	}
}

func TestResourcePublicationMediaCheckpoint(t *testing.T) {
	for _, order := range []string{"delete-first", "writer-first"} {
		t.Run(order, func(t *testing.T) {
			db, other := deletionConcurrencyDBs(t)
			repo, writer := repository.New(db), repository.New(other)
			resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
			task := model.Task{ID: "checkpoint-task", UserID: "user-1", Status: model.TaskStatusRunning, InputJSON: `{}`, ResultJSON: `{}`}
			for _, row := range []any{&resource, &task} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			svc := New(writer, t.TempDir())
			if err := os.WriteFile(filepath.Join(svc.dataDir, ".settings-key"), make([]byte, 32), 0600); err != nil {
				t.Fatal(err)
			}
			checkpoint := &mediaCheckpoint{Mode: "image", Items: []mediaCheckpointItem{{ResourceID: resource.ID}}}
			if order == "delete-first" {
				preparePublicationDeletion(t, repo, resource)
				if err := svc.saveMediaCheckpoint(&task, checkpoint, "register"); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("checkpoint admitted: %v", err)
				}
				stored, _ := writer.Task(task.ID)
				if stored.MediaRecoveryJSON != "" {
					t.Fatal("rejected checkpoint persisted")
				}
			} else {
				if err := svc.saveMediaCheckpoint(&task, checkpoint, "register"); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(task.MediaRecoveryJSON, resource.ID) {
					t.Fatal("checkpoint encryption weakened")
				}
				if err := writer.ReleaseExpiredCloudAgentResourceLeases(time.Now().Add(24 * time.Hour)); err != nil {
					t.Fatal(err)
				}
				if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
					t.Fatal("checkpoint reference not protected")
				}
				task.Status = model.TaskStatusSucceeded
				task.MediaRecoveryJSON = ""
				task.ResultJSON = `{}`
				if err := writer.SaveTaskCompletion(&task, model.TaskStatusRunning, nil); err != nil {
					t.Fatal(err)
				}
				if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
					t.Fatalf("cleared checkpoint still retained resource: %v", err)
				}
			}
		})
	}
}

func TestResourcePublicationKnownEmptyCheckpoint(t *testing.T) {
	db, other := deletionConcurrencyDBs(t)
	repo, writer := repository.New(db), repository.New(other)
	resource := model.Resource{ID: "unrelated", UserID: "user-1", Provider: "local", ObjectKey: "unrelated", Status: model.ResourceStatusReady}
	task := model.Task{ID: "empty-checkpoint", UserID: "user-1", Status: model.TaskStatusRunning}
	for _, row := range []any{&resource, &task} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := New(writer, t.TempDir())
	if err := os.WriteFile(filepath.Join(svc.dataDir, ".settings-key"), make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	checkpoint := &mediaCheckpoint{Mode: "image", Items: []mediaCheckpointItem{{Reference: protocol.MediaReference{URL: "https://example.com/image.png?token=private-test-token"}}}}
	if err := svc.saveMediaCheckpoint(&task, checkpoint, "download"); err != nil {
		t.Fatal(err)
	}
	stored, err := writer.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.MediaRecoveryJSON, "private-test-token") || strings.Contains(stored.MediaRecoveryJSON, "example.com") {
		t.Fatal("checkpoint exposed provider URL")
	}
	decoded, err := svc.decodeMediaCheckpoint(stored)
	if err != nil || decoded.Items[0].Reference.URL != checkpoint.Items[0].Reference.URL {
		t.Fatalf("checkpoint decode: %+v %v", decoded, err)
	}
	var leases int64
	if err := db.Model(&model.CloudAgentResourceLease{}).Count(&leases).Error; err != nil || leases != 0 {
		t.Fatalf("empty checkpoint must not fabricate resource leases: %d %v", leases, err)
	}
	if err := writer.ReleaseExpiredCloudAgentResourceLeases(time.Now().Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
		t.Fatalf("known-empty checkpoint blocked unrelated resource: %v", err)
	}
}

func TestResourcePublicationEmptyCheckpointLifecycle(t *testing.T) {
	db, other := deletionConcurrencyDBs(t)
	repo, writer := repository.New(db), repository.New(other)
	resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
	task := model.Task{ID: "checkpoint-task", UserID: "user-1", Status: model.TaskStatusRunning}
	for _, row := range []any{&resource, &task} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := New(writer, t.TempDir())
	if err := os.WriteFile(filepath.Join(svc.dataDir, ".settings-key"), make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	remote := &mediaCheckpoint{Mode: "image", Items: []mediaCheckpointItem{{Reference: protocol.MediaReference{URL: "https://example.com/remote.png"}}}}
	local := &mediaCheckpoint{Mode: "image", Items: []mediaCheckpointItem{{Reference: protocol.MediaReference{URL: "/api/resources/publication"}}}}
	assertBlocked := func() {
		t.Helper()
		if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
			t.Fatal("local checkpoint reference omitted")
		}
	}
	if err := svc.saveMediaCheckpoint(&task, local, "download"); err != nil {
		t.Fatal(err)
	}
	assertBlocked()
	before := publicationRows(t, db) + referenceAdmissionRows(t, db, "cloud_agent_resource_leases")
	stale := task
	stale.RouteRun++
	if err := svc.saveMediaCheckpoint(&stale, remote, "download"); !errors.Is(err, repository.ErrTaskStateConflict) {
		t.Fatalf("save CAS: %v", err)
	}
	if after := publicationRows(t, db) + referenceAdmissionRows(t, db, "cloud_agent_resource_leases"); after != before {
		t.Fatal("failed save changed projection or leases")
	}
	if stale.MediaRecoveryJSON != task.MediaRecoveryJSON {
		t.Fatal("failed save changed caller checkpoint")
	}
	assertBlocked()
	if err := svc.saveMediaCheckpoint(&task, remote, "download"); err != nil {
		t.Fatal(err)
	}
	ciphertext, empty, err := repository.TaskMediaCheckpointCiphertext(task.MediaRecoveryJSON)
	if err != nil || !empty || !strings.HasPrefix(ciphertext, "enc:v1:") {
		t.Fatalf("empty projection: %v %v", empty, err)
	}
	if err := writer.SaveTaskMediaCheckpoint(&task, task.MediaRecoveryJSON, "download", []string{resource.ID}); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("caller supplied projection accepted: %v", err)
	}
	for name, mutate := range map[string]func() error{
		"upsert-owner": func() error {
			return writer.UpsertCloudAgentResourceLeases(task.UserID, "run", "task-media:"+task.ID, nil, time.Now())
		},
		"upsert-run": func() error {
			return writer.UpsertCloudAgentResourceLeases(task.UserID, "task-media:"+task.ID, "owner", nil, time.Now())
		},
		"replace": func() error {
			return writer.ReplaceCloudAgentResourceLeases(task.UserID, "run", "task-media:"+task.ID, nil, time.Now())
		},
		"release-owner": func() error { return writer.ReleaseCloudAgentResourceLeases(task.UserID, "task-media:"+task.ID) },
		"release-run":   func() error { return writer.ReleaseCloudAgentResourceLeasesByRun(task.UserID, "task-media:"+task.ID) },
		"transfer-from": func() error {
			return writer.TransferCloudAgentResourceLeases(task.UserID, "task-media:"+task.ID, "other", "run", time.Now())
		},
		"transfer-to": func() error {
			return writer.TransferCloudAgentResourceLeases(task.UserID, "other", "task-media:"+task.ID, "run", time.Now())
		},
	} {
		if err := mutate(); !errors.Is(err, gorm.ErrInvalidData) {
			t.Fatalf("%s namespace accepted: %v", name, err)
		}
	}
	if err := svc.saveMediaCheckpoint(&task, local, "register"); err != nil {
		t.Fatal(err)
	}
	assertBlocked()
	if _, empty, err := repository.TaskMediaCheckpointCiphertext(task.MediaRecoveryJSON); err != nil || empty {
		t.Fatalf("nonempty projection marked empty: %v %v", empty, err)
	}
	forged := task
	forged.MediaRecoveryJSON = "task-media:empty:v1:" + ciphertext
	if err := writer.Save(&forged); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("raw marker forgery accepted: %v", err)
	}
	task.Status = model.TaskStatusFailed
	if err := writer.SaveTaskCompletion(&task, model.TaskStatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.ReleaseExpiredCloudAgentResourceLeases(time.Now().Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertBlocked()
	before = publicationRows(t, db) + referenceAdmissionRows(t, db, "cloud_agent_resource_leases")
	stale = task
	stale.MediaRecoveryJSON = "stale-ciphertext"
	if err := writer.RequeueTaskMediaRecovery(&stale, ciphertext, nil); !errors.Is(err, repository.ErrTaskStateConflict) {
		t.Fatalf("requeue CAS: %v", err)
	}
	if after := publicationRows(t, db) + referenceAdmissionRows(t, db, "cloud_agent_resource_leases"); after != before {
		t.Fatal("failed requeue changed projection or leases")
	}
	if err := writer.RequeueTaskMediaRecovery(&task, ciphertext, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := writer.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	unwrapped, empty, err := repository.TaskMediaCheckpointCiphertext(stored.MediaRecoveryJSON)
	if err != nil || !empty || unwrapped != ciphertext {
		t.Fatal("requeue changed ciphertext or lost marker")
	}
	if decoded, err := svc.decodeMediaCheckpoint(stored); err != nil || decoded.Items[0].Reference.URL != remote.Items[0].Reference.URL {
		t.Fatalf("requeue decode: %+v %v", decoded, err)
	}
	stored.Status, stored.MediaRecoveryJSON = model.TaskStatusSucceeded, ""
	if err := writer.SaveTaskCompletion(stored, model.TaskStatusQueued, nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.ReleaseExpiredCloudAgentResourceLeases(time.Now().Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.CloudAgentResourceLease{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("orphan projection rows: %d %v", count, err)
	}
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
		t.Fatalf("cleared checkpoint retained resource: %v", err)
	}
}

func TestResourcePublicationMalformedEmptyCheckpoint(t *testing.T) {
	for _, raw := range []string{"task-media:empty:v1:", "task-media:empty:v1:plain", "task-media:empty:v1:enc:v1:bad!", "task-media:empty:v1:enc:v1:YQ", "task-media:empty:v2:enc:v1:any", "task-media:empty:v1:task-media:empty:v1:enc:v1:any"} {
		t.Run(raw, func(t *testing.T) {
			db, other := deletionConcurrencyDBs(t)
			repo, writer := repository.New(db), repository.New(other)
			resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
			task := model.Task{ID: "malformed", UserID: "user-1", Status: model.TaskStatusFailed, MediaRecoveryJSON: raw}
			for _, row := range []any{&resource, &task} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
				t.Fatal("malformed marker authorized deletion")
			}
			svc := New(writer, t.TempDir())
			if _, err := svc.decodeMediaCheckpoint(&task); err == nil {
				t.Fatal("malformed marker decoded")
			}
		})
	}
}

func TestResourcePublicationCheckpointRollbackAndLegacy(t *testing.T) {
	db, other := deletionConcurrencyDBs(t)
	repo, writer := repository.New(db), repository.New(other)
	resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
	task := model.Task{ID: "checkpoint-task", UserID: "user-1", Status: model.TaskStatusFailed, MediaRecoveryJSON: "encrypted-legacy"}
	for _, row := range []any{&resource, &task} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
		t.Fatal("unprojected encrypted checkpoint must fail closed")
	}
	before := referenceAdmissionRows(t, db, "tasks") + referenceAdmissionRows(t, db, "cloud_agent_resource_leases")
	if err := writer.SaveTaskMediaCheckpoint(&task, "new-encrypted", "register", []string{resource.ID}); !errors.Is(err, repository.ErrTaskStateConflict) {
		t.Fatalf("CAS %v", err)
	}
	if after := referenceAdmissionRows(t, db, "tasks") + referenceAdmissionRows(t, db, "cloud_agent_resource_leases"); after != before {
		t.Fatal("failed CAS changed checkpoint/leases")
	}
	copy := task
	copy.MediaRecoveryJSON = "raw-unprojected"
	if err := writer.Save(&copy); !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("raw checkpoint bypass: %v", err)
	}
	if err := writer.RequeueTaskMediaRecovery(&task, "requeued", []string{resource.ID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err == nil {
		t.Fatal("requeued checkpoint missing reference")
	}
	if err := db.Delete(&model.Task{}, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := writer.ReleaseExpiredCloudAgentResourceLeases(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
		t.Fatalf("removed task lease not cleaned: %v", err)
	}
}

func TestResourcePublicationRegistrationNotifications(t *testing.T) {
	db, other := deletionConcurrencyDBs(t)
	repo := repository.New(db)
	resource := model.Resource{ID: "publication", UserID: "user-1", Provider: "local", ObjectKey: "publication", Status: model.ResourceStatusReady}
	task := model.Task{ID: "notify-task", UserID: "user-1", Status: model.TaskStatusRunning, InputJSON: `{}`, ResultJSON: `{}`}
	for _, row := range []any{&resource, &task} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	preparePublicationDeletion(t, repo, resource)
	notified := 0
	writer := repository.New(other).WithCanvasChangeNotifier(func(string, string) { notified++ })
	next := task
	next.Status = model.TaskStatusSucceeded
	err := writer.SaveTaskCompletionWithRegistration(&next, task.Status, nil, func(tx *repository.Repository) error {
		if err := tx.UpsertCanvasProject(&model.CanvasProject{ID: "notify-canvas", UserID: "user-1", PayloadJSON: `{}`}); err != nil {
			return err
		}
		next.ResultJSON = `{"resourceId":"publication"}`
		return nil
	})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("want rejection, got %v", err)
	}
	if notified != 0 {
		t.Fatal("rollback notified canvas")
	}
	var count int64
	db.Model(&model.CanvasProject{}).Count(&count)
	if count != 0 {
		t.Fatal("registration partially committed")
	}
	next = task
	next.Status = model.TaskStatusSucceeded
	if err := writer.SaveTaskCompletionWithRegistration(&next, task.Status, nil, func(tx *repository.Repository) error {
		return tx.UpsertCanvasProject(&model.CanvasProject{ID: "notify-canvas", UserID: "user-1", PayloadJSON: `{}`})
	}); err != nil {
		t.Fatal(err)
	}
	if notified != 1 {
		t.Fatalf("want one committed notification, got %d", notified)
	}
}

func preparePublicationDeletion(t *testing.T, repo *repository.Repository, resource model.Resource) {
	t.Helper()
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextResourceDeletionJob("publication", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim %v %v", job, err)
	}
	if _, _, err := repo.PrepareResourceDeletion(job); err != nil {
		t.Fatal(err)
	}
}
func publicationRows(t *testing.T, db *gorm.DB) string {
	t.Helper()
	out := ""
	for _, table := range []string{"tasks", "results", "task_logs", "system_settings"} {
		out += referenceAdmissionRows(t, db, table)
	}
	return out
}
