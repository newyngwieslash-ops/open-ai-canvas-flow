package app

import (
	"os"
	"path/filepath"
	"testing"
	"yingce/backend/internal/model"
)

func TestDeletionLifecycleAnnouncementDiscardStagesResource(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	row := model.Resource{ID: "draft-delete", UserID: "user-1", Provider: "local", ObjectKey: "draft", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(dir, "resources", row.ObjectKey), 0700); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &row)
	draft := model.AnnouncementImageDraft{ResourceID: row.ID, UserID: row.UserID}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DiscardAnnouncementImageDraft(row.UserID, &row, nil); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	stored, err := svc.repo.Resource(row.ID)
	if err != nil || stored.Status != model.ResourceStatusDeleting {
		t.Fatalf("discard lost resource before physical success: %v %v", stored, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "resources", row.ObjectKey)); err != nil || !info.IsDir() {
		t.Fatalf("failed physical object lost: %v %v", info, err)
	}
	var jobs int64
	if err := db.Model(&model.ResourceDeletionJob{}).Count(&jobs).Error; err != nil || jobs != 1 {
		t.Fatalf("failed deletion lost its retry job: %d %v", jobs, err)
	}
}

func TestDeletionLifecycleAssetUpsertAndReplaceCannotResurrect(t *testing.T) {
	for _, method := range []string{"upsert", "replace"} {
		t.Run(method, func(t *testing.T) {
			svc, db, _ := newResourceDeletionTestService(t)
			asset := model.Asset{ID: "deleting-asset", UserID: "user-1", Status: model.AssetVersionStatusDeleting, PayloadJSON: `{}`}
			if err := db.Create(&asset).Error; err != nil {
				t.Fatal(err)
			}
			asset.Status = model.AssetVersionStatusConfirmed
			var err error
			if method == "upsert" {
				err = svc.repo.UpsertAsset(&asset)
			} else {
				err = svc.repo.ReplaceAssets(asset.UserID, []model.Asset{asset})
			}
			if err == nil {
				t.Fatalf("%s accepted deleting asset", method)
			}
		})
	}
}

func TestDeletionLifecycleDirectDeleteAssetCollectsResources(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	row := model.Resource{ID: "direct-resource", UserID: "user-1", Provider: "local", ObjectKey: "direct", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(dir, "resources", row.ObjectKey), 0700); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &row)
	asset := model.Asset{ID: "direct-asset", UserID: row.UserID, PayloadJSON: `{"resourceId":"direct-resource"}`}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteAsset(asset.UserID, asset.ID); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	stored, err := svc.repo.AssetForUser(asset.UserID, asset.ID)
	if err != nil || stored.Status != model.AssetVersionStatusDeleting {
		t.Fatalf("direct delete skipped physical retention: %v %v", stored, err)
	}
	resource, err := svc.repo.Resource(row.ID)
	if err != nil || resource.Status != model.ResourceStatusDeleting {
		t.Fatalf("direct delete lost resource before physical success: %v %v", resource, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "resources", row.ObjectKey)); err != nil || !info.IsDir() {
		t.Fatalf("failed physical object lost: %v %v", info, err)
	}
	var jobs int64
	if err := db.Model(&model.ResourceDeletionJob{}).Count(&jobs).Error; err != nil || jobs != 1 {
		t.Fatalf("failed deletion lost its retry job: %d %v", jobs, err)
	}
}
