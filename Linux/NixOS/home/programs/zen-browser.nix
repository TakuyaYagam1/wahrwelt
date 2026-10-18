{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.programs.zen-browser;
  prepareProfile = lib.escapeShellArgs [
    "${pkgs.python3}/bin/python3"
    "${./zen-profile.py}"
    config.home.homeDirectory
    cfg.configPath
  ];
in
{
  programs.zen-browser = {
    enable = true;
    env.MOZ_LEGACY_PROFILES = "1";
    profiles.default = {
      isDefault = true;
      sine.enable = true;
    };
  };

  home = {
    # Keep existing profile registrations, including secondary profiles.
    file."${cfg.configPath}/profiles.ini".enable = false;

    activation = {
      checkZenProfile = lib.hm.dag.entryBefore [ "checkLinkTargets" ] ''
        ${prepareProfile} --check
      '';

      zenProfile = lib.hm.dag.entryBetween [ "linkGeneration" ] [ "writeBoundary" ] ''
        run ${prepareProfile}
      '';
    };

    # Shared Hyprland dotfiles and the non-NixOS installer use this command.
    packages = [
      (pkgs.writeShellScriptBin "zen" ''
        exec ${lib.getExe cfg.finalPackage} "$@"
      '')
    ];
  };
}
