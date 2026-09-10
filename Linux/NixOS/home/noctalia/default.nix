{
  config,
  homeLibs,
  inputs,
  lib,
  wahrwelt,
  options,
  pkgs,
  ...
}:

let
  dotfilesLib = homeLibs.dotfiles;
  trans = homeLibs.transparency;
  isV4 = wahrwelt.noctalia.version == "v4";
  patchedNoctalia = import ./patches/package.nix {
    inherit inputs pkgs;
    system = pkgs.stdenv.hostPlatform.system;
  };
  noctaliaV5Package = patchedNoctalia.v5;
  noctaliaV4Package = patchedNoctalia.v4;
  settingsJson = ./legacy-v4/settings.json;
  colorsJson = ./legacy-v4/colors.json;
  pluginsJson = ./legacy-v4/plugins.json;
  colorSchemesDir = ./legacy-v4/colorschemes;
  wallpapersJson = pkgs.writeText "noctalia-wallpapers.json" (
    builtins.toJSON {
      wallpapers = { };
      defaultWallpaper = "@HOME@/Pictures/Wallpapers/1.jpg";
      usedRandomWallpapers = { };
    }
  );
  colorSchemeFiles =
    if builtins.pathExists colorSchemesDir then
      lib.filesystem.listFilesRecursive colorSchemesDir
    else
      [ ];
  noctaliaConfigDir = "$HOME/.config/noctalia";
  noctaliaProgramOption = if options.programs ? noctalia then "noctalia" else "noctalia-shell";
  noctaliaProgramConfigV5 = {
    enable = true;
    package = noctaliaV5Package;
    systemd.enable = false;
    checkConfig = true;
    settings = {
      shell = {
        setup_wizard_enabled = false;
        telemetry_enabled = false;
        panel = {
          transparency_mode = "soft";
        };
      };
      theme = {
        mode = "dark";
      };
    };
    customPalettes = lib.mkForce { };
  };
  noctaliaProgramConfigV4 = {
    enable = true;
    package = noctaliaV4Package;
    systemd.enable = false;
    settings = lib.mkForce { };
  }
  // (
    if noctaliaProgramOption == "noctalia" then
      { customPalettes = lib.mkForce { }; }
    else
      { colors = lib.mkForce { }; }
  );
  seedColorSchemeFiles = lib.concatMapStringsSep "\n" (
    file:
    let
      relPath = lib.removePrefix "${toString colorSchemesDir}/" (toString file);
      relDir = builtins.dirOf relPath;
      storeFile = builtins.path {
        path = file;
        name = lib.replaceStrings [ " " ] [ "-" ] (builtins.baseNameOf file);
      };
      targetDirExpr =
        if relDir == "." then
          "${noctaliaConfigDir}/colorschemes"
        else
          "${noctaliaConfigDir}/colorschemes/${relDir}";
      targetExpr = "${noctaliaConfigDir}/colorschemes/${relPath}";
    in
    ''
      ensure_real_directory "${targetDirExpr}" || exit $?
      seed_if_missing "${targetExpr}" "${storeFile}" || exit $?
    ''
  ) colorSchemeFiles;
in
{
  programs =
    if isV4 then
      { ${noctaliaProgramOption} = noctaliaProgramConfigV4; }
    else
      { noctalia = noctaliaProgramConfigV5; };

  home.file = lib.mkIf isV4 {
    # Stylix can materialize this as a store symlink, but v4 treats it as
    # mutable runtime state for selected wallpapers.
    ".cache/noctalia/wallpapers.json".enable = lib.mkForce false;
  };

  home.activation = lib.mkIf isV4 {
    noctaliaSeedConfig = homeLibs.shellSeed.mkSeedActivation {
      dirs = [
        "$HOME/.config/noctalia"
        "$HOME/.cache/noctalia"
      ];
      body = ''
        seed_json_object "$HOME/.cache/noctalia/wallpapers.json" "${wallpapersJson}" "${config.home.homeDirectory}"
        seed_json_object "$HOME/.config/noctalia/settings.json" "${settingsJson}" "${config.home.homeDirectory}" '
            .bar //= {} |
            ${dotfilesLib.mkOpacityDefault trans ".bar.backgroundOpacity"} |
            ${dotfilesLib.mkOpacityDefault trans ".bar.capsuleOpacity"} |
            .dock //= {} |
            ${dotfilesLib.mkOpacityDefault trans ".dock.backgroundOpacity"} |
            .general //= {} |
            .general.dimmerOpacity //= ${toString trans.content} |
            .notifications //= {} |
            ${dotfilesLib.mkOpacityDefault trans ".notifications.backgroundOpacity"} |
            .osd //= {} |
            ${dotfilesLib.mkOpacityDefault trans ".osd.backgroundOpacity"} |
            .ui //= {} |
            ${dotfilesLib.mkOpacityDefault trans ".ui.panelBackgroundOpacity"}
        '

        seed_json_object "$HOME/.config/noctalia/colors.json" "${colorsJson}"
        seed_json_object "$HOME/.config/noctalia/plugins.json" "${pluginsJson}"

        ${seedColorSchemeFiles}
      '';
    };
  };
}
