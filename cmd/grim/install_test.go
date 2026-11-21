package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallTargets(t *testing.T) {
	home := filepath.Join("home", "user")
	targets := installTargets(home)
	if len(targets) != 2 {
		t.Fatalf("installTargets() returned %d targets, want 2", len(targets))
	}
	if targets[0] != "/usr/local/bin/grim" {
		t.Fatalf("first target = %q", targets[0])
	}
	wantUser := filepath.Join(home, ".local", "bin", "grim")
	if targets[1] != wantUser {
		t.Fatalf("second target = %q, want %q", targets[1], wantUser)
	}
}

func TestCopyExecutableAtomic(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "bin")
	src := filepath.Join(srcDir, "grim-src")
	dst := filepath.Join(dstDir, "grim")

	if err := os.WriteFile(src, []byte("binary-v1"), 0755); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}
	if err := copyExecutableAtomic(src, dst); err != nil {
		t.Fatalf("copyExecutableAtomic failed: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read destination: %v", err)
	}
	if string(data) != "binary-v1" {
		t.Fatalf("destination content = %q", data)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("failed to stat destination: %v", err)
		}
		if info.Mode().Perm()&0111 == 0 {
			t.Fatalf("destination is not executable: %v", info.Mode())
		}
	}

	leftovers, err := filepath.Glob(filepath.Join(dstDir, ".grim-install-*"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary install files were left behind: %v", leftovers)
	}

	if err := os.WriteFile(src, []byte("binary-v2"), 0755); err != nil {
		t.Fatalf("failed to update source: %v", err)
	}
	if err := copyExecutableAtomic(src, dst); err != nil {
		t.Fatalf("second copyExecutableAtomic failed: %v", err)
	}
	data, err = os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read overwritten destination: %v", err)
	}
	if string(data) != "binary-v2" {
		t.Fatalf("overwritten destination content = %q", data)
	}
}

func TestCopyExecutableAtomicSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grim")
	if err := os.WriteFile(path, []byte("binary"), 0755); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := copyExecutableAtomic(path, path); err != nil {
		t.Fatalf("same-path copy must be a no-op: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(data) != "binary" {
		t.Fatalf("same-path copy changed the file: %q", data)
	}
}

func TestCopyExecutableAtomicFailureLeavesNoTempFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "grim-src")
	if err := os.WriteFile(src, []byte("binary"), 0755); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	// Renaming a file over an existing directory must fail.
	dst := filepath.Join(dstDir, "grim")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("failed to create blocking directory: %v", err)
	}

	if err := copyExecutableAtomic(src, dst); err == nil {
		t.Fatalf("copyExecutableAtomic must fail when the destination is a directory")
	}

	leftovers, err := filepath.Glob(filepath.Join(dstDir, ".grim-install-*"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary install files were left behind after failure: %v", leftovers)
	}
}
