//go:build windows

package ramdisk

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// acquireWorkspaceLock takes an exclusive lock on the session lock file via
// LockFileEx. This is the Windows equivalent of flock: the lock lives as long
// as the descriptor is open and does not depend on a correct PID check.
func acquireWorkspaceLock(path string, info LockInfo) (*workspaceLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	var overlapped windows.Overlapped
	if err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped,
	); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("workspace is already locked by another process: %w", err)
	}

	if err := writeWorkspaceLockFile(file, info); err != nil {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		_ = file.Close()
		return nil, err
	}

	return &workspaceLock{file: file}, nil
}

// Close releases the lock and closes the descriptor. The method is idempotent.
func (l *workspaceLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	file := l.file
	l.file = nil

	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
	return file.Close()
}

// isWorkspaceLockHeld reports whether another process holds a LockFileEx lock.
// Any error other than "file does not exist" is treated as an active session.
func isWorkspaceLockHeld(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		return true
	}
	defer func() { _ = file.Close() }()

	var overlapped windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped,
	)
	if err == nil {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		return false
	}

	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true
	}

	return true
}
