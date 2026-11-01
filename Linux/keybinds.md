# Keybinds

Hyprland-based NixOS configuration with three swappable QuickShell-based shell
families. End4 has Official and pC variants, for four runtime profile IDs in
total. Use `Super+Shift+W` to switch between them at runtime; Caelestia is the
default.

The configuration is layered with explicit Lua `require` calls. There is no
legacy hyprlang source chain and no file auto-discovery. The canonical runtime loads
`hyprland.keybinds`, then the writable runtime `shell-keybinds.lua` adapter:

- `caelestia` -> `require("caelestia.keybinds")`
- `noctalia` -> `require("noctalia.keybinds")`
- `end4` and `end4-pc` ->
  `require("end4-adapter").load({ profile = ..., quickshell_config = ... })`

Each profile adapter explicitly requires the common modules it needs. The End4
adapter loads the shared patched End4 tree and then its Wahrwelt keybind overlay.
For each exact chord and submap, the first upstream End4 bind replaces any
earlier canonical bind. Additional upstream handlers on that same chord are
kept intentionally. Every Wahrwelt overlay bind uses `hl.unbind` before
installing its final replacement.

## How to read the tables

| Notation | Meaning |
| --- | --- |
| `Super` | Windows/Meta key |
| `Super+Shift, X` | hold Super+Shift, press X |
| `hl.bind` | normal Lua binding |
| `locked = true` | also fires when the session is locked |
| `repeating = true` | repeats while held, for example resize or workspace cycle |
| `release = true` | fires on release |
| `mouse = true` | mouse drag or resize binding |
| `v.kbX` | named value returned by `dots/hypr/variables.lua` |

Source files in the repository:

- `Linux/dots/hypr/variables.lua` - keybind values
- `Linux/dots/hypr/hyprland/keybinds.lua` - base Hyprland keybinds
- `Linux/dots/hypr/shell-common-keybinds.lua` - shared shell entrypoints
- `Linux/dots/hypr/shell-workspace-keybinds.lua` - workspace group navigation
- `Linux/dots/hypr/{caelestia,noctalia,end4}/keybinds.lua` - per-profile adapters
- `Linux/dots/hypr/{caelestia,noctalia,end4}/launcher.lua` - profile launchers

---

## Common (all shells)

These bindings are registered by all four runtime profiles (`caelestia`,
`noctalia`, `end4`, `end4-pc`). The base comes from
`hyprland/keybinds.lua`; profile modules require `shell-common-keybinds.lua`
and, where needed, `shell-workspace-keybinds.lua`.

User overrides belong in writable
`~/.config/hypr/user/default.lua`. Unbind an existing chord before
rebinding it so both actions do not fire:

```lua
hl.unbind("SUPER + Q")
hl.bind("SUPER + Q", hl.dsp.exec_cmd("notify-send custom-close"))
```

Save the file and apply it with:

```bash
hyprctl reload
```

### Shell selector

| Keys | Action |
| --- | --- |
| `Super+Shift+W` | Toggle the shell selector: Caelestia, Noctalia, or End4 with an Official/pC segmented choice |

An explicit choice uses a destination-aware honeycomb transition. Caelestia
and Noctalia use `3 + 3 + 3` seconds with three full handoff ticks. End4
Official and End4 pC use `3 + 5 + 3` seconds with five full handoff ticks. The
tick interval starts only after the opaque cover is confirmed on every output.
Login and Home Manager auto-start do not animate. An unconfirmed cover cancels
the switch before the old shell stops; a late or failed target restores the
previous shell.

### Application launchers

| Keys | Action |
| --- | --- |
| `Super+Return` | Terminal |
| `Super+Shift+F` | Zen Browser |
| `Super+Shift+C` | Cursor IDE |
| `Super+Shift+V` | VS Code |
| `Super+Shift+Z` | Zed |
| `Super+Shift+D` | DataGrip |
| `Super+Shift+A` | Antigravity IDE (`antigravity-ide`) |
| `Super+Shift+Q` | AmneziaVPN |
| `Super+Shift, v2rayN` | v2rayN |
| `Super+Shift+B` | Vesktop (Discord) |
| `Super+Shift+I` | Spotify (toggle) |
| `Super+Shift+T` | Telegram |
| `Ctrl+Shift+Escape` | btop |
| `Super+E` | File explorer |
| `Super+Alt+E` | File explorer (duplicate keybind) |
| `Ctrl+Alt+V` | pavucontrol (audio mixer) |

### Screenshots & screen recording

| Keys | Action |
| --- | --- |
| `Super+S` | Screenshot whole screen |
| `Super+Shift+S` | Screenshot region |
| `Super+Shift+Alt+S` | Screenshot + edit |
| `Super+C` | Color picker (`hyprpicker -a`) |
| `Super+R` | Toggle screen recording |

### Workspaces

| Keys | Action |
| --- | --- |
| `Super+1` … `Super+0` | Jump to workspace 1–10 |
| `Super+Shift+1` … `Super+Shift+0` | Move active window to workspace 1–10 |
| `Ctrl+Super+1` … `Ctrl+Super+0` | Jump to workspace group 1–10 |
| `Ctrl+Super+Alt+1` … `Ctrl+Super+Alt+0` | Move active window to workspace group 1–10 |
| `Ctrl+Super+Right` | Next workspace |
| `Ctrl+Super+Left` | Previous workspace |
| `Super+Tab` | Toggle special workspace |
| `Super+Page_Up` / `Super+Page_Down` | Previous / next workspace (repeats) |
| `Super+ScrollUp/Down` | Previous / next workspace |
| `Ctrl+Super+ScrollUp/Down` | Move ±10 workspaces |
| `Super+Alt+Page_Up/Down` | Move window ±1 workspace |
| `Super+Shift+ScrollUp/Down` | Move window ±1 workspace |
| `Ctrl+Super+Shift+Right/Left` | Move window ±1 workspace |
| `Ctrl+Super+Shift+Up` | Move window to special:special |
| `Ctrl+Super+Shift+Down` | Move window to current ws |
| `Super+Alt+S` | Move window to special:special |

### Special workspaces (toggled overlays)

| Keys | Action |
| --- | --- |
| `Ctrl+Shift+Alt+Escape` | System monitor overlay |
| `Super+M` | Music overlay |
| `Super+D` | Communication overlay |
| `Super+T` | Todo overlay |

### Window operations

| Keys | Action |
| --- | --- |
| `Super+Q` | App-aware close via `close-active.sh`: route Spotify to `special:music`, otherwise close the exact window address with a kill fallback |
| `Super+Space` | Toggle floating |
| `Super+P` | Pin window |
| `Ctrl+Shift+Return` | Fullscreen |
| `Super+Shift+Return` | Bordered fullscreen |
| `Super+Z` (held) | Move window with mouse |
| `Super+X` (held) | Resize window with mouse |
| `Super+LMB` (held) | Move window with mouse (mouse:272) |
| `Super+RMB` (held) | Resize window with mouse (mouse:273) |
| `Super+Alt+Backslash` | Picture-in-Picture |
| `Ctrl+Super+Backslash` | Center window |
| `Ctrl+Super+Alt+Backslash` | Resize to 55%×70% and center |
| `Super+Left/Right/Up/Down` | Focus window in direction |
| `Super+Shift+Left/Right/Up/Down` | Move window in direction |
| `Super+Minus` / `Super+Equal` | Resize horizontally ∓10% |
| `Super+Shift+Minus` / `Super+Shift+Equal` | Resize vertically ∓10% |
| `Super+Alt+Left/Right/Up/Down` | Resize active window |

### Window groups (tabbed)

| Keys | Action |
| --- | --- |
| `Super+Comma` | Toggle group |
| `Super+U` | Ungroup |
| `Alt+Tab` (held) | Cycle next in group |
| `Shift+Alt+Tab` (held) | Cycle previous in group |
| `Ctrl+Alt+Tab` (held) | Switch to next group member |
| `Ctrl+Shift+Alt+Tab` (held) | Switch to previous group member |
| `Super+Shift+Comma` | Lock active group |

### Audio & media (hardware keys)

| Keys | Action |
| --- | --- |
| `XF86AudioMute` | Toggle sink mute |
| `XF86AudioMicMute` | Toggle source mute |
| `Super+Shift+M` | Toggle sink mute |
| `XF86AudioRaiseVolume` (held) | Raise volume |
| `XF86AudioLowerVolume` (held) | Lower volume |

> Media play/pause/next/prev keybinds are **shell-specific** - see the
> per-shell sections below, because each shell wires its own IPC layer.

### System

| Keys | Action |
| --- | --- |
| `Super+Shift+L` | Suspend then hibernate |
| `Ctrl+Shift+Alt+V` | Paste last clipboard entry via ydotool (workaround) |

---

## Caelestia

Bindings specific to **caelestia-shell**, layered on top of Common.
Wired through caelestia's `global, caelestia:*` dispatchers and CLI helpers.

> Modules: `Linux/dots/hypr/caelestia/keybinds.lua`,
> `Linux/dots/hypr/caelestia/launcher.lua`.

### Launcher (Super release)

Release `Super` alone to toggle the launcher. Using Super with another key
or mouse action cancels the release binding in Hyprland.

The binding calls `caelestia shell drawers toggle launcher` directly, avoiding
an unconditional global-shortcut release callback after another shortcut.

### Shell panels

| Keys | Action |
| --- | --- |
| `Ctrl+Alt+Delete` | Session menu (`caelestia:session`) |
| `Super+N` | Show / hide sidebar (`caelestia:sidebar`) |
| `Ctrl+Alt+C` | Clear notifications (`caelestia:clearNotifs`) |
| `Super+K` | Show all panels (`caelestia:showall`) |
| `Super+L` | Lock screen (`caelestia:lock`) |
| `Super+Alt+L` | Restore lock state from last session |

### Brightness

| Keys | Action |
| --- | --- |
| `XF86MonBrightnessUp` | Brightness up |
| `XF86MonBrightnessDown` | Brightness down |

### Media

| Keys | Action |
| --- | --- |
| `Ctrl+Super+Space` | Play / pause (`caelestia:mediaToggle`) |
| `XF86AudioPlay` / `XF86AudioPause` | Play / pause |
| `Ctrl+Super+Equal` | Next track (`caelestia:mediaNext`) |
| `XF86AudioNext` | Next track |
| `Ctrl+Super+Minus` | Previous track (`caelestia:mediaPrev`) |
| `XF86AudioPrev` | Previous track |
| `XF86AudioStop` | Stop |

### Caelestia helpers

| Keys | Action |
| --- | --- |
| `Ctrl+Super+Shift+R` (release) | Kill caelestia shell |
| `Ctrl+Super+Alt+R` (release) | Restart caelestia shell |
| `Super+Alt+R` | Start screen recording (`caelestia record -s`) |
| `Ctrl+Alt+R` | Start screen recording (default) |
| `Super+Shift+Alt+R` | Caelestia record helper (`-r`) |

### Window-specific

| Keys | Action |
| --- | --- |
| `Super+Alt+Backslash` (from $kbWindowPip) | Resize active window to PiP |

### Clipboard & emoji

| Keys | Action |
| --- | --- |
| `Super+V` | Toggle caelestia clipboard (`pkill fuzzel` first) |
| `Super+Alt+V` | Caelestia clipboard with `-d` (delete entries) |
| `Super+Period` | Caelestia emoji picker (`-p`) |

---

## Noctalia

Bindings specific to **noctalia**, layered on top of Common.
Wired through `noctalia msg <command>`.

> Modules: `Linux/dots/hypr/noctalia/keybinds.lua`,
> `Linux/dots/hypr/noctalia/launcher.lua`,
> `Linux/dots/hypr/scripts/noctalia-msg.sh`.

### Launcher (Super release)

Release `Super` alone to toggle the launcher. Hyprland cancels the binding
when Super is used with another key or mouse action.

The binding calls `noctalia-msg.sh launcher-toggle` once, without separate
press/release processes or shared launcher state. The wrapper supports both
Noctalia v4 and v5 IPC commands.

### Shell panels

| Keys | Action |
| --- | --- |
| `Ctrl+Alt+Delete` | Session menu toggle |
| `Super+N` | Control center toggle |
| `Ctrl+Alt+C` | Clear notifications |
| `Super+K` | Settings toggle |
| `Super+L` | Lock screen |
| `Super+Alt+L` | Restore lock state from last session |

### Brightness

| Keys | Action |
| --- | --- |
| `XF86MonBrightnessUp` | Brightness increase |
| `XF86MonBrightnessDown` | Brightness decrease |

### Media

| Keys | Action |
| --- | --- |
| `Ctrl+Super+Space` | Play / pause |
| `XF86AudioPlay` / `XF86AudioPause` | Play / pause |
| `Ctrl+Super+Equal` | Next track |
| `XF86AudioNext` | Next track |
| `Ctrl+Super+Minus` | Previous track |
| `XF86AudioPrev` | Previous track |
| `XF86AudioStop` | Stop |

### Recording

| Keys | Action |
| --- | --- |
| `Super+R` | Toggle screen recording (`record-toggle.sh`) |

### Clipboard & emoji

| Keys | Action |
| --- | --- |
| `Super+V` | Noctalia clipboard launcher |
| `Super+Alt+V` | Noctalia clipboard launcher (same; placeholder for delete) |
| `Super+Period` | Noctalia emoji launcher |

---

## End4 (Illogical Impulse)

Bindings specific to the **End4** family, layered on top of Common. End4
Official uses profile ID `end4` and QuickShell config `ii`; End4 pC uses
profile ID `end4-pc` and QuickShell config `end4-pC`. Both variants use the
same upstream end-4 Hyprland base patched by
`Linux/NixOS/home/end4/patches/hypr.nix` and overlays Wahrwelt-specific
keybinds from `Linux/dots/hypr/end4/keybinds.lua`.

> Modules: `Linux/dots/hypr/end4/keybinds.lua`,
> upstream `end-4/dots-hyprland`, and optional `pctrade/end4-pC` QuickShell.

### Launcher

End4 ships its own QuickShell launcher tied to **Super long-press**. Wahrwelt
keeps one shared End4 Hyprland/keybind layer while the selector starts either
`qs -c ii` from `~/.config/quickshell/ii` or `qs -c end4-pC` from
`~/.config/quickshell/end4-pC`. No separate pC keybind table is needed.

| Trigger | Action |
| --- | --- |
| `Super` long-press | Open end4 launcher (handled by upstream IPC) |

### Wahrwelt overrides

| Keys | Action |
| --- | --- |
| `Ctrl+Super+Alt+Slash` | Open writable `~/.config/hypr/user/default.lua` in the default editor |

### Unbound upstream defaults

These upstream end-4 bindings are explicitly cleared before requiring the
shared Wahrwelt set, to avoid double-binds and conflicts:

| Cleared |
| --- |
| `Super+Return` |
| `Super+E` |
| `Super+Shift+C` |
| `Super+Shift+A` |
| `Super+Shift+N` |
| `Super+Shift+T` |
| `Super+Shift+B` |
| `Ctrl+Shift+Escape` |
| `Super+S` |
| `Super+Shift+S` |
| `Super+C` |
| `Super+F` |
| `Super+X` |
| `Super+Ctrl+Space` |

After clearing, End4 requires `shell-common-keybinds.lua` and
`shell-workspace-keybinds.lua` - see the [Common](#common-all-shells)
section above for the resulting set.

### Workspaces (end4 extras)

| Keys | Action |
| --- | --- |
| `$kbNextWs` (`Ctrl+Super+Right`) | Next workspace |
| `$kbPrevWs` (`Ctrl+Super+Left`) | Previous workspace |

### Window operations (end4 extras)

| Keys | Action |
| --- | --- |
| `$kbMoveWindow` (`Super+Z`, held) | Move window |
| `$kbResizeWindow` (`Super+X`, held) | Resize window |
| `$kbCloseWindow` (`Super+Q`) | Close active window |
| `$kbToggleWindowFloating` (`Super+Space`) | Toggle floating |
| `$kbPinWindow` (`Super+P`) | Pin window |
| `$kbWindowFullscreen` (`Ctrl+Shift+Return`) | Fullscreen |
| `Super+Ctrl+Return` | Fullscreen (extra alias) |
| `$kbWindowBorderedFullscreen` (`Super+Shift+Return`) | Bordered fullscreen |

### Notes

- All other end-4 panels, AI sidebar, anime corner, and shell-internal
  shortcuts come from the upstream `end-4/dots-hyprland` config. They are
  not duplicated here - refer to the upstream wiki for shell-internal
  bindings: <https://end-4.github.io/dots-hyprland-wiki/>.
- Wahrwelt keeps shared End4 settings in the mutable
  `~/.config/illogical-impulse/config.json`, so themes and shell-side changes
  persist across Official/pC switches without a rebuild.
- Both QuickShell trees come from immutable Nix store outputs. The pC
  self-updater is disabled; flake lock updates are the supported update path.
