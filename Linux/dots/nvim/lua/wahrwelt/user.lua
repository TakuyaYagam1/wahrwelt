-- Home Manager owns this adapter. Personal configuration belongs in lua/user/.
local M = {}
local configured = false

local function has_module(name)
  if package.loaded[name] or package.preload[name] then
    return true
  end
  local path = "lua/" .. name:gsub("%.", "/")
  return #vim.api.nvim_get_runtime_file(path .. ".lua", false) > 0
    or #vim.api.nvim_get_runtime_file(path .. "/init.lua", false) > 0
end

local function with_user_plugins(spec)
  local plugins = vim.api.nvim_get_runtime_file("lua/user/plugins/*.lua", false)
  if not has_module("user.plugins") and #plugins == 0 then
    return spec
  end
  -- lazy.nvim accepts nested specs. Keep the original spec and all its fields
  -- intact, including single-plugin specs and imports in customized lazy.lua.
  return { spec or {}, { import = "user.plugins" } }
end

local function wrap_lazy(lazy)
  local setup = lazy.setup
  lazy.setup = function(spec, opts, ...)
    lazy.setup = setup
    if type(spec) == "table" and spec.spec then
      local config = vim.tbl_extend("force", {}, spec)
      config.spec = with_user_plugins(spec.spec)
      return setup(config, opts, ...)
    end
    return setup(with_user_plugins(spec), opts, ...)
  end
end

function M.setup()
  if configured then
    return
  end
  configured = true

  local group = vim.api.nvim_create_augroup("WahrweltUserConfig", { clear = true })
  for _, name in ipairs({ "options", "keymaps", "autocmds" }) do
    vim.api.nvim_create_autocmd("User", {
      group = group,
      pattern = "LazyVim" .. name:sub(1, 1):upper() .. name:sub(2),
      once = true,
      callback = function()
        local module = "user." .. name
        if has_module(module) then
          require(module)
        end
      end,
    })
  end

  if package.loaded.lazy then
    wrap_lazy(package.loaded.lazy)
    return
  end

  -- Existing config.lazy modules add lazy.nvim to runtimepath themselves. Wait
  -- for that require, then remove this searcher before loading the real module.
  local loaders = package.loaders or package.searchers
  local searcher
  searcher = function(name)
    if name ~= "lazy" then
      return nil
    end
    for index, loader in ipairs(loaders) do
      if loader == searcher then
        table.remove(loaders, index)
        break
      end
    end
    for _, find in ipairs(loaders) do
      local loader, parameter = find(name)
      if type(loader) == "function" then
        return function(...)
          local lazy = loader(...)
          wrap_lazy(lazy)
          return lazy
        end,
          parameter
      end
    end
    return nil
  end
  table.insert(loaders, 1, searcher)
end

return M
