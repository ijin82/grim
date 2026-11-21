package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ijin82/grim/pkg/config"
	"github.com/ijin82/grim/pkg/ramdisk"
)

func TestValidatePassphrase(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		wantErr    bool
	}{
		{name: "too short", passphrase: "1234567", wantErr: true},
		{name: "minimum ASCII", passphrase: "12345678", wantErr: false},
		{name: "too short Cyrillic", passphrase: "пароль", wantErr: true},
		{name: "minimum Cyrillic runes", passphrase: "пароль12", wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePassphrase(tc.passphrase)
			if tc.wantErr && err == nil {
				t.Fatalf("validatePassphrase(%q) = nil, want error", tc.passphrase)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validatePassphrase(%q) = %v, want nil", tc.passphrase, err)
			}
		})
	}
}

func TestIsLockCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "lock", want: true},
		{input: "Q", want: true},
		{input: " exit ", want: true},
		{input: "close", want: true},
		{input: "status", want: false},
		{input: "", want: false},
	}

	for _, tc := range tests {
		if got := isLockCommand(tc.input); got != tc.want {
			t.Fatalf("isLockCommand(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestRemoveRejectsUnlockedVault(t *testing.T) {
	home := t.TempDir()
	runDir := filepath.Join(t.TempDir(), "run")
	emptyPath := t.TempDir()
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runDir)
	t.Setenv("PATH", emptyPath)

	const vaultName = "phase1-remove-test"
	const vaultPath = "/tmp/phase1-remove-vault"

	cfg := &config.Config{
		DefaultVault:  vaultName,
		DefaultEditor: "obsidian",
		Vaults: map[string]config.VaultConfig{
			vaultName: {
				Path:           vaultPath,
				Editor:         "obsidian",
				TimeoutMinutes: config.DefaultTimeoutMinutes,
			},
		},
	}
	if err := cfg.Save(""); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	ws, err := ramdisk.NewForEditor(vaultName, "")
	if err != nil {
		t.Fatalf("failed to create active workspace: %v", err)
	}
	defer func() { _ = ws.Destroy() }()

	if err := removeCmd.RunE(removeCmd, []string{vaultName}); err == nil {
		t.Fatalf("remove must fail while the vault is unlocked")
	}

	loaded, err := config.LoadConfig("")
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if _, exists := loaded.Vaults[vaultName]; !exists {
		t.Fatalf("vault entry must remain in config after refused remove")
	}

	if err := ws.Destroy(); err != nil {
		t.Fatalf("failed to destroy workspace: %v", err)
	}

	if err := removeCmd.RunE(removeCmd, []string{vaultName}); err != nil {
		t.Fatalf("remove failed for locked vault: %v", err)
	}

	loaded, err = config.LoadConfig("")
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if _, exists := loaded.Vaults[vaultName]; exists {
		t.Fatalf("vault entry must be removed from config")
	}
}
