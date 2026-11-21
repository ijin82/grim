package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ijin82/grim/pkg/crypto"
	"github.com/ijin82/grim/pkg/ramdisk"
)

func TestShouldSyncVaultFile(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		want    bool
	}{
		{name: "regular note", relPath: "notes/note.md", want: true},
		{name: "obsidian config", relPath: filepath.Join(".obsidian", "app.json"), want: true},
		{name: "root gitignore", relPath: ".gitignore", want: true},
		{name: "root gitattributes", relPath: ".gitattributes", want: true},
		{name: "session lock", relPath: ramdisk.LockFileName, want: false},
		{name: "git directory", relPath: filepath.Join(".git", "config"), want: false},
		{name: "hidden root file", relPath: ".secret", want: false},
		{name: "hidden nested file", relPath: filepath.Join("notes", ".secret"), want: false},
		{name: "hidden directory", relPath: filepath.Join(".trash", "note.md"), want: false},
		{name: "temporary file", relPath: "note.md.tmp", want: false},
		{name: "new file", relPath: "note.md.new", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSyncVaultFile(tc.relPath); got != tc.want {
				t.Fatalf("shouldSyncVaultFile(%q) = %v, want %v", tc.relPath, got, tc.want)
			}
		})
	}
}

func TestRootDotfileSyncRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "hidden-vault.enc")
	passphrase := "hidden-pass-2026"

	if err := Init(vaultPath, "HiddenVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	ramPath := filepath.Join(tempDir, "ram")
	createTestWorkspace(t, ramPath)
	if err := Unlock(vaultPath, ramPath, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	gitignorePath := filepath.Join(ramPath, ".gitignore")
	const gitignoreContent = "*.tmp\n"
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0600); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}
	if err := SyncFileToVault(gitignorePath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault(.gitignore) failed: %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(vaultPath, ".gitignore"))
	if err != nil {
		t.Fatalf("expected plaintext .gitignore in vault: %v", err)
	}
	if string(stored) != gitignoreContent {
		t.Fatalf(".gitignore in vault = %q, want %q", stored, gitignoreContent)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".gitignore.age")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore must never be encrypted")
	}

	gitConfigPath := filepath.Join(ramPath, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(gitConfigPath), 0700); err != nil {
		t.Fatalf("failed to create .git: %v", err)
	}
	if err := os.WriteFile(gitConfigPath, []byte("[core]\n"), 0600); err != nil {
		t.Fatalf("failed to write .git/config: %v", err)
	}
	if err := SyncFileToVault(gitConfigPath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault(.git/config) failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git internals must never be written to the vault")
	}

	hiddenPath := filepath.Join(ramPath, ".secret")
	if err := os.WriteFile(hiddenPath, []byte("secret"), 0600); err != nil {
		t.Fatalf("failed to write hidden file: %v", err)
	}
	if err := SyncFileToVault(hiddenPath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault(.secret) failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".secret.age")); !os.IsNotExist(err) {
		t.Fatalf("unexpected .secret.age in vault")
	}

	obsidianPath := filepath.Join(ramPath, ".obsidian", "app.json")
	if err := os.MkdirAll(filepath.Dir(obsidianPath), 0700); err != nil {
		t.Fatalf("failed to create .obsidian: %v", err)
	}
	if err := os.WriteFile(obsidianPath, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatalf("failed to write obsidian config: %v", err)
	}
	if err := SyncFileToVault(obsidianPath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault(.obsidian/app.json) failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".obsidian", "app.json.age")); err != nil {
		t.Fatalf("expected .obsidian/app.json.age in vault: %v", err)
	}

	lockPath := filepath.Join(ramPath, ramdisk.LockFileName)
	if err := SyncFileToVault(lockPath, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault(.grim-lock) failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ramdisk.LockFileName+AgeExt)); !os.IsNotExist(err) {
		t.Fatalf("session lock must never be written to the vault")
	}

	// The plaintext .gitignore must decrypt/copy back to the original content.
	freshRAM := filepath.Join(tempDir, "ram-fresh")
	createTestWorkspace(t, freshRAM)
	if err := Unlock(vaultPath, freshRAM, meta); err != nil {
		t.Fatalf("second Unlock failed: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(freshRAM, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read restored .gitignore: %v", err)
	}
	if string(restored) != gitignoreContent {
		t.Fatalf(".gitignore content mismatch: got %q, want %q", restored, gitignoreContent)
	}
}

func TestLegacyGitignoreAgeIsMigrated(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "legacy-gitignore.enc")
	passphrase := "legacy-gitignore-pass"

	if err := Init(vaultPath, "LegacyGitignoreVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	ramPath := filepath.Join(tempDir, "ram")
	createTestWorkspace(t, ramPath)
	if err := Unlock(vaultPath, ramPath, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Simulate a vault produced by a version that encrypted .gitignore.
	legacyEncrypted, err := crypto.EncryptWithKey([]byte("old-ignore\n"), meta.PublicKey)
	if err != nil {
		t.Fatalf("failed to encrypt legacy .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultPath, ".gitignore.age"), legacyEncrypted, 0600); err != nil {
		t.Fatalf("failed to write legacy .gitignore.age: %v", err)
	}

	const current = "new-ignore\n"
	ramGitignore := filepath.Join(ramPath, ".gitignore")
	if err := os.WriteFile(ramGitignore, []byte(current), 0600); err != nil {
		t.Fatalf("failed to write current .gitignore: %v", err)
	}
	if err := SyncFileToVault(ramGitignore, ramPath, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("SyncFileToVault failed: %v", err)
	}

	stored, err := os.ReadFile(filepath.Join(vaultPath, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read plaintext .gitignore: %v", err)
	}
	if string(stored) != current {
		t.Fatalf("plaintext .gitignore = %q, want %q", stored, current)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".gitignore.age")); !os.IsNotExist(err) {
		t.Fatalf("legacy .gitignore.age was not removed")
	}
}

func TestUnlockMigratesLegacyGitignoreAge(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "unlock-legacy.enc")
	passphrase := "unlock-legacy-pass"

	if err := Init(vaultPath, "UnlockLegacyVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	meta, err := VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	const content = "legacy-ignore\n"
	encrypted, err := crypto.EncryptWithKey([]byte(content), meta.PublicKey)
	if err != nil {
		t.Fatalf("failed to encrypt legacy .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultPath, ".gitignore.age"), encrypted, 0600); err != nil {
		t.Fatalf("failed to write legacy .gitignore.age: %v", err)
	}

	ramPath := filepath.Join(tempDir, "ram")
	createTestWorkspace(t, ramPath)
	if err := Unlock(vaultPath, ramPath, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	vaultIgnore, err := os.ReadFile(filepath.Join(vaultPath, ".gitignore"))
	if err != nil {
		t.Fatalf("vault .gitignore was not migrated: %v", err)
	}
	if string(vaultIgnore) != content {
		t.Fatalf("vault .gitignore = %q, want %q", vaultIgnore, content)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".gitignore.age")); !os.IsNotExist(err) {
		t.Fatalf("legacy .gitignore.age was not removed during unlock")
	}

	ramIgnore, err := os.ReadFile(filepath.Join(ramPath, ".gitignore"))
	if err != nil {
		t.Fatalf("RAM .gitignore is missing: %v", err)
	}
	if string(ramIgnore) != content {
		t.Fatalf("RAM .gitignore = %q, want %q", ramIgnore, content)
	}
}
