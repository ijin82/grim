package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ijin82/grim/pkg/ramdisk"
	"github.com/ijin82/grim/pkg/vault"
)

func TestEndToEndLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "secure-notes.enc")
	passphrase := "my-e2e-master-passphrase-2026"

	// 1. Init
	if err := vault.Init(vaultPath, "WorkNotes", passphrase); err != nil {
		t.Fatalf("Failed to init vault: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Failed to verify passphrase: %v", err)
	}

	// 2. Unlock to RAM
	ws, err := ramdisk.New("WorkNotes")
	if err != nil {
		t.Fatalf("Failed to create RAM workspace: %v", err)
	}

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Failed to unlock vault: %v", err)
	}

	// 3. Start background watcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncEvents := make(chan string, 10)
	go func() {
		_ = vault.WatchAndSync(ctx, ws.Path, vaultPath, meta.PublicKey, nil, func(event, path string) {
			syncEvents <- event + ":" + path
		})
	}()

	time.Sleep(50 * time.Millisecond)

	// 4. Create new markdown file in RAM
	noteContent := "# Kubernetes Secrets\nToken: eyJhbGciOi..."
	secretFile := filepath.Join(ws.Path, "k8s-secrets.md")
	if err := os.WriteFile(secretFile, []byte(noteContent), 0600); err != nil {
		t.Fatalf("Failed to write secret note: %v", err)
	}

	// Wait for sync event
	select {
	case ev := <-syncEvents:
		t.Logf("Received sync event: %s", ev)
	case <-time.After(3 * time.Second):
		t.Fatalf("Timeout waiting for fsnotify sync")
	}

	// Verify .age file exists on disk
	encFile := filepath.Join(vaultPath, "k8s-secrets.md.age")
	if _, err := os.Stat(encFile); os.IsNotExist(err) {
		t.Fatalf("Expected encrypted file %s to exist on disk", encFile)
	}

	// 5. Lock and wipe
	cancel()
	if err := vault.Lock(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("Failed to lock vault: %v", err)
	}

	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("RAM workspace was not deleted on lock")
	}

	// 6. Re-open in a fresh workspace
	ws2, err := ramdisk.New("WorkNotes-2")
	if err != nil {
		t.Fatalf("Failed to create second RAM workspace: %v", err)
	}
	defer func() {
		_ = ws2.Destroy()
	}()

	if err := vault.Unlock(vaultPath, ws2.Path, meta); err != nil {
		t.Fatalf("Failed to re-unlock vault: %v", err)
	}

	restoredFile := filepath.Join(ws2.Path, "k8s-secrets.md")
	restoredData, err := os.ReadFile(restoredFile)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}

	if string(restoredData) != noteContent {
		t.Fatalf("Restored content mismatch: got %s, want %s", string(restoredData), noteContent)
	}
}
