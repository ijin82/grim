# Grim v0.3.1

Release date: 2026-11-21

Grim v0.3.1 is a small patch release that adds an interactive editor restart
command. There are no vault-format or configuration changes.

## Added

- `r` / `reopen` command in the active Grim console.
- The command stops the current editor, closes Obsidian windows, and relaunches
  the editor against the existing RAM workspace.
- The session, watcher, and session locks stay active; the vault is not locked
  or wiped.
- Platform-specific Obsidian shutdown:
  - Flatpak: `flatpak kill md.obsidian.Obsidian`;
  - native Linux: `pkill -TERM -x obsidian`;
  - macOS: `osascript ... quit`;
  - Windows: `taskkill /IM Obsidian.exe /T`.
- Session banner and unknown-command hints now document `r` / `reopen`.
- README describes the reopen workflow.
- Regression tests for command parsing and editor restart.

## Changed

- The interactive session can now recover when Obsidian was already running
  with another vault, or when an editor lost the RAM workspace after a session
  restart.
- Other Obsidian vault windows are closed by `reopen`; this is documented in
  the README and the session banner.

## Fixed

- No separate bug fixes in this release.

## Security

- Reopening does not create a new workspace, does not unlock a vault, and does
  not weaken the session lock or watcher safeguards.
- See [SECURITY.md](SECURITY.md) for the threat model.

## Upgrade notes

- Existing vaults and configuration files are compatible.
- No migration is required.
- Use `r` / `reopen` only when you want to restart the editor for the current
  RAM workspace. Use `lock` / `q` to finish the session.
- On Windows, graceful Obsidian shutdown is best-effort via `taskkill`; a
  running or unresponsive instance may still require manual action.

## Verification

```bash
gofmt -l cmd pkg
go vet ./...
go test ./...
GOOS=windows GOARCH=amd64 go build ./...
```

## Installation

```bash
go install github.com/ijin82/grim/cmd/grim@latest
# or build from source:
make install
```

Existing installations can be upgraded with:

```bash
grim install
```

Full changelog: <https://github.com/ijin82/grim/compare/v0.3.0...v0.3.1>
