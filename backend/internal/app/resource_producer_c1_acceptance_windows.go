//go:build windows && profile_media_acceptance

package app

import "yingce/backend/internal/repository"

// NewWindowsC1AcceptanceService is an isolated acceptance composition root.
// The default product build does not contain this entry point.
func NewWindowsC1AcceptanceService(repo *repository.Repository, root, ffmpeg string) (*Service, error) {
	s := New(repo, root)
	controller, err := newC1PlaybackController(s.repo, root, ffmpeg)
	if err != nil {
		return nil, err
	}
	s.playbackControl = controller
	if err := controller.recover(s); err != nil {
		return nil, err
	}
	return s, nil
}
