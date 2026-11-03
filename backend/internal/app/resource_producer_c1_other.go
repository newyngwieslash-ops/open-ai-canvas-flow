//go:build !windows

package app

import "errors"

func playbackControlSupported() error {
	return errors.New("Windows C1 control unavailable on this platform")
}
func validateC1PlatformPath(string) error { return playbackControlSupported() }
func newPlaybackControlDomain() (playbackControlDomain, error) {
	return nil, playbackControlSupported()
}
