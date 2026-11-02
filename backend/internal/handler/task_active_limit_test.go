package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

func TestTaskActiveLimitHTTPProjection(t *testing.T) {
	recorder, ctx := responseTestContext()
	err := service.NewAppError(400, "活动任务已满")
	err.Reason = service.ReasonTaskActiveLimitExceeded
	failService(ctx, err)
	body := decodeFailureEnvelope(t, recorder)
	if recorder.Code != 400 || body.Code != 400 || body.Reason != "task_active_limit_exceeded" {
		t.Fatalf("status=%d body=%#v", recorder.Code, body)
	}
}

func setupTaskActiveLimitHTTPFixture(t *testing.T) (*gin.Engine, *gorm.DB, *service.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := database.Open(database.Config{Driver: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	capabilityConfigBytes, err := json.Marshal(service.DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceChatCompletion), "text-test"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []any{
		&model.User{ID: "limit-user", Username: "limit-user", Email: "limit@example.invalid", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "limit-session", UserID: "limit-user", TokenHash: auth.HashToken("limit-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.User{ID: "other-user", Username: "other-user", Email: "other@example.invalid", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "other-session", UserID: "other-user", TokenHash: auth.HashToken("other-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.ModelChannel{ID: "channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "受控测试"},
		&model.ChannelModel{ID: "cm", ChannelID: "channel", ModelKey: "text-test", Capability: "text", Protocol: model.ChannelInterfaceChatCompletion, CapabilityConfigJSON: string(capabilityConfigBytes), BillingMode: "fixed_request", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true},
		&model.ChannelModelPriceTier{ID: "tier", ChannelModelID: "cm", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true},
		&model.CreditAccount{UserID: "limit-user", AvailableMicrocredits: 10000},
	} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	router := gin.New()
	svc := service.New(repository.New(db), t.TempDir())
	previous := runtimeService
	ConfigureRuntime(svc)
	t.Cleanup(func() { ConfigureRuntime(previous) })
	RegisterTaskRoutes(router.Group("/api"), svc)
	return router, db, svc
}

func TestTaskActiveLimitHTTPRoutesAndRegressions(t *testing.T) {
	router, db, svc := setupTaskActiveLimitHTTPFixture(t)
	authCookie := &http.Cookie{Name: service.SessionCookieName, Value: "limit-session.limit-token"}
	otherCookie := &http.Cookie{Name: service.SessionCookieName, Value: "other-session.other-token"}

	validCreateBody := map[string]any{
		"type":   "canvas_text",
		"prompt": "只输出测试",
		"model":  "text-test",
		"input": map[string]any{
			"mode":   "text",
			"prompt": "只输出测试",
			"config": map[string]any{
				"channelId": "channel",
				"model":     "text-test",
			},
		},
	}
	validCreateBytes, err := json.Marshal(validCreateBody)
	if err != nil {
		t.Fatal(err)
	}

	// Create one task via HTTP and convert it to a retryable failed task.
	createReq := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(validCreateBytes))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(authCookie)
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("seed create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var createdEnvelope struct {
		Code int `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatal(err)
	}
	retryTaskID := createdEnvelope.Data.ID
	if err := db.Model(&model.Task{}).Where("id = ?", retryTaskID).Updates(map[string]any{
		"status":           model.TaskStatusFailed,
		"error":            "upstream error",
		"billing_order_id": "",
		"prompt":           "SECRET_PROMPT_DO_NOT_LEAK",
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Cross-account retry must not leak task content and must keep invalid_argument projection.
	crossRetryReq := httptest.NewRequest(http.MethodPost, "/api/tasks/"+retryTaskID+"/retry", nil)
	crossRetryReq.AddCookie(otherCookie)
	crossRetryRec := httptest.NewRecorder()
	router.ServeHTTP(crossRetryRec, crossRetryReq)
	crossRetryBody := decodeFailureEnvelope(t, crossRetryRec)
	if crossRetryRec.Code != http.StatusBadRequest || crossRetryBody.Code != 400 || crossRetryBody.Reason != "invalid_argument" {
		t.Fatalf("unexpected cross-user retry response: status=%d body=%#v", crossRetryRec.Code, crossRetryBody)
	}
	if strings.Contains(crossRetryRec.Body.String(), "SECRET_PROMPT_DO_NOT_LEAK") {
		t.Fatalf("cross-user retry leaked task prompt: %s", crossRetryRec.Body.String())
	}

	// Non-capacity invalid_argument on POST /tasks still preserves invalid_argument reason.
	invalidCreateBytes, _ := json.Marshal(map[string]any{
		"type":   "canvas_video",
		"prompt": "missing video config",
		"input":  map[string]any{"mode": "video"},
	})
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(invalidCreateBytes))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidReq.AddCookie(authCookie)
	invalidRec := httptest.NewRecorder()
	router.ServeHTTP(invalidRec, invalidReq)
	invalidBody := decodeFailureEnvelope(t, invalidRec)
	if invalidRec.Code != http.StatusBadRequest || invalidBody.Code != 400 || invalidBody.Reason != "invalid_argument" {
		t.Fatalf("expected invalid_argument projection, got status=%d body=%#v", invalidRec.Code, invalidBody)
	}

	// Fill active tasks up to ActiveTaskLimit.
	policy, err := svc.RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < policy.Task.ActiveTaskLimit; i++ {
		if err := db.Create(&model.Task{
			ID:     fmt.Sprintf("http-active-slot-%d", i),
			UserID: "limit-user",
			Type:   "canvas_text",
			Status: model.TaskStatusQueued,
			Prompt: "slot",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	// POST /api/tasks when active limit is full returns 400 / 400 / task_active_limit_exceeded.
	capCreateReq := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(validCreateBytes))
	capCreateReq.Header.Set("Content-Type", "application/json")
	capCreateReq.AddCookie(authCookie)
	capCreateRec := httptest.NewRecorder()
	router.ServeHTTP(capCreateRec, capCreateReq)
	capCreateBody := decodeFailureEnvelope(t, capCreateRec)
	if capCreateRec.Code != http.StatusBadRequest || capCreateBody.Code != 400 || capCreateBody.Reason != "task_active_limit_exceeded" {
		t.Fatalf("POST /tasks active limit projection failed: status=%d body=%#v", capCreateRec.Code, capCreateBody)
	}

	// POST /api/tasks/:id/retry when active limit is full returns 400 / 400 / task_active_limit_exceeded.
	capRetryReq := httptest.NewRequest(http.MethodPost, "/api/tasks/"+retryTaskID+"/retry", nil)
	capRetryReq.AddCookie(authCookie)
	capRetryRec := httptest.NewRecorder()
	router.ServeHTTP(capRetryRec, capRetryReq)
	capRetryBody := decodeFailureEnvelope(t, capRetryRec)
	if capRetryRec.Code != http.StatusBadRequest || capRetryBody.Code != 400 || capRetryBody.Reason != "task_active_limit_exceeded" {
		t.Fatalf("POST /tasks/:id/retry active limit projection failed: status=%d body=%#v", capRetryRec.Code, capRetryBody)
	}

	// Rate limit 429 on POST /api/tasks still returns 429 / 42901 / rate_limited with Retry-After.
	var sawRateLimited bool
	for i := 0; i <= policy.Request.TaskCreatePerMinute+5; i++ {
		rlReq := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(validCreateBytes))
		rlReq.Header.Set("Content-Type", "application/json")
		rlReq.AddCookie(authCookie)
		rlRec := httptest.NewRecorder()
		router.ServeHTTP(rlRec, rlReq)
		if rlRec.Code == http.StatusTooManyRequests {
			rlBody := decodeFailureEnvelope(t, rlRec)
			if rlBody.Code != 42901 || rlBody.Reason != "rate_limited" || rlRec.Header().Get("Retry-After") == "" {
				t.Fatalf("unexpected 429 envelope: status=%d header=%q body=%#v", rlRec.Code, rlRec.Header().Get("Retry-After"), rlBody)
			}
			sawRateLimited = true
			break
		}
	}
	if !sawRateLimited {
		t.Fatal("expected POST /api/tasks rate limit 429 to trigger")
	}
}
