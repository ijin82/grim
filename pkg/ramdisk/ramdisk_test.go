package ramdisk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRAMWorkspaceLifecycle(t *testing.T) {
	ws, err := New("testvault")
	if err != nil {
		t.Fatalf("Failed to create RAM workspace: %v", err)
	}

	if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
		t.Fatalf("Expected RAM path %s to exist", ws.Path)
	}

	// Create test note inside RAM workspace
	subDir := filepath.Join(ws.Path, "subfolder")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatalf("Failed to create subfolder: %v", err)
	}

	notePath := filepath.Join(subDir, "secret.md")
	if err := os.WriteFile(notePath, []byte("Super confidential password 12345"), 0600); err != nil {
		t.Fatalf("Failed to write note: %v", err)
	}

	// Destroy workspace
	if err := ws.Destroy(); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify directory is completely removed
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("Expected workspace directory to be deleted, but it still exists")
	}
}
