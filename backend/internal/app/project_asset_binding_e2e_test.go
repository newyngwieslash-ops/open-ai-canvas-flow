package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type capturedUpstreamState struct {
	mu            sync.Mutex
	textPrompts   []string
	imageRequests []map[string]any
	videoRequests []map[string]any
	textResponse  string
}

func (c *capturedUpstreamState) setTextResponse(raw string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.textResponse = raw
}

func (c *capturedUpstreamState) lastTextPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.textPrompts) == 0 {
		return ""
	}
	return c.textPrompts[len(c.textPrompts)-1]
}

func (c *capturedUpstreamState) lastImageRequest() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.imageRequests) == 0 {
		return nil
	}
	return c.imageRequests[len(c.imageRequests)-1]
}

func (c *capturedUpstreamState) lastVideoRequest() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.videoRequests) == 0 {
		return nil
	}
	return c.videoRequests[len(c.videoRequests)-1]
}

func makeDistinctPNGBytes(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeLocalImageResource(t *testing.T, db *gorm.DB, dataDir, userID, resourceID string, pngBytes []byte) string {
	t.Helper()
	resDir := filepath.Join(dataDir, "resources")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	objectKey := resourceID + ".png"
	if err := os.WriteFile(filepath.Join(resDir, objectKey), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	res := model.Resource{
		ID:        resourceID,
		UserID:    userID,
		Provider:  "local",
		ObjectKey: objectKey,
		MimeType:  "image/png",
		Width:     4,
		Height:    4,
		Size:      int64(len(pngBytes)),
		Kind:      "image",
		Status:    model.ResourceStatusReady,
	}
	if err := db.Create(&res).Error; err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
}

func startFakeE2EUpstreamServer(t *testing.T) (*httptest.Server, *capturedUpstreamState) {
	t.Helper()
	state := &capturedUpstreamState{}
	outPNG := makeDistinctPNGBytes(t, 200, 210, 220)
	outPNGB64 := base64.StdEncoding.EncodeToString(outPNG)
	fakeMP4 := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0x00, 0x00, 0x00, 0x00, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			var sb strings.Builder
			if msgs, ok := body["messages"].([]any); ok {
				for _, m := range msgs {
					if msgMap, ok := m.(map[string]any); ok {
						rawContent, _ := json.Marshal(msgMap["content"])
						if strContent, ok := msgMap["content"].(string); ok {
							sb.WriteString(strContent)
						} else {
							sb.Write(rawContent)
						}
						sb.WriteString("\n")
					}
				}
			}
			state.mu.Lock()
			state.textPrompts = append(state.textPrompts, sb.String())
			reply := state.textResponse
			state.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": reply}},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/generations"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			state.mu.Lock()
			state.imageRequests = append(state.imageRequests, body)
			state.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"b64_json": outPNGB64},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/videos"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			state.mu.Lock()
			state.videoRequests = append(state.videoRequests, body)
			idx := len(state.videoRequests)
			state.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": fmt.Sprintf("vid-e2e-%d", idx),
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/videos/") && strings.HasSuffix(r.URL.Path, "/content"):
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeMP4)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/videos/"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "succeeded",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", parsed.Hostname())
	return srv, state
}

func openE2EDialectService(t *testing.T, dialect string) (*Service, *gorm.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	var db *gorm.DB
	if dialect == "sqlite" {
		raw, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "e2e.db")+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := raw.DB()
		if err != nil {
			t.Fatal(err)
		}
		sqlDB.SetMaxOpenConns(4)
		t.Cleanup(func() { _ = sqlDB.Close() })
		db = raw
	} else {
		db = profileConcurrencyDatabases(t, "postgres")[0]
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		sqlDB.SetMaxOpenConns(4)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db), dataDir: dataDir, activeCancels: make(map[string]context.CancelFunc)}
	if err := os.WriteFile(filepath.Join(dataDir, ".settings-key"), []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFeatureAvailability(&model.User{ID: "admin", Role: "admin"}, FeatureAvailability{CustomChannelsEnabled: true, TaskCenterEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return svc, db, dataDir
}

func executeCanvasTaskAndPersistResult(t *testing.T, svc *Service, db *gorm.DB, taskID string) *model.Task {
	t.Helper()
	saved, err := svc.repo.Task(taskID)
	if err != nil {
		t.Fatal(err)
	}
	hydrated, err := svc.decryptTaskInputJSON(saved.InputJSON)
	if err != nil {
		t.Fatal(err)
	}
	resultMap, err := svc.processCanvasGenerationTask(context.Background(), saved.UserID, saved.ProjectID, saved.Type, saved.Prompt, hydrated)
	if err != nil {
		t.Fatalf("processCanvasGenerationTask(%s) failed: %v", taskID, err)
	}
	rawResult, err := json.Marshal(resultMap)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", taskID).Updates(map[string]any{
		"status":      model.TaskStatusSucceeded,
		"result_json": string(rawResult),
	}).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := svc.repo.Task(taskID)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestStoryboardAssetE2EGeneratedFullChain(t *testing.T) {
	dialects := []string{"sqlite"}
	if strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN")) != "" {
		dialects = append(dialects, "postgres")
	}

	for _, dialect := range dialects {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			svc, db, dataDir := openE2EDialectService(t, dialect)
			upstream, captured := startFakeE2EUpstreamServer(t)
			ctx := context.Background()

			// 1. Seed isolated project fixture:
			// - Zero existing ShotAssetReference rows in chapter unit-e2e
			// - 3 categories (character, environment, prop)
			// - 82 linked project assets spanning 2 candidate pages (pageSize=80 -> 80 on page 1, 2 on page 2)
			// - Duplicate-named assets ("黑衣卫") split across Page 1 (char-dup-p1) and Page 2 (char-dup-p2)
			// - Versionless asset (prop-no-ver -> missing_version)
			// - Bindable version without reference image (char-no-img / v-no-img-1 -> missing_image), while another non-bound version on the same asset has an image
			baseTime := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
			seedRows := []any{
				&model.User{ID: "u", Username: "e2e-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.User{ID: "foreign", Username: "e2e-foreign", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.Project{ID: "p-e2e", UserID: "u", Name: "E2E项目", Revision: 1},
				&model.Project{ID: "p-other", UserID: "u", Name: "其他项目", Revision: 1},
				&model.ProjectUnit{ID: "unit-e2e", ProjectID: "p-e2e", Title: "第一章 青云夜雨", SourceText: "阿青持青钢剑踏入青云大殿，无图老者静坐，黑衣卫从暗处现身。"},
				&model.ProjectUnit{ID: "unit-other", ProjectID: "p-e2e", Title: "第二章"},
				&model.ProjectUnit{ID: "unit-other-p", ProjectID: "p-other", Title: "异项目章节"},
				&model.Shot{ID: "shot-init-1", ProjectID: "p-e2e", UnitID: "unit-e2e", CurrentRevisionID: "rev-init-1", Title: "初始镜头", Description: "占位分镜", DurationMs: 3000, Status: "draft", Position: 0},
				&model.ShotRevision{ID: "rev-init-1", ShotID: "shot-init-1", Version: 1, PlotDescription: "占位分镜", DurationMs: 3000, ActionBeatsJSON: `[]`},
			}
			for _, row := range seedRows {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}

			heroV1DataURL := writeLocalImageResource(t, db, dataDir, "u", "res-hero-v1", makeDistinctPNGBytes(t, 10, 20, 30))
			heroV2DataURL := writeLocalImageResource(t, db, dataDir, "u", "res-hero-v2", makeDistinctPNGBytes(t, 99, 88, 77))
			envV1DataURL := writeLocalImageResource(t, db, dataDir, "u", "res-env-v1", makeDistinctPNGBytes(t, 40, 50, 60))
			propV1DataURL := writeLocalImageResource(t, db, dataDir, "u", "res-prop-v1", makeDistinctPNGBytes(t, 70, 80, 90))
			dupP1DataURL := writeLocalImageResource(t, db, dataDir, "u", "res-dup-p1", makeDistinctPNGBytes(t, 110, 120, 130))
			dupP2DataURL := writeLocalImageResource(t, db, dataDir, "u", "res-dup-p2", makeDistinctPNGBytes(t, 140, 150, 160))
			wrongFallbackDataURL := writeLocalImageResource(t, db, dataDir, "u", "res-wrong-fallback", makeDistinctPNGBytes(t, 250, 10, 10))
			fillDataURL := writeLocalImageResource(t, db, dataDir, "u", "res-fill", makeDistinctPNGBytes(t, 180, 180, 180))
			_, _, _ = dupP1DataURL, dupP2DataURL, fillDataURL

			coreAssets := []any{
				// 1. Character with bound historical version v-hero-1
				&model.Asset{ID: "char-hero", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "阿青", Status: "draft", PrimaryVersionID: "v-hero-1", PayloadJSON: `{"summary":"孤高剑客"}`, CreatedAt: baseTime.Add(200 * time.Minute), UpdatedAt: baseTime.Add(200 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-hero", ProjectID: "p-e2e", AssetID: "char-hero"},
				&model.AssetVersion{ID: "v-hero-1", AssetID: "char-hero", Version: 1, Status: "draft", DefinitionJSON: `{"summary":"阿青初版黑衣剑装","outfit":"黑衣剑装"}`, CreatedAt: baseTime, UpdatedAt: baseTime},
				&model.AssetRepresentation{ID: "rep-hero-1", AssetVersionID: "v-hero-1", ResourceID: "res-hero-v1", Role: "primary", MediaType: "image", MetadataJSON: `{}`},

				// 2. Environment asset
				&model.Asset{ID: "env-hall", UserID: "u", Category: model.AssetCategoryEnvironment, Kind: "image", Title: "青云大殿", Status: "draft", PrimaryVersionID: "v-env-1", PayloadJSON: `{"summary":"古殿夜雨"}`, CreatedAt: baseTime.Add(199 * time.Minute), UpdatedAt: baseTime.Add(199 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-env", ProjectID: "p-e2e", AssetID: "env-hall"},
				&model.AssetVersion{ID: "v-env-1", AssetID: "env-hall", Version: 1, Status: "draft", DefinitionJSON: `{"summary":"青云大殿石柱灯火","lighting":"夜雨烛火"}`, CreatedAt: baseTime, UpdatedAt: baseTime},
				&model.AssetRepresentation{ID: "rep-env-1", AssetVersionID: "v-env-1", ResourceID: "res-env-v1", Role: "primary", MediaType: "image", MetadataJSON: `{}`},

				// 3. Prop asset
				&model.Asset{ID: "prop-sword", UserID: "u", Category: model.AssetCategoryProp, Kind: "image", Title: "青钢剑", Status: "draft", PrimaryVersionID: "v-prop-1", PayloadJSON: `{"summary":"古剑青光"}`, CreatedAt: baseTime.Add(198 * time.Minute), UpdatedAt: baseTime.Add(198 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-prop", ProjectID: "p-e2e", AssetID: "prop-sword"},
				&model.AssetVersion{ID: "v-prop-1", AssetID: "prop-sword", Version: 1, Status: "draft", DefinitionJSON: `{"summary":"青钢古剑寒芒","material":"青钢"}`, CreatedAt: baseTime, UpdatedAt: baseTime},
				&model.AssetRepresentation{ID: "rep-prop-1", AssetVersionID: "v-prop-1", ResourceID: "res-prop-v1", Role: "primary", MediaType: "image", MetadataJSON: `{}`},

				// 4. Bindable asset whose bound version (v-no-img-1) has NO reference image (missing_image), while an older version (v-no-img-other) has an image
				&model.Asset{ID: "char-no-img", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "无图老者", Status: "draft", PrimaryVersionID: "v-no-img-1", PayloadJSON: `{}`, CreatedAt: baseTime.Add(197 * time.Minute), UpdatedAt: baseTime.Add(197 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-no-img", ProjectID: "p-e2e", AssetID: "char-no-img"},
				&model.AssetVersion{ID: "v-no-img-other", AssetID: "char-no-img", Version: 1, Status: "draft", DefinitionJSON: `{"summary":"旧版有图老者"}`},
				&model.AssetRepresentation{ID: "rep-no-img-other", AssetVersionID: "v-no-img-other", ResourceID: "res-wrong-fallback", Role: "primary", MediaType: "image", MetadataJSON: `{}`},
				&model.AssetVersion{ID: "v-no-img-1", AssetID: "char-no-img", Version: 2, Status: "draft", DefinitionJSON: `{"summary":"新版无图老者"}`},

				// 5. Versionless asset (missing_version)
				&model.Asset{ID: "prop-no-ver", UserID: "u", Category: model.AssetCategoryProp, Kind: "image", Title: "断剑残片", Status: "draft", PrimaryVersionID: "", PayloadJSON: `{}`, CreatedAt: baseTime.Add(196 * time.Minute), UpdatedAt: baseTime.Add(196 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-no-ver", ProjectID: "p-e2e", AssetID: "prop-no-ver"},

				// 6. Duplicate-named asset on Page 1 (high UpdatedAt)
				&model.Asset{ID: "char-dup-p1", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "黑衣卫", Status: "draft", PrimaryVersionID: "v-dup-p1", PayloadJSON: `{}`, CreatedAt: baseTime.Add(195 * time.Minute), UpdatedAt: baseTime.Add(195 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-dup-p1", ProjectID: "p-e2e", AssetID: "char-dup-p1"},
				&model.AssetVersion{ID: "v-dup-p1", AssetID: "char-dup-p1", Version: 1, Status: "draft", DefinitionJSON: `{"summary":"甲队黑衣卫"}`},
				&model.AssetRepresentation{ID: "rep-dup-p1", AssetVersionID: "v-dup-p1", ResourceID: "res-dup-p1", Role: "primary", MediaType: "image", MetadataJSON: `{}`},

				// 7. Duplicate-named asset on Page 2 (oldest UpdatedAt so it lands on page 2 after 80 items)
				&model.Asset{ID: "char-dup-p2", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "黑衣卫", Status: "draft", PrimaryVersionID: "v-dup-p2", PayloadJSON: `{}`, CreatedAt: baseTime.Add(-10 * time.Minute), UpdatedAt: baseTime.Add(-10 * time.Minute)},
				&model.ProjectAssetLink{ID: "link-dup-p2", ProjectID: "p-e2e", AssetID: "char-dup-p2"},
				&model.AssetVersion{ID: "v-dup-p2", AssetID: "char-dup-p2", Version: 1, Status: "draft", DefinitionJSON: `{"summary":"乙队黑衣卫"}`},
				&model.AssetRepresentation{ID: "rep-dup-p2", AssetVersionID: "v-dup-p2", ResourceID: "res-dup-p2", Role: "primary", MediaType: "image", MetadataJSON: `{}`},
			}
			for _, row := range coreAssets {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}

			// Seed 75 filler assets so total linked project assets = 7 + 75 = 82 (spans 2 pages of pageSize=80).
			categories := []model.AssetCategory{model.AssetCategoryCharacter, model.AssetCategoryEnvironment, model.AssetCategoryProp}
			for i := 1; i <= 75; i++ {
				assetID := fmt.Sprintf("fill-asset-%02d", i)
				versionID := fmt.Sprintf("fill-ver-%02d", i)
				ts := baseTime.Add(time.Duration(i) * time.Minute)
				cat := categories[(i-1)%len(categories)]
				kind := "image"
				if cat == model.AssetCategoryCharacter {
					kind = "entity"
				}
				rows := []any{
					&model.Asset{ID: assetID, UserID: "u", Category: cat, Kind: kind, Title: fmt.Sprintf("配角资产%02d", i), Status: "draft", PrimaryVersionID: versionID, PayloadJSON: `{}`, CreatedAt: ts, UpdatedAt: ts},
					&model.ProjectAssetLink{ID: fmt.Sprintf("link-fill-%02d", i), ProjectID: "p-e2e", AssetID: assetID},
					&model.AssetVersion{ID: versionID, AssetID: assetID, Version: 1, Status: "draft", DefinitionJSON: fmt.Sprintf(`{"summary":"描述%02d"}`, i)},
					&model.AssetRepresentation{ID: fmt.Sprintf("rep-fill-%02d", i), AssetVersionID: versionID, ResourceID: "res-fill", Role: "primary", MediaType: "image", MetadataJSON: `{}`},
				}
				for _, row := range rows {
					if err := db.Create(row).Error; err != nil {
						t.Fatal(err)
					}
				}
			}

			var initialRefCount int64
			if err := db.Model(&model.ShotAssetReference{}).Count(&initialRefCount).Error; err != nil || initialRefCount != 0 {
				t.Fatalf("expected 0 initial ShotAssetReference rows, got %d (err=%v)", initialRefCount, err)
			}

			// 2. Candidate Pagination Verification (page=1, pageSize=80 + page=2, pageSize=80)
			page1, err := svc.ProjectAssetsPage("u", "p-e2e", 1, 80, "", "", "", nil, "")
			if err != nil {
				t.Fatalf("ProjectAssetsPage page 1 failed: %v", err)
			}
			page2, err := svc.ProjectAssetsPage("u", "p-e2e", 2, 80, "", "", "", nil, "")
			if err != nil {
				t.Fatalf("ProjectAssetsPage page 2 failed: %v", err)
			}
			if page1.Total != 82 || len(page1.Assets) != 80 || !page1.HasMore || len(page2.Assets) != 2 || page2.HasMore {
				t.Fatalf("unexpected candidate pagination: total=%d page1=%d(hasMore=%v) page2=%d(hasMore=%v)", page1.Total, len(page1.Assets), page1.HasMore, len(page2.Assets), page2.HasMore)
			}

			page1IDs := make(map[string]ProjectAssetSummary, len(page1.Assets))
			for _, item := range page1.Assets {
				page1IDs[item.ID] = item
			}
			page2IDs := make(map[string]ProjectAssetSummary, len(page2.Assets))
			for _, item := range page2.Assets {
				page2IDs[item.ID] = item
			}
			if _, ok := page1IDs["char-dup-p1"]; !ok {
				t.Fatalf("expected char-dup-p1 on page 1")
			}
			if _, ok := page2IDs["char-dup-p2"]; !ok {
				t.Fatalf("expected char-dup-p2 on page 2")
			}

			// 3. CreateTask (canvas_text / storyboard) + Fake Provider Capture + Completion
			generatedRowsJSON := `{"rows":[` +
				`{"shotNumber":"SC.01","plotDescription":"阿青手持青钢剑走入青云大殿","dialogue":"阿青：来者何人","shotSize":"中景","camera":"平视","motion":"推进","durationSeconds":5,"imageGenerationPrompt":"阿青在青云大殿持青钢剑","videoMotionPrompt":"阿青持剑缓步前行","negativePrompt":"","continuityOut":"","timeBeats":"1s-5s 踏入大殿","projectAssetRefs":[{"assetId":"char-hero","category":"character"},{"assetId":"env-hall","category":"environment"},{"assetId":"prop-sword","category":"prop"}]},` +
				`{"shotNumber":"SC.02","plotDescription":"无图老者在殿角静坐凝视","dialogue":"","shotSize":"近景","camera":"平视","motion":"固定","durationSeconds":4,"imageGenerationPrompt":"无图老者静坐","videoMotionPrompt":"无图老者抬头","negativePrompt":"","continuityOut":"","timeBeats":"","projectAssetRefs":[{"assetId":"char-no-img","category":"character"}]},` +
				`{"shotNumber":"SC.03","plotDescription":"黑衣卫从殿柱暗影中现身包围","dialogue":"","shotSize":"全景","camera":"俯视","motion":"拉远","durationSeconds":4,"imageGenerationPrompt":"黑衣卫现身","videoMotionPrompt":"黑衣卫跃出","negativePrompt":"","continuityOut":"","timeBeats":"","projectAssetRefs":[{"category":"character","name":"黑衣卫"}]}` +
				`]}`
			captured.setTextResponse(generatedRowsJSON)

			promptText := "请根据章节正文生成分镜脚本：\n阿青持青钢剑踏入青云大殿，无图老者静坐，黑衣卫从暗处现身。"
			createdTask, err := svc.CreateTask("u", CreateTaskRequest{
				ProjectID: "p-e2e",
				Type:      "canvas_text",
				Operation: "storyboard",
				Prompt:    promptText,
				Input: map[string]any{
					"mode":        "text",
					"textOptions": map[string]any{"stream": false},
					"chapterAssetContext": map[string]any{
						"projectId": "p-e2e",
						"unitId":    "unit-e2e",
						"scope":     map[string]any{"kind": "all", "assetIds": []string{}},
					},
					"config": map[string]any{
						"baseUrl":       upstream.URL,
						"apiKey":        "e2e-test-key",
						"model":         "gpt-4o-mini",
						"interfaceType": "chat-completion",
						"capabilityConfig": map[string]any{
							"text": map[string]any{"contextWindowTokens": 128000, "maxOutputTokens": 16384},
						},
					},
				},
			})
			if err != nil {
				t.Fatalf("CreateTask failed: %v", err)
			}

			// Verify server computed chapterAssetContext.allowedAssetIds & summary across all 82 linked assets (both pages)
			savedTaskBeforeRun, err := svc.repo.Task(createdTask.ID)
			if err != nil {
				t.Fatal(err)
			}
			var persistedInput map[string]any
			if err := json.Unmarshal([]byte(savedTaskBeforeRun.InputJSON), &persistedInput); err != nil {
				t.Fatal(err)
			}
			trustedCtx, _ := persistedInput["chapterAssetContext"].(map[string]any)
			rawAllowed, _ := trustedCtx["allowedAssetIds"].([]any)
			if len(rawAllowed) != 82 {
				t.Fatalf("expected 82 allowedAssetIds in chapterAssetContext, got %d (%v)", len(rawAllowed), trustedCtx)
			}
			persistedSummary, _ := trustedCtx["summary"].(string)
			if persistedSummary == "" {
				t.Fatalf("expected non-empty persisted chapterAssetContext.summary")
			}

			// Run the text generation task through processCanvasGenerationTask and persist completion
			completedTask := executeCanvasTaskAndPersistResult(t, svc, db, createdTask.ID)

			upstreamPrompt := captured.lastTextPrompt()
			if !strings.Contains(upstreamPrompt, persistedSummary) {
				t.Fatalf("upstream text prompt missing persisted chapterAssetContext.summary: %s", upstreamPrompt)
			}
			if !strings.Contains(upstreamPrompt, "projectAssetRefs") || !strings.Contains(upstreamPrompt, "char-dup-p2") {
				t.Fatalf("upstream text prompt missing projectAssetRefs contract or page-2 asset char-dup-p2")
			}

			// 4. ResolveGeneratedProjectUnitShotAssetBindings
			resolved, err := svc.ResolveGeneratedProjectUnitShotAssetBindings(ctx, "u", "p-e2e", "unit-e2e", GeneratedBindingRequest{TaskID: completedTask.ID})
			if err != nil {
				t.Fatalf("ResolveGeneratedProjectUnitShotAssetBindings failed: %v", err)
			}
			if len(resolved.Rows) != 3 {
				t.Fatalf("expected 3 resolved rows, got %d", len(resolved.Rows))
			}
			// Row 0: character + environment + prop
			if !slices.Equal(resolved.Rows[0].AssetVersionIDs, []string{"v-hero-1", "v-env-1", "v-prop-1"}) || len(resolved.Rows[0].Issues) != 0 {
				t.Fatalf("unexpected row 0 resolution: %+v", resolved.Rows[0])
			}
			// Row 1: bindable asset without reference image -> bound to v-no-img-1 + missing_image issue
			if !slices.Equal(resolved.Rows[1].AssetVersionIDs, []string{"v-no-img-1"}) || len(resolved.Rows[1].Issues) != 1 || resolved.Rows[1].Issues[0].Kind != "missing_image" {
				t.Fatalf("unexpected row 1 resolution (want v-no-img-1 + missing_image): %+v", resolved.Rows[1])
			}
			// Row 2: name fallback "黑衣卫" hits char-dup-p1 (Page 1) and char-dup-p2 (Page 2) -> ambiguous issue, not auto-bound
			if len(resolved.Rows[2].AssetVersionIDs) != 0 || len(resolved.Rows[2].Issues) != 1 || resolved.Rows[2].Issues[0].Kind != "ambiguous" || len(resolved.Rows[2].Candidates) != 1 {
				t.Fatalf("unexpected row 2 resolution (want ambiguous across page 1 & page 2 duplicates): %+v", resolved.Rows[2])
			}

			// Also verify full-catalog ambiguity invariance under narrowed `selected` scope:
			// Even when scope only selects char-dup-p1, name reference "黑衣卫" must still detect char-dup-p2 across the full project catalog!
			narrowTask := model.Task{
				ID:         "task-narrow-scope",
				UserID:     "u",
				ProjectID:  "p-e2e",
				Type:       "canvas_text",
				Operation:  "storyboard",
				Status:     model.TaskStatusSucceeded,
				InputJSON:  `{"mode":"text","chapterAssetContext":{"projectId":"p-e2e","unitId":"unit-e2e","operation":"storyboard","scope":{"kind":"selected","assetIds":["char-dup-p1"]},"allowedAssetIds":["char-dup-p1"]}}`,
				ResultJSON: `{"rows":[{"plotDescription":"黑衣卫从暗影现身","projectAssetRefs":[{"category":"character","name":"黑衣卫"}]}]}`,
			}
			if err := db.Create(&narrowTask).Error; err != nil {
				t.Fatal(err)
			}
			narrowResolved, err := svc.ResolveGeneratedProjectUnitShotAssetBindings(ctx, "u", "p-e2e", "unit-e2e", GeneratedBindingRequest{TaskID: narrowTask.ID})
			if err != nil {
				t.Fatalf("narrow scope resolve failed: %v", err)
			}
			if len(narrowResolved.Rows[0].AssetVersionIDs) != 0 || len(narrowResolved.Rows[0].Issues) != 1 || narrowResolved.Rows[0].Issues[0].Kind != "ambiguous" {
				t.Fatalf("narrowed selected scope must not bypass full-catalog duplicate name ambiguity detection: %+v", narrowResolved.Rows[0])
			}

			// 5. ReplaceProjectUnitShots with GeneratedBinding
			shot0Versions := []string{"v-hero-1", "v-env-1", "v-prop-1"}
			shot1Versions := []string{"v-no-img-1"}
			shot2Versions := []string{}
			replaceReq := ReplaceProjectUnitShotsRequest{
				ExpectedShotIDs: []string{"shot-init-1"},
				GeneratedBinding: &GeneratedBindingInput{
					TaskID:     completedTask.ID,
					Baseline:   resolved.Baseline,
					RowIndices: []int{0, 1, 2},
				},
				Shots: []ReplaceProjectUnitShotInput{
					{
						CreateProjectShotRequest: generatedStoryboardScript(resolved.Rows[0], shot0Versions, 0),
						AssetVersionIDs:          shot0Versions,
					},
					{
						CreateProjectShotRequest: generatedStoryboardScript(resolved.Rows[1], shot1Versions, 1),
						AssetVersionIDs:          shot1Versions,
					},
					{
						CreateProjectShotRequest: generatedStoryboardScript(resolved.Rows[2], shot2Versions, 2),
						AssetVersionIDs:          shot2Versions,
					},
				},
			}
			savedShots, err := svc.ReplaceProjectUnitShots("u", "p-e2e", "unit-e2e", replaceReq)
			if err != nil {
				t.Fatalf("ReplaceProjectUnitShots failed: %v", err)
			}
			if len(savedShots) != 3 {
				t.Fatalf("expected 3 saved shots, got %d", len(savedShots))
			}

			// Verify persisted ShotAssetReference rows in DB across all 3 categories
			var shot0Refs, shot1Refs, shot2Refs []model.ShotAssetReference
			if err := db.Where("shot_id = ?", savedShots[0].ID).Order("id").Find(&shot0Refs).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Where("shot_id = ?", savedShots[1].ID).Order("id").Find(&shot1Refs).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Where("shot_id = ?", savedShots[2].ID).Order("id").Find(&shot2Refs).Error; err != nil {
				t.Fatal(err)
			}
			gotShot0Vers := []string{shot0Refs[0].AssetVersionID, shot0Refs[1].AssetVersionID, shot0Refs[2].AssetVersionID}
			slices.Sort(gotShot0Vers)
			wantShot0Vers := []string{"v-env-1", "v-hero-1", "v-prop-1"}
			if !slices.Equal(gotShot0Vers, wantShot0Vers) {
				t.Fatalf("shot 0 persisted references mismatch: got %v, want %v", gotShot0Vers, wantShot0Vers)
			}
			if len(shot1Refs) != 1 || shot1Refs[0].AssetVersionID != "v-no-img-1" {
				t.Fatalf("shot 1 persisted references mismatch: %+v", shot1Refs)
			}
			if len(shot2Refs) != 0 {
				t.Fatalf("shot 2 ambiguous reference must not be auto-persisted: %+v", shot2Refs)
			}

			// 6. Downstream Image/Video Reference Image Verification:
			// Switch char-hero's PrimaryVersionID to a newly created version v-hero-2 (with res-hero-v2) AFTER shot binding.
			// ProjectUnitWorkspace and downstream image/video generation MUST still use the bound historical version v-hero-1 (res-hero-v1)
			// and MUST NOT use v-hero-2 (res-hero-v2); and Shot 1 (v-no-img-1) MUST NOT fall back to v-no-img-other (res-wrong-fallback).
			if err := db.Create(&model.AssetVersion{ID: "v-hero-2", AssetID: "char-hero", Version: 2, Status: "draft", DefinitionJSON: `{"summary":"阿青后期白衣版"}`}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.AssetRepresentation{ID: "rep-hero-2", AssetVersionID: "v-hero-2", ResourceID: "res-hero-v2", Role: "primary", MediaType: "image", MetadataJSON: `{}`}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Asset{}).Where("id = ?", "char-hero").Update("primary_version_id", "v-hero-2").Error; err != nil {
				t.Fatal(err)
			}

			workspace, err := svc.ProjectUnitWorkspace("u", "p-e2e", "unit-e2e")
			if err != nil {
				t.Fatalf("ProjectUnitWorkspace failed: %v", err)
			}

			var shot0RefImages []map[string]any
			var shot1RefImages []map[string]any
			for _, ref := range workspace.ShotReferences {
				switch ref.ShotID {
				case savedShots[0].ID:
					if ref.Asset.ID == "char-hero" {
						if ref.Asset.PrimaryVersionID != "v-hero-2" {
							t.Fatalf("expected asset head primaryVersionId to be v-hero-2, got %s", ref.Asset.PrimaryVersionID)
						}
						if ref.AssetVersionID != "v-hero-1" || ref.ReferencedVersion.ID != "v-hero-1" || len(ref.ReferencedVersion.Representations) != 1 || ref.ReferencedVersion.Representations[0].ResourceID != "res-hero-v1" {
							t.Fatalf("expected Shot 0 char-hero reference to preserve bound historical version v-hero-1/res-hero-v1, got %+v", ref)
						}
					}
					if len(ref.ReferencedVersion.Representations) > 0 {
						shot0RefImages = append(shot0RefImages, map[string]any{
							"id":         ref.ID,
							"storageKey": "resource:" + ref.ReferencedVersion.Representations[0].ResourceID,
							"label":      ref.Asset.Title,
							"role":       "reference",
						})
					}
				case savedShots[1].ID:
					if ref.AssetVersionID != "v-no-img-1" || ref.ReferencedVersion.ID != "v-no-img-1" {
						t.Fatalf("unexpected Shot 1 reference version: %+v", ref)
					}
					if ref.ReferencedVersion.ReferenceImage != nil || len(ref.ReferencedVersion.Representations) != 0 {
						t.Fatalf("expected missing_image bound version v-no-img-1 to have nil ReferenceImage and 0 representations, got %+v", ref.ReferencedVersion)
					}
					if len(ref.ReferencedVersion.Representations) > 0 {
						shot1RefImages = append(shot1RefImages, map[string]any{
							"id":         ref.ID,
							"storageKey": "resource:" + ref.ReferencedVersion.Representations[0].ResourceID,
						})
					}
				}
			}
			if len(shot0RefImages) != 3 {
				t.Fatalf("expected 3 reference images for Shot 0, got %d", len(shot0RefImages))
			}
			if len(shot1RefImages) != 0 {
				t.Fatalf("expected 0 reference images for Shot 1 (missing_image), got %d", len(shot1RefImages))
			}

			// Execute downstream Shot 0 Image Generation task
			imgTask, err := svc.CreateTask("u", CreateTaskRequest{
				ProjectID: "p-e2e",
				Type:      "canvas_image",
				Operation: "generate",
				Prompt:    savedShots[0].Description,
				Input: map[string]any{
					"mode": "image",
					"config": map[string]any{
						"baseUrl":       upstream.URL,
						"apiKey":        "e2e-img-key",
						"model":         "seedream-e2e",
						"interfaceType": string(model.ChannelInterfaceVolcengineArkImage),
					},
					"referenceImages": shot0RefImages,
				},
			})
			if err != nil {
				t.Fatalf("CreateTask downstream image failed: %v", err)
			}
			executeCanvasTaskAndPersistResult(t, svc, db, imgTask.ID)

			lastImgReq := captured.lastImageRequest()
			rawSentImages, ok := lastImgReq["image"].([]any)
			if !ok || len(rawSentImages) != 3 {
				t.Fatalf("expected upstream image request to contain 3 hydrated reference images, got %+v", lastImgReq["image"])
			}
			sentImgStrs := []string{rawSentImages[0].(string), rawSentImages[1].(string), rawSentImages[2].(string)}
			for _, wantURL := range []string{heroV1DataURL, envV1DataURL, propV1DataURL} {
				if !slices.Contains(sentImgStrs, wantURL) {
					t.Fatalf("upstream image request missing expected bound version dataURL")
				}
			}
			if slices.Contains(sentImgStrs, heroV2DataURL) || slices.Contains(sentImgStrs, wrongFallbackDataURL) {
				t.Fatalf("upstream image request leaked head version or fallback image!")
			}

			// Execute downstream Shot 0 Video Generation task
			vidTask, err := svc.CreateTask("u", CreateTaskRequest{
				ProjectID: "p-e2e",
				Type:      "canvas_video",
				Operation: "generate",
				Prompt:    savedShots[0].Description,
				Input: map[string]any{
					"mode": "video",
					"config": map[string]any{
						"baseUrl": upstream.URL,
						"apiKey":  "e2e-vid-key",
						"model":   "grok-video-test",
					},
					"referenceImages": shot0RefImages,
				},
			})
			if err != nil {
				t.Fatalf("CreateTask downstream video failed: %v", err)
			}
			executeCanvasTaskAndPersistResult(t, svc, db, vidTask.ID)

			lastVidReq := captured.lastVideoRequest()
			rawVidImages, ok := lastVidReq["images"].([]any)
			if !ok || len(rawVidImages) != 3 {
				t.Fatalf("expected upstream video request to contain 3 hydrated reference images, got %+v", lastVidReq["images"])
			}
			sentVidStrs := []string{rawVidImages[0].(string), rawVidImages[1].(string), rawVidImages[2].(string)}
			for _, wantURL := range []string{heroV1DataURL, envV1DataURL, propV1DataURL} {
				if !slices.Contains(sentVidStrs, wantURL) {
					t.Fatalf("upstream video request missing expected bound version dataURL")
				}
			}
			if slices.Contains(sentVidStrs, heroV2DataURL) || slices.Contains(sentVidStrs, wrongFallbackDataURL) {
				t.Fatalf("upstream video request leaked head version or fallback image!")
			}

			// Execute downstream Shot 1 (missing_image) Image Generation task -> must send zero reference images
			shot1ImgTask, err := svc.CreateTask("u", CreateTaskRequest{
				ProjectID: "p-e2e",
				Type:      "canvas_image",
				Operation: "generate",
				Prompt:    savedShots[1].Description,
				Input: map[string]any{
					"mode": "image",
					"config": map[string]any{
						"baseUrl":       upstream.URL,
						"apiKey":        "e2e-img-key",
						"model":         "seedream-e2e",
						"interfaceType": string(model.ChannelInterfaceVolcengineArkImage),
					},
					"referenceImages": shot1RefImages,
				},
			})
			if err != nil {
				t.Fatalf("CreateTask Shot 1 image failed: %v", err)
			}
			executeCanvasTaskAndPersistResult(t, svc, db, shot1ImgTask.ID)
			shot1ImgReq := captured.lastImageRequest()
			if _, hasImage := shot1ImgReq["image"]; hasImage {
				t.Fatalf("expected missing_image Shot 1 request to send no reference image, got %+v", shot1ImgReq["image"])
			}

			// 7. Negative & Boundary Injections (Zero Unauthorized DB Writes) + Shared Canvas Regression
			expectedShotIDsAfterSave := []string{savedShots[0].ID, savedShots[1].ID, savedShots[2].ID}

			// 7a. Prompt budget overflow (> 24 KiB in project asset summary, respecting PostgreSQL varchar(240) per title)
			oversizedName := strings.Repeat("甲", 200) // 200 runes <= varchar(240), 600 UTF-8 bytes * 75 assets = 45,000 bytes > 24 KiB
			if err := db.Model(&model.Asset{}).Where("id LIKE ?", "fill-asset-%").Update("title", oversizedName).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CreateTask("u", CreateTaskRequest{
				ProjectID: "p-e2e",
				Type:      "canvas_text",
				Operation: "storyboard",
				Prompt:    "预算超限测试",
				Input: map[string]any{
					"mode":        "text",
					"textOptions": map[string]any{"stream": false},
					"chapterAssetContext": map[string]any{
						"projectId": "p-e2e",
						"unitId":    "unit-e2e",
						"scope":     map[string]any{"kind": "all", "assetIds": []string{}},
					},
					"config": map[string]any{
						"baseUrl":       upstream.URL,
						"apiKey":        "e2e-test-key",
						"model":         "gpt-4o-mini",
						"interfaceType": "chat-completion",
						"capabilityConfig": map[string]any{
							"text": map[string]any{"contextWindowTokens": 128000, "maxOutputTokens": 16384},
						},
					},
				},
			}); err == nil {
				t.Fatal("expected prompt budget overflow (>24KiB) to be rejected by CreateTask")
			}
			for i := 1; i <= 75; i++ {
				if err := db.Model(&model.Asset{}).Where("id = ?", fmt.Sprintf("fill-asset-%02d", i)).Update("title", fmt.Sprintf("配角资产%02d", i)).Error; err != nil {
					t.Fatal(err)
				}
			}

			// 7b. invalid_id, missing_version, and capacity_exceeded (> 6 versions) in resolution + save guard
			negativeTask := model.Task{
				ID:        "task-negative-cases",
				UserID:    "u",
				ProjectID: "p-e2e",
				Type:      "canvas_text",
				Operation: "storyboard",
				Status:    model.TaskStatusSucceeded,
				InputJSON: `{"mode":"text","chapterAssetContext":{"projectId":"p-e2e","unitId":"unit-e2e","operation":"storyboard","allowedAssetIds":["char-hero","env-hall","prop-sword","prop-no-ver","fill-asset-01","fill-asset-02","fill-asset-03","fill-asset-04"]}}`,
				ResultJSON: `{"rows":[` +
					`{"plotDescription":"伪造资产ID","projectAssetRefs":[{"assetId":"forged-not-allowed","category":"character"}]},` +
					`{"plotDescription":"断剑残片无版本","projectAssetRefs":[{"assetId":"prop-no-ver","category":"prop"}]},` +
					`{"plotDescription":"七个资产超容量","projectAssetRefs":[{"assetId":"char-hero","category":"character"},{"assetId":"env-hall","category":"environment"},{"assetId":"prop-sword","category":"prop"},{"assetId":"fill-asset-01","category":"character"},{"assetId":"fill-asset-02","category":"environment"},{"assetId":"fill-asset-03","category":"prop"},{"assetId":"fill-asset-04","category":"character"}]}` +
					`]}`,
			}
			if err := db.Create(&negativeTask).Error; err != nil {
				t.Fatal(err)
			}
			negResolved, err := svc.ResolveGeneratedProjectUnitShotAssetBindings(ctx, "u", "p-e2e", "unit-e2e", GeneratedBindingRequest{TaskID: negativeTask.ID})
			if err != nil {
				t.Fatalf("negResolved failed: %v", err)
			}
			if len(negResolved.Rows[0].Issues) != 1 || negResolved.Rows[0].Issues[0].Kind != "invalid_id" || len(negResolved.Rows[0].AssetVersionIDs) != 0 {
				t.Fatalf("expected invalid_id issue on row 0, got %+v", negResolved.Rows[0])
			}
			hasMissingVersion := false
			for _, iss := range negResolved.Rows[1].Issues {
				if iss.Kind == "missing_version" {
					hasMissingVersion = true
				}
			}
			if !hasMissingVersion || len(negResolved.Rows[1].AssetVersionIDs) != 0 {
				t.Fatalf("expected missing_version issue on row 1, got %+v", negResolved.Rows[1])
			}
			if len(negResolved.Rows[2].AssetVersionIDs) != 7 {
				t.Fatalf("expected 7 resolved versions on row 2 before user narrowing, got %+v", negResolved.Rows[2])
			}

			stateBeforeNegSaves := previewState(t, db)

			// Attempting to save > 6 versions on a shot must fail with zero DB writes
			overCapReq := ReplaceProjectUnitShotsRequest{
				ExpectedShotIDs:  expectedShotIDsAfterSave,
				GeneratedBinding: &GeneratedBindingInput{TaskID: negativeTask.ID, Baseline: negResolved.Baseline, RowIndices: []int{0, 1, 2}},
				Shots: []ReplaceProjectUnitShotInput{
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S1", Description: "d1", DurationMs: 3000}},
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S2", Description: "d2", DurationMs: 3000}},
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S3", Description: "d3", DurationMs: 3000}, AssetVersionIDs: negResolved.Rows[2].AssetVersionIDs},
				},
			}
			if _, err := svc.ReplaceProjectUnitShots("u", "p-e2e", "unit-e2e", overCapReq); err == nil {
				t.Fatal("expected >6 asset versions on a single shot to be rejected")
			}
			if previewState(t, db) != stateBeforeNegSaves {
				t.Fatal("failed capacity_exceeded save mutated DB state")
			}

			// 7c. Cross-unit and cross-project task misappropriation must be rejected with zero DB writes
			if _, err := svc.ResolveGeneratedProjectUnitShotAssetBindings(ctx, "u", "p-e2e", "unit-other", GeneratedBindingRequest{TaskID: negativeTask.ID}); err == nil {
				t.Fatal("expected cross-unit resolve to fail")
			}
			if _, err := svc.ResolveGeneratedProjectUnitShotAssetBindings(ctx, "u", "p-other", "unit-other-p", GeneratedBindingRequest{TaskID: negativeTask.ID}); err == nil {
				t.Fatal("expected cross-project resolve to fail")
			}
			if _, err := svc.ReplaceProjectUnitShots("u", "p-e2e", "unit-other", ReplaceProjectUnitShotsRequest{
				ExpectedShotIDs:  []string{},
				GeneratedBinding: &GeneratedBindingInput{TaskID: negativeTask.ID, Baseline: negResolved.Baseline, RowIndices: []int{0, 1, 2}},
				Shots: []ReplaceProjectUnitShotInput{
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S1", Description: "d1", DurationMs: 3000}},
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S2", Description: "d2", DurationMs: 3000}},
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S3", Description: "d3", DurationMs: 3000}},
				},
			}); err == nil {
				t.Fatal("expected cross-unit ReplaceProjectUnitShots to fail")
			}
			if previewState(t, db) != stateBeforeNegSaves {
				t.Fatal("cross-unit/cross-project rejection mutated DB state")
			}

			// 7d. Post-resolve version switch / catalog drift before save must be rejected with zero DB writes
			if err := db.Model(&model.Asset{}).Where("id = ?", "char-hero").Update("primary_version_id", "v-hero-1").Error; err != nil {
				t.Fatal(err)
			}
			stateAfterDriftSetup := previewState(t, db)
			driftReq := ReplaceProjectUnitShotsRequest{
				ExpectedShotIDs:  expectedShotIDsAfterSave,
				GeneratedBinding: &GeneratedBindingInput{TaskID: negativeTask.ID, Baseline: negResolved.Baseline, RowIndices: []int{0, 1, 2}},
				Shots: []ReplaceProjectUnitShotInput{
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S1", Description: "d1", DurationMs: 3000}},
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S2", Description: "d2", DurationMs: 3000}},
					{CreateProjectShotRequest: CreateProjectShotRequest{Title: "S3", Description: "d3", DurationMs: 3000}, AssetVersionIDs: negResolved.Rows[2].AssetVersionIDs[:6]},
				},
			}
			if _, err := svc.ReplaceProjectUnitShots("u", "p-e2e", "unit-e2e", driftReq); err == nil {
				t.Fatal("expected post-resolve primary_version_id switch to reject stale baseline")
			}
			if previewState(t, db) != stateAfterDriftSetup {
				t.Fatal("post-resolve drift rejection mutated DB state")
			}

			// 7e. Shared Canvas Storyboard Regression (no chapterAssetContext)
			sharedReply := `{"rows":[{"shotNumber":"SC.01","plotDescription":"共享画布普通分镜","durationSeconds":3}]}`
			captured.setTextResponse(sharedReply)
			sharedTask, err := svc.CreateTask("u", CreateTaskRequest{
				ProjectID: "p-e2e",
				Type:      "canvas_text",
				Operation: "storyboard",
				Prompt:    "普通画布分镜提示词",
				Input: map[string]any{
					"mode":        "text",
					"textOptions": map[string]any{"stream": false},
					"config": map[string]any{
						"baseUrl":       upstream.URL,
						"apiKey":        "e2e-test-key",
						"model":         "gpt-4o-mini",
						"interfaceType": "chat-completion",
						"capabilityConfig": map[string]any{
							"text": map[string]any{"contextWindowTokens": 128000, "maxOutputTokens": 16384},
						},
					},
				},
			})
			if err != nil {
				t.Fatalf("shared canvas CreateTask failed: %v", err)
			}
			savedSharedBefore, err := svc.repo.Task(sharedTask.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(savedSharedBefore.InputJSON, "chapterAssetContext") {
				t.Fatalf("shared canvas task without chapterAssetContext must not inject chapterAssetContext: %s", savedSharedBefore.InputJSON)
			}
			doneShared := executeCanvasTaskAndPersistResult(t, svc, db, sharedTask.ID)
			if !strings.Contains(doneShared.ResultJSON, "共享画布普通分镜") {
				t.Fatalf("unexpected shared canvas task result: %s", doneShared.ResultJSON)
			}
		})
	}
}

type legacyChapterFullSnapshot struct {
	ProjectRevision int64
	Shots           []model.Shot
	Revisions       []model.ShotRevision
	References      []model.ShotAssetReference
	Artifacts       []model.ShotArtifact
	TaskCount       int64
	OutboxIntents   int64
	OutboxJobs      int64
	OutboxObjects   int64
	EncodedFullDB   string
}

func captureLegacyChapterFullSnapshot(t *testing.T, db *gorm.DB, projectID, unitID string) legacyChapterFullSnapshot {
	t.Helper()
	var proj model.Project
	if err := db.Where("id = ?", projectID).First(&proj).Error; err != nil {
		t.Fatal(err)
	}
	var shots []model.Shot
	if err := db.Where("project_id = ? AND unit_id = ?", projectID, unitID).Order("id").Find(&shots).Error; err != nil {
		t.Fatal(err)
	}
	shotIDs := make([]string, 0, len(shots))
	for _, s := range shots {
		shotIDs = append(shotIDs, s.ID)
	}
	var revisions []model.ShotRevision
	if err := db.Where("shot_id IN ?", shotIDs).Order("id").Find(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	var references []model.ShotAssetReference
	if err := db.Where("shot_id IN ?", shotIDs).Order("id").Find(&references).Error; err != nil {
		t.Fatal(err)
	}
	var artifacts []model.ShotArtifact
	if err := db.Where("shot_id IN ?", shotIDs).Order("id").Find(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	var taskCount, intentCount, jobCount, objCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceDeletionIntent{}).Count(&intentCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceDeletionJob{}).Count(&jobCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceDeletionObject{}).Count(&objCount).Error; err != nil {
		t.Fatal(err)
	}
	return legacyChapterFullSnapshot{
		ProjectRevision: proj.Revision,
		Shots:           shots,
		Revisions:       revisions,
		References:      references,
		Artifacts:       artifacts,
		TaskCount:       taskCount,
		OutboxIntents:   intentCount,
		OutboxJobs:      jobCount,
		OutboxObjects:   objCount,
		EncodedFullDB:   appendFullState(t, db),
	}
}

func writeE2EBinEnvelope(w http.ResponseWriter, svcErr error, data any) {
	w.Header().Set("Content-Type", "application/json")
	if svcErr == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   0,
			"data":   data,
			"msg":    "ok",
			"reason": "",
		})
		return
	}
	if IsProjectNotFound(svcErr) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   CodeNotFound,
			"data":   nil,
			"msg":    svcErr.Error(),
			"reason": ReasonNotFound,
		})
		return
	}
	var appErr *AppError
	if strings.Contains(fmt.Sprintf("%T", svcErr), "AppError") {
		appErr = svcErr.(*AppError)
	}
	if appErr != nil {
		status := appErr.Status
		if status < 400 || status > 599 {
			status = http.StatusBadRequest
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   appErr.Code,
			"data":   nil,
			"msg":    appErr.Message,
			"reason": appErr.Reason,
		})
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":   500,
		"data":   nil,
		"msg":    svcErr.Error(),
		"reason": "internal",
	})
}

func newLegacyBindingHTTPHandler(svc *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := strings.TrimSpace(r.Header.Get("X-Test-User"))
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
		if r.Method != http.MethodPost || len(parts) != 5 || parts[1] != "units" || parts[3] != "shot-asset-bindings" {
			http.NotFound(w, r)
			return
		}
		projectID := parts[0]
		unitID := parts[2]
		action := parts[4]
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeE2EBinEnvelope(w, StoryboardAssetBindingInvalidRequest("读取请求失败"), nil)
			return
		}
		switch action {
		case "preview":
			var req BindingPreviewRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				writeE2EBinEnvelope(w, BadAuthRequest("请求格式错误"), nil)
				return
			}
			res, svcErr := svc.PreviewProjectUnitShotAssetBindings(r.Context(), userID, projectID, unitID, req)
			writeE2EBinEnvelope(w, svcErr, res)
		case "append":
			var req BindingAppendRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				writeE2EBinEnvelope(w, StoryboardAssetBindingInvalidRequest("绑定请求格式不合法"), nil)
				return
			}
			res, svcErr := svc.AppendProjectUnitShotAssetBindings(r.Context(), userID, projectID, unitID, req)
			writeE2EBinEnvelope(w, svcErr, res)
		default:
			http.NotFound(w, r)
		}
	})
}

func TestStoryboardAssetE2ELegacyMultiShotFullChain(t *testing.T) {
	dialects := []string{"sqlite"}
	if strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN")) != "" {
		dialects = append(dialects, "postgres")
	}

	for _, dialect := range dialects {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			svc, db, dataDir := openE2EDialectService(t, dialect)
			_, captured := startFakeE2EUpstreamServer(t)
			handler := newLegacyBindingHTTPHandler(svc)

			// Seed isolated legacy chapter fixture with 3 shots:
			// - Shot 1 (shot-leg-1): Actual New Addition (D != empty).
			//   Has existing manual references with the same assetVersionId ("v-sangruo-1") bound across multiple roles ("character" and "reference"),
			//   counting as 1 distinct version (existingVersionCount=1, remainingCapacity=5).
			//   Has historical ShotArtifact rows: succeeded/selected=true, approved/selected=false, failed/selected=true.
			//   Has explicit visual label evidence matching new bindable versions "v-peirang-1" (character) and "v-qiyunge-1" (environment).
			// - Shot 2 (shot-leg-2): All-Existing References (D = empty).
			//   Matches only asset versions that are ALREADY bound on Shot 2 ("v-peirang-1" and "v-qiyunge-1").
			//   Has historical ShotArtifact rows: succeeded/selected=true, approved/selected=false.
			// - Shot 3 (shot-leg-3): Narrative/Dialogue Mention Only (Pending Manual Selection).
			//   Contains only ordinary narrative/dialogue mentions ("桑若视线落在领口，裴让停止喝粥", "沈知意：天亮了") -> defaultSelected=false.
			//   Has historical ShotArtifact row: succeeded/selected=true.
			writeLocalImageResource(t, db, dataDir, "u", "res-leg-sangruo", makeDistinctPNGBytes(t, 11, 22, 33))
			writeLocalImageResource(t, db, dataDir, "u", "res-leg-peirang", makeDistinctPNGBytes(t, 44, 55, 66))
			writeLocalImageResource(t, db, dataDir, "u", "res-leg-qiyunge", makeDistinctPNGBytes(t, 77, 88, 99))
			writeLocalImageResource(t, db, dataDir, "u", "res-leg-shen", makeDistinctPNGBytes(t, 111, 122, 133))
			writeLocalImageResource(t, db, dataDir, "foreign-u", "res-leg-foreign", makeDistinctPNGBytes(t, 200, 10, 20))

			seedRows := []any{
				&model.User{ID: "u", Username: "legacy-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.User{ID: "foreign-u", Username: "foreign-user", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.Project{ID: "p-leg", UserID: "u", Name: "旧章多镜验收项目", Revision: 11},
				&model.Project{ID: "p-other", UserID: "u", Name: "同账号其他项目", Revision: 3},
				&model.Project{ID: "p-foreign", UserID: "foreign-u", Name: "PRIVATE_FOREIGN_PROJECT", Revision: 1},
				&model.ProjectUnit{ID: "unit-leg", ProjectID: "p-leg", Title: "第三章 栖云旧事", SourceText: "裴让步入栖云阁，桑若静候，沈知意轻声开口。"},
				&model.ProjectUnit{ID: "unit-other", ProjectID: "p-other", Title: "异项目章节"},
				&model.ProjectUnit{ID: "unit-foreign", ProjectID: "p-foreign", Title: "外部章节"},

				// 3 Legacy Shots + Revisions
				&model.Shot{ID: "shot-leg-1", ProjectID: "p-leg", UnitID: "unit-leg", CurrentRevisionID: "rev-leg-1", Title: "镜头一", Description: "角色：裴让\n场景：栖云阁", DurationMs: 4000, Status: "ready", Position: 1},
				&model.Shot{ID: "shot-leg-2", ProjectID: "p-leg", UnitID: "unit-leg", CurrentRevisionID: "rev-leg-2", Title: "镜头二", Description: "角色：裴让\n场景：栖云阁", DurationMs: 5000, Status: "ready", Position: 2},
				&model.Shot{ID: "shot-leg-3", ProjectID: "p-leg", UnitID: "unit-leg", CurrentRevisionID: "rev-leg-3", Title: "镜头三", Description: "清晨窗台微光", DurationMs: 3500, Status: "ready", Position: 3},
				&model.Shot{ID: "shot-cross-proj", ProjectID: "p-other", UnitID: "unit-other", CurrentRevisionID: "rev-cross-proj", Title: "跨项目镜头", Description: "异项目脚本", DurationMs: 3000, Status: "draft", Position: 1},
				&model.Shot{ID: "shot-foreign", ProjectID: "p-foreign", UnitID: "unit-foreign", CurrentRevisionID: "rev-foreign", Title: "PRIVATE_FOREIGN_SHOT", Description: "外账号脚本", DurationMs: 3000, Status: "draft", Position: 1},

				&model.ShotRevision{ID: "rev-leg-1", ShotID: "shot-leg-1", Version: 1, PlotDescription: "角色：裴让\n场景：栖云阁", Dialogue: "桑若：你来了", ImagePrompt: "裴让在栖云阁", VideoPrompt: "缓步走入", ActionBeatsJSON: `[{"beat":"enter"}]`, DurationMs: 4000},
				&model.ShotRevision{ID: "rev-leg-2", ShotID: "shot-leg-2", Version: 1, PlotDescription: "角色：裴让\n场景：栖云阁", Dialogue: "", ImagePrompt: "栖云阁内裴让驻足", VideoPrompt: "定格", ActionBeatsJSON: `[{"beat":"stand"}]`, DurationMs: 5000},
				&model.ShotRevision{ID: "rev-leg-3", ShotID: "shot-leg-3", Version: 1, PlotDescription: "桑若视线落在领口，裴让停止喝粥", Dialogue: "沈知意：天亮了", ImagePrompt: "晨光微露", VideoPrompt: "静默", ActionBeatsJSON: `[{"beat":"pause"}]`, DurationMs: 3500},
				&model.ShotRevision{ID: "rev-cross-proj", ShotID: "shot-cross-proj", Version: 1, PlotDescription: "异项目脚本", ActionBeatsJSON: `[]`, DurationMs: 3000},
				&model.ShotRevision{ID: "rev-foreign", ShotID: "shot-foreign", Version: 1, PlotDescription: "外账号脚本", ActionBeatsJSON: `[]`, DurationMs: 3000},

				// Catalog Assets + Versions + Representations
				&model.Asset{ID: "char-sangruo", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "桑若", Status: "draft", PrimaryVersionID: "v-sangruo-1", PayloadJSON: `{}`},
				&model.Asset{ID: "char-peirang", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "裴让", Status: "draft", PrimaryVersionID: "v-peirang-1", PayloadJSON: `{}`},
				&model.Asset{ID: "env-qiyunge", UserID: "u", Category: model.AssetCategoryEnvironment, Kind: "image", Title: "栖云阁", Status: "draft", PrimaryVersionID: "v-qiyunge-1", PayloadJSON: `{}`},
				&model.Asset{ID: "char-shen", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "沈知意", Status: "draft", PrimaryVersionID: "v-shen-1", PayloadJSON: `{}`},
				&model.Asset{ID: "char-unlinked", UserID: "u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "未关联角色", Status: "draft", PrimaryVersionID: "v-unlinked-1", PayloadJSON: `{}`},
				&model.Asset{ID: "char-foreign", UserID: "foreign-u", Category: model.AssetCategoryCharacter, Kind: "entity", Title: "PRIVATE_FOREIGN_TITLE", Status: "draft", PrimaryVersionID: "v-foreign-1", PayloadJSON: `{}`},

				&model.ProjectAssetLink{ID: "link-leg-sangruo", ProjectID: "p-leg", AssetID: "char-sangruo"},
				&model.ProjectAssetLink{ID: "link-leg-peirang", ProjectID: "p-leg", AssetID: "char-peirang"},
				&model.ProjectAssetLink{ID: "link-leg-qiyunge", ProjectID: "p-leg", AssetID: "env-qiyunge"},
				&model.ProjectAssetLink{ID: "link-leg-shen", ProjectID: "p-leg", AssetID: "char-shen"},

				&model.AssetVersion{ID: "v-sangruo-1", AssetID: "char-sangruo", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-leg-sangruo"}}`},
				&model.AssetVersion{ID: "v-peirang-1", AssetID: "char-peirang", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-leg-peirang"}}`},
				&model.AssetVersion{ID: "v-qiyunge-1", AssetID: "env-qiyunge", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-leg-qiyunge"}}`},
				&model.AssetVersion{ID: "v-shen-1", AssetID: "char-shen", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-leg-shen"}}`},
				&model.AssetVersion{ID: "v-unlinked-1", AssetID: "char-unlinked", Version: 1, Status: "draft", DefinitionJSON: `{}`},
				&model.AssetVersion{ID: "v-foreign-1", AssetID: "char-foreign", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-leg-foreign"}}`},

				// Existing ShotAssetReference rows:
				// Shot 1 has same assetVersionId "v-sangruo-1" bound across 2 roles ("character" and "reference")
				&model.ShotAssetReference{ID: "ref-s1-char", ShotID: "shot-leg-1", AssetVersionID: "v-sangruo-1", Role: "character", Status: "confirmed"},
				&model.ShotAssetReference{ID: "ref-s1-ref", ShotID: "shot-leg-1", AssetVersionID: "v-sangruo-1", Role: "reference", Status: "stale"},
				// Shot 2 already has all matched versions ("v-peirang-1" and "v-qiyunge-1")
				&model.ShotAssetReference{ID: "ref-s2-char", ShotID: "shot-leg-2", AssetVersionID: "v-peirang-1", Role: "character", Status: "confirmed"},
				&model.ShotAssetReference{ID: "ref-s2-env", ShotID: "shot-leg-2", AssetVersionID: "v-qiyunge-1", Role: "environment", Status: "linked"},

				// Historical ShotArtifact rows across all 3 shots:
				&model.ShotArtifact{ID: "art-s1-succeeded", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "shot-leg-1", RevisionID: "rev-leg-1", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"frame":"s1-ok"}`},
				&model.ShotArtifact{ID: "art-s1-approved", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "shot-leg-1", RevisionID: "rev-leg-1", Type: "image", Version: 2, Status: "approved", Selected: false, MetadataJSON: `{"frame":"s1-app"}`},
				&model.ShotArtifact{ID: "art-s1-failed", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "shot-leg-1", RevisionID: "rev-leg-1", Type: "image", Version: 3, Status: "failed", Selected: true, MetadataJSON: `{"frame":"s1-fail"}`},
				&model.ShotArtifact{ID: "art-s2-succeeded", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "shot-leg-2", RevisionID: "rev-leg-2", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"frame":"s2-ok"}`},
				&model.ShotArtifact{ID: "art-s2-approved", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "shot-leg-2", RevisionID: "rev-leg-2", Type: "image", Version: 2, Status: "approved", Selected: false, MetadataJSON: `{"frame":"s2-app"}`},
				&model.ShotArtifact{ID: "art-s3-succeeded", ProjectID: "p-leg", UnitID: "unit-leg", ShotID: "shot-leg-3", RevisionID: "rev-leg-3", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"frame":"s3-ok"}`},
			}
			for _, row := range seedRows {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}

			doHTTP := func(action string, payload any) (int, map[string]any, []byte) {
				t.Helper()
				rawReq, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				httpReq := httptest.NewRequest(http.MethodPost, "/api/projects/p-leg/units/unit-leg/shot-asset-bindings/"+action, bytes.NewReader(rawReq))
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("X-Test-User", "u")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httpReq)
				var env map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
					t.Fatalf("unmarshal %s response failed (%d %s): %v", action, rec.Code, rec.Body.String(), err)
				}
				return rec.Code, env, rec.Body.Bytes()
			}

			assertZeroPaidOrOutboxSideEffects := func(step string) {
				t.Helper()
				captured.mu.Lock()
				textCnt, imgCnt, vidCnt := len(captured.textPrompts), len(captured.imageRequests), len(captured.videoRequests)
				captured.mu.Unlock()
				if textCnt != 0 || imgCnt != 0 || vidCnt != 0 {
					t.Fatalf("%s triggered unexpected upstream model/media requests: text=%d image=%d video=%d", step, textCnt, imgCnt, vidCnt)
				}
				var taskCnt, intentCnt, jobCnt, objCnt int64
				_ = db.Model(&model.Task{}).Count(&taskCnt).Error
				_ = db.Model(&model.ResourceDeletionIntent{}).Count(&intentCnt).Error
				_ = db.Model(&model.ResourceDeletionJob{}).Count(&jobCnt).Error
				_ = db.Model(&model.ResourceDeletionObject{}).Count(&objCnt).Error
				if taskCnt != 0 || intentCnt != 0 || jobCnt != 0 || objCnt != 0 {
					t.Fatalf("%s created unexpected Task or Outbox records: tasks=%d intents=%d jobs=%d objects=%d", step, taskCnt, intentCnt, jobCnt, objCnt)
				}
			}

			// Capture full pre-operation DB snapshot
			preSnap := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			if preSnap.ProjectRevision != 11 || len(preSnap.Shots) != 3 || len(preSnap.Revisions) != 3 || len(preSnap.References) != 4 || len(preSnap.Artifacts) != 6 {
				t.Fatalf("unexpected pre-operation snapshot counts: rev=%d shots=%d revisions=%d refs=%d artifacts=%d",
					preSnap.ProjectRevision, len(preSnap.Shots), len(preSnap.Revisions), len(preSnap.References), len(preSnap.Artifacts))
			}

			// 1. POST /preview + Cancel Zero-Write Verification
			status, previewEnv, rawPreviewBody := doHTTP("preview", BindingPreviewRequest{
				ShotIDs: []string{"shot-leg-3", "shot-leg-1", "shot-leg-2", "shot-leg-1"},
			})
			if status != http.StatusOK || int(previewEnv["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on preview, got status=%d env=%+v", status, previewEnv)
			}
			var decodedPreviewEnv struct {
				Code int            `json:"code"`
				Data BindingPreview `json:"data"`
			}
			if err := json.Unmarshal(rawPreviewBody, &decodedPreviewEnv); err != nil {
				t.Fatal(err)
			}
			preview := decodedPreviewEnv.Data
			if !slices.Equal(preview.Baseline.ShotIDs, []string{"shot-leg-1", "shot-leg-2", "shot-leg-3"}) || len(preview.Shots) != 3 {
				t.Fatalf("expected deduplicated sorted 3-shot preview, got %+v", preview.Baseline.ShotIDs)
			}
			// Verify Shot 1 preview: multi-role same-version v-sangruo-1 counts as 1 distinct version -> remainingCapacity = 5;
			// explicit visual candidates v-peirang-1 and v-qiyunge-1 have defaultSelected=true.
			shot1Prev := preview.Shots[0]
			if shot1Prev.ShotID != "shot-leg-1" || len(shot1Prev.ExistingReferences) != 2 || shot1Prev.ExistingVersionCount != 1 || shot1Prev.RemainingCapacity != 5 {
				t.Fatalf("unexpected Shot 1 preview capacity/existing refs: %+v", shot1Prev)
			}
			shot1DefaultSelected := []string{}
			for _, c := range shot1Prev.Candidates {
				if c.DefaultSelected && c.AssetVersionID != nil {
					shot1DefaultSelected = append(shot1DefaultSelected, *c.AssetVersionID)
				}
			}
			slices.Sort(shot1DefaultSelected)
			shot1DefaultSelected = slices.Compact(shot1DefaultSelected)
			if !slices.Equal(shot1DefaultSelected, []string{"v-peirang-1", "v-qiyunge-1"}) {
				t.Fatalf("expected Shot 1 defaultSelected=[v-peirang-1, v-qiyunge-1], got %v (candidates=%+v)", shot1DefaultSelected, shot1Prev.Candidates)
			}

			// Verify Shot 2 preview: all matched versions already bound -> ExistingVersionCount=2, RemainingCapacity=4, and every candidate version is already in ExistingReferences
			shot2Prev := preview.Shots[1]
			if shot2Prev.ShotID != "shot-leg-2" || len(shot2Prev.ExistingReferences) != 2 || shot2Prev.ExistingVersionCount != 2 || shot2Prev.RemainingCapacity != 4 {
				t.Fatalf("unexpected Shot 2 preview (all-existing): %+v", shot2Prev)
			}
			shot2ExistingSet := map[string]bool{}
			for _, ref := range shot2Prev.ExistingReferences {
				shot2ExistingSet[ref.AssetVersionID] = true
			}
			for _, c := range shot2Prev.Candidates {
				if c.AssetVersionID == nil || !shot2ExistingSet[*c.AssetVersionID] {
					t.Fatalf("expected all Shot 2 candidates to be already bound in ExistingReferences, got candidate %+v", c)
				}
			}

			// Verify Shot 3 preview: ordinary narrative/dialogue mentions only -> defaultSelected=false for all candidates
			shot3Prev := preview.Shots[2]
			if shot3Prev.ShotID != "shot-leg-3" || len(shot3Prev.ExistingReferences) != 0 || shot3Prev.ExistingVersionCount != 0 || shot3Prev.RemainingCapacity != 6 || len(shot3Prev.Candidates) != 3 {
				t.Fatalf("unexpected Shot 3 preview: %+v", shot3Prev)
			}
			for _, c := range shot3Prev.Candidates {
				if c.DefaultSelected {
					t.Fatalf("Shot 3 narrative/dialogue candidate %s must have defaultSelected=false: %+v", c.Name, c)
				}
				if len(c.Evidence) == 0 || (c.Evidence[0].Kind != "mention" && c.Evidence[0].Kind != "speaker") {
					t.Fatalf("expected Shot 3 candidate %s evidence kind in (mention, speaker), got %+v", c.Name, c.Evidence)
				}
			}

			// User cancels after preview -> verify 100% byte-for-byte identical DB snapshot and zero tasks/outbox/upstream calls
			afterCancelSnap := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			if afterCancelSnap.EncodedFullDB != preSnap.EncodedFullDB {
				t.Fatalf("DB mutated after preview/cancel!")
			}
			assertZeroPaidOrOutboxSideEffects("preview_and_cancel")

			// 2. All-Existing Selection across all 3 shots (D = empty) -> 200 OK unchanged=true, addedCount=0, zero DB writes
			cleanupAllExistDetector := installBusinessWriteDetector(t, db)
			status, allExistEnv, rawAllExistBody := doHTTP("append", BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "shot-leg-1", AssetVersionIDs: []string{"v-sangruo-1"}},
					{ShotID: "shot-leg-2", AssetVersionIDs: []string{"v-peirang-1", "v-qiyunge-1"}},
					{ShotID: "shot-leg-3", AssetVersionIDs: []string{}},
				},
			})
			allExistWrites := cleanupAllExistDetector()
			if status != http.StatusOK || int(allExistEnv["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on all-existing append, got status=%d env=%+v", status, allExistEnv)
			}
			var decodedAllExist struct {
				Data BindingAppendResult `json:"data"`
			}
			if err := json.Unmarshal(rawAllExistBody, &decodedAllExist); err != nil {
				t.Fatal(err)
			}
			if !decodedAllExist.Data.Unchanged || decodedAllExist.Data.AddedCount != 0 || decodedAllExist.Data.ProjectRevision != 11 {
				t.Fatalf("unexpected all-existing append result: %+v", decodedAllExist.Data)
			}
			if allExistWrites != 0 {
				t.Fatalf("expected 0 business writes on all-existing append, got %d", allExistWrites)
			}
			afterAllExistSnap := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			if afterAllExistSnap.EncodedFullDB != preSnap.EncodedFullDB {
				t.Fatalf("all-existing append mutated DB state!")
			}
			assertZeroPaidOrOutboxSideEffects("all_existing_append")

			// 3. Unauthorized / Cross-Project Shot or Asset Version in Multi-Shot Batch -> Atomic Rollback & Zero Leak
			for _, badVer := range []string{"v-unlinked-1", "v-foreign-1"} {
				status, badEnv, _ := doHTTP("append", BindingAppendRequest{
					Baseline: preview.Baseline,
					Selections: []BindingSelection{
						{ShotID: "shot-leg-1", AssetVersionIDs: []string{"v-sangruo-1", "v-peirang-1"}},
						{ShotID: "shot-leg-2", AssetVersionIDs: []string{"v-peirang-1", "v-qiyunge-1"}},
						{ShotID: "shot-leg-3", AssetVersionIDs: []string{badVer}},
					},
				})
				if status != http.StatusUnprocessableEntity || int(badEnv["code"].(float64)) != CodeStoryboardAssetBindingInvalidReference || badEnv["reason"] != string(ReasonStoryboardAssetBindingInvalidReference) {
					t.Fatalf("expected 422 invalid_reference for %s, got status=%d env=%+v", badVer, status, badEnv)
				}
				for _, secret := range []string{"PRIVATE_FOREIGN_TITLE", "PRIVATE_FOREIGN_PROJECT", "PRIVATE_FOREIGN_SHOT"} {
					if strings.Contains(fmt.Sprintf("%v", badEnv), secret) {
						t.Fatalf("response leaked foreign secret %q: %+v", secret, badEnv)
					}
				}
				if captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg").EncodedFullDB != preSnap.EncodedFullDB {
					t.Fatalf("batch with unauthorized version %s partially committed!", badVer)
				}
			}

			for _, badShotID := range []string{"shot-cross-proj", "shot-foreign"} {
				bBadShot := preview.Baseline
				bBadShot.ShotIDs = []string{badShotID, "shot-leg-1", "shot-leg-2"}
				bBadShot.Shots = []BindingShotBaseline{
					{
						ShotID:              badShotID,
						ShotFingerprint:     "fp-bad-shot",
						RevisionFingerprint: "fp-bad-rev",
						References:          []BindingReference{},
						Artifacts:           []BindingArtifact{},
					},
					preview.Baseline.Shots[0],
					preview.Baseline.Shots[1],
				}
				status, badEnv, _ := doHTTP("append", BindingAppendRequest{
					Baseline: bBadShot,
					Selections: []BindingSelection{
						{ShotID: badShotID, AssetVersionIDs: []string{"v-peirang-1"}},
						{ShotID: "shot-leg-1", AssetVersionIDs: []string{"v-sangruo-1", "v-peirang-1"}},
						{ShotID: "shot-leg-2", AssetVersionIDs: []string{"v-peirang-1", "v-qiyunge-1"}},
					},
				})
				if status != http.StatusUnprocessableEntity || int(badEnv["code"].(float64)) != CodeStoryboardAssetBindingInvalidReference || badEnv["reason"] != string(ReasonStoryboardAssetBindingInvalidReference) {
					t.Fatalf("expected 422 invalid_reference for cross/foreign shot %s, got status=%d env=%+v", badShotID, status, badEnv)
				}
				for _, secret := range []string{"PRIVATE_FOREIGN_TITLE", "PRIVATE_FOREIGN_PROJECT", "PRIVATE_FOREIGN_SHOT"} {
					if strings.Contains(fmt.Sprintf("%v", badEnv), secret) {
						t.Fatalf("response leaked foreign secret %q: %+v", secret, badEnv)
					}
				}
				if captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg").EncodedFullDB != preSnap.EncodedFullDB {
					t.Fatalf("batch with cross/foreign shot %s partially committed!", badShotID)
				}
			}

			// 4. Multi-Shot Selective Append:
			// - Shot 1: selects existing "v-sangruo-1" + new ["v-peirang-1", "v-qiyunge-1"] (D = 2 versions)
			// - Shot 2: selects existing ["v-peirang-1", "v-qiyunge-1"] (D = empty)
			// - Shot 3: leaves narrative mentions unselected [] (D = empty)
			selectiveReq := BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "shot-leg-1", AssetVersionIDs: []string{"v-sangruo-1", "v-peirang-1", "v-qiyunge-1"}},
					{ShotID: "shot-leg-2", AssetVersionIDs: []string{"v-peirang-1", "v-qiyunge-1"}},
					{ShotID: "shot-leg-3", AssetVersionIDs: []string{}},
				},
			}
			status, appendEnv, rawAppendBody := doHTTP("append", selectiveReq)
			if status != http.StatusOK || int(appendEnv["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on selective multi-shot append, got status=%d env=%+v", status, appendEnv)
			}
			var decodedAppend struct {
				Data BindingAppendResult `json:"data"`
			}
			if err := json.Unmarshal(rawAppendBody, &decodedAppend); err != nil {
				t.Fatal(err)
			}
			appendRes := decodedAppend.Data
			if appendRes.Unchanged || appendRes.AddedCount != 2 || appendRes.ProjectRevision != 12 || len(appendRes.Shots) != 3 {
				t.Fatalf("unexpected selective append result: %+v", appendRes)
			}
			if appendRes.Shots[0].ShotID != "shot-leg-1" || appendRes.Shots[0].Unchanged || appendRes.Shots[0].AddedCount != 2 || !slices.Equal(appendRes.Shots[0].AddedVersionIDs, []string{"v-peirang-1", "v-qiyunge-1"}) {
				t.Fatalf("unexpected Shot 1 append result: %+v", appendRes.Shots[0])
			}
			if appendRes.Shots[1].ShotID != "shot-leg-2" || !appendRes.Shots[1].Unchanged || appendRes.Shots[1].AddedCount != 0 || len(appendRes.Shots[1].AddedVersionIDs) != 0 {
				t.Fatalf("unexpected Shot 2 append result: %+v", appendRes.Shots[1])
			}
			if appendRes.Shots[2].ShotID != "shot-leg-3" || !appendRes.Shots[2].Unchanged || appendRes.Shots[2].AddedCount != 0 || len(appendRes.Shots[2].AddedVersionIDs) != 0 {
				t.Fatalf("unexpected Shot 3 append result: %+v", appendRes.Shots[2])
			}

			postAppendSnap := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			// Project.Revision incremented exactly once (11 -> 12)
			if postAppendSnap.ProjectRevision != 12 {
				t.Fatalf("expected Project.Revision=12 after selective append, got %d", postAppendSnap.ProjectRevision)
			}
			// Shot IDs and ShotRevision scripts 100% preserved
			if len(postAppendSnap.Shots) != len(preSnap.Shots) || len(postAppendSnap.Revisions) != len(preSnap.Revisions) {
				t.Fatalf("shots or revisions count changed: before=%d/%d after=%d/%d", len(preSnap.Shots), len(preSnap.Revisions), len(postAppendSnap.Shots), len(postAppendSnap.Revisions))
			}
			for i := range preSnap.Shots {
				if postAppendSnap.Shots[i].ID != preSnap.Shots[i].ID ||
					postAppendSnap.Shots[i].CurrentRevisionID != preSnap.Shots[i].CurrentRevisionID ||
					postAppendSnap.Shots[i].Title != preSnap.Shots[i].Title ||
					postAppendSnap.Shots[i].Description != preSnap.Shots[i].Description ||
					postAppendSnap.Shots[i].DurationMs != preSnap.Shots[i].DurationMs {
					t.Fatalf("Shot[%d] mutated: before=%+v after=%+v", i, preSnap.Shots[i], postAppendSnap.Shots[i])
				}
			}
			for i := range preSnap.Revisions {
				if postAppendSnap.Revisions[i].ID != preSnap.Revisions[i].ID ||
					postAppendSnap.Revisions[i].PlotDescription != preSnap.Revisions[i].PlotDescription ||
					postAppendSnap.Revisions[i].Dialogue != preSnap.Revisions[i].Dialogue ||
					postAppendSnap.Revisions[i].ImagePrompt != preSnap.Revisions[i].ImagePrompt ||
					postAppendSnap.Revisions[i].VideoPrompt != preSnap.Revisions[i].VideoPrompt ||
					postAppendSnap.Revisions[i].ActionBeatsJSON != preSnap.Revisions[i].ActionBeatsJSON {
					t.Fatalf("ShotRevision[%d] mutated: before=%+v after=%+v", i, preSnap.Revisions[i], postAppendSnap.Revisions[i])
				}
			}

			// Verify ShotAssetReference rows: original 4 rows keep exact ID/Role/Status + 2 new rows on Shot 1 with role="reference", status="linked"
			if len(postAppendSnap.References) != 6 {
				t.Fatalf("expected 6 total references after selective append, got %d: %+v", len(postAppendSnap.References), postAppendSnap.References)
			}
			refMap := make(map[string]model.ShotAssetReference, len(postAppendSnap.References))
			newS1Versions := []string{}
			for _, ref := range postAppendSnap.References {
				refMap[ref.ID] = ref
				if ref.ID != "ref-s1-char" && ref.ID != "ref-s1-ref" && ref.ID != "ref-s2-char" && ref.ID != "ref-s2-env" {
					if ref.ShotID != "shot-leg-1" || ref.Role != "reference" || ref.Status != "linked" {
						t.Fatalf("unexpected newly appended reference tuple: %+v", ref)
					}
					newS1Versions = append(newS1Versions, ref.AssetVersionID)
				}
			}
			slices.Sort(newS1Versions)
			if !slices.Equal(newS1Versions, []string{"v-peirang-1", "v-qiyunge-1"}) {
				t.Fatalf("unexpected newly appended versions on Shot 1: %v", newS1Versions)
			}
			for _, origRef := range preSnap.References {
				got, ok := refMap[origRef.ID]
				if !ok || got.ShotID != origRef.ShotID || got.AssetVersionID != origRef.AssetVersionID || got.Role != origRef.Role || got.Status != origRef.Status {
					t.Fatalf("original reference %s mutated: before=%+v after=%+v", origRef.ID, origRef, got)
				}
			}

			// Verify selective ShotArtifact invalidation:
			// ONLY Shot 1 (addedCount > 0) has non-failed artifacts -> status="stale", selected=false and failed -> status="failed", selected=false.
			// Shot 2 and Shot 3 (addedCount == 0) keep their ShotArtifact status and selected=true completely untouched!
			artByID := make(map[string]model.ShotArtifact, len(postAppendSnap.Artifacts))
			for _, a := range postAppendSnap.Artifacts {
				artByID[a.ID] = a
			}
			if artByID["art-s1-succeeded"].Status != "stale" || artByID["art-s1-succeeded"].Selected || artByID["art-s1-succeeded"].MetadataJSON != `{"frame":"s1-ok"}` {
				t.Fatalf("expected art-s1-succeeded to be stale/selected=false with preserved content, got %+v", artByID["art-s1-succeeded"])
			}
			if artByID["art-s1-approved"].Status != "stale" || artByID["art-s1-approved"].Selected || artByID["art-s1-approved"].MetadataJSON != `{"frame":"s1-app"}` {
				t.Fatalf("expected art-s1-approved to be stale/selected=false with preserved content, got %+v", artByID["art-s1-approved"])
			}
			if artByID["art-s1-failed"].Status != "failed" || artByID["art-s1-failed"].Selected || artByID["art-s1-failed"].MetadataJSON != `{"frame":"s1-fail"}` {
				t.Fatalf("expected art-s1-failed to remain status=failed with selected=false and preserved content, got %+v", artByID["art-s1-failed"])
			}
			if artByID["art-s2-succeeded"].Status != "succeeded" || !artByID["art-s2-succeeded"].Selected || artByID["art-s2-succeeded"].MetadataJSON != `{"frame":"s2-ok"}` {
				t.Fatalf("Shot 2 (addedCount=0) artifact art-s2-succeeded was mutated: %+v", artByID["art-s2-succeeded"])
			}
			if artByID["art-s2-approved"].Status != "approved" || artByID["art-s2-approved"].Selected || artByID["art-s2-approved"].MetadataJSON != `{"frame":"s2-app"}` {
				t.Fatalf("Shot 2 (addedCount=0) artifact art-s2-approved was mutated: %+v", artByID["art-s2-approved"])
			}
			if artByID["art-s3-succeeded"].Status != "succeeded" || !artByID["art-s3-succeeded"].Selected || artByID["art-s3-succeeded"].MetadataJSON != `{"frame":"s3-ok"}` {
				t.Fatalf("Shot 3 (addedCount=0) artifact art-s3-succeeded was mutated: %+v", artByID["art-s3-succeeded"])
			}
			assertZeroPaidOrOutboxSideEffects("selective_multi_shot_append")

			// 5. Disconnect Idempotent Replay Verification:
			// Replay the exact same { baseline, selections } request after commit -> 200 OK unchanged=true, addedCount=0, projectRevision=12, zero DB writes.
			cleanupReplayDetector := installBusinessWriteDetector(t, db)
			status, replayEnv, rawReplayBody := doHTTP("append", selectiveReq)
			replayWrites := cleanupReplayDetector()
			if status != http.StatusOK || int(replayEnv["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on disconnect idempotent replay, got status=%d env=%+v", status, replayEnv)
			}
			var decodedReplay struct {
				Data BindingAppendResult `json:"data"`
			}
			if err := json.Unmarshal(rawReplayBody, &decodedReplay); err != nil {
				t.Fatal(err)
			}
			replayRes := decodedReplay.Data
			if !replayRes.Unchanged || replayRes.AddedCount != 0 || replayRes.ProjectRevision != 12 || len(replayRes.Shots) != 3 {
				t.Fatalf("unexpected disconnect replay result: %+v", replayRes)
			}
			for i, sr := range replayRes.Shots {
				if !sr.Unchanged || sr.AddedCount != 0 || len(sr.AddedVersionIDs) != 0 {
					t.Fatalf("expected shot[%d] in replay to be unchanged=true addedCount=0, got %+v", i, sr)
				}
			}
			if replayWrites != 0 {
				t.Fatalf("expected 0 business DB writes on disconnect replay, got %d", replayWrites)
			}
			if captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg").EncodedFullDB != postAppendSnap.EncodedFullDB {
				t.Fatalf("disconnect replay mutated DB snapshot!")
			}
			assertZeroPaidOrOutboxSideEffects("disconnect_idempotent_replay")

			// 6. Post-Commit Drift -> 409 Conflict Verification:
			// (a) Re-select artifact on Shot 1 (selected = true)
			if err := db.Model(&model.ShotArtifact{}).Where("id = ?", "art-s1-succeeded").Updates(map[string]any{"selected": true, "status": "succeeded"}).Error; err != nil {
				t.Fatal(err)
			}
			snapDriftA := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			status, driftEnvA, _ := doHTTP("append", selectiveReq)
			if status != http.StatusConflict || int(driftEnvA["code"].(float64)) != CodeConflict || driftEnvA["reason"] != string(ReasonStoryboardAssetBindingConflict) {
				t.Fatalf("expected 409 storyboard_asset_binding_conflict after artifact re-select, got status=%d env=%+v", status, driftEnvA)
			}
			if captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg").EncodedFullDB != snapDriftA.EncodedFullDB {
				t.Fatalf("rejected 409 replay (artifact re-select) mutated DB!")
			}
			if err := db.Model(&model.ShotArtifact{}).Where("id = ?", "art-s1-succeeded").Updates(map[string]any{"selected": false, "status": "stale"}).Error; err != nil {
				t.Fatal(err)
			}

			// (b) Add another reference on Shot 3
			if err := db.Create(&model.ShotAssetReference{ID: "ref-s3-extra", ShotID: "shot-leg-3", AssetVersionID: "v-shen-1", Role: "character", Status: "confirmed"}).Error; err != nil {
				t.Fatal(err)
			}
			snapDriftB := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			status, driftEnvB, _ := doHTTP("append", selectiveReq)
			if status != http.StatusConflict || int(driftEnvB["code"].(float64)) != CodeConflict || driftEnvB["reason"] != string(ReasonStoryboardAssetBindingConflict) {
				t.Fatalf("expected 409 storyboard_asset_binding_conflict after extra reference added, got status=%d env=%+v", status, driftEnvB)
			}
			if captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg").EncodedFullDB != snapDriftB.EncodedFullDB {
				t.Fatalf("rejected 409 replay (extra reference) mutated DB!")
			}
			if err := db.Delete(&model.ShotAssetReference{}, "id = ?", "ref-s3-extra").Error; err != nil {
				t.Fatal(err)
			}

			// (c) Edit a shot script
			if err := db.Model(&model.ShotRevision{}).Where("id = ?", "rev-leg-2").Update("plot_description", "角色：裴让（修订台本）").Error; err != nil {
				t.Fatal(err)
			}
			snapDriftC := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			status, driftEnvC, _ := doHTTP("append", selectiveReq)
			if status != http.StatusConflict || int(driftEnvC["code"].(float64)) != CodeConflict || driftEnvC["reason"] != string(ReasonStoryboardAssetBindingConflict) {
				t.Fatalf("expected 409 storyboard_asset_binding_conflict after shot script edit, got status=%d env=%+v", status, driftEnvC)
			}
			if captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg").EncodedFullDB != snapDriftC.EncodedFullDB {
				t.Fatalf("rejected 409 replay (script edit) mutated DB!")
			}
			if err := db.Model(&model.ShotRevision{}).Where("id = ?", "rev-leg-2").Update("plot_description", "角色：裴让\n场景：栖云阁").Error; err != nil {
				t.Fatal(err)
			}

			// 7. Follow-up fresh preview + manual selection on Shot 3 ("v-shen-1") -> invalidates ONLY Shot 3, leaving Shot 2 untouched
			status, preview2Env, rawPreview2Body := doHTTP("preview", BindingPreviewRequest{
				ShotIDs: []string{"shot-leg-1", "shot-leg-2", "shot-leg-3"},
			})
			if status != http.StatusOK || int(preview2Env["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on fresh preview, got status=%d env=%+v", status, preview2Env)
			}
			var decodedPreview2 struct {
				Data BindingPreview `json:"data"`
			}
			if err := json.Unmarshal(rawPreview2Body, &decodedPreview2); err != nil {
				t.Fatal(err)
			}
			status, append2Env, _ := doHTTP("append", BindingAppendRequest{
				Baseline: decodedPreview2.Data.Baseline,
				Selections: []BindingSelection{
					{ShotID: "shot-leg-1", AssetVersionIDs: []string{"v-sangruo-1", "v-peirang-1", "v-qiyunge-1"}},
					{ShotID: "shot-leg-2", AssetVersionIDs: []string{"v-peirang-1", "v-qiyunge-1"}},
					{ShotID: "shot-leg-3", AssetVersionIDs: []string{"v-shen-1"}},
				},
			})
			if status != http.StatusOK || int(append2Env["code"].(float64)) != 0 {
				t.Fatalf("expected 200 OK on Shot 3 manual selection append, got status=%d env=%+v", status, append2Env)
			}
			finalSnap := captureLegacyChapterFullSnapshot(t, db, "p-leg", "unit-leg")
			if finalSnap.ProjectRevision != 13 {
				t.Fatalf("expected Project.Revision=13 after second append, got %d", finalSnap.ProjectRevision)
			}
			finalArtByID := make(map[string]model.ShotArtifact, len(finalSnap.Artifacts))
			for _, a := range finalSnap.Artifacts {
				finalArtByID[a.ID] = a
			}
			if finalArtByID["art-s3-succeeded"].Status != "stale" || finalArtByID["art-s3-succeeded"].Selected {
				t.Fatalf("expected Shot 3 artifact art-s3-succeeded to become stale/selected=false after manual selection append, got %+v", finalArtByID["art-s3-succeeded"])
			}
			if finalArtByID["art-s2-succeeded"].Status != "succeeded" || !finalArtByID["art-s2-succeeded"].Selected {
				t.Fatalf("Shot 2 artifact art-s2-succeeded must STILL remain succeeded/selected=true, got %+v", finalArtByID["art-s2-succeeded"])
			}
			assertZeroPaidOrOutboxSideEffects("shot3_manual_selection_append")
		})
	}
}

