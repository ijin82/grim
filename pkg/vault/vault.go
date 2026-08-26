package vault

import (
	"context"
	"crypto/sha256"
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
	"github.com/ijin82/grim/pkg/crypto"
	"github.com/ijin82/grim/pkg/ramdisk"
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

		// Skip .git directory entirely (git repo stays on physical disk, never copied to RAM)
		if relPath == ".git" || strings.HasPrefix(relPath, ".git/") || strings.HasPrefix(relPath, ".git\\") {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
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

// SyncTracker tracks the SHA-256 content hashes of files in RAM to avoid redundant re-encryptions.
type SyncTracker struct {
	mu     sync.Mutex
	hashes map[string][32]byte
}

// NewSyncTracker initializes a thread-safe content hash tracker.
func NewSyncTracker() *SyncTracker {
	return &SyncTracker{
		hashes: make(map[string][32]byte),
	}
}

func (t *SyncTracker) Get(relPath string) ([32]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.hashes[relPath]
	return h, ok
}

func (t *SyncTracker) Set(relPath string, hash [32]byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hashes[relPath] = hash
}

func (t *SyncTracker) Delete(relPath string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.hashes, relPath)
}

// SyncFileToVault encrypts a single file from RAM into the vault storage using the Master Key.
func SyncFileToVault(ramFilePath string, ramRoot string, vaultRoot string, pubKey string) error {
	_, err := syncFileWithTracker(ramFilePath, ramRoot, vaultRoot, pubKey, nil)
	return err
}

// syncFileWithTracker returns true if file was actually encrypted and written to disk, or false if skipped due to identical content hash.
func syncFileWithTracker(ramFilePath string, ramRoot string, vaultRoot string, pubKey string, tracker *SyncTracker) (bool, error) {
	relPath, err := filepath.Rel(ramRoot, ramFilePath)
	if err != nil {
		return false, err
	}

	if relPath == "." || strings.HasPrefix(relPath, ".") && !strings.HasPrefix(relPath, ".obsidian") {
		// Ignore hidden files except .obsidian settings
		if strings.HasPrefix(relPath, ".") && relPath != ".obsidian" && !strings.HasPrefix(relPath, ".obsidian/") {
			return false, nil
		}
	}

	data, err := os.ReadFile(ramFilePath)
	if err != nil {
		return false, err
	}

	currentHash := sha256.Sum256(data)
	vaultFilePath := filepath.Join(vaultRoot, relPath+AgeExt)

	// Deduplication: if content hash matches existing state and target .age exists, skip re-encryption!
	if tracker != nil {
		if prevHash, exists := tracker.Get(relPath); exists && prevHash == currentHash {
			if _, err := os.Stat(vaultFilePath); err == nil {
				return false, nil
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(vaultFilePath), 0700); err != nil {
		return false, err
	}

	if err := crypto.EncryptFileWithKey(ramFilePath, vaultFilePath, pubKey); err != nil {
		return false, err
	}

	if tracker != nil {
		tracker.Set(relPath, currentHash)
	}

	return true, nil
}

// RemoveFileFromVault removes the encrypted counterpart of a deleted RAM file or directory.
func RemoveFileFromVault(ramFilePath string, ramRoot string, vaultRoot string) error {
	relPath, err := filepath.Rel(ramRoot, ramFilePath)
	if err != nil {
		return err
	}

	if relPath == "." || relPath == "" || relPath == MetaFileName || relPath == ".git" {
		return nil
	}

	// 1. If a directory with this relative path exists in vault, remove it and all its contents
	vaultDirPath := filepath.Join(vaultRoot, relPath)
	if fi, err := os.Stat(vaultDirPath); err == nil && fi.IsDir() {
		_ = os.RemoveAll(vaultDirPath)
	}

	// 2. If an encrypted file with this relative path exists in vault, remove it
	vaultAgePath := filepath.Join(vaultRoot, relPath+AgeExt)
	if err := os.Remove(vaultAgePath); err != nil && !os.IsNotExist(err) {
		// Ignore error if it didn't exist as a direct file
	}

	// 3. If an unencrypted file (e.g. .gitignore) exists in vault, remove it
	_ = os.Remove(vaultDirPath)

	// 4. Clean up empty parent directories up to vaultRoot
	vaultParent := filepath.Dir(vaultAgePath)
	_ = removeEmptyDirs(vaultParent, vaultRoot)

	return nil
}

// PruneDeletedFromVault walks vaultRoot and removes any files or folders that no longer exist in ramRoot.
func PruneDeletedFromVault(ramRoot string, vaultRoot string) error {
	return filepath.WalkDir(vaultRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}

		relPath, err := filepath.Rel(vaultRoot, path)
		if err != nil || relPath == "." || relPath == "" || relPath == MetaFileName {
			return nil
		}

		// Skip .git repository
		if relPath == ".git" || strings.HasPrefix(relPath, ".git/") || strings.HasPrefix(relPath, ".git\\") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			// If directory does not exist in RAM, remove entire directory tree in vault
			ramDirPath := filepath.Join(ramRoot, relPath)
			if _, err := os.Stat(ramDirPath); os.IsNotExist(err) {
				_ = os.RemoveAll(path)
				return filepath.SkipDir
			}
			return nil
		}

		// If it's a file
		if strings.HasSuffix(relPath, AgeExt) {
			decRelPath := strings.TrimSuffix(relPath, AgeExt)
			ramFilePath := filepath.Join(ramRoot, decRelPath)
			if _, err := os.Stat(ramFilePath); os.IsNotExist(err) {
				_ = os.Remove(path)
			}
		} else {
			// Non-age file
			ramFilePath := filepath.Join(ramRoot, relPath)
			if _, err := os.Stat(ramFilePath); os.IsNotExist(err) {
				_ = os.Remove(path)
			}
		}

		return nil
	})
}

// FullSyncToVault walks ramPath and encrypts all files to vaultPath using Master Key.
func FullSyncToVault(ramPath string, vaultPath string, pubKey string, trackers ...*SyncTracker) error {
	var tracker *SyncTracker
	if len(trackers) > 0 {
		tracker = trackers[0]
	}

	return filepath.WalkDir(ramPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}

		relPath, _ := filepath.Rel(ramPath, path)
		if relPath == ".git" || strings.HasPrefix(relPath, ".git/") || strings.HasPrefix(relPath, ".git\\") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		_, err = syncFileWithTracker(path, ramPath, vaultPath, pubKey, tracker)
		return err
	})
}

// WatchAndSync starts an fsnotify watcher on ramRoot and syncs changes to vaultRoot in real-time.
func WatchAndSync(ctx context.Context, ramRoot string, vaultRoot string, pubKey string, tracker *SyncTracker, onSync func(event string, path string)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	defer watcher.Close()

	if tracker == nil {
		tracker = NewSyncTracker()
	}

	// Add all subdirectories to watcher and seed initial SHA-256 content hashes
	_ = filepath.WalkDir(ramRoot, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d != nil {
			relPath, _ := filepath.Rel(ramRoot, path)
			if relPath == ".git" || strings.HasPrefix(relPath, ".git/") || strings.HasPrefix(relPath, ".git\\") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				_ = watcher.Add(path)
			} else {
				if data, err := os.ReadFile(path); err == nil {
					tracker.Set(relPath, sha256.Sum256(data))
				}
			}
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

			// If a new directory is created/renamed, watch it recursively and sync any files inside
			if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
				if event.Has(fsnotify.Create) {
					_ = filepath.WalkDir(event.Name, func(p string, d fs.DirEntry, err error) error {
						if err == nil && d != nil {
							relPath, _ := filepath.Rel(ramRoot, p)
							if relPath == ".git" || strings.HasPrefix(relPath, ".git/") || strings.HasPrefix(relPath, ".git\\") {
								if d.IsDir() {
									return filepath.SkipDir
								}
								return nil
							}
							if d.IsDir() {
								_ = watcher.Add(p)
							} else {
								written, err := syncFileWithTracker(p, ramRoot, vaultRoot, pubKey, tracker)
								if err == nil && written {
									if onSync != nil {
										rel, _ := filepath.Rel(ramRoot, p)
										onSync("SYNC", rel)
									}
								}
							}
						}
						return nil
					})
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
						written, err := syncFileWithTracker(filePath, ramRoot, vaultRoot, pubKey, tracker)
						if err == nil && written {
							if onSync != nil {
								rel, _ := filepath.Rel(ramRoot, filePath)
								onSync("SYNC", rel)
							}
						}
					}
				} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					rel, _ := filepath.Rel(ramRoot, filePath)
					tracker.Delete(rel)
					if err := RemoveFileFromVault(filePath, ramRoot, vaultRoot); err == nil {
						if onSync != nil {
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

// Lock performs a final full sync from RAM to Vault, prunes deleted items, and securely destroys the RAM workspace.
func Lock(ramPath string, vaultPath string, pubKey string, trackers ...*SyncTracker) error {
	if _, err := os.Stat(ramPath); os.IsNotExist(err) {
		return nil
	}

	var tracker *SyncTracker
	if len(trackers) > 0 {
		tracker = trackers[0]
	}

	// 1. Final sync to make sure no uncommitted edits remain (skips untouched files)
	if err := FullSyncToVault(ramPath, vaultPath, pubKey, tracker); err != nil {
		return fmt.Errorf("failed to complete final sync before locking: %w", err)
	}

	// 2. Prune any files or directories in vault that were deleted in RAM
	if err := PruneDeletedFromVault(ramPath, vaultPath); err != nil {
		return fmt.Errorf("failed to prune deleted files before locking: %w", err)
	}

	// 3. Clean empty directories in vault
	_ = cleanEmptyVaultDirs(vaultPath)

	// 4. Securely wipe RAM workspace
	if err := ramdisk.WipeAndRemove(ramPath); err != nil {
		return fmt.Errorf("failed to securely wipe RAM workspace: %w", err)
	}

	return nil
}

func cleanEmptyVaultDirs(vaultRoot string) error {
	return filepath.WalkDir(vaultRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() || path == vaultRoot {
			return nil
		}
		relPath, _ := filepath.Rel(vaultRoot, path)
		if relPath == ".git" || strings.HasPrefix(relPath, ".git/") || strings.HasPrefix(relPath, ".git\\") {
			return filepath.SkipDir
		}
		_ = removeEmptyDirs(path, vaultRoot)
		return nil
	})
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
