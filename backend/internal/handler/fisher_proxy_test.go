package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"
)

func TestFisherSystemProxyAuthorization(t *testing.T) {
	channel := &model.ModelChannel{ModelsJSON: `["MiniMax-H3"]`}
	for _, path := range []string{"/v2/video_generation", "/v2/query/video_generation/task-one"} {
		method := http.MethodPost
		if strings.Contains(path, "query") {
			method = http.MethodGet
		}
		if err := authorizeSystemProxy(channel, "fisher-h3-video", method, path, "application/json", []byte(`{"model":"MiniMax-H3","content":[{"type":"text","text":"prompt"}],"resolution":"768P","duration":5,"ratio":"16:9"}`)); err != nil {
			t.Fatalf("Fisher endpoint denied: %v", err)
		}
	}
	for _, path := range []string{"/chat/completions", "/v2/query/video_generation", "/v2/video_generation/task-one"} {
		if err := authorizeSystemProxy(channel, "fisher-h3-video", http.MethodPost, path, "application/json", []byte(`{"model":"MiniMax-H3"}`)); err == nil {
			t.Fatalf("unexpected endpoint allowed %s", path)
		}
	}
}

func TestFisherSystemMissingBodyTaskIDReturnsRecoverableError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(database.Models()...); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "fisher-proxy-user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "feature_availability", ValueJSON: `{"creditsEnabled":false}`}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), t.TempDir())
	t.Cleanup(func() { _ = svc.Close() })
	previous := runtimeService
	runtimeService = svc
	t.Cleanup(func() { runtimeService = previous })
	for index, body := range []string{`{"request_id":"trace-only"}`, `broken`} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			posts := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts++
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Task-Id", "existing-fisher-task")
				fmt.Fprint(w, body)
			}))
			defer upstream.Close()
			channel := &model.ModelChannel{ID: fmt.Sprintf("fisher-%d", index), Scope: model.ChannelScopeSystem, Enabled: true, BaseURL: upstream.URL, APIKey: "test-key", APIFormat: "openai", ModelsJSON: `["MiniMax-H3"]`}
			if err := db.Create(channel).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ChannelModel{ID: channel.ID, ChannelID: channel.ID, ModelKey: "MiniMax-H3", Protocol: "fisher-h3-video", Capability: "video", Enabled: true}).Error; err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/system/"+channel.ID+"/v2/video_generation", strings.NewReader(`{"model":"MiniMax-H3","content":[{"type":"text","text":"prompt"}],"resolution":"768P","duration":5,"ratio":"16:9"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			proxySystemRequestPath(c, svc, user, channel, "/v2/video_generation")
			if posts != 1 || rec.Code != http.StatusBadGateway || rec.Header().Get("X-Task-Id") != "existing-fisher-task" {
				t.Fatalf("existing task cannot reach client recovery: posts=%d status=%d headers=%v body=%s", posts, rec.Code, rec.Header(), rec.Body.String())
			}
		})
	}
}

func TestFisherRelayValidatesProtocolHeaderBeforeConnecting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/custom", strings.NewReader(`{"model":"MiniMax-H3","content":[{"type":"text","text":"prompt"}],"resolution":"768P","ratio":"16:9","duration":60}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer test-key")
	c.Request.Header.Set("X-Canvas-Upstream-URL", "https://127.0.0.1/v2/video_generation")
	c.Request.Header.Set("X-Canvas-Upstream-Protocol", "fisher-h3-video")
	proxyCustomRelayRequest(c, defaultCustomRelayTestPolicy())
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Fisher-H3") {
		t.Fatalf("invalid Fisher relay reached upstream: %d %s", rec.Code, rec.Body.String())
	}
}

func TestFisherResponseNeverUsesTraceOrUnsafeHeaderAsTask(t *testing.T) {
	for _, id := range []string{"", "../bad", "-bad", "a/key", "a\nkey", strings.Repeat("a", 257)} {
		if actual := fisherProxyTaskID([]byte(`{"request_id":"trace"}`), id); actual != "" {
			t.Fatalf("unsafe ID %q accepted as %q", id, actual)
		}
	}
	if id := fisherProxyTaskID([]byte(`broken`), "task:one"); id != "task:one" {
		t.Fatalf("valid header lost: %s", id)
	}
}

func TestFisherRelayPreservesTaskHeaderAndRedactsSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/custom", nil)
	c.Request.Header.Set("X-Canvas-Upstream-URL", "https://api.work-fisher.com/v2/video_generation")
	resp := &http.Response{StatusCode: 503, Status: "503", Header: http.Header{"Content-Type": []string{"application/json"}, "X-Task-Id": []string{"task-one"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"test-key"}}`))}
	writeCustomRelayResponse(c, nil, resp, "test-key", 1024, false)
	if rec.Header().Get("X-Task-Id") != "task-one" || strings.Contains(rec.Body.String(), "test-key") {
		t.Fatalf("task header/redaction: %v %s", rec.Header(), rec.Body.String())
	}
}

func TestFisherSystemRejectsInvalidDuration(t *testing.T) {
	channel := &model.ModelChannel{ModelsJSON: `["MiniMax-H3"]`}
	body := []byte(`{"model":"MiniMax-H3","content":[{"type":"text","text":"prompt"}],"resolution":"768P","ratio":"16:9","duration":60}`)
	if err := authorizeSystemProxy(channel, "fisher-h3-video", http.MethodPost, "/v2/video_generation", "application/json", body); err == nil {
		t.Fatal("ordinary 60 second task passed authorization validation")
	}
}

func TestFisherCustomMissingBodyTaskIDReturnsRecoverableError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, protocol, body, taskHeader string
		contentType                      string
		upstreamStatus, wantStatus       int
	}{
		{"trace_only", "fisher-h3-video", `{"request_id":"trace-only"}`, "existing-fisher-task", "application/json", 200, 502},
		{"accepted_without_id", "fisher-h3-video", `{"request_id":"trace-only"}`, "", "application/json", 202, 502},
		{"unsafe_body_id", "fisher-h3-video", `{"task_id":"../unsafe"}`, "existing-fisher-task", "application/json", 200, 502},
		{"key_header", "fisher-h3-video", `{"request_id":"trace-only"}`, "test-key", "application/json", 200, 502},
		{"valid_id", "fisher-h3-video", `{"task_id":"accepted-fisher-task"}`, "", "application/json", 200, 200},
		{"other_protocol", "minimax-video", `{"request_id":"trace-only"}`, "existing-fisher-task", "application/json", 200, 200},
		{"fisher_sse", "fisher-h3-video", "data: {}\n\n", "existing-fisher-task", "text/event-stream", 200, 502},
		{"fisher_sse_no_header", "fisher-h3-video", "data: {}\n\n", "", "text/event-stream", 200, 502},
		{"other_protocol_sse", "minimax-video", "data: {}\n\n", "existing-fisher-task", "text/event-stream", 200, 200},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			posts := 0
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method %s", r.Method)
				}
				posts++
				w.Header().Set("Content-Type", scenario.contentType)
				w.Header().Set("X-Task-Id", scenario.taskHeader)
				w.WriteHeader(scenario.upstreamStatus)
				fmt.Fprint(w, scenario.body)
			}))
			defer upstream.Close()
			useCustomRelayTestClient(t, upstream.Client())
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/custom", strings.NewReader(`{"model":"MiniMax-H3","content":[{"type":"text","text":"prompt"}],"resolution":"768P","duration":5,"ratio":"16:9"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Authorization", "Bearer test-key")
			c.Request.Header.Set("X-Canvas-Upstream-URL", upstream.URL+"/v2/video_generation")
			c.Request.Header.Set("X-Canvas-Upstream-Protocol", scenario.protocol)
			prepareRelayTestRequest(t, c.Request)
			proxyCustomRelayRequest(c, defaultCustomRelayTestPolicy())
			wantHeader := scenario.taskHeader
			if wantHeader == "test-key" {
				wantHeader = ""
			}
			if posts != 1 || rec.Code != scenario.wantStatus || rec.Header().Get("X-Task-Id") != wantHeader {
				t.Fatalf("custom task cannot reach client recovery: posts=%d status=%d headers=%v body=%s", posts, rec.Code, rec.Header(), rec.Body.String())
			}
		})
	}
}
