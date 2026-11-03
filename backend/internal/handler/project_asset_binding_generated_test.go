package handler

import (
	"net/http"
	"strings"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

func TestStoryboardAssetGeneratedHTTP(t *testing.T) {
	router, db, cookie := profileHandlerFixture(t)
	svc := service.New(repository.New(db), t.TempDir())
	t.Cleanup(func() { _ = svc.Close() })
	registerProjectUnitRoutes(router.Group("/api"), svc)
	for _, row := range []any{&model.Project{ID: "generated-p", UserID: "profile-user", Name: "generated"}, &model.ProjectUnit{ID: "generated-u", ProjectID: "generated-p"}, &model.Task{ID: "generated-t", UserID: "profile-user", ProjectID: "generated-p", Type: "canvas_text", Operation: "storyboard", Status: model.TaskStatusSucceeded, InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"generated-p","unitId":"generated-u","operation":"storyboard","allowedAssetIds":[]}}`, ResultJSON: `{"rows":[{"projectAssetRefs":[]}]}`}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/projects/generated-p/units/generated-u/shot-asset-bindings/resolve-generated"
	for _, tc := range []struct {
		body, cookie string
		status       int
	}{{`{"taskId":"generated-t"}`, "", http.StatusUnauthorized}, {`{"taskId":""}`, cookie, http.StatusBadRequest}, {`{"taskId":"generated-t"}`, cookie, http.StatusOK}} {
		response := profileRequest(router, "POST", path, tc.body, tc.cookie)
		if response.Code != tc.status {
			t.Fatalf("HTTP %d want %d: %s", response.Code, tc.status, response.Body.String())
		}
		if tc.status == http.StatusOK && !strings.Contains(response.Body.String(), `"taskResultFingerprint"`) {
			t.Fatal("generated envelope missing baseline")
		}
		if tc.status != http.StatusOK && strings.Contains(response.Body.String(), `"code":0`) {
			t.Fatal("failure returned success envelope")
		}
	}
}
