package database

import (
	"errors"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func TestProducerP1MigrationDoesNotInventLegacyEvidence(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "schema.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, status := range []model.ResourceStatus{model.ResourceStatusReady, model.ResourceStatusPending, model.ResourceStatusFailed} {
		if err := db.Create(&model.Resource{ID: string(status), UserID: "owner", Provider: "local", ObjectKey: string(status), Status: status}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Reconstruct the immediately preceding schema in this disposable database.
	if err := db.Migrator().DropTable(&model.ResourceProducerOperation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = 60").Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("injected migration rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, migration := range schemaMigrations {
			if migration.version == 60 {
				if err := migration.apply(tx); err != nil {
					return err
				}
			}
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if db.Migrator().HasTable(&model.ResourceProducerOperation{}) {
		t.Fatal("rolled back migration left producer table")
	}
	for i := 0; i < 2; i++ {
		if err := MigrateSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(Models()...); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.ResourceProducerOperation{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invented evidence: %d %v", count, err)
	}
	var rows []model.Resource
	if err := db.Find(&rows).Error; err != nil || len(rows) != 3 {
		t.Fatalf("old rows changed: %v %v", rows, err)
	}
	for _, row := range rows {
		if string(row.Status) != row.ID {
			t.Fatalf("legacy status rewritten: %v", row)
		}
	}
}
