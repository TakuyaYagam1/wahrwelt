{
  config,
  homeLibs,
  inputs,
  lib,
  pkgs,
  ...
}:

let
  dotfilesLib = homeLibs.dotfiles;
  shellProfiles = import ./profiles.nix;
  inherit (dotfilesLib) dotsRoot;
  shellSelectorRoot = ./quickshell/wahrwelt-shell-selector;
  shellTransitionRoot = ./quickshell/wahrwelt-shell-transition;
  quickshellPackage = inputs.quickshell.packages.${pkgs.stdenv.hostPlatform.system}.default;
  defaultProfile = shellProfiles.byId.${shellProfiles.defaultProfile};
  end4Profile = shellProfiles.byId.end4;
  end4PCProfile = shellProfiles.byId.end4-pc;
  hyprDir = "${config.xdg.configHome}/hypr";
  hyprRuntimeDir = "${config.xdg.stateHome}/wahrwelt/hypr-runtime";
  stableEntrypoints = dotfilesLib.hyprRuntimeFiles;
  inherit (dotfilesLib) hyprScripts;
  # end4 is installed as one patched tree at hypr/end4. Adding nested
  # xdg.configFile entries under that symlink makes Home Manager reject them as
  # outside $HOME.
  standaloneShellProfiles = lib.filter (profile: profile.family != "end4") shellProfiles.ordered;

  hyprScriptFiles = lib.genAttrs (map (name: "hypr/scripts/${name}") hyprScripts) (target: {
    force = true;
    executable = true;
    source = dotsRoot + "/${target}";
  });

  wahrweltHyprFiles = {
    "hypr/user/hyprland.lua" = {
      source = dotsRoot + "/hypr/hyprland.lua";
    };
    "hypr/lib/wahrwelt.lua" = dotfilesLib.forcedSource (dotsRoot + "/hypr/lib/wahrwelt.lua");
    "hypr/variables.lua" = dotfilesLib.forcedSource (dotsRoot + "/hypr/variables.lua");
    "hypr/scheme/default.lua" = dotfilesLib.forcedSource (dotsRoot + "/hypr/scheme/default.lua");
  }
  // lib.genAttrs (map (name: "hypr/hyprland/${name}.lua") [
    "animations"
    "decoration"
    "env"
    "execs"
    "general"
    "gestures"
    "group"
    "input"
    "keybinds"
    "misc"
    "rules"
    "scrolling"
  ]) (target: dotfilesLib.forcedSource (dotsRoot + "/${target}"))
  // lib.genAttrs (map (target: "hypr/${target}") (
    lib.concatMap (profile: [
      profile.launcher
      profile.keybinds
    ]) standaloneShellProfiles
  )) (target: dotfilesLib.forcedSource (dotsRoot + "/${target}"));

  commonHyprFiles = {
    "hypr/end4-adapter.lua" = {
      force = true;
      source = dotsRoot + "/hypr/end4-adapter.lua";
    };
    "hypr/shell-common-keybinds.lua" = {
      force = true;
      source = dotsRoot + "/hypr/shell-common-keybinds.lua";
    };
    "hypr/shell-common-rules.lua" = {
      force = true;
      source = dotsRoot + "/hypr/shell-common-rules.lua";
    };
    "hypr/shell-workspace-keybinds.lua" = {
      force = true;
      source = dotsRoot + "/hypr/shell-workspace-keybinds.lua";
    };
    "hypr/vm-keybinds.lua" = {
      force = true;
      source = dotsRoot + "/hypr/vm-keybinds.lua";
    };
  };

  selectorShellOptions = homeLibs.shellSelectorOptions.buildOptionsFile shellProfiles.ordered;

  shellSelectorSource = homeLibs.shellSelectorOptions.buildSelectorSource {
    selectorRoot = shellSelectorRoot;
    optionsFile = selectorShellOptions;
  };

  shellTransitionSource =
    pkgs.runCommand "wahrwelt-shell-transition"
      {
        nativeBuildInputs = [
          pkgs.kdePackages.qtdeclarative
          pkgs.kdePackages.qtshadertools
          pkgs.nodejs
        ];
      }
      ''
        node ${shellTransitionRoot}/tests/transition-model.test.mjs
        QML_IMPORT_PATH=${pkgs.kdePackages.qtdeclarative}/lib/qt-6/qml \
          QMLTESTRUNNER=${pkgs.kdePackages.qtdeclarative}/bin/qmltestrunner \
          node ${shellTransitionRoot}/tests/qml-controller.test.mjs
        qmlformat --ignore-settings ${shellTransitionRoot}/shell.qml >/dev/null
        qmlformat --ignore-settings ${shellTransitionRoot}/TransitionController.qml >/dev/null
        qmllint -W 0 --bare \
          -I ${pkgs.kdePackages.qtdeclarative}/lib/qt-6/qml \
          -I ${quickshellPackage}/lib/qt-6/qml \
          -I ${shellTransitionRoot} \
          ${shellTransitionRoot}/shell.qml \
          ${shellTransitionRoot}/TransitionController.qml

        mkdir -p "$out/assets" "$out/shaders"
        install -m 0444 ${shellTransitionRoot}/shell.qml "$out/shell.qml"
        install -m 0444 \
          ${shellTransitionRoot}/TransitionController.qml \
          "$out/TransitionController.qml"
        install -m 0444 ${shellTransitionRoot}/transition-model.js "$out/transition-model.js"
        install -m 0444 \
          ${shellSelectorRoot}/assets/caelestia.svg \
          "$out/assets/caelestia.svg"
        install -m 0444 \
          ${shellSelectorRoot}/assets/illogical-impulse.svg \
          "$out/assets/illogical-impulse.svg"
        install -m 0444 \
          ${shellSelectorRoot}/assets/noctalia.svg \
          "$out/assets/noctalia.svg"
        install -m 0444 \
          ${shellTransitionRoot}/shaders/honeycomb.frag \
          "$out/shaders/honeycomb.frag"
        install -m 0444 \
          ${shellTransitionRoot}/shaders/LICENSE-Noctalia-MIT.txt \
          "$out/shaders/LICENSE-Noctalia-MIT.txt"
        qsb --qt6 \
          -o "$out/shaders/honeycomb.frag.qsb" \
          ${shellTransitionRoot}/shaders/honeycomb.frag
      '';

  stableEntrypointFiles = lib.genAttrs (map (name: "hypr/${name}") stableEntrypoints) (
    target:
    let
      name = lib.removePrefix "hypr/" target;
    in
    if lib.hasSuffix ".lua" name then
      dotfilesLib.stableLuaRuntimeSourceFile "${hyprRuntimeDir}/${name}"
    else
      dotfilesLib.stableRuntimeSourceFile "${hyprRuntimeDir}/${name}"
  );

  runtimeActivation = pkgs.writeShellApplication {
    name = "wahrwelt-runtime-activation";
    runtimeInputs = [
      pkgs.bash
      pkgs.coreutils
      pkgs.python3
      pkgs.util-linux
    ];
    text = builtins.readFile ./runtime-activation.sh;
  };

  wahrweltFsHelper = inputs.self.packages.${pkgs.stdenv.hostPlatform.system}.wahrwelt-fs-helper;

  liveShellBootstrapPath = builtins.concatStringsSep ":" [
    "${pkgs.bash}/bin"
    "${pkgs.coreutils}/bin"
    "${pkgs.dbus}/bin"
    "${pkgs.diffutils}/bin"
    "${pkgs.python3}/bin"
    "${pkgs.systemd}/bin"
    "${pkgs.util-linux}/bin"
    "${wahrweltFsHelper}/bin"
  ];

  canonicalHyprRuntime = pkgs.writeText "wahrwelt-hypr-runtime-default" ''
    -- Wahrwelt canonical Hyprland runtime entrypoint
    local home = os.getenv("HOME")
    if home == nil then
        error("HOME is not set; cannot locate Wahrwelt Hyprland config")
    end

    local config_home = os.getenv("XDG_CONFIG_HOME") or (home .. "/.config")
    local hypr_root = config_home .. "/hypr"
    package.path = hypr_root .. "/?.lua;" .. hypr_root .. "/?/init.lua;" .. package.path
    dofile(hypr_root .. "/user/hyprland.lua")
  '';

  defaultHyprUserConfig = pkgs.writeText "wahrwelt-hypr-default" ''
    local wahrwelt = require("lib.wahrwelt")

    wahrwelt.optional_require("wahrwelt.execs")
    wahrwelt.optional_require("wahrwelt.general")
    wahrwelt.optional_require("wahrwelt.rules")
    wahrwelt.optional_require("wahrwelt.keybinds")
  '';

  defaultShellProfileRuntime = pkgs.writeText "wahrwelt-shell-profile-default" ''
    -- Runtime shell launcher
    hl.on("hyprland.start", function()
        hl.exec_cmd("${hyprDir}/scripts/start-shell.sh")
    end)
  '';

  defaultHyprlockRuntime = pkgs.writeText "wahrwelt-empty-hyprlock" ''
    # Active Hyprlock profile: shell-managed
    # Caelestia and Noctalia use shell-native lock flows.
  '';
  defaultHypridleRuntime = pkgs.writeText "wahrwelt-empty-hypridle" ''
    # Active Hypridle profile: shell-managed
    # Caelestia and Noctalia use shell-native idle flows.
  '';

  defaultShellLauncherRuntime = pkgs.writeText "wahrwelt-shell-launcher-default" ''
    -- Active shell launcher profile: ${defaultProfile.id}
    require("${defaultProfile.id}.launcher")
  '';

  defaultShellKeybindRuntime = pkgs.writeText "wahrwelt-shell-keybinds-default" ''
    -- Wahrwelt shell adapter: ${defaultProfile.id}
    require("${defaultProfile.adapter}")
  '';

  end4RuntimeContract = pkgs.writeText "end4-runtime-contract" "end4-adapter-v1\n";

  mkEnd4RuntimeBundle = profile: {
    profile = defaultShellProfileRuntime;
    lock = pkgs.writeText "wahrwelt-hyprlock-${profile.id}" ''
      # Active Hyprlock profile: ${profile.id}
      source = ${hyprDir}/end4/hyprlock.conf
    '';
    idle = pkgs.writeText "wahrwelt-hypridle-${profile.id}" ''
      # Active Hypridle profile: ${profile.id}
      source = ${hyprDir}/end4/hypridle.conf
    '';
    launcher = pkgs.writeText "wahrwelt-shell-launcher-${profile.id}" ''
      -- Active shell launcher profile: ${profile.id}
      require("end4.launcher")
    '';
    keybinds = pkgs.writeText "wahrwelt-shell-keybinds-${profile.id}" ''
      -- Wahrwelt shell adapter: ${profile.id}
      require("${profile.adapter}").load({ profile = "${profile.id}", quickshell_config = "${config.xdg.configHome}/quickshell/${profile.quickshellConfig}" })
    '';
    legacyLauncher = pkgs.writeText "wahrwelt-shell-launcher-legacy-${profile.id}" ''
      -- Active shell launcher profile: ${profile.id}
      -- end4 registers launcher bindings from its own Hyprland Lua modules.
    '';
    legacyKeybinds = pkgs.writeText "wahrwelt-shell-keybinds-legacy-${profile.id}" ''
      -- Active shell keybind profile: ${profile.id}
      -- end4 registers keybinds from its own Hyprland Lua modules.
    '';
  };

  end4Runtime = mkEnd4RuntimeBundle end4Profile;
  end4PCRuntime = mkEnd4RuntimeBundle end4PCProfile;

in
{
  imports = [ ../lib/managed-config.nix ];

  # The runtime adapter has its own migration/ownership check. Static defaults
  # must also refuse edited replacements before Home Manager's forced links.
  wahrwelt.guardedFiles = lib.filter (name: name != "hypr/user/hyprland.lua") (
    builtins.attrNames (wahrweltHyprFiles // commonHyprFiles // hyprScriptFiles)
  );

  xdg.configFile =
    hyprScriptFiles
    // stableEntrypointFiles
    // wahrweltHyprFiles
    // commonHyprFiles
    // {
      "quickshell/wahrwelt-shell-selector" = {
        force = true;
        source = shellSelectorSource;
      };
      "quickshell/wahrwelt-shell-transition" = {
        force = true;
        source = shellTransitionSource;
      };
    };

  home = {
    packages = [
      pkgs.python3
      wahrweltFsHelper
    ];

    sessionVariables.WAHRWELT_FS_HELPER = "${wahrweltFsHelper}/bin/wahrwelt-fs-helper";

    activation = {
      prepareWahrweltHyprDirectory =
        lib.hm.dag.entryBetween [ "checkLinkTargets" ] [ "migrateWahrweltUserPaths" ]
          ''
            user_dir="${hyprDir}/user"
            $DRY_RUN_CMD "${runtimeActivation}/bin/wahrwelt-runtime-activation" \
              activate-user-dir \
              "$user_dir" \
              "${dotsRoot}/hypr/hyprland.lua" \
              "''${oldGenPath:-}" \
              "${defaultHyprUserConfig}"
          '';

      seedHyprShellRuntime =
        lib.hm.dag.entryAfter
          [
            "caelestiaSeedShellJson"
            "noctaliaSeedConfig"
            "end4SeedConfig"
            "end4SeedAppConfig"
            "linkGeneration"
          ]
          ''
            runtime_dir="${hyprRuntimeDir}"
            activation_helper="${runtimeActivation}/bin/wahrwelt-runtime-activation"
            hypr_runtime_source="${canonicalHyprRuntime}"
            wahrwelt_direct_end4_process_upgrade=""
            wahrwelt_direct_end4_process_runtime_hex=""
            wahrwelt_direct_end4_process_runtime_id=""

            wahrwelt_v1_to_v2_direct_end4_evidence() {
              local candidate source

              for candidate in "$runtime_dir/hyprland.lua" "${hyprDir}/hyprland.lua"; do
                [ -e "$candidate" ] || continue
                for source in \
                  "${../migrations/v1_to_v2/hypr-runtime/end4.lua}" \
                  "${../migrations/v1_to_v2/hypr-runtime/end4-pc.lua}"
                do
                  if ${pkgs.diffutils}/bin/cmp -s -- "$candidate" "$source"; then
                    return 0
                  fi
                done
              done
              if [ -n "''${XDG_RUNTIME_DIR:-}" ] &&
                { [ -e "''${XDG_RUNTIME_DIR}/wahrwelt-end4-upgrade" ] ||
                  [ -L "''${XDG_RUNTIME_DIR}/wahrwelt-end4-upgrade" ]; }; then
                return 0
              fi
              return 1
            }

            direct_end4_bundle_args=(
              "$runtime_dir" \
              "${hyprDir}/hyprland.lua" \
              "${config.xdg.stateHome}/wahrwelt/end4-variant" \
              "$hypr_runtime_source" \
              "${../migrations/v1_to_v2/hypr-runtime/end4.lua}" \
              "${../migrations/v1_to_v2/hypr-runtime/end4-pc.lua}" \
              "${end4Runtime.profile}" \
              "${end4Runtime.lock}" \
              "${end4Runtime.idle}" \
              "${end4Runtime.launcher}" \
              "${end4Runtime.keybinds}" \
              "${end4Runtime.legacyLauncher}" \
              "${end4Runtime.legacyKeybinds}" \
              "${end4PCRuntime.profile}" \
              "${end4PCRuntime.lock}" \
              "${end4PCRuntime.idle}" \
              "${end4PCRuntime.launcher}" \
              "${end4PCRuntime.keybinds}" \
              "${end4PCRuntime.legacyLauncher}" \
              "${end4PCRuntime.legacyKeybinds}" \
              "${dotsRoot}/hypr/hyprland.lua" \
              "${dotsRoot}/hypr/end4-adapter.lua" \
              "${dotsRoot}/hypr/end4/launcher.lua" \
              "${end4RuntimeContract}" \
              "${hyprDir}/user/hyprland.lua" \
              "${hyprDir}/end4-adapter.lua" \
              "${hyprDir}/end4/launcher.lua" \
              "${hyprDir}/end4/hyprland.lua" \
              "${hyprDir}/end4/.wahrwelt-runtime-contract" \
              "${config.xdg.configHome}/quickshell/${end4Profile.quickshellConfig}/shell.qml" \
              "${config.xdg.configHome}/quickshell/${end4PCProfile.quickshellConfig}/shell.qml" \
              "${dotsRoot}/hypr/scripts/start-shell.sh"
            )

            if wahrwelt_v1_to_v2_direct_end4_evidence && [ -n "''${DRY_RUN_CMD:-}" ]; then
              $DRY_RUN_CMD "$activation_helper" migrate-direct-end4-bundle \
                "''${direct_end4_bundle_args[@]}"
            elif wahrwelt_v1_to_v2_direct_end4_evidence; then
              migration_result="$(
                "$activation_helper" migrate-direct-end4-bundle \
                  "''${direct_end4_bundle_args[@]}"
              )"
              case "$migration_result" in
                current | legacy-upgrade=)
                  ;;
                legacy-upgrade=*)
                  migration_payload="''${migration_result#legacy-upgrade=}"
                  case "$migration_payload" in
                    *';runtime-hex='*';runtime-id='*)
                      wahrwelt_direct_end4_process_upgrade="''${migration_payload%%;runtime-hex=*}"
                      migration_runtime="''${migration_payload#*;runtime-hex=}"
                      wahrwelt_direct_end4_process_runtime_hex="''${migration_runtime%%;runtime-id=*}"
                      wahrwelt_direct_end4_process_runtime_id="''${migration_runtime#*;runtime-id=}"
                      ;;
                    *)
                      echo "Direct End4 upgrade result has no runtime proof" >&2
                      exit 1
                      ;;
                  esac
                  ;;
                resume-upgrade-runtime-hex=*)
                  migration_runtime="''${migration_result#resume-upgrade-runtime-hex=}"
                  case "$migration_runtime" in
                    *';runtime-id='*)
                      wahrwelt_direct_end4_process_runtime_hex="''${migration_runtime%%;runtime-id=*}"
                      wahrwelt_direct_end4_process_runtime_id="''${migration_runtime#*;runtime-id=}"
                      ;;
                    *)
                      echo "Direct End4 resume result has no runtime identity" >&2
                      exit 1
                      ;;
                  esac
                  ;;
                *)
                  echo "Invalid direct End4 runtime migration result" >&2
                  exit 1
                  ;;
              esac
              case "$wahrwelt_direct_end4_process_runtime_hex" in
                "")
                  if [ -n "$wahrwelt_direct_end4_process_runtime_id" ] || \
                    [ -n "$wahrwelt_direct_end4_process_upgrade" ]; then
                    echo "Incomplete direct End4 runtime proof" >&2
                    exit 1
                  fi
                  ;;
                *[!0-9a-f]*)
                  echo "Invalid direct End4 runtime proof" >&2
                  exit 1
                  ;;
                *)
                  if [ "$(( ''${#wahrwelt_direct_end4_process_runtime_hex} % 2 ))" -ne 0 ]; then
                    echo "Invalid direct End4 runtime proof length" >&2
                    exit 1
                  fi
                  ;;
              esac
              if [ -n "$wahrwelt_direct_end4_process_runtime_hex" ]; then
                runtime_dev=""
                runtime_ino=""
                runtime_uid=""
                runtime_mode=""
                runtime_extra=""
                IFS=: read -r runtime_dev runtime_ino runtime_uid runtime_mode runtime_extra \
                  <<<"$wahrwelt_direct_end4_process_runtime_id"
                case "$runtime_dev:$runtime_ino:$runtime_uid:$runtime_mode" in
                  *[!0-9:]* | *::*)
                    echo "Invalid direct End4 runtime identity" >&2
                    exit 1
                    ;;
                esac
                if [ -z "$runtime_dev" ] || [ -z "$runtime_ino" ] || \
                  [ -z "$runtime_uid" ] || [ -z "$runtime_mode" ] || \
                  [ -n "$runtime_extra" ] || [ "$runtime_mode" != 700 ]; then
                  echo "Invalid direct End4 runtime identity shape" >&2
                  exit 1
                fi
              fi
            fi

            $DRY_RUN_CMD "$activation_helper" activate-runtime-dir \
              "$runtime_dir" \
              "$hypr_runtime_source" \
              "$hypr_runtime_source" \
              "${defaultShellProfileRuntime}" \
              "${defaultHyprlockRuntime}" \
              "${defaultHypridleRuntime}" \
              "${defaultShellLauncherRuntime}" \
              "${defaultShellKeybindRuntime}"
          '';

      liveSyncHyprShell =
        lib.hm.dag.entryAfter
          [
            "seedHyprShellRuntime"
            "linkGeneration"
          ]
          ''
            activation_helper="${runtimeActivation}/bin/wahrwelt-runtime-activation"
            hyprctl_path="${pkgs.hyprland}/bin/hyprctl"
            run_live_shell_command() {
              if [ -n "''${wahrwelt_direct_end4_process_runtime_hex:-}" ]; then
                $DRY_RUN_CMD "$activation_helper" run-with-runtime-hex \
                  "$wahrwelt_direct_end4_process_runtime_hex" \
                  "$wahrwelt_direct_end4_process_runtime_id" \
                  "$@"
              else
                $DRY_RUN_CMD "$@"
              fi
            }
            select_live_hypr_signature() {
              ${pkgs.jq}/bin/jq -er \
                --arg existing "''${HYPRLAND_INSTANCE_SIGNATURE:-}" \
                --arg socket "''${WAYLAND_DISPLAY:-}" '
                  if type != "array" then empty
                  else
                    . as $instances
                    | [$instances[]?
                        | select((.instance | type) == "string" and (.instance | length) > 0)] as $valid
                    | [$valid[] | select($existing != "" and .instance == $existing) | .instance]
                      | unique as $by_existing
                    | [$valid[] | select($socket != "" and .wl_socket == $socket) | .instance]
                      | unique as $by_socket
                    | [$valid[] | .instance] | unique as $all
                    | if ($by_existing | length) == 1 and ($by_socket | length) == 1 then
                        if $by_existing[0] == $by_socket[0] then $by_existing[0] else empty end
                      elif ($by_existing | length) == 1 then $by_existing[0]
                      elif ($by_socket | length) == 1 then $by_socket[0]
                      elif ($all | length) == 1 then $all[0]
                      else empty
                      end
                  end
                '
            }
            run_live_hypr_command() {
              run_live_shell_command "$hyprctl_path" -i "$hypr_instance_signature" "$@"
            }
            if [ -n "$hyprctl_path" ] && [ "''${hyprctl_path#/}" != "$hyprctl_path" ] && \
              [ -x "$hyprctl_path" ] && \
              hypr_instances="$(run_live_shell_command "$hyprctl_path" -j instances 2>/dev/null)"; then
              if ! hypr_instance_signature="$(
                printf '%s\n' "$hypr_instances" | select_live_hypr_signature
              )"; then
                hypr_instance_signature=""
              fi
              case "$hypr_instance_signature" in
                "" | *[!A-Za-z0-9_.-]*)
                  echo "Skipping live Hyprland reload; no unique active instance was found." >&2
                  hypr_instance_signature=""
                  ;;
              esac
            else
              hypr_instance_signature=""
            fi
            live_session_env=(
              "${pkgs.coreutils}/bin/env"
              "HYPRLAND_INSTANCE_SIGNATURE=$hypr_instance_signature"
              "PATH=${liveShellBootstrapPath}:$PATH"
              "WAHRWELT_FS_HELPER=${wahrweltFsHelper}/bin/wahrwelt-fs-helper"
            )
            if [ -n "$hypr_instance_signature" ] && \
              [ -z "''${wahrwelt_direct_end4_process_runtime_hex:-}" ]; then
              live_xdg_runtime_dir="''${XDG_RUNTIME_DIR:-/run/user/$UID}"
              live_dbus_address="''${DBUS_SESSION_BUS_ADDRESS:-unix:path=$live_xdg_runtime_dir/bus}"
              if [ "''${live_xdg_runtime_dir#/}" = "$live_xdg_runtime_dir" ]; then
                echo "Skipping live Hyprland reload; the user runtime directory is unavailable." >&2
                hypr_instance_signature=""
              else
                live_session_env+=(
                  "XDG_RUNTIME_DIR=$live_xdg_runtime_dir"
                  "DBUS_SESSION_BUS_ADDRESS=$live_dbus_address"
                )
              fi
            fi
            if [ -n "$hypr_instance_signature" ]; then
              if ! hypr_version="$(
                run_live_hypr_command version 2>/dev/null \
                  | ${pkgs.gawk}/bin/awk 'NR == 1 { print $2 }'
              )"; then
                echo "WARN Failed to query the selected Hyprland instance; persistent configuration is installed. Logout or reboot to activate it." >&2
              else
                hypr_version="''${hypr_version#v}"
                hypr_major="''${hypr_version%%.*}"
                hypr_rest="''${hypr_version#*.}"
                hypr_minor="''${hypr_rest%%.*}"

                case "$hypr_major" in
                  "" | *[!0-9]*)
                    hypr_major=0
                    ;;
                esac
                case "$hypr_minor" in
                  "" | *[!0-9]*)
                    hypr_minor=0
                    ;;
                esac

                if [ "$hypr_major" -gt 0 ] || [ "$hypr_minor" -ge 55 ]; then
                  if ! run_live_hypr_command reload; then
                    echo "WARN Failed to reload the active Hyprland configuration; persistent configuration is installed. Logout or reboot to activate it." >&2
                  else
                    if run_live_shell_command \
                      "''${live_session_env[@]}" \
                      "${pkgs.util-linux}/bin/setsid" \
                      "${pkgs.systemd}/bin/systemd-run" \
                      --user \
                      --scope \
                      --collect \
                      --quiet \
                      -- \
                      "${config.xdg.configHome}/hypr/scripts/start-shell.sh"; then
                      start_shell_status=0
                    else
                      start_shell_status=$?
                    fi
                    if [ "$start_shell_status" -ne 0 ]; then
                      echo "WARN Failed to start the configured Wahrwelt shell profile (status $start_shell_status); persistent configuration is installed. Retry the shell switch or logout/reboot." >&2
                    else
                      wahrwelt_direct_end4_process_upgrade=""
                      wahrwelt_direct_end4_process_runtime_hex=""
                      wahrwelt_direct_end4_process_runtime_id=""
                    fi
                  fi
                else
                  echo "Skipping live Hyprland reload; running Hyprland $hypr_version cannot load Lua runtime. Logout or reboot after switch." >&2
                fi
              fi
            fi
          '';
    };
  };
}
