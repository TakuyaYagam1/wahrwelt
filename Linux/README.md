# NixOS Configuration

NixOS + Hyprland configuration managed by the `wahrwelt` Go TUI/CLI installer.
Hyprland itself is configured through Lua for Hyprland 0.55+; this setup no
longer maintains a `hyprland.conf` fallback.

The old `install.sh` and `dots/install.fish` entry points are gone. System
settings, host-local generated files, and user dotfiles are applied through the
flake app.

## Quick Start

Already booted into NixOS and the host has `/etc/nixos/hardware-configuration.nix`?
The single command below opens the TUI installer, lets you pick a preset, and
applies everything:

```bash
nix run "path:$PWD"
```

Run this from the cloned repository root. The detailed forms (GitHub-direct,
pinned commit/branch, refresh) are documented in the next section.

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

### Supported MySetup compatibility

Existing hosts do not need a manual migration. The old repository URL, `#mysetup` output,
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

To test the latest fixes before they are merged to `main`, run the TUI from the
`dev` branch and select `General -> Wahrwelt channel -> development` before
applying:

```bash
nix run --refresh 'github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS#wahrwelt' -- tui
```

The TUI `General` section includes `Wahrwelt channel`. Keep `stable` to track the
`main` branch after install, or select `development` to generate a wrapper that
tracks `github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS/presets/<preset>`.

Nix caches the resolved source for ~1 hour (`tarball-ttl` default). If you just
pushed a commit and want the new HEAD right now, keep `--refresh` in the command.
For a cached stable TUI launch, `--refresh` is optional:

```bash
nix run 'github:TakuyaYagam1/wahrwelt'
```

For reproducible installs, pin a commit, branch, or tag:

```bash
# Specific commit (full SHA):
nix run 'github:TakuyaYagam1/wahrwelt/<commit>'

# Specific branch (e.g. dev):
nix run 'github:TakuyaYagam1/wahrwelt/dev'
```

Fresh install through `/mnt` is not supported in v1. Run this on an already
booted NixOS system with an existing `/etc/nixos/hardware-configuration.nix`.
On first adoption from a stock NixOS/KDE install, Wahrwelt replaces the active
`/etc/nixos/configuration.nix` with a clean host-local override file. The old
tree is kept in the `/etc/nixos.bak.<timestamp>.<pid>.<n>` backup made before
writing `/etc/nixos`. VPN/proxy tools such as Amnezia are only a network
workaround before running Wahrwelt; they are not required in the bootstrap
`configuration.nix`.

Nix may ask whether to allow the flake app to use additional substituters. That
is expected for this config; the installer and checks use these binary caches:

- `https://nix-community.cachix.org`
- `https://hyprland.cachix.org`
- `https://quickshell.cachix.org`
- `https://numtide.cachix.org`

The first full build can still be large because this config includes multiple
Wayland shells, Qt/QML packages, desktop apps, and optional CTF/development
stacks.

Wahrwelt's low-RAM bootstrap path uses conservative build settings so 8 GB RAM
machines do not get killed by OOM during rebuilds: `max-jobs = 1`, `cores = 2`,
zram swap is enabled, and the existing `/var/lib/swapfile` remains a disk
fallback. On Btrfs, NixOS creates the managed swapfile with
`btrfs filesystem mkswapfile`, so manual CoW workarounds are not needed for that
declarative swapfile.

For the very first remote installer launch, pass the same limits before the app
separator because the installer cannot control the resources used to build
itself. This is a non-interactive apply path and will not open the TUI:

```bash
nix run --refresh --option max-jobs 1 --option cores 2 \
  'github:TakuyaYagam1/wahrwelt?dir=Linux/NixOS#wahrwelt' -- \
  apply --lock-mode managed
```

Installed machines with more RAM should override the low-RAM values in
`host-vars.nix`. For a 16-thread machine with about 30 GB RAM, use a bounded pair
such as `maxJobs = 4; cores = 4;`. If the active system is still using the old
low-RAM Nix daemon config, run the first switch with explicit options:

```bash
sudo nixos-rebuild switch --flake /etc/nixos#NixOS --option max-jobs 4 --option cores 4
```

## State And Secrets

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

System secret bootstrap:

```bash
nix-shell -p sops ssh-to-age age
ssh-to-age -private-key -i /etc/ssh/ssh_host_ed25519_key > /tmp/hostkey.txt
# or: sudo ssh-to-age < /etc/ssh/ssh_host_ed25519_key.pub
sops /etc/nixos/secrets/secrets.yaml
```

Put the public age key into `/etc/nixos/secrets/.sops.yaml`. In the default thin
layout, `mkWahrweltHost` wires `/etc/nixos/secrets/secrets.yaml` into sops-nix
when that file exists. Declare the required `sops.secrets.<name>` entries in an
imported local system module under `/etc/nixos/user/`.

On repeat TUI runs, the `Passwords` section checks the external hash and known
v1 migration paths, then shows
`already exists` when a value is present. Leaving both password and confirmation
blank preserves the existing value. Enter a new value only when changing it.

For non-interactive CLI apply, pass secrets through files so they do not leak
through shell history:

```bash
nix run "path:$PWD" -- apply \
  --user-password-file /path/to/user-password
```

Secret files must be regular files, non-symlinks, and not group/world readable.

## What The Installer Handles

- Host/user settings: hostname, username, full name, home directory.
- Wahrwelt channel: stable/main branch or development/dev branch for the
  generated thin wrapper.
- Git identity: `user.name` and `user.email` for Home Manager Git config.
- Locale/region fields: timezone, locale, console keymap, weather location.
- Display: monitor name, mode, position, scale, Hypr keyboard layouts/toggle.
- Package preset: `personal`, `developer`, `desktop`, or `minimal`.
- Feature flags: GPU type, Secure Boot, CTF tools, OmniRouter, Portainer.
- Passwords: Linux user password hash.
- Dots: Hyprland Lua config, scripts chmod, wallpapers, Zen Browser Catppuccin
  chrome, optional Sine profile, Neovim, v2rayN `sing-box`, and the runtime
  shell selector with End4 Official/pC variants.

Presets are cumulative - each one includes everything below it and adds more
(`minimal` -> `desktop` -> `developer` -> `personal`). The full per-tier
breakdown is in the [root README](../README.md#package-presets); in short:

- `minimal`: console only. Core CLI/system tools plus Wayland libraries, but
  **no display manager and no Hyprland desktop** - you log in at a text TTY. If
  you picked this and hit a black screen with a blinking cursor, that is the
  console login working, not a bug.
- `desktop`: the first preset with a graphical session. Adds the SDDM login
  screen, Hyprland, three shell families and four runtime profiles
  (`caelestia`, `noctalia`, `end4`, `end4-pc`), and everyday GUI apps (browser,
  chat, office, media, file manager).
- `developer`: `desktop` plus developer/API/container tooling, Claude Code,
  Codex CLI, and Codex Desktop.
- `personal`: `developer` plus the full private-workstation load - extra apps,
  IDEs, additional AI tools, and games. This is the heaviest build.

Steam does not open Remote Play, dedicated-server, or local-transfer firewall
ports automatically. Opt in to only the ports required by the current host.

How it works under the hood: `lib/mk-host.nix` composes the shared
`modules/mysetup-stack.nix`, and that stack imports the base, desktop,
developer, and feature profile layers. Each module turns itself on or off based
on `wahrwelt.packages.preset`, so all four presets run through the same code
path.

Each preset has a public flake and its own lock under
`Linux/NixOS/presets/<preset>`. `minimal` contains only core inputs and does not
own `end4-pc`. `desktop`, `developer`, and `personal` add the graphical shell
graph, including the pinned `pctrade/end4-pC` source. The latter two also add
the Claude Code, Codex CLI, and Codex Desktop inputs. The old `Linux/NixOS`
flake remains the full compatibility entrypoint and owns `end4-pc` too.

The generated wrapper `flake.nix` points at the selected preset flake.
Independent mode keeps moving core inputs host-owned and adds
Quickshell/End4/Zen, including `end4-pc`, only for desktop or higher. AI inputs
stay owned by the developer/personal preset lock. Lanzaboote is injected by the
host wrapper only when Secure Boot is enabled, so it never pollutes the preset
locks.

### ChatGPT Desktop settings

The `developer` and `personal` presets install the unofficial
[`ilysenko/codex-desktop-linux`](https://github.com/ilysenko/codex-desktop-linux)
Nix package and points its launcher at the Codex CLI from this configuration.
The app still owns its runtime settings under `~/.config/codex-desktop`; that
directory is deliberately not declared through Home Manager
`xdg.configFile`, so its files stay writable and survive NixOS rebuilds without
becoming read-only `/nix/store` symlinks. The separate `~/.codex` CLI
configuration is also left untouched.

## Hyprland Lua Runtime

The active Hyprland config is Lua-only and assumes Hyprland 0.55 or newer:

- Home Manager owns the stable entrypoints, canonical modules, scripts,
  `shell-common-rules.lua`, `end4-adapter.lua`, and the shared patched
  `hypr/end4` tree. These paths are managed store links and must be changed in
  this repository.
- `~/.config/hypr/hyprland.lua` loads the writable
  `$XDG_STATE_HOME/wahrwelt/hypr-runtime/hyprland.lua`. The shell runtime owns
  that file plus `shell-profile.lua`, `shell-launcher.lua`,
  `shell-keybinds.lua`, `hyprlock.conf`, and `hypridle.conf` in the same state
  directory.
- `~/.config/hypr/user/` is an ordinary user directory. Its `hyprland.lua`
  entrypoint is managed and refreshed, but `default.lua` and arbitrary user
  modules are writable and preserved. Installer and Home Manager seed
  `default.lua` only when no file or symlink already exists.

`hyprlock.conf` and `hypridle.conf` intentionally stay in hyprlang because the
Hyprland companion tools have not moved to Lua config here. Hyprland's old
`hyprland.conf`, `shell-keybinds.conf`, `shell-launcher.conf`, and
`shell-profile.conf` are no longer active entrypoints.

Lua bind helpers call `hl.dsp.*` directly. This matters on Hyprland 0.55:
legacy commands such as `hyprctl dispatch movewindow l` are parsed as Lua and
will not behave like old hyprlang dispatchers.

### Canonical load order

The stable entrypoint loads the runtime entrypoint, which then loads
`~/.config/hypr/user/hyprland.lua`. The canonical module order inside that
file is exact:

1. `lib.wahrwelt` and base modules: `env`, `general`, `input`, `misc`,
   `animations`, `decoration`, `group`, `execs`, `rules`, `gestures`,
   `scrolling`, and `keybinds`.
2. Runtime adapters: `shell-profile.lua`, `shell-launcher.lua`, then
   `shell-keybinds.lua`.
3. `vm-keybinds`.
4. `wahrwelt.default` when it exists. Only when it is absent are the compatibility
   modules `wahrwelt.execs`, `wahrwelt.general`, `wahrwelt.rules`, and `wahrwelt.keybinds`
   loaded as fallbacks.

There is no directory scan or automatic module discovery. The seeded
`default.lua` explicitly opts into the four compatibility modules with
`optional_require`, so existing per-file customizations keep working while a
custom `default.lua` remains in full control.

The physical directory is named `user/`, but its Lua namespace remains
`wahrwelt.*`. A scoped loader maps only that namespace to files under
`hypr/user/`. `lib.wahrwelt` and the internal
`$XDG_STATE_HOME/wahrwelt/` runtime namespace are also unchanged.

### Customizing Hyprland without forking the repo

Edit `~/.config/hypr/user/default.lua`. It can use ordinary `require` for a
required arbitrary module and `optional_require` for a file that may be absent:

```lua
local wahrwelt = require("lib.wahrwelt")

require("wahrwelt.my-host")
wahrwelt.optional_require("wahrwelt.keybinds")
```

This loads `~/.config/hypr/user/my-host.lua` and, when present,
`~/.config/hypr/user/keybinds.lua`. Nothing else in the directory is loaded
unless `default.lua` requests it. In particular, `user.env` is not an
implicit hook.

For keybind overrides, Hyprland does not auto-replace a duplicate bind - if you
bind an already-used combo again, both actions fire. To cleanly override one,
`hl.unbind()` it first, then rebind
([Hyprland Wiki: Binds](https://wiki.hypr.land/Configuring/Basics/Binds/)).

```lua
hl.unbind("SUPER + SHIFT + Q")
hl.bind("SUPER + SHIFT + Q", hl.dsp.exec_cmd("openvpn --config ~/my.ovpn"))
```

Apply edits without logging out:

```bash
hyprctl reload
```

### Shared window rules and app-aware close

`hyprland/rules.lua` requires the Home Manager-owned top-level
`shell-common-rules.lua`. The installer copies the same file during direct
dotfile apply. It defines the four shared special-workspace routes:
`special:sysmon`, `special:music`, `special:communication`, and `special:todo`.
End4 does not load this file directly; the canonical base loads it before the
profile adapter, avoiding duplicate rules.

`Super+Q` runs `scripts/close-active.sh` in every profile. Normal windows close
by exact address. Spotify is routed to `special:music`, or that workspace is
toggled when Spotify is already there. A failed addressed close falls back to
the active-window kill dispatcher.

## Runtime Shells

Shell selection is runtime-managed after the system is applied. It is no longer
a TUI install-time choice.

Use:

```text
Super+Shift+W
```

The QuickShell selector opens on the focused monitor. It presents three shell
families, with a segmented Official/pC choice inside End4:

- Caelestia -> profile ID `caelestia` (default)
- Noctalia -> profile ID `noctalia`
- End4 Official -> profile ID `end4`, QuickShell config `ii`
- End4 pC -> profile ID `end4-pc`, QuickShell config `end4-pC`

An explicit selector choice runs one destination-aware transition. Caelestia
and Noctalia take nine seconds: three seconds to hide, three full ticks for the
shell handoff, and three seconds to reveal. End4 Official and End4 pC take
eleven seconds with five full handoff ticks instead. The handoff interval starts
only after every output has presented the opaque cover. Login and Home Manager
auto-start do not animate.

If capture cannot start, the switch continues without an overlay. If an opaque
frame cannot be confirmed on every output, the switch is canceled before the
old shell stops. If the target cannot launch before the reveal boundary, the
previous shell is restored. Target readiness does not restart or extend the
timeline. If the selected shell fails, rollback restores the previous shell.

The full per-shell keybind reference (common + Caelestia + Noctalia + End4)
lives in the [GitHub Wiki](https://github.com/TakuyaYagam1/wahrwelt/wiki) or
[`Linux/keybinds.md`](keybinds.md).

Runtime state:

```text
$XDG_STATE_HOME/wahrwelt/active-shell
```

The file is not preseeded by Home Manager. `start-shell.sh` updates it only
after the requested runtime is prepared and the shell starts successfully.

Runtime log:

```text
$XDG_RUNTIME_DIR/wahrwelt-shell.log
```

Manual switch commands:

```bash
~/.config/hypr/scripts/start-shell.sh caelestia
~/.config/hypr/scripts/start-shell.sh noctalia
~/.config/hypr/scripts/start-shell.sh end4
~/.config/hypr/scripts/start-shell.sh end4-pc
```

The shell stack is intentionally split:

- `caelestia` and `noctalia` use the installer-managed `Linux/dots/hypr`
  Lua entrypoint with shared `shell-common-keybinds.lua` and
  `shell-workspace-keybinds.lua` fragments.
- End4 Official and pC share the dedicated End4 Home Manager and patched
  Hyprland tree. The top-level `end4-adapter.lua` loads that tree
  in an isolated Lua scope after canonical input, gestures, shared rules, and keybinds.
  Official starts `qs -c ii` from
  `~/.config/quickshell/ii`; pC starts `qs -c end4-pC` from
  `~/.config/quickshell/end4-pC`.
- Both End4 variants read the same mutable
  `~/.config/illogical-impulse/config.json`, so themes and user settings remain
  dynamic and survive switching between Official and pC without a rebuild.
- End4 sources are immutable Nix store outputs. The pC fork's upstream
  self-update path is disabled; updates arrive only through Wahrwelt flake lock
  refreshes and CI validation.

Ownership contract:

- Installer owns first-apply, legacy, and `minimal` user dotfile sync,
  unmanaged backups, executable bits for Hypr scripts, and immediate runtime
  bootstrap. On an established `desktop`, `developer`, or `personal` Home
  Manager tree, it leaves the active generation untouched until the rebuild;
  Home Manager publishes the new coherent tree during activation.
- Home Manager owns stable Hypr entrypoints, managed Hypr scripts, shell
  selector and transition assets, and shell profile metadata after rebuild.
- Runtime shell scripts own mutable files under `$XDG_STATE_HOME/wahrwelt` and
  may rewrite active-shell fragments during profile switches.
- User/vendor state remains mutable under shell-specific config/cache paths,
  especially Caelestia/Noctalia JSON and end4 Illogical Impulse settings.

Validate the complete runtime contracts and realize the independently
validated End4 Hypr artifact with:

```bash
make -C Linux test-hypr-integration
make -C Linux nix-shell-transition-build
make -C Linux nix-end4-hypr-build
make -C Linux nix-end4-pc-quickshell-build
```

## Boot Theme

Customize the GRUB, SDDM, and Plymouth boot logos with your own image.

Drop your own logo into:

```text
~/.config/wahrwelt/boot-theme/
```

First apply seeds that directory once with an example `logo.png` (the current
default theme logo) and a `README.txt` explaining the naming below. After that
first seed it's entirely yours - delete `logo.png` and it stays deleted, it
won't come back on the next apply.

Naming, highest priority first:

- `grub.png` / `grub.jpg` - GRUB boot menu logo only
- `sddm.png` / `sddm.jpg` - SDDM login avatar only
- `plymouth.png` / `plymouth.jpg` - Plymouth boot splash only
- `logo.png` / `logo.jpg` - shared fallback used by all three

A per-service file always wins over `logo.png`. This is deliberately strict
once the directory exists: every one of grub/sddm/plymouth must resolve to a
real file (its own override or `logo.png`) - covering some services and
leaving others with no fallback fails the build instead of silently guessing.
Delete the whole directory to go back to the built-in default. GRUB's image
is auto-resized to 320x320 (its theme layout is hand-calibrated for that
size); SDDM and Plymouth accept any aspect ratio.

Reading these files requires `--impure` on `nixos-rebuild` (they live outside
the flake source, under `$HOME`) - `nixos-update` and `wahrwelt apply` already
pass it. Running `nixos-rebuild` yourself for something else? Add `--impure`
or these overrides silently fall back to the built-in default.

Takes effect on the next `wahrwelt apply` / `nixos-rebuild switch`. SDDM shows
the new avatar at the next login screen; GRUB and Plymouth render before Linux
even starts, so you need an actual reboot to see them - `switch` alone
prepares the files but can't repaint a bootloader or splash screen that's
already run.

## Apply Flow

The installer applies changes defensively:

1. Creates a temporary staging wrapper flake for `/etc/nixos`.
2. Writes generated `host-vars.nix`, `configuration.nix`, and `home.nix`
   templates when they do not already exist.
3. Preserves host-local `hardware-configuration.nix`, `user/`, and `secrets/`.
   Existing `hashed-password.nix` files are handled only as exact v1 migration
   inputs. Existing `private/` trees are renamed to `user/`
   only when the canonical target is absent. Existing legacy MySetup thin installs also keep
   `flake.lock`, `configuration.nix`, and `home.nix`; generated wrapper
   `flake.nix` files may be regenerated to pick up the selected lock mode.
   Stock NixOS or legacy non-thin configs are replaced with the generated thin
   wrapper and clean override templates.
4. Publishes the password hash as root-owned
   `/etc/wahrwelt/hashed-password`; the staging flake contains only a non-secret
   enable marker.
5. Runs `nix flake update --flake <staging>` for the default thin wrapper, so
   the installed host owns the important external input revisions in
   `/etc/nixos/flake.lock`. Use `--lock-mode managed` to keep the compatibility
   behavior of updating only the Wahrwelt input and using Wahrwelt's transitive lock.
6. Runs `nixos-rebuild dry-build` against the staging flake before touching
   `/etc/nixos`.
7. Backs up `/etc/nixos` to a unique `/etc/nixos.bak.<timestamp>.<pid>.<n>`.
8. Syncs the thin staging tree to `/etc/nixos` while preserving migration state.
9. Applies selected user dotfiles and reloads Hypr when a session is running.
10. Asks before `nixos-rebuild switch` in TUI mode.
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

## Commands

```bash
# Local checkout: open the interactive TUI.
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt"
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- tui

# Remote stable/main: open the interactive TUI.
nix run --refresh 'github:TakuyaYagam1/wahrwelt'

# Remote latest/dev: open the interactive TUI, then select the development channel.
nix run --refresh 'github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS#wahrwelt' -- tui

# Read-only inspection commands.
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- doctor
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- print-state

# Non-interactive apply commands. These use saved state and do not open the TUI.
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --no-switch
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --source-channel development --no-switch
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --lock-mode managed --no-switch
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --layout full --no-switch

# Managed cleanup.
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- cleanup
```

`apply --no-switch` is a validation mode: it stages the build and stops after
`nixos-rebuild dry-build`, before writing `/etc/nixos`, user dotfiles,
activation, or activated state.

Use `--dry-run` to preview filesystem actions where supported. The installer logs
commands but does not execute external command actions through the shared runner,
and it avoids host-local side effects such as password hashing or writing
`/etc/nixos`:

```bash
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --dry-run --no-switch
```

Useful checks after changing installer or shell integration:

| Make target | Run it when |
| --- | --- |
| `make -C Linux/installer check` | Before pushing or applying - full local CI: lint, fmt-check, hypr-bind-check, shell-check, tests, nix evals. |
| `make -C Linux/installer shell-check` | After editing Hypr scripts, JSON, or Python patch sources under `Linux/dots/hypr/`. |
| `make -C Linux test-hypr-integration` | After changing shell lifecycle, overlay behavior, Lua adapters, shared rules, app-aware close behavior, or selector/transition models. |
| `make -C Linux nix-shell-transition-build` | Realize the Home Manager-owned transition artifact and verify QML, the compiled shader, duration, license, and lock-free contract. |
| `make -C Linux/installer nix-hm-eval` | After touching `home/`, End4 runtime-env, or shell-profile imports - evaluates the runtime shell module and all-on Home Manager imports including End4 Official and pC. |
| `make -C Linux nix-end4-hypr-build` | Realize the exact Home Manager-owned validated End4 Hypr artifact and inspect lifecycle/rules contracts. |
| `make -C Linux nix-end4-pc-quickshell-build` | Realize the managed End4 pC QuickShell tree and reject any direct QuickShell lifecycle launch. |
| `make -C Linux/installer nix-installed-mirror-build` | After flake changes that affect an already-installed system - builds `wahrwelt` and the supported `mysetup` compatibility alias from an `/etc/nixos`-style temporary mirror. |
| `make all` (run from `Linux/`) | Aggregate: delegates to installer Makefile + `statix` + `deadnix` + json-lint. |

The scheduled flake updater also enforces the pC dependency boundary: the
`end4-pc` input must exist in the root/full, `desktop`, `developer`, and
`personal` locks, must be absent from `minimal`, and the generated
`xdg.configFile."quickshell/end4-pC".source` and the managed QuickShell
transition artifact must build before an update PR can merge.

## Configuration Structure

```text
Linux/NixOS/
├── flake.nix
├── flake.lock
├── presets/                       # minimal / desktop / developer / personal
│   └── <preset>/                  # public flake.nix + isolated flake.lock
├── modules/
│   ├── mysetup-options.nix        # `wahrwelt.*` + supported compatibility aliases
│   └── mysetup-stack.nix          # reusable workstation stack
├── hosts/NixOS/
│   ├── host-vars.nix
│   ├── secrets/                   # optional sops-nix system secrets
│   └── hardware-configuration.nix # host-local, preserved from /etc/nixos
├── lib/                           # flake glue: layout, hosts, packages,
│   │                              # overlays, modules, presets, ports,
│   │                              # wahrweltLib helpers
│   └── package-sets/              # per-preset package set definitions
├── profiles/                      # base / desktop / developer / features
│                                  # import layers (composed in hosts/NixOS)
├── home/                          # home-manager root + shell profiles
│   ├── home.nix
│   ├── theming.nix
│   ├── avatar.jpg
│   ├── lib/                       # dotfile sync + shell selector helpers
│   ├── caelestia/                 # caelestia-shell profile
│   ├── noctalia/                  # noctalia profile
│   ├── end4/                      # shared End4 Official/pC profile
│   ├── programs/                  # btop, cava, fastfetch, fish, foot, git,
│   │                              # packages (preset-gated home pkgs),
│   │                              # starship, thunar, vesktop, uwsm, …
│   └── shells/
│       └── quickshell/
│           ├── wahrwelt-shell-selector/    # Super+Shift+W picker
│           └── wahrwelt-shell-transition/  # Managed honeycomb overlay
├── pkgs/                          # pure derivations
│                                  # (omnirouter, sddm-meowrch-theme)
├── programs/                      # system-wide program modules
│                                  # (dev-tools, fish, hyprland, thunar, …)
├── services/                      # databases, observability, sddm,
│                                  # virtualization, omnirouter, portainer, …
├── system/                        # fonts, hardware, kernel, locale,
│   │                              # networking, nvidia, packages, settings
│   └── boot/                      # grub, plymouth, secure boot
├── themes/                        # active theme switch + grub/plymouth/sddm
├── users/                         # user + android-sdk modules
└── Wallpapers/

Linux/dots/
├── hypr/
│   ├── hyprland.lua
│   ├── variables.lua
│   ├── caelestia/
│   ├── noctalia/
│   ├── end4/
│   ├── hyprland/                  # shared Hyprland Lua modules
│   ├── lib/                       # Lua helpers for paths and bind wrappers
│   ├── scheme/                    # color scheme outputs
│   ├── scripts/                   # bash + fish helpers (sourced by Hypr)
│   ├── shell-common-keybinds.lua
│   └── shell-workspace-keybinds.lua
├── nvim/                          # LazyVim-based Neovim config
└── zen/
    └── chrome/                    # Zen Browser Catppuccin chrome

Linux/installer/                   # Go TUI/CLI (`wahrwelt` plus supported `mysetup` alias)
├── cmd/wahrwelt/                  # primary binary entrypoint
├── internal/                      # app, apply, cleanup, config, defaults,
│                                  # doctor, dots, paths, rollback, run,
│                                  # secrets, shellruntime, tui, zenutil
├── scripts/                       # nix-eval helpers used by Makefile checks
├── Makefile                       # local CI: lint, fmt, test, nix checks
├── go.mod
└── go.sum

Linux/Makefile                     # aggregate make targets for the Linux tree
                                   # (delegates to installer/Makefile + nix)
```

## Layer Model: NixOS vs Home-Manager

Many programs (Thunar, fish, Hyprland, …) intentionally appear in **two**
files. They are not duplicates - they configure two different layers:

| Layer | Scope | Owns | Example |
| --- | --- | --- | --- |
| NixOS module (`programs/<x>.nix`, `services/<x>.nix`, `system/<x>.nix`) | system-wide, applied by root via `nixos-rebuild` | package install in `/etc`, polkit/dbus/systemd, mime database, kernel/udev | `programs/thunar.nix`: `programs.thunar.enable`, `environment.systemPackages = [ tumbler ffmpegthumbnailer ]` |
| Home-Manager module (`home/programs/<x>.nix`) | per-user, applied by your user via `home-manager activate` | `~/.config` dotfiles, xfconf/gsettings, GTK theme, user-only `home.activation` hooks | `home/programs/thunar.nix`: `xfconf.settings.thunar.*`, `xdg.configFile."Thunar/uca.xml"`, restart of user thumbnail daemons |

These two scopes run in **different processes with different privileges** and
cannot be merged into one file. The same split exists in every serious NixOS
configuration. If a program only has one file, it is because it does not need
the other half:

- **NixOS-only** (no home-manager half): `programs/gaming.nix`,
  `programs/xdg-portal.nix`, `programs/system-tools.nix`,
  `programs/development.nix`. These provide system features that have no
  per-user dotfile surface (gamemode, steam, portals, build tooling).
- **Home-Manager-only** (no NixOS half): `home/programs/btop.nix`,
  `home/programs/starship.nix`, `home/programs/cava.nix`,
  `home/programs/fastfetch.nix`. These are user-scope CLI tools with config
  files in `~/.config/`; no system service or polkit/dbus rule is required.
- **Asymmetric pair**: `programs/fish.nix` (system-wide shell enable +
  `users.defaultUserShell`) plus `home/programs/fish.nix` (aliases,
  abbreviations, functions, integrations like `direnv` / `zoxide`).

Preset gating (`wahrweltLib.mkIfPresetOrMore "desktop" config.wahrwelt`) decides
**whether** a NixOS module is active per host preset
(`minimal -> desktop -> developer -> personal`). Home-manager packages use the
same preset helpers via `home/programs/packages.nix`.

Compositional note: there is no separate `profiles/personal.nix` file. The
`personal` preset is just `developer` + `desktop` + everything gated on
`presets.personal` (e.g. games in `home/programs/packages.nix`).
`modules/mysetup-stack.nix` imports
`profiles/{base,desktop,developer,features}.nix`, and each module checks
`wahrwelt.packages.preset` at evaluation time.

## Recovery

The installer creates a unique `/etc/nixos.bak.<timestamp>.<pid>.<n>` backup
before replacing `/etc/nixos`. A root-owned marker authenticates backups made
by current releases, and a successful activated apply keeps the newest three
authenticated backups. Historical unmarked backups and the displaced
`/etc/.nixos.migration.<suffix>` tree are never adopted or deleted
automatically. Review and remove those old copies manually only after the
updated system and the next `nixos-update` both succeed.

To recover manually, first retain the current tree under a new, verified path.
Then pick the most recent backup and roll it forward. Replace the timestamp in
`CURRENT_RECOVERY` with a unique value and do not continue if that path exists:

```bash
ls -dt /etc/nixos.bak.* | head -1                # most recent backup
CURRENT_RECOVERY=/etc/nixos.before-manual-recovery.YYYYMMDD-HHMMSS
sudo test ! -e "$CURRENT_RECOVERY"
sudo cp -a --reflink=auto -- /etc/nixos "$CURRENT_RECOVERY"
sudo test -f "$CURRENT_RECOVERY/flake.nix"
sudo rsync -a --delete --delete-excluded --exclude=/.wahrwelt-backup-v1 /etc/nixos.bak.<timestamp>.<pid>.<n>/ /etc/nixos/
sudo nixos-rebuild switch --flake /etc/nixos#NixOS
```

Keep `CURRENT_RECOVERY` until the rebuilt system and the next update both
succeed. It contains files that were newer than the selected backup and would
otherwise be removed by `rsync --delete`.

Run Doctor for checks and recovery hints:

```bash
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- doctor
```

For shell-switch issues, inspect:

```bash
test -n "${XDG_RUNTIME_DIR:-}" && cat "$XDG_RUNTIME_DIR/wahrwelt-shell.log"
```

There is intentionally no `/tmp` fallback. If `XDG_RUNTIME_DIR` is unset or
unsafe, the shell runtime fails closed and preserves the colliding path.

## Troubleshooting

Quick triage for the most common breakage paths. If none of these apply,
run `wahrwelt doctor` and check the relevant log.

- **Shell-swap (Super+Shift+W) does nothing or freezes.** Check
  `$XDG_STATE_HOME/wahrwelt/active-shell` (should be one of `caelestia`,
  `noctalia`, `end4`, `end4-pc`). Tail
  `$XDG_RUNTIME_DIR/wahrwelt-shell.log` while you press the binding. If the
  log reports a stale lock or ownership collision, run `wahrwelt doctor`,
  preserve any reported recovery path, and re-apply the selected profile.
  Do not remove the whole `hypr-runtime/` directory.
- **Hyprland keybinds are listed but do nothing.** Run `hyprctl configerrors`
  first. On Hyprland 0.55+, dispatch commands must use the Lua dispatcher API;
  Wahrwelt binds should go through `lib/wahrwelt.lua` helpers or direct
  `hl.dsp.*` calls, not old `hyprctl dispatch movewindow l` style strings.
- **Thunar shows generic icons instead of image previews.** Check that
  `tumbler` is running (`pgrep -f tumbler-1/tumblerd`), that
  `~/.cache/thumbnails/normal/` is writable, and that `XDG_DATA_DIRS`
  contains `/run/current-system/sw/share` and `~/.nix-profile/share`. If end4
  is active, this is most likely upstream `env = XDG_DATA_DIRS,...` leaking
  through - re-apply and re-login.
- **`apply --no-switch` fails at dry-build.** Read the Nix error tail; the
  installer aborts before touching `/etc/nixos`. Re-run
  `make -C Linux/installer nix-hm-eval` to localise the failure to a single
  module before retrying apply.
- **Build is huge / slow on first apply.** Expected: the config pulls in
  multiple Wayland shells, Qt/QML, desktop apps, optional CTF stacks. Make
  sure the listed binary caches are accepted (see Install / Reconfigure) -
  without them everything compiles from source.
- **First `switch` fails after writing `/etc/nixos` with dbus/systemd activation
  errors.** Reboot into the new generation boundary, then run
  `sudo nixos-rebuild switch --flake /etc/nixos#NixOS`. The dry-build already
  passed; this usually means the live system could not restart part of the
  desktop/session stack cleanly.
- **`sudo nixos-rebuild switch` works locally but `wahrwelt apply` fails.**
  The installer wraps switch with stricter checks (dry-build, password
  hash, dots mirror). Run `nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" --
  doctor` to see which precondition is missing.

## Maintenance

System update:

```bash
cd /etc/nixos
sudo nix flake update
sudo nixos-rebuild switch --flake .#NixOS
```

Default thin installs use an independent host lock: `/etc/nixos/flake.lock`
owns nixpkgs, Home Manager, Stylix, Quickshell, shell flakes, and the selected
Wahrwelt source revision. The `stable` channel uses the `main` branch; the
`development` channel uses `dev`. Managed thin installs update only the
`wahrwelt` input (plus host-owned Lanzaboote when Secure Boot is enabled) and
reuse the selected preset's transitive lock shipped by Wahrwelt.

Existing recognized installer-generated wrappers are canonically regenerated
on the next `wahrwelt apply`, preserving the selected channel and lock mode
while switching to the matching preset entrypoint. A plain `nixos-update`
continues to update `/etc/nixos/flake.lock` and rebuild normally. Changing the
preset or toggling Secure Boot requires one `wahrwelt apply` so the wrapper
structure can be regenerated; unrecognized user-owned flakes are preserved.

Garbage collection:

```bash
sudo nix-collect-garbage -d
sudo nixos-rebuild boot --flake /etc/nixos#NixOS
```
