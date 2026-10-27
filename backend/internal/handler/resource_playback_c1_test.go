package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

// Synthetic bytes exercise the real authenticated route, not encoder or browser
// playback acceptance. C1 media acceptance still requires a capable ffmpeg.
func TestProducerP3CPlaybackHTTPRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	// Pre-provision a synthetic key: Windows directory Sync during first-key
	// publication is a separate existing limitation, not a playback route check.
	if err := os.WriteFile(filepath.Join(dir, ".settings-key"), []byte("01234567890123456789012345678901"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "route.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, record := range []any{
		&model.User{ID: "c1-owner", Username: "c1-owner", Email: "c1@example.invalid", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "c1-session", UserID: "c1-owner", TokenHash: auth.HashToken("fixture-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.Resource{ID: "c1-playback", UserID: "c1-owner", Kind: "video", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "original.mp4", MimeType: "video/mp4", PlaybackStatus: model.PlaybackStatusReady, PlaybackObjectKey: "c1-playback.mp4"},
		&model.Resource{ID: "c1-foreign", UserID: "someone-else", Kind: "video", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "foreign.mp4", MimeType: "video/mp4", PlaybackStatus: model.PlaybackStatusReady, PlaybackObjectKey: "c1-playback.mp4"},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "playback"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "playback", "c1-playback.mp4"), []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), service.New(repository.New(db), dir))
	for _, tc := range []struct {
		name, id, byteRange, body, contentRange string
		authenticated                           bool
		status                                  int
	}{
		{name: "whole", id: "c1-playback", authenticated: true, status: 200, body: "0123456789"},
		{name: "range", id: "c1-playback", byteRange: "bytes=2-5", authenticated: true, status: 206, body: "2345", contentRange: "bytes 2-5/10"},
		{name: "suffix", id: "c1-playback", byteRange: "bytes=-3", authenticated: true, status: 206, body: "789", contentRange: "bytes 7-9/10"},
		{name: "unsatisfiable", id: "c1-playback", byteRange: "bytes=20-30", authenticated: true, status: 416, contentRange: "bytes */10"},
		{name: "anonymous", id: "c1-playback", status: 401},
		{name: "foreign", id: "c1-foreign", authenticated: true, status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/resources/"+tc.id+"/file?variant=playback", nil)
			req.Header.Set("Range", tc.byteRange)
			if tc.authenticated {
				req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "c1-session.fixture-token"})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || tc.body != "" && response.Body.String() != tc.body || response.Header().Get("Content-Range") != tc.contentRange {
				t.Fatalf("status=%d body=%q headers=%v", response.Code, response.Body.String(), response.Header())
			}
		})
	}
}
