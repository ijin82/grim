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

- CLI version: `0.2.0` ([cmd/grim/main.go](cmd/grim/main.go)).
- Go module: `github.com/ijin82/grim`.
- Dependencies: age, fsnotify, Cobra, YAML, x/term, x/sys.
- Tests: `go test ./...` and `go test -race ./...` pass.
- Formatting and vet: clean.
- Windows cross-compilation: passes.
- Installed binary: `~/.local/bin/grim` (primary) and `~/go/bin/grim` (mirror), both v0.2.0.
- Release/install process: manual, no release scripts. See the release checklist at the end of this file.
- The security-hardening changes described in Phase 0 are **uncommitted**. They must be reviewed and committed before the next release tag.

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

## Phase 2 — Correctness and UX

### SYNC-01 — Hidden files other than `.obsidian` are not synced back
`[ ]` `P2`

Problem: `syncFileWithTracker` ignores hidden files except `.obsidian`. A user editing `.gitignore` in the RAM workspace loses the change on lock.

Proposed fix:
- Explicitly support selected root dotfiles (`.gitignore`, `.gitattributes`).
- Continue excluding `.grim-lock`, `.git`, temporary files, and editor caches.
- Add a test for `.gitignore` round-trip.

### LOCK-01 — `grim lock` can wipe another session's workspace
`[ ]` `P2`

Problem: `lock` calls `WipeAndRemove` directly, even if another grim process owns the workspace. The owner later sees a missing lock and refuses to lock; the vault stays safe, but the session is left in an inconsistent state.

Proposed fix:
- Detect an active owner before wiping.
- Either signal the owner to lock gracefully or refuse with an explanation.
- Keep the current safety behavior as the fallback.

### LOCK-02 — `grim lock` resolves the workspace path from the current environment
`[ ]` `P2`

Problem: `lock` uses `ramdisk.GetPath`, which depends on the current editor/Flatpak detection, not necessarily the editor used to open the session.

Proposed fix:
- Search all candidate workspace locations for the vault name.
- Wipe only the one whose lock metadata matches the vault.

### OBS-01 — Stale Obsidian registry entries after a crash
`[ ]` `P2`

Problem: `RegisterObsidianVault` adds the RAM workspace to `obsidian.json`; `UnregisterObsidianVault` runs on a clean lock. After a crash, the stale entry remains and may point to a missing workspace.

Proposed fix:
- During stale cleanup, also unregister the removed workspace from Obsidian.
- Make registry writes error-aware.
- Add a test with a temporary `obsidian.json`.

### UX-02 — Non-deterministic vault ordering
`[ ]` `P3`

Problem: `list`, `setup`, and vault selection iterate Go maps, so output order changes between runs.

Proposed fix:
- Sort vault names before printing or selecting.

### UX-03 — User-facing messages mention hidden `grim open`
`[ ]` `P3`

Problem: `init`/`add` suggest `grim open <vault>`, but `open` is a hidden command.

Proposed fix:
- Suggest `grim <vault>` or unhide `open` and document it.

### UX-04 — Passphrase input consumes buffered lines in non-TTY mode
`[ ]` `P3`

Problem: each `promptPassword` call creates a new buffered reader. With piped input, the first reader can consume both lines, and the confirmation prompt gets `EOF`.

Proposed fix:
- Use one shared buffered stdin reader for the whole command.
- Add a non-TTY test for `init`.

## Phase 3 — Robustness, performance, and portability

### CFG-03 — Config writes are not atomic
`[ ]` `P3`

Problem: `Config.Save` writes directly to the target file. A crash during write can leave a truncated YAML config.

Proposed fix:
- Write to `config.yaml.tmp`, fsync, then rename.
- Use the same pattern already used for encrypted files.

### PERF-01 — Files are read entirely into memory
`[ ]` `P3`

Problem: crypto and sync functions use `os.ReadFile`, which spikes memory for large attachments and plugin bundles.

Proposed fix:
- Stream encryption/decryption through `io.Reader`/`io.Writer`.
- Keep atomic temp-file + rename semantics.
- Add a test with a large sparse file if practical.

### ERR-01 — Watcher errors are swallowed
`[ ]` `P3`

Problem: the watcher goroutine ignores `WatchAndSync`'s error. If inotify limits are reached or the watcher fails to start, the user believes live sync is active.

Proposed fix:
- Propagate watcher errors to the session.
- Print a clear warning and recommend restarting after fixing the cause.

### INST-01 — `grim install` portability and atomicity
`[ ]` `P3`

Problem: `install` uses `$HOME` and a direct copy. On Windows, `HOME` may be unset, and a partial copy can leave a broken binary.

Proposed fix:
- Use `os.UserHomeDir()`.
- Copy to a temporary file, chmod, then rename.
- Add platform-specific tests where possible.

### TEST-01 — Some tests are not hermetic
`[ ]` `P3`

Problem: several tests call `ramdisk.New`, which writes into the real runtime/cache directory, and some ignore errors from `New` or `Launch`. On failure they can panic instead of reporting a useful message.

Proposed fix:
- Allow tests to inject a base workspace directory.
- Replace ignored errors with `t.Fatal`.
- Keep Unicode fixtures, but isolate them under `t.TempDir()`.

### TEST-02 — Runner lifecycle test does not test a live process
`[ ]` `P3`

Problem: `TestRunnerLifecycle` launches `sleep 10` with the target path appended, which makes `sleep` fail immediately rather than testing a running process.

Proposed fix:
- Use a helper process/script that ignores arguments and blocks.
- Verify stop semantics and timeout behavior.

## Phase 4 — Tooling, CI, and release process

### CI-01 — No CI pipeline
`[ ]` `P3`

Proposed:
- GitHub Actions matrix: Linux, macOS, Windows.
- Steps: `gofmt -l`, `go vet`, `go test`, `go test -race` on Linux, `GOOS=windows go build`.
- No secrets required for tests.

### RELEASE-01 — Release process is fully manual
`[ ]` `P3`

Current process:
1. Update `Version` in [cmd/grim/main.go](cmd/grim/main.go).
2. Commit the bump.
3. Create an annotated tag.
4. Push the branch and tag to all remotes.
5. Build and install.

Proposed:
- Add `make release VERSION=x.y.z` or a small `scripts/release.sh` that:
  - verifies a clean working tree;
  - runs formatting, vet, tests, and race tests;
  - updates `Version`;
  - builds and installs;
  - creates the tag and prints the exact push commands.
- Add a `RELEASING.md` describing the manual fallback.
- Do not push automatically.

### DOC-03 — No SECURITY.md or threat model
`[ ]` `P3`

Proposed:
- Document the trust boundary: passphrase, master key, encrypted vault, RAM workspace, cache fallback, editor sandbox.
- Document known limitations: passphrase rotation, metadata rollback in git, wipe guarantees on SSD/COW filesystems, Flatpak PID namespaces.
- Add a responsible-disclosure contact.

## Known minor issues / backlog

- `WipeAndRemove` secure-overwrite guarantees are limited on SSDs, copy-on-write filesystems, and network filesystems.
- Symlinks inside the RAM workspace are followed by sync; define and document the expected behavior.
- File modes and timestamps are not preserved exactly (decrypted files use `0600`).
- `obsidian.json` is rewritten with `0600`; verify this matches Obsidian expectations.
- `RegisterObsidianVault` silently ignores write errors.
- `CleanupStaleWorkspaces` still runs for broad commands such as `list` and `status`; with flock this is safe, but consider narrowing it further.
- `getBaseWorkspaceDir` treats any installed Flatpak as evidence of a Flatpak Obsidian; make detection more precise.
- `go.mod` has `go 1.26.5`; verify the intended minimum Go version before release.
- README quick-start examples should be checked after the next version bump.
- Release notes and version references are duplicated manually; keep them in sync.

## Suggested execution order

1. Review, manually verify, and commit Phase 1.
2. Add SEC-05b: true master-key rotation with staged re-encryption and rollback.
3. Start Phase 2 with SYNC-01, LOCK-01, and LOCK-02.
4. Add CI and release automation.
5. Work through Phase 3 by priority.

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
