# Instructions and conventions for AI assistants (AGENTS.md)

## Project goal

`Grim` (Grimoire) is a command-line manager for encrypted Markdown vaults for Obsidian, VS Code, Vim, and other editors. The project is written in Go and uses `filippo.io/age` (X25519 + scrypt), `fsnotify`, and Cobra.

**This is a security-sensitive project.** A mistake in synchronization, RAM cleanup, or key handling can leak notes, destroy data, or corrupt a vault. Every change in these paths must be justified, covered by tests, and checked for unintended deletions.

## Core requirements

1. **Language: English only**
   - Everything in this repository is written and maintained in English: source code, comments, identifiers, tests, documentation, README, commit messages, release notes, CLI help, and user-facing strings.
   - Do not add Russian text (or any other non-English language) to code, comments, docs, commits, or user-facing output.
   - The only exceptions are intentional Unicode test fixtures (for example, Cyrillic filenames and content in Unicode-handling tests) and proper names or technical identifiers.
   - Respond to the user in English unless they explicitly ask for another language. All artifacts committed to the repository must be English.

2. **Comments and rationale**
   - Write comments for new non-trivial code in English.
   - Explain not only *what* the code does but also *why*: fsnotify behavior, debounce, concurrent access to RAM, file locking, atomic writes, and the trust boundary between RAM and the encrypted vault.
   - Keep existing comments in English. Do not introduce comments in other languages and do not rewrite existing comments without a reason.

3. **README.md**
   - `README.md` is the single main project documentation. When behavior, commands, paths, or system requirements change, update the README in the same change.
   - Do not promise more in the README than the code does. This applies especially to claims that "plaintext never touches disk": macOS, Windows, and Flatpak editor setups use cache directories.

4. **Incremental, clear commits**
   - Commit changes in logical steps with English messages that explain the purpose and the reason.
   - Do not mix refactoring, formatting, and behavior changes in one commit.
   - Do not run `git push` without the user's explicit permission.

5. **Architecture and cleanliness**
   - `cmd/grim/main.go` — Cobra CLI and interactive flows.
   - `pkg/config` — YAML configuration.
   - `pkg/crypto` — age/scrypt and atomic file writes.
   - `pkg/vault` — metadata, unlock/lock, fsnotify synchronization, and prune.
   - `pkg/ramdisk` — RAM workspace, lock file, stale-session protection, and secure wipe.
   - `pkg/runner` — editor launching and Obsidian integration.
   - Do not add dependencies without a reason. Prefer the standard library and packages already in use.
   - Handle errors explicitly; do not ignore deletion, write, or lock errors in security-sensitive paths.

6. **Security invariants**
   - **Never delete `.age` files merely because the RAM workspace is empty.** Deletion is allowed only when session integrity is confirmed.
   - `.grim-lock` is an internal session file. It must never be synchronized into the vault, deleted manually, or treated as a user file.
   - Session liveness is determined by an **advisory file lock (`flock` / `LockFileEx`)**, not by PID alone. PID checks do not work across PID namespaces (Flatpak, containers) and are subject to PID reuse.
   - `CleanupStaleWorkspaces` must not run for `completion`, `help`, `__complete`, or other machine-readable commands. Cleanup messages must always go to `stderr`.
   - If `.grim-lock` disappears, the watcher must stop deleting files from the vault and must not prune.
   - Every delete operation based on a relative path must verify that the path does not escape `vaultRoot`.
   - `WipeAndRemove` applies only to the RAM workspace, never to the encrypted vault.

7. **Tests and checks**
   - Before installing, always run:
     ```bash
     gofmt -l .
     go vet ./...
     go test ./...
     ```
   - For changes in concurrent code, the watcher, locking, or cleanup, also run:
     ```bash
     go test -race ./...
     ```
   - Cover new behavior with regression tests. For fsnotify, use timeouts and avoid fixed `sleep` calls when possible.
   - Tests must be hermetic: never touch the real `~/.config/grim`, `~/.cache/grim`, or production vaults. Use `t.TempDir()` and `t.Setenv`.

8. **Build and system installation**
   - After successful checks and a successful build, **always install the fresh binary** so the user can immediately run manual tests:
     ```bash
     go build -o grim ./cmd/grim
     install -m 755 grim ~/.local/bin/grim
     command -v grim
     grim --version
     ```
   - Verify that `command -v grim` points to `~/.local/bin/grim` (or to the path where the fresh binary was installed) and that the version matches expectations.
   - `make install` is acceptable as an equivalent, but it may try to install into `/usr/local/bin`; in that case, verify the actual installation path.
   - Do not leave uncommitted temporary build directories or test vaults in the working tree.

9. **Versions and releases**
   - The CLI version is stored in `cmd/grim/main.go` (`var Version`). When preparing a release, update it consistently with the tag and README.
   - Release notes for GitHub must be written in English.
