package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

// 预检响应合同：data 直接是 ProjectAssetBatchPreflightResult（见 openapi.yaml），
// 前端 preflightProjectAssetBatch 依赖 data.items / data.preflightDigest。
func TestProjectAssetBatchPreflightReturnsResultAsEnvelopeData(t *testing.T) {
	router, db, cookie := profileHandlerFixture(t)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), dataDir)
	t.Cleanup(func() { _ = svc.Close() })
	registerCharacterLibraryRoutes(router.Group("/api"), svc)
	if err := db.Save(&model.SystemSetting{Key: "feature_availability", ValueJSON: `{"customChannelsEnabled":true}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{ID: "p", UserID: "profile-user", Name: "batch-proj", Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	character, err := svc.CreateProjectCharacter("profile-user", "p", service.CreateProjectCharacterRequest{Name: "主角"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(service.PreflightProjectAssetBatchRequest{
		Provider: "custom",
		Model:    "gemini-2.5-flash-image",
		Config: map[string]any{
			"interfaceType": "gemini-image",
			"baseUrl":       "https://api.example.com/v1",
			"apiKey":        "synthetic-key",
			"model":         "gemini-2.5-flash-image",
		},
		Items: []service.ProjectAssetBatchItemInput{{
			AssetID:           character.Asset.ID,
			ExpectedVersionID: character.Asset.PrimaryVersionID,
			ReferenceSource:   "none",
			Prompt:            "角色三视图",
			Operation:         "character_turnaround",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := profileRequest(router, http.MethodPost, "/api/projects/p/asset-generation-batches/preflight", string(body), cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			PreflightDigest string           `json:"preflightDigest"`
			Items           []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Code != 0 || env.Data.PreflightDigest == "" || len(env.Data.Items) != 1 {
		t.Fatalf("preflight envelope data must be the result itself: %s", w.Body.String())
	}
}
