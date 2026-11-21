package ramdisk

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFakeCommand(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("failed to create fake command dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatalf("failed to write fake command: %v", err)
	}
	return path
}

func TestIsFlatpakEditorDetection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell command detection is Unix-specific")
	}
	const installedFlatpak = "#!/bin/sh\n[ \"$1\" = \"info\" ] && [ \"$2\" = \"md.obsidian.Obsidian\" ] && exit 0\nexit 1\n"
	const missingFlatpak = "#!/bin/sh\nexit 1\n"

	t.Run("specific Obsidian Flatpak installed", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeCommand(t, bin, "flatpak", installedFlatpak)
		t.Setenv("PATH", bin)

		if !isFlatpakEditor("obsidian") {
			t.Fatalf("expected Obsidian Flatpak to be detected")
		}
	})

	t.Run("unrelated Flatpak does not enable cache mode", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeCommand(t, bin, "flatpak", missingFlatpak)
		t.Setenv("PATH", bin)

		if isFlatpakEditor("obsidian") {
			t.Fatalf("an installed flatpak binary without Obsidian must not be treated as Flatpak Obsidian")
		}
	})

	t.Run("native Obsidian wins over Flatpak", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeCommand(t, bin, "flatpak", installedFlatpak)
		writeFakeCommand(t, bin, "obsidian", "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", bin)

		if isFlatpakEditor("obsidian") {
			t.Fatalf("native Obsidian must take precedence over Flatpak detection")
		}
	})

	t.Run("snap wrapper", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeCommand(t, filepath.Join(bin, "snap"), "obsidian", "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", filepath.Join(bin, "snap"))

		if !isFlatpakEditor("obsidian") {
			t.Fatalf("expected snap-based Obsidian to use the cache workspace")
		}
	})

	t.Run("non-Obsidian editor", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeCommand(t, bin, "flatpak", installedFlatpak)
		t.Setenv("PATH", bin)

		if isFlatpakEditor("vim") {
			t.Fatalf("non-Obsidian editors must use tmpfs, not the cache")
		}
	})
}
