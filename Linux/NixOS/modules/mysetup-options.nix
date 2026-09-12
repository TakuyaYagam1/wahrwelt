{ lib, ... }:

let
  inherit (lib) mkOption types;

  inherit (import ../lib/preset-names.nix) presetNames defaultPreset;

  gpuKinds = [
    "amd"
    "intel"
    "nvidia"
    "other"
  ];

  strOption = mkOption { type = types.str; };
  boolOption =
    default:
    mkOption {
      type = types.bool;
      inherit default;
    };

  wahrweltType = types.submodule {
    options = {
      host = mkOption {
        type = types.submodule {
          options = {
            hostname = strOption;
            stateVersion = strOption;
            configDirectory = strOption;
            autoGarbageCollector = boolOption true;
            autoOptimiseStore = boolOption true;
          };
        };
      };
      user = mkOption {
        type = types.submodule {
          options = {
            username = strOption;
            fullName = strOption;
            homeDirectory = strOption;
          };
        };
      };
      locale = mkOption {
        type = types.submodule {
          options = {
            timeZone = strOption;
            defaultLocale = strOption;
            extraLocale = strOption;
            consoleKeyMap = strOption;
            weatherLocation = strOption;
          };
        };
      };
      git = mkOption {
        type = types.submodule {
          options = {
            username = strOption;
            email = strOption;
          };
        };
      };
      packages = mkOption {
        type = types.submodule {
          options.preset = mkOption {
            type = types.enum presetNames;
            default = defaultPreset;
          };
        };
      };
      noctalia = mkOption {
        type = types.submodule {
          options.version = mkOption {
            type = types.enum [
              "v4"
              "v5"
            ];
            default = "v5";
          };
        };
        default = { };
      };
      hardware = mkOption {
        type = types.submodule {
          options.gpu = mkOption {
            type = types.enum gpuKinds;
          };
        };
      };
      features = mkOption {
        type = types.submodule {
          freeformType = types.attrsOf types.bool;
          options = {
            secureBoot = boolOption false;
            ctfTools = boolOption false;
            claudeDesktopCowork = boolOption false;
            omnirouter = boolOption false;
            portainer = boolOption false;
            observability = boolOption false;
          };
        };
      };
      nix = mkOption {
        type = types.submodule {
          options = {
            gcRetention = mkOption {
              type = types.str;
              default = "14d";
              description = "Retention window passed to nix-collect-garbage --delete-older-than.";
            };
            maxJobs = mkOption {
              type = types.either types.int (types.enum [ "auto" ]);
              default = 1;
              description = "Maximum number of Nix derivations to build in parallel.";
            };
            cores = mkOption {
              type = types.int;
              default = 2;
              description = "Maximum cores exposed to each individual Nix build.";
            };
            swapSizeMiB = mkOption {
              type = types.nullOr types.ints.positive;
              default = 32 * 1024;
              description = "Declarative disk swapfile size in MiB; null disables the managed swapfile.";
            };
            zram = mkOption {
              type = types.submodule {
                options = {
                  enable = boolOption true;
                  memoryPercent = mkOption {
                    type = types.ints.positive;
                    default = 50;
                    description = "Maximum compressed zram swap size as a percentage of physical RAM.";
                  };
                };
              };
              default = { };
              description = "Compressed RAM swap settings used before falling back to disk swap.";
            };
          };
        };
        default = { };
      };
      hypr = mkOption {
        type = types.submodule {
          options = {
            keyboardLayouts = strOption;
            keyboardToggle = strOption;
            windowOpacity = strOption;
          };
        };
      };
      display = mkOption {
        type = types.submodule {
          options = {
            monitorName = strOption;
            monitorMode = strOption;
            monitorPosition = strOption;
            monitorScale = strOption;
            extraMonitors = mkOption {
              type = types.listOf (
                types.submodule {
                  options = {
                    name = strOption;
                    mode = mkOption {
                      type = types.str;
                      default = "preferred";
                    };
                    position = mkOption {
                      type = types.str;
                      default = "auto";
                    };
                    scale = mkOption {
                      type = types.str;
                      default = "1";
                    };
                  };
                }
              );
              default = [ ];
              description = "Additional Hyprland outputs beyond the primary monitor.";
            };
          };
        };
      };
      wallpapers = mkOption {
        type = types.submodule {
          options.enable = boolOption false;
        };
      };
    };
  };
in
{
  imports = [
    (lib.mkAliasOptionModule [ "mysetup" ] [ "wahrwelt" ])
  ];

  options.wahrwelt = mkOption {
    type = wahrweltType;
    default = { };
    description = "Canonical Wahrwelt host/user configuration.";
  };

}
