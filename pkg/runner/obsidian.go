package runner

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
// It returns an aggregated error when an existing registry cannot be read,
// parsed, or written; a missing Obsidian config directory is not an error.
func RegisterObsidianVault(vaultPath string) error {
	candidates := findObsidianConfigFiles()

	h := md5.Sum([]byte(vaultPath))
	vaultID := hex.EncodeToString(h[:8]) // 16-hex characters

	var errs []error
	for _, cfgPath := range candidates {
		dir := filepath.Dir(cfgPath)
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("failed to inspect Obsidian config dir %s: %w", dir, err))
			continue
		}

		cfg := obsidianConfig{Vaults: make(map[string]obsidianVaultEntry)}
		if data, err := os.ReadFile(cfgPath); err == nil {
			if err := json.Unmarshal(data, &cfg); err != nil {
				errs = append(errs, fmt.Errorf("failed to parse Obsidian config %s: %w", cfgPath, err))
				continue
			}
		} else if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("failed to read Obsidian config %s: %w", cfgPath, err))
			continue
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

		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to serialize Obsidian config %s: %w", cfgPath, err))
			continue
		}
		if err := os.WriteFile(cfgPath, data, 0600); err != nil {
			errs = append(errs, fmt.Errorf("failed to write Obsidian config %s: %w", cfgPath, err))
		}
	}

	return errors.Join(errs...)
}

// UnregisterObsidianVault cleans up the vault entry from Obsidian's known vaults list.
// It returns an aggregated error for registry files that exist but cannot be
// read, parsed, or written.
func UnregisterObsidianVault(vaultPath string) error {
	candidates := findObsidianConfigFiles()

	var errs []error
	for _, cfgPath := range candidates {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("failed to read Obsidian config %s: %w", cfgPath, err))
			continue
		}

		var cfg obsidianConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			errs = append(errs, fmt.Errorf("failed to parse Obsidian config %s: %w", cfgPath, err))
			continue
		}

		changed := false
		for id, v := range cfg.Vaults {
			if v.Path == vaultPath {
				delete(cfg.Vaults, id)
				changed = true
			}
		}

		if !changed {
			continue
		}

		data, err = json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to serialize Obsidian config %s: %w", cfgPath, err))
			continue
		}
		if err := os.WriteFile(cfgPath, data, 0600); err != nil {
			errs = append(errs, fmt.Errorf("failed to write Obsidian config %s: %w", cfgPath, err))
		}
	}

	return errors.Join(errs...)
}
