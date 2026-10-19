# Installation, host state & migrations

[← Linux guide](../README.md)

Use this reference for remote installs, build limits, passwords, lock modes,
and migration from an existing installation. For a first run, start with the
[short installation guide](../README.md#install).

## Install / Reconfigure

Run from the repository root:

```bash
nix run "path:$PWD"
```

Use the repository root flake for local runs. It exposes the installer as the
default app and carries the reusable shell and workstation inputs.

Or run directly from GitHub without cloning first. This opens the interactive
TUI and uses the stable `main` branch by default:

```bash
nix run --refresh 'github:TakuyaYagam1/wahrwelt'
```

This is the normal thin apply path. Nix fetches the repository source into
`/nix/store`, the wrapped installer points `WAHRWELT_REPO_ROOT` at that immutable
source, then the installer writes a small `/etc/nixos` wrapper that tracks
the selected `github:TakuyaYagam1/wahrwelt/main?dir=Linux/NixOS/presets/<preset>`
entrypoint by default.

### Channels

To test the latest fixes before they are merged to `main`, run the TUI from the
`dev` branch and select `General -> Wahrwelt channel -> development` before
applying:

```bash
nix run --refresh 'github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS#wahrwelt' -- tui
```

The TUI `General` section includes `Wahrwelt channel`. Keep `stable` to track the
`main` branch after install, or select `development` to generate a wrapper that
tracks `github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS/presets/<preset>`.

Nix can reuse a cached branch revision. Keep `--refresh` when you want the
current remote revision, for example just after pushing a commit.
For a cached stable TUI launch, `--refresh` is optional:

```bash
nix run 'github:TakuyaYagam1/wahrwelt'
```

### Pin the installer source

Pin a full commit SHA to run a specific installer revision. A branch tracks
new commits instead. This chooses the installer source; the installed system
continues to follow the channel selected in the TUI:

```bash
# Specific commit (full SHA):
nix run 'github:TakuyaYagam1/wahrwelt/<commit>'

# Specific branch (e.g. dev):
nix run 'github:TakuyaYagam1/wahrwelt/dev'
```

### Before first apply

Fresh install through `/mnt` is not supported in v1. Run this on an already
booted NixOS system with an existing `/etc/nixos/hardware-configuration.nix`.
On first adoption from a stock NixOS/KDE install, Wahrwelt replaces the active
`/etc/nixos/configuration.nix` with a clean host-local override file. The old
tree is kept in the `/etc/nixos.bak.<timestamp>.<pid>.<n>` backup made before
writing `/etc/nixos`. VPN/proxy tools such as Amnezia are only a network
workaround before running Wahrwelt; they are not required in the bootstrap
`configuration.nix`.

### Binary caches

Nix may ask whether to allow the flake app to use additional substituters. That
is expected for this config; the installer and checks use these binary caches:

- `https://nix-community.cachix.org`
- `https://hyprland.cachix.org`
- `https://quickshell.cachix.org`
- `https://numtide.cachix.org`
- `https://wahrwelt.cachix.org`

The public Wahrwelt cache is configured to publish selected personal-preset
QuickShell, Caelestia, Noctalia, Codex CLI, and OmniRouter builds from the
`main` branch. The `Cache desktop builds` workflow also supports manual runs.
Publishing requires the repository Actions secret
`CACHIX_AUTH_TOKEN`; the token is never needed on machines that only install
the configuration. Other packages still come from their usual substituters or
build locally when a matching binary is unavailable. Claude Code, Claude Desktop,
and Codex Desktop are not uploaded to this public cache because their package
metadata marks them unfree. Happ also comes from an upstream binary package
without redistribution metadata, and Portainer is an OCI image rather than a
Nix build. The 5 GB cache limit can evict older revisions, so a cache hit is
not guaranteed after every flake update.

### Build resources

The first full build can still be large because this config includes multiple
Wayland shells, Qt/QML packages, desktop apps, and optional CTF/development
stacks.

Wahrwelt's low-RAM bootstrap path limits parallel builds to reduce memory
pressure on 8 GB machines: `max-jobs = 1`, `cores = 2`,
zram swap is enabled, and the existing `/var/lib/swapfile` remains a disk
fallback. On Btrfs, NixOS creates the managed swapfile with
`btrfs filesystem mkswapfile`, so manual CoW workarounds are not needed for that
declarative swapfile.

For the very first remote installer launch, pass the same limits before the app
separator because the installer cannot control the resources used to build
itself. This launches CLI apply without the TUI. It asks before switching unless
you pass `--yes`; use the TUI when you need to choose account settings or a preset:

```bash
nix run --refresh --option max-jobs 1 --option cores 2 \
  'github:TakuyaYagam1/wahrwelt?dir=Linux/NixOS#wahrwelt' -- \
  apply --lock-mode managed
```

For a 16-thread machine with about 30 GB RAM, a bounded pair such as four
jobs with four cores each can be a useful starting point. Put persistent
overrides in `/etc/nixos/configuration.nix`; Apply regenerates `host-vars.nix`:

```nix
{ lib, ... }:
{
  wahrwelt.nix.maxJobs = lib.mkForce 4;
  wahrwelt.nix.cores = lib.mkForce 4;
}
```

Merge these settings into the existing module. If the active system is still
using the old low-RAM Nix daemon config, run the first switch with explicit
options:

```bash
sudo nixos-rebuild switch --impure --flake /etc/nixos#NixOS --option max-jobs 4 --option cores 4
```

Use your configured host output instead of `#NixOS` if it has a different name.

## State and secrets

The installer writes machine-local state to:

```text
/etc/nixos/installer-state.json
```

The in-progress TUI draft is stored at:

```text
$XDG_STATE_HOME/wahrwelt/draft.json
```

Plain passwords are never stored in state or draft JSON.

The Linux password hash is stored outside the flake source:

```text
/etc/wahrwelt/hashed-password
```

It is owned by root and is not copied into the Nix store. The non-secret marker
`/etc/nixos/.wahrwelt-password-hash-enabled` only tells the host module to use
that external file. Existing `hashed-password.nix` files are accepted only as
v1-to-v2 migration inputs and are removed from the live flake tree after the
external hash is published safely.

Optional sops-nix system secrets live at:

- `/etc/nixos/secrets/secrets.yaml`: system secrets, decrypted through the host
  SSH key.

To obtain the host's public age recipient:

```bash
nix-shell -p sops ssh-to-age age
ssh-to-age < /etc/ssh/ssh_host_ed25519_key.pub
```

Configure `/etc/nixos/secrets/.sops.yaml` with a creation rule matching
`secrets.yaml`. Include the host recipient for system decryption and your own
age recipient for editing. Only public recipients belong in this file; keep
private keys outside the configuration tree. Then open the encrypted file:

```bash
cd /etc/nixos/secrets
sops --config .sops.yaml secrets.yaml
```

In the default thin layout, `mkWahrweltHost` wires
`/etc/nixos/secrets/secrets.yaml` into sops-nix when that file exists. Declare
the required `sops.secrets.<name>` entries in an imported local system module
under `/etc/nixos/user/`.

On repeat TUI runs, the `Passwords` section checks the external hash and known
v1 migration paths, then shows
`already exists` when a value is present. Leaving both password and confirmation
blank preserves the existing value. Enter a new value only when changing it.

For CLI apply, pass secrets through files so they do not leak
through shell history:

```bash
nix run "path:$PWD" -- apply \
  --user-password-file /path/to/user-password
```

Secret files must be regular files, non-symlinks, and not group/world readable.

## Apply flow

The installer applies changes defensively:

1. Creates a temporary staging wrapper flake for `/etc/nixos`.
2. Regenerates `host-vars.nix` from the selected settings. Creates
   `configuration.nix` and `home.nix` templates when they do not already exist.
3. Preserves host-local `hardware-configuration.nix`, `user/`, and `secrets/`.
   Existing `hashed-password.nix` files are handled only as exact v1 migration
   inputs. Existing `private/` trees are renamed to `user/`
   only when the canonical target is absent. Existing legacy MySetup thin installs also keep
   `flake.lock`, `configuration.nix`, and `home.nix`. Existing wrapper
   `flake.nix` files receive targeted changes to Wahrwelt-owned fields;
   custom inputs, modules, and outputs are preserved. Conflicting user edits
   stop the apply before publication.
   A missing flake gets a new thin wrapper. An existing unrecognized flake
   requires explicit migration and is left unchanged.
4. Prepares a non-secret password enable marker in the staging flake. The
   password hash stays outside that tree.
5. Runs `nix flake update --flake <staging>` for the default thin wrapper, so
   the installed host owns the important external input revisions in
   `/etc/nixos/flake.lock`. Use `--lock-mode managed` to keep the compatibility
   behavior of updating only the Wahrwelt input and using Wahrwelt's transitive lock.
6. Runs `nixos-rebuild dry-build` against the staging flake before touching
   `/etc/nixos`.
7. Backs up `/etc/nixos` to a unique `/etc/nixos.bak.<timestamp>.<pid>.<n>`.
8. Publishes any supplied password hash as root-owned
   `/etc/wahrwelt/hashed-password` and syncs the thin staging tree to `/etc/nixos`
   while preserving migration state.
9. Applies selected user dotfiles and reloads Hypr when a session is running.
10. Asks before `nixos-rebuild switch`, unless confirmation was skipped with `--yes`.
11. Writes `/etc/nixos/installer-state.json` only after switch succeeds, then
    safely removes `/etc/nixos/wahrwelt/state.json` and
    `/etc/nixos/mysetup/state.json`. An exact empty legacy parent is moved to an
    identity-proven hidden quarantine inside `/etc/nixos`; a nonempty or
    concurrently replaced parent is never deleted.
12. When exact v1 evidence exists, a versioned one-shot migration rewrites only
    known generated wrapper and lock fields. It validates with an offline build
    from the revision already present in the current generation, then publishes
    through one pinned same-filesystem exact atomic exchange. It never updates a
    mutable branch during boot and never rewrites arbitrary user modules.

Use `--layout full` to keep the old full-mirror behavior for debugging or
migration fallback. Use `--lock-mode managed` with the thin layout when you want
the host to track only the Wahrwelt commit while reusing Wahrwelt's tested
transitive dependency lock.

Rollback is intentionally scoped to `/etc/nixos`. If user dotfile sync fails
after partial writes under `~/.config`, run `wahrwelt doctor` and re-apply or
clean those user-level files separately.

`/etc/nixos` is the canonical activation target. Building a full system
toplevel directly from the cloned checkout can fail until host-local files are
present; use the installer or build from `/etc/nixos` for real activation
checks.

The default installed layout is intentionally small:

```text
/etc/nixos/
├── flake.nix
├── flake.lock
├── host-vars.nix
├── hardware-configuration.nix
├── configuration.nix      # system-level overrides
├── home.nix               # Home Manager overrides
├── .wahrwelt-password-hash-enabled # non-secret external-hash marker
├── user/
│   └── default.nix         # local-only Nix module imports
├── secrets/               # optional system sops-nix secrets
└── installer-state.json   # written after successful activation
```

Add NixOS packages, services, and system overrides to `configuration.nix`.
Add user packages and Home Manager overrides to `home.nix`.
Use `user/` for explicit local imports that must not live in the public
repository. Fresh installs import `./user` from `configuration.nix`; add local
modules to `user/default.nix`.

On the first successful update of an existing installation, either a Wahrwelt
apply or the one-shot service activated by the ordinary `nixos-update` flow
migrates `/etc/nixos/private/` to `/etc/nixos/user/`. It reads legacy state
from `/etc/nixos/wahrwelt/state.json` or `/etc/nixos/mysetup/state.json` and
publishes `/etc/nixos/installer-state.json` only with a validated configuration.
The automatic service retains the displaced tree beside `/etc/nixos` for
manual recovery. Home Manager also migrates exactly one
`~/.config/hypr/mysetup/` or
`~/.config/hypr/wahrwelt/` tree to `~/.config/hypr/user/`. If old and new trees
coexist, migration stops without merging or deleting either tree.

## Supported MySetup compatibility

Recognized older installations migrate automatically. The old repository URL, `#mysetup` output,
`mysetup` executable, `nixosModules.mysetup`, `config.mysetup`, and
`mysetup.lib.mkMySetupHost` remain supported aliases. The next Wahrwelt apply rewrites a
recognized generated wrapper to the new `wahrwelt` input and constructor. The first
successful update also moves host-local modules to `/etc/nixos/user/`, installer state
to `/etc/nixos/installer-state.json`, and writable Hypr modules to
`~/.config/hypr/user/`. Internal runtime state remains under the `wahrwelt` namespace.
If both an old and a new path contain conflicting data, migration stops before
overwriting either one.

This compatibility command remains valid:

```bash
nix run --refresh 'github:TakuyaYagam1/MySetup?dir=Linux/NixOS#mysetup' -- doctor
```
