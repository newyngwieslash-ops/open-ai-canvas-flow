package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestDeletionLifecycleGlobalReferencesProtectAllEntrypoints(t *testing.T) {
	for _, kind := range []string{"appearance", "announcement"} {
		t.Run(kind, func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			row := model.Resource{ID: "global-image", UserID: "admin-b", Provider: "local", ObjectKey: "global-image", Status: model.ResourceStatusReady}
			path := filepath.Join(dir, "resources", row.ObjectKey)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("global-image"), 0600); err != nil {
				t.Fatal(err)
			}
			createClosedLocalFixtureResource(t, svc, &row)
			if kind == "appearance" {
				if err := db.Create(&model.SystemSetting{Key: "appearance", ValueJSON: `{"logoResourceId":"global-image"}`}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Create(&model.Announcement{ID: "global-announcement", CreatedBy: "admin-a", ImageResourceID: row.ID, Title: "cross admin"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			var referenced *repository.ResourceDeletionReferencedError
			if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); !errors.As(err, &referenced) || len(referenced.References) == 0 {
				t.Fatalf("%s global reference not protected: %v", kind, err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "global-image" {
				t.Fatalf("global reference lost bytes: %q %v", data, err)
			}
		})
	}
}

func TestDeletionLifecyclePendingUploadRetainedAndPlaybackAliasFenced(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	pending := model.Resource{ID: "pending-upload", UserID: "user-1", Provider: "local", ObjectKey: "pending", Status: model.ResourceStatusPending}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{pending}, nil); err == nil {
		t.Error("active upload accepted deletion")
	}
	source := model.Resource{ID: "playback-fence", UserID: "user-1", Provider: "local", ObjectKey: "source", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", source.ObjectKey), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &source)
	op, err := svc.repo.ClaimPlaybackProduction(source.ID)
	if err != nil || op == nil {
		t.Fatalf("playback claim: %v", err)
	}
	if err := svc.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	writeP1Playback(t, dir, source.ID)
	if err := svc.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CompletePlaybackTranscode(source.UserID, source.ID, model.PlaybackStatusReady, "", op); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{source}, nil); err != nil {
		t.Fatal(err)
	}
	alias := model.Resource{ID: "playback-late-alias", UserID: "user-1", Provider: "local", ObjectKey: "different", PlaybackObjectKey: source.ID + ".mp4", Status: model.ResourceStatusReady}
	if err := svc.repo.CreateResource(&alias); err == nil {
		t.Error("late playback alias accepted")
	}
}
