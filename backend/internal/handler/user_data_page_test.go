package handler

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func TestCanvasRevisionRecoveryHTTP(t *testing.T) {
	router, db, cookie := profileHandlerFixture(t)
	svc := service.New(repository.New(db), t.TempDir())
	t.Cleanup(func() { _ = svc.Close() })
	RegisterUserDataRoutes(router.Group("/api"), svc)
	for _, record := range []any{
		&model.CanvasProject{ID: "history", UserID: "profile-user", Revision: 2, PayloadJSON: `{"nodes":[],"connections":[]}`},
		&model.CanvasProject{ID: "foreign", UserID: "other", Revision: 2, PayloadJSON: `{"nodes":[],"connections":[]}`},
		&model.CanvasMutationReceipt{ID: "receipt", UserID: "profile-user", CanvasID: "history", Revision: 1, MutationID: "original", ResultJSON: `{"document":{"id":"history","revision":1,"title":"original","nodes":[],"connections":[]}}`},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path, auth string
		status     int
	}{
		{"history/revisions/1", cookie, 200}, {"history/revisions/1", "", 401}, {"foreign/revisions/1", cookie, 404},
		{"history/revisions/99", cookie, 404}, {"history/revisions/no", cookie, 400}, {"history/revisions/0", cookie, 400},
	} {
		rec := profileRequest(router, http.MethodGet, "/api/canvas-projects/"+tc.path, "", tc.auth)
		if rec.Code != tc.status {
			t.Fatalf("%s: status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		if tc.status == 200 && (!strings.Contains(rec.Body.String(), `"title":"original"`) || rec.Header().Get("Cache-Control") != "private, no-store") {
			t.Fatalf("unexpected historical response: %s %v", rec.Body.String(), rec.Header())
		}
	}
}

func TestUserDataIncrementalRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), &service.Service{})
	wanted := map[string]bool{
		"GET /api/canvas-projects":                         false,
		"GET /api/canvas-projects/:id":                     false,
		"GET /api/canvas-projects/:id/revisions/:revision": false,
		"POST /api/assets/batch":                           false,
		"POST /api/assets/batch-delete":                    false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := wanted[key]; exists {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Errorf("missing route: %s", route)
		}
	}
}

func TestCanvasProjectResponseETagChangesWithRevisionMetadata(t *testing.T) {
	project := &model.CanvasProject{Revision: 7, UpdatedAt: time.Date(2026, 9, 28, 12, 0, 0, 123000000, time.UTC)}
	first := canvasProjectResponseETag(project)
	if first != `W/"canvas-7"` {
		t.Fatalf("unexpected canvas ETag: %s", first)
	}
	if !ifNoneMatch(`"canvas-7"`, first) || !ifNoneMatch(first, first) {
		t.Fatal("strong and weak validators should match for conditional GET")
	}
	project.Revision++
	if first == canvasProjectResponseETag(project) {
		t.Fatal("revision change must invalidate the canvas ETag")
	}
}

func TestAcceptsGzipHonorsQualityAndWildcard(t *testing.T) {
	for _, test := range []struct {
		header string
		want   bool
	}{
		{header: "gzip, br", want: true},
		{header: "gzip;q=0, *;q=1", want: false},
		{header: "br, *;q=0.5", want: true},
		{header: "br, gzip;q=0.2", want: true},
		{header: "br, gzip;q=0", want: false},
	} {
		if got := acceptsGzip(test.header); got != test.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", test.header, got, test.want)
		}
	}
}

func TestOkCanvasProjectCompressesLargeResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/canvas-projects/canvas", nil)
	ctx.Request.Header.Set("Accept-Encoding", "gzip")
	project := json.RawMessage(`{"nodes":["` + strings.Repeat("x", 2048) + `"]}`)

	okCanvasProject(ctx, project)

	if got := recorder.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	reader, err := gzip.NewReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(body) {
		t.Fatalf("decompressed response is not valid JSON: %s", body)
	}
}
