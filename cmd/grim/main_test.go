package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ijin82/grim/pkg/ramdisk"
)

// TestMain isolates every cmd/grim test from the real user config, cache,
// runtime directory, and temporary files.
func TestMain(m *testing.M) {
	base, err := os.MkdirTemp("", "grim-cmd-test-*")
	if err != nil {
		panic("failed to create test root: " + err.Error())
	}

	home := filepath.Join(base, "home")
	runDir := filepath.Join(base, "run")
	cacheDir := filepath.Join(base, "cache")
	tmpDir := filepath.Join(base, "tmp")
	appData := filepath.Join(home, "AppData", "Roaming")
	localAppData := filepath.Join(home, "AppData", "Local")
	for _, dir := range []string{home, runDir, cacheDir, tmpDir, appData, localAppData} {
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
	_ = os.Setenv("APPDATA", appData)
	_ = os.Setenv("LOCALAPPDATA", localAppData)

	code := m.Run()
	_ = os.RemoveAll(base)
	os.Exit(code)
}

// mustNewWorkspace creates an isolated RAM workspace or fails the test with a
// useful message instead of panicking on a nil workspace.
func mustNewWorkspace(t *testing.T, name string) *ramdisk.Workspace {
	t.Helper()
	ws, err := ramdisk.New(name)
	if err != nil {
		t.Fatalf("failed to create workspace %q: %v", name, err)
	}
	return ws
}
