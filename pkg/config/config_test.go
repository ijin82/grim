package config

import (
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
