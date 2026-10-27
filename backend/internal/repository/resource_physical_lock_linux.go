package repository

import (
	"os"
	"syscall"
)

func checkPhysicalLockPath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSymlink != 0 || (!directory && stat.Nlink != 1) {
		return os.ErrInvalid
	}
	return nil
}
