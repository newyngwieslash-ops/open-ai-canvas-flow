package database

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestProducerC1MigrationColumns(t *testing.T) {
	db := producerC1Database(t)
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"control_owner", "control_phase", "control_result"} {
		if !db.Migrator().HasColumn(&model.ResourceProducerOperation{}, column) {
			t.Fatalf("controlled launch evidence column missing: %s", column)
		}
	}
}

func producerC1Database(t *testing.T) *gorm.DB {
	t.Helper()
	config := Config{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "c1.sqlite")}
	if dsn := os.Getenv("PRODUCER_P2_POSTGRES_DSN"); dsn != "" {
		admin, err := Open(Config{Driver: "postgres", DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf("producer_c1_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
			sql, _ := admin.DB()
			_ = sql.Close()
		})
		config = Config{Driver: "postgres", DSN: dsn + "&search_path=" + schema}
	}
	db, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestProducerC1MigrationUpgradeRepeatRollback(t *testing.T) {
	db := producerC1Database(t)
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	old := model.ResourceProducerOperation{ID: "legacy", ResourceID: "old-resource", UserID: "old-owner", Variant: "playback", Provider: "local", ObjectKey: "legacy.mp4", Identity: "old-identity", Protocol: "producer-accounting-v1", PhysicalState: "unknown", Outcome: "failed", Proof: "legacy-untracked", Fenced: true}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	var before model.ResourceProducerOperation
	// Explicit model columns keep the result order stable when this test drops
	// and re-adds columns on one PostgreSQL prepared-statement connection.
	if err := db.Session(&gorm.Session{QueryFields: true}).First(&before, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() == "postgres" {
		var order string
		if err := db.Raw("SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='resource_producer_operations'").Scan(&order).Error; err != nil {
			t.Fatal(err)
		}
		t.Log("before physical columns:", order)
	}
	columns := []string{"control_owner", "control_phase", "control_result"}
	for _, column := range columns {
		if err := db.Migrator().DropColumn(&model.ResourceProducerOperation{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = 61").Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("injected control migration rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, m := range schemaMigrations {
			if m.version == 61 {
				if err := m.apply(tx); err != nil {
					return err
				}
			}
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	for _, column := range columns {
		if db.Migrator().HasColumn(&model.ResourceProducerOperation{}, column) {
			t.Fatalf("rolled back column remains: %s", column)
		}
	}
	for i := 0; i < 2; i++ {
		if err := MigrateSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(Models()...); err != nil {
		t.Fatal(err)
	}
	var after model.ResourceProducerOperation
	if db.Dialector.Name() == "postgres" {
		var order string
		if err := db.Raw("SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='resource_producer_operations'").Scan(&order).Error; err != nil {
			t.Fatal(err)
		}
		t.Log("after physical columns:", order)
	}
	if err := db.Session(&gorm.Session{QueryFields: true}).First(&after, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy evidence changed: before=%+v after=%+v", before, after)
	}
}
