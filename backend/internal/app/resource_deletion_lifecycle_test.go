package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"yingce/backend/internal/model"
)

func TestDeletionLifecyclePurgeProtectsCanvas(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	resource := model.Resource{ID: "strict-resource", UserID: "user-1", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "strict.png"}
	asset := model.Asset{ID: "strict-asset", UserID: "user-1", PayloadJSON: `{"resourceId":"strict-resource"}`}
	canvas := model.CanvasProject{ID: "strict-canvas", UserID: "user-1", Title: "live canvas", PayloadJSON: `{"resourceId":"strict-resource"}`}
	for _, row := range []any{&resource, &asset, &canvas} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.PurgeUserAsset("user-1", asset.ID); err == nil || !strings.Contains(err.Error(), "live canvas") {
		t.Fatalf("purge must return reference source; got %v", err)
	}
}

func TestDeletionLifecycleFailureRetainsWholeAsset(t *testing.T) {
	svc, db, dataDir := newResourceDeletionTestService(t)
	// A directory cannot be removed as a resource object: actual physical failure, no mock.
	if err := os.MkdirAll(filepath.Join(dataDir, "resources", "blocked"), 0700); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "retain-resource", UserID: "user-1", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "blocked"}
	createClosedLocalFixtureResource(t, svc, &resource)
	asset := model.Asset{ID: "retain-asset", UserID: "user-1", PrimaryVersionID: "retain-version", PayloadJSON: `{"resourceId":"retain-resource"}`}
	version := model.AssetVersion{ID: "retain-version", AssetID: asset.ID, DefinitionJSON: `{}`}
	representation := model.AssetRepresentation{ID: "retain-representation", AssetVersionID: version.ID, ResourceID: resource.ID, Role: "primary"}
	for _, row := range []any{&asset, &version, &representation} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.DeleteUserAsset("user-1", asset.ID); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	for _, row := range []any{&model.Asset{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.Resource{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("physical failure lost %T: count %d", row, count)
		}
	}
	stored, err := svc.repo.Resource(resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Status) != "deleting" {
		t.Fatalf("status = %s", stored.Status)
	}
}

func TestDeletionLifecycleLateSaveCannotResurrect(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	resource := model.Resource{ID: "late", UserID: "user-1", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "late.png"}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&resource).Update("status", "deleting").Error; err != nil {
		t.Fatal(err)
	}
	resource.Status = model.ResourceStatusReady
	if err := svc.repo.SaveResource(&resource); err == nil {
		t.Error("late SaveResource accepted deleting row")
	}
	if err := db.Delete(&model.Resource{}, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveResource(&resource); err == nil {
		t.Error("late SaveResource recreated absent row")
	}
}
