# Grim 📖 (Grimoire)

A lightweight, high-performance CLI tool for managing secure, encrypted Markdown note vaults for **Obsidian**, **VS Code**, **Vim**, and other text editors.

Powered by [`filippo.io/age`](https://github.com/FiloSottile/age) (X25519 Master Key Architecture + Scrypt KDF) and written in pure Go.

### 💖 Acknowledgments & Credits

- **[Filippo Valsorda](https://github.com/FiloSottile)** — Author and creator of the [`age`](https://filippo.io/age) encryption tool and format.
- **[Steve Francia (spf13)](https://github.com/spf13)** & contributors — Creators of the [`Cobra`](https://github.com/spf13/cobra) CLI framework.
- **[fsnotify Team](https://github.com/fsnotify/fsnotify)** — High-performance filesystem event notifications for Go.
- **Go Community & The Go Authors (Google)** — For the elegance, speed, and standard library of the Go language.
- **[Antigravity](https://deepmind.google/) (Google DeepMind)** — AI pair programmer & co-creator of the Grim codebase and architecture.

---

## 📜 Origin & Name

**Grim** is short for **Grimoire** (*\ɡrɪmˈwɑːr\*). 

Historically, a *grimoire* is a textbook of magic, secret knowledge, and esoteric recipes, traditionally kept under lock and key by scholars and practitioners. 

In the digital era, **Grim** serves as the modern cryptographic grimoire for developers, security researchers, and note-takers: a personal sanctuary to hold your most sensitive thoughts, infrastructure credentials, server keys, and private journals — invisible to the operating system's disk, and unlocked only in temporary volatile memory.

---

## 🌟 Key Features

- **X25519 Master Key Architecture:** Instantaneous vault unlock and microsecond file saves, regardless of vault size (10 or 10,000 notes).
- **Age Encryption:** Modern file-level encryption using `age` (Scrypt KDF protects the master key metadata).
- **Isolated RAM Workspace:** Unlocks files directly into memory (`~/.cache/grim` / `tmpfs`), never writing plaintext notes to physical SSD/HDD.
- **Continuous Live Sync:** Uses `fsnotify` to detect file saves and immediately auto-encrypts changes to `.age` files on physical disk.
- **Obsidian, VS Code & Vim Integration:** Automatically registers and launches your preferred GUI or terminal editor targeting the in-memory Vault.
- **Auto-Lock & Multi-Pass Memory Wipe:** Closes the session on timeout, `Ctrl+C`, `lock` command, or editor exit, and overwrites RAM files with zeroes and random bytes before unmounting.
- **Interactive Setup Wizard:** Built-in `grim setup` wizard to configure editors, auto-lock timeouts, and re-encrypt vaults.
- **Git-Friendly:** Because each note is encrypted to its own `.md.age` file, you can easily use Git (`git init`, `git commit`, `git push`) to sync your encrypted vault.

---

## 🚀 Quick Start

### 1. Build & Install

```bash
# Build the binary
go build -o grim ./cmd/grim

# Install to system PATH (~/.local/bin or /usr/local/bin)
./grim install
# or with Makefile:
make install
```

---

### 2. Initialize a New Vault

```bash
grim init work ~/Documents/SecretGrimoire.enc
```
You will be prompted to enter and confirm your master passphrase.

---

### 3. Open and Start Editing

```bash
grim open work
# or simply:
grim
```
1. Enter your master passphrase.
2. The vault is decrypted into RAM in less than a second.
3. Your editor (Obsidian / VS Code / Vim) opens automatically.
4. Every time you save a note (`Ctrl+S`), Grim instantly encrypts it to disk.

---

### 4. Lock & Safe Exit

To lock your vault and wipe the in-memory notes:
- Press `Ctrl+C` in the terminal, or
- Type `lock` (or `q`) and press Enter, or
- For terminal editors (like `vim`/`nvim`): simply exit the editor (`:wq` / `:q`), or
- Wait for the auto-lock timeout (default: 30 minutes).

---

## 🛠️ Settings & Setup Wizard (`grim setup`)

Launch the interactive configuration wizard:
```bash
grim setup
```

Interactive Menu:
```text
📖 Grim Setup & Settings
==================================================
1) Select Default Editor      (current: obsidian)
2) Change Auto-lock Timeout
3) Change Vault Master Passphrase (re-encrypt)
4) Configure Specific Vault Settings
5) Set Default Vault          (current: work)
0) Exit
==================================================
Choose option [0-5]:
```

### Direct CLI Commands
```bash
# Set global default editor:
grim setup editor vim
grim setup editor code

# Change master passphrase for a vault:
grim setup passwd work

# Set default auto-lock timeout (in minutes):
grim setup timeout 20

# Configure editor for a specific vault:
grim setup work code
```

---

## ⚡ Shell Autocompletion (`Tab`)

Grim supports full shell autocompletion for subcommands and vault names.

### Bash
```bash
# Enable permanently for all new sessions:
echo 'source <(grim completion bash)' >> ~/.bashrc
```

### Zsh
```bash
# Enable permanently:
echo 'source <(grim completion zsh)' >> ~/.zshrc
```

### Fish
```bash
grim completion fish > ~/.config/fish/completions/grim.fish
```

---

## ⚙️ Configuration File

Configuration is saved in `~/.config/grim/config.yaml` (`%APPDATA%\grim\config.yaml` on Windows):

```yaml
default_vault: work
default_editor: obsidian
vaults:
  work:
    path: /home/user/Documents/SecretGrimoire.enc
    editor: obsidian
    timeout_minutes: 30
  servers:
    path: /home/user/Dropbox/ServerKeys.enc
    editor: vim
    timeout_minutes: 15
  personal:
    path: /home/user/Dropbox/PersonalVault.enc
    editor: code
    timeout_minutes: 20
```

---

## 📋 Commands Summary

- `grim` / `grim open [name]` — Unlock vault to RAM, start sync, launch editor.
- `grim init <name> <path>` — Create a new encrypted vault.
- `grim lock [name]` — Force lock and wipe RAM workspace.
- `grim list` — List all configured vaults and their status.
- `grim status` — View active unlocked vaults.
- `grim setup` — Full interactive configuration wizard.
- `grim set-default <name>` — Set the default vault.
- `grim install` — Install `grim` binary into system `$PATH`.
- `grim completion [bash|zsh|fish|powershell]` — Generate shell autocompletion script.
