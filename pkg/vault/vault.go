package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ijin/crypto-notes/pkg/crypto"
	"github.com/ijin/crypto-notes/pkg/ramdisk"
)

const (
	MetaFileName = ".vault-meta.age"
	AgeExt       = ".age"
)

type Meta struct {
	Version    int       `json:"version"`
	VaultName  string    `json:"vault_name"`
	CreatedAt  time.Time `json:"created_at"`
	PrivateKey string    `json:"private_key"` // X25519 Identity
	PublicKey  string    `json:"public_key"`  // X25519 Recipient
}

// Init creates a new encrypted vault at vaultPath with a random Master Key protected by passphrase.
func Init(vaultPath string, vaultName string, passphrase string) error {
	if passphrase == "" {
		return fmt.Errorf("passphrase cannot be empty")
	}

	if err := os.MkdirAll(vaultPath, 0700); err != nil {
		return fmt.Errorf("failed to create vault directory at %s: %w", vaultPath, err)
	}

	// Generate random 256-bit Master Key
	privKey, pubKey, err := crypto.GenerateMasterKey()
	if err != nil {
		return fmt.Errorf("failed to generate master key: %w", err)
	}

	meta := Meta{
		Version:    2,
		VaultName:  vaultName,
		CreatedAt:  time.Now(),
		PrivateKey: privKey,
		PublicKey:  pubKey,
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}

	encMeta, err := crypto.Encrypt(metaBytes, passphrase)
	if err != nil {
		return fmt.Errorf("failed to encrypt vault metadata: %w", err)
	}

	metaPath := filepath.Join(vaultPath, MetaFileName)
	if err := os.WriteFile(metaPath, encMeta, 0600); err != nil {
		return fmt.Errorf("failed to write meta file: %w", err)
	}

	// Create initial Welcome note encrypted with fast Master Key
	welcomeNote := fmt.Sprintf("# Welcome to %s\n\nThis vault is encrypted using `age` X25519 Master Key Architecture.\nAll your notes and attachments are automatically encrypted on save in microseconds.\n", vaultName)
	encWelcome, err := crypto.EncryptWithKey([]byte(welcomeNote), pubKey)
	if err == nil {
		_ = os.WriteFile(filepath.Join(vaultPath, "Welcome.md"+AgeExt), encWelcome, 0600)
	}

	return nil
}

// VerifyPassphrase checks if the passphrase can unlock the Master Key in .vault-meta.age.
func VerifyPassphrase(vaultPath string, passphrase string) (*Meta, error) {
	metaPath := filepath.Join(vaultPath, MetaFileName)
	encData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("vault metadata not found at %s: %w", metaPath, err)
	}

	decBytes, err := crypto.Decrypt(encData, passphrase)
	if err != nil {
		return nil, fmt.Errorf("invalid passphrase or corrupted vault")
	}

	var meta Meta
	if err := json.Unmarshal(decBytes, &meta); err != nil {
		return nil, fmt.Errorf("invalid metadata format: %w", err)
	}

	return &meta, nil
}

// Unlock decrypts all files from vaultPath into ramPath using the fast Master Key.
func Unlock(vaultPath string, ramPath string, meta *Meta) error {
	if meta == nil || meta.PrivateKey == "" {
		return fmt.Errorf("invalid master key metadata")
	}

	err := filepath.WalkDir(vaultPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(vaultPath, path)
		if err != nil {
			return err
		}

		if relPath == "." || relPath == MetaFileName {
			return nil
		}

		// Skip temporary files
		if strings.HasSuffix(relPath, ".tmp") || strings.HasSuffix(relPath, ".new") {
			return nil
		}

		if d.IsDir() {
			targetDir := filepath.Join(ramPath, relPath)
			return os.MkdirAll(targetDir, 0700)
		}

		// Decrypt file with Master Key in microseconds
		if strings.HasSuffix(relPath, AgeExt) {
			decRelPath := strings.TrimSuffix(relPath, AgeExt)
			targetFilePath := filepath.Join(ramPath, decRelPath)

			if err := os.MkdirAll(filepath.Dir(targetFilePath), 0700); err != nil {
				return err
			}

			return crypto.DecryptFileWithKey(path, targetFilePath, meta.PrivateKey)
		}

		// Copy unencrypted file (e.g. .gitignore)
		targetFilePath := filepath.Join(ramPath, relPath)
		return copyFile(path, targetFilePath)
	})

	return err
}

// SyncFileToVault encrypts a single file from RAM into the vault storage using the Master Key.
func SyncFileToVault(ramFilePath string, ramRoot string, vaultRoot string, pubKey string) error {
	relPath, err := filepath.Rel(ramRoot, ramFilePath)
	if err != nil {
		return err
	}

	if relPath == "." || strings.HasPrefix(relPath, ".") && !strings.HasPrefix(relPath, ".obsidian") {
		// Ignore hidden files except .obsidian settings
		if strings.HasPrefix(relPath, ".") && relPath != ".obsidian" && !strings.HasPrefix(relPath, ".obsidian/") {
			return nil
		}
	}

	// Target encrypted file path in vault
	vaultFilePath := filepath.Join(vaultRoot, relPath+AgeExt)
	if err := os.MkdirAll(filepath.Dir(vaultFilePath), 0700); err != nil {
		return err
	}

	return crypto.EncryptFileWithKey(ramFilePath, vaultFilePath, pubKey)
}

// RemoveFileFromVault removes the encrypted counterpart of a deleted RAM file.
func RemoveFileFromVault(ramFilePath string, ramRoot string, vaultRoot string) error {
	relPath, err := filepath.Rel(ramRoot, ramFilePath)
	if err != nil {
		return err
	}

	vaultFilePath := filepath.Join(vaultRoot, relPath+AgeExt)
	if err := os.Remove(vaultFilePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	vaultDir := filepath.Dir(vaultFilePath)
	_ = removeEmptyDirs(vaultDir, vaultRoot)

	return nil
}

// FullSyncToVault walks ramPath and encrypts all files to vaultPath using Master Key.
func FullSyncToVault(ramPath string, vaultPath string, pubKey string) error {
	return filepath.WalkDir(ramPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		return SyncFileToVault(path, ramPath, vaultPath, pubKey)
	})
}

// WatchAndSync starts an fsnotify watcher on ramRoot and syncs changes to vaultRoot in real-time.
func WatchAndSync(ctx context.Context, ramRoot string, vaultRoot string, pubKey string, onSync func(event string, path string)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	defer watcher.Close()

	// Add all subdirectories to watcher
	_ = filepath.WalkDir(ramRoot, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d != nil && d.IsDir() {
			_ = watcher.Add(path)
		}
		return nil
	})

	var mu sync.Mutex
	debounceMap := make(map[string]*time.Timer)

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// If a new directory is created, watch it
			if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
				if event.Has(fsnotify.Create) {
					_ = watcher.Add(event.Name)
				}
				continue
			}

			filePath := event.Name
			mu.Lock()
			if t, exists := debounceMap[filePath]; exists {
				t.Stop()
			}

			debounceMap[filePath] = time.AfterFunc(50*time.Millisecond, func() {
				mu.Lock()
				delete(debounceMap, filePath)
				mu.Unlock()

				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if _, err := os.Stat(filePath); err == nil {
						if err := SyncFileToVault(filePath, ramRoot, vaultRoot, pubKey); err == nil {
							if onSync != nil {
								rel, _ := filepath.Rel(ramRoot, filePath)
								onSync("SYNC", rel)
							}
						}
					}
				} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					if err := RemoveFileFromVault(filePath, ramRoot, vaultRoot); err == nil {
						if onSync != nil {
							rel, _ := filepath.Rel(ramRoot, filePath)
							onSync("REMOVE", rel)
						}
					}
				}
			})
			mu.Unlock()

		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
		}
	}
}

// ChangePassphrase re-encrypts ONLY the .vault-meta.age with a new passphrase.
func ChangePassphrase(vaultPath string, oldPassphrase string, newPassphrase string) error {
	if newPassphrase == "" {
		return fmt.Errorf("new passphrase cannot be empty")
	}

	meta, err := VerifyPassphrase(vaultPath, oldPassphrase)
	if err != nil {
		return fmt.Errorf("current passphrase verification failed: %w", err)
	}

	// Re-marshal metadata
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}

	// Encrypt with new passphrase
	encMeta, err := crypto.Encrypt(metaBytes, newPassphrase)
	if err != nil {
		return fmt.Errorf("failed to encrypt new metadata: %w", err)
	}

	metaPath := filepath.Join(vaultPath, MetaFileName)
	tmpPath := metaPath + ".new"
	if err := os.WriteFile(tmpPath, encMeta, 0600); err != nil {
		return fmt.Errorf("failed to write new metadata: %w", err)
	}

	if err := os.Rename(tmpPath, metaPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to commit new metadata: %w", err)
	}

	return nil
}

// Lock performs a final full sync from RAM to Vault and securely destroys the RAM workspace.
func Lock(ramPath string, vaultPath string, pubKey string) error {
	if _, err := os.Stat(ramPath); os.IsNotExist(err) {
		return nil
	}

	// 1. Final sync to make sure no uncommitted edits remain
	if err := FullSyncToVault(ramPath, vaultPath, pubKey); err != nil {
		return fmt.Errorf("failed to complete final sync before locking: %w", err)
	}

	// 2. Securely wipe RAM workspace
	if err := ramdisk.WipeAndRemove(ramPath); err != nil {
		return fmt.Errorf("failed to securely wipe RAM workspace: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func removeEmptyDirs(dir string, stopAt string) error {
	if dir == stopAt || dir == "." || dir == "/" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
		return removeEmptyDirs(filepath.Dir(dir), stopAt)
	}
	return nil
}
