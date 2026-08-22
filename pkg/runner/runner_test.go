package runner

import (
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
	// Launch a dummy sleep process
	proc, err := Launch("sleep 10", "/tmp")
	if err != nil {
		t.Fatalf("Failed to launch test process: %v", err)
	}

	if proc.PID <= 0 {
		t.Fatalf("Expected valid PID, got %d", proc.PID)
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}
}
