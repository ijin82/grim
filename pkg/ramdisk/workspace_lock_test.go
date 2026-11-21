package ramdisk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceLockMarksSessionActive(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "grim-locked")
	if err := os.MkdirAll(workspacePath, 0700); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	lock, err := acquireWorkspaceLock(filepath.Join(workspacePath, LockFileName), LockInfo{
		PID:       0, // PID must not affect the result: this test verifies flock itself
		VaultName: "locked",
	})
	if err != nil {
		t.Fatalf("failed to acquire workspace lock: %v", err)
	}

	if !isWorkspaceActive(workspacePath) {
		t.Fatalf("workspace with held lock must be considered active")
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("failed to release workspace lock: %v", err)
	}

	if isWorkspaceActive(workspacePath) {
		t.Fatalf("workspace must not be active after lock release")
	}
}

func TestCleanupSkipsLockedWorkspace(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	runDir := filepath.Join(base, "run")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("failed to create home: %v", err)
	}
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runDir)

	protected := filepath.Join(runDir, "grim", "grim-protected")
	if err := os.MkdirAll(protected, 0700); err != nil {
		t.Fatalf("failed to create protected workspace: %v", err)
	}
	lock, err := acquireWorkspaceLock(filepath.Join(protected, LockFileName), LockInfo{
		PID:       0,
		VaultName: "protected",
	})
	if err != nil {
		t.Fatalf("failed to lock protected workspace: %v", err)
	}
	defer func() { _ = lock.Close() }()

	stale := filepath.Join(runDir, "grim", "grim-stale")
	if err := os.MkdirAll(stale, 0700); err != nil {
		t.Fatalf("failed to create stale workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, LockFileName), []byte(`{"pid":0}`), 0600); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	cleaned := CleanupStaleWorkspaces()
	for _, path := range cleaned {
		if path == protected {
			t.Fatalf("cleanup must not wipe a workspace with held lock")
		}
	}

	if _, err := os.Stat(protected); err != nil {
		t.Fatalf("protected workspace was removed: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale workspace should have been wiped")
	}
}
