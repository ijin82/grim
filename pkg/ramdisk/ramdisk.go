package ramdisk

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const LockFileName = ".grim-lock"

// Workspace manages an isolated workspace directory for a decrypted vault.
type Workspace struct {
	Path      string
	VaultName string
	PID       int
}

type LockInfo struct {
	PID       int       `json:"pid"`
	VaultName string    `json:"vault_name"`
	CreatedAt time.Time `json:"created_at"`
}

// GetPath returns the expected workspace path for a vault without creating it.
func GetPath(vaultName string) string {
	if vaultName == "" {
		vaultName = "default"
	}
	baseDir := getBaseWorkspaceDir()
	return filepath.Join(baseDir, "grim-"+vaultName)
}

// IsUnlocked returns true if the workspace directory currently exists and is owned by an active process.
func IsUnlocked(vaultName string) bool {
	p := GetPath(vaultName)
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		// Check fallback locations
		for _, altBase := range getAllPossibleWorkspaceDirs() {
			altPath := filepath.Join(altBase, "grim-"+vaultName)
			if altFi, err := os.Stat(altPath); err == nil && altFi.IsDir() {
				if isWorkspaceActive(altPath) {
					return true
				}
				_ = WipeAndRemove(altPath)
			}
		}
		return false
	}

	if isWorkspaceActive(p) {
		return true
	}

	// Stale directory from crash - wipe it
	_ = WipeAndRemove(p)
	return false
}

// New creates a new isolated workspace directory in pure RAM (tmpfs) for a vault.
func New(vaultName string) (*Workspace, error) {
	vaultRAMPath := GetPath(vaultName)

	// Clean up if something was left from a previous crash
	if _, err := os.Stat(vaultRAMPath); err == nil {
		_ = WipeAndRemove(vaultRAMPath)
	}

	// Create with strict 0700 permissions (only owner can read/write/cd)
	if err := os.MkdirAll(vaultRAMPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create RAM workspace at %s: %w", vaultRAMPath, err)
	}

	pid := os.Getpid()
	lockData, _ := json.Marshal(LockInfo{
		PID:       pid,
		VaultName: vaultName,
		CreatedAt: time.Now(),
	})
	_ = os.WriteFile(filepath.Join(vaultRAMPath, LockFileName), lockData, 0600)

	return &Workspace{
		Path:      vaultRAMPath,
		VaultName: vaultName,
		PID:       pid,
	}, nil
}

// getBaseWorkspaceDir prioritizes true volatile RAM tmpfs mounts in Linux, and OS cache in macOS/Windows.
func getBaseWorkspaceDir() string {
	if runtime.GOOS == "linux" {
		// Priority 1: Systemd / Linux user runtime directory (/run/user/$UID - pure RAM tmpfs)
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			grimRun := filepath.Join(runtimeDir, "grim")
			if err := os.MkdirAll(grimRun, 0700); err == nil {
				return grimRun
			}
		}

		// Priority 2: POSIX Shared Memory (/dev/shm - pure RAM tmpfs)
		if fi, err := os.Stat("/dev/shm"); err == nil && fi.IsDir() {
			uid := os.Getuid()
			shmDir := filepath.Join("/dev/shm", fmt.Sprintf("grim-%d", uid))
			if err := os.MkdirAll(shmDir, 0700); err == nil {
				return shmDir
			}
		}

		// Priority 3: XDG Cache Directory (~/.cache/grim)
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			cacheDir := os.Getenv("XDG_CACHE_HOME")
			if cacheDir == "" {
				cacheDir = filepath.Join(home, ".cache")
			}
			return filepath.Join(cacheDir, "grim")
		}
	}

	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			return filepath.Join(home, "Library", "Caches", "grim")
		}
	}

	if runtime.GOOS == "windows" {
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			return filepath.Join(localApp, "grim", "cache")
		}
	}

	// Fallback to OS TempDir
	return filepath.Join(os.TempDir(), "grim")
}

// getAllPossibleWorkspaceDirs returns all candidate directories where workspaces could have been created.
func getAllPossibleWorkspaceDirs() []string {
	var dirs []string
	if runtime.GOOS == "linux" {
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			dirs = append(dirs, filepath.Join(runtimeDir, "grim"))
		}
		uid := os.Getuid()
		dirs = append(dirs, filepath.Join("/dev/shm", fmt.Sprintf("grim-%d", uid)))
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".cache", "grim"))
		}
	} else if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "Library", "Caches", "grim"))
		}
	} else if runtime.GOOS == "windows" {
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			dirs = append(dirs, filepath.Join(localApp, "grim", "cache"))
		}
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), "grim"))
	return dirs
}

// isWorkspaceActive checks if a workspace lockfile exists and its process is alive.
func isWorkspaceActive(workspacePath string) bool {
	lockPath := filepath.Join(workspacePath, LockFileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		// If simple PID integer was written
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || !isProcessAlive(pid) {
			return false
		}
		return true
	}

	return isProcessAlive(info.PID)
}

// isProcessAlive checks if a process with given PID exists.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		// On Windows, process probing
		return true
	}
	// Unix: signal 0 checks process existence without killing
	err := syscall.Kill(pid, 0)
	return err == nil
}

// CleanupStaleWorkspaces scans all workspace locations and securely wipes any orphaned folders from crashed sessions.
func CleanupStaleWorkspaces() []string {
	var cleaned []string
	visited := make(map[string]bool)

	for _, baseDir := range getAllPossibleWorkspaceDirs() {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "grim-") {
				wsPath := filepath.Join(baseDir, entry.Name())
				if visited[wsPath] {
					continue
				}
				visited[wsPath] = true

				if !isWorkspaceActive(wsPath) {
					_ = WipeAndRemove(wsPath)
					cleaned = append(cleaned, wsPath)
				}
			}
		}
	}

	return cleaned
}

// Destroy securely wipes all files in the workspace with zeros and random bytes.
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
