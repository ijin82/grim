//go:build !windows

package ramdisk

import "syscall"

// isProcessAlive checks whether a process exists via signal 0.
// It is only used as a fallback for legacy sessions without flock.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
