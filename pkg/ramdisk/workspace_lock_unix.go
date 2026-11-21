//go:build !windows

package ramdisk

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquireWorkspaceLock takes an exclusive flock on the session lock file.
// The lock is held by the open descriptor until Close(), so other processes
// (including processes in another PID namespace) see it as active.
func acquireWorkspaceLock(path string, info LockInfo) (*workspaceLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("workspace is already locked by another process: %w", err)
	}

	if err := writeWorkspaceLockFile(file, info); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, err
	}

	return &workspaceLock{file: file}, nil
}

// Close releases the lock and closes the descriptor. The method is idempotent:
// Workspace.Close may be called both directly and through Destroy.
func (l *workspaceLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	file := l.file
	l.file = nil
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return file.Close()
}

// isWorkspaceLockHeld reports whether another process holds an exclusive flock.
//
// Open errors other than "file does not exist" are treated as "session is
// active": failing to remove a live workspace is safer than leaving a stale
// plaintext copy behind.
func isWorkspaceLockHeld(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		return true
	}
	defer func() { _ = file.Close() }()

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return false
	}

	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return true
	}

	// Unknown error: do not assume the workspace is abandoned.
	return true
}
