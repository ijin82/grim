# Grim Improvement Plan

Status: active
Last updated: 2026-11-21
Scope: findings from the initial project audit plus the security-hardening work already present in the working tree.

## Status legend

- `[x]` implemented in the working tree, tests passing, pending review/commit
- `[ ]` planned
- `[~]` in progress
- `P0` data loss or remote code execution risk
- `P1` security or data-integrity risk
- `P2` correctness, UX, or maintainability issue
- `P3` cleanup, portability, or documentation improvement

## Current baseline

- CLI version: `0.3.0` ([cmd/grim/main.go](cmd/grim/main.go)).
- Go module: `github.com/ijin82/grim`.
- Dependencies: age, fsnotify, Cobra, YAML, x/term, x/sys.
- Tests: `go test ./...` and `go test -race ./...` pass.
- Formatting and vet: clean.
- Windows cross-compilation: passes.
- Installed binary: `~/.local/bin/grim` (primary) and `~/go/bin/grim` (mirror), both v0.3.0.
- Release/install process: `make release VERSION=x.y.z` via [scripts/release.sh](scripts/release.sh). The script commits, installs, and tags but never pushes; see the release checklist at the end of this file.
- Release tag: `v0.3.0`.

## Phase 0 — Security hardening already implemented (pending commit)

### SEC-01 — Session liveness must not depend on PID
`[x]`

Problem: `isWorkspaceActive` relied on `syscall.Kill(pid, 0)`. A process inside a Flatpak/container PID namespace cannot see the host PID, so it treats an active workspace as stale and wipes it. This was the root cause of the production vault deletion incident.

Fix:
- Hold an advisory `flock` (Unix) or `LockFileEx` (Windows) on `.grim-lock` for the whole session.
- Check the advisory lock first; keep PID probing only as a fallback for legacy sessions.
- Fixed Windows process probing and Windows compilation.

Files:
- [pkg/ramdisk/workspace_lock.go](pkg/ramdisk/workspace_lock.go)
- [pkg/ramdisk/workspace_lock_unix.go](pkg/ramdisk/workspace_lock_unix.go)
- [pkg/ramdisk/workspace_lock_windows.go](pkg/ramdisk/workspace_lock_windows.go)
- [pkg/ramdisk/process_unix.go](pkg/ramdisk/process_unix.go)
- [pkg/ramdisk/process_windows.go](pkg/ramdisk/process_windows.go)
- [pkg/ramdisk/ramdisk.go](pkg/ramdisk/ramdisk.go)

Tests:
- [pkg/ramdisk/workspace_lock_test.go](pkg/ramdisk/workspace_lock_test.go)

### SEC-02 — Cleanup must not run for machine-readable commands
`[x]`

Problem: `CleanupStaleWorkspaces` ran before every command, including `grim completion`. Its human-readable output was included in the generated completion script, and a non-interactive `.bashrc` executed it as a shell command. This was the visible `🛡️: command not found` symptom.

Fix:
- Run cleanup only for commands that need it.
- Skip `completion`, `help`, `__complete`, and `__completeNoDesc`.
- Write cleanup messages to `stderr`, never `stdout`.

Files:
- [cmd/grim/main.go](cmd/grim/main.go)

Tests:
- [cmd/grim/cleanup_test.go](cmd/grim/cleanup_test.go)

### SEC-03 — Watcher circuit breaker
`[x]`

Problem: when the RAM workspace was wiped externally, fsnotify delivered `REMOVE` events for every file and directory. The watcher treated this as user deletion and removed every `.age` file from the vault.

Fix:
- Verify `.grim-lock` exists before processing events and before every debounced action.
- On lock disappearance, switch to `BLOCKED` mode, stop all deletions, and notify the session.
- Prevent `RemoveFileFromVault` from recursively deleting vault content when the session lock is missing.
- Refuse `FullSyncToVault`, `PruneDeletedFromVault`, and `Lock` without a valid workspace lock.
- Add a path-traversal guard for relative deletion paths.

Files:
- [pkg/vault/vault.go](pkg/vault/vault.go)

Tests:
- [pkg/vault/watch_guard_test.go](pkg/vault/watch_guard_test.go)

### SEC-04 — External cleanup must not delete vault data
`[x]`

Problem: a stale-session cleanup was destructive by design and could also remove the plaintext workspace of a live session. That is acceptable only for a workspace that is provably abandoned.

Fix:
- Never wipe a workspace whose advisory lock is held.
- Treat unknown lock-check errors as "active" rather than "abandoned".
- Reject invalid deletions instead of silently ignoring them.

### DOC-01 — English-only project and security documentation
`[x]`

- [AGENTS.md](AGENTS.md) translated to English and now requires English-only repository artifacts.
- [README.md](README.md) gained a session-protection and `BASH_ENV` warning section.

## Phase 1 — Data-loss and security blockers (implemented, pending review)

### DATA-01 — `grim init` overwrites an existing vault
`[x]` `P0`

Problem: `Init` used to write a new `.vault-meta.age` without checking whether one already existed. Existing `.age` notes became undecryptable while the command reported success.

Implemented:
- `Init` refuses an existing `.vault-meta.age`.
- It also refuses a directory that already contains `.age` files without metadata.
- `InitWithForce` and `grim init --force` provide an explicit opt-in with a warning.
- A failed `init` does not register a config entry.

Tests:
- [pkg/vault/vault_test.go](pkg/vault/vault_test.go) covers refusal, orphaned `.age` files, and forced reinitialization.

Manual check:
- Run `grim init` twice against the same directory; the second run must fail and leave metadata unchanged.
- Run `grim init ... --force`; it must replace metadata and the new passphrase must work.

### SEC-05 — Passphrase change does not rotate the master key
`[~]` `P1`

Problem: `ChangePassphrase` only rewraps the existing metadata. The X25519 private key is unchanged. An attacker with old `.vault-meta.age` from git history and the old passphrase can still decrypt current and future notes.

Implemented:
- The limitation is documented in [README.md](README.md).
- The CLI prints an explicit warning after a passphrase change.
- The function comment in [pkg/vault/vault.go](pkg/vault/vault.go) states that key rotation is not performed.

Remaining:
- SEC-05b: implement true master-key rotation with staged re-encryption and rollback.
- Add a regression test proving that old metadata plus the old passphrase cannot decrypt newly rotated files.

Manual check:
- Change a passphrase and confirm the warning is printed.
- Do not treat passphrase rotation as revocation until SEC-05b is implemented.

### SEC-06 — `grim remove` can leave plaintext in RAM
`[x]` `P1`

Problem: removing a vault from configuration could leave an active RAM session running and its plaintext available.

Implemented:
- `remove` refuses to remove a vault that is currently unlocked.
- The error tells the user to run `grim lock <vault>` first.
- Stale Obsidian registry cleanup remains tracked as OBS-01.

Tests:
- [cmd/grim/phase1_test.go](cmd/grim/phase1_test.go) verifies rejection while unlocked and successful removal after lock/wipe.

### UX-01 — Any typed input locks the vault
`[x]` `P1`

Problem: the session `select` reacted to the first input line, so any text other than `lock/q/exit/close` still closed the session.

Implemented:
- The input handler loops until an explicit lock command, signal, timeout, or editor exit.
- Unknown input prints a hint and keeps the session open.
- Command matching is case-insensitive and trims whitespace.

Tests:
- [cmd/grim/phase1_test.go](cmd/grim/phase1_test.go) covers accepted and rejected commands.

Manual check:
- Open a vault, type an unknown command such as `status`, verify the session stays open, then type `q`.

### CFG-01 — `timeout_minutes: 0` silently disables auto-lock
`[x]` `P2`

Implemented:
- [pkg/config/config.go](pkg/config/config.go) applies `DefaultTimeoutMinutes` (30) when a vault has a missing or non-positive timeout.
- `init` and `add` use the same constant.

Tests:
- [pkg/config/config_test.go](pkg/config/config_test.go) covers zero, missing, and explicit timeout values.

### SEC-07 — Minimum passphrase length is four characters
`[x]` `P2`

Implemented:
- The minimum is now 8 Unicode code points for new and changed passphrases.
- Validation counts runes, so Cyrillic passphrases are measured correctly.
- README documents the minimum and the key-rotation limitation.

Tests:
- [cmd/grim/phase1_test.go](cmd/grim/phase1_test.go) covers ASCII and Cyrillic boundary cases.

Manual check:
- `grim init` with a 7-character passphrase must fail; 8 characters must succeed.

### DOC-02 — README still claims plaintext never touches disk
`[x]` `P2`

Implemented:
- The origin statement and feature list now describe volatile RAM when available and cache fallback for Flatpak/macOS/Windows.
- The platform table already documents cache locations.

### CFG-02 — Windows config path differs from README
`[x]` `P2`

Implemented:
- Windows uses `%APPDATA%\grim\config.yaml`.
- If the new path does not exist but the legacy `~/.config/grim/config.yaml` does, the legacy file is used for compatibility.
- Linux and macOS keep the historical `~/.config/grim/config.yaml` path.

Tests:
- [pkg/config/config_test.go](pkg/config/config_test.go) covers platform path selection.

Manual check:
- On Windows, confirm `grim list` reads `%APPDATA%\grim\config.yaml` and preserves an existing legacy config.

## Phase 2 — Correctness and UX (implemented, pending review)

### SYNC-01 — Hidden files other than `.obsidian` are not synced back
`[x]` `P2`

Problem: `syncFileWithTracker` ignored hidden files except `.obsidian`. Editing `.gitignore` in the RAM workspace lost the change on lock.

Implemented:
- Root `.gitignore` and `.gitattributes` are now encrypted and synchronized.
- `.obsidian/**` remains synchronized.
- `.grim-lock`, `.git/**`, temporary files, and all other hidden paths are still excluded.

Tests:
- [pkg/vault/sync_hidden_test.go](pkg/vault/sync_hidden_test.go) covers the allowlist and a `.gitignore` encrypt/decrypt round-trip.

### LOCK-01 — `grim lock` can wipe another session's workspace
`[x]` `P2`

Problem: `lock` wiped the workspace directly, even when another grim process owned it.

Implemented:
- Active sessions are detected through the workspace lock.
- The lock command sends `SIGINT` to the owner and waits up to 5 seconds for a graceful lock.
- A workspace is never wiped while its owner is still alive.
- If the owner does not exit, the command returns an error and leaves the workspace intact.

Tests:
- [cmd/grim/phase2_test.go](cmd/grim/phase2_test.go) covers stale workspaces, self-signal refusal, and a helper process that removes its workspace on `SIGINT`.

Manual check:
- Open a vault, run `grim lock <vault>` from another terminal, and confirm both the command and the session report a graceful lock.

### LOCK-02 — `grim lock` resolves the workspace path from the current environment
`[x]` `P2`

Implemented:
- `ramdisk.FindWorkspacePaths` scans every supported workspace base directory.
- `lock` works with all discovered paths and does not depend on the current editor/Flatpak detection.
- `list`, `status`, and the "already unlocked" path use the active workspace location.

Tests:
- [cmd/grim/phase2_test.go](cmd/grim/phase2_test.go) exercises stale and active paths under an isolated runtime directory.

### OBS-01 — Stale Obsidian registry entries after a crash
`[x]` `P2`

Problem: a crash left the RAM workspace registered in `obsidian.json`, pointing to a missing directory.

Implemented:
- Stale workspace cleanup now calls `UnregisterObsidianVault` for every removed workspace.
- `lock` also unregisters wiped workspaces.

Tests:
- [cmd/grim/phase2_test.go](cmd/grim/phase2_test.go) registers a temporary workspace in a fake `obsidian.json` and verifies cleanup removes the entry.

Remaining:
- Registry write errors are still ignored; tracked as OBS-02 in Phase 3.

### UX-02 — Non-deterministic vault ordering
`[x]` `P3`

Implemented:
- `list`, `status`, setup selection, passphrase selection, default-vault selection, and default fallback after removal use `sortedVaultNames`.

Tests:
- [cmd/grim/phase2_test.go](cmd/grim/phase2_test.go) verifies deterministic ordering.

### UX-03 — User-facing messages mention hidden `grim open`
`[x]` `P3`

Implemented:
- `init` and `add` now suggest `grim <vault>` instead of the hidden `grim open <vault>`.

### UX-04 — Passphrase input consumes buffered lines in non-TTY mode
`[x]` `P3`

Implemented:
- `init` and passphrase change share one `bufio.Reader` across prompts.
- Non-TTY `grim init` with piped passphrase and confirmation now succeeds.
- The single-prompt `open`/`add` paths remain unchanged.

Manual check:
- `printf 'pass12345\npass12345\n' | grim init test /tmp/test-vault` succeeds.

## Phase 3 — Robustness, performance, and portability

### ENV-01 — Flatpak Obsidian detection was imprecise
`[x]` `P2`

Problem: any installed `flatpak` binary caused `obsidian`/default editor to use the cache workspace, even when the Obsidian Flatpak itself was absent. Native Obsidian could also lose to sandbox detection depending on PATH.

Implemented:
- Native `obsidian` in PATH wins.
- Flatpak mode requires `flatpak info md.obsidian.Obsidian` to succeed.
- Snap mode requires the Snap package or a snap wrapper path.
- Custom commands containing `flatpak`/`snap` still use the cache.

Tests:
- [pkg/ramdisk/flatpak_test.go](pkg/ramdisk/flatpak_test.go) covers native, missing Flatpak, unrelated Flatpak, snap, and non-Obsidian editors.

### CFG-03 — Config writes are not atomic
`[x]` `P3`

Implemented:
- `Config.Save` writes a unique temporary file in the config directory, fsyncs it, and atomically renames it over `config.yaml`.
- Temporary files are removed on failure.

Tests:
- [pkg/config/config_test.go](pkg/config/config_test.go) verifies reload and absence of temporary leftovers.

### PERF-01 — Files are read entirely into memory
`[x]` `P3`

Implemented:
- File encryption/decryption now streams through `io.Reader`/`io.Writer` with atomic temp-file + rename.
- SHA-256 deduplication and watcher hash seeding stream files instead of using `os.ReadFile`.
- `EncryptFileWithKey`, `DecryptFileWithKey`, `EncryptFile`, and `DecryptFile` no longer load whole files.

Tests:
- [pkg/crypto/crypto_test.go](pkg/crypto/crypto_test.go) includes a 4 MiB file round-trip.

### ERR-01 — Watcher errors are swallowed
`[x]` `P3`

Implemented:
- `WatchAndSync` returns fsnotify errors instead of ignoring them.
- `executeOpen` listens on a watcher error channel, reports the error, and locks the vault safely.

Tests:
- [pkg/vault/watch_guard_test.go](pkg/vault/watch_guard_test.go) covers refusal to start without a lock.

### OBS-02 — Obsidian registry write errors are ignored
`[x]` `P3`

Implemented:
- `RegisterObsidianVault` and `UnregisterObsidianVault` return aggregated errors.
- Malformed or unreadable `obsidian.json` is no longer overwritten.
- Editor launch and vault locking print warnings but continue safely.

Tests:
- [pkg/runner/obsidian_test.go](pkg/runner/obsidian_test.go) covers register/unregister, malformed JSON, and missing registry.

### TEST-01 — Some tests are not hermetic
`[x]` `P3`

Implemented:
- `cmd/grim` and `pkg/ramdisk` tests run under `TestMain` with isolated `HOME`, `XDG_RUNTIME_DIR`, `XDG_CACHE_HOME`, `TMPDIR`, `APPDATA`, `LOCALAPPDATA`, and `USERPROFILE`.
- `cmd/grim` advanced tests use `mustNewWorkspace`, which reports workspace creation failures with `t.Fatalf` instead of panicking on a nil workspace.
- Linux-specific workspace cleanup and shell-based tests are skipped on unsupported platforms.
- Piped/non-TTY passphrase tests and platform-specific path tests are separated from the default test environment.

### TEST-02 — Runner lifecycle test does not test a live process
`[x]` `P3`

Implemented:
- The test launches a blocking shell helper that ignores arguments, waits for `SIGINT`, and verifies `Stop`.
- Windows skips the Unix shell-specific test.

### INST-01 — `grim install` portability and atomicity
`[x]` `P3`

Implemented:
- `install` resolves the home directory through `os.UserHomeDir()` instead of `$HOME`.
- The binary is copied into a unique temporary file in the destination directory, fsynced, chmodded to `0755`, and atomically renamed into place.
- Same-path installation is a no-op; failures remove the temporary file.
- `/usr/local/bin` is attempted first, then `~/.local/bin`.

Tests:
- [cmd/grim/install_test.go](cmd/grim/install_test.go) covers targets, first install, overwrite, same-path no-op, and cleanup on failure.

## Phase 4 — Tooling, CI, and release process

### CI-01 — No CI pipeline
`[x]` `P3`

Implemented:
- [.github/workflows/ci.yml](.github/workflows/ci.yml) runs formatting, vet, tests, and Linux race tests on Ubuntu and macOS.
- A separate job cross-builds `windows/amd64`.
- Windows native runtime tests are intentionally not part of CI yet; they need a dedicated runner or platform-specific test pass.

### RELEASE-01 — Release process is fully manual
`[x]` `P3`

Implemented:
- [scripts/release.sh](scripts/release.sh) verifies a clean tree, runs formatting/vet/tests/race tests, bumps the version, builds, installs, commits, and creates an annotated tag.
- `make release VERSION=x.y.z` wraps the script.
- The script never pushes; it prints the exact push commands.
- The manual fallback is documented in [README.md](README.md).

Remaining:
- A separate `RELEASING.md` is optional; README currently holds the short release instructions.

### DOC-03 — No SECURITY.md or threat model
`[x]` `P3`

Implemented:
- Added [SECURITY.md](SECURITY.md) with supported versions, private vulnerability reporting, scope, assets, trust boundaries, security properties, and known limitations.
- Documented the non-tmpfs workspace fallback, passphrase non-rotation, metadata leakage, rollback risk, wipe limits, Obsidian Sync risk, `BASH_ENV`, and Windows lock behavior.
- Added a short security section with a link in [README.md](README.md).

## Post-release enhancements

### UX-05 — Reopen the editor without restarting Grim
`[x]` `P2`

Implemented:
- The active Grim console accepts `r` or `reopen`.
- Grim stops the previous editor process, closes the Obsidian application
  (Flatpak, native, macOS, or Windows as applicable), and relaunches the editor
  against the existing RAM workspace.
- The session, watcher, and locks stay active; no lock/wipe is performed.
- The session banner and unknown-command hint document the command.
- README describes the workflow.

Tests:
- `isReopenCommand`, editor restart with a fake helper process, and the safe
  non-Obsidian close path are covered.

## Known minor issues / backlog


- `WipeAndRemove` secure-overwrite guarantees are limited on SSDs, copy-on-write filesystems, and network filesystems.
- Symlinks inside the RAM workspace are followed by sync; define and document the expected behavior.
- File modes and timestamps are not preserved exactly (decrypted files use `0600`).
- `obsidian.json` is rewritten with `0600`; verify this matches Obsidian expectations.
- `CleanupStaleWorkspaces` still runs for broad commands such as `list` and `status`; with flock this is safe, but consider narrowing it further.
- A legacy plaintext `.gitignore` in the vault is not removed when the new encrypted `.gitignore.age` is created.
- Graceful `grim lock` relies on `SIGINT`; Windows currently returns a safe error instead of a graceful shutdown.
- `go.mod` has `go 1.26.5`; verify the intended minimum Go version before release.
- README quick-start examples should be checked after the next version bump.
- Release notes and version references are duplicated manually; keep them in sync.

## Suggested execution order

1. Review, manually verify, and commit Phase 3.
2. Optionally implement SEC-05b (true master-key rotation).
3. Polish the backlog items as needed.

## Release checklist (current manual process)

```bash
git status
gofmt -l .
go vet ./...
go test ./...
go test -race ./...

# update cmd/grim/main.go: var Version = "x.y.z"
git add cmd/grim/main.go
git commit -m "chore: bump version to vx.y.z"

git tag -a vx.y.z -m "Grim vx.y.z"
git push origin master --tags
# optional mirrors:
git push origin-gv master --tags
git push origin-wm master --tags

make build
make install
command -v grim
grim --version
```

Do not create a tag while the working tree contains unreviewed changes. The tag must point to the commit that contains the version bump.
