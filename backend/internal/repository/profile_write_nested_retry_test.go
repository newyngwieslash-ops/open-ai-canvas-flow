package repository

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func TestProfileWriteNestedRetryRestartsOuterTransaction(t *testing.T) {
	for _, message := range []string{"database is locked", "busy_snapshot", "SQLSTATE 40001", "SQLSTATE 40P01"} {
		t.Run(message, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "retry.db"))+"?_journal_mode=WAL"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sqlDB.Close() })
			if err := db.AutoMigrate(&model.Asset{}, &model.Resource{}); err != nil {
				t.Fatal(err)
			}
			lifecycleLocks := 0
			if err := db.Callback().Raw().After("gorm:raw").Register("test:lifecycle_lock", func(tx *gorm.DB) {
				if strings.Contains(tx.Statement.SQL.String(), "UPDATE resources SET status = status WHERE 1 = 0") && tx.Error == nil {
					lifecycleLocks++
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.Asset{ID: "asset", UserID: "owner", Title: "original"}).Error; err != nil {
				t.Fatal(err)
			}
			injected := errors.New(message)
			outerAttempts, innerAttempts, notifications := 0, 0, 0
			if err := db.Callback().Update().Before("gorm:update").Register("test:nested_write_failure", func(tx *gorm.DB) {
				if value, ok := tx.Statement.Dest.(map[string]interface{}); ok && value["title"] == "nested" && outerAttempts == 1 {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			repo := New(db).WithCanvasChangeNotifier(func(userID, canvasID string) {
				if userID != "owner" || canvasID != "canvas" {
					t.Errorf("unexpected notification: %s %s", userID, canvasID)
				}
				notifications++
			})
			err = repo.ProfileWriteTransaction(func(outer *Repository) error {
				outerAttempts++
				if lifecycleLocks != outerAttempts {
					t.Errorf("outer callback preceded lifecycle lock: locks=%d attempts=%d", lifecycleLocks, outerAttempts)
				}
				asset, err := outer.LockProfileAsset("owner", "asset")
				if err != nil {
					return err
				}
				if asset.Title != "original" {
					t.Fatalf("outer retry did not roll back its previous write: %q", asset.Title)
				}
				if err := outer.db.Model(&model.Asset{}).Where("id = ?", asset.ID).Update("title", "outer").Error; err != nil {
					return err
				}
				outer.canvasChanged("owner", "canvas")
				return outer.ProfileWriteTransaction(func(inner *Repository) error {
					innerAttempts++
					inner.canvasChanged("owner", "canvas")
					if notifications != 0 {
						t.Fatal("notification ran before outer commit")
					}
					return inner.db.Model(&model.Asset{}).Where("id = ?", asset.ID).Update("title", "nested").Error
				})
			})
			if err != nil {
				t.Fatalf("nested retry must restart outer transaction, got %v (outer=%d inner=%d)", err, outerAttempts, innerAttempts)
			}
			if outerAttempts != 2 || innerAttempts != 2 || notifications != 1 {
				t.Fatalf("outer=%d inner=%d notifications=%d, want 2/2/1", outerAttempts, innerAttempts, notifications)
			}
			var asset model.Asset
			if err := db.First(&asset, "id = ?", "asset").Error; err != nil || asset.Title != "nested" {
				t.Fatalf("committed value: %+v %v", asset, err)
			}
		})
	}
}

func TestProfileWriteNestedRetryPreservesRawErrorForExternalOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "external.db"))), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	for _, expected := range []error{errors.New("database is locked"), gorm.ErrRecordNotFound} {
		calls := 0
		err := db.Transaction(func(tx *gorm.DB) error {
			return New(tx).ProfileWriteTransaction(func(inner *Repository) error { calls++; return expected })
		})
		if !errors.Is(err, expected) || calls != 1 {
			t.Fatalf("nested owner lost raw error or retried snapshot: err=%v calls=%d", err, calls)
		}
	}
}
