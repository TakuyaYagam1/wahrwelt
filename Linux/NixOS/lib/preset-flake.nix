{
  inputs,
  preset,
  nixosRoot,
}:

let
  inherit (inputs)
    home-manager
    nixpkgs
    nixpkgs-stable
    self
    ;

  system = "x86_64-linux";
  layout = import ./layout.nix { inherit nixosRoot; };
  expectedInputNames = builtins.attrNames (import ./preset-inputs.nix { inherit preset; });
  actualInputNames = builtins.filter (name: name != "self") (builtins.attrNames inputs);

  inputsForModules = inputs // {
    mysetup = self;
    wahrwelt = self;
  };

  wahrweltLib = import ./mysetup.nix {
    inherit (nixpkgs) lib;
  };

  adaptV1ToV2HostArgs = import ./migrations/v1_to_v2/legacy-hashed-password.nix {
    inherit (nixpkgs) lib;
  };

  mkSystemContext =
    targetSystem:
    let
      pkgs-stable = import nixpkgs-stable {
        localSystem = targetSystem;
        config = {
          allowUnfree = true;
          permittedInsecurePackages = import ./permitted-insecure-packages.nix;
        };
      };

      overlays = import ./flake-overlays.nix {
        inputs = inputsForModules;
        system = targetSystem;
        inherit preset;
      };

      flakeModules = import ./flake-modules.nix {
        inherit
          wahrweltLib
          overlays
          pkgs-stable
          ;
        inputs = inputsForModules;
      };
    in
    {
      inherit
        flakeModules
        overlays
        pkgs-stable
        ;
    };

  defaultContext = mkSystemContext system;

  baseMkWahrweltHost = import ./mk-host.nix {
    inherit
      home-manager
      wahrweltLib
      nixpkgs
      nixpkgs-stable
      preset
      ;
    inputs = inputsForModules;
  };

  mkWahrweltHost =
    args:
    let
      hostVars = if builtins.isAttrs args.hostVars then args.hostVars else import args.hostVars;
      selectedPreset = wahrweltLib.presets.fromConfig hostVars;
    in
    if preset != "full" && selectedPreset != preset then
      throw "Wahrwelt preset flake '${preset}' cannot build host preset '${selectedPreset}'"
    else
      baseMkWahrweltHost (adaptV1ToV2HostArgs args);

  mkMySetupHost = mkWahrweltHost;

  flakeOutputs = import ./flake-packages.nix {
    inherit
      inputs
      layout
      nixpkgs
      system
      ;
  };

  presetHostVars =
    let
      defaults = import (nixosRoot + "/hosts/NixOS/host-vars.nix");
    in
    defaults
    // {
      packages = (defaults.packages or { }) // {
        inherit preset;
      };
      features = (defaults.features or { }) // {
        secureBoot = false;
      };
    };

  presetHost =
    if preset == "full" then
      null
    else
      mkWahrweltHost {
        inherit system;
        hostname = "wahrwelt-${preset}-check";
        hostVars = presetHostVars;
        extraModules = [
          (_: {
            fileSystems."/" = {
              device = "none";
              fsType = "tmpfs";
            };
          })
        ];
      };

  hosts =
    if preset == "full" then
      import ./hosts.nix {
        inherit mkWahrweltHost system;
      }
    else
      { };
in
assert
  builtins.length expectedInputNames == builtins.length actualInputNames
  && builtins.all (name: builtins.elem name actualInputNames) expectedInputNames
  || throw "Wahrwelt ${preset} flake inputs drifted from lib/preset-inputs.nix";
{
  lib = {
    inherit mkMySetupHost mkWahrweltHost;
    inherit wahrweltLib;
    mysetupLib = wahrweltLib;
    wahrwelt = wahrweltLib;
    mysetup = wahrweltLib;
  };

  nixosModules = rec {
    wahrwelt = defaultContext.flakeModules.wahrweltModule;
    mysetup = wahrwelt;
    default = wahrwelt;
  };

  packages.${system} = flakeOutputs.packages;
  checks.${system} =
    flakeOutputs.checks
    // (
      if preset == "full" then
        { }
      else
        {
          preset-host = presetHost.config.system.build.toplevel;
        }
    );
  apps.${system} = flakeOutputs.apps;
  formatter.${system} = nixpkgs.legacyPackages.${system}.nixfmt-tree;
}
// hosts
