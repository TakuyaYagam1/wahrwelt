{
  config,
  lib,
  wahrweltLib,
  pkgs,
  ...
}:

let
  cfg = config.wahrwelt;
  packageSets = import ../lib/package-sets.nix { inherit lib pkgs; };
  presetPackages = packageSets.forPreset (wahrweltLib.presets.fromConfig cfg);
in
{
  config = wahrweltLib.mkIfPresetOrMore "developer" cfg {
    environment.systemPackages = presetPackages.developmentPackages;

    environment.variables = {
      PLAYWRIGHT_BROWSERS_PATH = "${pkgs.playwright-driver.browsers}";
      PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS = "true";
    };
  };
}
