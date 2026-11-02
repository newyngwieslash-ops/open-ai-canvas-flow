package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// Both operations enter their real public transaction owner. Only an observation
// callback pauses a successfully acquired lifecycle lock; no transaction wrapper
// or test-only production bypass changes their lock order.
func TestProfileSourceDeletionAdmissionRace(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, operation := range []string{"create", "complete", "adopt"} {
				for _, first := range []string{"reference", "delete"} {
					t.Run(operation+"/"+first+"_locks_first", func(t *testing.T) {
						databases := profileConcurrencyDatabases(t, dialect)
						db := databases[0]
						if err := db.AutoMigrate(database.Models()...); err != nil {
							t.Fatal(err)
						}
						services := [2]*Service{{repo: repository.New(db), dataDir: t.TempDir()}, {repo: repository.New(databases[1]), dataDir: t.TempDir()}}
						owner := "source-race-user"
						project := model.Project{ID: newID(), UserID: owner, Name: "source race", Status: model.ProjectStatusActive}
						resource := model.Resource{ID: newID(), UserID: owner, Provider: "local", ObjectKey: "source-" + newID() + ".png", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 32, Height: 32, Size: 100}
						path := filepath.Join(services[0].dataDir, "resources", resource.ObjectKey)
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte(strings.Repeat("x", int(resource.Size))), 0600); err != nil {
							t.Fatal(err)
						}
						createClosedLocalFixtureResource(t, services[0], &resource)
						capability := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
						capability.Text.References.MaxImages, capability.Text.References.MaxImageBytes = 1, 100
						for _, row := range []any{&project, &model.ModelChannel{ID: "source-channel", Scope: model.ChannelScopeSystem, Enabled: true}, &model.ChannelModel{ID: "source-cm", ChannelID: "source-channel", ModelKey: "text-test", Capability: "text", Protocol: model.ChannelInterfaceChatCompletion, CapabilityConfigJSON: mustEncodeModelCapabilityConfig(t, capability), Enabled: true, PriceConfigured: true, BillingMode: "fixed_request", UnitPriceMicrocredits: 100}, &model.ChannelModelPriceTier{ID: "source-tier", ChannelModelID: "source-cm", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", UnitPriceMicrocredits: 100, Enabled: true, PriceConfigured: true}, &model.CreditAccount{UserID: owner, AvailableMicrocredits: 10000}} {
							if err := db.Create(row).Error; err != nil {
								t.Fatal(err)
							}
						}
						asset, err := services[0].CreateProjectAssetEntity(owner, project.ID, CreateProjectAssetEntityRequest{Name: "source race asset", Category: model.AssetCategoryProp, Definition: map[string]any{"description": "old", "prompt": ""}, ReferenceImages: []ProjectReferenceImage{{resource.ID}}})
						if err != nil {
							t.Fatal(err)
						}
						binding := ProfileAnalysisBinding{1, project.ID, asset.ID, asset.Category, asset.PrimaryVersionID, ProjectReferenceImage{resource.ID}, 1}
						input := profileMatrixJSON(t, map[string]any{"metadata": map[string]any{"operation": profileAnalysisOperation, "profileAnalysis": binding}})
						result := profileMatrixJSON(t, map[string]any{"mode": "text", "assetProfileAnalysis": ProfileAnalysisResult{SchemaVersion: 1, AssetID: asset.ID, Category: asset.Category, BaseVersionID: asset.PrimaryVersionID, Source: binding.Source, Draft: map[string]string{"description": "visible", "prompt": ""}}})
						task := model.Task{ID: newID(), UserID: owner, ProjectID: project.ID, Type: "canvas_text", Operation: profileAnalysisOperation, Status: model.TaskStatusRunning, InputJSON: input, CreatedAt: time.Now(), UpdatedAt: time.Now()}
						if operation == "adopt" {
							task.Status = model.TaskStatusSucceeded
							task.ResultJSON = result
						}
						if operation != "create" {
							if err := db.Create(&task).Error; err != nil {
								t.Fatal(err)
							}
						}
						var producedID string
						write := func() error {
							switch operation {
							case "create":
								view, err := services[0].CreateProjectProfileAnalysis(owner, project.ID, asset.ID, CreateProfileAnalysisRequest{asset.PrimaryVersionID, resource.ID, ProfileAnalysisModelSelection{Kind: "system", ChannelID: "source-channel", ModelKey: "text-test"}})
								producedID = view.TaskID
								return err
							case "complete":
								err := services[0].saveTaskCompletionWithinStorageQuota(&task, []byte(result), nil, false)
								producedID = task.ID
								return err
							default:
								response, err := services[0].AdoptProjectProfileAnalysis(owner, project.ID, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "source-race-key-0001"})
								producedID = response.AdoptedVersionID
								return err
							}
						}
						remove := func() error { return services[1].repo.DeleteDetachedResources([]model.Resource{resource}, nil) }
						admitted, release := make(chan struct{}), make(chan struct{})
						var pauseOnce, releaseOnce sync.Once
						unblock := func() { releaseOnce.Do(func() { close(release) }) }
						defer unblock()
						firstIndex := 0
						firstAction, secondAction := write, remove
						if first == "delete" {
							firstIndex = 1
							firstAction, secondAction = remove, write
						}
						isLifecycle := func(tx *gorm.DB) bool {
							sql := tx.Statement.SQL.String()
							return sql == "UPDATE resources SET status = status WHERE 1 = 0" || (strings.Contains(sql, "pg_advisory_xact_lock") && len(tx.Statement.Vars) == 1 && tx.Statement.Vars[0] == "resource-lifecycle")
						}
						if err := databases[firstIndex].Callback().Raw().After("gorm:raw").Register("profile_source_hold", func(tx *gorm.DB) {
							if tx.Error == nil && isLifecycle(tx) {
								pauseOnce.Do(func() { close(admitted); <-release })
							}
						}); err != nil {
							t.Fatal(err)
						}
						defer databases[firstIndex].Callback().Raw().Remove("profile_source_hold")
						firstDone, secondDone := make(chan error, 1), make(chan error, 1)
						go func() { firstDone <- firstAction() }()
						select {
						case <-admitted:
						case err := <-firstDone:
							t.Fatalf("first operation failed before barrier: %v", err)
						case <-time.After(10 * time.Second):
							t.Fatal("lifecycle admission timed out")
						}
						attempting := make(chan struct{})
						var attemptOnce sync.Once
						if err := databases[1-firstIndex].Callback().Raw().Before("gorm:raw").Register("profile_source_contender", func(tx *gorm.DB) {
							sql := tx.Statement.SQL.String()
							if isLifecycle(tx) || strings.Contains(sql, "pg_advisory_xact_lock") || sql == "UPDATE assets SET id = id WHERE 1 = 0" {
								attemptOnce.Do(func() { close(attempting) })
							}
						}); err != nil {
							t.Fatal(err)
						}
						defer databases[1-firstIndex].Callback().Raw().Remove("profile_source_contender")
						go func() { secondDone <- secondAction() }()
						select {
						case <-attempting:
						case err := <-secondDone:
							unblock()
							<-firstDone
							t.Fatalf("contender failed before lock: %v", err)
						case <-time.After(10 * time.Second):
							t.Fatal("contender did not reach lock")
						}
						select {
						case err := <-secondDone:
							unblock()
							<-firstDone
							t.Fatalf("contender bypassed real transaction: %v", err)
						case <-time.After(100 * time.Millisecond):
						}
						unblock()
						firstErr := <-firstDone
						var secondErr error
						select {
						case secondErr = <-secondDone:
						case <-time.After(10 * time.Second):
							t.Fatal("contender did not settle")
						}
						writeErr, deleteErr := firstErr, secondErr
						if first == "delete" {
							writeErr, deleteErr = secondErr, firstErr
						}
						if writeErr != nil {
							t.Fatalf("real %s writer: %v", operation, writeErr)
						}
						if !errors.Is(deleteErr, repository.ErrOriginalImageReferenced) {
							t.Fatalf("source deletion must reject: %v", deleteErr)
						}
						stored, err := services[0].repo.Resource(resource.ID)
						if err != nil || stored.Status != model.ResourceStatusReady {
							t.Fatalf("source lost or changed: %+v %v", stored, err)
						}
						for _, row := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}} {
							var count int64
							if err := db.Model(row).Count(&count).Error; err != nil {
								t.Fatal(err)
							}
							if count != 0 {
								t.Fatalf("rejected deletion persisted %d %T", count, row)
							}
						}
						// This is a separate scanner assertion AFTER the legal race has settled.
						// It must not be confused with a race requiring an absent/invalid baseline.
						if err := db.Model(&model.AssetVersion{}).Where("id = ?", asset.PrimaryVersionID).Update("definition_json", `{"description":"old","prompt":""}`).Error; err != nil {
							t.Fatal(err)
						}
						if err := db.Model(&model.Asset{}).Where("id = ?", asset.ID).Update("payload_json", `{}`).Error; err != nil {
							t.Fatal(err)
						}
						if operation == "complete" {
							if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("input_json", `{}`).Error; err != nil {
								t.Fatal(err)
							}
						}
						if operation == "adopt" {
							if err := db.Delete(&model.Task{}, "id = ?", task.ID).Error; err != nil {
								t.Fatal(err)
							}
							var next model.AssetVersion
							if err := db.First(&next, "id = ?", producedID).Error; err != nil {
								t.Fatal(err)
							}
							var definition map[string]json.RawMessage
							if err := json.Unmarshal([]byte(next.DefinitionJSON), &definition); err != nil {
								t.Fatal(err)
							}
							delete(definition, "referenceImage")
							if err := db.Model(&next).Update("definition_json", profileMatrixJSON(t, definition)).Error; err != nil {
								t.Fatal(err)
							}
						}
						snapshot, err := services[0].repo.ResourceReferenceSnapshot(owner, "", []string{resource.ID})
						if err != nil {
							t.Fatal(err)
						}
						refs := adminResourceReferences(snapshot, []model.Resource{resource})[resource.ID]
						if len(refs) != 1 || refs[0].ID != producedID {
							t.Fatalf("new source not isolated: %+v want %s", refs, producedID)
						}
						if err := remove(); !errors.Is(err, repository.ErrOriginalImageReferenced) {
							t.Fatalf("isolated produced source ignored: %v", err)
						}
						if data, err := os.ReadFile(path); err != nil || string(data) != strings.Repeat("x", int(resource.Size)) {
							t.Fatalf("protected source bytes changed: %q %v", data, err)
						}
						t.Logf("%s %s %s locks first: real writer succeeds, deletion refuses; isolated produced source also protects; no physical deletion", dialect, operation, first)
					})
				}
			}
		})
	}
}
