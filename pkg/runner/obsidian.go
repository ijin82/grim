package runner

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type obsidianVaultEntry struct {
	Path string `json:"path"`
	Ts   int64  `json:"ts"`
	Open bool   `json:"open"`
}

type obsidianConfig struct {
	Vaults map[string]obsidianVaultEntry `json:"vaults"`
}

// findObsidianConfigFiles returns possible paths to obsidian.json on the system.
func findObsidianConfigFiles() []string {
	var candidates []string
	home, err := os.UserHomeDir()
	if err != nil {
		return candidates
	}

	switch runtime.GOOS {
	case "linux":
		candidates = append(candidates,
			filepath.Join(home, ".var", "app", "md.obsidian.Obsidian", "config", "obsidian", "obsidian.json"),
			filepath.Join(home, ".config", "obsidian", "obsidian.json"),
			filepath.Join(home, "snap", "obsidian", "current", ".config", "obsidian", "obsidian.json"),
		)
	case "darwin":
		candidates = append(candidates,
			filepath.Join(home, "Library", "Application Support", "obsidian", "obsidian.json"),
		)
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			candidates = append(candidates, filepath.Join(appData, "obsidian", "obsidian.json"))
		}
	}

	return candidates
}

// RegisterObsidianVault ensures the target path is registered in Obsidian's known vaults list.
func RegisterObsidianVault(vaultPath string) {
	candidates := findObsidianConfigFiles()

	h := md5.Sum([]byte(vaultPath))
	vaultID := hex.EncodeToString(h[:8]) // 16-hex characters

	for _, cfgPath := range candidates {
		dir := filepath.Dir(cfgPath)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		var cfg obsidianConfig
		cfg.Vaults = make(map[string]obsidianVaultEntry)

		if data, err := os.ReadFile(cfgPath); err == nil {
			_ = json.Unmarshal(data, &cfg)
		}

		if cfg.Vaults == nil {
			cfg.Vaults = make(map[string]obsidianVaultEntry)
		}

		// Add or update vault entry
		cfg.Vaults[vaultID] = obsidianVaultEntry{
			Path: vaultPath,
			Ts:   time.Now().UnixMilli(),
			Open: true,
		}

		if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			_ = os.WriteFile(cfgPath, data, 0600)
		}
	}
}

// UnregisterObsidianVault cleans up the vault entry from Obsidian's known vaults list.
func UnregisterObsidianVault(vaultPath string) {
	candidates := findObsidianConfigFiles()

	for _, cfgPath := range candidates {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}

		var cfg obsidianConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		changed := false
		for id, v := range cfg.Vaults {
			if v.Path == vaultPath {
				delete(cfg.Vaults, id)
				changed = true
			}
		}

		if changed {
			if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
				_ = os.WriteFile(cfgPath, data, 0600)
			}
		}
	}
}
