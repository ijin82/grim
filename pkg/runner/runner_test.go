package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunnerCommandResolution(t *testing.T) {
	cmd, args := resolveEditorCommand("code", "/tmp/test")
	if len(args) != 1 || args[0] != "/tmp/test" {
		t.Errorf("Unexpected args: %v", args)
	}
	if cmd == "" {
		t.Errorf("Expected non-empty cmd")
	}

	cmdCustom, argsCustom := resolveEditorCommand("subl -n", "/tmp/test")
	if cmdCustom != "subl" || len(argsCustom) != 2 || argsCustom[0] != "-n" || argsCustom[1] != "/tmp/test" {
		t.Errorf("Unexpected custom command resolution: %s %v", cmdCustom, argsCustom)
	}

	cmdObs, argsObs := resolveEditorCommand("obsidian", "/tmp/test-vault")
	if len(argsObs) != 1 || !strings.Contains(argsObs[0], "obsidian://open?path=") {
		t.Errorf("Expected obsidian URI argument, got %v", argsObs)
	}
	if cmdObs == "" {
		t.Errorf("Expected non-empty cmd for obsidian")
	}
}

func TestRunnerLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based lifecycle test is Unix-specific")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	script := filepath.Join(t.TempDir(), "fake-editor.sh")
	body := "#!/bin/sh\ntrap 'exit 0' INT TERM\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatalf("failed to write fake editor: %v", err)
	}

	target := t.TempDir()
	proc, err := Launch(script, target)
	if err != nil {
		t.Fatalf("failed to launch fake editor: %v", err)
	}
	if proc.PID <= 0 {
		t.Fatalf("expected valid PID, got %d", proc.PID)
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("failed to stop fake editor: %v", err)
	}
}

func TestCloseEditorApplicationNonObsidian(t *testing.T) {
	if err := CloseEditorApplication("vim"); err != nil {
		t.Fatalf("CloseEditorApplication(vim) returned error: %v", err)
	}
}
