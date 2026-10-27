package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestTaskMediaExistingRequiresUploadClaim(t *testing.T) {
	for _, state := range []model.ResourceStatus{model.ResourceStatusPending, model.ResourceStatusDeleting, model.ResourceStatusReady, model.ResourceStatusDeleted} {
		t.Run(string(state), func(t *testing.T) {
			s, db := newMediaRecoveryTestService(t)
			picture := mediaTestPNG(t)
			name, err := s.stageInlineMedia(picture)
			if err != nil {
				t.Fatal(err)
			}
			row := model.Resource{ID: "stale-media", UserID: "media-user", Kind: "image", Provider: "local", ObjectKey: "existing.png", MimeType: "image/png", Size: int64(len(picture)), Status: state}
			if state != model.ResourceStatusDeleted {
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(s.dataDir, "resources", row.ObjectKey)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			stale := row
			stale.Status = model.ResourceStatusFailed
			_, err = s.storeTaskMediaFile(&model.Task{ID: "task", UserID: row.UserID}, 0, s.mediaTempPath(name), row.MimeType, row.Kind, &stale)
			if err == nil {
				t.Error("stale failed snapshot admitted without a live claim")
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != "preserve" {
				t.Fatalf("unclaimed writer touched object: %q %v", got, readErr)
			}
			stored, lookupErr := s.repo.Resource(row.ID)
			if state == model.ResourceStatusDeleted {
				if lookupErr == nil {
					t.Fatal("removed row recreated")
				}
			} else if lookupErr != nil || stored.Status != state {
				t.Fatalf("claim rejection changed row: %+v %v", stored, lookupErr)
			}
		})
	}
}

func TestTaskMediaClaimBlocksDeletionBeforePhysicalWrite(t *testing.T) {
	t.Setenv("DELETION_TEST_POSTGRES_DSN", "")
	db, other := deletionConcurrencyDBs(t)
	s := &Service{repo: repository.New(db), dataDir: t.TempDir()}
	picture := mediaTestPNG(t)
	name, err := s.stageInlineMedia(picture)
	if err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "claimed-media", UserID: "media-user", Kind: "image", Provider: "local", ObjectKey: "claimed.png", MimeType: "image/png", Size: int64(len(picture)), Status: model.ResourceStatusFailed}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	claimed := false
	if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test_media_claim", func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if tx.Statement.Table != "resources" || !ok || values["status"] != model.ResourceStatusPending || tx.Error != nil || tx.RowsAffected != 1 {
			return
		}
		claimed = true
		if err := repository.New(other).DeleteResource(row.UserID, row.ID); err == nil {
			t.Error("deletion admitted during physical producer claim")
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove("test_media_claim")
	resource, err := s.storeTaskMediaFile(&model.Task{ID: "task", UserID: row.UserID}, 0, s.mediaTempPath(name), row.MimeType, row.Kind, &row)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("physical upload had no durable claim")
	}
	if resource.Status != model.ResourceStatusReady {
		t.Fatalf("status = %s", resource.Status)
	}
	got, err := os.ReadFile(filepath.Join(s.dataDir, "resources", row.ObjectKey))
	if err != nil || !bytes.Equal(got, picture) {
		t.Fatalf("claimed upload = %q %v", got, err)
	}
}

func TestTaskMediaClaimFailureCanRetryLocalWrite(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	picture := mediaTestPNG(t)
	name, err := s.stageInlineMedia(picture)
	if err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "retry-media", UserID: "media-user", Kind: "image", Provider: "local", ObjectKey: "blocked", MimeType: "image/png", Size: int64(len(picture)), Status: model.ResourceStatusFailed}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dataDir, "resources", row.ObjectKey)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: row.UserID}
	if _, err := s.storeTaskMediaFile(task, 0, s.mediaTempPath(name), row.MimeType, row.Kind, &row); err == nil {
		t.Fatal("expected physical failure")
	}
	stored, err := s.repo.Resource(row.ID)
	if err != nil || stored.Status != model.ResourceStatusFailed {
		t.Fatalf("local failure stranded claim: %+v %v", stored, err)
	}
	used, err := s.repo.DailyUploadBytes(row.UserID, time.Now().UTC().Format("2006-01-02"))
	if err != nil || used != 0 {
		t.Fatalf("failed retry retained quota: %d %v", used, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.storeTaskMediaFile(task, 0, s.mediaTempPath(name), row.MimeType, row.Kind, stored); err != nil {
		t.Fatal(err)
	}
}
