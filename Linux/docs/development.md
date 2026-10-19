# CLI & contributor reference

[← Linux guide](../README.md)

Commands, source layout and validation targets for changing Wahrwelt itself.
For local host overrides, see [NixOS modules vs Home Manager](../README.md#nixos-modules-vs-home-manager).

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

# Update the installed system using its saved settings.
nix run --refresh 'github:TakuyaYagam1/wahrwelt?dir=Linux/NixOS#wahrwelt' -- update

# Preview wrapper changes, or build the update without installing it.
nix run --refresh 'github:TakuyaYagam1/wahrwelt?dir=Linux/NixOS#wahrwelt' -- update --dry-run
nix run --refresh 'github:TakuyaYagam1/wahrwelt?dir=Linux/NixOS#wahrwelt' -- update --no-switch

# Validate saved configuration without publishing it or opening the TUI.
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --no-switch
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --source-channel development --no-switch
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --lock-mode managed --no-switch
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --layout full --no-switch

# Installer cleanup (separate from the Fish cleanup / nixy gc commands).
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- cleanup
```

`apply --no-switch` is a validation mode: it stages the build and stops after
`nixos-rebuild dry-build`, before writing `/etc/nixos`, user dotfiles,
activation, or activated state.

Use `apply --dry-run` to preview filesystem actions. The installer logs
commands but does not execute external command actions through the shared runner,
and it avoids host-local side effects such as password hashing or writing
`/etc/nixos`:

```bash
nix run "path:$PWD?dir=Linux/NixOS#wahrwelt" -- apply --dry-run --no-switch
```

Building the installer from source requires Go 1.27.2 or newer. CI reads the
version from `Linux/installer/go.mod`; the Nix package uses `buildGoLatestModule`
so it does not fall back to nixpkgs' older default Go. No `GOEXPERIMENT` flags
are required. After updating Go dependencies, update the installer's
`vendorHash` in `NixOS/lib/flake-packages.nix` and verify the Nix build too.

Useful checks after changing installer or shell integration:

| Make target | Run it when |
| --- | --- |
| `make -C Linux/installer check` | Before pushing or applying - full local CI: lint, fmt-check, hypr-bind-check, shell-check, tests, nix evals. |
| `make -C Linux/installer shell-check` | After editing Hypr scripts, JSON, or Python patch sources under `Linux/dots/hypr/`. |
| `make -C Linux test-dotfile-updates` | After changing Fish/Neovim user layers, reconciliation, or activation guards. |
| `make -C Linux test-hypr-integration` | After changing shell lifecycle, overlay behavior, Lua adapters, shared rules, app-aware close behavior, or selector/transition models. |
| `make -C Linux nix-shell-transition-build` | Realize the Home Manager-owned transition artifact and verify QML, the compiled shader, duration, license, and lock-free contract. |
| `make -C Linux/installer nix-hm-eval` | After touching `home/`, End4 runtime-env, or shell-profile imports - evaluates the runtime shell module and all-on Home Manager imports including End4 Official and pC. |
| `make -C Linux nix-end4-hypr-build` | Realize the exact Home Manager-owned validated End4 Hypr artifact and inspect lifecycle/rules contracts. |
| `make -C Linux nix-end4-pc-quickshell-build` | Realize the managed End4 pC QuickShell tree and reject any direct QuickShell lifecycle launch. |
| `make -C Linux/installer nix-installed-mirror-build` | After flake changes that affect an already-installed system - builds `wahrwelt` and the supported `mysetup` compatibility alias from an `/etc/nixos`-style temporary mirror. |
| `make all` (run from `Linux/`) | Aggregate Linux linting and tests, including Go, Nix, shell, Lua, JSON, Python and runtime checks. |

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

## Layer model: NixOS vs Home Manager

Many programs (Thunar, fish, Hyprland, …) intentionally appear in **two**
files. They are not duplicates - they configure two different layers:

| Layer | Scope | Owns | Example |
| --- | --- | --- | --- |
| NixOS module (`programs/<x>.nix`, `services/<x>.nix`, `system/<x>.nix`) | system-wide, applied by root via `nixos-rebuild` | system packages, `/etc` configuration, polkit/dbus/systemd, kernel/udev | `programs/thunar.nix`: `programs.thunar.enable`, `environment.systemPackages = [ tumbler ffmpegthumbnailer ]` |
| Home-Manager module (`home/programs/<x>.nix`) | per-user, activated as part of the NixOS rebuild | `~/.config` dotfiles, xfconf/gsettings, GTK theme, user-only `home.activation` hooks | `home/programs/thunar.nix`: `xfconf.settings.thunar.*`, `xdg.configFile."Thunar/uca.xml"`, restart of user thumbnail daemons |

NixOS and Home Manager use separate module scopes. This repository wires
Home Manager into NixOS, so the same system rebuild activates both. Place an
option in the module scope that defines it. A program may need one or both:

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

### Add a local module

For a system-wide package, create `/etc/nixos/user/custom-system.nix`:

```nix
{ pkgs, ... }:
{
  environment.systemPackages = [ pkgs.hello ];
}
```

Add `./custom-system.nix` to the existing `imports` list in
`/etc/nixos/user/default.nix`. That entrypoint is imported by
`configuration.nix`, so it uses NixOS options.

For a package only in your user environment, create
`/etc/nixos/user/custom-home.nix` instead:

```nix
{ pkgs, ... }:
{
  home.packages = [ pkgs.hello ];
}
```

Add `./user/custom-home.nix` to the existing `imports` list in
`/etc/nixos/home.nix`. Do not also import this file through `user/default.nix`:
`home.packages` belongs to Home Manager's module scope.

Choose the scope you need, keep existing imports, and apply with
`nixy rebuild --impure`. Local modules remain in the installed host tree
across updates.

## Preset entrypoints and locks

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
