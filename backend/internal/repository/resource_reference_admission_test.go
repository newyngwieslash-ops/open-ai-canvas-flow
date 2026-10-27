package repository

import (
	"errors"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/model"
)

func TestReferenceAdmissionCollectorScopesAndAtomicity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "admission.db")+"?_journal_mode=WAL"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	if err := db.AutoMigrate(&model.Resource{}, &model.Project{}, &model.StyleProfile{}, &model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []model.Resource{
		{ID: "owned", UserID: "owner", Status: model.ResourceStatusReady},
		{ID: "foreign", UserID: "admin", Status: model.ResourceStatusReady},
		{ID: "deleting", UserID: "owner", Status: model.ResourceStatusDeleting},
	} {
		if err := db.Create(&resource).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	for _, tc := range []struct {
		name      string
		raw       string
		wantError bool
	}{
		{"nested-ready", `{"metadata":{"videoPreview":{"storageKey":"/api/resources/owned"}}}`, false},
		{"nested-deleting", `{"metadata":{"videoPreview":{"storageKey":"/api/resources/deleting"}}}`, true},
		{"foreign", `{"source":{"resourceId":"foreign"}}`, true},
		{"missing", `{"source":{"resourceId":"missing"}}`, true},
		{"invalid-json", `{`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := repo.CreateProject(&model.Project{ID: tc.name, Name: tc.name, UserID: "owner", StyleProfileJSON: tc.raw})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			var count int64
			if err := db.Model(&model.Project{}).Where("id = ?", tc.name).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if tc.wantError && count != 0 {
				t.Fatal("rejected document persisted")
			}
		})
	}
	// Global settings intentionally support resources uploaded by another admin.
	if err := repo.SaveSystemSetting(&model.SystemSetting{Key: "appearance", ValueJSON: `{"darkLogoResourceId":"foreign"}`}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSystemSettings(&model.SystemSetting{Key: "before", ValueJSON: `{}`}, &model.SystemSetting{Key: "appearance", ValueJSON: `{"logoResourceId":"deleting"}`}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ready rejection, got %v", err)
	}
	var count int64
	if err := db.Model(&model.SystemSetting{}).Where("key = ?", "before").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("batch settings partially committed")
	}
}
