package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func obsidianConfigDir(t *testing.T, home string) string {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		appData := t.TempDir()
		t.Setenv("APPDATA", appData)
		return filepath.Join(appData, "obsidian")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "obsidian")
	default:
		return filepath.Join(home, ".config", "obsidian")
	}
}

func setupObsidianHome(t *testing.T) (home string, configDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	configDir = obsidianConfigDir(t, home)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("failed to create obsidian config dir: %v", err)
	}
	return home, configDir
}

func TestObsidianRegisterUnregisterVault(t *testing.T) {
	_, configDir := setupObsidianHome(t)
	vaultPath := filepath.Join(t.TempDir(), "vault")

	if err := RegisterObsidianVault(vaultPath); err != nil {
		t.Fatalf("RegisterObsidianVault failed: %v", err)
	}

	configPath := filepath.Join(configDir, "obsidian.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read obsidian.json: %v", err)
	}
	if !strings.Contains(string(data), vaultPath) {
		t.Fatalf("vault path was not registered: %s", data)
	}

	if err := UnregisterObsidianVault(vaultPath); err != nil {
		t.Fatalf("UnregisterObsidianVault failed: %v", err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read obsidian.json after unregister: %v", err)
	}
	if strings.Contains(string(data), vaultPath) {
		t.Fatalf("vault path was not removed: %s", data)
	}
}

func TestObsidianRegisterMalformedJSON(t *testing.T) {
	_, configDir := setupObsidianHome(t)
	configPath := filepath.Join(configDir, "obsidian.json")
	if err := os.WriteFile(configPath, []byte("{not json"), 0600); err != nil {
		t.Fatalf("failed to write malformed config: %v", err)
	}

	if err := RegisterObsidianVault(filepath.Join(t.TempDir(), "vault")); err == nil {
		t.Fatalf("RegisterObsidianVault must report malformed JSON")
	}
}

func TestObsidianUnregisterMissingRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := UnregisterObsidianVault(filepath.Join(t.TempDir(), "vault")); err != nil {
		t.Fatalf("missing registry must not be an error: %v", err)
	}
}
