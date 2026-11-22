# NixOS and Home Manager ownership

The Wahrwelt NixOS flake owns the reproducible system and Home Manager layer.
The main installation, shell switching, and validation guide is
[`Linux/README.md`](../README.md). The binding reference is
[`Linux/keybinds.md`](../keybinds.md).

Home Manager owns the stable `~/.config/hypr` entrypoints, canonical Lua
modules, executable shell scripts, top-level shared rules, selector assets,
transition assets, `end4-adapter.lua`, and one shared validated `hypr/end4`
tree. Managed paths are store links and should be changed in this repository.
During an update of an established graphical preset, the installer does not
mirror over those live links before `nixos-rebuild switch`; Home Manager
replaces the generation as one activation boundary.

The runtime owns writable profile files under
`$XDG_STATE_HOME/wahrwelt/hypr-runtime` and the active profile state under
`$XDG_STATE_HOME/wahrwelt`. Home Manager seeds missing runtime files but does
not preseed `active-shell` or replace valid user state. `start-shell.sh` writes
the active profile only after runtime preparation and shell startup succeed.

Host-local NixOS modules belong in `/etc/nixos/user/`. Fresh installs import
`./user` and create `/etc/nixos/user/default.nix` with mode 0644 only when the
path is absent. The installer state is the adjacent
`/etc/nixos/installer-state.json`, not a one-file `wahrwelt/` directory.
Existing installs migrate `/etc/nixos/private/` to `/etc/nixos/user/` only
when the target is absent. They read `/etc/nixos/wahrwelt/state.json` or
`/etc/nixos/mysetup/state.json` as legacy input and remove those exact files
only after the canonical state is written successfully. An exact empty legacy
parent is retained under an identity-proven hidden quarantine name; nonempty or
concurrently replaced parents are not removed. Unsupported nodes or coexisting
old and new directories stop the operation without merging them.
The first ordinary `nixos-update` activates this migration automatically. It
uses the versioned `system/migrations/v1_to_v2` recognizer, rewrites only an
exact installer-generated v1 wrapper and its root lock keys, and preserves the
locked revision and `narHash`. It builds the candidate offline, then uses a
pinned same-filesystem atomic exchange and retains the displaced tree beside
`/etc/nixos` for manual recovery. Arbitrary user modules and the supported
`mysetup` compatibility API are not rewritten.
The service is scheduled only when an exact v1 path exists: `private/`, one of
the two historical state files, or a generated legacy password module. A fresh
canonical tree does not load the v1 recognizers. Successful migration records
the one-shot root-owned `v1_to_v2.complete` marker under
`/var/lib/wahrwelt/migration`; remaining v1 evidence after that marker is an
ownership collision. A real `./private` path in any top-level module other
than the installer-owned `configuration.nix` is preserved and must be updated
manually before retrying.

The Linux password hash lives at root-owned
`/etc/wahrwelt/hashed-password`, outside the flake source. The installed tree
contains only `.wahrwelt-password-hash-enabled`, which has no secret content.
Known v1 `hashed-password.nix` modules are migration inputs only; unknown files
or symlinks are ownership collisions.

`~/.config/hypr/user/default.lua` is user-owned. Activation creates the
exact default aggregator only when no filesystem node exists, preserving
regular files, symlinks, broken symlinks, and arbitrary user modules. The
managed `~/.config/hypr/user/hyprland.lua` entrypoint is the exception
inside that otherwise writable directory. The physical directory is `user/`,
while user Lua modules keep the internal `wahrwelt.*` namespace.

End4 Official (`end4`) and End4 pC (`end4-pc`) share the same Home
Manager-owned Hypr tree. The top-level adapter supplies the exact profile and
QuickShell path while preserving canonical input, gestures, rules, and runtime
ordering. End4 does not install another top-level shared-rules hook.

Explicit runtime shell choices use a destination-aware honeycomb timeline.
Caelestia and Noctalia use `3 + 3 + 3` seconds; End4 Official and End4 pC use
`3 + 5 + 3` seconds. The old shell is not stopped until every output reports
that two opaque cover frames have been swapped, and only then does the full
three- or five-tick handoff interval begin.

Activation migrates one legacy `~/.config/hypr/mysetup/` or
`~/.config/hypr/wahrwelt/` user tree to `~/.config/hypr/user/`. It replaces
only known generated runtime entrypoints. Unknown files and link targets are
preserved, and ownership collisions fail closed.

Useful checks from the repository root:

```bash
make -C Linux test-hypr-integration
make -C Linux/installer nix-hm-eval
make -C Linux nix-shell-transition-build
make -C Linux nix-end4-hypr-build
make -C Linux nix-end4-pc-quickshell-build
nix build --no-link --no-write-lock-file ./Linux/NixOS/presets/personal#noctalia
nix eval --raw --no-write-lock-file "path:$PWD?dir=Linux/NixOS/presets/minimal#checks.x86_64-linux.preset-host.drvPath"
```
