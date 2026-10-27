package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/gorm"
)

func TestDeleteAdminResourcesSupportsPartialSuccessAndBatchedReferences(t *testing.T) {
	svc, db, dataDir, admin := newAdminStorageDeleteTestService(t)
	resources := []model.Resource{
		{ID: "resource-free", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "users/user-1/image/delete-directory"},
		{ID: "resource-blocked", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "users/user-1/image/blocked.png"},
	}
	canvas := model.CanvasProject{ID: "canvas-1", UserID: "user-1", Title: "广告分镜", PayloadJSON: `{"nodes":[{"data":{"storageKey":"resource:resource-blocked"}}]}`}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	directoryPath := filepath.Join(dataDir, "resources", filepath.FromSlash(resources[0].ObjectKey))
	if err := os.MkdirAll(directoryPath, 0o750); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &resources[0])
	blockedPath := writeClosedDeletionFixtureResource(t, svc, &resources[1])

	result, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{"resource-free", "resource-blocked", "resource-free"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "resource-free" {
		t.Fatalf("deleted = %#v", result.Deleted)
	}
	if len(result.Blocked) != 1 || result.Blocked[0].ID != "resource-blocked" || len(result.Blocked[0].References) != 1 || result.Blocked[0].References[0].Kind != "画布" {
		t.Fatalf("blocked = %#v", result.Blocked)
	}
	stopResourceDeletionTestWorkers(t, svc)
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, "resource-free", model.ResourceStatusDeleting)
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, "resource-blocked", model.ResourceStatusReady)
	assertResourceDeletionRetry(t, db, "resource-free", "本地资源路径指向目录")
	assertModelCount(t, db, &model.ResourceDeletionJob{}, "resource_id = ?", 0, "resource-blocked")
	assertModelCount(t, db, &model.AdminAuditEvent{}, "action = ? AND target_id = ?", 1, "resource.delete", "resource-free")
	assertModelCount(t, db, &model.AdminAuditEvent{}, "action = ? AND target_id = ?", 0, "resource.delete", "resource-blocked")
	if info, err := os.Stat(directoryPath); err != nil || !info.IsDir() {
		t.Fatalf("failed physical deletion changed directory: info=%v error=%v", info, err)
	}
	if data, err := os.ReadFile(blockedPath); err != nil || string(data) != "original" {
		t.Fatalf("referenced bytes changed: %q %v", data, err)
	}
}

func TestDeleteAdminResourcesBlocksAnnouncementImage(t *testing.T) {
	svc, db, _, admin := newAdminStorageDeleteTestService(t)
	resource := model.Resource{ID: "announcement-image", UserID: admin.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "announcements/cover.png"}
	announcement := model.Announcement{ID: "announcement-1", Title: "维护通知", Content: "content", ImageResourceID: resource.ID, CreatedBy: admin.ID, Status: model.AnnouncementStatusActive}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&announcement).Error; err != nil {
		t.Fatal(err)
	}

	result, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{resource.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 0 || len(result.Blocked) != 1 || result.Blocked[0].References[0].Kind != "公告" {
		t.Fatalf("result = %#v", result)
	}
	assertModelCount(t, db, &model.Resource{}, "id = ?", 1, resource.ID)
}

func TestDeleteAdminResourcesBlocksDarkAppearanceLogo(t *testing.T) {
	svc, db, _, admin := newAdminStorageDeleteTestService(t)
	resource := model.Resource{ID: "appearance-logo", UserID: admin.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "appearance/logo.png", MimeType: "image/png"}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAppearance(admin, AppearanceSetting{BrandName: "HIMA Studio", BrandSlug: "hima-studio", AuthHeroTitle: defaultAppearanceHeroTitle, DarkLogoResourceID: resource.ID, LogoFrameEnabled: true, SkinID: defaultAppearanceSkinID}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{resource.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 0 || len(result.Blocked) != 1 || len(result.Blocked[0].References) != 1 || result.Blocked[0].References[0].Kind != "外观" {
		t.Fatalf("result = %#v", result)
	}
	assertModelCount(t, db, &model.Resource{}, "id = ?", 1, resource.ID)
}

func TestDeleteAdminResourcesKeepsSharedPhysicalObject(t *testing.T) {
	svc, db, dataDir, admin := newAdminStorageDeleteTestService(t)
	stopResourceDeletionTestWorkers(t, svc)
	objectPath := filepath.Join(dataDir, "resources", "shared", "object.png")
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("shared image"), 0o640); err != nil {
		t.Fatal(err)
	}
	resources := []model.Resource{
		{ID: "resource-delete", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "shared/object.png"},
		{ID: "resource-keep", UserID: "user-2", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "shared/object.png"},
	}
	for index := range resources {
		createClosedLocalFixtureResource(t, svc, &resources[index])
	}
	if err := db.Model(&resources[0]).Update("provider", "").Error; err != nil {
		t.Fatal(err)
	}

	result, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{"resource-delete"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, "resource-delete", model.ResourceStatusDeleting)
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, "resource-keep", model.ResourceStatusReady)
	assertModelCount(t, db, &model.ResourceDeletionJob{}, "resource_id = ?", 1, "resource-delete")
	svc.drainResourceDeletionJobs(1)
	assertModelCount(t, db, &model.Resource{}, "id = ?", 0, "resource-delete")
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, "resource-keep", model.ResourceStatusReady)
	assertModelCount(t, db, &model.ResourceDeletionJob{}, "1 = 1", 0)
	assertModelCount(t, db, &model.ResourceDeletionIntent{}, "status = ?", 1, "completed")
	assertModelCount(t, db, &model.ResourceDeletionObject{}, "status = ?", 1, "retained")
	if content, err := os.ReadFile(objectPath); err != nil || string(content) != "shared image" {
		t.Fatalf("shared physical object changed: content=%q error=%v", content, err)
	}
	var audit model.AdminAuditEvent
	if err := db.First(&audit, "target_id = ?", "resource-delete").Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit.MetadataJSON, `"physicalDeleteQueued":false`) {
		t.Fatalf("audit metadata = %s", audit.MetadataJSON)
	}
}

func TestDeleteAdminResourcesRollsBackWhenAuditInsertFails(t *testing.T) {
	svc, db, _, admin := newAdminStorageDeleteTestService(t)
	resource := model.Resource{ID: "resource-rollback", UserID: admin.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "audit-rollback"}
	path := writeClosedDeletionFixtureResource(t, svc, &resource)
	binding := model.ArkPrivateAssetBinding{ID: "binding-1", UserID: admin.ID, ResourceID: resource.ID, ProjectName: "project", Status: "active"}
	draft := model.AnnouncementImageDraft{ResourceID: resource.ID, UserID: admin.ID}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_resource_audit BEFORE INSERT ON admin_audit_events WHEN NEW.action = 'resource.delete' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END;").Error; err != nil {
		t.Fatal(err)
	}

	result, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{resource.ID}})
	if err != nil || result == nil || len(result.Deleted) != 0 || len(result.Blocked) != 1 || result.Blocked[0].ID != resource.ID || len(result.Blocked[0].References) != 1 || result.Blocked[0].References[0].Kind != "Ark 私有素材" || result.Blocked[0].References[0].ID != binding.ID {
		t.Fatalf("Ark reference must block deletion: result=%+v error=%v", result, err)
	}
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, resource.ID, model.ResourceStatusReady)
	assertModelCount(t, db, &model.ArkPrivateAssetBinding{}, "resource_id = ?", 1, resource.ID)
	assertModelCount(t, db, &model.AnnouncementImageDraft{}, "resource_id = ?", 1, resource.ID)
	assertModelCount(t, db, &model.ResourceDeletionIntent{}, "1 = 1", 0)
	assertModelCount(t, db, &model.ResourceDeletionObject{}, "1 = 1", 0)
	assertModelCount(t, db, &model.ResourceDeletionJob{}, "1 = 1", 0)
	// Release each protected reference explicitly before reaching the audit fault.
	if err := db.Delete(&binding).Error; err != nil {
		t.Fatal(err)
	}
	var referenced *repository.ResourceDeletionReferencedError
	if _, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{resource.ID}}); !errors.As(err, &referenced) || len(referenced.References) != 1 || referenced.References[0].Kind != "公告草稿" {
		t.Fatalf("draft reference must block deletion: %v", err)
	}
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, resource.ID, model.ResourceStatusReady)
	assertModelCount(t, db, &model.AnnouncementImageDraft{}, "resource_id = ?", 1, resource.ID)
	for _, row := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}, &model.AdminAuditEvent{}} {
		assertModelCount(t, db, row, "1 = 1", 0)
	}
	if err := db.Delete(&draft).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{ResourceIDs: []string{resource.ID}}); err == nil || !strings.Contains(err.Error(), "forced audit failure") {
		t.Fatalf("expected forced audit failure after releasing draft reference, got %v", err)
	}
	assertModelCount(t, db, &model.ResourceDeletionIntent{}, "1 = 1", 0)
	assertModelCount(t, db, &model.ResourceDeletionObject{}, "1 = 1", 0)
	assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ?", 1, resource.ID, model.ResourceStatusReady)
	assertModelCount(t, db, &model.Resource{}, "id = ?", 1, resource.ID)
	assertModelCount(t, db, &model.ArkPrivateAssetBinding{}, "resource_id = ?", 0, resource.ID)
	assertModelCount(t, db, &model.AnnouncementImageDraft{}, "resource_id = ?", 0, resource.ID)
	assertModelCount(t, db, &model.ResourceDeletionJob{}, "1 = 1", 0)
	assertModelCount(t, db, &model.AdminAuditEvent{}, "action = ?", 0, "resource.delete")
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("failed audit changed physical bytes: %q %v", data, err)
	}
}

func TestDeleteAdminResourcesRejectsNonAdminAndOversizedBatch(t *testing.T) {
	svc := &Service{}
	user := &model.User{ID: "user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if _, err := svc.DeleteAdminResources(user, AdminResourceDeleteRequest{ResourceIDs: []string{"resource-1"}}); err == nil {
		t.Fatal("expected non-admin rejection")
	}
	ids := make([]string, maxAdminResourceDeleteCount+1)
	for index := range ids {
		ids[index] = newID()
	}
	if _, err := normalizeAdminResourceDeleteIDs(ids); err == nil {
		t.Fatal("expected oversized batch rejection")
	}
}

func newAdminStorageDeleteTestService(t *testing.T) (*Service, *gorm.DB, string, *model.User) {
	t.Helper()
	db := newSQLiteTestDB(t)
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	svc := New(repository.New(db), dataDir)
	startDeletionTestWorkers(t, svc)
	return svc, db, dataDir, admin
}

func assertModelCount(t *testing.T, db *gorm.DB, value any, query string, expected int64, args ...any) {
	t.Helper()
	var count int64
	dbQuery := db.Model(value).Where(query, args...)
	if err := dbQuery.Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("count = %d, want %d", count, expected)
	}
}
