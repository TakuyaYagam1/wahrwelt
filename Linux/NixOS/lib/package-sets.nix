{
  lib,
  pkgs,
  pkgs-stable ? pkgs,
  inputs ? null,
  system ? pkgs.stdenv.hostPlatform.system,
}:

let
  systemSets = import ./package-sets/system.nix {
    inherit pkgs pkgs-stable;
  };
  devSets = import ./package-sets/dev.nix {
    inherit pkgs;
  };
  runtimeSets = import ./package-sets/runtime.nix {
    inherit lib pkgs;
  };
  fontSets = import ./package-sets/fonts.nix {
    inherit pkgs-stable;
  };
  ctfSets = import ./package-sets/ctf.nix {
    inherit pkgs pkgs-stable inputs system;
  };
in
{
  system = {
    base = systemSets.systemBase;
    desktop = systemSets.systemDesktop;
    developer = systemSets.systemDeveloper;
    personal = systemSets.systemPersonal;
    personalStable = systemSets.systemPersonalStable;
  };
  development = {
    tools = devSets.devTools;
    personalTools = devSets.personalDevTools;
  };
  runtime = {
    inherit (runtimeSets) waylandCore waylandTools;
    end4 = {
      binPackages = runtimeSets.end4BinPackages;
      qtPackages = runtimeSets.end4QtPackages;
    };
  };
  fonts = {
    desktop = fontSets.desktopFonts;
  };
  ctf = {
    cloud = ctfSets.ctfCloud;
    crypto = ctfSets.ctfCrypto;
    forensics = ctfSets.ctfForensics;
    hardware = ctfSets.ctfHardware;
    misc = ctfSets.ctfMisc;
    mobile = ctfSets.ctfMobile;
    network = ctfSets.ctfNetwork;
    osint = ctfSets.ctfOsint;
    pwn = ctfSets.ctfPwn;
    reverse = ctfSets.ctfReverse;
    stego = ctfSets.ctfStego;
    web = ctfSets.ctfWeb;
  };
  home = args: import ./package-sets/home.nix ({ inherit pkgs pkgs-stable; } // args);
}
