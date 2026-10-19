{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.wahrwelt;
  home = config.home.homeDirectory;
  root = config.xdg.configHome;
  state = "${config.xdg.stateHome}/wahrwelt/dotfiles";
  configPrefix = "${lib.removePrefix "${home}/" root}/";
  manifest = pkgs.writeText "wahrwelt-mutable-dotfiles.json" (
    builtins.toJSON {
      version = 1;
      files = lib.mapAttrs (_: entry: entry // { source = "${entry.source}"; }) cfg.mutableFiles;
    }
  );
  guarded = lib.filter (
    name: builtins.hasAttr name config.xdg.configFile && config.xdg.configFile.${name}.enable
  ) (lib.unique cfg.guardedFiles);
  guardManifest = pkgs.writeText "wahrwelt-managed-dotfiles.json" (
    builtins.toJSON {
      version = 1;
      files = builtins.listToAttrs (
        map (name: {
          name = lib.removePrefix configPrefix config.xdg.configFile.${name}.target;
          value = {
            source = "${config.xdg.configFile.${name}.source}";
            baselines = map (path: "${path}") (cfg.guardedFileBaselines.${name} or [ ]);
          };
        }) guarded
      );
    }
  );
  reconcileArgs = lib.escapeShellArgs [
    "--home"
    home
    "--root"
    root
    "--state"
    state
    "--manifest"
    (toString manifest)
    "--git"
    "${pkgs.git}/bin/git"
    "--luac"
    "${pkgs.lua}/bin/luac"
    "--fish"
    "${pkgs.fish}/bin/fish"
  ];
  hasMutable = cfg.mutableFiles != { };
in
{
  options.wahrwelt = {
    mutableFiles = lib.mkOption {
      default = { };
      description = "Writable defaults relative to XDG_CONFIG_HOME, reconciled against retained upstream bytes.";
      type = lib.types.attrsOf (
        lib.types.submodule {
          options = {
            source = lib.mkOption { type = lib.types.path; };
            policy = lib.mkOption {
              type = lib.types.enum [
                "merge"
                "seed"
              ];
              default = "merge";
            };
            validator = lib.mkOption {
              type = lib.types.enum [
                "lua"
                "json"
                "fish"
                "none"
              ];
              default = "none";
            };
          };
        }
      );
    };
    guardedFiles = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "xdg.configFile keys whose local replacements must be checked before relinking.";
    };
    guardedFileBaselines = lib.mkOption {
      type = lib.types.attrsOf (lib.types.listOf lib.types.path);
      default = { };
      description = "Known bootstrap templates accepted as unmodified regular copies of a guarded file.";
    };
  };

  config = {
    assertions = [
      {
        assertion = lib.hasPrefix "${home}/" root && lib.hasPrefix "${home}/" state;
        message = "Wahrwelt dotfile reconciliation requires XDG_CONFIG_HOME and XDG_STATE_HOME inside the user's home.";
      }
      {
        assertion = lib.all (name: lib.hasPrefix configPrefix config.xdg.configFile.${name}.target) guarded;
        message = "Wahrwelt guarded config targets must stay inside XDG_CONFIG_HOME.";
      }
    ];

    home.activation = {
      wahrweltDotfilePreflight = lib.hm.dag.entryBefore [ "checkLinkTargets" "writeBoundary" ] ''
        ${lib.optionalString (guarded != [ ]) ''
          previous_root=()
          if [ -n "''${oldGenPath:-}" ]; then
            previous_root=(--previous-root "$oldGenPath/home-files/"${lib.escapeShellArg (lib.removePrefix "${home}/" root)})
          fi
          ${pkgs.python3}/bin/python3 ${./managed-file-guard.py} \
            --home ${lib.escapeShellArg home} --root ${lib.escapeShellArg root} \
            --manifest ${guardManifest} "''${previous_root[@]}"
        ''}
        ${lib.optionalString hasMutable ''
          ${pkgs.python3}/bin/python3 ${./reconcile.py} check ${reconcileArgs}
        ''}
      '';

      wahrweltDotfileApply = lib.mkIf hasMutable (
        lib.hm.dag.entryAfter [ "linkGeneration" ] ''
          $DRY_RUN_CMD ${pkgs.python3}/bin/python3 ${./reconcile.py} apply ${reconcileArgs}
        ''
      );
    };
  };
}
