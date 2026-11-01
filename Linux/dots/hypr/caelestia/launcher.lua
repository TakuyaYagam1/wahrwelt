local wahrwelt = require("lib.wahrwelt")

-- Hyprland cancels a release bind when Super is used in another chord.
-- Use IPC to avoid an unconditional global-shortcut release callback.
wahrwelt.bind_exec("SUPER + SUPER_L", "caelestia shell drawers toggle launcher", { release = true })
