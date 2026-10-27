package repository

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
)

func TestProducerP2UnlockFailureRollsBackFinish(t *testing.T) {
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "unlock.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	if err := db.AutoMigrate(database.Models()...); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	row := model.Resource{ID: "unlock-resource", UserID: "unlock-user", Provider: "local", ObjectKey: "unlock", Status: model.ResourceStatusDeleting}
	op := resourceProducerIdentity(&row, "original")
	op.ID, op.Protocol, op.PhysicalState, op.Proof, op.Fenced = "unlock-operation", LocalGoProducerProtocol, "settled", "local-writer-returned", true
	intent := model.ResourceDeletionIntent{ID: "unlock-intent", UserID: row.UserID, AssetIDsJSON: "[]", ResourceIDsJSON: deletionJSON([]string{row.ID}), Status: "pending"}
	object := model.ResourceDeletionObject{ID: "unlock-job", IntentID: intent.ID, ResourceIDsJSON: intent.ResourceIDsJSON, Variant: "original", Provider: "local", ObjectKey: row.ObjectKey, Status: "pending"}
	expires := time.Now().Add(time.Minute)
	job := model.ResourceDeletionJob{ID: object.ID, UserID: row.UserID, ResourceID: row.ID, Provider: "local", ObjectKey: row.ObjectKey, Status: model.ResourceDeletionStatusProcessing, LeaseOwner: "unlock-worker", LeaseExpiresAt: &expires}
	for _, value := range []any{&row, &op, &intent, &object, &job} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	locks, err := AcquireResourcePhysicalLocks(root, []string{op.Identity})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locks.Close() })
	failure := errors.New("injected OS unlock failure")
	locks.closeFile = func(lock *flock.Flock) error { return errors.Join(lock.Close(), failure) }
	if err := repo.WithResourcePhysicalLocks(locks).FinishResourceDeletion(&job, false); !errors.Is(err, failure) {
		t.Fatalf("Finish did not return unlock failure: %v", err)
	}
	if err := db.First(&object, "id = ?", object.ID).Error; err != nil || object.Status != "pending" {
		t.Fatalf("object lost after failed release: %+v %v", object, err)
	}
	if err := db.First(&intent, "id = ?", intent.ID).Error; err != nil || intent.Status != "pending" {
		t.Fatalf("intent lost after failed release: %+v %v", intent, err)
	}
	for _, value := range []any{&row, &job} {
		if err := db.First(value).Error; err != nil {
			t.Fatalf("record removed after release failure: %v", err)
		}
	}
	// A fresh capability may retry. The old capability can never commit after close.
	if err := repo.WithResourcePhysicalLocks(locks).FinishResourceDeletion(&job, false); err == nil {
		t.Fatal("closed capability reused")
	}
	next, err := AcquireResourcePhysicalLocks(root, []string{op.Identity})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := repo.WithResourcePhysicalLocks(next).FinishResourceDeletion(&job, false); err != nil {
		t.Fatal(err)
	}
}
