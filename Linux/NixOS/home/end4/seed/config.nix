{
  config,
  end4Lib,
  pkgs,
  ...
}:

let
  inherit (end4Lib) dotfilesLib homeLibs settings;
  trans = settings.transparency;
  end4BaseConfig =
    let
      baseConfigPath = ../config.json;
    in
    if builtins.pathExists baseConfigPath then
      builtins.fromJSON (builtins.readFile baseConfigPath)
    else
      settings.fallbackBaseConfig;
  end4ConfigJson = pkgs.writeText "end4-config.json" (builtins.toJSON end4BaseConfig);
in
{
  home.activation.end4SeedConfig = homeLibs.shellSeed.mkSeedActivation {
    dirs = [ "$HOME/.config/illogical-impulse" ];
    body = ''
          seed_json_object "$HOME/.config/illogical-impulse/config.json" "${end4ConfigJson}" "${config.home.homeDirectory}" '
              .appearance //= {} |
              .appearance.transparency //= {} |
              ${dotfilesLib.mkBoolDefault ".appearance.transparency.enable" true} |
              .appearance.transparency.automatic //= false |
              .appearance.transparency.backgroundTransparency //= ${toString trans.shell} |
          ${
            dotfilesLib.mkOpacityFallback ".appearance.transparency.contentTransparency" {
              vendor = 0;
              target = trans.content;
            }
          } |
          .regionSelector //= {} |
          .regionSelector.targetRegions //= {} |
          ${
            dotfilesLib.mkOpacityFallback ".regionSelector.targetRegions.contentRegionOpacity" {
              vendor = 0.2;
              target = trans.regionContent;
            }
          } |
          ${dotfilesLib.mkOpacityFallback ".regionSelector.targetRegions.opacity" {
            vendor = 0.3;
            target = trans.shell;
          }}
      '
    '';
  };
}
