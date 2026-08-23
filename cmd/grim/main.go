package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ijin82/grim/pkg/config"
	"github.com/ijin82/grim/pkg/ramdisk"
	"github.com/ijin82/grim/pkg/runner"
	"github.com/ijin82/grim/pkg/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var Version = "0.1.5"

var rootCmd = &cobra.Command{
	Use:          "grim [vault]",
	Version:      Version,
	SilenceUsage: true,
	Short:        "Grim (Grimoire) — Secure Encrypted Markdown Note Vaults for Obsidian & text editors",
	Long: `📖 Grim (Grimoire) is a fast, secure CLI tool for managing encrypted markdown vaults.
All notes are encrypted with filippo.io/age (X25519 Master Key Architecture + Scrypt KDF).
When unlocked, notes live in an isolated RAM workspace with live auto-sync.

Quick Commands:
  grim [vault]           # Open vault, start live sync & launch editor
  grim list              # Show all configured vaults & RAM status
  grim setup             # Interactive configuration wizard
  grim add               # Auto-scan disk & attach vaults
  grim lock              # Wipe RAM workspace & lock
  grim status            # Check active in-memory vaults
  grim remove <vault>    # Remove vault from configuration
  grim init <name> <dir> # Create a new encrypted vault
  grim install           # Install grim binary to system PATH`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultName := ""
		if len(args) > 0 {
			vaultName = args[0]
		}
		return executeOpen(vaultName)
	},
}

var initCmd = &cobra.Command{
	Use:   "init <vault> <dir>",
	Short: "Initialize a new encrypted vault",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultName := args[0]
		vaultDir := args[1]

		absDir, err := expandPath(vaultDir)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}

		fmt.Printf("📖 Initializing new Grim vault '%s' at: %s\n", vaultName, absDir)

		pass1, err := promptPassword("Enter master passphrase: ")
		if err != nil {
			return err
		}
		if len(pass1) < 4 {
			return fmt.Errorf("passphrase is too short (minimum 4 characters)")
		}

		pass2, err := promptPassword("Confirm master passphrase: ")
		if err != nil {
			return err
		}

		if pass1 != pass2 {
			return fmt.Errorf("passphrases do not match")
		}

		if err := vault.Init(absDir, vaultName, pass1); err != nil {
			return fmt.Errorf("failed to initialize vault: %w", err)
		}

		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		cfg.Vaults[vaultName] = config.VaultConfig{
			Path:           absDir,
			Editor:         cfg.DefaultEditor,
			TimeoutMinutes: 30,
		}

		if cfg.DefaultVault == "" {
			cfg.DefaultVault = vaultName
		}

		if err := cfg.Save(""); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("🎉 Vault '%s' successfully initialized and registered in config!\n", vaultName)
		fmt.Printf("👉 To open your notes, run: grim open %s\n", vaultName)
		return nil
	},
}

var addCmd = &cobra.Command{
	Use:     "add [vault] [dir]",
	Aliases: []string{"a"},
	Short:   "Attach an existing encrypted vault from disk (or scan for vaults)",
	Long: `Attach an existing encrypted vault directory (e.g. synced from Cloud/Git/Dropbox):
  grim add                   # Scan common folders for existing Grim vaults
  grim add work /path/to/dir # Attach /path/to/dir as vault 'work'`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		if len(args) == 0 {
			return runDiscoverAndAddInteractive(cfg)
		}

		vaultName := args[0]
		vaultDir := ""
		if len(args) == 2 {
			vaultDir = args[1]
		} else {
			// Check if single arg is actually an existing directory
			if exp, err := expandPath(vaultName); err == nil {
				if fi, err := os.Stat(exp); err == nil && fi.IsDir() {
					vaultDir = exp
					vaultName = filepath.Base(vaultDir)
				}
			}
			if vaultDir == "" {
				fmt.Print("Enter directory path for this vault: ")
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					vaultDir = strings.TrimSpace(scanner.Text())
				}
			}
		}

		absDir, err := expandPath(vaultDir)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}

		return attachVaultDirect(cfg, vaultName, absDir)
	},
}

type DiscoveredVault struct {
	Path      string
	Name      string
	VaultMeta *vault.Meta
}

func scanDirectoryForVaults(baseDir string, maxDepth int) []DiscoveredVault {
	var results []DiscoveredVault
	visited := make(map[string]bool)

	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return results
	}

	// Check if baseDir itself is a vault
	metaPath := filepath.Join(baseDir, vault.MetaFileName)
	if fi, err := os.Stat(metaPath); err == nil && !fi.IsDir() {
		abs, _ := filepath.Abs(baseDir)
		return []DiscoveredVault{{Path: abs, Name: filepath.Base(abs)}}
	}

	_ = filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}

		rel, _ := filepath.Rel(baseDir, path)
		if strings.Count(rel, string(filepath.Separator)) > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() && path != baseDir {
			subMeta := filepath.Join(path, vault.MetaFileName)
			if fi, err := os.Stat(subMeta); err == nil && !fi.IsDir() {
				abs, _ := filepath.Abs(path)
				if !visited[abs] {
					visited[abs] = true
					results = append(results, DiscoveredVault{
						Path: abs,
						Name: filepath.Base(abs),
					})
				}
				return filepath.SkipDir
			}
		}
		return nil
	})

	return results
}

func scanForVaults() []DiscoveredVault {
	home, _ := os.UserHomeDir()
	candidateDirs := []string{
		".",
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Dropbox"),
		filepath.Join(home, "Nextcloud"),
		filepath.Join(home, "Sync"),
		filepath.Join(home, "GDrive"),
		filepath.Join(home, "Google Drive"),
		filepath.Join(home, "OneDrive"),
		filepath.Join(home, "Yandex.Disk"),
		filepath.Join(home, "iCloudDrive"),
		filepath.Join(home, "Notes"),
		filepath.Join(home, "Vaults"),
	}

	var results []DiscoveredVault
	visited := make(map[string]bool)

	for _, cand := range candidateDirs {
		found := scanDirectoryForVaults(cand, 3)
		for _, f := range found {
			if !visited[f.Path] {
				visited[f.Path] = true
				results = append(results, f)
			}
		}
	}

	return results
}

func runDiscoverAndAddInteractive(cfg *config.Config) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("🔍 Scanning common directories for existing Grim vaults...")
	discovered := scanForVaults()

	var unconfigured []DiscoveredVault
	for _, d := range discovered {
		alreadyConfigured := false
		for _, v := range cfg.Vaults {
			if v.Path == d.Path {
				alreadyConfigured = true
				break
			}
		}
		if !alreadyConfigured {
			unconfigured = append(unconfigured, d)
		}
	}

	if len(unconfigured) == 0 {
		if len(discovered) > 0 {
			fmt.Println("✨ All discovered vaults on disk are already in your configuration!")
		} else {
			fmt.Println("🤷 No existing encrypted vaults found automatically.")
		}
		fmt.Print("\nEnter path to existing vault directory (or press Enter to cancel): ")
		if scanner.Scan() {
			path := strings.TrimSpace(scanner.Text())
			if path != "" {
				absDir, err := expandPath(path)
				if err != nil {
					return fmt.Errorf("invalid path: %w", err)
				}
				return attachVaultDirect(cfg, filepath.Base(absDir), absDir)
			}
		}
		return nil
	}

	fmt.Printf("\n📂 Discovered %d unconfigured vault(s) on disk:\n", len(unconfigured))
	fmt.Println("--------------------------------------------------")
	for i, d := range unconfigured {
		fmt.Printf(" %d) %s\n    Path: %s\n", i+1, d.Name, d.Path)
	}
	fmt.Printf(" %d) Enter custom path...\n", len(unconfigured)+1)
	fmt.Println(" 0) Cancel")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("Select vault to attach [0-%d]: ", len(unconfigured)+1)

	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		idx, err := strconv.Atoi(text)
		if err == nil && idx >= 1 && idx <= len(unconfigured) {
			chosen := unconfigured[idx-1]
			return attachVaultDirect(cfg, chosen.Name, chosen.Path)
		} else if err == nil && idx == len(unconfigured)+1 {
			fmt.Print("Enter vault directory path: ")
			if scanner.Scan() {
				path := strings.TrimSpace(scanner.Text())
				if path != "" {
					absDir, err := expandPath(path)
					if err != nil {
						return fmt.Errorf("invalid path: %w", err)
					}
					return attachVaultDirect(cfg, filepath.Base(absDir), absDir)
				}
			}
		}
	}

	return nil
}

func attachVaultDirect(cfg *config.Config, suggestedName string, absDir string) error {
	scanner := bufio.NewScanner(os.Stdin)
	metaFile := filepath.Join(absDir, vault.MetaFileName)

	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		// If directly not a vault, search subfolders
		found := scanDirectoryForVaults(absDir, 4)
		if len(found) == 0 {
			return fmt.Errorf("no Grim vault found at '%s' or any of its subfolders (missing %s)", absDir, vault.MetaFileName)
		}
		if len(found) == 1 {
			fmt.Printf("🔍 Detected Grim vault in subfolder: %s\n", found[0].Path)
			absDir = found[0].Path
			suggestedName = found[0].Name
		} else {
			fmt.Printf("\n📂 Found %d Grim vaults inside '%s':\n", len(found), absDir)
			fmt.Println("--------------------------------------------------")
			for i, f := range found {
				fmt.Printf(" %d) %s\n    Path: %s\n", i+1, f.Name, f.Path)
			}
			fmt.Printf("Select vault to attach [1-%d]: ", len(found))
			if scanner.Scan() {
				idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
				if err != nil || idx < 1 || idx > len(found) {
					return fmt.Errorf("invalid selection")
				}
				absDir = found[idx-1].Path
				suggestedName = found[idx-1].Name
			}
		}
	}

	fmt.Printf("\n🔑 Found encrypted Grim vault at: %s\n", absDir)
	passphrase, err := promptPassword("Enter master passphrase for this vault: ")
	if err != nil {
		return err
	}

	meta, err := vault.VerifyPassphrase(absDir, passphrase)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	vaultName := suggestedName
	if meta.VaultName != "" && meta.VaultName != "default" {
		vaultName = meta.VaultName
	}

	fmt.Printf("Enter alias/name for this vault [default: %s]: ", vaultName)
	if scanner.Scan() {
		inputName := strings.TrimSpace(scanner.Text())
		if inputName != "" {
			vaultName = inputName
		}
	}

	if _, exists := cfg.Vaults[vaultName]; exists {
		fmt.Printf("⚠️  Vault '%s' already exists in config. Overwrite path? [y/N]: ", vaultName)
		if scanner.Scan() {
			ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if ans != "y" && ans != "yes" {
				fmt.Println("Action cancelled.")
				return nil
			}
		}
	}

	cfg.Vaults[vaultName] = config.VaultConfig{
		Path:           absDir,
		Editor:         cfg.DefaultEditor,
		TimeoutMinutes: 30,
	}

	if cfg.DefaultVault == "" {
		cfg.DefaultVault = vaultName
	}

	if err := cfg.Save(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n🎉 Vault '%s' successfully attached and saved to config!\n", vaultName)
	fmt.Printf("👉 To open your notes, run: grim open %s\n", vaultName)
	return nil
}

var openCmd = &cobra.Command{
	Use:    "open [vault]",
	Hidden: true,
	Short:  "Unlock a vault into RAM, start live sync, and launch editor",
	Args:   cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vaultName := ""
		if len(args) > 0 {
			vaultName = args[0]
		}
		return executeOpen(vaultName)
	},
}

var lockCmd = &cobra.Command{
	Use:   "lock [vault]",
	Short: "Force lock a vault and wipe its RAM workspace",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		vaultName := ""
		if len(args) > 0 {
			vaultName = args[0]
		} else {
			vaultName = cfg.DefaultVault
		}

		if vaultName == "" {
			return fmt.Errorf("no vault specified and no default vault configured")
		}

		wsPath := ramdisk.GetPath(vaultName)
		if err := ramdisk.WipeAndRemove(wsPath); err != nil {
			return fmt.Errorf("failed to wipe RAM workspace: %w", err)
		}

		runner.UnregisterObsidianVault(wsPath)
		fmt.Printf("🧹 RAM workspace for '%s' wiped and locked.\n", vaultName)
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all configured vaults and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		if len(cfg.Vaults) == 0 {
			fmt.Println("No vaults configured yet.")
			fmt.Println("👉 Create a new vault:       grim init <vault> <dir>")
			fmt.Println("👉 Attach an existing vault:  grim add")
			return nil
		}

		fmt.Println("Configured Vaults:")
		fmt.Println("------------------------------------------------------------")
		for name, v := range cfg.Vaults {
			defMarker := " "
			if name == cfg.DefaultVault {
				defMarker = "*"
			}

			status := "🔒 LOCKED"
			if ramdisk.IsUnlocked(name) {
				status = "🔓 UNLOCKED (in RAM)"
			} else if _, err := os.Stat(filepath.Join(v.Path, vault.MetaFileName)); os.IsNotExist(err) {
				status = "❓ NOT FOUND ON DISK"
			}

			ed := v.Editor
			if ed == "" {
				ed = cfg.DefaultEditor
			}
			if ed == "" {
				ed = "obsidian"
			}

			fmt.Printf("%s [%s] %s\n", defMarker, name, status)
			fmt.Printf("    Path:    %s\n", v.Path)
			fmt.Printf("    Editor:  %s\n", ed)
			fmt.Printf("    Timeout: %d min\n\n", v.TimeoutMinutes)
		}
		fmt.Println("(* indicates default vault)")
		return nil
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove <vault>",
	Aliases: []string{"rm"},
	Short:   "Remove a vault from configuration (does not delete files from disk)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		if _, exists := cfg.Vaults[name]; !exists {
			return fmt.Errorf("vault '%s' not found in configuration", name)
		}

		delete(cfg.Vaults, name)
		if cfg.DefaultVault == name {
			cfg.DefaultVault = ""
			for k := range cfg.Vaults {
				cfg.DefaultVault = k
				break
			}
		}

		if err := cfg.Save(""); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("🗑️  Vault '%s' successfully removed from configuration.\n", name)
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"st"},
	Short:   "Check unlocked vaults in RAM",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		unlockedCount := 0
		for name, v := range cfg.Vaults {
			if ramdisk.IsUnlocked(name) {
				unlockedCount++
				fmt.Printf("🔓 Active: %s -> RAM: %s (Source: %s)\n", name, ramdisk.GetPath(name), v.Path)
			}
		}

		if unlockedCount == 0 {
			fmt.Println("🔒 All vaults are locked.")
		}
		return nil
	},
}

var setDefaultCmd = &cobra.Command{
	Use:   "set-default <vault>",
	Short: "Set the default vault",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		if _, exists := cfg.Vaults[name]; !exists {
			return fmt.Errorf("vault '%s' is not in configuration", name)
		}

		cfg.DefaultVault = name
		if err := cfg.Save(""); err != nil {
			return err
		}

		fmt.Printf("⭐ Default vault set to '%s'\n", name)
		return nil
	},
}

func executeOpen(vaultName string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return err
	}

	if vaultName == "" {
		vaultName = cfg.DefaultVault
	}

	if vaultName == "" {
		if len(cfg.Vaults) == 1 {
			for k := range cfg.Vaults {
				vaultName = k
				break
			}
		} else {
			return fmt.Errorf("no vault specified and no default vault found. Run 'grim list'")
		}
	}

	vCfg, exists := cfg.Vaults[vaultName]
	if !exists {
		return fmt.Errorf("vault '%s' not configured. Run 'grim list'", vaultName)
	}

	// Check if vault files actually exist on disk
	metaFile := filepath.Join(vCfg.Path, vault.MetaFileName)
	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		fmt.Printf("⚠️  Vault '%s' was not found on disk at: %s\n", vaultName, vCfg.Path)
		fmt.Print("Would you like to remove this missing vault from your configuration? [Y/n]: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if ans == "" || ans == "y" || ans == "yes" {
				delete(cfg.Vaults, vaultName)
				if cfg.DefaultVault == vaultName {
					cfg.DefaultVault = ""
					for k := range cfg.Vaults {
						cfg.DefaultVault = k
						break
					}
				}
				if err := cfg.Save(""); err != nil {
					return fmt.Errorf("failed to update config: %w", err)
				}
				fmt.Printf("🗑️  Vault '%s' removed from configuration.\n", vaultName)
				return nil
			}
		}
		return fmt.Errorf("vault metadata missing at %s", vCfg.Path)
	}

	if ramdisk.IsUnlocked(vaultName) {
		fmt.Printf("⚠️  Vault '%s' is ALREADY UNLOCKED in RAM.\n", vaultName)
		wsPath := ramdisk.GetPath(vaultName)
		ed := vCfg.Editor
		if ed == "" {
			ed = cfg.DefaultEditor
		}
		if ed == "" {
			ed = "obsidian"
		}
		fmt.Printf("🚀 Launching editor (%s) pointing to existing RAM workspace...\n", ed)
		_, _ = runner.Launch(ed, wsPath)
		return nil
	}

	fmt.Printf("🔑 Opening Grim vault '%s' (%s)\n", vaultName, vCfg.Path)
	passphrase, err := promptPassword("Enter master passphrase: ")
	if err != nil {
		return err
	}

	// 1. Verify passphrase & retrieve Master Key
	meta, err := vault.VerifyPassphrase(vCfg.Path, passphrase)
	if err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}
	editor := vCfg.Editor
	if editor == "" {
		editor = cfg.DefaultEditor
	}
	if editor == "" {
		editor = "obsidian"
	}

	// 2. Create isolated RAM workspace
	ws, err := ramdisk.NewForEditor(vaultName, editor)
	if err != nil {
		return fmt.Errorf("failed to create RAM workspace: %w", err)
	}

	// 3. Decrypt all notes into RAM
	fmt.Printf("📂 Decrypting files into in-memory workspace at %s...\n", ws.Path)
	if err := vault.Unlock(vCfg.Path, ws.Path, meta); err != nil {
		_ = ws.Destroy()
		return fmt.Errorf("failed to decrypt vault files: %w", err)
	}

	// 4. Inactivity Timer setup
	var timer *time.Timer
	var timerMu sync.Mutex
	var timeoutChan <-chan time.Time

	resetInactivityTimer := func() {
		if vCfg.TimeoutMinutes <= 0 {
			return
		}
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(time.Duration(vCfg.TimeoutMinutes) * time.Minute)
		}
	}

	if vCfg.TimeoutMinutes > 0 {
		timer = time.NewTimer(time.Duration(vCfg.TimeoutMinutes) * time.Minute)
		timeoutChan = timer.C
		defer timer.Stop()
	}

	// 5. Start background continuous sync with timer reset on edits
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = vault.WatchAndSync(ctx, ws.Path, vCfg.Path, meta.PublicKey, func(event, relPath string) {
			resetInactivityTimer()
			fmt.Printf("\r⚡ [%s] Auto-encrypted: %s\n> ", event, relPath)
		})
	}()

	// 6. Launch editor
	fmt.Printf("🚀 Launching editor (%s) pointing to RAM workspace...\n", editor)
	proc, err := runner.Launch(editor, ws.Path)
	if err != nil {
		fmt.Printf("⚠️  Note: Could not automatically launch editor: %v\n", err)
		fmt.Printf("👉 You can manually open folder: %s in your editor.\n", ws.Path)
	} else {
		fmt.Printf("📌 Editor launched (PID: %d).\n", proc.PID)
	}

	// 7. Interactive lock listener & inactivity timeout
	fmt.Println("\n=======================================================")
	fmt.Printf("🛡️  SESSION ACTIVE: Vault '%s' is UNLOCKED in RAM.\n", vaultName)
	if vCfg.TimeoutMinutes > 0 {
		fmt.Printf("⏱️  Inactivity auto-lock timeout: %d minutes (resets on file saves).\n", vCfg.TimeoutMinutes)
	}
	fmt.Println("👉 Press Ctrl+C or type 'lock' / 'q' to safely close & wipe RAM.")
	fmt.Println("=======================================================")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	inputChan := make(chan string)
	if proc == nil || !proc.IsTerminal {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for {
				fmt.Print("> ")
				if scanner.Scan() {
					text := strings.TrimSpace(scanner.Text())
					if text != "" {
						resetInactivityTimer()
						inputChan <- text
					}
				} else {
					return
				}
			}
		}()
	}

	var editorWait <-chan error
	if proc != nil && proc.IsTerminal {
		editorWait = proc.WaitChan
	}

	select {
	case <-sigChan:
		fmt.Println("\n🔒 Interrupt signal received. Locking vault...")
	case input := <-inputChan:
		if input == "lock" || input == "q" || input == "exit" || input == "close" {
			fmt.Println("\n🔒 Lock command received. Locking vault...")
		}
	case <-timeoutChan:
		fmt.Printf("\n⏰ %d minutes of inactivity reached! Automatically locking vault...\n", vCfg.TimeoutMinutes)
	case <-editorWait:
		fmt.Println("\n🚪 Editor closed. Locking vault...")
	}

	// Stop watcher
	cancel()

	// Terminate editor gracefully
	if proc != nil {
		fmt.Println("🛑 Closing editor...")
		_ = proc.Stop()
	}

	// Lock, final sync, and wipe RAM
	fmt.Println("🧹 Syncing final changes and securely wiping RAM workspace...")
	if err := vault.Lock(ws.Path, vCfg.Path, meta.PublicKey); err != nil {
		return fmt.Errorf("error during lock: %w", err)
	}

	runner.UnregisterObsidianVault(ws.Path)
	fmt.Println("✨ Vault safely locked. All temporary plaintext wiped from RAM.")
	return nil
}

type EditorOption struct {
	Name        string
	DisplayName string
	Binary      string
	Detected    bool
}

func getKnownEditors() []EditorOption {
	editors := []EditorOption{
		{Name: "obsidian", DisplayName: "Obsidian Markdown Editor", Binary: "obsidian"},
		{Name: "code", DisplayName: "Visual Studio Code (code)", Binary: "code"},
		{Name: "vim", DisplayName: "Vim (console text editor)", Binary: "vim"},
		{Name: "nvim", DisplayName: "Neovim (console text editor)", Binary: "nvim"},
		{Name: "nano", DisplayName: "GNU Nano (console text editor)", Binary: "nano"},
		{Name: "micro", DisplayName: "Micro (console text editor)", Binary: "micro"},
		{Name: "hx", DisplayName: "Helix (console text editor)", Binary: "hx"},
		{Name: "subl", DisplayName: "Sublime Text", Binary: "subl"},
	}

	for i := range editors {
		if _, err := exec.LookPath(editors[i].Binary); err == nil {
			editors[i].Detected = true
		} else if editors[i].Name == "obsidian" {
			// Check flatpak obsidian
			if _, err := exec.LookPath("flatpak"); err == nil {
				out, err := exec.Command("flatpak", "info", "md.obsidian.Obsidian").Output()
				if err == nil && len(out) > 0 {
					editors[i].Detected = true
				}
			}
		}
	}

	return editors
}

var setupCmd = &cobra.Command{
	Use:   "setup [command-or-vault] [value]",
	Short: "Interactive settings wizard (editor, timeout, passphrase, vaults)",
	Long: `Interactive configuration wizard or direct CLI setter:
  grim setup                 # Interactive settings wizard
  grim setup work            # Configure settings for vault 'work'
  grim setup editor vim      # Set global default editor to vim
  grim setup passwd work     # Change master passphrase for vault 'work'
  grim setup timeout 20      # Set default timeout for all vaults`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig("")
		if err != nil {
			return err
		}

		if len(args) == 1 {
			arg := args[0]
			if v, exists := cfg.Vaults[arg]; exists {
				return configureVaultInteractive(cfg, arg, v)
			}
			if arg == "passwd" || arg == "password" {
				return changePassphraseInteractive(cfg, "")
			}
			if arg == "editor" {
				return selectEditorInteractive(cfg)
			}
			cfg.DefaultEditor = arg
			if err := cfg.Save(""); err != nil {
				return err
			}
			fmt.Printf("✅ Global default editor set to '%s'\n", arg)
			return nil
		}

		if len(args) == 2 {
			sub := args[0]
			val := args[1]

			switch sub {
			case "passwd", "password":
				return changePassphraseInteractive(cfg, val)
			case "editor":
				cfg.DefaultEditor = val
				if err := cfg.Save(""); err != nil {
					return err
				}
				fmt.Printf("✅ Global default editor set to '%s'\n", val)
				return nil
			case "timeout":
				to, err := strconv.Atoi(val)
				if err != nil || to <= 0 {
					return fmt.Errorf("invalid timeout minutes: %s", val)
				}
				for k, v := range cfg.Vaults {
					v.TimeoutMinutes = to
					cfg.Vaults[k] = v
				}
				if err := cfg.Save(""); err != nil {
					return err
				}
				fmt.Printf("✅ Timeout for all vaults set to %d minutes\n", to)
				return nil
			default:
				v, exists := cfg.Vaults[sub]
				if !exists {
					return fmt.Errorf("vault '%s' not found", sub)
				}
				v.Editor = val
				cfg.Vaults[sub] = v
				if err := cfg.Save(""); err != nil {
					return err
				}
				fmt.Printf("✅ Editor for vault '%s' set to '%s'\n", sub, val)
				return nil
			}
		}

		return runInteractiveSetup(cfg)
	},
}

func runInteractiveSetup(cfg *config.Config) error {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Println("\n📖 Grim Setup & Settings")
		fmt.Println("==================================================")
		fmt.Printf("1) Select Default Editor      (current: %s)\n", cfg.DefaultEditor)
		fmt.Println("2) Change Auto-lock Timeout")
		fmt.Println("3) Change Vault Master Passphrase (re-encrypt)")
		fmt.Println("4) Configure Specific Vault Settings")
		fmt.Printf("5) Set Default Vault          (current: %s)\n", cfg.DefaultVault)
		fmt.Println("0) Exit")
		fmt.Println("==================================================")
		fmt.Print("Choose option [0-5]: ")

		if !scanner.Scan() {
			return nil
		}
		choice := strings.TrimSpace(scanner.Text())

		switch choice {
		case "1":
			_ = selectEditorInteractive(cfg)
		case "2":
			_ = changeTimeoutInteractive(cfg)
		case "3":
			_ = changePassphraseInteractive(cfg, "")
		case "4":
			_ = selectVaultToConfigure(cfg)
		case "5":
			_ = setDefaultVaultInteractive(cfg)
		case "0", "q", "exit":
			fmt.Println("👋 Exiting setup.")
			return nil
		default:
			fmt.Println("⚠️ Invalid option.")
		}
	}
}

func promptEditorSelection(currentEditor string) string {
	scanner := bufio.NewScanner(os.Stdin)
	editors := getKnownEditors()
	for i, ed := range editors {
		detectedStatus := ""
		if ed.Detected {
			detectedStatus = " (detected on system)"
		}
		currentMarker := " "
		if ed.Name == currentEditor {
			currentMarker = "*"
		}
		fmt.Printf(" %s %d) %s%s\n", currentMarker, i+1, ed.DisplayName, detectedStatus)
	}
	fmt.Printf("   %d) Custom command...\n\n", len(editors)+1)

	fmt.Printf("Select editor [1-%d] (press Enter to keep '%s'): ", len(editors)+1, currentEditor)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			idx, err := strconv.Atoi(text)
			if err == nil && idx >= 1 && idx <= len(editors) {
				return editors[idx-1].Name
			} else if err == nil && idx == len(editors)+1 {
				fmt.Print("Enter custom editor command: ")
				if scanner.Scan() {
					customCmd := strings.TrimSpace(scanner.Text())
					if customCmd != "" {
						return customCmd
					}
				}
			} else {
				return text
			}
		}
	}
	return currentEditor
}

func selectEditorInteractive(cfg *config.Config) error {
	fmt.Println("\n📝 Select Default Text Editor")
	fmt.Println("--------------------------------------------------")
	cfg.DefaultEditor = promptEditorSelection(cfg.DefaultEditor)

	if err := cfg.Save(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Default editor set to: %s\n", cfg.DefaultEditor)
	return nil
}

func changeTimeoutInteractive(cfg *config.Config) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("\nEnter default auto-lock timeout in minutes [1-1440]: ")
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			to, err := strconv.Atoi(text)
			if err != nil || to <= 0 {
				fmt.Println("⚠️ Invalid number of minutes.")
				return nil
			}
			for k, v := range cfg.Vaults {
				v.TimeoutMinutes = to
				cfg.Vaults[k] = v
			}
			if err := cfg.Save(""); err != nil {
				return err
			}
			fmt.Printf("✅ Auto-lock timeout set to %d minutes for all vaults.\n", to)
		}
	}
	return nil
}

func changePassphraseInteractive(cfg *config.Config, vaultName string) error {
	if len(cfg.Vaults) == 0 {
		fmt.Println("No vaults configured.")
		return nil
	}

	if vaultName == "" {
		if len(cfg.Vaults) == 1 {
			for k := range cfg.Vaults {
				vaultName = k
				break
			}
		} else {
			fmt.Println("\nSelect vault to change passphrase:")
			var names []string
			for k := range cfg.Vaults {
				names = append(names, k)
				fmt.Printf(" %d) %s\n", len(names), k)
			}
			fmt.Printf("Choice [1-%d]: ", len(names))
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
				if err != nil || idx < 1 || idx > len(names) {
					fmt.Println("⚠️ Invalid selection.")
					return nil
				}
				vaultName = names[idx-1]
			}
		}
	}

	vCfg, exists := cfg.Vaults[vaultName]
	if !exists {
		return fmt.Errorf("vault '%s' not found", vaultName)
	}

	if ramdisk.IsUnlocked(vaultName) {
		return fmt.Errorf("vault '%s' is currently unlocked in RAM. Please lock it first ('grim lock %s')", vaultName, vaultName)
	}

	fmt.Printf("\n🔑 Changing master passphrase for vault '%s' (%s)\n", vaultName, vCfg.Path)
	oldPass, err := promptPassword("Enter CURRENT master passphrase: ")
	if err != nil {
		return err
	}

	if _, err := vault.VerifyPassphrase(vCfg.Path, oldPass); err != nil {
		fmt.Println("❌ Current passphrase is incorrect.")
		return nil
	}

	newPass1, err := promptPassword("Enter NEW master passphrase: ")
	if err != nil {
		return err
	}
	if len(newPass1) < 4 {
		fmt.Println("❌ New passphrase is too short (min 4 chars).")
		return nil
	}

	newPass2, err := promptPassword("Confirm NEW master passphrase: ")
	if err != nil {
		return err
	}

	if newPass1 != newPass2 {
		fmt.Println("❌ Passphrases do not match.")
		return nil
	}

	fmt.Println("⏳ Re-encrypting master key metadata with new passphrase...")
	if err := vault.ChangePassphrase(vCfg.Path, oldPass, newPass1); err != nil {
		return fmt.Errorf("failed to change passphrase: %w", err)
	}

	fmt.Printf("🎉 Master passphrase for vault '%s' successfully changed!\n", vaultName)
	return nil
}

func selectVaultToConfigure(cfg *config.Config) error {
	if len(cfg.Vaults) == 0 {
		fmt.Println("No vaults configured.")
		return nil
	}

	fmt.Println("\nSelect vault to configure:")
	var names []string
	for k := range cfg.Vaults {
		names = append(names, k)
		fmt.Printf(" %d) %s (editor: %s, timeout: %d min)\n", len(names), k, cfg.Vaults[k].Editor, cfg.Vaults[k].TimeoutMinutes)
	}
	fmt.Printf("Choice [1-%d]: ", len(names))
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if err != nil || idx < 1 || idx > len(names) {
			fmt.Println("⚠️ Invalid selection.")
			return nil
		}
		vaultName := names[idx-1]
		return configureVaultInteractive(cfg, vaultName, cfg.Vaults[vaultName])
	}
	return nil
}

func setDefaultVaultInteractive(cfg *config.Config) error {
	if len(cfg.Vaults) == 0 {
		fmt.Println("No vaults configured.")
		return nil
	}

	fmt.Println("\nSelect default vault:")
	var names []string
	for k := range cfg.Vaults {
		names = append(names, k)
		defMarker := " "
		if k == cfg.DefaultVault {
			defMarker = "*"
		}
		fmt.Printf(" %s %d) %s\n", defMarker, len(names), k)
	}
	fmt.Printf("Choice [1-%d]: ", len(names))
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if err != nil || idx < 1 || idx > len(names) {
			fmt.Println("⚠️ Invalid selection.")
			return nil
		}
		cfg.DefaultVault = names[idx-1]
		if err := cfg.Save(""); err != nil {
			return err
		}
		fmt.Printf("⭐ Default vault set to '%s'\n", cfg.DefaultVault)
	}
	return nil
}

func configureVaultInteractive(cfg *config.Config, vaultName string, v config.VaultConfig) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("\n⚙️  Configuring Vault: '%s'\n", vaultName)
	fmt.Printf("Current editor:  [%s]\n", v.Editor)
	fmt.Printf("Current timeout: [%d min]\n\n", v.TimeoutMinutes)

	fmt.Println("Select editor for this vault:")
	v.Editor = promptEditorSelection(v.Editor)

	fmt.Print("\nEnter auto-lock timeout in minutes (or press Enter to keep): ")
	if scanner.Scan() {
		toStr := strings.TrimSpace(scanner.Text())
		if toStr != "" {
			if val, err := strconv.Atoi(toStr); err == nil && val > 0 {
				v.TimeoutMinutes = val
			}
		}
	}

	cfg.Vaults[vaultName] = v
	if err := cfg.Save(""); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Vault '%s' configuration updated! (Editor: %s, Timeout: %d min)\n", vaultName, v.Editor, v.TimeoutMinutes)
	return nil
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install grim binary to system PATH (/usr/local/bin or ~/.local/bin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to determine executable path: %w", err)
		}

		targetDirs := []string{"/usr/local/bin", filepath.Join(os.Getenv("HOME"), ".local", "bin")}
		var installedPath string

		// Try /usr/local/bin first
		targetPath := "/usr/local/bin/grim"
		if err := copyExecutable(exePath, targetPath); err == nil {
			installedPath = targetPath
		} else {
			// Try ~/.local/bin
			userBin := filepath.Join(os.Getenv("HOME"), ".local", "bin")
			_ = os.MkdirAll(userBin, 0755)
			targetPath = filepath.Join(userBin, "grim")
			if err := copyExecutable(exePath, targetPath); err == nil {
				installedPath = targetPath
			} else {
				return fmt.Errorf("failed to install to %v (try running with sudo: 'sudo grim install'): %w", targetDirs, err)
			}
		}

		fmt.Printf("🎉 Grim successfully installed to: %s\n", installedPath)
		fmt.Println("👉 You can now run 'grim' from any terminal!")
		fmt.Println("\n💡 Tip: To enable shell completion in bash:")
		fmt.Println("  echo 'source <(grim completion bash)' >> ~/.bashrc")
		return nil
	},
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "\"'")

	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand home dir: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	} else if strings.HasPrefix(path, "$") {
		path = os.ExpandEnv(path)
	}

	return filepath.Abs(path)
}

func promptPassword(promptText string) (string, error) {
	fmt.Print(promptText)

	fd := int(syscall.Stdin)
	if !term.IsTerminal(fd) {
		reader := bufio.NewReader(os.Stdin)
		pass, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(pass), nil
	}

	bytePassword, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytePassword)), nil
}

func main() {
	// Auto-cleanup any stale orphaned RAM workspaces from previous crashes
	if cleaned := ramdisk.CleanupStaleWorkspaces(); len(cleaned) > 0 {
		for _, p := range cleaned {
			fmt.Printf("🛡️  Securely wiped stale unencrypted workspace from previous session: %s\n", p)
		}
	}

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(lockCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(setDefaultCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.SetUsageTemplate(strings.ReplaceAll(rootCmd.UsageTemplate(), "{{rpad .Name .NamePadding }}", "{{rpad .NameAndAliases .NamePadding }}"))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
