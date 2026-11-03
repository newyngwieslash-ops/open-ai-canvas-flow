package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofrs/flock"
	"yingce/backend/internal/model"
)

const LocalGoProducerProtocol = "local-go-lock-v1"

type physicalLocksContextKey struct{}

// ResourcePhysicalLocks is a scoped OS-lock capability, never a lease or flag.
// Do not remove lock files: replacing their inode breaks exclusion with waiters.
type ResourcePhysicalLocks struct {
	root, directory         string
	rootInfo, directoryInfo os.FileInfo
	files                   map[string]*flock.Flock
	infos                   map[string]os.FileInfo
	closed                  bool
	closeFile               func(*flock.Flock) error
}

func ResourcePhysicalIdentity(row *model.Resource, variant string) string {
	return resourceProducerIdentity(row, variant).Identity
}

func AcquireResourcePhysicalLocks(root string, identities []string) (_ *ResourcePhysicalLocks, err error) {
	if len(identities) == 0 {
		return &ResourcePhysicalLocks{files: map[string]*flock.Flock{}, infos: map[string]os.FileInfo{}}, nil
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for at := root; ; at = filepath.Dir(at) {
		info, e := os.Lstat(at)
		if e != nil {
			return nil, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrResourceProducerUnsettled
		}
		if e := checkPhysicalLockPath(at, true); e != nil {
			return nil, e
		}
		if filepath.Dir(at) == at {
			break
		}
	}
	directory := filepath.Join(root, ".resource-producer-locks")
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	l := &ResourcePhysicalLocks{root: root, directory: directory, files: map[string]*flock.Flock{}, infos: map[string]os.FileInfo{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, l.Close())
		}
	}()
	if l.rootInfo, err = os.Lstat(root); err != nil {
		return nil, err
	}
	if l.directoryInfo, err = os.Lstat(directory); err != nil {
		return nil, err
	}
	if !l.directoryInfo.IsDir() || l.directoryInfo.Mode()&os.ModeSymlink != 0 {
		return nil, ErrResourceProducerUnsettled
	}
	if err = checkPhysicalLockPath(directory, true); err != nil {
		return nil, err
	}
	keys := append([]string(nil), identities...)
	sort.Strings(keys)
	for _, identity := range keys {
		if len(identity) != 64 || strings.Trim(identity, "0123456789abcdef") != "" {
			return nil, ErrResourceProducerUnsettled
		}
		if l.files[identity] != nil {
			continue
		}
		path := filepath.Join(directory, identity+".lock")
		if before, e := os.Lstat(path); e == nil {
			if !before.Mode().IsRegular() {
				return nil, ErrResourceProducerUnsettled
			}
			if e = checkPhysicalLockPath(path, false); e != nil {
				return nil, e
			}
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		file, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		info, e := file.Stat()
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return nil, errors.Join(e, closeErr)
		}
		if !info.Mode().IsRegular() {
			return nil, ErrResourceProducerUnsettled
		}
		if e = checkPhysicalLockPath(path, false); e != nil {
			return nil, e
		}
		lock := flock.New(path) // A fresh instance for every acquisition; never reentrant.
		ok, e := lock.TryLock()
		if e != nil || !ok {
			return nil, errors.Join(ErrResourceProducerUnsettled, e, lock.Close())
		}
		l.files[identity], l.infos[identity] = lock, info
		if e = l.Validate(identity); e != nil {
			return nil, e
		}
	}
	return l, nil
}

func (l *ResourcePhysicalLocks) Validate(identity string) error {
	if l == nil || l.closed || l.files[identity] == nil || !l.files[identity].Locked() {
		return ErrResourceProducerUnsettled
	}
	for path, expected := range map[string]os.FileInfo{l.root: l.rootInfo, l.directory: l.directoryInfo, l.files[identity].Path(): l.infos[identity]} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, expected) {
			return ErrResourceProducerUnsettled
		}
		if err := checkPhysicalLockPath(path, info.IsDir()); err != nil {
			return err
		}
	}
	return nil
}

func (l *ResourcePhysicalLocks) Close() error {
	if l == nil || l.closed {
		return nil
	}
	l.closed = true
	var err error
	for _, lock := range l.files {
		if l.closeFile != nil {
			err = errors.Join(err, l.closeFile(lock))
		} else {
			err = errors.Join(err, lock.Close())
		}
	}
	return err
}

func (r *Repository) WithResourcePhysicalLocks(locks *ResourcePhysicalLocks) *Repository {
	return r.WithContext(context.WithValue(r.db.Statement.Context, physicalLocksContextKey{}, locks))
}

func (r *Repository) physicalLocks() *ResourcePhysicalLocks {
	locks, _ := r.db.Statement.Context.Value(physicalLocksContextKey{}).(*ResourcePhysicalLocks)
	return locks
}
