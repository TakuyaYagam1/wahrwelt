{
  config,
  lib,
  wahrweltLib,
  pkgs,
  pkgs-stable,
  ...
}:

let
  inherit (wahrweltLib) presets;
  cfg = config.wahrwelt;
  packageSets = import ../lib/package-sets.nix {
    inherit
      lib
      pkgs
      pkgs-stable
      ;
  };
  presetPackages = packageSets.forPreset (presets.fromConfig cfg);
in
{
  environment.pathsToLink = [ "/share/icons" ];

  environment.systemPackages = presetPackages.systemPackages;
}
