package app

import (
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func deletionConcurrencyDBs(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	dsn := os.Getenv("DELETION_TEST_POSTGRES_DSN")
	var first, second *gorm.DB
	var err error
	if dsn != "" {
		admin, e := gorm.Open(postgres.Open(dsn), cfg)
		if e != nil {
			t.Fatal(e)
		}
		schema := fmt.Sprintf("deletion_%d", time.Now().UnixNano())
		if e := admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); sql, _ := admin.DB(); sql.Close() })
		first, err = gorm.Open(postgres.Open(dsn+"&search_path="+schema), cfg)
		if err == nil {
			second, err = gorm.Open(postgres.Open(dsn+"&search_path="+schema), cfg)
		}
	} else {
		path := filepath.Join(t.TempDir(), "concurrent.db") + "?_journal_mode=WAL&_busy_timeout=100"
		first, err = gorm.Open(sqlite.Open(path), cfg)
		if err == nil {
			second, err = gorm.Open(sqlite.Open(path), cfg)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*gorm.DB{first, second} {
		sql, e := db.DB()
		if e != nil {
			t.Fatal(e)
		}
		sql.SetMaxOpenConns(1)
		t.Cleanup(func() { sql.Close() })
	}
	if err := database.MigrateSchema(first); err != nil {
		t.Fatal(err)
	}
	if dsn == "" {
		var mode string
		if err := second.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil || mode != "wal" {
			t.Fatalf("real WAL required: %s %v", mode, err)
		}
	}
	return first, second
}

func TestDeletionConcurrencyStageVersusLateSave(t *testing.T) {
	db1, db2 := deletionConcurrencyDBs(t)
	repo1, repo2 := repository.New(db1), repository.New(db2)
	svc := &Service{repo: repo1, dataDir: t.TempDir()}
	for i := 0; i < 12; i++ {
		row := model.Resource{ID: fmt.Sprintf("race-%d", i), UserID: "user-1", Provider: "local", ObjectKey: fmt.Sprintf("race-%d", i), Status: model.ResourceStatusReady}
		path := writeClosedDeletionFixtureResource(t, svc, &row)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var stageErr, errorSave error
		go func() { defer wg.Done(); <-start; stageErr = repo1.DeleteDetachedResources([]model.Resource{row}, nil) }()
		go func() {
			defer wg.Done()
			<-start
			copy := row
			copy.ETag = "late"
			errorSave = repo2.SaveResource(&copy)
		}()
		close(start)
		wg.Wait()
		if stageErr != nil {
			t.Fatalf("stage: %v (save %v)", stageErr, errorSave)
		}
		stored, err := repo2.Resource(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != model.ResourceStatusDeleting {
			t.Fatalf("resurrected %s", stored.Status)
		}
		if err := repo2.SaveResource(&row); err == nil {
			t.Fatal("post-stage save accepted")
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
			t.Fatalf("admission changed physical object: %q %v", data, err)
		}
	}
}

func TestDeletionConcurrencyFencedReclaimAndNewAlias(t *testing.T) {
	db1, db2 := deletionConcurrencyDBs(t)
	repo1, repo2 := repository.New(db1), repository.New(db2)
	row := model.Resource{ID: "fence-row", UserID: "user-1", Provider: "local", ObjectKey: "fence-object", Status: model.ResourceStatusReady}
	path := writeClosedDeletionFixtureResource(t, &Service{repo: repo1, dataDir: t.TempDir()}, &row)
	if err := repo1.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
		t.Fatal(err)
	}
	old, err := repo1.ClaimNextResourceDeletionJob("same-owner", time.Minute)
	if err != nil || old == nil {
		t.Fatalf("claim %v %v", old, err)
	}
	if _, _, err := repo1.PrepareResourceDeletion(old); err != nil {
		t.Fatal(err)
	}
	alias := row
	alias.ID = "new-alias"
	alias.UserID = "user-2"
	if err := repo2.CreateResource(&alias); err == nil {
		t.Fatal("new alias crossed physical identity fence")
	}
	if err := db1.Model(&model.ResourceDeletionJob{}).Where("id = ?", old.ID).Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	newer, err := repo2.ClaimNextResourceDeletionJob("same-owner", time.Minute)
	if err != nil || newer == nil {
		t.Fatalf("reclaim %v %v", newer, err)
	}
	if old.LeaseOwner == newer.LeaseOwner {
		t.Fatal("claim generation reused")
	}
	if err := repo1.RetryResourceDeletionJob(old.ID, old.LeaseOwner, "stale", time.Now()); err == nil {
		t.Fatal("old retry accepted")
	}
	if err := repo1.FinishResourceDeletion(old, false); err == nil {
		t.Fatal("old completion accepted")
	}
	if err := repo2.FinishResourceDeletion(newer, false); err != nil {
		t.Fatal(err)
	}
	if err := repo1.SaveResource(&row); err == nil {
		t.Fatal("completed row resurrected")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("repository-only completion changed physical object: %q %v", data, err)
	}
}

// The caller owns this synchronous local write; arbitrary ready rows get no evidence.
func writeClosedDeletionFixtureResource(t *testing.T, svc *Service, row *model.Resource) string {
	t.Helper()
	path := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(row.ObjectKey))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, row)
	return path
}
