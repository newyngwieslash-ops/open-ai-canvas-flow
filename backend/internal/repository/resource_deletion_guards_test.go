package repository

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"yingce/backend/internal/model"
)

func TestDeletionActiveFilterExcludesDeletingAndDraftCreationFenced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "guards.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	if err := db.AutoMigrate(&model.Asset{}, &model.Resource{}, &model.AnnouncementImageDraft{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	asset := model.Asset{ID: "guard-asset", UserID: "user-1", Status: model.AssetVersionStatusDeleting}
	row := model.Resource{ID: "guard-resource", UserID: "user-1", Status: model.ResourceStatusDeleting}
	for _, v := range []any{&asset, &row} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, total, err := repo.UserAssetsPage(asset.UserID, 1, 20, UserAssetPageFilter{Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Error("deleting asset presented as active")
	}
	if err := repo.CreateAnnouncementImageDraft(&model.AnnouncementImageDraft{ResourceID: row.ID, UserID: row.UserID}); err == nil {
		t.Error("draft introduced reference to deleting resource")
	}
}
