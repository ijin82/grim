package runner

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type EditorProcess struct {
	Cmd        *exec.Cmd
	PID        int
	TargetPath string
	IsObsidian bool
	IsTerminal bool
	WaitChan   chan error
}

// Launch starts the requested editor pointing to the target directory.
func Launch(editorName string, targetPath string) (*EditorProcess, error) {
	if editorName == "" {
		editorName = "obsidian"
	}

	lower := strings.ToLower(strings.Fields(editorName)[0])
	isObsidian := lower == "obsidian"
	isTerminal := isTerminalEditor(lower)

	if isObsidian {
		// Automatically register the vault path in obsidian.json so Obsidian knows it
		RegisterObsidianVault(targetPath)
	}

	cmdName, args := resolveEditorCommand(editorName, targetPath)

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = targetPath

	if isTerminal {
		// Terminal editors need interactive TTY
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		// Suppress noisy Chromium / Electron / GTK logs from polluting the interactive CLI terminal
		logFile := getEditorLogFile()
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		} else {
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
		}
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to launch editor '%s': %w", cmdName, err)
	}

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	waitChan := make(chan error, 1)
	go func() {
		waitChan <- cmd.Wait()
	}()

	return &EditorProcess{
		Cmd:        cmd,
		PID:        pid,
		TargetPath: targetPath,
		IsObsidian: isObsidian,
		IsTerminal: isTerminal,
		WaitChan:   waitChan,
	}, nil
}

func isTerminalEditor(name string) bool {
	switch name {
	case "vim", "vi", "nvim", "nano", "micro", "helix", "hx", "emacs", "kak":
		return true
	default:
		return false
	}
}

func getEditorLogFile() *os.File {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	logDir := filepath.Join(home, ".config", "cryptovault")
	_ = os.MkdirAll(logDir, 0700)
	f, err := os.OpenFile(filepath.Join(logDir, "editor.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil
	}
	return f
}

// resolveEditorCommand finds the right executable and arguments.
func resolveEditorCommand(editorName string, targetPath string) (string, []string) {
	lower := strings.ToLower(editorName)

	switch lower {
	case "obsidian":
		obsidianURI := fmt.Sprintf("obsidian://open?path=%s", url.QueryEscape(targetPath))

		if path, err := exec.LookPath("obsidian"); err == nil {
			return path, []string{obsidianURI}
		}
		if runtime.GOOS == "darwin" {
			// macOS app bundle
			return "open", []string{obsidianURI}
		}
		// Try xdg-open on Linux first if available
		if path, err := exec.LookPath("xdg-open"); err == nil {
			return path, []string{obsidianURI}
		}
		// Flatpak fallback on Linux
		if path, err := exec.LookPath("flatpak"); err == nil {
			return path, []string{"run", "--filesystem=" + targetPath, "md.obsidian.Obsidian", obsidianURI}
		}
		return "obsidian", []string{obsidianURI}

	case "code", "vscode":
		if path, err := exec.LookPath("code"); err == nil {
			return path, []string{targetPath}
		}
		return "code", []string{targetPath}

	case "vim", "vi", "nvim":
		welcomePath := filepath.Join(targetPath, "Welcome.md")
		if _, err := os.Stat(welcomePath); err == nil {
			return editorName, []string{welcomePath}
		}
		return editorName, []string{"."}

	default:
		// Split custom command string if any
		parts := strings.Fields(editorName)
		if len(parts) > 1 {
			return parts[0], append(parts[1:], targetPath)
		}
		return editorName, []string{targetPath}
	}
}

// Stop gracefully signals the editor to terminate and cleans up registrations.
func (p *EditorProcess) Stop() error {
	if p == nil {
		return nil
	}

	if p.IsObsidian && p.TargetPath != "" {
		UnregisterObsidianVault(p.TargetPath)
	}

	if p.Cmd == nil || p.Cmd.Process == nil {
		return nil
	}

	// Try graceful interrupt
	_ = p.Cmd.Process.Signal(os.Interrupt)

	select {
	case <-p.WaitChan:
		return nil
	case <-time.After(2 * time.Second):
		// Force kill if not exited within 2s
		_ = p.Cmd.Process.Kill()
		return nil
	}
}
