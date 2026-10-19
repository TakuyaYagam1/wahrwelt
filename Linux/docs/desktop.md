# Desktop & runtime reference

[← Linux guide](../README.md)

Shell switching, Hyprland load order, boot logos and desktop services.
For personal Fish, Neovim or Hyprland edits, use [User space](user-space.md).

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
[`Linux/keybinds.md`](../keybinds.md).

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

### Personal Hyprland modules

Use `~/.config/hypr/user/default.lua` to load your own modules. The
[User space guide](user-space.md#hyprland) covers module names, binding
overrides and reloads.

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

## Containers

The developer and personal presets use rootless Docker by default. Portainer
and lazydocker connect to the same per-user engine socket by default. Fish
leaves `docker` as the real Docker command, and `dc` expands to `docker compose`.
An explicit `DOCKER_HOST` continues to select a custom endpoint for CLI tools.
To select rootless Podman, set `wahrwelt.containers.engine = "podman"` in the
host configuration. This does not remove or copy existing data: images,
containers, and volumes remain in the previous engine's storage. If Portainer
was previously run by the rootful system service, its `/var/lib/portainer` data
is left untouched; the new per-user service starts with separate state and
requires a fresh first-visit admin setup. Portainer with rootless Podman is
best-effort and is not officially supported by Portainer.

Rootless Docker has practical limits. Publishing ports below 1024 needs
additional host configuration, and resource limits require cgroup v2 with
systemd. See the [Docker rootless mode documentation](https://docs.docker.com/engine/security/rootless/)
and its [known limitations](https://docs.docker.com/engine/security/rootless/troubleshoot/#known-limitations).

After applying an engine change, log out and back in once so Fish drops an old
`docker` alias and the session refreshes `DOCKER_HOST`. Check the default
developer/personal setup with:

```bash
systemctl --user status docker.service
echo "$DOCKER_HOST"
type -a docker
docker info --format '{{json .SecurityOptions}}'
docker compose version
docker buildx version
```

If Portainer is enabled, also check `systemctl --user status portainer.service`
and visit `https://127.0.0.1:9443` for its fresh first-run admin setup. In a
project containing a Compose file, run `docker compose up -d` and then
`docker compose ps` to verify the engine can start a project.

Steam does not open Remote Play, dedicated-server, or local-transfer firewall
ports automatically. Opt in to only the ports required by the current host.

## Codex Desktop settings

The `developer` and `personal` presets install the unofficial
[`ilysenko/codex-desktop-linux`](https://github.com/ilysenko/codex-desktop-linux)
Nix package and point its launcher at the Codex CLI from this configuration.
The app still owns its runtime settings under `~/.config/codex-desktop`; that
directory is deliberately not declared through Home Manager
`xdg.configFile`, so its files stay writable and survive NixOS rebuilds without
becoming read-only `/nix/store` symlinks. The separate `~/.codex` CLI
configuration is also left untouched.
