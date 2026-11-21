package ramdisk

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const LockFileName = ".grim-lock"

// Workspace manages an isolated workspace directory for a decrypted vault.
type Workspace struct {
	Path      string
	VaultName string
	PID       int
	lock      *workspaceLock
}

type LockInfo struct {
	PID       int       `json:"pid"`
	VaultName string    `json:"vault_name"`
	CreatedAt time.Time `json:"created_at"`
}

// GetPath returns the expected workspace path for a vault without creating it.
func GetPath(vaultName string) string {
	return GetPathForEditor(vaultName, "")
}

// GetPathForEditor returns the expected workspace path tailored to the editor type.
func GetPathForEditor(vaultName string, editorName string) string {
	if vaultName == "" {
		vaultName = "default"
	}
	baseDir := getBaseWorkspaceDir(editorName)
	return filepath.Join(baseDir, "grim-"+vaultName)
}

// IsUnlocked returns true if the workspace directory currently exists and is owned by an active process.
func IsUnlocked(vaultName string) bool {
	// Check all possible locations
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

// New creates a new isolated workspace directory in pure RAM (tmpfs) for a vault.
func New(vaultName string) (*Workspace, error) {
	return NewForEditor(vaultName, "")
}

// NewForEditor creates a new isolated workspace directory tailored to the editor.
func NewForEditor(vaultName string, editorName string) (*Workspace, error) {
	vaultRAMPath := GetPathForEditor(vaultName, editorName)

	// Clean up if something was left from a previous crash
	if _, err := os.Stat(vaultRAMPath); err == nil {
		_ = WipeAndRemove(vaultRAMPath)
	}

	// Create with strict 0700 permissions (only owner can read/write/cd)
	if err := os.MkdirAll(vaultRAMPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create RAM workspace at %s: %w", vaultRAMPath, err)
	}

	pid := os.Getpid()
	lock, err := acquireWorkspaceLock(filepath.Join(vaultRAMPath, LockFileName), LockInfo{
		PID:       pid,
		VaultName: vaultName,
		CreatedAt: time.Now(),
	})
	if err != nil {
		// Never leave a workspace directory without a valid lock: otherwise the
		// next grim invocation would treat it as abandoned and try to wipe it.
		_ = os.RemoveAll(vaultRAMPath)
		return nil, fmt.Errorf("failed to lock RAM workspace at %s: %w", vaultRAMPath, err)
	}

	return &Workspace{
		Path:      vaultRAMPath,
		VaultName: vaultName,
		PID:       pid,
		lock:      lock,
	}, nil
}

// Close releases the session advisory lock without deleting workspace files.
// It is called after Lock/Destroy or when executeOpen returns.
func (w *Workspace) Close() error {
	if w == nil || w.lock == nil {
		return nil
	}
	err := w.lock.Close()
	w.lock = nil
	return err
}

func isFlatpakEditor(editorName string) bool {
	lower := strings.ToLower(editorName)
	if lower == "" || lower == "obsidian" {
		// If native obsidian binary exists in PATH, it's native!
		if path, err := exec.LookPath("obsidian"); err == nil && !strings.Contains(path, "flatpak") && !strings.Contains(path, "snap") {
			return false
		}
		// If flatpak is installed, check if md.obsidian.Obsidian is present
		if _, err := exec.LookPath("flatpak"); err == nil {
			return true
		}
	}
	if strings.Contains(lower, "flatpak") {
		return true
	}
	return false
}

// getBaseWorkspaceDir prioritizes true volatile RAM tmpfs mounts for native editors, and cache for Flatpak/Snap.
func getBaseWorkspaceDir(editorName string) string {
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

	// Linux
	if isFlatpakEditor(editorName) {
		// Sandboxed Flatpak Obsidian requires ~/.cache/grim
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			cacheDir := os.Getenv("XDG_CACHE_HOME")
			if cacheDir == "" {
				cacheDir = filepath.Join(home, ".cache")
			}
			return filepath.Join(cacheDir, "grim")
		}
	} else {
		// Native editors (Native Obsidian, Vim, VS Code, Helix, etc.) get pure RAM tmpfs!
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			grimRun := filepath.Join(runtimeDir, "grim")
			if err := os.MkdirAll(grimRun, 0700); err == nil {
				return grimRun
			}
		}
		if fi, err := os.Stat("/dev/shm"); err == nil && fi.IsDir() {
			uid := os.Getuid()
			shmDir := filepath.Join("/dev/shm", fmt.Sprintf("grim-%d", uid))
			if err := os.MkdirAll(shmDir, 0700); err == nil {
				return shmDir
			}
		}
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			return filepath.Join(home, ".cache", "grim")
		}
	}

	// Fallback to OS TempDir
	return filepath.Join(os.TempDir(), "grim")
}

// getAllPossibleWorkspaceDirs returns all candidate directories where workspaces could have been created.
func getAllPossibleWorkspaceDirs() []string {
	var dirs []string
	switch runtime.GOOS {
	case "linux":
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			dirs = append(dirs, filepath.Join(runtimeDir, "grim"))
		}
		uid := os.Getuid()
		dirs = append(dirs, filepath.Join("/dev/shm", fmt.Sprintf("grim-%d", uid)))
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".cache", "grim"))
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "Library", "Caches", "grim"))
		}
	case "windows":
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			dirs = append(dirs, filepath.Join(localApp, "grim", "cache"))
		}
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), "grim"))
	return dirs
}

// isWorkspaceActive checks whether a workspace is held by an active session.
//
// The primary check is the advisory file lock: it works even from another PID
// namespace (Flatpak, containers) and is not affected by PID reuse. The PID
// check remains only for compatibility with older grim versions that did not
// hold an flock yet.
func isWorkspaceActive(workspacePath string) bool {
	lockPath := filepath.Join(workspacePath, LockFileName)
	if isWorkspaceLockHeld(lockPath) {
		return true
	}

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

// IsWorkspaceActive reports whether workspacePath belongs to a live session.
// It is exported so the lock command can inspect all candidate workspace
// locations instead of only the path derived from the current environment.
func IsWorkspaceActive(workspacePath string) bool {
	return isWorkspaceActive(workspacePath)
}

// ReadLockInfo reads the session metadata from a workspace lock file.
// Legacy lock files containing only a PID are still accepted.
func ReadLockInfo(workspacePath string) (*LockInfo, error) {
	lockPath := filepath.Join(workspacePath, LockFileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file at %s: %w", lockPath, err)
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err == nil && info.PID > 0 {
		return &info, nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid lock file at %s", lockPath)
	}
	return &LockInfo{PID: pid}, nil
}

// FindWorkspacePaths returns all existing workspace directories for a vault
// across every supported base directory. The result is sorted to keep command
// behavior deterministic.
func FindWorkspacePaths(vaultName string) []string {
	if vaultName == "" {
		vaultName = "default"
	}

	var paths []string
	seen := make(map[string]bool)
	for _, baseDir := range getAllPossibleWorkspaceDirs() {
		workspacePath := filepath.Join(baseDir, "grim-"+vaultName)
		if seen[workspacePath] {
			continue
		}
		seen[workspacePath] = true
		if fi, err := os.Stat(workspacePath); err == nil && fi.IsDir() {
			paths = append(paths, workspacePath)
		}
	}

	sort.Strings(paths)
	return paths
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
	if w == nil {
		return nil
	}
	_ = w.Close()
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
