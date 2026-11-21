package ramdisk

import (
	"encoding/json"
	"fmt"
	"os"
)

// workspaceLock is a platform-specific advisory lock held on the session lock file.
//
// Unlike PID checks, flock/LockFileEx works at the kernel level and does not
// depend on the PID namespace. This matters for Flatpak and containers: a
// process inside the sandbox cannot see the host PID, but it can see the same
// file lock.
type workspaceLock struct {
	file *os.File
}

// writeWorkspaceLockFile writes session metadata into an already locked file
// descriptor. Truncate+Seek are required because the file may be left over
// from a previous session and may be longer than the new JSON payload.
func writeWorkspaceLockFile(file *os.File, info LockInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal lock info: %w", err)
	}

	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate lock file: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek lock file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync lock file: %w", err)
	}

	return nil
}
