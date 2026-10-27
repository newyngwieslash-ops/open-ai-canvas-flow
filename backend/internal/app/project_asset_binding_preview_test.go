package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"yingce/backend/internal/outbound"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func previewCall(t *testing.T, s *Service, ctx context.Context, user, project, unit string, ids []string) (map[string]any, error) {
	t.Helper()
	out, err := s.PreviewProjectUnitShotAssetBindings(ctx, user, project, unit, BindingPreviewRequest{ShotIDs: ids})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result, nil
}

func previewFixture(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "preview.db")+"?_journal_mode=WAL&_txlock=immediate"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectUnit{}, &model.Shot{}, &model.ShotRevision{}, &model.ShotAssetReference{}, &model.ShotArtifact{}, &model.ProjectAssetLink{}, &model.Asset{}, &model.AssetVersion{}, &model.AssetRepresentation{}, &model.Resource{}, &model.Task{}); err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&model.Project{ID: "p", UserID: "u", Name: "preview", Revision: 17},
		&model.ProjectUnit{ID: "unit", ProjectID: "p"}, &model.ProjectUnit{ID: "other-unit", ProjectID: "p"},
		&model.Shot{ID: "s", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev", Description: "角色：阿青", Title: "shot", Status: "draft"},
		&model.Shot{ID: "cross", ProjectID: "p", UnitID: "other-unit"},
		&model.ShotRevision{ID: "rev", ShotID: "s", Version: 1, Dialogue: "阿青：你好", ActionBeatsJSON: `[{"action":"look"}]`},
		&model.Asset{ID: "a", UserID: "u", Category: "character", Title: "阿青", Status: "draft", PrimaryVersionID: "v"},
		&model.Asset{ID: "b", UserID: "u", Category: "prop", Title: "未提及道具", Status: "draft", PrimaryVersionID: "vb"},
		&model.Asset{ID: "empty", UserID: "u", Category: "environment", Title: "无版本场景", Status: "draft"},
		&model.ProjectAssetLink{ID: "link-a", ProjectID: "p", AssetID: "a"}, &model.ProjectAssetLink{ID: "link-b", ProjectID: "p", AssetID: "b"}, &model.ProjectAssetLink{ID: "link-empty", ProjectID: "p", AssetID: "empty"},
		&model.AssetVersion{ID: "v", AssetID: "a", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"original"}}`},
		&model.AssetVersion{ID: "v-old", AssetID: "a", Version: 2, Status: "archived"},
		&model.AssetVersion{ID: "vb", AssetID: "b", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetRepresentation{ID: "rep", AssetVersionID: "vb", ResourceID: "missing", Role: "primary", MediaType: "image", MetadataJSON: `{"x":1}`},
		&model.Resource{ID: "original", UserID: "u", Kind: "image", Status: model.ResourceStatusReady, Width: 10, Height: 10, MimeType: "image/png", ObjectKey: "sensitive-storage-key", StorageSettingID: "private-setting"},
		&model.ShotAssetReference{ID: "r-z", ShotID: "s", AssetVersionID: "v-old", Role: "character", Status: "confirmed"},
		&model.ShotAssetReference{ID: "r-a", ShotID: "s", AssetVersionID: "v-old", Role: "reference", Status: "stale"},
		&model.ShotArtifact{ID: "artifact", ProjectID: "p", UnitID: "unit", ShotID: "s", RevisionID: "rev", Type: "image", Version: 1, Status: "ready", Selected: true, MetadataJSON: `{"history":true}`},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &Service{repo: repository.New(db)}, db
}

func previewState(t *testing.T, db *gorm.DB) string {
	t.Helper()
	state := map[string]any{}
	for _, table := range []string{"projects", "project_units", "shots", "shot_revisions", "shot_asset_references", "shot_artifacts", "project_asset_links", "assets", "asset_versions", "asset_representations", "resources", "tasks"} {
		var rows []map[string]any
		if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		state[table] = rows
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func previewMap(v any) map[string]any                 { return v.(map[string]any) }
func previewList(v any) []any                         { return v.([]any) }
func previewBaseline(v map[string]any) map[string]any { return previewMap(v["baseline"]) }
func previewJSON(v any) string                        { b, _ := json.Marshal(v); return string(b) }

func TestStoryboardAssetPreviewValidation(t *testing.T) {
	s, db := previewFixture(t)
	before := previewState(t, db)
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("s-%03d", i)
	}
	for _, ids := range [][]string{nil, {}, {""}, {" "}, {"s", ""}, many, {"cross"}, {"missing"}} {
		if _, err := previewCall(t, s, context.Background(), "u", "p", "unit", ids); err == nil {
			t.Fatalf("accepted invalid ids %v", ids)
		}
	}
	for _, scope := range [][3]string{{"other-user", "p", "unit"}, {"u", "p", "other-unit"}, {"u", "missing", "unit"}, {"", "p", "unit"}} {
		if _, err := previewCall(t, s, context.Background(), scope[0], scope[1], scope[2], []string{"s"}); err == nil {
			t.Fatalf("accepted scope %v", scope)
		}
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "s"
	}
	result, err := previewCall(t, s, context.Background(), "u", "p", "unit", ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(previewList(previewBaseline(result)["shotIds"])) != 1 {
		t.Fatal("duplicate shot IDs not deduplicated")
	}
	if before != previewState(t, db) {
		t.Fatal("validation or deduplicated preview wrote database rows")
	}
}

type previewTransport struct{ model, media atomic.Int32 }

func (p *previewTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet {
		p.media.Add(1)
	} else {
		p.model.Add(1)
	}
	return nil, errors.New("preview must not call models or media")
}

func TestStoryboardAssetPreviewReadOnlyCompleteStable(t *testing.T) {
	s, db := previewFixture(t)
	trap := &previewTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = trap
	t.Cleanup(func() { http.DefaultTransport = original })
	// Providers and media use guarded outbound transports, not DefaultTransport.
	// Trap their actual dial paths as well; no real network/model call is allowed.
	for _, client := range []*http.Client{outbound.OutboundHTTPClient(time.Second), outbound.CustomRelayHTTPClient(time.Second)} {
		transport := client.Transport.(*http.Transport)
		previous := transport.DialContext
		transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
			trap.model.Add(1)
			trap.media.Add(1)
			return nil, errors.New("preview outbound call forbidden")
		}
		t.Cleanup(func() { transport.DialContext = previous })
	}
	before := previewState(t, db)
	var connection gorm.ConnPool
	reads := 0
	if err := db.Callback().Query().Before("gorm:query").Register("preview_read_only", func(tx *gorm.DB) {
		reads++
		if connection == nil {
			connection = tx.Statement.ConnPool
		} else if connection != tx.Statement.ConnPool {
			t.Error("snapshot reads escaped pinned transaction connection")
		}
		var readOnly int
		if err := tx.Statement.ConnPool.QueryRowContext(tx.Statement.Context, "PRAGMA query_only").Scan(&readOnly); err != nil || readOnly != 1 {
			t.Errorf("SQLite preview must enforce read-only, got %d: %v", readOnly, err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	result, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Callback().Query().Remove("preview_read_only")
	if reads < 8 {
		t.Fatalf("incomplete snapshot reads: %d", reads)
	}
	again, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s", "s"})
	if err != nil {
		t.Fatal(err)
	}
	if previewJSON(result) != previewJSON(again) {
		t.Fatal("preview is not deterministic")
	}
	baseline := previewBaseline(result)
	if baseline["projectRevision"] != float64(17) {
		t.Fatal("project revision missing")
	}
	shot := previewMap(previewList(baseline["shots"])[0])
	refs := previewList(shot["references"])
	if len(refs) != 2 || previewMap(refs[0])["referenceId"] != "r-a" || previewMap(refs[0])["status"] != "stale" || previewMap(refs[1])["role"] != "character" {
		t.Fatalf("lost full reference tuples: %v", refs)
	}
	if len(previewList(shot["artifacts"])) != 1 {
		t.Fatal("history missing")
	}
	catalog := previewList(baseline["catalog"])
	if len(catalog) != 3 || previewMap(catalog[0])["assetId"] != "a" || len(previewList(previewMap(catalog[0])["versions"])) != 2 || len(previewList(previewMap(catalog[2])["versions"])) != 0 {
		t.Fatalf("incomplete or unsorted catalog: %v", catalog)
	}
	row := previewMap(previewList(result["shots"])[0])
	if row["existingVersionCount"] != float64(1) || row["remainingCapacity"] != float64(5) {
		t.Fatal("capacity counts reference roles instead of distinct versions")
	}
	candidates := previewList(row["candidates"])
	if len(candidates) < 2 {
		t.Fatal("resolver evidence missing")
	}
	visual := previewMap(candidates[0])
	if visual["defaultSelected"] != true || visual["missingImage"] != false {
		t.Fatalf("original resource not recognized or resolver not used: %v", visual)
	}
	if strings.Contains(previewJSON(result), "sensitive-storage-key") || strings.Contains(previewJSON(result), "private-setting") {
		t.Fatal("storage identity leaked")
	}
	if before != previewState(t, db) {
		t.Fatal("preview changed table state, project revision or task count")
	}
	if trap.model.Load() != 0 || trap.media.Load() != 0 {
		t.Fatal("preview invoked model or media transport")
	}
}

func TestStoryboardAssetPreviewFingerprints(t *testing.T) {
	changes := []struct {
		name, table, id, column string
		value                   any
		section                 string
	}{
		{"direct description", "shots", "s", "description", "角色：阿青\n改变", "shots"},
		{"full revision", "shot_revisions", "rev", "negative_prompt", "negative", "shots"},
		{"script action beats", "shot_revisions", "rev", "action_beats_json", `[{"action":"changed"}]`, "shots"},
		{"unselected candidate", "assets", "b", "title", "未选择但改名", "catalog"},
		{"unselected version", "asset_versions", "vb", "prompt", "new prompt", "catalog"},
		{"project link", "project_asset_links", "link-b", "position", 9, "catalog"},
		{"representation", "asset_representations", "rep", "metadata_json", `{"x":2}`, "catalog"},
		{"original storage", "resources", "original", "storage_setting_id", "new-private-setting", "catalog"},
		{"resource owner", "resources", "original", "user_id", "other", "catalog"},
		{"historical artifact", "shot_artifacts", "artifact", "metadata_json", `{"history":false}`, "shots"},
		{"reference role", "shot_asset_references", "r-a", "role", "prop", "shots"},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			s, db := previewFixture(t)
			before, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Table(change.table).Where("id = ?", change.id).UpdateColumn(change.column, change.value).Error; err != nil {
				t.Fatal(err)
			}
			after, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
			if err != nil {
				t.Fatal(err)
			}
			if previewJSON(previewBaseline(before)[change.section]) == previewJSON(previewBaseline(after)[change.section]) {
				t.Fatalf("%s did not change baseline", change.name)
			}
		})
	}
	t.Run("resource existence", func(t *testing.T) {
		s, db := previewFixture(t)
		before, _ := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
		if err := db.Delete(&model.Resource{}, "id = ?", "original").Error; err != nil {
			t.Fatal(err)
		}
		after, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
		if err != nil {
			t.Fatal(err)
		}
		if previewJSON(previewBaseline(before)["catalog"]) == previewJSON(previewBaseline(after)["catalog"]) {
			t.Fatal("resource disappearance omitted")
		}
	})
}

func TestStoryboardAssetPreviewSQLiteSnapshotDoesNotTakeWriterLock(t *testing.T) {
	s, db := previewFixture(t)
	var writeErr error
	changed := false
	if err := db.Callback().Query().After("gorm:query").Register("preview_concurrent_catalog", func(tx *gorm.DB) {
		if tx.Statement.Table == "shots" && !changed {
			changed = true
			writeErr = db.Model(&model.Asset{}).Where("id = ?", "b").UpdateColumn("title", "并发修改").Error
		}
	}); err != nil {
		t.Fatal(err)
	}
	result, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
	_ = db.Callback().Query().Remove("preview_concurrent_catalog")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || writeErr != nil {
		t.Fatalf("preview blocked the independent writer: %v", writeErr)
	}
	after, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	if previewJSON(previewBaseline(result)["catalog"]) == previewJSON(previewBaseline(after)["catalog"]) {
		t.Fatal("preview mixed post-snapshot catalog with earlier shot read")
	}
}

func TestStoryboardAssetPreviewBatchOrdering(t *testing.T) {
	s, db := previewFixture(t)
	ids := []string{"s"}
	for i := 98; i >= 0; i-- {
		id := fmt.Sprintf("batch-%03d", i)
		ids = append(ids, id)
		if err := db.Create(&model.Shot{ID: id, ProjectID: "p", UnitID: "unit"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	before := previewState(t, db)
	result, err := previewCall(t, s, context.Background(), "u", "p", "unit", ids)
	if err != nil {
		t.Fatal(err)
	}
	shots := previewList(previewBaseline(result)["shots"])
	if len(shots) != 100 {
		t.Fatalf("100-shot boundary lost rows: %d", len(shots))
	}
	for i := 1; i < len(shots); i++ {
		if previewMap(shots[i-1])["shotId"].(string) >= previewMap(shots[i])["shotId"].(string) {
			t.Fatal("shot baseline order is unstable")
		}
	}
	if before != previewState(t, db) {
		t.Fatal("batch preview wrote rows")
	}
}

func TestStoryboardAssetPreviewDanglingReferencedVersion(t *testing.T) {
	s, db := previewFixture(t)
	for _, row := range []any{
		&model.AssetVersion{ID: "orphan-version", AssetID: "deleted-asset", Version: 1, Status: "draft", Prompt: "original"},
		&model.ShotAssetReference{ID: "orphan-reference", ShotID: "s", AssetVersionID: "orphan-version", Role: "reference"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	before, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AssetVersion{}).Where("id = ?", "orphan-version").UpdateColumn("prompt", "changed").Error; err != nil {
		t.Fatal(err)
	}
	after, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	if previewJSON(previewBaseline(before)["catalog"]) == previewJSON(previewBaseline(after)["catalog"]) {
		t.Fatal("existing orphan version omitted from baseline")
	}
}

func TestStoryboardAssetPreviewCapacityAndCancellation(t *testing.T) {
	s, db := previewFixture(t)
	for i := 0; i < 6; i++ {
		if err := db.Create(&model.ShotAssetReference{ID: fmt.Sprintf("extra-%d", i), ShotID: "s", AssetVersionID: fmt.Sprintf("missing-%d", i), Role: "reference"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	before := previewState(t, db)
	result, err := previewCall(t, s, context.Background(), "u", "p", "unit", []string{"s"})
	if err != nil {
		t.Fatal(err)
	}
	row := previewMap(previewList(result["shots"])[0])
	if row["existingVersionCount"] != float64(7) || row["remainingCapacity"] != float64(0) || !strings.Contains(previewJSON(row["issues"]), "capacity_exceeded") {
		t.Fatal("over-capacity references silently repaired or accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := previewCall(t, s, ctx, "u", "p", "unit", []string{"s"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_ = db.Callback().Query().After("gorm:query").Register("cancel_preview", func(tx *gorm.DB) {
		if tx.Statement.Table == "shots" {
			cancel()
		}
	})
	if _, err := previewCall(t, s, ctx, "u", "p", "unit", []string{"s"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-read cancellation lost: %v", err)
	}
	_ = db.Callback().Query().Remove("cancel_preview")
	if before != previewState(t, db) {
		t.Fatal("cancellation or over-capacity preview wrote rows")
	}
	if err := db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("revision", 18).Error; err != nil {
		t.Fatalf("read-only flag/transaction leaked to pool: %v", err)
	}
}
