//go:build windows && profile_media_acceptance

package handler

import (
	"os"
	"testing"

	"yingce/backend/internal/app"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

const realMediaC1Available = true

func newRealMediaAcceptanceService(t *testing.T, repo *repository.Repository, root, ffmpeg string) (*service.Service, bool) {
	t.Helper()
	if os.Getenv("PROFILE_MEDIA_C1") != "1" {
		return service.New(repo, root), false
	}
	s, err := app.NewWindowsC1AcceptanceService(repo, root, ffmpeg)
	if err != nil {
		t.Fatal("explicit Windows C1 acceptance composition failed:", err)
	}
	return s, true
}
