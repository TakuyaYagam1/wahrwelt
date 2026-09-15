{
  config,
  lib,
  wahrweltLib,
  pkgs,
  ...
}:

let
  inherit (wahrweltLib) presets;
  cfg = config.wahrwelt;
  cfgDir = cfg.host.configDirectory;
  desktopOrMore = presets.desktopOrMore cfg;
  personal = presets.personal cfg;
in
{
  programs = {
    amnezia-vpn = lib.mkIf personal {
      enable = true;
      package = pkgs.amnezia-vpn;
    };

    dconf.enable = true;

    appimage = lib.mkIf desktopOrMore {
      enable = true;
      binfmt = true;
    };

    nh = {
      enable = true;
      clean.enable = true;
      clean.extraArgs = "--keep-since 4d --keep 3";
      flake = cfgDir;
    };
  };

  environment.systemPackages = lib.optionals personal [
    pkgs.amneziawg-tools
  ];

  boot.extraModulePackages = lib.optionals personal [ config.boot.kernelPackages.amneziawg ];
  boot.kernelModules = lib.optionals personal [ "amneziawg" ];

  # AmneziaWG sends packets whose reverse path doesn't match the routing table;
  # strict rp_filter (set in system/networking.nix) drops them. Override to "loose".
  networking.firewall.checkReversePath = lib.mkIf personal (lib.mkForce "loose");
}
