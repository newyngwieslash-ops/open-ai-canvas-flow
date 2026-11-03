package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestDeletionLifecycleProcessingPlaybackCannotBeDeletedEarly(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	row := model.Resource{ID: "producer", UserID: "user-1", Provider: "local", ObjectKey: "producer-source", Status: model.ResourceStatusReady}
	path := filepath.Join(dir, "resources", row.ObjectKey)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &row)
	if err := db.Model(&row).Update("playback_status", model.PlaybackStatusProcessing).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
		t.Fatalf("untracked playback admitted deletion: %v", err)
	}
	svc.drainResourceDeletionJobs(8)
	if data, err := os.ReadFile(path); err != nil || string(data) != "source" {
		t.Fatalf("source changed while playback producer active: %q %v", data, err)
	}
	var count int64
	db.Model(&model.Resource{}).Where("id = ?", row.ID).Count(&count)
	if count != 1 {
		t.Fatal("processing resource finalized before producer settled")
	}
	stored, err := svc.repo.Resource(row.ID)
	if err != nil || stored.Status != model.ResourceStatusReady || stored.PlaybackStatus != model.PlaybackStatusProcessing {
		t.Fatalf("untracked playback state changed: %+v %v", stored, err)
	}
	for _, table := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}} {
		if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("blocked playback created %T: %d %v", table, count, err)
		}
	}
}

func TestDeletionLifecycleCompletedIdentityCannotBeReintroduced(t *testing.T) {
	svc, _, dir := newResourceDeletionTestService(t)
	row := model.Resource{ID: "tombstone", UserID: "user-1", Provider: "local", ObjectKey: "never-reuse", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", row.ObjectKey), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &row)
	if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	row.ID = "new-tombstone-alias"
	if err := svc.repo.CreateResource(&row); err == nil {
		t.Fatal("completed physical identity reused despite potentially stale I/O worker")
	}
}

func TestDeletionLifecycleDelayedUploadCannotBeDeletedBeforeWrite(t *testing.T) {
	svc, _, dir := newResourceDeletionTestService(t)
	started := make(chan model.Resource, 1)
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := svc.storeResourceWithWriter("user-1", "image", "late.png", "image/png", 4, 1, 1, 0, strings.NewReader("late"), nil, true, func(resource *model.Resource, _ string, body io.Reader) (resourceWriteResult, error) {
			started <- *resource
			<-resume
			path := filepath.Join(dir, "resources", resource.ObjectKey)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return resourceWriteResult{}, err
			}
			data, err := io.ReadAll(body)
			if err != nil {
				return resourceWriteResult{}, err
			}
			return resourceWriteResult{ETag: "etag"}, os.WriteFile(path, data, 0600)
		})
		done <- err
	}()
	row := <-started
	stageErr := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil)
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if stageErr == nil {
		t.Fatal("delete was accepted while physical upload paused")
	}
	stored, err := svc.repo.Resource(row.ID)
	if err != nil || stored.Status != model.ResourceStatusReady {
		t.Fatalf("uploaded row lost: %v %v", stored, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", row.ObjectKey)); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionLifecycleLatePlaybackCleansAndSettles(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-untracked-retained", true: "closed-writer-deleted"}[tracked], func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			row := model.Resource{ID: "late-playback", UserID: "user-1", Provider: "local", ObjectKey: "late-original", Status: model.ResourceStatusReady}
			original := filepath.Join(dir, "resources", row.ObjectKey)
			if err := os.MkdirAll(filepath.Dir(original), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(original, []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			createClosedLocalFixtureResource(t, svc, &row)
			var op *model.ResourceProducerOperation
			if tracked {
				var err error
				op, err = svc.repo.ClaimPlaybackProduction(row.ID)
				if err != nil || op == nil {
					t.Fatalf("playback claim: %v", err)
				}
				if err := svc.repo.StartResourceProduction(op); err != nil {
					t.Fatal(err)
				}
			} else if err := db.Model(&row).Update("playback_status", model.PlaybackStatusProcessing).Error; err != nil {
				t.Fatal(err)
			}
			path := writeP1Playback(t, dir, row.ID)
			if tracked {
				if err := svc.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
					t.Fatal(err)
				}
				if err := svc.repo.CompletePlaybackTranscode(row.UserID, row.ID, model.PlaybackStatusReady, "", op); err != nil {
					t.Fatal(err)
				}
			}
			stageErr := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil)
			if tracked && stageErr != nil || !tracked && !errors.Is(stageErr, repository.ErrResourceProducerUnsettled) {
				t.Fatalf("tracked=%v deletion admission: %v", tracked, stageErr)
			}
			// The legacy no-token invocation cannot settle or remove untracked bytes.
			svc.runPlaybackTranscode(row.UserID, row.ID, "unused")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("producer bypassed alias-aware deletion worker: %v", err)
			}
			svc.drainResourceDeletionJobs(8)
			if tracked {
				for _, path := range []string{original, path} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("worker did not clean settled object: %v", err)
					}
				}
				if _, err := svc.repo.Resource(row.ID); err == nil {
					t.Fatal("settled deletion did not finish")
				}
				return
			}
			stored, err := svc.repo.Resource(row.ID)
			if err != nil || stored.Status != model.ResourceStatusReady || stored.PlaybackStatus != model.PlaybackStatusProcessing {
				t.Fatalf("legacy producer incorrectly settled: %+v %v", stored, err)
			}
			if data, err := os.ReadFile(original); err != nil || string(data) != "source" {
				t.Fatalf("legacy original lost: %q %v", data, err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("legacy playback lost: %v", err)
			}
			for _, table := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}} {
				var count int64
				if err := db.Model(table).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("legacy producer created %T: %d %v", table, count, err)
				}
			}
		})
	}
}

func TestDeletionLifecyclePausedOldPhysicalAttemptCannotDestroyReusedIdentity(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	row := model.Resource{ID: "old-physical", UserID: "user-1", Provider: "local", ObjectKey: "old-physical", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", row.ObjectKey), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &row)
	if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
		t.Fatal(err)
	}
	old, err := svc.repo.ClaimNextResourceDeletionJob("worker", time.Minute)
	if err != nil || old == nil {
		t.Fatal(err)
	}
	if _, _, err := svc.repo.PrepareResourceDeletion(old); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceDeletionJob{}).Where("id = ?", old.ID).Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	row.ID = "would-be-new-live-alias"
	if err := svc.repo.CreateResource(&row); err == nil {
		t.Fatal("identity reused while old physical attempt can still resume")
	}
	if err := svc.deleteLocalResourceObject(old.ObjectKey); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.FinishResourceDeletion(old, false); err == nil {
		t.Fatal("stale completion accepted")
	}
}
