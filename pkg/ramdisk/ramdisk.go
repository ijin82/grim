package ramdisk

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Workspace manages an isolated workspace directory for a decrypted vault.
type Workspace struct {
	Path      string
	VaultName string
}

// GetPath returns the expected workspace path for a vault without creating it.
func GetPath(vaultName string) string {
	if vaultName == "" {
		vaultName = "default"
	}
	baseDir := getBaseWorkspaceDir()
	return filepath.Join(baseDir, "grim-"+vaultName)
}

// IsUnlocked returns true if the workspace directory currently exists on disk.
func IsUnlocked(vaultName string) bool {
	p := GetPath(vaultName)
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// New creates a new isolated workspace directory for a vault.
func New(vaultName string) (*Workspace, error) {
	vaultRAMPath := GetPath(vaultName)

	// Clean up if something was left from a previous crash
	if _, err := os.Stat(vaultRAMPath); err == nil {
		_ = WipeAndRemove(vaultRAMPath)
	}

	// Create with strict 0700 permissions (only owner can read/write/cd)
	if err := os.MkdirAll(vaultRAMPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create workspace at %s: %w", vaultRAMPath, err)
	}

	return &Workspace{
		Path:      vaultRAMPath,
		VaultName: vaultName,
	}, nil
}

// getBaseWorkspaceDir finds the best workspace directory across Linux, macOS, and Windows.
func getBaseWorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Caches", "grim")
		}
		if runtime.GOOS == "windows" {
			if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
				return filepath.Join(localApp, "grim", "cache")
			}
		}
		cacheDir := os.Getenv("XDG_CACHE_HOME")
		if cacheDir == "" {
			cacheDir = filepath.Join(home, ".cache")
		}
		return filepath.Join(cacheDir, "grim")
	}

	// Fallback to OS TempDir
	return filepath.Join(os.TempDir(), "grim")
}

// WipeAndRemove securely overwrites all files in the directory with zeros before deletion.
func (w *Workspace) Destroy() error {
	return WipeAndRemove(w.Path)
}

// WipeAndRemove recursively walks a directory, securely overwrites each file, and removes everything.
func WipeAndRemove(dirPath string) error {
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return nil
	}

	// Walk files and securely wipe them
	_ = filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}

		if !d.IsDir() {
			_ = secureWipeFile(path)
		}
		return nil
	})

	// Remove all directories
	return os.RemoveAll(dirPath)
}

// secureWipeFile overwrites a file with zeroes and random bytes, syncs, then removes it.
func secureWipeFile(filePath string) error {
	fi, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	fileSize := fi.Size()
	if fileSize > 0 {
		f, err := os.OpenFile(filePath, os.O_WRONLY, 0600)
		if err == nil {
			// Zero pass
			zeroes := make([]byte, 4096)
			var written int64
			for written < fileSize {
				toWrite := int64(len(zeroes))
				if fileSize-written < toWrite {
					toWrite = fileSize - written
				}
				_, _ = f.Write(zeroes[:toWrite])
				written += toWrite
			}

			// Random pass
			_, _ = f.Seek(0, 0)
			randomBuf := make([]byte, 4096)
			written = 0
			for written < fileSize {
				toWrite := int64(len(randomBuf))
				if fileSize-written < toWrite {
					toWrite = fileSize - written
				}
				_, _ = rand.Read(randomBuf[:toWrite])
				_, _ = f.Write(randomBuf[:toWrite])
				written += toWrite
			}

			_ = f.Sync()
			_ = f.Close()
		}
	}

	return os.Remove(filePath)
}
