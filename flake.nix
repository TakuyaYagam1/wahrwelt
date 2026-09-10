{
  description = "Reusable Wahrwelt shell modules and installer entrypoint";

  inputs = {
    # Temporary compatibility pin: avoid Hyprland 0.56.1 Glaze packaging failure.
    nixpkgs.url = "github:NixOS/nixpkgs?rev=643809054d65fdd466a63e3155b8c498cb483c04";
    nixpkgs-stable.url = "github:NixOS/nixpkgs/nixos-26.05";

    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    caelestia-shell = {
      url = "github:caelestia-dots/shell/v2.4.0";
      inputs = {
        caelestia-cli.follows = "caelestia-cli";
        nixpkgs.follows = "nixpkgs";
        quickshell.follows = "quickshell";
      };
    };
    caelestia-cli = {
      url = "github:caelestia-dots/cli/v1.1.2";
      inputs = {
        caelestia-shell.follows = "caelestia-shell";
        nixpkgs.follows = "nixpkgs";
      };
    };
    quickshell = {
      url = "github:outfoxxed/quickshell";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    noctalia = {
      url = "github:noctalia-dev/noctalia/v5.0.1";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    noctalia-shell = {
      url = "github:noctalia-dev/noctalia-shell/v4.7.7";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    end4-dotfiles = {
      url = "git+https://github.com/end-4/dots-hyprland?submodules=1";
      flake = false;
    };
    end4-pc = {
      url = "github:pctrade/end4-pC";
      flake = false;
    };

    zen-browser = {
      url = "github:youwen5/zen-browser-flake";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    claude-code = {
      url = "github:sadjow/claude-code-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    codex = {
      url = "github:sadjow/codex-cli-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    codex-desktop-linux = {
      url = "github:ilysenko/codex-desktop-linux";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    kimi-code = {
      url = "github:MoonshotAI/kimi-code";
    };
    happ-nix = {
      url = "github:DaHL-gh/happ-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    neovim-nightly-overlay = {
      url = "github:nix-community/neovim-nightly-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    lanzaboote = {
      url = "github:nix-community/lanzaboote";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nixos-hardware.url = "github:NixOS/nixos-hardware/master";
    nix-snapd = {
      url = "github:nix-community/nix-snapd";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    sops-nix = {
      url = "github:Mic92/sops-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nix-index-database = {
      url = "github:nix-community/nix-index-database";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    stylix = {
      url = "github:danth/stylix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      home-manager,
      ...
    }@inputs:
    let
      inherit (nixpkgs) lib;
      systems = [ "x86_64-linux" ];
      forSystems = lib.genAttrs systems;

      layout = import ./Linux/NixOS/lib/layout.nix {
        nixosRoot = ./Linux/NixOS;
      };

      wahrweltLib = import ./Linux/NixOS/lib/mysetup.nix {
        inherit lib;
      };

      shellOverlays = import ./Linux/NixOS/lib/shell-overlays.nix {
        inherit inputs;
      };

      shellHomeModule =
        {
          config,
          lib,
          modulesPath,
          wahrwelt,
          pkgs,
          ...
        }:
        {
          disabledModules = lib.optionals (builtins.pathExists "${modulesPath}/programs/noctalia.nix") [
            "programs/noctalia.nix"
          ];

          _module.args.homeLibs = import ./Linux/NixOS/home/lib {
            inherit lib pkgs;
          };

          imports = [
            inputs.caelestia-shell.homeManagerModules.default
            (
              if wahrwelt.noctalia.version == "v4" then
                inputs.noctalia-shell.homeModules.default
              else
                inputs.noctalia.homeModules.default
            )
            ./Linux/NixOS/home/shells
            ./Linux/NixOS/home/caelestia
            ./Linux/NixOS/home/noctalia
            ./Linux/NixOS/home/end4
          ];

          home = {
            username = lib.mkDefault wahrwelt.user.username;
            homeDirectory = lib.mkDefault wahrwelt.user.homeDirectory;
            stateVersion = lib.mkDefault wahrwelt.host.stateVersion;
          };

          programs.waybar.enable = lib.mkForce false;
        };

      shellsNixosModule =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        {
          imports = [
            ./Linux/NixOS/modules/mysetup-options.nix
            home-manager.nixosModules.home-manager
          ];

          wahrwelt = {
            host = {
              hostname = lib.mkDefault config.networking.hostName;
              stateVersion = lib.mkDefault config.system.stateVersion;
              configDirectory = lib.mkDefault "/etc/nixos";
            };
            locale = {
              timeZone = lib.mkDefault "UTC";
              defaultLocale = lib.mkDefault "en_US.UTF-8";
              extraLocale = lib.mkDefault "en_US.UTF-8";
              consoleKeyMap = lib.mkDefault "us";
              weatherLocation = lib.mkDefault "";
            };
            git = {
              username = lib.mkDefault config.wahrwelt.user.username;
              email = lib.mkDefault "";
            };
            packages.preset = lib.mkDefault "desktop";
            noctalia.version = lib.mkDefault "v5";
            hardware.gpu = lib.mkDefault "other";
            features = {
              secureBoot = lib.mkDefault false;
              ctfTools = lib.mkDefault false;
              omnirouter = lib.mkDefault false;
              observability = lib.mkDefault false;
            };
            hypr = {
              keyboardLayouts = lib.mkDefault "us";
              keyboardToggle = lib.mkDefault "";
              windowOpacity = lib.mkDefault "1.0";
            };
            display = {
              monitorName = lib.mkDefault "";
              monitorMode = lib.mkDefault "preferred";
              monitorPosition = lib.mkDefault "auto";
              monitorScale = lib.mkDefault "1";
            };
            wallpapers.enable = lib.mkDefault false;
          };

          nixpkgs.overlays = [
            shellOverlays.shellPackagesOverlay
            shellOverlays.valkeyNoCheckOverlay
          ];

          programs.hyprland = {
            enable = lib.mkDefault true;
            xwayland.enable = lib.mkDefault true;
          };

          services.dbus.enable = lib.mkDefault true;

          users.users.${config.wahrwelt.user.username} = {
            isNormalUser = lib.mkDefault true;
            description = lib.mkDefault config.wahrwelt.user.fullName;
            home = lib.mkDefault config.wahrwelt.user.homeDirectory;
          };

          xdg.portal = {
            enable = lib.mkDefault true;
            xdgOpenUsePortal = lib.mkDefault true;
            extraPortals = with pkgs; [
              xdg-desktop-portal-gtk
              xdg-desktop-portal-hyprland
            ];
            config = {
              common.default = lib.mkDefault [ "gtk" ];
              hyprland.default = lib.mkDefault [
                "hyprland"
                "gtk"
              ];
            };
          };

          home-manager = {
            useGlobalPkgs = lib.mkDefault true;
            useUserPackages = lib.mkDefault true;
            backupFileExtension = lib.mkDefault "backup";
            overwriteBackup = lib.mkDefault true;
            users.${config.wahrwelt.user.username} = shellHomeModule;
            extraSpecialArgs = {
              inherit inputs wahrweltLib;
              inherit (config) wahrwelt;
              mysetup = config.wahrwelt;
              mysetupLib = wahrweltLib;
            };
          };
        };

      installerOutputsFor =
        system:
        import ./Linux/NixOS/lib/flake-packages.nix {
          inherit layout nixpkgs system;
        };

      wahrweltPackageFor = system: (installerOutputsFor system).packages.wahrwelt;
      omnirouterPackageFor = system: (installerOutputsFor system).packages.omnirouter;
      claudeDesktopPackageFor = system: (installerOutputsFor system).packages.claude-desktop;
      wahrweltAppFor = system: {
        type = "app";
        program = "${wahrweltPackageFor system}/bin/wahrwelt";
        meta.description = "Run the Wahrwelt NixOS installer";
      };
      mysetupAppFor = system: {
        type = "app";
        program = "${wahrweltPackageFor system}/bin/mysetup";
        meta.description = "Run the supported MySetup compatibility NixOS installer entrypoint";
      };

      linuxNixosOutputs = (import ./Linux/NixOS/flake.nix).outputs inputs;

      shellModuleCheckFor =
        system:
        let
          nixos = lib.nixosSystem {
            inherit system;
            modules = [
              shellsNixosModule
              (
                { ... }:
                {
                  system.stateVersion = "26.05";
                  wahrwelt.user = {
                    username = "alice";
                    fullName = "Alice";
                    homeDirectory = "/home/alice";
                  };
                }
              )
            ];
          };
          shellHome = nixos.config.home-manager.users.alice;
          runtimePythonPresent = builtins.any (
            package: (package.outPath or "") == nixos.pkgs.python3.outPath
          ) shellHome.home.packages;
        in
        assert lib.assertMsg runtimePythonPresent "desktop Hypr runtime must directly install python3";
        shellHome.home.activationPackage;
    in
    {
      overlays = rec {
        shells = shellOverlays.shellPackagesOverlay;
        default = shells;
        valkeyNoCheck = shellOverlays.valkeyNoCheckOverlay;
      };

      nixosModules = rec {
        wahrwelt = linuxNixosOutputs.nixosModules.wahrwelt;
        mysetup = wahrwelt;
        workstation = wahrwelt;
        shells = shellsNixosModule;
        default = shells;
      };

      lib = (linuxNixosOutputs.lib or { }) // {
        inherit wahrweltLib;
        mysetupLib = wahrweltLib;
        homeManagerModules = rec {
          shells = shellHomeModule;
          default = shells;
        };
      };

      packages = forSystems (system: {
        claude-desktop = claudeDesktopPackageFor system;
        wahrwelt = wahrweltPackageFor system;
        wahrwelt-fs-helper = wahrweltPackageFor system;
        mysetup = self.packages.${system}.wahrwelt;
        omnirouter = omnirouterPackageFor system;
        default = self.packages.${system}.wahrwelt;
      });

      apps = forSystems (system: {
        wahrwelt = wahrweltAppFor system;
        mysetup = mysetupAppFor system;
        default = self.apps.${system}.wahrwelt;
      });

      checks = forSystems (system: {
        wahrwelt = self.packages.${system}.wahrwelt;
        mysetup = self.packages.${system}.mysetup;
        "shells-module" = shellModuleCheckFor system;
      });

      formatter = forSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);
    };
}
