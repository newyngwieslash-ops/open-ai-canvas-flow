package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// Prepare has committed, but physical deletion has not started. Writers use an independent connection.
func TestReferenceAdmissionAfterPrepare(t *testing.T) {
	db, other := deletionConcurrencyDBs(t)
	repo, writer := repository.New(db), repository.New(other)
	resource := model.Resource{ID: "admission-resource", UserID: "user-1", Provider: "local", ObjectKey: "admission-object", Status: model.ResourceStatusReady}
	path := writeClosedDeletionFixtureResource(t, &Service{repo: repo, dataDir: t.TempDir()}, &resource)
	project := model.Project{ID: "admission-project", UserID: "user-1", Name: "before", StyleProfileJSON: `{}`, Revision: 1}
	canvas := model.CanvasProject{ID: "admission-canvas", UserID: "user-1", ProjectID: project.ID, Title: "before", PayloadJSON: `{}`, Revision: 1}
	style := model.StyleProfile{ID: "admission-style", UserID: "user-1", Name: "before", ProfileJSON: `{}`}
	run := model.CreationRun{ID: "admission-run", UserID: "user-1", ClientKey: "before", StateJSON: `{}`, ApprovedOperationsJSON: `[]`}
	task := model.Task{ID: "admission-task", UserID: "user-1", Status: model.TaskStatusFailed, InputJSON: `{}`}
	instance := model.WorkflowInstance{ID: "admission-workflow", ProjectID: project.ID}
	step := model.WorkflowStepInstance{ID: "admission-step", WorkflowInstanceID: instance.ID, OutputJSON: `{}`}
	tool := model.WorkbenchTool{ID: "existing-tool", Name: "before"}
	expiredLease := model.CloudAgentResourceLease{UserID: "user-1", RunID: "old-run", OwnerID: "expired-owner", ResourceID: resource.ID, ExpiresAt: time.Now().Add(-time.Hour)}
	for _, row := range []any{&project, &canvas, &style, &run, &task, &instance, &step, &tool, &expiredLease} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteDetachedResources([]model.Resource{resource}, nil); err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextResourceDeletionJob("admission", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim: %v %v", job, err)
	}
	if _, _, err := repo.PrepareResourceDeletion(job); err != nil {
		t.Fatal(err)
	}
	raw := `{"nested":{"resourceId":"admission-resource"}}`
	url := "/api/resources/admission-resource"
	cases := []struct {
		name  string
		table string
		write func() error
	}{
		{"canvas-create", "canvas_projects", func() error {
			return writer.UpsertCanvasProject(&model.CanvasProject{ID: "new-canvas", UserID: "user-1", PayloadJSON: raw})
		}},
		{"canvas-update", "canvas_projects", func() error { v := canvas; v.PayloadJSON = raw; return writer.UpsertCanvasProject(&v) }},
		{"canvas-unassign", "canvas_projects", func() error {
			return writer.UnassignCanvasFromProject("user-1", project.ID, canvas.ID, raw, time.Now(), 1)
		}},
		{"project-delete-rewrite", "projects", func() error {
			v := canvas
			v.PayloadJSON = raw
			return writer.DeleteProject("user-1", project.ID, []model.CanvasProject{v})
		}},
		{"project-create-cover", "projects", func() error {
			return writer.CreateProject(&model.Project{ID: "new-project", UserID: "user-1", Name: "new", CoverResourceID: resource.ID})
		}},
		{"project-update-cover", "projects", func() error { v := project; v.CoverResourceID = resource.ID; return writer.UpdateProject(&v) }},
		{"project-update-style", "projects", func() error { v := project; v.StyleProfileJSON = raw; return writer.UpdateProject(&v) }},
		{"style-create-cover", "style_profiles", func() error {
			return writer.CreateStyleProfile(&model.StyleProfile{ID: "new-style", UserID: "user-1", CoverURL: url})
		}},
		{"style-update-json", "style_profiles", func() error { v := style; v.ProfileJSON = raw; return writer.UpdateStyleProfile(&v) }},
		{"creation-create", "creation_runs", func() error {
			return writer.CreateCreationRun(&model.CreationRun{ID: "new-run", UserID: "user-1", ClientKey: "new", StateJSON: raw})
		}},
		{"creation-mutate", "creation_runs", func() error {
			return writer.MutateCreationRun("user-1", run.ID, func(v *model.CreationRun, _ *repository.Repository) error { v.ApprovedOperationsJSON = raw; return nil })
		}},
		{"creation-submission", "creation_submissions", func() error {
			return writer.SaveCreationSubmission(&model.CreationSubmission{ID: "new-submission", UserID: "user-1", RunID: run.ID, RequestJSON: raw})
		}},
		{"lease-upsert", "cloud_agent_resource_leases", func() error {
			return writer.UpsertCloudAgentResourceLeases("user-1", "run", "owner", []string{resource.ID}, time.Now().Add(time.Hour))
		}},
		{"lease-replace", "cloud_agent_resource_leases", func() error {
			return writer.ReplaceCloudAgentResourceLeases("user-1", "run", "owner", []string{resource.ID}, time.Now().Add(time.Hour))
		}},
		{"task-create", "tasks", func() error {
			return writer.CreateTaskWithActiveLimit(&model.Task{ID: "new-task", UserID: "user-1", InputJSON: raw}, 100)
		}},
		{"task-paid-create", "tasks", func() error {
			return writer.CreateTaskWithCreditReservation(&model.Task{ID: "new-paid-task", UserID: "user-1", InputJSON: raw}, &model.BillingOrder{ID: "late-order", UserID: "user-1"}, 100)
		}},
		{"task-retry", "tasks", func() error {
			v := task
			v.InputJSON = raw
			_, err := writer.RetryTaskWithBilling("user-1", &v, nil, 100)
			return err
		}},
		{"tool-create", "tools", func() error {
			_, err := writer.CreateTool(&model.Tool{OwnerID: "user-1", Label: "late", Cover: url})
			return err
		}},
		{"workbench-create", "workbench_tools", func() error {
			return writer.CreateWorkbenchTool(&model.WorkbenchTool{ID: "new-tool", Name: "late", CoverResourceID: resource.ID, Enabled: true})
		}},
		{"settings-save", "system_settings", func() error { return writer.SaveSystemSetting(&model.SystemSetting{Key: "admission", ValueJSON: raw}) }},
		{"voice-create", "voice_profiles", func() error {
			return writer.CreateVoiceProfile(&model.VoiceProfile{ID: "new-voice", UserID: "user-1", SampleResourceID: resource.ID})
		}},
		{"candidate-create", "project_asset_candidates", func() error {
			_, err := writer.CreateProjectAssetCandidate(&model.ProjectAssetCandidate{ID: "new-candidate", ProjectID: project.ID, DetailsJSON: raw})
			return err
		}},
		{"artifact-create", "shot_artifacts", func() error {
			return writer.CreateShotArtifact(&model.ShotArtifact{ID: "new-artifact", ProjectID: project.ID, ResourceID: resource.ID})
		}},
		{"workflow-step", "workflow_step_instances", func() error { v := step; v.OutputJSON = raw; progress := instance; progress.Revision++; return writer.UpdateWorkflowProgress(&v, nil, &progress, project.ID) }},
		{"workflow-progress", "workflow_step_instances", func() error {
			v := step
			v.OutputJSON = raw
			nextInstance := instance
			nextInstance.Revision++
			return writer.UpdateWorkflowProgress(&v, nil, &nextInstance, project.ID)
		}},
		{"workflow-create", "workflow_instances", func() error {
			return writer.CreateWorkflowInstance(&model.WorkflowInstance{ID: "new-workflow", ProjectID: project.ID, UnitID: "new-unit"}, []model.WorkflowStepInstance{{ID: "new-step", WorkflowInstanceID: "new-workflow", InputJSON: raw}})
		}},
		{"lease-transfer", "cloud_agent_resource_leases", func() error {
			return writer.TransferCloudAgentResourceLeases("user-1", "expired-owner", "new-owner", "run", time.Now().Add(time.Hour))
		}},
		{"workbench-save", "workbench_tools", func() error { v := tool; v.IconResourceID = resource.ID; return writer.SaveWorkbenchTool(&v) }},
		{"workbench-save-presets", "workbench_tools", func() error {
			v := tool
			v.CoverResourceID = resource.ID
			return writer.SaveWorkbenchToolWithPresets(&v, nil, false)
		}},
		{"workbench-if-missing", "workbench_tools", func() error {
			return writer.CreateWorkbenchToolIfMissing(&model.WorkbenchTool{ID: "missing-tool", Name: "late", IconResourceID: resource.ID})
		}},
		{"appearance-save", "system_settings", func() error {
			return writer.SaveSystemSetting(&model.SystemSetting{Key: "appearance", ValueJSON: `{"logoResourceId":"admission-resource"}`})
		}},
		{"voice-ensure", "voice_profiles", func() error {
			return writer.EnsureVoiceProfiles([]model.VoiceProfile{{ID: "ensured-voice", UserID: "user-1", SampleResourceID: resource.ID}})
		}},
		{"character-create", "assets", func() error {
			return writer.CreateUserCharacter(&model.Asset{ID: "late-character", UserID: "user-1", PayloadJSON: raw}, &model.AssetVersion{ID: "late-character-version", AssetID: "late-character", DefinitionJSON: `{}`}, nil, nil)
		}},
		{"character-project-create", "assets", func() error {
			return writer.CreateProjectCharacter(project.ID, &model.Asset{ID: "late-project-character", UserID: "user-1", PayloadJSON: raw}, &model.AssetVersion{ID: "late-project-character-version", AssetID: "late-project-character", DefinitionJSON: `{}`}, &model.ProjectAssetLink{ID: "late-character-link", ProjectID: project.ID, AssetID: "late-project-character"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := referenceAdmissionRows(t, db, tc.table)
			err := tc.write()
			if err == nil {
				t.Errorf("late reference accepted after Prepare")
			} else if !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, repository.ErrResourceDeleting) {
				t.Errorf("unexpected rejection (not ready check): %v", err)
			}
			if after := referenceAdmissionRows(t, db, tc.table); after != before {
				t.Errorf("rejected write changed %s rows", tc.table)
			}
		})
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("late writers changed physical object: %q %v", data, err)
	}
}

func TestReferenceAdmissionStaleAssetAfterPrepare(t *testing.T) {
	db, other := deletionConcurrencyDBs(t)
	repo, writer := repository.New(db), repository.New(other)
	resource := model.Resource{ID: "stale-resource", UserID: "user-1", Provider: "local", ObjectKey: "stale-object", Status: model.ResourceStatusReady}
	path := writeClosedDeletionFixtureResource(t, &Service{repo: repo, dataDir: t.TempDir()}, &resource)
	asset := model.Asset{ID: "stale-asset", UserID: "user-1", Status: model.AssetVersionStatusConfirmed, PayloadJSON: `{"resourceId":"stale-resource"}`}
	for _, row := range []any{&asset, &model.Project{ID: "project", UserID: "user-1", Name: "stale-link-project", Revision: 1}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteAsset("user-1", asset.ID); err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextResourceDeletionJob("stale", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim: %v %v", job, err)
	}
	if _, _, err := repo.PrepareResourceDeletion(job); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		write func() error
	}{
		{"domain", func() error { return writer.UpdateAssetDomain(&asset) }},
		{"link", func() error {
			_, err := writer.LinkProjectAsset(&asset, nil, &model.ProjectAssetLink{ID: "late-link", ProjectID: "project", AssetID: asset.ID})
			return err
		}},
		{"version", func() error {
			return writer.CreateAssetVersion(&model.AssetVersion{ID: "late-version", AssetID: asset.ID, DefinitionJSON: `{}`})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := referenceAdmissionRows(t, db, "assets") + referenceAdmissionRows(t, db, "project_asset_links") + referenceAdmissionRows(t, db, "asset_versions") + referenceAdmissionRows(t, db, "projects")
			if err := tc.write(); !errors.Is(err, repository.ErrResourceDeleting) {
				t.Fatalf("want deleting, got %v", err)
			}
			after := referenceAdmissionRows(t, db, "assets") + referenceAdmissionRows(t, db, "project_asset_links") + referenceAdmissionRows(t, db, "asset_versions") + referenceAdmissionRows(t, db, "projects")
			if before != after {
				t.Fatal("late write mutated asset/link/version")
			}
		})
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("stale writers changed physical object: %q %v", data, err)
	}
}

func TestReferenceAdmissionRaceSerialized(t *testing.T) {
	for _, first := range []string{"delete", "writer"} {
		t.Run(first, func(t *testing.T) {
			db, other := deletionConcurrencyDBs(t)
			repo, writer := repository.New(db), repository.New(other)
			resource := model.Resource{ID: "race-admission", UserID: "user-1", Provider: "local", ObjectKey: "race-admission", Status: model.ResourceStatusReady}
			path := writeClosedDeletionFixtureResource(t, &Service{repo: repo, dataDir: t.TempDir()}, &resource)
			canvas := model.CanvasProject{ID: "race-canvas", UserID: "user-1", PayloadJSON: `{"resourceId":"race-admission"}`}
			admitted, release := make(chan struct{}), make(chan struct{})
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() {
				// The test's outer transaction must follow owner -> lifecycle too;
				// taking lifecycle first would exercise an intentionally rejected caller.
				firstDone <- repo.WithStoryboardAssetOwners(context.Background(), []string{resource.UserID}, func(owned *repository.Repository) error {
					return owned.ResourceLifecycleTransaction(func(tx *repository.Repository) error {
						var err error
						if first == "delete" {
							err = tx.DeleteDetachedResources([]model.Resource{resource}, nil)
						} else {
							err = tx.UpsertCanvasProject(&canvas)
						}
						close(admitted)
						<-release
						return err
					})
				})
			}()
			select {
			case <-admitted:
			case err := <-firstDone:
				t.Fatalf("winner failed before admission: %v", err)
			case <-time.After(10 * time.Second):
				close(release)
				t.Fatal("winner never reached admission")
			}
			contending := make(chan struct{})
			go func() {
				close(contending)
				if first == "delete" {
					copy := canvas
					secondDone <- writer.UpsertCanvasProject(&copy)
				} else {
					secondDone <- writer.DeleteDetachedResources([]model.Resource{resource}, nil)
				}
			}()
			<-contending
			select {
			case err := <-secondDone:
				close(release)
				<-firstDone
				t.Fatalf("contender bypassed admission lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			if err := <-firstDone; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-secondDone:
				if first == "delete" {
					if !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, repository.ErrResourceDeleting) {
						t.Fatalf("late writer must reject unavailable resource, got %v", err)
					}
				} else {
					var referenced *repository.ResourceDeletionReferencedError
					if !errors.As(err, &referenced) {
						t.Fatalf("losing deletion must reject committed reference, got %v", err)
					}
				}
			case <-time.After(10 * time.Second):
				t.Fatal("admission deadlocked")
			}
			var count int64
			if err := db.Model(&model.CanvasProject{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if first == "delete" && count != 0 {
				t.Fatal("late canvas persisted")
			}
			if first == "writer" && count != 1 {
				t.Fatal("winning canvas lost")
			}
			stored, err := repo.Resource(resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantIntents := model.ResourceStatusReady, int64(0)
			if first == "delete" {
				wantStatus, wantIntents = model.ResourceStatusDeleting, 1
			} else {
				var saved model.CanvasProject
				if err := db.First(&saved, "id = ?", canvas.ID).Error; err != nil || saved.PayloadJSON != canvas.PayloadJSON {
					t.Fatalf("winning reference changed: %+v %v", saved, err)
				}
			}
			if stored.Status != wantStatus {
				t.Fatalf("resource status = %s, want %s", stored.Status, wantStatus)
			}
			for _, row := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}} {
				if err := db.Model(row).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != wantIntents {
					t.Fatalf("%T count = %d, want %d; losing deletion must not persist work", row, count, wantIntents)
				}
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
				t.Fatalf("admission race changed physical object: %q %v", data, err)
			}
		})
	}
}

func referenceAdmissionRows(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var rows []map[string]any
	if err := db.Table(table).Order("1").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
