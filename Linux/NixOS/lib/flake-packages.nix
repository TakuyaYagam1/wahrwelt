{
  inputs,
  layout,
  nixpkgs,
  system,
}:

let
  flakePkgs = import nixpkgs {
    localSystem = system;
    config = {
      allowUnfree = true;
    };
  };

  nixosSource = layout.nixos;
  dotsSource = layout.dots;
  installerSource = layout.installer;
  caelestiaPackages = import ../home/caelestia/patches/package.nix {
    inherit inputs system;
    prev = flakePkgs;
  };

  wahrweltRuntimeSource = flakePkgs.runCommand "wahrwelt-runtime-source" { } ''
    mkdir -p "$out"
    cp -a ${nixosSource} "$out/NixOS"
    cp -a ${dotsSource} "$out/dots"
    cp -a ${installerSource} "$out/installer"
  '';

  wahrwelt = flakePkgs.buildGoModule {
    pname = "wahrwelt";
    version = "0.1.0";
    src = installerSource;
    subPackages = [
      "cmd/wahrwelt"
      "cmd/wahrwelt-fs-helper"
    ];
    vendorHash = "sha256-t56VdyNPlGtPmLBvFuPrONvoPEOvvJn9foNmUBQlIeI=";
    nativeBuildInputs = [ flakePkgs.makeWrapper ];
    ldflags = [
      "-s"
      "-w"
    ];
    postInstall = ''
      ln -s wahrwelt $out/bin/mysetup
      wrapProgram $out/bin/wahrwelt \
        --set WAHRWELT_REPO_ROOT ${wahrweltRuntimeSource}/NixOS \
        --set MYSETUP_REPO_ROOT ${wahrweltRuntimeSource}/NixOS \
        --set WAHRWELT_XKB_RULES_DIR ${flakePkgs.xkeyboard_config}/share/X11/xkb/rules \
        --set MYSETUP_XKB_RULES_DIR ${flakePkgs.xkeyboard_config}/share/X11/xkb/rules \
        --set WAHRWELT_PRIVILEGED_PYTHON ${flakePkgs.python3}/bin/python3 \
        --set WAHRWELT_PRIVILEGED_FS_HELPER $out/bin/wahrwelt-fs-helper \
        --set WAHRWELT_FS_HELPER $out/bin/wahrwelt-fs-helper \
        --prefix PATH : ${
          flakePkgs.lib.makeBinPath (
            with flakePkgs;
            [
              coreutils
              findutils
              gnused
              python3
              rsync
              mkpasswd
              nix
              nixos-rebuild
              git
              curl
              jq
              hyprland
              libarchive
              unzip
              sing-box
            ]
          )
        }
    '';
  };

  packages = {
    inherit (flakePkgs) cachix;
    claude-desktop = flakePkgs.callPackage ../pkgs/claude-desktop.nix { };
    omnirouter = flakePkgs.callPackage ../pkgs/omnirouter.nix { };
    inherit wahrwelt;
    wahrwelt-fs-helper = wahrwelt;
    mysetup = wahrwelt;
    default = wahrwelt;
  }
  // flakePkgs.lib.optionalAttrs (inputs ? quickshell) {
    quickshell = inputs.quickshell.packages.${system}.default;
  }
  // flakePkgs.lib.optionalAttrs (inputs ? caelestia-shell) {
    caelestia-cli = caelestiaPackages.cli;
    caelestia-shell = caelestiaPackages.shell;
  }
  // flakePkgs.lib.optionalAttrs (inputs ? noctalia) {
    noctalia =
      (import ../home/noctalia/patches/package.nix {
        inherit inputs system;
        pkgs = flakePkgs;
      }).v5;
  }
  // flakePkgs.lib.optionalAttrs (inputs ? codex) {
    codex = inputs.codex.packages.${system}.default;
  };

  checks = {
    inherit wahrwelt;
    mysetup = wahrwelt;
  };

  wahrweltApp = {
    type = "app";
    program = "${packages.wahrwelt}/bin/wahrwelt";
    meta.description = "Run the Wahrwelt NixOS installer";
  };

  mysetupApp = {
    type = "app";
    program = "${packages.mysetup}/bin/mysetup";
    meta.description = "Run the supported MySetup compatibility installer entrypoint";
  };
in
{
  inherit checks packages;

  apps = {
    wahrwelt = wahrweltApp;
    mysetup = mysetupApp;
    default = wahrweltApp;
  };
}
