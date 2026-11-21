package runner

import (
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// CloseEditorApplication closes an editor application that can outlive the
// launcher process. Obsidian launched through xdg-open or a Flatpak wrapper is
// the main case: stopping the wrapper does not necessarily close the GUI.
func CloseEditorApplication(editorName string) error {
	lower := strings.ToLower(strings.TrimSpace(editorName))
	if lower == "" {
		lower = "obsidian"
	}
	if !strings.Contains(lower, "obsidian") {
		return nil
	}
	return closeObsidianApp()
}

func closeObsidianApp() error {
	switch runtime.GOOS {
	case "linux":
		// Try the Flatpak app first, then any native Obsidian process. Both
		// calls are best-effort: a missing process is not an error.
		if flatpakPath, err := exec.LookPath("flatpak"); err == nil {
			_ = exec.Command(flatpakPath, "kill", "md.obsidian.Obsidian").Run()
		}
		if pkillPath, err := exec.LookPath("pkill"); err == nil {
			_ = exec.Command(pkillPath, "-TERM", "-x", "obsidian").Run()
		}
		time.Sleep(1 * time.Second)
		return nil

	case "darwin":
		if osascriptPath, err := exec.LookPath("osascript"); err == nil {
			_ = exec.Command(osascriptPath, "-e", `tell application "Obsidian" to quit`).Run()
			time.Sleep(1 * time.Second)
		}
		return nil

	case "windows":
		if taskkillPath, err := exec.LookPath("taskkill"); err == nil {
			// Send a graceful close first; Obsidian may show a save prompt.
			_ = exec.Command(taskkillPath, "/IM", "Obsidian.exe", "/T").Run()
			time.Sleep(1 * time.Second)
		}
		return nil

	default:
		return nil
	}
}
