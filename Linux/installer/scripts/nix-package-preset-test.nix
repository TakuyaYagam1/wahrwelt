let
  nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR";
  flake = builtins.getFlake ("path:" + nixosDir);
  system = "x86_64-linux";
  pkgs = import flake.inputs.nixpkgs {
    inherit system;
    config.allowUnfree = true;
  };
  pkgs-stable = import flake.inputs.nixpkgs-stable {
    inherit system;
    config.allowUnfree = true;
  };

  fakeLayers = import (nixosDir + "/lib/package-sets/preset-layers.nix") {
    systemSets = {
      base = [ "system-minimal" ];
      desktop = [ "system-desktop" ];
      developer = [ "system-developer" ];
      personal = [ "system-personal" ];
      personalStable = [ "system-personal-stable" ];
    };
    homeSets = {
      media = [ "home-media" ];
      desktop = [ "home-desktop" ];
      misc = [ "home-desktop-extra" ];
      apiTools = [ "home-developer-api" ];
      containers = [ "home-developer-containers" ];
      dev = [ "home-developer" ];
      personal = [ "home-personal" ];
      games = [ "home-personal-games" ];
    };
    developmentSets = {
      tools = [ "development" ];
      personalTools = [ "development-personal" ];
    };
    runtimeSets.waylandTools = [ "desktop-runtime" ];
    fontSets.desktop = [ "desktop-font" ];
  };

  expected = {
    minimal = {
      systemPackages = [ "system-minimal" ];
      homePackages = [ ];
      developmentPackages = [ ];
      fontPackages = [ ];
    };
    desktop = {
      systemPackages = [
        "system-minimal"
        "system-desktop"
      ];
      homePackages = [
        "desktop-runtime"
        "home-media"
        "home-desktop"
        "home-desktop-extra"
      ];
      developmentPackages = [ ];
      fontPackages = [ "desktop-font" ];
    };
    developer = {
      systemPackages = [
        "system-minimal"
        "system-desktop"
        "system-developer"
      ];
      homePackages = [
        "desktop-runtime"
        "home-media"
        "home-desktop"
        "home-desktop-extra"
        "home-developer-api"
        "home-developer-containers"
        "home-developer"
      ];
      developmentPackages = [ "development" ];
      fontPackages = [ "desktop-font" ];
    };
    personal = {
      systemPackages = [
        "system-minimal"
        "system-desktop"
        "system-developer"
        "system-personal"
        "system-personal-stable"
      ];
      homePackages = [
        "desktop-runtime"
        "home-media"
        "home-desktop"
        "home-desktop-extra"
        "home-developer-api"
        "home-developer-containers"
        "home-developer"
        "home-personal"
        "home-personal-games"
      ];
      developmentPackages = [
        "development"
        "development-personal"
      ];
      fontPackages = [ "desktop-font" ];
    };
  };

  packageSets = import (nixosDir + "/lib/package-sets.nix") {
    inherit pkgs pkgs-stable;
    lib = pkgs.lib;
  };
  minimalSystemNames = map pkgs.lib.getName (packageSets.forPreset "minimal").systemPackages;
  desktopSystemNames = map pkgs.lib.getName (packageSets.forPreset "desktop").systemPackages;
  minimalHomeNames = map pkgs.lib.getName (packageSets.forPreset "minimal").homePackages;
  desktopNames = map pkgs.lib.getName (packageSets.forPreset "desktop").homePackages;

  wahrweltLib = import (nixosDir + "/lib/mysetup.nix") { lib = pkgs.lib; };
  baseWahrwelt = import (nixosDir + "/hosts/NixOS/host-vars.nix");
  mkHome =
    preset:
    let
      wahrwelt = baseWahrwelt // {
        packages = baseWahrwelt.packages // {
          inherit preset;
        };
      };
    in
    flake.inputs.home-manager.lib.homeManagerConfiguration {
      inherit pkgs;
      modules = [ (nixosDir + "/home/home.nix") ];
      extraSpecialArgs = {
        inherit wahrwelt wahrweltLib;
        inputs = flake.inputs;
        mysetup = wahrwelt;
        mysetupLib = wahrweltLib;
        pkgs-stable = pkgs-stable;
      };
    };
  minimalHome = mkHome "minimal";
  desktopHome = mkHome "desktop";
  mimeDefaults = desktopHome.config.xdg.mimeApps.defaultApplications;
in
assert fakeLayers.presets == expected;
assert !(builtins.elem "neovim" minimalSystemNames);
assert !(builtins.elem "btop" minimalSystemNames);
assert builtins.elem "neovim" desktopSystemNames;
assert builtins.elem "btop" desktopSystemNames;
assert !(builtins.elem "cava" minimalHomeNames);
assert builtins.elem "cava" desktopNames;
assert builtins.elem "vscode" desktopNames;
assert builtins.elem "wpsoffice" desktopNames;
assert !(builtins.elem "firefox" desktopNames);
assert !(builtins.elem "libreoffice" desktopNames);
assert mimeDefaults."text/markdown" == [ "code.desktop" ];
assert
  mimeDefaults."application/vnd.openxmlformats-officedocument.wordprocessingml.document" == [
    "wps-office-wps.desktop"
  ];
assert !minimalHome.config.programs.neovim.enable;
assert !minimalHome.config.programs.btop.enable;
assert desktopHome.config.programs.neovim.enable;
assert desktopHome.config.programs.btop.enable;
"ok"
