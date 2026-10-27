package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"yingce/backend/internal/model"
)

func TestDeletionLifecycleExpiredLeaseCannotRetryOrComplete(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	expired := time.Now().Add(-time.Minute)
	job := model.ResourceDeletionJob{ID: "fenced", UserID: "user-1", Status: model.ResourceDeletionStatusProcessing, LeaseOwner: "same-worker", LeaseExpiresAt: &expired, Attempts: 2}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.RetryResourceDeletionJob(job.ID, job.LeaseOwner, "stale", time.Now()); err == nil {
		t.Error("expired lease accepted retry")
	}
	if err := svc.repo.CompleteResourceDeletionJob(job.ID, job.LeaseOwner); err == nil {
		t.Error("unverified legacy job accepted completion")
	}
}

func TestDeletionLifecyclePlaybackClaimRejectsDeleting(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	row := model.Resource{ID: "playback-deleting", UserID: "user-1", Status: model.ResourceStatus("deleting")}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimPlaybackTranscode(row.ID)
	if err != nil || claimed {
		t.Fatalf("claim deleting = %v, %v", claimed, err)
	}
}

func TestDeletionLifecycleLegacyJobCannotDeleteLiveObject(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	path := filepath.Join(dir, "resources", "live")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "legacy-live", UserID: "user-1", Provider: "local", ObjectKey: "live", Status: model.ResourceStatusReady}
	job := model.ResourceDeletionJob{ID: "legacy-job", UserID: row.UserID, ResourceID: row.ID, Provider: row.Provider, ObjectKey: row.ObjectKey, Status: model.ResourceDeletionStatusPending, NextAttemptAt: time.Now()}
	for _, item := range []any{&row, &job} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc.drainResourceDeletionJobs(1)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("legacy job removed live object: %v", err)
	}
}
