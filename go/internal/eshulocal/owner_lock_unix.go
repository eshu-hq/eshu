//go:build !windows

package eshulocal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrWorkspaceOwned indicates another process already owns the workspace lock.
var ErrWorkspaceOwned = errors.New("workspace already owned")

// OwnerLock guards one workspace root at a time.
type OwnerLock struct {
	file *os.File
}

// AcquireOwnerLock tries to acquire the workspace owner lock without blocking.
func AcquireOwnerLock(path string) (*OwnerLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create owner lock directory: %w", err)
	}

	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open owner lock file: %w", err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrWorkspaceOwned
		}
		return nil, fmt.Errorf("acquire owner lock: %w", err)
	}
	return &OwnerLock{file: lockFile}, nil
}

// Close releases the owner lock.
func (l *OwnerLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer func() {
		l.file = nil
	}()
	if err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN); err != nil {
		_ = l.file.Close()
		return fmt.Errorf("unlock owner lock: %w", err)
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close owner lock file: %w", err)
	}
	return nil
}
