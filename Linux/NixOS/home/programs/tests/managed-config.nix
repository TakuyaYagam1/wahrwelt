{ home, nixosDir }:
let
  flake = builtins.getFlake ("path:" + nixosDir);
  pkgs = import flake.inputs.nixpkgs { system = "x86_64-linux"; };
  inherit (pkgs) lib;
  hm = flake.inputs.home-manager.lib.homeManagerConfiguration {
    inherit pkgs;
    modules = [
      ../../lib/managed-config.nix
      ../fish.nix
      ../neovim.nix
      {
        home = {
          username = "dotfile-test";
          homeDirectory = home;
          stateVersion = "26.05";
        };
        xdg.configHome = "${home}/config with spaces";
        xdg.stateHome = "${home}/state";
        programs.neovim = {
          plugins = lib.mkForce [ ];
          withRuby = lib.mkForce false;
          withPython3 = lib.mkForce false;
        };
      }
    ];
    extraSpecialArgs = {
      homeLibs = import ../../lib { inherit lib pkgs; };
      wahrwelt = { };
      wahrweltLib.presets.desktopOrMore = _: true;
    };
  };
  preflight = hm.config.home.activation.wahrweltDotfilePreflight.data;
  sorted = flake.inputs.home-manager.lib.hm.dag.topoSort hm.config.home.activation;
in
{
  inherit preflight;
  apply = hm.config.home.activation.wahrweltDotfileApply.data;
  assertions = map (entry: entry.assertion) hm.config.assertions;
  order = map (entry: entry.name) sorted.result;
  manifests = builtins.filter (
    path: builtins.match ".*wahrwelt-(mutable|managed)-dotfiles.json.drv" path != null
  ) (builtins.attrNames (builtins.getContext preflight));
}
