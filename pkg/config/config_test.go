package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadSave(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")

	cfg := &Config{
		DefaultVault:  "work",
		DefaultEditor: "obsidian",
		Vaults: map[string]VaultConfig{
			"work": {
				Path:           "/home/user/vaults/work.enc",
				Editor:         "obsidian",
				TimeoutMinutes: 30,
			},
		},
	}

	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	loaded, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if loaded.DefaultVault != "work" {
		t.Errorf("Expected default vault 'work', got '%s'", loaded.DefaultVault)
	}

	v, exists := loaded.Vaults["work"]
	if !exists {
		t.Fatalf("Expected vault 'work' to exist in loaded config")
	}

	if v.TimeoutMinutes != 30 {
		t.Errorf("Expected timeout 30, got %d", v.TimeoutMinutes)
	}
}

func TestLoadConfigAppliesDefaultTimeout(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")

	data := []byte(`vaults:
  zero:
    path: /vaults/zero
    timeout_minutes: 0
  missing:
    path: /vaults/missing
  custom:
    path: /vaults/custom
    timeout_minutes: 15
`)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if got := cfg.Vaults["zero"].TimeoutMinutes; got != DefaultTimeoutMinutes {
		t.Errorf("zero timeout: got %d, want %d", got, DefaultTimeoutMinutes)
	}
	if got := cfg.Vaults["missing"].TimeoutMinutes; got != DefaultTimeoutMinutes {
		t.Errorf("missing timeout: got %d, want %d", got, DefaultTimeoutMinutes)
	}
	if got := cfg.Vaults["custom"].TimeoutMinutes; got != 15 {
		t.Errorf("custom timeout: got %d, want 15", got)
	}
}

func TestDefaultConfigPathFor(t *testing.T) {
	home := filepath.Join("home", "user")
	appData := filepath.Join("Users", "user", "AppData", "Roaming")

	tests := []struct {
		name    string
		goos    string
		appData string
		want    string
	}{
		{
			name: "linux",
			goos: "linux",
			want: filepath.Join(home, ".config", "grim", "config.yaml"),
		},
		{
			name: "darwin keeps legacy path",
			goos: "darwin",
			want: filepath.Join(home, ".config", "grim", "config.yaml"),
		},
		{
			name:    "windows appdata",
			goos:    "windows",
			appData: appData,
			want:    filepath.Join(appData, "grim", "config.yaml"),
		},
		{
			name: "windows without appdata falls back",
			goos: "windows",
			want: filepath.Join(home, ".config", "grim", "config.yaml"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultConfigPathFor(tc.goos, tc.appData, home); got != tc.want {
				t.Fatalf("defaultConfigPathFor() = %q, want %q", got, tc.want)
			}
		})
	}
}
