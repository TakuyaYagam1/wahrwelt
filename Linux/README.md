# Wahrwelt · Linux

NixOS with Hyprland, interchangeable desktop shells, and a Go TUI installer.
Choose a package preset, keep your own settings in User space, and update the
shared configuration with `nixos-update`.

[Install](#install) · [Presets](#presets) · [User space](#user-space) ·
[NixOS & Home Manager](#nixos-modules-vs-home-manager) ·
[Shells](#runtime-shells) · [Maintenance](#maintenance)

<img src="../assets/preview.png" alt="Wahrwelt Hyprland desktop preview" width="900">

[Desktop gallery](../README.md#screenshots) · [Keybinds](keybinds.md) ·
[Video tour](https://youtu.be/pA6Xnz_d-KU)

## Find your way

| I want to… | Go to |
| --- | --- |
| Install or change my preset | [Install](#install) and [Presets](#presets) |
| Add an alias, plugin, or keybind | [User space](#user-space) |
| Add a system service or a user package | [NixOS modules vs Home Manager](#nixos-modules-vs-home-manager) |
| Switch Caelestia / Noctalia / End4 | [Runtime shells](#runtime-shells) |
| Update, reclaim space, or recover | [Maintenance](#maintenance) |
| Change Wahrwelt itself | [CLI & contributor reference](docs/development.md) |

## Install

Start from an **already booted NixOS system** with
`/etc/nixos/hardware-configuration.nix`. This installer configures that system;
it does not perform a fresh installation into `/mnt`.

Open the stable installer directly from GitHub:

```bash
nix run --refresh 'github:TakuyaYagam1/wahrwelt'
```

The TUI lets you choose the account, package preset, channel, display, keyboard,
locale, GPU, Secure Boot and optional features. Review the choices, then Apply.
On first adoption, the existing `/etc/nixos` tree is backed up before replacement.

<details>
<summary>Local checkout or development channel</summary>

From the repository root:

```bash
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt"
```

To run the development version:

```bash
nix run --refresh 'github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS#wahrwelt' -- tui
```

Select **General -> Wahrwelt channel -> development** before applying to keep
future updates on `dev`. Launching the development TUI alone does not change
the saved channel. `stable` tracks `main`.

</details>

A first build can be large. On a low-memory host, limit the installer build too:

```bash
nix run --refresh --option max-jobs 1 --option cores 2 \
  'github:TakuyaYagam1/wahrwelt'
```

[Installation reference](docs/install.md) covers binary caches, build limits,
passwords, thin/full layouts, lock modes and migration from older installations.

## Presets

Presets decide **how much is installed**. Each tier includes the tiers before it:

```text
minimal -> desktop -> developer -> personal
```

| Preset | What you boot into | What it adds |
| --- | --- | --- |
| `minimal` | Text console | Base system and CLI tools. No SDDM or Hyprland session. |
| `desktop` | Hyprland desktop | SDDM, all four shell profiles, browser, chat, office and media apps. |
| `developer` | Same desktop | Development/API tools, rootless Docker, Compose, Buildx, lazydocker and Claude/Codex tools. |
| `personal` | Same desktop | Extra workstation apps, IDEs, AI tools and games. The largest build. |

For the desktop shown above, start with `desktop`. Choose `developer` if you
also want the development stack. The current default is `personal`, so check
the preset in the TUI instead of accepting it by habit.

Change presets through the installer: the choice affects both host settings
and the preset flake selected by `/etc/nixos/flake.nix`. Editing only
`packages.preset` can leave those two out of sync.

[Package breakdown](../README.md#package-presets) ·
[Preset entrypoints and locks](docs/development.md#preset-entrypoints-and-locks)

## User space

Put personal changes in the writable user layer. The repository supplies the
defaults; your settings load after them. Paths below assume the usual
`XDG_CONFIG_HOME=~/.config`.

| Application | Edit here | How changes take effect |
| --- | --- | --- |
| Fish | `~/.config/fish/user/config.fish` | Open a new terminal. Loads after the generated aliases and settings. |
| Fish functions / completions | `~/.config/fish/user/functions/`, `user/completions/` | User directories take priority in lookup paths. |
| Neovim / LazyVim | `~/.config/nvim/lua/user/` | Restart Neovim. Options, keymaps, autocmds and plugin specs follow the base configuration. |
| Hyprland | `~/.config/hypr/user/default.lua` and modules it loads | Run `hyprctl reload`. |

For example, add an alias in `~/.config/fish/user/config.fish`:

```fish
alias ll 'eza -lah --icons'
```

Or set your indentation in `~/.config/nvim/lua/user/options.lua`:

```lua
vim.opt.expandtab = true
vim.opt.shiftwidth = 4
vim.opt.tabstop = 4
```

Existing user files are preserved during updates. Missing Fish/Neovim user
templates are recreated. A new repository alias arrives through the base
Fish configuration; a personal alias with the same name still takes priority.

Files linked into `/nix/store` are generated defaults. Replacing such a link
with an edited file is detected before Home Manager overwrites it. For copied
Neovim defaults, known base versions allow a three-way merge; conflicting edits
stop that batch. Older differing files with no known base remain unchanged and
are reported as unmanaged.

### Customizing Hyprland without forking the repo

`~/.config/hypr/user/` is writable, except for the managed `hyprland.lua`
entrypoint. Start with `default.lua`; arbitrary files are loaded only when it
requests them. The physical directory is `user/`, while the Lua namespace remains
`wahrwelt.*`.

[User space recipes](docs/user-space.md) cover functions, plugins, Hyprland
bindings, load order and resolving conflicts without losing personal settings.

## NixOS modules vs Home Manager

A **preset** selects the feature set. A **module** configures part of it.
Both NixOS and Home Manager use Nix modules, but their options have different
scopes:

| Scope | Typical changes | Local entrypoint | Repository defaults |
| --- | --- | --- | --- |
| NixOS | Hardware, services, networking, system packages | `/etc/nixos/configuration.nix` | [`NixOS/system/`](NixOS/system/), [`services/`](NixOS/services/), [`programs/`](NixOS/programs/) |
| Home Manager | User packages, aliases, dotfiles, desktop preferences | `/etc/nixos/home.nix` | [`NixOS/home/`](NixOS/home/) |
| Application user files | Personal Fish, Neovim and Hyprland overrides | The User space paths above | Seed templates and loading hooks |

For example, Fish has a system module that enables the login shell and a
Home Manager module that defines its aliases and functions. These files serve
different purposes.

Home Manager is integrated into this NixOS configuration. **One NixOS rebuild
activates both layers**; you do not need a separate Home Manager command.
Application user files usually need only a reload or a new application process.

### Organizing local modules

The installed tree keeps local overrides alongside the small flake wrapper:

```text
/etc/nixos/
├── flake.nix                 # selected Wahrwelt preset and local imports
├── flake.lock                # pinned inputs
├── host-vars.nix             # saved host choices
├── hardware-configuration.nix
├── configuration.nix         # NixOS overrides; imports ./user
├── home.nix                  # Home Manager overrides
├── user/default.nix          # explicit local NixOS module imports
└── installer-state.json      # saved after successful activation
```

`/etc/nixos/user/` is a place to organize files, not an automatic directory scan.
For a local system module, add its import to `user/default.nix`:

```nix
{ ... }:
{
  imports = [ ./custom-system.nix ];
}
```

A Home Manager module belongs in the imports of `/etc/nixos/home.nix`, even
if you store the file in that same directory:

```nix
{ ... }:
{
  imports = [ ./user/custom-home.nix ];
}
```

Merge these entries into the existing files, keeping their other imports and
settings. Do not import a Home Manager module through `user/default.nix`.
Apply local Nix edits with `nixy rebuild --impure`; use the TUI when changing
saved installer choices such as preset or channel.

[Module examples and source map](docs/development.md#layer-model-nixos-vs-home-manager)

## Runtime shells

Press **Super+Shift+W** to open the shell selector on the focused monitor.
Switching shells does not require a rebuild or reinstall.

| Choice | Profile ID | Notes |
| --- | --- | --- |
| Caelestia | `caelestia` | Default profile |
| Noctalia | `noctalia` | Alternative desktop shell |
| End4 Official | `end4` | QuickShell config `ii` |
| End4 pC | `end4-pc` | QuickShell config `end4-pC`; shares End4 user settings |

[Keybind reference](keybinds.md) ·
[Manual switching, logs and runtime details](docs/desktop.md#runtime-shells) ·
[Containers and Portainer](docs/desktop.md#containers)

## Boot theme

Place a shared `logo.png` or `logo.jpg` in `~/.config/wahrwelt/boot-theme/`.
Per-service `grub.*`, `sddm.*` and `plymouth.*` images take priority over it.
Keep a shared fallback if you only override some services.

Apply with `nixy rebuild --impure`. SDDM changes appear at the next login
screen; GRUB and Plymouth need a reboot.
[Formats, priorities and reset behavior](docs/desktop.md#boot-theme).

## Maintenance

| Task | Command | Effect |
| --- | --- | --- |
| Update the installed system | `nixos-update` | Fetches the selected channel, builds and switches. |
| Preview an update | `nixos-update --dry-run` | Shows proposed wrapper changes. |
| Build without installing | `nixos-update --no-switch` | Builds the candidate without publishing or activating it. |
| Apply local Nix edits | `nixy rebuild --impure` | Rebuilds from the installed configuration. |
| Routine cleanup | `cleanup` | Removes generations older than the retention window; preserves available build cache. |
| Full cleanup | `clean-full` | Removes old generations and unused build cache; later builds may need it again. |
| Inspect the host | `wahrwelt doctor` | Checks the installation and reports recovery hints. |

`cleanup` defaults to a 14-day retention window. `clean-full` can mean more
rebuilding next time; current generations and other GC roots remain protected.
These Fish commands call `nixy gc` / `nixy gc-full`, which are separate from the
installer's `wahrwelt cleanup` command.

`nixos-update` uses the saved settings in `/etc/nixos/installer-state.json`.
It follows the channel selected by the installed wrapper: `stable` uses `main`,
`development` uses `dev`. Change installer choices through **TUI -> Apply**.
After updating Fish defaults, open a new terminal.

### When something goes wrong

| Symptom | First check |
| --- | --- |
| Text console instead of a desktop | Confirm the preset: `minimal` intentionally has no graphical login. |
| Missing new Fish aliases | Open a new shell, then check the selected channel. |
| Config conflict or unmanaged Neovim file | Read the reported path and the [User space recovery steps](docs/user-space.md#existing-installations-and-conflicts). |
| Shell selector stalls | Inspect `$XDG_RUNTIME_DIR/wahrwelt-shell.log` and run `wahrwelt doctor`. |
| Hyprland keybind does nothing | Run `hyprctl configerrors`; use the Lua dispatcher API. |
| Rebuild fails | Read the actual error tail, then use the [troubleshooting guide](docs/maintenance.md#troubleshooting). |

Do not delete a reported backup or recovery journal while investigating.
Host backups use `/etc/nixos.bak.<timestamp>.<pid>.<n>`; legacy migration can
also retain `/etc/.nixos.migration.<suffix>`. Restoring the NixOS configuration
does not automatically restore every file in your home directory.

[Update and recovery reference](docs/maintenance.md) ·
[Older MySetup installations](docs/install.md#supported-mysetup-compatibility) ·
[Contributor checks](docs/development.md#commands)
