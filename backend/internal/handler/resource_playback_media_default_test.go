//go:build !windows || !profile_media_acceptance

package handler

import (
	"os"
	"testing"

	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

const realMediaC1Available = false

func newRealMediaAcceptanceService(t *testing.T, repo *repository.Repository, root, ffmpeg string) (*service.Service, bool) {
	t.Helper()
	if os.Getenv("PROFILE_MEDIA_C1") == "1" {
		t.Fatal("PROFILE_MEDIA_C1=1 requires Windows and -tags profile_media_acceptance; refusing ordinary-launcher fallback")
	}
	return service.New(repo, root), false
}
