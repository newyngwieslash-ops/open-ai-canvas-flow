package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

func TestStoryboardAssetPreviewRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerProjectUnitRoutes(router.Group("/api"), &service.Service{})
	for _, route := range router.Routes() {
		if route.Method == "POST" && route.Path == "/api/projects/:id/units/:unitId/shot-asset-bindings/preview" {
			return
		}
	}
	t.Fatal("read-only preview POST route missing")
}

func TestStoryboardAssetPreviewHTTP(t *testing.T) {
	router, db, cookie := profileHandlerFixture(t)
	svc := service.New(repository.New(db), t.TempDir())
	t.Cleanup(func() { _ = svc.Close() })
	registerProjectUnitRoutes(router.Group("/api"), svc)
	for _, row := range []any{&model.Project{ID: "preview-p", UserID: "profile-user", Name: "preview", Revision: 3}, &model.ProjectUnit{ID: "preview-u", ProjectID: "preview-p"}, &model.Shot{ID: "preview-s", ProjectID: "preview-p", UnitID: "preview-u"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/projects/preview-p/units/preview-u/shot-asset-bindings/preview"
	for _, tc := range []struct {
		body, cookie string
		status       int
	}{
		{`{"shotIds":["preview-s"]}`, "", http.StatusUnauthorized},
		{`{"shotIds":[]}`, cookie, http.StatusBadRequest},
		{`{"shotIds":["foreign"]}`, cookie, http.StatusNotFound},
		{`{"shotIds":["preview-s"]}`, cookie, http.StatusOK},
	} {
		response := profileRequest(router, "POST", path, tc.body, tc.cookie)
		if response.Code != tc.status {
			t.Fatalf("HTTP %d, want %d: %s", response.Code, tc.status, response.Body.String())
		}
		if tc.status == http.StatusOK && (!strings.Contains(response.Body.String(), `"baseline"`) || !strings.Contains(response.Body.String(), `"code":0`)) {
			t.Fatal("preview response envelope mismatch")
		}
		if tc.status != http.StatusOK && strings.Contains(response.Body.String(), `"code":0`) {
			t.Fatal("failure returned successful business code")
		}
	}
}
