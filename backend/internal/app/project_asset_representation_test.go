package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// 通用项目资产图片绑定：场景/道具可绑定图片生成新版本；角色被拒；资源未就绪整体失败。
func seedAssetBindingFixture(t *testing.T, db *gorm.DB, category model.AssetCategory, kind string) (assetID string, versionID string) {
	t.Helper()
	now := assetBindingTestTime()
	assetID, versionID = "asset-"+string(category), "version-1"
	payload, err := json.Marshal(map[string]any{
		"id": assetID, "kind": kind, "category": category, "status": "confirmed",
		"primaryVersionId": versionID, "title": "测试资产", "tags": []string{},
		"data": map[string]any{"content": "", "storageKey": "resource:cover-old"},
		"createdAt": now, "updatedAt": now,
	})
	if err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{ID: assetID, UserID: "user-1", Kind: kind, Category: category, Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: versionID, Title: "测试资产", PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now}
	version := model.AssetVersion{ID: versionID, AssetID: assetID, Version: 1, Status: model.AssetVersionStatusConfirmed, DefinitionJSON: "{}", CreatedAt: now, UpdatedAt: now}
	link := model.ProjectAssetLink{ID: newID(), ProjectID: "project-1", AssetID: assetID, Position: 0, CreatedAt: now}
	for _, item := range []any{&asset, &version, &link} {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("seed %T: %v", item, err)
		}
	}
	return assetID, versionID
}

func assetBindingTestTime() time.Time { return time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC) }

func TestReplaceProjectAssetRepresentationsCreatesNewVersion(t *testing.T) {
	service, db := newProjectAssetLinkTestService(t)
	project := model.Project{ID: "project-1", UserID: "user-1", Name: "短剧", Status: model.ProjectStatusActive}
	resource := model.Resource{ID: "resource-scene", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Size: 100, Width: 100, Height: 100}
	for _, item := range []any{&project, &resource} {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("seed %T: %v", item, err)
		}
	}
	assetID, oldVersionID := seedAssetBindingFixture(t, db, model.AssetCategoryEnvironment, "text")

	summary, err := service.ReplaceProjectAssetRepresentations("user-1", "project-1", assetID, ReplaceAssetRepresentationsRequest{
		Representations: []CharacterRepresentationInput{{ResourceID: "resource-scene"}},
	})
	if err != nil {
		t.Fatalf("ReplaceProjectAssetRepresentations: %v", err)
	}
	if summary.PrimaryVersionID == oldVersionID {
		t.Fatal("binding must create a new version")
	}
	if summary.VersionCount != 2 {
		t.Errorf("summary.VersionCount = %d, want 2", summary.VersionCount)
	}

	var representation model.AssetRepresentation
	if err := db.First(&representation, "asset_version_id = ?", summary.PrimaryVersionID).Error; err != nil {
		t.Fatalf("representation not created: %v", err)
	}
	if representation.Role != "primary" || representation.MediaType != "image" || representation.ResourceID != "resource-scene" {
		t.Errorf("representation = %+v, want primary/image/resource-scene", representation)
	}

	var asset model.Asset
	if err := db.First(&asset, "id = ?", assetID).Error; err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Data struct {
			StorageKey string `json:"storageKey"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(asset.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.StorageKey != "resource:resource-scene" {
		t.Errorf("payload storageKey = %q, want resource:resource-scene", payload.Data.StorageKey)
	}

	var projectAfter model.Project
	if err := db.First(&projectAfter, "id = ?", "project-1").Error; err != nil {
		t.Fatal(err)
	}
	if projectAfter.Revision != 1 {
		t.Errorf("project.Revision = %d, want 1", projectAfter.Revision)
	}
}

func TestReplaceProjectAssetRepresentationsRejectsCharacter(t *testing.T) {
	service, db := newProjectAssetLinkTestService(t)
	projectRow := model.Project{ID: "project-1", UserID: "user-1", Name: "短剧", Status: model.ProjectStatusActive}
	resource := model.Resource{ID: "resource-img", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Size: 100}
	for _, item := range []any{&projectRow, &resource} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	assetID, versionID := seedAssetBindingFixture(t, db, model.AssetCategoryCharacter, "entity")

	if _, err := service.ReplaceProjectAssetRepresentations("user-1", "project-1", assetID, ReplaceAssetRepresentationsRequest{
		Representations: []CharacterRepresentationInput{{ResourceID: "resource-img"}},
	}); err == nil || !strings.Contains(err.Error(), "角色形象接口") {
		t.Fatalf("want character rejected with guidance, got %v", err)
	}

	var count int64
	db.Model(&model.AssetVersion{}).Where("asset_id = ?", assetID).Count(&count)
	if count != 1 {
		t.Errorf("version count = %d, want 1 (rejected request must not create version)", count)
	}
	var asset model.Asset
	db.First(&asset, "id = ?", assetID)
	if asset.PrimaryVersionID != versionID {
		t.Errorf("primaryVersionId changed on rejected request")
	}
}

func TestReplaceProjectAssetRepresentationsFailsClosedOnUnreadyResource(t *testing.T) {
	service, db := newProjectAssetLinkTestService(t)
	projectRow := model.Project{ID: "project-1", UserID: "user-1", Name: "短剧", Status: model.ProjectStatusActive}
	ready := model.Resource{ID: "resource-ready", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Size: 100}
	pending := model.Resource{ID: "resource-pending", UserID: "user-1", Kind: "image", Status: model.ResourceStatusPending, MimeType: "image/png", Size: 100}
	for _, item := range []any{&projectRow, &ready, &pending} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	assetID, _ := seedAssetBindingFixture(t, db, model.AssetCategoryProp, "text")

	if _, err := service.ReplaceProjectAssetRepresentations("user-1", "project-1", assetID, ReplaceAssetRepresentationsRequest{
		Representations: []CharacterRepresentationInput{{ResourceID: "resource-ready"}, {ResourceID: "resource-pending"}},
	}); err == nil {
		t.Fatal("unready resource must fail the whole request")
	}

	var count int64
	db.Model(&model.AssetVersion{}).Where("asset_id = ?", assetID).Count(&count)
	if count != 1 {
		t.Errorf("version count = %d, want 1 (failed request must not create version)", count)
	}
	var representationCount int64
	db.Model(&model.AssetRepresentation{}).Count(&representationCount)
	if representationCount != 0 {
		t.Errorf("representation count = %d, want 0 (no partial binding)", representationCount)
	}
}
