package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ijin82/grim/pkg/ramdisk"
)

// TestWatcherStopsDeletingWhenLockDisappears is a regression test for the
// Flatpak incident: external cleanup removes .grim-lock, and the watcher must
// not delete encrypted files from the vault afterwards.
func TestWatcherStopsDeletingWhenLockDisappears(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "guard-vault.enc")
	ramPath := filepath.Join(tempDir, "ram")
	passphrase := "guard-pass-2026"

	if err := Init(vaultPath, "GuardVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	createTestWorkspace(t, ramPath)
	if err := Unlock(vaultPath, ramPath, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	notePath := filepath.Join(ramPath, "secret.md")
	noteText := "secret content"
	if err := os.WriteFile(notePath, []byte(noteText), 0600); err != nil {
		t.Fatalf("failed to write note: %v", err)
	}
	if err := SyncFileToVault(notePath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault failed: %v", err)
	}

	encPath := filepath.Join(vaultPath, "secret.md.age")
	if _, err := os.Stat(encPath); err != nil {
		t.Fatalf("expected encrypted note to exist: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan string, 10)
	go func() {
		_ = WatchAndSync(ctx, ramPath, vaultPath, meta.PublicKey, nil, func(event, path string) {
			events <- event + ":" + path
		})
	}()

	time.Sleep(100 * time.Millisecond)

	// Emulate an external wipe: the lock file disappears first, then the note.
	if err := os.Remove(filepath.Join(ramPath, ramdisk.LockFileName)); err != nil {
		t.Fatalf("failed to remove lock file: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.Remove(notePath); err != nil {
		t.Fatalf("failed to remove note: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	cancel()

	if _, err := os.Stat(encPath); err != nil {
		t.Fatalf("watcher deleted encrypted note after lock disappeared: %v", err)
	}
}

// TestRemoveFileFromVaultRefusesWithoutLock verifies a direct removal call:
// without .grim-lock it must not touch the vault.
func TestRemoveFileFromVaultRefusesWithoutLock(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "no-lock.enc")
	ramPath := filepath.Join(tempDir, "ram")
	passphrase := "no-lock-pass-2026"

	if err := Init(vaultPath, "NoLockVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	createTestWorkspace(t, ramPath)
	if err := Unlock(vaultPath, ramPath, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	notePath := filepath.Join(ramPath, "note.md")
	if err := os.WriteFile(notePath, []byte("data"), 0600); err != nil {
		t.Fatalf("failed to write note: %v", err)
	}
	if err := SyncFileToVault(notePath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault failed: %v", err)
	}
	encPath := filepath.Join(vaultPath, "note.md.age")

	if err := os.Remove(filepath.Join(ramPath, ramdisk.LockFileName)); err != nil {
		t.Fatalf("failed to remove lock: %v", err)
	}
	if err := RemoveFileFromVault(notePath, ramPath, vaultPath); err == nil {
		t.Fatalf("RemoveFileFromVault must fail when workspace lock is missing")
	}
	if _, err := os.Stat(encPath); err != nil {
		t.Fatalf("encrypted note must stay intact: %v", err)
	}
}

// TestLockRefusesPruneWithoutLock verifies that Lock does not delete the vault
// when the RAM workspace has lost its lock file.
func TestLockRefusesPruneWithoutLock(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "lock-guard.enc")
	ramPath := filepath.Join(tempDir, "ram")
	passphrase := "lock-guard-pass-2026"

	if err := Init(vaultPath, "LockGuardVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	// A directory without .grim-lock emulates a partially wiped workspace.
	if err := os.MkdirAll(ramPath, 0700); err != nil {
		t.Fatalf("failed to create ram dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ramPath, "leftover.md"), []byte("leftover"), 0600); err != nil {
		t.Fatalf("failed to write leftover: %v", err)
	}

	encPath := filepath.Join(vaultPath, "Welcome.md.age")
	if err := Lock(ramPath, vaultPath, meta.PublicKey); err == nil {
		t.Fatalf("Lock must refuse to prune without workspace lock")
	}
	if _, err := os.Stat(encPath); err != nil {
		t.Fatalf("vault file was deleted during refused lock: %v", err)
	}
	if _, err := os.Stat(ramPath); !os.IsNotExist(err) {
		t.Fatalf("unsafe workspace should have been wiped")
	}
}
