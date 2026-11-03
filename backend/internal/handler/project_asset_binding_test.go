package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

func setupAppendRouter(t *testing.T) (*gin.Engine, *service.Service, *gorm.DB, string) {
	t.Helper()
	router, db, cookie := profileHandlerFixture(t)
	svc := service.New(repository.New(db), t.TempDir())
	t.Cleanup(func() { _ = svc.Close() })
	registerProjectUnitRoutes(router.Group("/api"), svc)
	registerProjectWorkflowRoutes(router.Group("/api"), svc)
	rows := []any{
		&model.User{ID: "foreign", Username: "foreign", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.Project{ID: "p", UserID: "profile-user", Name: "handler-proj", Revision: 7},
		&model.Project{ID: "foreign-p", UserID: "foreign", Name: "SECRET_PROJECT", Revision: 1},
		&model.ProjectUnit{ID: "unit", ProjectID: "p", Title: "u1"},
		&model.Shot{ID: "s1", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "r1", Description: "角色：阿青", Title: "shot1", Status: "draft", Position: 1},
		&model.ShotRevision{ID: "r1", ShotID: "s1", Version: 1, Dialogue: "阿青：走", ActionBeatsJSON: `[]`},
		&model.Asset{ID: "a", UserID: "profile-user", Category: model.AssetCategoryCharacter, Title: "阿青", Status: "draft", PrimaryVersionID: "v1", PayloadJSON: `{}`},
		&model.Asset{ID: "b", UserID: "profile-user", Category: model.AssetCategoryProp, Title: "剑", Status: "draft", PrimaryVersionID: "vb1", PayloadJSON: `{}`},
		&model.ProjectAssetLink{ID: "la", ProjectID: "p", AssetID: "a"},
		&model.ProjectAssetLink{ID: "lb", ProjectID: "p", AssetID: "b"},
		&model.AssetVersion{ID: "v1", AssetID: "a", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb1", AssetID: "b", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb2", AssetID: "b", Version: 2, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb3", AssetID: "b", Version: 3, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb4", AssetID: "b", Version: 4, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb5", AssetID: "b", Version: 5, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb6", AssetID: "b", Version: 6, Status: "draft", DefinitionJSON: `{}`},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return router, svc, db, cookie
}

func doAppendRequest(t *testing.T, r *gin.Engine, cookie, projectID, unitID string, body any) (int, map[string]any) {
	t.Helper()
	var raw string
	switch v := body.(type) {
	case string:
		raw = v
	default:
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	w := profileRequest(r, http.MethodPost, "/api/projects/"+projectID+"/units/"+unitID+"/shot-asset-bindings/append", raw, cookie)
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal response (%d %s): %v", w.Code, w.Body.String(), err)
	}
	return w.Code, env
}

func TestProjectUnitShotAssetBindingsAppendRoute(t *testing.T) {
	r, svc, db, cookie := setupAppendRouter(t)

	preview, err := svc.PreviewProjectUnitShotAssetBindings(httptest.NewRequest(http.MethodPost, "/", nil).Context(), "profile-user", "p", "unit", service.BindingPreviewRequest{ShotIDs: []string{"s1"}})
	if err != nil {
		t.Fatal(err)
	}

	// 1. 401 Unauthorized
	status, env := doAppendRequest(t, r, "", "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}},
	})
	if status != http.StatusUnauthorized || int(env["code"].(float64)) != service.CodeUnauthorized {
		t.Fatalf("expected 401 unauthorized, got status=%d env=%+v", status, env)
	}

	// 2. 404 NotFound for missing or foreign project/unit (never 500!)
	for _, tc := range []struct{ p, u string }{
		{"missing-p", "unit"},
		{"foreign-p", "unit"},
		{"p", "missing-unit"},
	} {
		b := preview.Baseline
		b.ProjectID = tc.p
		b.UnitID = tc.u
		status, env := doAppendRequest(t, r, cookie, tc.p, tc.u, service.BindingAppendRequest{
			Baseline:   b,
			Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}},
		})
		if status != http.StatusNotFound || int(env["code"].(float64)) != service.CodeNotFound || env["reason"] != string(service.ReasonNotFound) {
			t.Fatalf("expected 404 not_found for %+v, got status=%d env=%+v", tc, status, env)
		}
		if strings.Contains(env["msg"].(string), "SECRET_PROJECT") {
			t.Fatalf("leaked foreign project name in 404 response: %+v", env)
		}
	}

	// 3. 400 Invalid request (malformed JSON & invalid selections)
	status, env = doAppendRequest(t, r, cookie, "p", "unit", "{bad-json")
	if status != http.StatusBadRequest || int(env["code"].(float64)) != service.CodeInvalidArgument || env["reason"] != string(service.ReasonStoryboardAssetBindingInvalidRequest) {
		t.Fatalf("expected 400 storyboard_asset_binding_invalid_request on bad json, got status=%d env=%+v", status, env)
	}

	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{},
	})
	if status != http.StatusBadRequest || int(env["code"].(float64)) != service.CodeInvalidArgument || env["reason"] != string(service.ReasonStoryboardAssetBindingInvalidRequest) {
		t.Fatalf("expected 400 storyboard_asset_binding_invalid_request on empty selections, got status=%d env=%+v", status, env)
	}

	// 4. 422 Invalid reference
	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"non-existent-version"}}},
	})
	if status != http.StatusUnprocessableEntity || int(env["code"].(float64)) != service.CodeStoryboardAssetBindingInvalidReference || env["reason"] != string(service.ReasonStoryboardAssetBindingInvalidReference) {
		t.Fatalf("expected 422 storyboard_asset_binding_invalid_reference, got status=%d env=%+v", status, env)
	}

	// 5. 422 Capacity exceeded (7 versions selected > 6)
	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1", "vb1", "vb2", "vb3", "vb4", "vb5", "vb6"}}},
	})
	if status != http.StatusUnprocessableEntity || int(env["code"].(float64)) != service.CodeStoryboardAssetBindingCapacityExceeded || env["reason"] != string(service.ReasonStoryboardAssetBindingCapacityExceeded) {
		t.Fatalf("expected 422 storyboard_asset_binding_capacity_exceeded, got status=%d env=%+v", status, env)
	}

	// 6. 409 Baseline conflict
	staleBaseline := preview.Baseline
	staleBaseline.ProjectRevision = 999
	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   staleBaseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}},
	})
	if status != http.StatusConflict || int(env["code"].(float64)) != service.CodeConflict || env["reason"] != string(service.ReasonStoryboardAssetBindingConflict) {
		t.Fatalf("expected 409 storyboard_asset_binding_conflict, got status=%d env=%+v", status, env)
	}

	// 7. 503 Busy when owner coordination returns 503
	cbName := "simulate_owner_busy_503"
	if err := db.Callback().Raw().Before("gorm:raw").Register(cbName, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "UPDATE assets SET id = id WHERE 1 = 0") {
			tx.AddError(service.StoryboardAssetBindingBusy("资产绑定繁忙，请稍后重试"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}},
	})
	_ = db.Callback().Raw().Remove(cbName)
	if status != http.StatusServiceUnavailable || int(env["code"].(float64)) != service.CodeUnavailable || env["reason"] != string(service.ReasonStoryboardAssetBindingBusy) {
		t.Fatalf("expected 503 storyboard_asset_binding_busy, got status=%d env=%+v", status, env)
	}

	// 8. 200 OK happy path
	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}},
	})
	if status != http.StatusOK || int(env["code"].(float64)) != 0 {
		t.Fatalf("expected 200 OK, got status=%d env=%+v", status, env)
	}
	data, ok := env["data"].(map[string]any)
	if !ok || int(data["projectRevision"].(float64)) != 8 || int(data["addedCount"].(float64)) != 1 || data["unchanged"].(bool) {
		t.Fatalf("unexpected 200 response data: %+v", env)
	}
	shots, ok := data["shots"].([]any)
	if !ok || len(shots) != 1 {
		t.Fatalf("expected 1 shot in 200 response data, got %+v", data["shots"])
	}
	shot0, ok := shots[0].(map[string]any)
	if !ok || shot0["shotId"] != "s1" || shot0["unchanged"].(bool) {
		t.Fatalf("unexpected shot[0] in 200 response data: %+v", shots[0])
	}
	shotAddedCount, hasAddedCount := shot0["addedCount"].(float64)
	if !hasAddedCount || int(shotAddedCount) != 1 {
		t.Fatalf("expected shot[0].addedCount=1 in 200 response data, got %+v", shot0)
	}
	refs, hasRefs := shot0["references"].([]any)
	if !hasRefs || len(refs) != 1 {
		t.Fatalf("expected shot[0].references len=1 in 200 response data, got %+v", shot0)
	}
	ref0, ok := refs[0].(map[string]any)
	if !ok || ref0["assetVersionId"] != "v1" || ref0["role"] != "reference" || ref0["status"] != "linked" || strings.TrimSpace(ref0["referenceId"].(string)) == "" {
		t.Fatalf("unexpected shot[0].references[0]: %+v", refs[0])
	}
	if _, hasArts := shot0["artifacts"].([]any); !hasArts {
		t.Fatalf("expected shot[0].artifacts array in 200 response data, got %+v", shot0)
	}

	// 9. 200 OK idempotent replay of the exact same request
	status, env = doAppendRequest(t, r, cookie, "p", "unit", service.BindingAppendRequest{
		Baseline:   preview.Baseline,
		Selections: []service.BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}},
	})
	if status != http.StatusOK || int(env["code"].(float64)) != 0 {
		t.Fatalf("expected 200 OK on replay, got status=%d env=%+v", status, env)
	}
	data, ok = env["data"].(map[string]any)
	if !ok || int(data["projectRevision"].(float64)) != 8 || int(data["addedCount"].(float64)) != 0 || !data["unchanged"].(bool) {
		t.Fatalf("unexpected 200 replay response data: %+v", env)
	}
	shots, ok = data["shots"].([]any)
	if !ok || len(shots) != 1 {
		t.Fatalf("expected 1 shot in 200 replay response data, got %+v", data["shots"])
	}
	shot0, ok = shots[0].(map[string]any)
	if !ok || shot0["shotId"] != "s1" || !shot0["unchanged"].(bool) {
		t.Fatalf("unexpected shot[0] in 200 replay response data: %+v", shots[0])
	}
	shotAddedCount, hasAddedCount = shot0["addedCount"].(float64)
	if !hasAddedCount || int(shotAddedCount) != 0 {
		t.Fatalf("expected shot[0].addedCount=0 in 200 replay response data, got %+v", shot0)
	}
}

func TestProjectUnitShotAssetBindingsGeneratedAndReplaceRoutesNotFound(t *testing.T) {
	r, svc, db, cookie := setupAppendRouter(t)
	validTask := model.Task{
		ID:         "gen-ok",
		UserID:     "profile-user",
		ProjectID:  "p",
		Type:       "canvas_text",
		Operation:  "storyboard",
		Status:     model.TaskStatusSucceeded,
		InputJSON:  `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a"]}}`,
		ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"}]}]}`,
	}
	foreignTask := model.Task{
		ID:         "gen-foreign",
		UserID:     "foreign",
		ProjectID:  "foreign-p",
		Type:       "canvas_text",
		Operation:  "storyboard",
		Status:     model.TaskStatusSucceeded,
		InputJSON:  `{"mode":"text","chapterAssetContext":{"projectId":"foreign-p","unitId":"unit","operation":"storyboard","allowedAssetIds":["a"]}}`,
		ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"}]}]}`,
	}
	wrongUnitTask := model.Task{
		ID:         "gen-wrong-unit",
		UserID:     "profile-user",
		ProjectID:  "p",
		Type:       "canvas_text",
		Operation:  "storyboard",
		Status:     model.TaskStatusSucceeded,
		InputJSON:  `{"mode":"text","chapterAssetContext":{"projectId":"p","unitId":"other-unit","operation":"storyboard","allowedAssetIds":["a"]}}`,
		ResultJSON: `{"rows":[{"projectAssetRefs":[{"assetId":"a","category":"character"}]}]}`,
	}
	if err := db.Create(&[]model.Task{validTask, foreignTask, wrongUnitTask}).Error; err != nil {
		t.Fatal(err)
	}

	doJSON := func(method, path string, authCookie string, body any) (int, map[string]any) {
		t.Helper()
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		w := profileRequest(r, method, path, string(b), authCookie)
		var env map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to unmarshal response (%d %s): %v", w.Code, w.Body.String(), err)
		}
		return w.Code, env
	}

	// 1. 401 Unauthorized on both resolve-generated and PUT shots
	status, env := doJSON(http.MethodPost, "/api/projects/p/units/unit/shot-asset-bindings/resolve-generated", "", map[string]any{"taskId": "gen-ok"})
	if status != http.StatusUnauthorized || int(env["code"].(float64)) != service.CodeUnauthorized {
		t.Fatalf("expected 401 on resolve-generated without cookie, got %d %+v", status, env)
	}
	status, env = doJSON(http.MethodPut, "/api/projects/p/units/unit/shots", "", map[string]any{"shots": []any{}})
	if status != http.StatusUnauthorized || int(env["code"].(float64)) != service.CodeUnauthorized {
		t.Fatalf("expected 401 on PUT shots without cookie, got %d %+v", status, env)
	}

	// 2. 404 NotFound on resolve-generated for missing/foreign project, missing unit, and missing/foreign task
	for _, tc := range []struct {
		p, u, task string
	}{
		{"missing-p", "unit", "gen-ok"},
		{"foreign-p", "unit", "gen-ok"},
		{"p", "missing-unit", "gen-ok"},
		{"p", "unit", "missing-task"},
		{"p", "unit", "gen-foreign"},
	} {
		status, env := doJSON(http.MethodPost, "/api/projects/"+tc.p+"/units/"+tc.u+"/shot-asset-bindings/resolve-generated", cookie, map[string]any{"taskId": tc.task})
		if status != http.StatusNotFound || int(env["code"].(float64)) != service.CodeNotFound || env["reason"] != string(service.ReasonNotFound) {
			t.Fatalf("expected 404 not_found on resolve-generated for %+v, got status=%d env=%+v", tc, status, env)
		}
		if strings.Contains(env["msg"].(string), "SECRET_PROJECT") {
			t.Fatalf("leaked foreign project name in 404 response: %+v", env)
		}
	}

	// 3. 400 BadRequest on resolve-generated when task belongs to user but targets wrong unit
	status, env = doJSON(http.MethodPost, "/api/projects/p/units/unit/shot-asset-bindings/resolve-generated", cookie, map[string]any{"taskId": "gen-wrong-unit"})
	if status != http.StatusBadRequest || int(env["code"].(float64)) != service.CodeInvalidArgument {
		t.Fatalf("expected 400 invalid_argument on wrong-unit task, got status=%d env=%+v", status, env)
	}

	// 4. 404 NotFound on PUT /api/projects/:id/units/:unitId/shots for missing/foreign project, missing unit, and missing/foreign generatedBinding.taskId
	resolved, err := svc.ResolveGeneratedProjectUnitShotAssetBindings(httptest.NewRequest(http.MethodPost, "/", nil).Context(), "profile-user", "p", "unit", service.GeneratedBindingRequest{TaskID: "gen-ok"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		p, u, task string
	}{
		{"missing-p", "unit", "gen-ok"},
		{"foreign-p", "unit", "gen-ok"},
		{"p", "missing-unit", "gen-ok"},
		{"p", "unit", "missing-task"},
		{"p", "unit", "gen-foreign"},
	} {
		body := service.ReplaceProjectUnitShotsRequest{
			ExpectedShotIDs:  []string{"s1"},
			GeneratedBinding: &service.GeneratedBindingInput{TaskID: tc.task, Baseline: resolved.Baseline, RowIndices: []int{0}},
			Shots: []service.ReplaceProjectUnitShotInput{{
				CreateProjectShotRequest: service.CreateProjectShotRequest{Title: "镜头01", Description: "阿青出场", DurationMs: 5000},
				AssetVersionIDs:          []string{"v1"},
			}},
		}
		status, env := doJSON(http.MethodPut, "/api/projects/"+tc.p+"/units/"+tc.u+"/shots", cookie, body)
		if status != http.StatusNotFound || int(env["code"].(float64)) != service.CodeNotFound || env["reason"] != string(service.ReasonNotFound) {
			t.Fatalf("expected 404 not_found on PUT shots for %+v, got status=%d env=%+v", tc, status, env)
		}
		if strings.Contains(env["msg"].(string), "SECRET_PROJECT") {
			t.Fatalf("leaked foreign project name in PUT shots 404 response: %+v", env)
		}
	}
}

func setupLegacyMultiShotHandlerRouter(t *testing.T, dialect string) (*gin.Engine, *service.Service, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	options := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	if dialect == "sqlite" {
		path := filepath.ToSlash(filepath.Join(dir, "legacy-handler.db"))
		raw, err := gorm.Open(sqlite.Open("file:"+path+"?_journal_mode=WAL&_busy_timeout=5000"), options)
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := raw.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		db = raw
	} else {
		dsn := strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN"))
		if dsn == "" {
			t.Fatal("CANVAS_TEST_POSTGRES_DSN required for postgres dialect")
		}
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		admin, err := gorm.Open(postgres.Open(dsn), options)
		if err != nil {
			t.Fatal(err)
		}
		adminSQL, err := admin.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = adminSQL.Close() })
		schema := fmt.Sprintf("handler_legacy_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		})
		q := parsed.Query()
		q.Set("search_path", schema)
		q.Set("lock_timeout", "10000")
		q.Set("statement_timeout", "15000")
		parsed.RawQuery = q.Encode()
		raw, err := gorm.Open(postgres.Open(parsed.String()), options)
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := raw.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		db = raw
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "profile-user", Username: "profile-user", DisplayName: "Creator", Role: model.UserRoleUser, Status: model.UserStatusActive, PasswordHash: hash}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AuthSession{
		ID:        "profile-session",
		UserID:    user.ID,
		TokenHash: auth.HashToken("fixture-token"),
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), dir)
	ConfigureRuntime(svc)
	t.Cleanup(func() {
		ConfigureRuntime(nil)
		_ = svc.Close()
	})
	router := gin.New()
	RegisterAuthRoutes(router.Group("/api"), svc)
	registerProjectUnitRoutes(router.Group("/api"), svc)
	registerProjectWorkflowRoutes(router.Group("/api"), svc)
	return router, svc, db, "profile-session.fixture-token"
}

func captureHandlerLegacyDBState(t *testing.T, db *gorm.DB) string {
	t.Helper()
	state := map[string]any{}
	for _, table := range []string{
		"projects",
		"project_units",
		"shots",
		"shot_revisions",
		"shot_asset_references",
		"shot_artifacts",
		"project_asset_links",
		"assets",
		"asset_versions",
		"tasks",
		"resource_deletion_intents",
		"resource_deletion_jobs",
	} {
		var rows []map[string]any
		if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		state[table] = rows
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProjectUnitShotAssetBindingsLegacyMultiShotFullChainHTTP(t *testing.T) {
	dialects := []string{"sqlite"}
	if strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN")) != "" {
		dialects = append(dialects, "postgres")
	}

	for _, dialect := range dialects {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			r, _, db, cookie := setupLegacyMultiShotHandlerRouter(t, dialect)
			seedRows := []any{
				&model.User{ID: "foreign", Username: "foreign", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.Project{ID: "p-leg", UserID: "profile-user", Name: "handler-legacy", Revision: 9},
				&model.Project{ID: "foreign-p", UserID: "foreign", Name: "SECRET_FOREIGN_PROJ", Revision: 1},
				&model.ProjectUnit{ID: "unit-leg", ProjectID: "p-leg", Title: "u-leg"},
				&model.ProjectUnit{ID: "foreign-unit", ProjectID: "foreign-p", Title: "u-foreign"},

				// 3 shots:
				// s1: real addition (v-peirang-1) + existing multi-role v-sangruo-1 + succeeded/approved/failed artifacts
				// s2: all-existing (v-peirang-1) + succeeded artifact
				// s3: narrative/dialogue mention only (defaultSelected=false) + succeeded artifact
				&model.Shot{ID: "s1", ProjectID: "p-leg", UnitID: "unit-leg", CurrentRevisionID: "r1", Title: "镜头1", Description: "角色：裴让", DurationMs: 4000, Status: "ready", Position: 1},
				&model.Shot{ID: "s2", ProjectID: "p-leg", UnitID: "unit-leg", CurrentRevisionID: "r2", Title: "镜头2", Description: "角色：裴让", DurationMs: 4000, Status: "ready", Position: 2},
				&model.Shot{ID: "s3", ProjectID: "p-leg", UnitID: "unit-leg", CurrentRevisionID: "r3", Title: "镜头3", Description: "窗台晨雾", DurationMs: 3000, Status: "ready", Position: 3},
				&model.Shot{ID: "foreign-shot", ProjectID: "foreign-p", UnitID: "foreign-unit", CurrentRevisionID: "rf", Title: "SECRET_FOREIGN_SHOT", Description: "x", DurationMs: 3000, Status: "draft", Position: 1},

				&model.ShotRevision{ID: "r1", ShotID: "s1", Version: 1, PlotDescription: "角色：裴让", Dialogue: "桑若：请进", ActionBeatsJSON: `[]`},
				&model.ShotRevision{ID: "r2", ShotID: "s2", Version: 1, PlotDescription: "角色：裴让", Dialogue: "", ActionBeatsJSON: `[]`},
				&model.ShotRevision{ID: "r3", ShotID: "s3", Version: 1, PlotDescription: "桑若视线落在领口，裴让停止喝粥", Dialogue: "桑若：天亮了", ActionBeatsJSON: `[]`},
				&model.ShotRevision{ID: "rf", ShotID: "foreign-shot", Version: 1, PlotDescription: "foreign", ActionBeatsJSON: `[]`},

				&model.Asset{ID: "a-sangruo", UserID: "profile-user", Category: model.AssetCategoryCharacter, Title: "桑若", Status: "draft", PrimaryVersionID: "v-sangruo-1", PayloadJSON: `{}`},
				&model.Asset{ID: "a-peirang", UserID: "profile-user", Category: model.AssetCategoryCharacter, Title: "裴让", Status: "draft", PrimaryVersionID: "v-peirang-1", PayloadJSON: `{}`},
				&model.Asset{ID: "a-foreign", UserID: "foreign", Category: model.AssetCategoryCharacter, Title: "SECRET_FOREIGN_ASSET", Status: "draft", PrimaryVersionID: "v-foreign-1", PayloadJSON: `{}`},

				&model.ProjectAssetLink{ID: "l-sangruo", ProjectID: "p-leg", AssetID: "a-sangruo"},
				&model.ProjectAssetLink{ID: "l-peirang", ProjectID: "p-leg", AssetID: "a-peirang"},

				&model.AssetVersion{ID: "v-sangruo-1", AssetID: "a-sangruo", Version: 1, Status: "draft", DefinitionJSON: `{}`},
				&model.AssetVersion{ID: "v-peirang-1", AssetID: "a-peirang", Version: 1, Status: "draft", DefinitionJSON: `{}`},
				&model.AssetVersion{ID: "v-foreign-1", AssetID: "a-foreign", Version: 1, Status: "draft", DefinitionJSON: `{}`},

				// Existing multi-role references on s1 for v-sangruo-1 + existing reference on s2 for v-peirang-1
				&model.ShotAssetReference{ID: "ref-s1-char", ShotID: "s1", AssetVersionID: "v-sangruo-1", Role: "character", Status: "confirmed"},
				&model.ShotAssetReference{ID: "ref-s1-ref", ShotID: "s1", AssetVersionID: "v-sangruo-1", Role: "reference", Status: "linked"},
				&model.ShotAssetReference{ID: "ref-s2-char", ShotID: "s2", AssetVersionID: "v-peirang-1", Role: "character", Status: "confirmed"},

				// Historical ShotArtifact rows
				&model.ShotArtifact{ID: "art-s1-ok", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "s1", RevisionID: "r1", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"s":1}`},
				&model.ShotArtifact{ID: "art-s1-app", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "s1", RevisionID: "r1", Type: "image", Version: 2, Status: "approved", Selected: false, MetadataJSON: `{"s":11}`},
				&model.ShotArtifact{ID: "art-s1-fail", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "s1", RevisionID: "r1", Type: "image", Version: 3, Status: "failed", Selected: true, MetadataJSON: `{"s":12}`},
				&model.ShotArtifact{ID: "art-s2-ok", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "s2", RevisionID: "r2", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"s":2}`},
				&model.ShotArtifact{ID: "art-s3-ok", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "s3", RevisionID: "r3", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"s":3}`},
			}
			for _, row := range seedRows {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}

			preState := captureHandlerLegacyDBState(t, db)

			// 1. POST /preview + Cancel Zero-Write
			wPrev := profileRequest(r, http.MethodPost, "/api/projects/p-leg/units/unit-leg/shot-asset-bindings/preview", `{"shotIds":["s1","s2","s3"]}`, cookie)
			if wPrev.Code != http.StatusOK {
				t.Fatalf("expected 200 on preview, got %d: %s", wPrev.Code, wPrev.Body.String())
			}
			var prevResp struct {
				Code int                    `json:"code"`
				Data service.BindingPreview `json:"data"`
			}
			if err := json.Unmarshal(wPrev.Body.Bytes(), &prevResp); err != nil {
				t.Fatal(err)
			}
			if prevResp.Code != 0 || len(prevResp.Data.Shots) != 3 {
				t.Fatalf("unexpected preview response: %+v", prevResp)
			}
			if prevResp.Data.Shots[0].ExistingVersionCount != 1 || prevResp.Data.Shots[0].RemainingCapacity != 5 {
				t.Fatalf("s1 multi-role same-version should count as 1 distinct version: %+v", prevResp.Data.Shots[0])
			}
			for _, c := range prevResp.Data.Shots[2].Candidates {
				if c.DefaultSelected {
					t.Fatalf("s3 narrative/dialogue candidate must have defaultSelected=false: %+v", c)
				}
			}
			// User cancels before append -> 100% unchanged DB state
			if captureHandlerLegacyDBState(t, db) != preState {
				t.Fatalf("preview mutated DB state!")
			}

			// 2. All-existing selection (D = empty across all 3 shots) -> 200 OK unchanged=true, addedCount=0, zero DB writes
			status, envAllExist := doAppendRequest(t, r, cookie, "p-leg", "unit-leg", service.BindingAppendRequest{
				Baseline: prevResp.Data.Baseline,
				Selections: []service.BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-sangruo-1"}},
					{ShotID: "s2", AssetVersionIDs: []string{"v-peirang-1"}},
					{ShotID: "s3", AssetVersionIDs: []string{}},
				},
			})
			if status != http.StatusOK || int(envAllExist["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on all-existing append, got %d %+v", status, envAllExist)
			}
			dataAllExist := envAllExist["data"].(map[string]any)
			if !dataAllExist["unchanged"].(bool) || int(dataAllExist["addedCount"].(float64)) != 0 || int(dataAllExist["projectRevision"].(float64)) != 9 {
				t.Fatalf("unexpected all-existing append data: %+v", dataAllExist)
			}
			if captureHandlerLegacyDBState(t, db) != preState {
				t.Fatalf("all-existing append mutated DB state!")
			}

			// 3. Unauthorized foreign version mixed into multi-shot batch -> 422 rollback without foreign secret leak
			status, envBadVer := doAppendRequest(t, r, cookie, "p-leg", "unit-leg", service.BindingAppendRequest{
				Baseline: prevResp.Data.Baseline,
				Selections: []service.BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-sangruo-1", "v-peirang-1"}},
					{ShotID: "s2", AssetVersionIDs: []string{"v-peirang-1"}},
					{ShotID: "s3", AssetVersionIDs: []string{"v-foreign-1"}},
				},
			})
			if status != http.StatusUnprocessableEntity || int(envBadVer["code"].(float64)) != service.CodeStoryboardAssetBindingInvalidReference {
				t.Fatalf("expected 422 on foreign version in batch, got %d %+v", status, envBadVer)
			}
			if strings.Contains(fmt.Sprintf("%v", envBadVer), "SECRET_FOREIGN") {
				t.Fatalf("foreign secret leaked in 422 response: %+v", envBadVer)
			}
			if captureHandlerLegacyDBState(t, db) != preState {
				t.Fatalf("rejected batch mutated DB state!")
			}

			// 4. Selective multi-shot append: s1 adds v-peirang-1 (D != empty), s2 keeps all-existing (D = empty), s3 unselected (D = empty)
			selectiveReq := service.BindingAppendRequest{
				Baseline: prevResp.Data.Baseline,
				Selections: []service.BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-sangruo-1", "v-peirang-1"}},
					{ShotID: "s2", AssetVersionIDs: []string{"v-peirang-1"}},
					{ShotID: "s3", AssetVersionIDs: []string{}},
				},
			}
			status, envAppend := doAppendRequest(t, r, cookie, "p-leg", "unit-leg", selectiveReq)
			if status != http.StatusOK || int(envAppend["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on selective append, got %d %+v", status, envAppend)
			}
			dataAppend := envAppend["data"].(map[string]any)
			if dataAppend["unchanged"].(bool) || int(dataAppend["addedCount"].(float64)) != 1 || int(dataAppend["projectRevision"].(float64)) != 10 {
				t.Fatalf("unexpected selective append response data: %+v", dataAppend)
			}
			var arts []model.ShotArtifact
			if err := db.Order("id").Find(&arts).Error; err != nil {
				t.Fatal(err)
			}
			artMap := map[string]model.ShotArtifact{}
			for _, a := range arts {
				artMap[a.ID] = a
			}
			if artMap["art-s1-ok"].Status != "stale" || artMap["art-s1-ok"].Selected ||
				artMap["art-s1-app"].Status != "stale" || artMap["art-s1-app"].Selected ||
				artMap["art-s1-fail"].Status != "failed" || artMap["art-s1-fail"].Selected {
				t.Fatalf("s1 artifacts not selectively invalidated properly: %+v", arts)
			}
			if artMap["art-s2-ok"].Status != "succeeded" || !artMap["art-s2-ok"].Selected ||
				artMap["art-s3-ok"].Status != "succeeded" || !artMap["art-s3-ok"].Selected {
				t.Fatalf("s2/s3 untouched artifacts were mutated: s2=%+v s3=%+v", artMap["art-s2-ok"], artMap["art-s3-ok"])
			}
			var refs []model.ShotAssetReference
			if err := db.Where("shot_id = ?", "s1").Order("id").Find(&refs).Error; err != nil {
				t.Fatal(err)
			}
			if len(refs) != 3 {
				t.Fatalf("expected 3 references on s1 (2 existing multi-role + 1 new), got %+v", refs)
			}
			refIDs := []string{refs[0].ID, refs[1].ID, refs[2].ID}
			if !slices.Contains(refIDs, "ref-s1-char") || !slices.Contains(refIDs, "ref-s1-ref") {
				t.Fatalf("existing multi-role reference IDs not preserved: %+v", refs)
			}

			postCommitState := captureHandlerLegacyDBState(t, db)

			// 5. Disconnect Idempotent Replay of exact selectiveReq -> 200 OK unchanged=true, addedCount=0, projectRevision=10
			status, envReplay := doAppendRequest(t, r, cookie, "p-leg", "unit-leg", selectiveReq)
			if status != http.StatusOK || int(envReplay["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on disconnect replay, got %d %+v", status, envReplay)
			}
			dataReplay := envReplay["data"].(map[string]any)
			if !dataReplay["unchanged"].(bool) || int(dataReplay["addedCount"].(float64)) != 0 || int(dataReplay["projectRevision"].(float64)) != 10 {
				t.Fatalf("unexpected disconnect replay data: %+v", dataReplay)
			}
			if captureHandlerLegacyDBState(t, db) != postCommitState {
				t.Fatalf("disconnect replay mutated DB state!")
			}

			// 6. Post-Commit Drift -> 409 Conflict
			if err := db.Model(&model.ShotArtifact{}).Where("id = ?", "art-s1-ok").Updates(map[string]any{"selected": true, "status": "succeeded"}).Error; err != nil {
				t.Fatal(err)
			}
			driftState := captureHandlerLegacyDBState(t, db)
			status, envDrift := doAppendRequest(t, r, cookie, "p-leg", "unit-leg", selectiveReq)
			if status != http.StatusConflict || int(envDrift["code"].(float64)) != service.CodeConflict || envDrift["reason"] != string(service.ReasonStoryboardAssetBindingConflict) {
				t.Fatalf("expected 409 conflict on post-commit drift replay, got %d %+v", status, envDrift)
			}
			if captureHandlerLegacyDBState(t, db) != driftState {
				t.Fatalf("rejected 409 drift replay mutated DB state!")
			}
		})
	}
}


