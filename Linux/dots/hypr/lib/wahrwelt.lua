local M = {}

M.home = os.getenv("HOME")
if M.home == nil then
	error("HOME is not set; cannot locate Wahrwelt Hyprland config")
end

M.config_home = os.getenv("XDG_CONFIG_HOME") or (M.home .. "/.config")
M.state_home = os.getenv("XDG_STATE_HOME") or (M.home .. "/.local/state")
M.hypr = M.config_home .. "/hypr"
M.runtime = M.state_home .. "/wahrwelt/hypr-runtime"

function M.runtime_file(name)
	return M.runtime .. "/" .. name
end

function M.config_file(name)
	return M.hypr .. "/" .. name
end

function M.file_exists(path)
	local file, open_error, error_code = io.open(path, "r")
	if file == nil then
		if error_code == 2 then
			return false
		end
		error(open_error or ("failed to open " .. path), 2)
	end
	file:close()
	return true
end

function M.optional_require(module)
	local path_module = module
	if module:sub(1, #"wahrwelt.") == "wahrwelt." then
		path_module = "user." .. module:sub(#"wahrwelt." + 1)
	end
	local path = M.hypr .. "/" .. path_module:gsub("%.", "/") .. ".lua"
	if not M.file_exists(path) then
		return false
	end
	require(module)
	return true
end

function M.load_runtime(name)
	dofile(M.runtime_file(name))
end

function M.bind_exec(keys, command, opts)
	hl.bind(keys, hl.dsp.exec_cmd(command), opts)
end

local function split_dispatch(command)
	local name, rest = command:match("^%s*(%S+)%s*(.-)%s*$")
	return name or "", rest or ""
end

local function parse_resize_arg(value)
	if value == nil or value == "" or value == "0" then
		return 0
	end
	value = value:gsub("%%$", "")
	return tonumber(value) or 0
end

function M.dispatcher(command)
	local name, rest = split_dispatch(command)

	if name == "global" then
		return hl.dsp.global(rest)
	elseif name == "workspace" then
		return hl.dsp.focus({ workspace = rest })
	elseif name == "movetoworkspace" then
		return hl.dsp.window.move({ workspace = rest })
	elseif name == "togglespecialworkspace" then
		return hl.dsp.workspace.toggle_special(rest)
	elseif name == "movefocus" then
		return hl.dsp.focus({ direction = rest })
	elseif name == "movewindow" then
		return hl.dsp.window.move({ direction = rest })
	elseif name == "cyclenext" then
		return hl.dsp.window.cycle_next({ next = rest ~= "prev" })
	elseif name == "changegroupactive" then
		if rest == "b" then
			return hl.dsp.group.prev()
		end
		return hl.dsp.group.next()
	elseif name == "togglegroup" then
		return hl.dsp.group.toggle()
	elseif name == "moveoutofgroup" then
		return hl.dsp.window.move({ out_of_group = true })
	elseif name == "lockactivegroup" then
		return hl.dsp.group.lock({ action = rest ~= "" and rest or "toggle" })
	elseif name == "centerwindow" then
		return hl.dsp.window.center()
	elseif name == "resizeactive" then
		local exact, x, y = rest:match("^(exact)%s+(%S+)%s+(%S+)$")
		if exact ~= nil then
			return hl.dsp.window.resize({ x = parse_resize_arg(x), y = parse_resize_arg(y) })
		end
		x, y = rest:match("^(%S+)%s+(%S+)$")
		return hl.dsp.window.resize({ x = parse_resize_arg(x), y = parse_resize_arg(y), relative = true })
	elseif name == "pin" then
		return hl.dsp.window.pin()
	elseif name == "fullscreen" then
		return hl.dsp.window.fullscreen({ mode = rest == "1" and "maximized" or "fullscreen" })
	elseif name == "togglefloating" then
		return hl.dsp.window.float({ action = "toggle" })
	elseif name == "killactive" then
		return hl.dsp.window.kill()
	end

	error("unsupported Hyprland dispatcher: " .. command)
end

function M.bind_dispatch(keys, command, opts)
	hl.bind(keys, M.dispatcher(command), opts)
end

return M
