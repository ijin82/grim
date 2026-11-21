# Grim v0.3.0

Release date: 2026-11-21

Grim v0.3.0 focuses on robustness, portability, performance, and release
engineering. There are no vault-format changes; existing encrypted vaults and
configuration files remain compatible.

## Highlights

- More accurate Obsidian Flatpak/Snap detection.
- Streaming file encryption, decryption, and SHA-256 hashing.
- Atomic configuration writes and atomic `grim install`.
- Watcher and Obsidian registry errors are no longer silently ignored.
- Hermetic test suite with isolated HOME/cache/runtime directories.
- CI workflow and a `make release` helper.
- New `SECURITY.md` with the threat model and vulnerability-reporting process.

## Added

- GitHub Actions CI for Ubuntu and macOS, plus a Windows/amd64 cross-build job.
- `make release VERSION=x.y.z` and `scripts/release.sh` for checks, version
  bump, build, install, commit, and tag.
- `SECURITY.md` covering assets, trust boundaries, security properties, known
  limitations, and private vulnerability reporting.
- Regression tests for Flatpak detection, atomic config writes, streaming
  crypto, Obsidian registry errors, atomic install, and test isolation.

## Changed

- Flatpak mode is enabled only when `flatpak info md.obsidian.Obsidian`
  succeeds; native Obsidian and unrelated Flatpak applications no longer
  accidentally select a cache workspace.
- File crypto now streams through `io.Reader`/`io.Writer` instead of loading
  complete files into memory. SHA-256 deduplication and watcher hash seeding
  stream as well.
- `Config.Save` writes a temporary file, fsyncs it, and atomically renames it.
- `grim install` resolves the home directory with `os.UserHomeDir()` and
  installs through a temporary file plus atomic rename.
- `WatchAndSync` returns fsnotify errors; the session reports them and locks
  safely.
- `RegisterObsidianVault` and `UnregisterObsidianVault` return aggregated
  errors. Launch and lock continue safely, but registry failures are visible.
- `cmd/grim` and `pkg/ramdisk` tests run under `TestMain` with isolated
  HOME/cache/runtime/temp directories.
- The runner lifecycle test now exercises a real blocking helper process.
- `PLAN.md` records the completed Phase 3 work and remaining backlog.

## Fixed

- Unrelated installed Flatpak applications caused the Obsidian workspace to be
  placed in the cache even when the Obsidian Flatpak was absent.
- Malformed Obsidian registry files could be overwritten silently.
- A failed config write could leave truncated YAML.
- A failed binary install could leave a partially written executable.
- Watcher startup/runtime errors were hidden from the user.
- Tests could write into the real user cache/runtime directories.

## Security

- Active sessions are protected by advisory file locks across PID namespaces.
- Vault deletion and pruning require a valid session lock.
- Cleanup does not run for completion/help commands.
- Encrypted files and configuration use atomic replace.
- Vault removals reject path traversal.
- Streaming file handling reduces the plaintext memory footprint.
- See [SECURITY.md](SECURITY.md) for the full threat model and limitations.

## Upgrade notes

- No vault migration is required.
- Existing configuration files are compatible. Windows continues to support
  the legacy `~/.config/grim/config.yaml` fallback alongside
  `%APPDATA%\grim\config.yaml`.
- If a previous environment selected a cache workspace because of an unrelated
  Flatpak installation, v0.3.0 may choose tmpfs instead. Session liveness is
  checked across all supported workspace locations.
- Passphrase changes still do not rotate the X25519 master key; see
  [SECURITY.md](SECURITY.md).

## Verification

The release was checked with:

```bash
gofmt -l cmd pkg
go vet ./...
go test ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go build ./...
```

## Installation

```bash
go install github.com/ijin82/grim/cmd/grim@latest
# or build from source:
make install
```

Existing installations can be upgraded atomically with:

```bash
grim install
```

Full changelog: <https://github.com/ijin82/grim/compare/v0.2.0...v0.3.0>
