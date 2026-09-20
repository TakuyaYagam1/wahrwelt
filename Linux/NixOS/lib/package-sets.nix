{
  lib,
  pkgs,
  pkgs-stable ? pkgs,
  inputs ? null,
  system ? pkgs.stdenv.hostPlatform.system,
}:

let
  systemSource = import ./package-sets/system.nix {
    inherit pkgs pkgs-stable;
  };
  developmentSource = import ./package-sets/dev.nix {
    inherit pkgs;
  };
  runtimeSource = import ./package-sets/runtime.nix {
    inherit lib pkgs;
  };
  fontSource = import ./package-sets/fonts.nix {
    inherit pkgs-stable;
  };
  ctfSets = import ./package-sets/ctf.nix {
    inherit
      pkgs
      pkgs-stable
      inputs
      system
      ;
  };
  systemPackages = {
    base = systemSource.systemBase;
    desktop = systemSource.systemDesktop;
    developer = systemSource.systemDeveloper;
    personal = systemSource.systemPersonal;
    personalStable = systemSource.systemPersonalStable;
  };
  development = {
    tools = developmentSource.devTools;
    personalTools = developmentSource.personalDevTools;
  };
  runtime = {
    inherit (runtimeSource) waylandCore waylandTools;
    end4 = {
      binPackages = runtimeSource.end4BinPackages;
      qtPackages = runtimeSource.end4QtPackages;
    };
  };
  fonts = {
    desktop = fontSource.desktopFonts;
  };
  homeSets = import ./package-sets/home.nix { inherit pkgs pkgs-stable; };
  presetLayers = import ./package-sets/preset-layers.nix {
    systemSets = systemPackages;
    developmentSets = development;
    runtimeSets = runtime;
    fontSets = fonts;
    inherit homeSets;
  };
in
{
  inherit
    development
    fonts
    runtime
    ;
  system = systemPackages;
  inherit (presetLayers) deltas forPreset presets;
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
