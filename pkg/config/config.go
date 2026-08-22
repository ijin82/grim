package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type VaultConfig struct {
	Path           string `yaml:"path"`
	Editor         string `yaml:"editor,omitempty"`
	TimeoutMinutes int    `yaml:"timeout_minutes,omitempty"`
}

type Config struct {
	DefaultVault  string                 `yaml:"default_vault,omitempty"`
	DefaultEditor string                 `yaml:"default_editor,omitempty"`
	Vaults        map[string]VaultConfig `yaml:"vaults,omitempty"`
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home dir: %w", err)
	}

	grimPath := filepath.Join(home, ".config", "grim", "config.yaml")

	// If grim config doesn't exist but old cryptovault config exists, migrate it
	if _, err := os.Stat(grimPath); os.IsNotExist(err) {
		legacyPath := filepath.Join(home, ".config", "cryptovault", "config.yaml")
		if _, err := os.Stat(legacyPath); err == nil {
			// Migrate legacy file
			data, err := os.ReadFile(legacyPath)
			if err == nil {
				_ = os.MkdirAll(filepath.Dir(grimPath), 0700)
				_ = os.WriteFile(grimPath, data, 0600)
			}
		}
	}

	return grimPath, nil
}

func LoadConfig(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Config{
			DefaultEditor: "obsidian",
			Vaults:        make(map[string]VaultConfig),
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config at %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config: %w", err)
	}

	if cfg.Vaults == nil {
		cfg.Vaults = make(map[string]VaultConfig)
	}

	return &cfg, nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config dir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config to %s: %w", path, err)
	}

	return nil
}
