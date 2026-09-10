{
  config,
  homeLibs,
  pkgs,
  lib,
  wahrwelt,
  wahrweltLib,
  ...
}:

let
  dotfilesLib = homeLibs.dotfiles;
  trans = homeLibs.transparency;
  wahrweltPkgs = pkgs.wahrwelt or (pkgs.mysetup or { });
  shellDefaults = builtins.fromJSON (builtins.readFile ./default-settings.json);
  shellSettings = lib.recursiveUpdate shellDefaults config.caelestiaShellSettings;
  shellJson = pkgs.writeText "caelestia-shell.json" (builtins.toJSON shellSettings);
  liveWallpapersEnabled =
    wahrwelt.features.caelestiaLiveWallpapers or (wahrweltLib.presets.desktopOrMore wahrwelt);
  caelestiaPackage =
    if liveWallpapersEnabled then
      wahrweltPkgs.caelestia-live-shell
    else
      wahrweltPkgs.caelestia-shell or pkgs.caelestia-shell;
  caelestiaCliPackage =
    if liveWallpapersEnabled then
      wahrweltPkgs.caelestia-live-cli
    else
      wahrweltPkgs.caelestia-cli or pkgs.caelestia-cli;
in
{
  imports = [
    ./appearance.nix
    ./background.nix
    ./bar.nix
    ./general.nix
    ./launcher.nix
    ./services.nix
    ./session.nix
    ./utilities.nix
  ];

  options.caelestiaShellSettings = lib.mkOption {
    type = lib.types.attrsOf lib.types.anything;
    default = { };
    description = "Aggregated settings tree merged from slice modules and seeded into ~/.config/caelestia/shell.json.";
  };

  config = {
    programs.caelestia = {
      enable = true;
      package = caelestiaPackage;

      systemd = {
        enable = false;
        target = "graphical-session.target";
        environment = [ ];
      };

      cli = {
        enable = true;
        package = caelestiaCliPackage;
        settings.theme.enableGtk = false;
      };
    };

    home = {
      activation = {
        caelestiaSeedShellJson = homeLibs.shellSeed.mkSeedActivation {
          dirs = [ "$HOME/.config/caelestia" ];
          body = ''
            seed_json_object "$HOME/.config/caelestia/shell.json" "${shellJson}" "" '
                .bar //= {} |
                if .bar.status? then .bar |= del(.status) else . end |
                if .bar.excludedScreens? then .bar.excludedScreens |= map(select(. != "")) else . end |
                .general //= {} |
                .general.logo = "${config.caelestiaShellSettings.general.logo}" |
                .appearance //= {} |
                .appearance.transparency //= {} |
                ${dotfilesLib.mkBoolDefault ".appearance.transparency.enabled" true} |
                ${dotfilesLib.mkOpacityDefault trans ".appearance.transparency.base"} |
                .appearance.transparency.layers //= ${toString trans.content} |
                .background //= {} |
                .background.desktopClock //= {} |
                .background.desktopClock.shadow //= {} |
                ${dotfilesLib.mkOpacityDefault trans ".background.desktopClock.shadow.opacity"} |
                .background.desktopClock.background //= {} |
                ${dotfilesLib.mkOpacityDefault trans ".background.desktopClock.background.opacity"}
            '
          '';
        };

        caelestiaSeedDynamicScheme = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          scheme_state="$HOME/.local/state/caelestia/scheme.json"
          if [ ! -e "$scheme_state" ]; then
            $DRY_RUN_CMD ${pkgs.coreutils}/bin/mkdir -p "$HOME/.local/state/caelestia"
            if command -v caelestia >/dev/null 2>&1; then
              $DRY_RUN_CMD caelestia scheme set -n dynamic -v rainbow >/dev/null 2>&1 || true
            fi
          fi
        '';
      };

      packages = [
        (wahrweltPkgs.quickshell or pkgs.quickshell)
        # caelestia-shell uses xmllint to resolve XKB layout descriptions.
        pkgs.libxml2
      ];
    };
  };
}
