# Anka Crypt

Run your coding agents (Grok Build, Claude, Codex, ...) inside isolated, disposable Anka macOS VMs.

Crypt clones a prepared base VM into a shadow clone, launches the agent in
unattended ("YOLO") mode, and **keeps the clone running between sessions** so
agent state is preserved. Pass `--destroy` to delete after a run, or run
`crypt destroy` later. By default the guest cannot reach your host filesystem.
Pass `--mount` only when you need the agent to edit files in the current
directory — that shared path is writable from the VM and changes apply on the
host, so mount only directories you are willing to expose.

```text
   host                           Anka VM (kept after run)
  ┌───────────────┐   clone      ┌───────────────────────────────┐
  │ crypt grok    │ ──────────▶  │ grok --always-approve         │
  │ --mount       │ ◀── mount ─▶ │ /Volumes/My Shared Files/$CWD │
  └───────────────┘              └───────────────────────────────┘
```


![Demo of Crypt in action](./anka-crypt-demo-loop4.gif)


## Why

Agents run far more smoothly when they aren't stopping to ask permission for every
file edit or shell command. The flags that unlock that (`--always-approve`,
`--dangerously-skip-permissions`, `--dangerously-bypass-approvals-and-sandbox`) are
genuinely dangerous on your host. Crypt makes them safe by confining the agent to
an ephemeral VM.

## Requirements

- **[Anka Virtualization](https://docs.veertu.com/anka/anka-virtualization-cli/getting-started/installing-the-anka-virtualization-package/).** Any recent version works; **3.9 or newer** is required
  when using `--mount` (host directory mounting was added in
  [Anka 3.9.0](https://docs.veertu.com/anka/whats-new/anka-3.9.0/#ability-to-mount-host-directories-inside-of-the-vm)).
- **Apple Silicon** is required for `--mount`. Directory mounts are not supported on Intel.
- **Anka Enterprise** is required for `--no-local` network isolation.
- A base Anka VM that you have prepared with the agent installed and authenticated.

## Install

```sh
brew tap veertuinc/crypt https://github.com/veertuinc/crypt
brew trust veertuinc/crypt
brew update && brew install --cask crypt
```

Homebrew's short tap name (`veertuinc/crypt`) expects a repo named `homebrew-crypt`; the
explicit URL tells it to use this repository instead.

Or download the latest release (macOS only; installs to `/usr/local/bin`, or set `INSTALL_DIR`):

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/veertuinc/crypt/edge/scripts/install.sh)"
```

Or build from source (requires Go 1.25+):

```sh
go install github.com/veertuinc/crypt@latest
```

## Prepare a base VM (one time)

Crypt does not download VM templates. You create and prepare your own base VM, and
Crypt clones it for each run.

1. Create the base VM (name it `crypt-base`, or anything and pass `--vm` later):

   ```sh
   anka create crypt-base latest
   ```

2. Boot it and install + authenticate the agent(s) you want to use inside it:

   ```sh
   anka start crypt-base
   anka run crypt-base zsh -lc '/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" && brew install node'
   # Install Claude (claude)
   anka run crypt-base zsh -lc 'npm install -g @anthropic-ai/claude-code'
   # Optional: set the model to use (default is latest model from anthropic)
   anka run crypt-base bash -c "echo 'export ANTHROPIC_MODEL=\"claude-sonnet-4-5-20250929\"' >> ~/.zprofile"
   anka run crypt-base zsh -lc 'claude'    # follow the login prompts
   # Install Codex (codex)
   anka run crypt-base zsh -lc 'npm install -g @openai/codex'
   anka run crypt-base zsh -lc 'codex'     # follow the login prompts
   # Install Sakana Fugu (codex-fugu)
   anka run crypt-base zsh -lc 'curl -fsSL https://sakana.ai/fugu/install | bash'
   anka run crypt-base zsh -lc 'codex-fugu'     # follow the login prompts
   # Install Grok Build (grok / agent)
   anka run crypt-base zsh -lc 'curl -fsSL https://x.ai/cli/install.sh | bash'
   # Grok adds PATH to ~/.zshrc; Crypt uses login shells (~/.zprofile) for task runs
   anka run crypt-base bash -c "echo 'export PATH=\"\$HOME/.grok/bin:\$PATH\"' >> ~/.zprofile"
   anka run crypt-base zsh -lc 'grok login'     # follow the login prompts
   # stop the base VM
   anka stop crypt-base
   ```

> [!NOTE]
> Interactive sessions (`crypt grok` with no prompt) connect over SSH so the
> agent gets a real terminal. Crypt generates a dedicated SSH key per clone on
> first use (`~/.config/crypt/keys/<vm>/id_ed25519`) and authorizes it in that
> clone automatically via `anka run`; you only need Remote Login enabled in the
> base VM. Crypt logs in as the `anka` user by default — override with
> `CRYPT_SSH_USER`. Keys are removed when the clone is destroyed.
>
> Task-mode runs (`crypt grok "fix the test"`) and interactive SSH both launch
> agents with `zsh -lc`, a login shell that reads `~/.zprofile`, not `~/.zshrc`.
> If an installer only updates `~/.zshrc` (Grok does this), add its bin directory
> to `~/.zprofile` as shown above.

3. On first setup, harden network access on the base VM (recommended; requires
   Anka Enterprise). Clones inherit this setting:

> [!IMPORTANT]
> `--no-local` is only available with an Enterprise or Enterprise Plus license.

   ```sh
   anka modify crypt-base network --no-local
   ```

Every clone Crypt makes inherits this prepared state, so you only authenticate once.

## Usage

Two approaches:

1. Use `crypt grok` to run the agent in interactive mode.
2. Run `crypt grok "fix the UI bugs from the make test output"` for non-interactive mode.

```bash
❯ crypt grok 'who are you?'
crypt: VM name is crypt-clone-1
crypt: cloning crypt-base -> crypt-clone-1
crypt: starting crypt-clone-1
crypt: SSH: ssh -i '/Users/you/Library/Application Support/crypt/keys/crypt-clone-1/id_ed25519' -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 anka@192.168.64.8
crypt: VNC: open vnc://anka@192.168.64.8
crypt: mounting /Users/you/project
crypt: waiting for SSH on anka@192.168.64.8
crypt: authorizing SSH key in crypt-clone-1 via anka run
crypt: launching grok
I'm Grok Build, xAI's terminal-native coding agent. I can read and edit files, run
shell commands, search your codebase, and help with software engineering tasks.

Is there something specific you'd like help with?
crypt: kept VM crypt-clone-1 running (crypt destroy when done)


❯ crypt grok --mount "do you see the mount in the VM under /Volumes/My Shared Files"
Yes, I can see the mount! It's visible at `/Volumes/My Shared Files` and is mounted using **AppleVirtIOFS** (Apple's virtualization filesystem for sharing between host and VM).

**Mount details:**
- Device: `/dev/disk0`
- Mount point: `/Volumes/My Shared Files`
- Filesystem: AppleVirtIOFS
- Size: 926GB total, 789GB used, 137GB free
- Contains the `crypt` directory we're currently working in

The mount is working and accessible.
```

#### Examples

```sh
crypt claude --mount "keep going"                          # takes the current folder where we're executing the command and mounts that temporarily into the VM for the duration of the command
crypt claude                                               # interactive; VM kept until crypt destroy
crypt codex-fugu --mount "investigate the flaky test"      # Sakana Fugu (codex -p fugu)
crypt grok --mount "fix the failing test"                  # Grok Build
crypt --name backend claude --mount "add endpoint"         # separate named VM for another project
crypt claude --destroy "one-shot"                          # delete when the run ends
crypt destroy                                              # delete the kept VM for this directory
crypt --name backend destroy                               # delete a named VM
```

Flags:

| Flag         | Default      | Description                                        |
| ------------ | ------------ | -------------------------------------------------- |
| `--name`     | *(auto)* | Explicit clone VM name (`crypt-clone-1`, `crypt-clone-2`, … when omitted; same directory reuses its clone) |
| `--vm`       | `crypt-base` | Base Anka VM to clone for the sandbox              |
| `--cpu`      | `0`          | Override vCPU core count (`0` = use the VM setting) |
| `--memory`   | `0`          | Override RAM in MB (`0` = use the VM setting)       |
| `--mount`    | `false`      | Mount the current directory into the VM (exposes that path on the host) |
| `--destroy`  | `false`      | Delete the clone when the run ends (default: keep until `crypt destroy`) |
| `--no-local` | `false`      | Block VM-to-VM and VM-to-host network on the clone (Anka Enterprise)   |

Unknown flags (e.g. `--model`, `--resume`) are forwarded to the agent unchanged.

## How a run works

1. Verify Anka is installed and the base VM exists (Anka 3.9+ when using `--mount`).
2. `anka clone <base> crypt-clone-N` — an instant shadow clone (lowest free number; printed on stderr).
3. With `--no-local`, `anka modify <clone> network --no-local` — block VM-to-VM
   and VM-to-host network traffic (requires Anka Enterprise; usually set once on
   the base VM instead).
4. `anka start` the clone (applying any `--cpu` / `--memory` overrides first).
5. With `--mount`, `anka mount <clone> <cwd>` — your directory appears at
   `/Volumes/My Shared Files/project1` in the guest. The agent can read and write
   that path; changes are visible on the host. Only mount a directory you trust
   the agent with.
6. Launch the agent with its unattended-mode flags injected. For Claude Code,
   Crypt also pre-trusts the guest workspace in `~/.claude.json` so the
   "Do you trust this folder?" dialog is skipped. A task prompt
   (`crypt claude "fix the UI"`) runs unattended over SSH; an interactive
   session (`crypt claude`) connects over `ssh -t` so the agent gets a real
   terminal for its full TUI.
7. On exit (including Ctrl-C), the clone stays running and is kept on disk unless
   you passed `--destroy`, which deletes it. Remove kept clones with
   `crypt destroy` (or `crypt --name <vm> destroy`). If `--mount` is added to an
   already-running kept clone, Crypt unmounts that temporary host path after the
   command finishes. Your base VM is never modified.

## Network isolation

> [!IMPORTANT]
> These features require an Enterprise or Enterprise Plus license.

[`--no-local`](https://docs.veertu.com/anka/anka-virtualization-cli/advanced-security-features/#block-vm-to-vm-and-vm-to-host-communication)
blocks VM-to-VM and VM-to-host **network** communication. Enable it once when you
prepare your base VM (recommended — clones inherit the setting):

```sh
anka modify crypt-base network --no-local
```

Or pass `--no-local` on a Crypt run to apply it to that clone only.

For tighter control, bake [IP filtering rules](https://docs.veertu.com/anka/anka-virtualization-cli/advanced-security-features/#ip-filtering-rules)
into your base VM before Crypt clones it. Rules are evaluated in order; the first
match wins. Example — block local traffic and deny everything else:

```sh
cat <<'EOF' | anka modify crypt-base network -f-
block local
block any
EOF
```

You can also set global host rules with `anka config net_filter`, or embed
per-VM rules so clones inherit them. See Anka's
[Advanced Security Features](https://docs.veertu.com/anka/anka-virtualization-cli/advanced-security-features/)
for the full rule syntax and additional options (including TUN/WireGuard on the
host for routing all VM traffic through a VPN).

## A note on file syncing

When using `--mount`, Anka shares directories via Apple's virtiofs implementation
on macOS. That stack has a known caveat: **edits made on the host after mounting
may appear stale inside the guest.** This is an Apple platform limitation — not a
Crypt bug. In practice it does not affect the common workflow — the agent edits
files *inside* the guest, and those writes propagate back to the host correctly.
If you edit files on the host mid-session, prefer creating new files or atomically
replacing them (write a temp file, then `mv` over the target) so the guest picks
up the change.