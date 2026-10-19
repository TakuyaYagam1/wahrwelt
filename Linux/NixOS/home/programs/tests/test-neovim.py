"""Exercise the Neovim user layer in an isolated, plugin-free headless process."""

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

DOTS = Path(__file__).resolve().parents[4] / "dots" / "nvim"


class NeovimUserLayerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.config = self.home / ".config" / "nvim"
        shutil.copytree(DOTS, self.config)
        self.env = os.environ | {
            "HOME": str(self.home),
            "XDG_CONFIG_HOME": str(self.home / ".config"),
            "XDG_DATA_HOME": str(self.home / ".local/share"),
            "XDG_STATE_HOME": str(self.home / ".local/state"),
            "XDG_CACHE_HOME": str(self.home / ".cache"),
            "NVIM_APPNAME": "nvim",
        }

    def write(self, name, text):
        path = self.config / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def run_lua(self, text):
        script = self.home / "test.lua"
        script.write_text(text)
        result = subprocess.run(
            ["nvim", "--headless", "-u", "NONE", "-i", "NONE", "-l", str(script)],
            env=self.env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=15,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout)

    def test_existing_config_and_user_overrides_run_in_order(self):
        # Keep the actual legacy setup shape: it has no user hook or import.
        self.write(
            "lua/config/lazy.lua",
            """
local opts = { spec = { { import = "plugins.custom" } }, custom = 42 }
local a, b, c = require("lazy").setup(opts, nil, "forwarded")
assert(a == 11 and b == nil and c == 13)
assert(opts.spec[2] == nil, "must not mutate existing config tables")
vim.g.legacy_lazy_ran = true
""",
        )
        self.write("lua/config/options.lua", "vim.opt.tabstop = 3\n")
        self.write("lua/config/keymaps.lua", 'vim.keymap.set("n", "<F6>", "legacy")\n')
        self.write("lua/config/autocmds.lua", 'vim.g.autocmd_order = "legacy"\n')
        self.write(
            "lua/plugins/custom.lua", 'vim.g.plugin_order = "legacy"; return {}\n'
        )
        self.write(
            "lua/user/options.lua",
            """
assert(vim.o.tabstop == 3)
vim.opt.tabstop = 7
vim.g.user_options_count = (vim.g.user_options_count or 0) + 1
""",
        )
        self.write(
            "lua/user/keymaps.lua",
            """
assert(vim.fn.maparg("<F6>", "n") == "legacy")
vim.keymap.set("n", "<F6>", "user")
""",
        )
        self.write(
            "lua/user/autocmds.lua",
            """
assert(vim.g.autocmd_order == "legacy")
vim.api.nvim_create_autocmd("User", {
  pattern = "UserTest", callback = function() vim.g.autocmd_order = "user" end,
})
""",
        )
        self.write(
            "lua/user/plugins/init.lua",
            """
assert(vim.g.plugin_order == "legacy")
vim.g.plugin_order = "user"
return {}
""",
        )
        # This is the external lazy.nvim boundary only. Config files, require,
        # keymaps, options, and the lifecycle events all execute in real Neovim.
        self.write(
            "lua/lazy/init.lua",
            """
local M = {}
function M.setup(opts, unused, forwarded)
  assert(M.setup == _G.original_lazy_setup, "setup wrapper must be restored")
  assert(opts.custom == 42 and unused == nil and forwarded == "forwarded")
  assert(opts.spec[#opts.spec].import == "user.plugins")
  local function imports(spec)
    if spec.import then require(spec.import) end
    for _, child in ipairs(spec) do imports(child) end
  end
  imports(opts.spec)
  for _, name in ipairs({ "options", "keymaps", "autocmds" }) do
    require("config." .. name)
    local pattern = "LazyVim" .. name:sub(1, 1):upper() .. name:sub(2)
    vim.api.nvim_exec_autocmds("User", { pattern = pattern })
    vim.api.nvim_exec_autocmds("User", { pattern = pattern })
  end
  return 11, nil, 13
end
_G.original_lazy_setup = M.setup
return M
""",
        )
        self.run_lua("""
local loaders_before = #package.loaders
dofile(vim.fn.stdpath("config") .. "/init.lua")
assert(vim.g.legacy_lazy_ran)
assert(vim.o.tabstop == 7 and vim.g.user_options_count == 1)
assert(vim.fn.maparg("<F6>", "n") == "user")
vim.api.nvim_exec_autocmds("User", { pattern = "UserTest" })
assert(vim.g.autocmd_order == "user")
assert(vim.g.plugin_order == "user")
assert(#package.loaders == loaders_before, "temporary lazy loader must be removed")
""")

    def test_user_errors_are_reported(self):
        self.write("lua/user/options.lua", 'error("deliberate user failure")\n')
        self.run_lua("""
require("wahrwelt.user").setup()
vim.api.nvim_exec_autocmds("User", { pattern = "LazyVimOptions" })
local messages = vim.api.nvim_exec2("messages", { output = true }).output
assert(messages:find("deliberate user failure", 1, true), messages)
""")

    def test_absent_user_module_is_optional(self):
        shutil.rmtree(self.config / "lua/user", ignore_errors=True)
        self.run_lua("""
require("wahrwelt.user").setup()
for _, pattern in ipairs({ "LazyVimOptions", "LazyVimKeymaps", "LazyVimAutocmds" }) do
  vim.api.nvim_exec_autocmds("User", { pattern = pattern })
end
""")

    def test_setup_supports_spec_and_options_arguments_and_reports_errors(self):
        self.write(
            "lua/lazy/init.lua",
            """
local M = {}
function M.setup(spec, opts, ...)
  assert(M.setup == _G.original_lazy_setup)
  assert(spec[1][1] == "original/plugin" and spec[1].enabled == false)
  assert(spec[2].import == "user.plugins")
  assert(opts == _G.original_options and select("#", ...) == 2)
  error("deliberate setup failure")
end
_G.original_lazy_setup = M.setup
return M
""",
        )
        self.run_lua("""
local spec = { "original/plugin", enabled = false }
_G.original_options = { custom = 42 }
require("wahrwelt.user").setup()
local lazy = require("lazy")
local ok, err = pcall(lazy.setup, spec, _G.original_options, nil, "last")
assert(not ok and err:find("deliberate setup failure", 1, true), tostring(err))
assert(lazy.setup == _G.original_lazy_setup)
assert(spec[2] == nil and spec.enabled == false)
""")

    def test_all_lua_files_parse_without_running_them(self):
        paths = [str(path) for path in DOTS.rglob("*.lua")]
        lua_paths = "{" + ", ".join(json.dumps(path) for path in paths) + "}"
        self.run_lua(
            f"for _, path in ipairs({lua_paths}) do assert(loadfile(path)) end\n"
        )


if __name__ == "__main__":
    unittest.main()
