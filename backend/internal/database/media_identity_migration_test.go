package database

import (
	"fmt"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yingce/backend/internal/model"
)

func TestMediaDeletionSchemaRegistrationAndUpgrade(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			db := mediaMigrationDatabase(t, driver)
			if err := MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			for _, table := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}} {
				if !db.Migrator().HasTable(table) {
					t.Fatal("new deletion schema table missing")
				}
			}
			if err := db.Migrator().DropTable(&model.ResourceDeletionObject{}, &model.ResourceDeletionIntent{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("DELETE FROM schema_migrations WHERE version > 58").Error; err != nil {
				t.Fatal(err)
			}
			if err := MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			for _, table := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}} {
				if !db.Migrator().HasTable(table) {
					t.Fatal("upgraded deletion schema table missing")
				}
			}
			if err := MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func mediaMigrationDatabase(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "media.db")
	if driver == "postgres" {
		dsn = os.Getenv("CANVAS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured")
		}
		base, err := Open(Config{Driver: driver, DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf("media_identity_%d", time.Now().UnixNano())
		if err := base.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { base.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); sqlDB, _ := base.DB(); sqlDB.Close() })
		dsn, err = postgresDSNWithSearchPath(dsn, schema)
		if err != nil {
			t.Fatal(err)
		}
	}
	db, err := Open(Config{Driver: driver, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func TestMediaIdentitySchemaAndUpgrade(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			for _, upgrade := range []bool{false, true} {
				name := "fresh"
				if upgrade {
					name = "upgrade57"
				}
				t.Run(name, func(t *testing.T) {
					db := mediaMigrationDatabase(t, driver)
					if err := MigrateSchema(db); err != nil {
						t.Fatal(err)
					}
					original := model.AssetRepresentation{ID: "original", AssetVersionID: "v1", TaskID: "media-task", Role: "primary", ResourceID: "image"}
					if err := db.Create(&original).Error; err != nil {
						t.Fatal(err)
					}
					if upgrade {
						if err := db.Exec("DELETE FROM schema_migrations WHERE version > 57").Error; err != nil {
							t.Fatal(err)
						}
						if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_representations_task_role ON asset_representations(task_id, role)").Error; err != nil {
							t.Fatal(err)
						}
					}
					if err := MigrateSchema(db); err != nil {
						t.Fatal(err)
					}
					if err := MigrateSchema(db); err != nil {
						t.Fatal(err)
					}
					if err := db.AutoMigrate(&model.AssetRepresentation{}); err != nil {
						t.Fatal(err)
					}
					copy := original
					copy.ID = "copy"
					copy.AssetVersionID = "v2"
					if err := db.Create(&copy).Error; err != nil {
						t.Fatalf("legitimate provenance copy rejected: %v", err)
					}
					// Re-running startup AutoMigrate with actual copies must not restore
					// the obsolete constraint or make subsequent startup fail.
					if err := db.AutoMigrate(&model.AssetRepresentation{}); err != nil {
						t.Fatal(err)
					}
					if err := MigrateSchema(db); err != nil {
						t.Fatal(err)
					}
					if db.Migrator().HasIndex(&model.AssetRepresentation{}, "idx_asset_representations_task_role") {
						t.Fatal("global task/role index remains")
					}
					if !db.Migrator().HasIndex(&model.AssetRepresentation{}, "idx_asset_representations_task_id") {
						t.Fatal("provenance lookup index missing")
					}
					copy.ID = "duplicate"
					copy.TaskID = "other-task"
					if err := db.Create(&copy).Error; err == nil {
						t.Fatal("duplicate version/role accepted")
					}
					var saved model.AssetRepresentation
					if err := db.First(&saved, "id = ?", original.ID).Error; err != nil || saved.TaskID != original.TaskID || saved.ResourceID != original.ResourceID {
						t.Fatalf("history changed: %+v %v", saved, err)
					}
				})
			}
		})
	}
}
