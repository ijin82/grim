package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ijin82/grim/pkg/config"
	"github.com/ijin82/grim/pkg/ramdisk"
	"github.com/ijin82/grim/pkg/runner"
)

func setupPhase2Env(t *testing.T) (home string, runDir string) {
	t.Helper()
	home = t.TempDir()
	runDir = filepath.Join(t.TempDir(), "run")
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runDir)
	t.Setenv("PATH", t.TempDir())
	return home, runDir
}

func TestSortedVaultNames(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"zeta":  {Path: "/z"},
			"alpha": {Path: "/a"},
			"mu":    {Path: "/m"},
		},
	}

	got := sortedVaultNames(cfg)
	want := []string{"alpha", "mu", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedVaultNames() = %v, want %v", got, want)
	}
}

func TestLockVaultWipesStaleWorkspace(t *testing.T) {
	_, runDir := setupPhase2Env(t)
	const vaultName = "stale-lock-test"
	wsPath := filepath.Join(runDir, "grim", "grim-"+vaultName)
	if err := os.MkdirAll(wsPath, 0700); err != nil {
		t.Fatalf("failed to create stale workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsPath, ramdisk.LockFileName), []byte(`{"pid":0}`), 0600); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	if err := lockVault(vaultName); err != nil {
		t.Fatalf("lockVault failed: %v", err)
	}
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Fatalf("stale workspace was not wiped")
	}
}

func TestLockVaultRefusesCurrentProcess(t *testing.T) {
	_, _ = setupPhase2Env(t)
	const vaultName = "active-lock-test"
	ws, err := ramdisk.NewForEditor(vaultName, "")
	if err != nil {
		t.Fatalf("failed to create active workspace: %v", err)
	}
	defer func() { _ = ws.Destroy() }()

	if err := lockVault(vaultName); err == nil {
		t.Fatalf("lockVault must refuse to signal the current process")
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("active workspace must remain intact: %v", err)
	}
}

func TestLockVaultSendsGracefulSignal(t *testing.T) {
	_, runDir := setupPhase2Env(t)
	const vaultName = "signal-lock-test"
	wsPath := filepath.Join(runDir, "grim", "grim-"+vaultName)
	if err := os.MkdirAll(wsPath, 0700); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// The helper emulates a live grim owner: it removes the workspace on SIGINT.
	script := `trap 'rm -rf "$WS"' INT; while :; do /bin/sleep 0.1; done`
	helper := exec.Command("/bin/sh", "-c", script)
	helper.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=/bin:/usr/bin",
		"WS=" + wsPath,
	}
	if err := helper.Start(); err != nil {
		t.Fatalf("failed to start helper: %v", err)
	}
	defer func() {
		_ = helper.Process.Kill()
		_, _ = helper.Process.Wait()
	}()

	lockInfo := fmt.Sprintf(`{"pid":%d,"vault_name":%q}`, helper.Process.Pid, vaultName)
	if err := os.WriteFile(filepath.Join(wsPath, ramdisk.LockFileName), []byte(lockInfo), 0600); err != nil {
		t.Fatalf("failed to write helper lock: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := lockVault(vaultName); err != nil {
		t.Fatalf("lockVault failed: %v", err)
	}
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Fatalf("helper workspace was not removed after signal")
	}
}

func TestCleanupStaleWorkspacesUnregistersObsidian(t *testing.T) {
	home, runDir := setupPhase2Env(t)
	const vaultName = "obsidian-cleanup-test"
	wsPath := filepath.Join(runDir, "grim", "grim-"+vaultName)
	if err := os.MkdirAll(wsPath, 0700); err != nil {
		t.Fatalf("failed to create stale workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsPath, ramdisk.LockFileName), []byte(`{"pid":0}`), 0600); err != nil {
		t.Fatalf("failed to write stale lock: %v", err)
	}

	obsidianDir := filepath.Join(home, ".config", "obsidian")
	if err := os.MkdirAll(obsidianDir, 0700); err != nil {
		t.Fatalf("failed to create obsidian config dir: %v", err)
	}
	runner.RegisterObsidianVault(wsPath)

	cleaned := cleanupStaleWorkspaces()
	found := false
	for _, path := range cleaned {
		if path == wsPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cleanup did not report the stale workspace")
	}

	data, err := os.ReadFile(filepath.Join(obsidianDir, "obsidian.json"))
	if err != nil {
		t.Fatalf("failed to read obsidian.json: %v", err)
	}
	if strings.Contains(string(data), wsPath) {
		t.Fatalf("stale Obsidian registry entry was not removed: %s", data)
	}
}
