{
  lib,
  pkgs,
  homeLibs,
  wahrwelt,
  wahrweltLib,
  ...
}:

let
  desktopOrMore = wahrweltLib.presets.desktopOrMore wahrwelt;
  wahrweltPkgs = pkgs.wahrwelt or (pkgs.mysetup or { });
  source = homeLibs.dotfiles.dotsRoot + "/nvim";
  relativeFiles = map (path: lib.removePrefix (toString source + "/") (toString path)) (
    lib.filesystem.listFilesRecursive source
  );
  mutableFiles = builtins.filter (
    path:
    path != "init.lua"
    && path != "lua/wahrwelt/user.lua"
    && (
      lib.hasSuffix ".lua" path
      || builtins.elem path [
        "lazy-lock.json"
        "lazyvim.json"
        "stylua.toml"
      ]
    )
  ) relativeFiles;
in
{
  imports = [ ../lib/managed-config.nix ];

  config = lib.mkIf desktopOrMore {
    programs.neovim = {
      enable = true;
      package = wahrweltPkgs.neovim or pkgs.neovim;
      withRuby = true;
      withPython3 = true;
      plugins = [
        (pkgs.symlinkJoin {
          name = "nvim-treesitter-parsers";
          paths = pkgs.vimPlugins.nvim-treesitter.withAllGrammars.dependencies;
        })
      ];
      initLua = builtins.readFile (source + "/init.lua");
    };

    wahrwelt = {
      mutableFiles = builtins.listToAttrs (
        map (path: {
          name = "nvim/${path}";
          value = {
            source = source + "/${path}";
            policy = if lib.hasPrefix "lua/user/" path || lib.hasSuffix ".json" path then "seed" else "merge";
            validator =
              if lib.hasSuffix ".lua" path then
                "lua"
              else if lib.hasSuffix ".json" path then
                "json"
              else
                "none";
          };
        }) mutableFiles
      );

      guardedFiles = [
        "nvim/init.lua"
        "nvim/lua/wahrwelt/user.lua"
      ];
      guardedFileBaselines."nvim/init.lua" = [
        (source + "/init.lua")
        (pkgs.writeText "nvim-legacy-init.lua" ''
          -- bootstrap lazy.nvim, LazyVim and your plugins
          require("config.lazy")
        '')
      ];
      guardedFileBaselines."nvim/lua/wahrwelt/user.lua" = [
        (source + "/lua/wahrwelt/user.lua")
      ];
    };
    xdg.configFile."nvim/lua/wahrwelt/user.lua".source = source + "/lua/wahrwelt/user.lua";
  };
}
