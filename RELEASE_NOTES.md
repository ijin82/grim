# Grim Release Notes

## 0.3.2 — 2026-11-21

### Changed

- Consolidated all release history into this single `RELEASE_NOTES.md` file, newest release first.

## 0.3.1 — 2026-11-21

### Added

- `r` / `reopen` command to restart the editor without locking the vault.

### Changed

- `reopen` closes Obsidian windows and relaunches the editor against the existing RAM workspace; the session, watcher, and locks stay active.

## 0.3.0 — 2026-11-21

### Added

- GitHub Actions CI and `make release` / `scripts/release.sh`.
- `SECURITY.md` with the threat model and vulnerability-reporting process.
- Regression tests for Flatpak detection, streaming crypto, atomic writes, registry errors, and install behavior.

### Changed

- Flatpak mode requires the Obsidian Flatpak, not merely an installed `flatpak` binary.
- File encryption, decryption, and SHA-256 hashing are streamed.
- Configuration writes and `grim install` are atomic.
- Watcher and Obsidian registry errors are reported instead of ignored.
- Tests use isolated HOME/cache/runtime directories.

### Fixed

- Unrelated Flatpak applications no longer select the cache workspace.
- Malformed `obsidian.json` and partial config/binary writes are handled safely.
