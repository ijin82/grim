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

	"github.com/ijin82/grim/pkg/crypto"
	"github.com/ijin82/grim/pkg/ramdisk"
	"github.com/ijin82/grim/pkg/vault"
)

// Test 1: Full UTF-8 & Cyrillic structure verification
func Test1_UnicodeAndCyrillicFidelity(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "cyrillic-vault.enc")
	passphrase := "qwe123QWE!@#"

	if err := vault.Init(vaultPath, "CyrillicVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("VerifyPassphrase failed: %v", err)
	}

	// Create Cyrillic test notes
	_ = os.MkdirAll(filepath.Join(vaultPath, "стихи"), 0700)
	_ = os.MkdirAll(filepath.Join(vaultPath, "цитаты"), 0700)
	_ = os.MkdirAll(filepath.Join(vaultPath, "примеры кода go"), 0700)

	pushkinText := "# У лукоморья дуб зелёный\n*Александр Пушкин*"
	encPushkin, _ := crypto.EncryptWithKey([]byte(pushkinText), meta.PublicKey)
	_ = os.WriteFile(filepath.Join(vaultPath, "стихи", "пушкин_у_лукоморья.md.age"), encPushkin, 0600)

	pikeText := "# Роб Пайк\n> Clear is better than clever."
	encPike, _ := crypto.EncryptWithKey([]byte(pikeText), meta.PublicKey)
	_ = os.WriteFile(filepath.Join(vaultPath, "цитаты", "роб_пайк.md.age"), encPike, 0600)

	ws, err := ramdisk.New("test1-cyrillic")
	if err != nil {
		t.Fatalf("Failed to create RAM workspace: %v", err)
	}
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock vault failed: %v", err)
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

	// Create a folder with multiple notes: poems/verse1.md, poems/verse2.md
	poemsDir := filepath.Join(ws.Path, "стихи")
	_ = os.MkdirAll(poemsDir, 0700)
	_ = os.WriteFile(filepath.Join(poemsDir, "verse1.md"), []byte("Строка 1"), 0600)
	_ = os.WriteFile(filepath.Join(poemsDir, "verse2.md"), []byte("Строка 2"), 0600)
	if err := vault.FullSyncToVault(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("FullSyncToVault failed: %v", err)
	}

	encPoem1 := filepath.Join(vaultPath, "стихи", "verse1.md.age")
	if _, err := os.Stat(encPoem1); os.IsNotExist(err) {
		t.Fatalf("Expected encrypted poem at %s", encPoem1)
	}

	// Delete entire folder "стихи" in RAM and remove from vault
	_ = os.RemoveAll(poemsDir)
	if err := vault.RemoveFileFromVault(poemsDir, ws.Path, vaultPath); err != nil {
		t.Fatalf("RemoveFileFromVault for directory failed: %v", err)
	}

	encPoemsDir := filepath.Join(vaultPath, "стихи")
	if _, err := os.Stat(encPoemsDir); !os.IsNotExist(err) {
		t.Fatalf("Expected directory %s to be deleted from encrypted vault", encPoemsDir)
	}

	// Re-lock and unlock into a new workspace, verifying deleted folder does NOT resurrect
	if err := vault.Lock(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	wsReopened, _ := ramdisk.New("test3-reopen")
	defer func() { _ = wsReopened.Destroy() }()

	if err := vault.Unlock(vaultPath, wsReopened.Path, meta); err != nil {
		t.Fatalf("Reopen unlock failed: %v", err)
	}

	reopenedPoemsDir := filepath.Join(wsReopened.Path, "стихи")
	if _, err := os.Stat(reopenedPoemsDir); !os.IsNotExist(err) {
		t.Fatalf("Bug detected: deleted folder %s resurrected upon reopen!", reopenedPoemsDir)
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
		_ = vault.WatchAndSync(ctx, ws.Path, vaultPath, meta.PublicKey, nil, func(event, path string) {
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

// Test 6: Folder Copying and Nested Tree Synchronization
func Test6_FolderCopyAndNestedSync(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "copy-vault.enc")
	passphrase := "copy-pass-2026"

	if err := vault.Init(vaultPath, "CopyVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	ws, _ := ramdisk.New("test6-copy")
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = vault.WatchAndSync(ctx, ws.Path, vaultPath, meta.PublicKey, nil, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create source tree: projects/alpha/docs/spec.md
	srcDir := filepath.Join(ws.Path, "projects", "alpha", "docs")
	_ = os.MkdirAll(srcDir, 0700)
	_ = os.WriteFile(filepath.Join(srcDir, "spec.md"), []byte("# Spec Alpha v1.0"), 0600)

	time.Sleep(100 * time.Millisecond)

	// Simulate copying "alpha" folder to "beta": projects/beta/docs/spec.md
	dstDir := filepath.Join(ws.Path, "projects", "beta", "docs")
	_ = os.MkdirAll(dstDir, 0700)
	_ = os.WriteFile(filepath.Join(dstDir, "spec.md"), []byte("# Spec Beta v1.0"), 0600)

	time.Sleep(200 * time.Millisecond)

	// Lock and reconcile
	cancel()
	if err := vault.Lock(ws.Path, vaultPath, meta.PublicKey); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Verify both encrypted trees exist on disk
	encAlpha := filepath.Join(vaultPath, "projects", "alpha", "docs", "spec.md.age")
	encBeta := filepath.Join(vaultPath, "projects", "beta", "docs", "spec.md.age")

	if _, err := os.Stat(encAlpha); os.IsNotExist(err) {
		t.Fatalf("Expected encrypted alpha at %s", encAlpha)
	}
	if _, err := os.Stat(encBeta); os.IsNotExist(err) {
		t.Fatalf("Expected encrypted beta at %s", encBeta)
	}

	// Reopen in a new workspace and verify content fidelity
	wsReopen, _ := ramdisk.New("test6-reopen")
	defer func() { _ = wsReopen.Destroy() }()

	if err := vault.Unlock(vaultPath, wsReopen.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	betaContent, err := os.ReadFile(filepath.Join(wsReopen.Path, "projects", "beta", "docs", "spec.md"))
	if err != nil {
		t.Fatalf("Failed to read beta spec in reopened vault: %v", err)
	}
	if string(betaContent) != "# Spec Beta v1.0" {
		t.Fatalf("Content mismatch! Got: %s, Want: # Spec Beta v1.0", string(betaContent))
	}
}

// Test 7: Redundant Saves Deduplication via Content Hashing
func Test7_ContentHashDeduplication(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "dedup-vault.enc")
	passphrase := "dedup-pass-2026"

	if err := vault.Init(vaultPath, "DedupVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	ws, _ := ramdisk.New("test7-dedup")
	defer func() { _ = ws.Destroy() }()

	if err := vault.Unlock(vaultPath, ws.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var syncEvents int
	var mu sync.Mutex

	go func() {
		_ = vault.WatchAndSync(ctx, ws.Path, vaultPath, meta.PublicKey, nil, func(event, path string) {
			mu.Lock()
			syncEvents++
			mu.Unlock()
		})
	}()

	time.Sleep(50 * time.Millisecond)

	testFile := filepath.Join(ws.Path, "autosave.md")
	content := []byte("Initial text payload")
	_ = os.WriteFile(testFile, content, 0600)

	// Wait for first sync
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	initialSyncs := syncEvents
	mu.Unlock()

	if initialSyncs == 0 {
		t.Fatalf("Expected initial sync event for new file")
	}

	// Now simulate Obsidian or editor repeatedly saving the identical content (e.g. 10 times)
	for i := 0; i < 10; i++ {
		_ = os.WriteFile(testFile, content, 0600)
		time.Sleep(20 * time.Millisecond)
	}

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	redundantSyncs := syncEvents
	mu.Unlock()

	if redundantSyncs > initialSyncs {
		t.Fatalf("Redundant sync detected! Content was identical, but got %d additional sync events", redundantSyncs-initialSyncs)
	}

	// Now actually change the content
	_ = os.WriteFile(testFile, []byte("Updated text payload with changes"), 0600)
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	updatedSyncs := syncEvents
	mu.Unlock()

	if updatedSyncs <= initialSyncs {
		t.Fatalf("Expected sync event after real content change, but got none")
	}
}

// Test 8: Exit & Lock without modifications produces ZERO changes to .age files on disk
func Test8_ExitWithoutChangesProducesZeroGitDiff(t *testing.T) {
	tempDir := t.TempDir()
	vaultPath := filepath.Join(tempDir, "git-diff-vault.enc")
	passphrase := "git-diff-pass-2026"

	if err := vault.Init(vaultPath, "GitDiffVault", passphrase); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	meta, err := vault.VerifyPassphrase(vaultPath, passphrase)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	// 1. Create 5 initial notes in vault
	wsSetup, _ := ramdisk.New("test8-setup")
	_ = vault.Unlock(vaultPath, wsSetup.Path, meta)
	for i := 1; i <= 5; i++ {
		_ = os.WriteFile(filepath.Join(wsSetup.Path, fmt.Sprintf("note_%d.md", i)), []byte(fmt.Sprintf("Static Note Content %d", i)), 0600)
	}
	_ = vault.Lock(wsSetup.Path, vaultPath, meta.PublicKey)
	_ = wsSetup.Destroy()

	// 2. Record exact SHA-256 of encrypted .age files on disk
	initialEncHashes := make(map[string][32]byte)
	for i := 1; i <= 5; i++ {
		encFile := filepath.Join(vaultPath, fmt.Sprintf("note_%d.md.age", i))
		data, err := os.ReadFile(encFile)
		if err != nil {
			t.Fatalf("Failed to read initial enc file %s: %v", encFile, err)
		}
		initialEncHashes[encFile] = sha256.Sum256(data)
	}

	// 3. Open session (simulate user opening grim, reading notes, and editing ONLY note_3.md)
	wsSession, _ := ramdisk.New("test8-session")
	defer func() { _ = wsSession.Destroy() }()

	if err := vault.Unlock(vaultPath, wsSession.Path, meta); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	tracker := vault.NewSyncTracker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = vault.WatchAndSync(ctx, wsSession.Path, vaultPath, meta.PublicKey, tracker, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	// Modify ONLY note_3.md
	_ = os.WriteFile(filepath.Join(wsSession.Path, "note_3.md"), []byte("Modified Note Content 3"), 0600)
	time.Sleep(150 * time.Millisecond)

	// 4. Close session and Lock
	cancel()
	if err := vault.Lock(wsSession.Path, vaultPath, meta.PublicKey, tracker); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// 5. Verify: untouched notes (1, 2, 4, 5) have EXACT SAME BYTES on disk (0 diff), while note 3 changed
	for i := 1; i <= 5; i++ {
		encFile := filepath.Join(vaultPath, fmt.Sprintf("note_%d.md.age", i))
		data, err := os.ReadFile(encFile)
		if err != nil {
			t.Fatalf("Failed to read post-lock enc file %s: %v", encFile, err)
		}
		currentHash := sha256.Sum256(data)
		prevHash := initialEncHashes[encFile]

		if i == 3 {
			if currentHash == prevHash {
				t.Fatalf("Expected note_3.md.age to be updated, but ciphertext was identical")
			}
		} else {
			if currentHash != prevHash {
				t.Fatalf("Bug: untouched file note_%d.md.age was re-encrypted and changed on disk! Diff detected in git!", i)
			}
		}
	}
}
