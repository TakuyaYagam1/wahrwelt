{
  lib,
  wahrwelt,
  wahrweltLib,
  pkgs,
  pkgs-stable ? pkgs,
  ...
}:

let
  packageSets = import ../../lib/package-sets.nix {
    inherit lib pkgs pkgs-stable;
  };
  presetPackages = packageSets.forPreset (wahrweltLib.presets.fromConfig wahrwelt);
in
{
  home.packages = presetPackages.homePackages;
}
