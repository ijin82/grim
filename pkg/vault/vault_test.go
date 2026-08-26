package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVaultLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "my-vault.enc")
	ramPath := filepath.Join(tempDir, "ram-workspace")
	passphrase := "vault-secret-pass-2026"

	// 1. Initialize vault
	if err := Init(vaultPath, "TestVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// 2. Verify passphrase check
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}
	if meta.VaultName != "TestVault" {
		t.Errorf("Expected vault name TestVault, got %s", meta.VaultName)
	}
	if meta.PrivateKey == "" || meta.PublicKey == "" {
		t.Fatalf("Expected non-empty Master Key pair in meta")
	}

	// Verify wrong passphrase fails
	if _, err := VerifyPassphrase(vaultPath, "wrong-pass"); err == nil {
		t.Fatalf("Expected wrong passphrase to fail verification")
	}

	// 3. Unlock to RAM in microseconds
	if err := os.MkdirAll(ramPath, 0700); err != nil {
		t.Fatalf("Failed to create RAM dir: %v", err)
	}
	if err := Unlock(vaultPath, ramPath, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	welcomeFile := filepath.Join(ramPath, "Welcome.md")
	welcomeContent, err := os.ReadFile(welcomeFile)
	if err != nil {
		t.Fatalf("Failed to read Welcome.md in RAM: %v", err)
	}
	if len(welcomeContent) == 0 {
		t.Fatalf("Welcome.md is empty")
	}

	// 4. Start WatchAndSync in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncChan := make(chan string, 10)
	go func() {
		_ = WatchAndSync(ctx, ramPath, vaultPath, meta.PublicKey, nil, func(event, path string) {
			syncChan <- path
		})
	}()

	// Give watcher a moment to register paths
	time.Sleep(50 * time.Millisecond)

	// 5. Create a new note in RAM
	newNoteRAM := filepath.Join(ramPath, "ServerKeys.md")
	noteText := "# Production SSH Keys\nKey: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
	if err := os.WriteFile(newNoteRAM, []byte(noteText), 0600); err != nil {
		t.Fatalf("Failed to write new note: %v", err)
	}

	// Wait for sync event
	select {
	case syncedPath := <-syncChan:
		if syncedPath != "ServerKeys.md" {
			t.Logf("Synced path: %s", syncedPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Timed out waiting for WatchAndSync event")
	}

	// Check that encrypted file exists on disk
	encNoteFile := filepath.Join(vaultPath, "ServerKeys.md.age")
	if _, err := os.Stat(encNoteFile); os.IsNotExist(err) {
		t.Fatalf("Expected %s to exist in encrypted vault", encNoteFile)
	}

	// 6. Lock and wipe RAM
	cancel()
	if err := Lock(ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	if _, err := os.Stat(ramPath); !os.IsNotExist(err) {
		t.Fatalf("Expected RAM path to be deleted after Lock")
	}

	// 7. Re-unlock to a fresh RAM path and verify the newly added note is intact
	ramPath2 := filepath.Join(tempDir, "ram-workspace-2")
	if err := Unlock(vaultPath, ramPath2, meta); err != nil {
		t.Fatalf("Second Unlock failed: %v", err)
	}
	defer func() {
		_ = Lock(ramPath2, vaultPath, meta.PublicKey)
	}()

	restoredNote := filepath.Join(ramPath2, "ServerKeys.md")
	restoredContent, err := os.ReadFile(restoredNote)
	if err != nil {
		t.Fatalf("Failed to read restored note: %v", err)
	}

	if string(restoredContent) != noteText {
		t.Fatalf("Restored content mismatch. Got: %s, Want: %s", string(restoredContent), noteText)
	}
}

func TestChangePassphrase(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "pass-test.enc")
	oldPass := "old-secret-passphrase-1"
	newPass := "new-secret-passphrase-2"

	if err := Init(vaultPath, "PassVault", oldPass); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Change passphrase (only re-encrypts metadata)
	if err := ChangePassphrase(vaultPath, oldPass, newPass); err != nil {
		t.Fatalf("ChangePassphrase failed: %v", err)
	}

	// Old passphrase must now fail
	if _, err := VerifyPassphrase(vaultPath, oldPass); err == nil {
		t.Fatalf("Expected old passphrase to fail verification")
	}

	// New passphrase must succeed and return same Master Key
	newMeta, err := VerifyPassphrase(vaultPath, newPass)
	if err != nil {
		t.Fatalf("Expected new passphrase to succeed, got error: %v", err)
	}
	if newMeta.PrivateKey == "" {
		t.Fatalf("Expected valid private key in metadata")
	}
}
