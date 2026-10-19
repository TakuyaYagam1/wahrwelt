# User space

[← Linux guide](../README.md)

Keep personal settings in the writable user files below. Home Manager can then
update the shared configuration while your overrides load afterward.

Paths in this guide use `~/.config`. If you set `XDG_CONFIG_HOME`, use that
directory instead. You can edit these files without rebuilding NixOS.

| Application | Start here | Load changes |
| --- | --- | --- |
| Fish | `~/.config/fish/user/config.fish` | Open a new shell |
| Neovim | `~/.config/nvim/lua/user/` | Restart Neovim |
| Hyprland | `~/.config/hypr/user/default.lua` | Run `hyprctl reload` |

## Fish

Put aliases, abbreviations, environment variables, and overrides in
`~/.config/fish/user/config.fish`. It runs after the generated aliases,
abbreviations, and shell settings. For example:

```fish
set -gx EDITOR nvim

if status is-interactive
    alias ll 'eza -lah --icons'
    abbr --add gds 'git diff --staged'

    # Replace the default banner with an empty greeting.
    function fish_greeting
    end
end
```

The user directories come first in Fish's function and completion lookup paths:

- `~/.config/fish/user/functions/`: one autoloaded function per `.fish` file.
- `~/.config/fish/user/completions/`: personal command completions.

For example, save this as `user/functions/cconfig.fish`:

```fish
function cconfig
    cd /etc/nixos
end
```

Define replacements for aliases, explicitly defined functions, or event
functions in `user/config.fish`; changing the autoload path does not override
an explicit definition. Open a new shell to check the final startup order.

Home Manager owns `fish/config.fish` and the functions, completions, and plugin
snippets it generates. Add personal settings to `fish/user/` instead of editing
those links. Existing user files keep their contents across updates. Missing
templates are created again, so leave an empty `user/config.fish` if you want no
personal startup commands.

## Neovim

The LazyVim configuration has a separate `lua/user/` layer:

| File under `~/.config/nvim/` | Purpose |
| --- | --- |
| `lua/user/options.lua` | Options after LazyVim and `lua/config/options.lua` |
| `lua/user/keymaps.lua` | Mappings after LazyVim and `lua/config/keymaps.lua` |
| `lua/user/autocmds.lua` | Autocommands after LazyVim and `lua/config/autocmds.lua` |
| `lua/user/plugins/*.lua` | Plugin specs imported after existing specs |

For example, add to `lua/user/options.lua`:

```lua
vim.opt.relativenumber = false
vim.opt.autochdir = false
vim.opt.wrap = true
```

Add a mapping in `lua/user/keymaps.lua`:

```lua
vim.keymap.set("n", "<leader>uw", "<cmd>set wrap!<cr>", {
  desc = "Toggle line wrapping",
})
```

To disable the startup dashboard, create `lua/user/plugins/dashboard.lua`:

```lua
return {
  {
    "folke/snacks.nvim",
    opts = { dashboard = { enabled = false } },
  },
}
```

Restart Neovim after editing. The options, mappings, and autocommand hooks run
on the corresponding LazyVim events; a replacement configuration that removes
LazyVim must load its personal modules itself.

`init.lua` and `lua/wahrwelt/user.lua` are managed entrypoints. Keep changes in
`lua/user/`. The repository's other Lua files and `stylua.toml` are writable
defaults that can receive merged updates. `lazy-lock.json`, `lazyvim.json`, and
the supplied `lua/user/` templates are created when missing and otherwise keep
their local contents.

## Hyprland

Edit `~/.config/hypr/user/default.lua`. It loads after the shared configuration,
shell adapters, and VM bindings. Add settings after any existing imports:

```lua
hl.config({
  general = {
    gaps_in = 4,
    gaps_out = 8,
  },
})

hl.unbind("SUPER + SHIFT + RETURN")
hl.bind("SUPER + SHIFT + RETURN", hl.dsp.exec_cmd("foot"))
```

Unbind an existing combination before replacing it. Adding the same binding
twice can run both actions.

Split larger customizations into files if useful. For example, add this to
`default.lua` to load `~/.config/hypr/user/monitor.lua`:

```lua
require("wahrwelt.monitor")
```

The physical directory is `user/`, but its Lua namespace remains `wahrwelt.*`.
`lib.wahrwelt` remains the shared helper module. Files are not discovered
automatically: `default.lua` decides which personal modules to load.

The supplied `default.lua` optionally loads `wahrwelt.execs`,
`wahrwelt.general`, `wahrwelt.rules`, and `wahrwelt.keybinds`. Keep those imports
if you use the matching files. When `default.lua` is absent, the entrypoint
tries those four compatibility modules directly.

Reload and check for configuration errors:

```bash
hyprctl reload
hyprctl configerrors
```

`hypr/user/hyprland.lua` is a managed entrypoint even though it sits inside
`user/`. Shared Hyprland files are managed links. Personal `default.lua` and
other user modules remain writable; do not edit the runtime files under
`~/.local/state/wahrwelt/hypr-runtime/` for persistent changes.

## What updates change

An activated `nixos-update` runs Home Manager. It refreshes generated Fish and
shared Hyprland configuration and reconciles writable Neovim defaults.
`update --dry-run` and `update --no-switch` do not activate these dotfiles.

| File type | Update behavior |
| --- | --- |
| Generated configuration links | Refresh from the selected configuration |
| Personal templates and Neovim JSON files | Create when absent; preserve existing contents |
| Known writable defaults | Compare local content with the saved and incoming defaults |
| Files you added | Leave untouched unless a later release introduces the same path |

For known writable defaults, an unchanged file receives the new version.
Independent local and upstream edits merge. Overlapping edits stop the whole
mutable-file batch before it writes changes. Proposed Lua, JSON, and Fish
content is syntax-checked before publication.

An upstream deletion removes an unchanged managed default and preserves a
locally edited one. A local deletion stays deleted while upstream is unchanged;
an upstream modification then produces a delete/modify conflict. Personal
templates use the separate create-when-missing behavior described above.

## Existing installations and conflicts

An existing writable default can be adopted when it exactly matches the
incoming version. If it differs and there is no known upstream baseline, the
updater reports it as **unmanaged** and preserves it. It does not guess which
parts are your changes.

To let that file receive updates again, move your customizations into
`lua/user/`, retain a backup, and restore the matching default from the Wahrwelt
revision you are applying. Retry activation. Use the same approach for merge
conflicts: preserve your changes in the user layer before restoring the
incoming default. Merely retrying with the conflicting file unchanged will
stop again.

Replacing a protected generated link with an edited regular file or unrelated
symlink stops the preactivation check. Move the customization into the user
layer, back up the replacement, remove it, and retry. Exact known defaults from
the previous generation or accepted bootstrap templates can be relinked.

The mutable-file reconciler keeps `baseline.json` and the latest `journal.json`
in `$XDG_STATE_HOME/wahrwelt/dotfiles/`, normally
`~/.local/state/wahrwelt/dotfiles/`. The baseline records delivered defaults;
the journal retains before/after bytes for the latest batch. Keep both when
investigating a failed update.

A failed publication rolls back that batch when safe. An interrupted operation
or concurrent edit can require manual recovery; retain the journal and reported
recovery files. This mechanism does not roll back an already-switched NixOS
generation. See [maintenance and recovery](maintenance.md) for system recovery.
