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
  home = packageSets.home { };
in
{
  config = lib.mkMerge [
    (wahrweltLib.mkIfPresetOrMore "minimal" wahrwelt {
      home.packages = home.media ++ packageSets.runtime.waylandTools;
    })
    (wahrweltLib.mkIfPresetOrMore "desktop" wahrwelt {
      home.packages = home.desktop ++ home.misc;
    })
    (wahrweltLib.mkIfPresetOrMore "developer" wahrwelt {
      home.packages = home.apiTools ++ home.containers ++ home.dev;
    })
    (lib.mkIf (wahrweltLib.presets.personal wahrwelt) {
      home.packages = home.personal ++ home.games;
    })
  ];
}
