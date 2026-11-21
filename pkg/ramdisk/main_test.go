package ramdisk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain isolates ramdisk tests from the real runtime, cache, and temp
// directories. Tests may override these values with t.Setenv when needed.
func TestMain(m *testing.M) {
	base, err := os.MkdirTemp("", "grim-ramdisk-test-*")
	if err != nil {
		panic("failed to create test root: " + err.Error())
	}

	home := filepath.Join(base, "home")
	runDir := filepath.Join(base, "run")
	cacheDir := filepath.Join(base, "cache")
	tmpDir := filepath.Join(base, "tmp")
	localAppData := filepath.Join(home, "AppData", "Local")
	for _, dir := range []string{home, runDir, cacheDir, tmpDir, localAppData} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			_ = os.RemoveAll(base)
			panic("failed to create test directory: " + err.Error())
		}
	}

	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	_ = os.Setenv("XDG_RUNTIME_DIR", runDir)
	_ = os.Setenv("XDG_CACHE_HOME", cacheDir)
	_ = os.Setenv("TMPDIR", tmpDir)
	_ = os.Setenv("LOCALAPPDATA", localAppData)

	code := m.Run()
	_ = os.RemoveAll(base)
	os.Exit(code)
}
