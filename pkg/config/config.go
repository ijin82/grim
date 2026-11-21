package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// DefaultTimeoutMinutes is applied during config loading when a vault has no
// valid auto-lock timeout. A zero value must not silently disable auto-lock.
const DefaultTimeoutMinutes = 30

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

// defaultConfigPathFor returns the platform-specific config path.
// On Windows the documented location is %APPDATA%\grim\config.yaml; Linux and
// macOS keep the historical ~/.config/grim/config.yaml location.
func defaultConfigPathFor(goos string, appData string, home string) string {
	if goos == "windows" && appData != "" {
		return filepath.Join(appData, "grim", "config.yaml")
	}
	return filepath.Join(home, ".config", "grim", "config.yaml")
}

func DefaultConfigPath() (string, error) {
	home, homeErr := os.UserHomeDir()
	appData := os.Getenv("APPDATA")

	if runtime.GOOS == "windows" && appData != "" {
		primary := defaultConfigPathFor(runtime.GOOS, appData, home)

		// Keep compatibility with configs created by older Windows builds that
		// used the Unix-style location. Prefer the legacy file if it exists.
		if homeErr == nil {
			legacy := filepath.Join(home, ".config", "grim", "config.yaml")
			if _, err := os.Stat(primary); os.IsNotExist(err) {
				if _, legacyErr := os.Stat(legacy); legacyErr == nil {
					return legacy, nil
				}
			}
		}

		return primary, nil
	}

	if homeErr != nil {
		return "", fmt.Errorf("failed to get user home dir: %w", homeErr)
	}

	return defaultConfigPathFor(runtime.GOOS, appData, home), nil
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

	// Missing or zero timeouts must not silently disable auto-lock.
	for name, vaultCfg := range cfg.Vaults {
		if vaultCfg.TimeoutMinutes <= 0 {
			vaultCfg.TimeoutMinutes = DefaultTimeoutMinutes
			cfg.Vaults[name] = vaultCfg
		}
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

	tmpFile, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary config file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temporary config file %s: %w", tmpPath, err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temporary config file %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary config file %s: %w", tmpPath, err)
	}

	// Atomic replacement: a crash either keeps the old config or exposes the
	// fully written new one, never a truncated YAML file.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace config %s: %w", path, err)
	}

	return nil
}
