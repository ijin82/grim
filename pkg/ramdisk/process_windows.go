//go:build windows

package ramdisk

import "golang.org/x/sys/windows"

// isProcessAlive checks a process via OpenProcess/GetExitCodeProcess.
// It is only a fallback for legacy sessions without LockFileEx; the current
// version uses a file lock and does not depend on PIDs.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}

	// STILL_ACTIVE is defined in WinBase.h but is not exported by x/sys/windows.
	const stillActive = 259
	return exitCode == stillActive
}
