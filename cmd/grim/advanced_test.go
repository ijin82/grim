package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ijin/crypto-notes/pkg/crypto"
	"github.com/ijin/crypto-notes/pkg/ramdisk"
	"github.com/ijin/crypto-notes/pkg/vault"
)

// Test 1: Full UTF-8 & Cyrillic structure verification on vault5
func Test1_UnicodeAndCyrillicFidelity(t *testing.T) {
	vaultPath := "/home/ijin/crypto-notes-tests/vault5"
	passphrase := "qwe123QWE!@#"

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	ws, err := ramdisk.New("test1-cyrillic")
	if err != nil {
		t.Fatalf("Failed to create RAM workspace: %v", err)
	}
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock vault5 failed: %v", err)
	}

	// Verify all 3 folders exist
	expectedFolders := []string{"стихи", "цитаты", "примеры кода go"}
	for _, folder := range expectedFolders {
		p := filepath.Join(ws.Path, folder)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			t.Fatalf("Expected folder %s to exist in decrypted RAM workspace", folder)
		}
	}

	// Verify specific poem content
	pushkinFile := filepath.Join(ws.Path, "стихи", "пушкин_у_лукоморья.md")
	content, err := os.ReadFile(pushkinFile)
	if err != nil {
		t.Fatalf("Failed to read pushkin note: %v", err)
	}
	if !strings.Contains(string(content), "У лукоморья дуб зелёный") {
		t.Fatalf("Content mismatch in pushkin note: %s", string(content))
	}

	// Verify Rob Pike quote
	pikeFile := filepath.Join(ws.Path, "цитаты", "роб_пайк.md")
	contentPike, err := os.ReadFile(pikeFile)
	if err != nil {
		t.Fatalf("Failed to read rob pike note: %v", err)
	}
	if !strings.Contains(string(contentPike), "Clear is better than clever") {
		t.Fatalf("Content mismatch in pike quote: %s", string(contentPike))
	}
}

// Test 2: Binary attachments and high-entropy data (PNG, binary blobs) integrity check
func Test2_BinaryAttachmentsIntegrity(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "bin-vault.enc")
	passphrase := "binary-pass-2026"

	if err := vault.Init(vaultPath, "BinVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	// Generate 128KB random binary payload
	binaryBlob := make([]byte, 128*1024)
	if _, err := rand.Read(binaryBlob); err != nil {
		t.Fatalf("Failed to generate random binary: %v", err)
	}
	originalHash := sha256.Sum256(binaryBlob)

	ws, err := ramdisk.New("test2-bin")
	if err != nil {
		t.Fatalf("RAM ws create failed: %v", err)
	}
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Save binary attachment (e.g. image or crypto key)
	binFilePath := filepath.Join(ws.Path, "attachments", "archive.bin")
	_ = os.MkdirAll(filepath.Dir(binFilePath), 0700)
	if err := os.WriteFile(binFilePath, binaryBlob, 0600); err != nil {
		t.Fatalf("Failed to write binary file: %v", err)
	}

	// Sync & lock with Master Key
	if err := vault.Lock(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Re-unlock in a new workspace and verify sha256 checksum
	ws2, _ := ramdisk.New("test2-bin-verify")
	defer func() { _ = ws2.Destroy() }()

	if err := vault.Unlock(vaultPath, ws2.Path, meta); err != nil {
		t.Fatalf("Second unlock failed: %v", err)
	}

	restoredBinPath := filepath.Join(ws2.Path, "attachments", "archive.bin")
	restoredBlob, err := os.ReadFile(restoredBinPath)
	if err != nil {
		t.Fatalf("Failed to read restored binary: %v", err)
	}

	restoredHash := sha256.Sum256(restoredBlob)
	if restoredHash != originalHash {
		t.Fatalf("SHA256 checksum mismatch! Original: %x, Restored: %x", originalHash, restoredHash)
	}
}

// Test 3: Deep nested folder structures and deletion prune
func Test3_DeepNestedFolderAndPrune(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "deep-vault.enc")
	passphrase := "deep-pass-2026"

	if err := vault.Init(vaultPath, "DeepVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	ws, _ := ramdisk.New("test3-deep")
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Create deeply nested note: projects/backend/services/auth/tokens.md
	deepDir := filepath.Join(ws.Path, "projects", "backend", "services", "auth")
	if err := os.MkdirAll(deepDir, 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	notePath := filepath.Join(deepDir, "tokens.md")
	if err := os.WriteFile(notePath, []byte("JWT Secret Key: xyz987"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := vault.FullSyncToVault(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("FullSyncToVault failed: %v", err)
	}

	// Verify encrypted file exists at exact relative path
	encNote := filepath.Join(vaultPath, "projects", "backend", "services", "auth", "tokens.md.age")
	if _, err := os.Stat(encNote); os.IsNotExist(err) {
		t.Fatalf("Expected encrypted note at %s", encNote)
	}

	// Now delete note and verify removal
	if err := os.Remove(notePath); err != nil {
		t.Fatalf("Remove note failed: %v", err)
	}
	if err := vault.RemoveFileFromVault(notePath, ws.Path, vaultPath); err != nil {
		t.Fatalf("RemoveFileFromVault failed: %v", err)
	}

	if _, err := os.Stat(encNote); !os.IsNotExist(err) {
		t.Fatalf("Expected %s to be deleted from encrypted vault", encNote)
	}
}

// Test 4: Atomic Passphrase Re-encryption on a multi-file vault
func Test4_AtomicPassphraseMigration(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "migration-vault.enc")
	oldPass := "initial-password-123"
	newPass := "migrated-super-secure-pass-456"

	if err := vault.Init(vaultPath, "MigrationVault", oldPass); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, oldPass)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	// Add 5 sample notes using Master Key
	for i := 1; i <= 5; i++ {
		enc, _ := crypto.EncryptWithKey([]byte(fmt.Sprintf("Secret Content #%d", i)), meta.PublicKey)
		_ = os.WriteFile(filepath.Join(vaultPath, fmt.Sprintf("Note_%d.md.age", i)), enc, 0600)
	}

	// Verify wrong old passphrase fails without changing anything
	if err := vault.ChangePassphrase(vaultPath, "wrong-old-pass", newPass); err == nil {
		t.Fatalf("Expected wrong old passphrase to fail")
	}

	// Perform legitimate passphrase migration (re-encrypts only metadata!)
	if err := vault.ChangePassphrase(vaultPath, oldPass, newPass); err != nil {
		t.Fatalf("ChangePassphrase failed: %v", err)
	}

	// Verify old passphrase fails now
	if _, err := vault.VerifyPassphrase(vaultPath, oldPass); err == nil {
		t.Fatalf("Expected old passphrase to fail verification after change")
	}

	// Unlock with new passphrase and verify all 5 notes
	newMeta, err := vault.VerifyPassphrase(vaultPath, newPass)
	if err != nil {
		t.Fatalf("Verify with new pass failed: %v", err)
	}

	ws, _ := ramdisk.New("test4-migration")
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, newMeta); err != nil {
		t.Fatalf("Unlock with new passphrase failed: %v", err)
	}

	for i := 1; i <= 5; i++ {
		content, err := os.ReadFile(filepath.Join(ws.Path, fmt.Sprintf("Note_%d.md", i)))
		if err != nil {
			t.Fatalf("Failed to read migrated Note_%d: %v", i, err)
		}
		expected := fmt.Sprintf("Secret Content #%d", i)
		if string(content) != expected {
			t.Fatalf("Content mismatch for Note_%d. Got: %s, Want: %s", i, string(content), expected)
		}
	}
}

// Test 5: Live Watcher Concurrency & Rapid Edits Stress Test
func Test5_LiveWatcherConcurrencyStress(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "stress-vault.enc")
	passphrase := "stress-pass-2026"

	if err := vault.Init(vaultPath, "StressVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	ws, _ := ramdisk.New("test5-stress")
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var syncCount int
	var mu sync.Mutex

	go func() {
		_ = vault.WatchAndSync(ctx, ws.Path, vaultPath, meta.PublicKey, func(event, path string) {
			mu.Lock()
			syncCount++
			mu.Unlock()
		})
	}()

	time.Sleep(50 * time.Millisecond)

	// Concurrently write 20 different files in RAM
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fPath := filepath.Join(ws.Path, fmt.Sprintf("concurrent_note_%d.md", id))
			data := bytes.Repeat([]byte(fmt.Sprintf("Data line from worker %d\n", id)), 50)
			_ = os.WriteFile(fPath, data, 0600)
		}(i)
	}
	wg.Wait()

	// Give watcher time to debounce and encrypt
	time.Sleep(300 * time.Millisecond)

	// Perform full lock
	cancel()
	if err := vault.Lock(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Verify all 20 files were successfully encrypted to disk
	for i := 1; i <= 20; i++ {
		encPath := filepath.Join(vaultPath, fmt.Sprintf("concurrent_note_%d.md.age", i))
		if _, err := os.Stat(encPath); os.IsNotExist(err) {
			t.Fatalf("Expected encrypted note %s to exist after concurrent writes", encPath)
		}
	}
}
