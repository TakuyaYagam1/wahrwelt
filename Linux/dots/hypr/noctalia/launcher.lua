local wahrwelt = require("lib.wahrwelt")

-- Let Hyprland cancel Super chords without racing separate press/release processes.
wahrwelt.bind_exec("SUPER + SUPER_L", wahrwelt.hypr .. "/scripts/noctalia-msg.sh launcher-toggle", { release = true })
