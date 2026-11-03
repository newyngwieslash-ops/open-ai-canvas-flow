//go:build !windows && !linux

package repository

import "os"

func checkPhysicalLockPath(string, bool) error { return os.ErrInvalid }
