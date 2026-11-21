# Security Policy and Threat Model

This document describes the security properties, trust boundaries, known
limitations, and vulnerability-reporting process for Grim.

## Supported versions

| Version | Supported |
| --- | --- |
| `0.2.x` | Yes |
| `master` | Development branch; fixes land here first |
| `< 0.2` | No; upgrade to the latest release |

Grim is pre-1.0. There are no long-term support branches. Security fixes are
released in the latest version, and users are expected to upgrade.

## Reporting a vulnerability

Please do **not** open a public GitHub issue for a security vulnerability.

Use GitHub's private vulnerability reporting:

- <https://github.com/ijin82/grim/security/advisories/new>

If private reporting is unavailable, contact the maintainer through the
repository's GitHub profile and clearly mark the message as a security report.

Please include:

- affected Grim version (`grim --version`) and commit hash if applicable;
- operating system and editor/Flatpak/Snap environment;
- a minimal reproduction or proof of concept;
- impact assessment: confidentiality, integrity, availability, or data loss;
- whether the issue is already public.

The maintainer will acknowledge the report as soon as possible and will
coordinate disclosure after a fix is available. There is no bug bounty program.

## Scope

### In scope

- The age/scrypt cryptography used by Grim.
- Protection of vault contents at rest against offline theft.
- Passphrase and master-key handling.
- RAM workspace isolation, locking, cleanup, and secure-wipe behavior.
- fsnotify synchronization and prune safety.
- Path traversal and unintended file deletion.
- Obsidian registry manipulation.
- Flatpak/Snap workspace selection.
- Supply-chain issues in direct dependencies.

### Out of scope

- A compromised operating system, root/administrator access, malware, or a
  keylogger.
- Memory dumps, cold-boot attacks, or forensic recovery of process memory.
- A compromised editor, editor plugin, or Obsidian vault plugin.
- A compromised Git remote, cloud-sync provider, or backup provider.
- Denial of service caused by deleting or corrupting the vault directory.
- Loss of a passphrase or master key.
- Weak user-chosen passphrases.
- Physical access to an unlocked machine.

## Assets

- The master passphrase.
- The X25519 master private key stored inside `.vault-meta.age`.
- Decrypted notes and attachments in the RAM workspace.
- Encrypted `.age` files and vault metadata.
- The local configuration file.
- The Obsidian vault registry (`obsidian.json`).

## Architecture and trust boundaries

### Vault at rest

Each note or attachment is encrypted individually with `age` using an X25519
recipient. The vault metadata file `.vault-meta.age` contains the X25519
private key and is protected by the master passphrase using the scrypt KDF.

The passphrase is never stored by Grim. The private key exists in plaintext only
while a vault is unlocked and is kept in process memory and the RAM workspace.

### Workspace

When a vault is unlocked, decrypted files are written to an isolated workspace:

- native Linux editors: `$XDG_RUNTIME_DIR/grim` or `/dev/shm/grim-$UID` (falling back to `~/.cache/grim` if neither is available);
- Linux Flatpak editors: `$XDG_CACHE_HOME/grim` or `~/.cache/grim`;
- macOS: `~/Library/Caches/grim`;
- Windows: `%LOCALAPPDATA%\grim\cache`.

Only the native Linux paths are guaranteed to be volatile RAM (`tmpfs`). On
macOS, Windows, and Flatpak, plaintext may be written to a normal filesystem
cache. The workspace is removed on lock, subject to the wipe limitations below.

### Session locking

An active session holds an advisory file lock (`flock` on Unix, `LockFileEx` on
Windows) on `.grim-lock` for its lifetime. This is intentionally independent of
PID checks: PIDs are not visible across PID namespaces and can be reused.

Workspace cleanup must never remove a workspace whose lock is held. If the lock
file disappears, the watcher stops deleting files from the vault and `Lock`
refuses to prune. See [AGENTS.md](AGENTS.md) for the invariants.

### Editor and Obsidian boundary

Grim launches the editor as a separate process. A native editor runs with the
user's permissions and can read the decrypted workspace. Flatpak/Snap editors
run in their own sandbox; Grim exposes only the workspace path they need.

Obsidian plugins and editor extensions run with access to the decrypted
workspace. They are outside Grim's security boundary.

## Security properties

Grim aims to provide the following properties:

- **Confidentiality at rest:** an attacker with a copy of the encrypted vault
  cannot recover note content without the passphrase or master key.
- **Per-file authenticated encryption:** each `.age` file is independently
  authenticated by age; ciphertext tampering is detected on decryption.
- **No plaintext written back by Grim for encrypted notes:** decrypted content
  is written only to the workspace, never back into `.age` files. Root
  `.gitignore` and `.gitattributes` are intentionally plaintext so Git can use
  them; never store secrets in those files.
- **No automatic deletion without a valid session:** watcher deletions and
  pruning require a live session lock and a present `.grim-lock`.
- **Atomic file writes:** encrypted files and configuration are written through
  temporary files and renamed into place.
- **Bounded cleanup:** stale cleanup only targets `grim-*` workspace
  directories, never the vault itself.

## Known limitations

### Passphrase changes do not rotate the master key

`grim setup passwd` re-wraps `.vault-meta.age` with the new passphrase, but the
X25519 master keypair is unchanged. An attacker who has an old copy of
`.vault-meta.age` from Git history and knows the old passphrase can still
decrypt current and future notes encrypted to the same public key.

True master-key rotation (`SEC-05b`) is not implemented yet. Treat passphrase
change as protection against casual reuse, not as key revocation.

### Metadata leakage

File names, directory structure, file sizes, modification times, the number of
notes, and the existence of `.vault-meta.age` are visible to anyone with the
vault directory or Git repository. Age encrypts content, not this metadata.

### No rollback or anti-tamper protection

Git and filesystem history are outside Grim. An attacker with write access to
the vault can delete, restore an old version of, or reorder `.age` files. Content
authenticity is protected per file by age, but Grim does not provide a signed
manifest, rollback detection, or freshness guarantees.

### Workspace plaintext on non-tmpfs platforms

On macOS, Windows, and Flatpak, the workspace is a cache directory on persistent
storage. The plaintext is removed on lock, but it may have been written to disk,
backup, swap, or a filesystem journal before removal.

Even on tmpfs, `WipeAndRemove` is best-effort: it overwrites files with zeroes
and random bytes before deletion. It cannot guarantee physical erasure on SSDs,
copy-on-write filesystems, network filesystems, snapshots, or backups.

### Process and memory threats

Grim does not protect against a compromised OS, root access, debugging of the
running process, memory dumps, keyloggers, or malicious libraries. Decrypted
data necessarily exists in process memory and in the workspace while unlocked.

### Obsidian Sync and cloud plugins

If Obsidian Sync or a cloud-sync plugin is enabled for the RAM workspace, it may
upload decrypted notes outside Grim's control. Disable such plugins for Grim
workspaces, or accept that the plaintext leaves the machine.

### Symlinks, permissions, and timestamps

Sync follows symlinks in the workspace. Do not place symlinks to sensitive files
inside a vault. Decrypted files are created with `0600`; executable bits,
extended attributes, ownership, and timestamps are not preserved exactly.

### Windows graceful lock

`grim lock` asks the active process to shut down via `SIGINT`. On Windows this
mechanism is not available; the command returns a safe error and leaves the
workspace intact. Use `Ctrl+C` in the active terminal instead.

### `BASH_ENV` and non-interactive shells

If `BASH_ENV` points to `.bashrc`, non-interactive shells (including some
Flatpak launchers) may execute interactive shell code. Grim no longer runs stale
cleanup for `completion`/`help`, and active sessions are protected by a file
lock, but users should guard `.bashrc` with an interactive check:

```bash
case $- in
  *i*) ;;
  *) return ;;
esac
```

## Operational recommendations

- Use a long, unique master passphrase. The CLI minimum is 8 characters; prefer
  a generated passphrase of 12+ characters.
- Keep an offline backup and test restoring it.
- Verify `grim list` and `grim lock` behavior on a copy before using a
  production vault.
- Do not enable Obsidian Sync or third-party cloud plugins for the RAM workspace
  unless you accept plaintext leaving the machine.
- Run `grim` as your normal user, not as root.
- Keep Go, age, and other dependencies updated.
- Do not commit plaintext files, editor caches, or the workspace directory.
- Treat `.vault-meta.age` as sensitive: it contains the passphrase-wrapped
  master key.

## Security-relevant implementation safeguards

Current safeguards include:

- X25519 + scrypt via `filippo.io/age`;
- advisory file locks for session liveness across PID namespaces;
- refusal to prune or delete from the vault without `.grim-lock`;
- no stale cleanup for completion/help commands;
- cleanup messages written to `stderr`;
- atomic writes for encrypted files and configuration;
- path-traversal checks on vault deletions;
- hidden-file policy: `.obsidian` is encrypted, root `.gitignore` and
  `.gitattributes` stay plaintext for Git, and other hidden paths are excluded;
- streaming encryption/decryption and streaming SHA-256 for large files;
- platform-specific Flatpak Obsidian detection;
- regression tests for watcher, locking, cleanup, and install behavior.

See [PLAN.md](PLAN.md) for open security work.
