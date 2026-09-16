{
  config,
  end4Lib,
  inputs,
  lib,
  pkgs,
  ...
}:

let
  dotfilesSource = inputs.end4-dotfiles;
  inherit (end4Lib) dotfilesLib runtimeEnv settings;
  wahrweltHyprSource = dotfilesLib.dotsRoot + "/hypr/end4";
  end4WindowOpacity = settings.window.opacity;
  luaString = value: builtins.toJSON (toString value);
  luaEnvLines = lib.concatStringsSep "\n" (
    lib.mapAttrsToList (
      name: value: "hl.env(${luaString name}, ${luaString value})"
    ) runtimeEnv.hyprVariables
  );
  isAutoMonitor =
    monitor: monitor.mode == "preferred" || monitor.mode == "auto" || monitor.mode == "";
  renderLuaMonitor =
    monitor:
    let
      auto = isAutoMonitor monitor;
    in
    ''
      hl.monitor({
          output = ${luaString (if auto then "" else monitor.name)},
          mode = ${luaString (if monitor.mode == "" then "preferred" else monitor.mode)},
          position = ${luaString (if auto then "auto" else monitor.position)},
          scale = ${luaString monitor.scale}
      })
    '';
  luaMonitorLines = lib.concatMapStringsSep "\n" renderLuaMonitor settings.monitor.definitions;

  hyprEnvPrelude = pkgs.writeText "end4-hypr-env.lua" ''
    ${luaEnvLines}
  '';

  hypridleConf = pkgs.writeText "end4-hypridle.conf" ''
    $lock_cmd = ${lib.escapeShellArg "${config.xdg.configHome}/hypr/scripts/lock-active.sh"}
    $hibernate_cmd = systemctl hibernate || loginctl hibernate

    general {
        lock_cmd = $lock_cmd
        before_sleep_cmd = $lock_cmd
        after_sleep_cmd = hyprctl dispatch global quickshell:lockFocus
        inhibit_sleep = 3
    }

    listener {
        timeout = ${toString settings.idle.lockTimeout}
        on-timeout = $lock_cmd
    }

    listener {
        timeout = ${toString settings.idle.screenOffTimeout}
        on-timeout = hyprctl dispatch dpms off
        on-resume = hyprctl dispatch dpms on
    }

    listener {
        timeout = ${toString settings.idle.hibernateTimeout}
        on-timeout = $hibernate_cmd
    }
  '';

  customGeneralLua = pkgs.writeText "end4-custom-general.lua" ''
    ${luaMonitorLines}

    hl.config({
        general = {
            border_size = 3,
            col = {
                active_border = "rgba(c2c1ffe6)",
                inactive_border = "rgba(c8c5d111)"
            },
            gaps_in = 10,
            gaps_out = 40
        },
        misc = {
            vrr = 2
        },
        render = {
            direct_scanout = 1
        },
        cursor = {
            no_hardware_cursors = false
        }
    })

    hl.curve("wahrweltStandard", {
        type = "bezier",
        points = { { 0.2, 0 }, { 0, 1 } }
    })
    hl.animation({
        leaf = "workspaces",
        enabled = true,
        speed = 5,
        bezier = "wahrweltStandard",
        style = "slide"
    })
  '';

  customRulesLua = pkgs.writeText "end4-custom-rules.lua" ''
    hl.window_rule({ match = { fullscreen = 0 }, opacity = "${end4WindowOpacity} override" })
    hl.window_rule({ match = { class = ".*" }, no_blur = false })
    hl.layer_rule({ match = { namespace = "quickshell:.*" }, ignore_alpha = 0.79 })
  '';

  customKeybindsLua = pkgs.writeText "end4-custom-keybinds.lua" ''
    local home = os.getenv("HOME")
    local config_home = os.getenv("XDG_CONFIG_HOME") or (home .. "/.config")
    dofile(config_home .. "/hypr/end4/wahrwelt/keybinds.lua")
  '';

  upstreamSettingsApp = pkgs.writeText "end4-upstream-settings-app.lua" ''
    settingsApp = "XDG_CURRENT_DESKTOP=gnome ~/.config/hypr/hyprland/scripts/launch_first_available.sh 'qs -p ~/.config/quickshell/$qsConfig/settings.qml' 'systemsettings' 'gnome-control-center' 'better-control'"
  '';

  managedSettingsApp = pkgs.writeText "end4-managed-settings-app.lua" ''
    settingsApp = "wahrwelt-end4-settings"
  '';

  upstreamLockBinding = pkgs.writeText "end4-upstream-lock-binding.lua" ''
    hl.bind("SUPER + L", hl.dsp.exec_cmd("loginctl lock-session"), { description = "Session: Lock" })
  '';

  managedLockBinding = pkgs.writeText "end4-managed-lock-binding.lua" ''
    hl.bind("SUPER + L", hl.dsp.exec_cmd(${luaString "${config.xdg.configHome}/hypr/scripts/lock-active.sh"}), { description = "Session: Lock" })
  '';

  end4SettingsLauncher = pkgs.writeShellApplication {
    name = "wahrwelt-end4-settings";
    runtimeInputs = [ pkgs.coreutils ];
    text = ''
      config_home="''${XDG_CONFIG_HOME:-$HOME/.config}"
      state_home="''${XDG_STATE_HOME:-$HOME/.local/state}"
      active_profile_file="$state_home/wahrwelt/active-shell"
      official_config="$config_home/quickshell/ii"
      pc_config="$config_home/quickshell/end4-pC"

      if [ -L "$active_profile_file" ] || [ ! -f "$active_profile_file" ]; then
        printf 'refusing missing or unsafe active shell state: %s\n' "$active_profile_file" >&2
        exit 64
      fi
      active_profile="$(tr -d '[:space:]' < "$active_profile_file")"

      case "$active_profile" in
        end4)
          export WAHRWELT_END4_PROFILE=end4
          export WAHRWELT_QS_CONFIG="$official_config"
          export qsConfig="$official_config"
          exec qs-end4 -n -d -p "$official_config/settings.qml"
          ;;
        end4-pc)
          export WAHRWELT_END4_PROFILE=end4-pc
          export WAHRWELT_QS_CONFIG="$pc_config"
          export qsConfig="$pc_config"
          exec qs-end4 -c end4-pC ipc call settings open
          ;;
        *)
          printf 'refusing non-End4 active shell profile: %s\n' "''${active_profile:-unset}" >&2
          exit 64
          ;;
      esac
    '';
  };

  runtimeContract = pkgs.writeText "end4-runtime-contract" "end4-adapter-v1\n";

  end4OwnershipGuard = pkgs.writeShellApplication {
    name = "wahrwelt-end4-ownership-guard";
    runtimeInputs = [ pkgs.coreutils ];
    text = builtins.readFile ./end4-ownership-guard.sh;
  };

  patchedHypr =
    pkgs.runCommand "end4-hypr-patched"
      {
        buildInputs = [ pkgs.bash ];
      }
      ''
        source ${./hypr-helpers.sh}

        cp -r ${dotfilesSource}/dots/.config/hypr "$out"
        chmod -R +w "$out"
        find "$out" -type f -name '*.sh' -exec chmod +x {} +
        mkdir -p "$out/wahrwelt"
        install -m 0644 ${wahrweltHyprSource}/keybinds.lua "$out/wahrwelt/keybinds.lua"
        install -m 0644 ${wahrweltHyprSource}/launcher.lua "$out/launcher.lua"

        install -m 0644 ${hyprEnvPrelude} "$out/hyprland/env.lua"
        cat ${dotfilesSource}/dots/.config/hypr/hyprland/env.lua >> "$out/hyprland/env.lua"
        optional_patch_line "$out/hyprland/env.lua" \
          'local xdg_data_dirs_old = os.getenv("XDG_DATA_DIRS") or ""' \
          '/^local xdg_data_dirs_old = os.getenv("XDG_DATA_DIRS") or ""$/d'
        if ! grep -Eq '^hl\.env\("XDG_DATA_DIRS",' "$out/hyprland/env.lua"; then
          echo "missing XDG_DATA_DIRS patch target (NixOS session environment owns XDG_DATA_DIRS): $out/hyprland/env.lua" >&2
          exit 1
        fi
        sed -i '/^hl\.env("XDG_DATA_DIRS",/d' "$out/hyprland/env.lua"

        strict_patch_line "$out/hyprland/execs.lua" \
          '    hl.exec_cmd("qs -c $qsConfig")' \
          '/^    hl\.exec_cmd("qs -c \$qsConfig")$/d' \
          'Wahrwelt start-shell owns end4 QuickShell lifecycle'
        strict_patch_line "$out/hyprland/execs.lua" \
          '    hl.exec_cmd("hypridle")' \
          '/^    hl\.exec_cmd("hypridle")$/d' \
          'Wahrwelt start-shell owns end4 hypridle lifecycle'
        strict_patch_two_lines "$out/hyprland/keybinds.lua" \
          'hl.bind("CTRL + SUPER + R", hl.dsp.exec_cmd("killall ydotool qs quickshell; qs -c $qsConfig &"),' \
          '    { description = "Shell: Restart widgets" })' \
          'Wahrwelt start-shell owns end4 QuickShell restart lifecycle'
        strict_patch_line "$out/hyprland/keybinds.lua" \
          'hl.bind("SHIFT + SUPER + ALT + Slash", hl.dsp.exec_cmd("qs -p $HOME/.config/quickshell/$qsConfig/welcome.qml"))' \
          '/^hl\.bind("SHIFT + SUPER + ALT + Slash", hl\.dsp\.exec_cmd("qs -p \$HOME\/.config\/quickshell\/\$qsConfig\/welcome\.qml"))$/d' \
          'Wahrwelt start-shell owns end4 QuickShell welcome lifecycle'
        strict_replace_line_from_files "$out/hyprland/variables.lua" \
          ${upstreamSettingsApp} \
          ${managedSettingsApp} \
          'Wahrwelt owns End4 native settings lifecycle'
        strict_replace_line_from_files "$out/hyprland/keybinds.lua" \
          ${upstreamLockBinding} \
          ${managedLockBinding} \
          'Wahrwelt owns End4 native lock routing'

        install -m 0555 \
          ${end4SettingsLauncher}/bin/wahrwelt-end4-settings \
          "$out/wahrwelt/settings"

        install -m 0644 ${hypridleConf} "$out/hypridle.conf"
        install -m 0644 ${customGeneralLua} "$out/custom/general.lua"
        install -m 0644 ${customRulesLua} "$out/custom/rules.lua"
        install -m 0644 ${customKeybindsLua} "$out/custom/keybinds.lua"

        # Keep the complete upstream tree while moving its absolute config
        # references beneath the managed End4 namespace.
        find "$out" -type f -exec sed -i \
          -e 's|~/.config/hypr/hyprland/|~/.config/hypr/end4/hyprland/|g' \
          -e 's|~/.config/hypr/custom/|~/.config/hypr/end4/custom/|g' \
          -e 's|$HOME/.config/hypr/hyprland/|$HOME/.config/hypr/end4/hyprland/|g' \
          -e 's|$HOME/.config/hypr/custom/|$HOME/.config/hypr/end4/custom/|g' \
          -e 's|HOME \.\. "/.config/hypr/custom/|HOME .. "/.config/hypr/end4/custom/|g' \
          -e 's|~/.config/hypr/hyprlock/|~/.config/hypr/end4/hyprlock/|g' \
          -e 's|''${XDG_CONFIG_HOME:-$HOME/.config}/hypr/hyprlock/|''${XDG_CONFIG_HOME:-$HOME/.config}/hypr/end4/hyprlock/|g' \
          {} +

        patchShebangs "$out"
      '';

  validatedHypr =
    pkgs.runCommand "end4-hypr-validated"
      {
        nativeBuildInputs = [
          pkgs.lua
          pkgs.bash
          pkgs.diffutils
          pkgs.python3
        ];
      }
      ''
        cp -r ${patchedHypr} "$out"
        chmod -R u+w "$out"

        for file in \
          "$out/hyprland.lua" \
          "$out/hyprland/execs.lua" \
          "$out/custom/execs.lua" \
          "$out/custom/general.lua" \
          "$out/custom/rules.lua" \
          "$out/custom/keybinds.lua" \
          "$out/wahrwelt/keybinds.lua" \
          "$out/hypridle.conf"
        do
          if [ ! -f "$file" ]; then
            echo "validated End4 artifact is missing $file" >&2
            exit 1
          fi
        done

        if grep -Fqx '    hl.exec_cmd("qs -c $qsConfig")' "$out/hyprland/execs.lua"; then
          echo "validated End4 artifact retained upstream QuickShell startup" >&2
          exit 1
        fi
        if grep -Fqx '    hl.exec_cmd("hypridle")' "$out/hyprland/execs.lua"; then
          echo "validated End4 artifact retained upstream hypridle startup" >&2
          exit 1
        fi
        if grep -Fq 'start-shell.sh' "$out/custom/execs.lua"; then
          echo "validated End4 artifact contains a second shell startup handler" >&2
          exit 1
        fi

        for module in execs general rules keybinds; do
          if ! grep -Fq "require(\"custom.$module\")" "$out/hyprland.lua"; then
            echo "validated End4 artifact does not load custom.$module" >&2
            exit 1
          fi
        done
        for module in workspaces monitors hyprland.shellOverrides.main; do
          if ! grep -Fq "require(\"$module\")" "$out/hyprland.lua"; then
            echo "validated End4 artifact lost upstream $module integration" >&2
            exit 1
          fi
        done

        if ! grep -Fq '/hypr/end4/custom/' "$out/hyprland.lua"; then
          echo "validated End4 artifact is missing custom namespace rewrites" >&2
          exit 1
        fi
        if ! grep -Fq '/hypr/end4/hyprland/' "$out/hyprland/variables.lua"; then
          echo "validated End4 artifact is missing hyprland namespace rewrites" >&2
          exit 1
        fi
        if ! grep -Fq '/scripts/close-active.sh' "$out/wahrwelt/keybinds.lua"; then
          echo "validated End4 artifact is missing app-aware close overlay" >&2
          exit 1
        fi
        if grep -R -Fq 'shell-common-rules.lua' "$out"; then
          echo "validated End4 artifact contains a direct common-rules hook" >&2
          exit 1
        fi
        if ! grep -Fq '/hypr/scripts/lock-active.sh' "$out/hypridle.conf"; then
          echo "validated End4 artifact is missing the managed lock helper" >&2
          exit 1
        fi
        if ! grep -Fq '/hypr/scripts/lock-active.sh' "$out/hyprland/keybinds.lua"; then
          echo "validated End4 artifact is missing the managed lock keybinding" >&2
          exit 1
        fi
        if grep -Fq 'loginctl lock-session' "$out/hypridle.conf" || \
          grep -Fq 'loginctl lock-session' "$out/hyprland/keybinds.lua"; then
          echo "validated End4 artifact can enter logind before the native lock dispatcher" >&2
          exit 1
        fi
        if grep -Eq 'pidof[[:space:]]+(qs|quickshell|hyprlock)' "$out/hypridle.conf"; then
          echo "validated End4 artifact retained generic process-name lock fallback" >&2
          exit 1
        fi

        for file in \
          "$out/hyprland/env.lua" \
          "$out/custom/general.lua" \
          "$out/custom/rules.lua" \
          "$out/custom/keybinds.lua" \
          "$out/hypridle.conf"
        do
          if grep -Eq '(^|[^[:alnum:]_])EOF([^[:alnum:]_]|$)|patchShebangs \$out|find \$out' "$file"; then
            echo "validated End4 payload swallowed build-script content: $file" >&2
            exit 1
          fi
        done

        ${pkgs.bash}/bin/bash \
          ${dotfilesLib.dotsRoot}/hypr/scripts/tests/end4-artifact-test.sh \
          "$out"
        ${pkgs.lua}/bin/lua \
          ${dotfilesLib.dotsRoot}/hypr/scripts/tests/end4-close-bind-test.lua \
          ${dotfilesLib.dotsRoot}/hypr \
          "$out/wahrwelt/keybinds.lua"
        if grep -R -E \
          --include='*.lua' \
          'hl\.exec_cmd\([^)]*(^|[[:space:]";&|/])hypridle([[:space:]";&|]|$)' \
          "$out"
        then
          echo "validated End4 artifact contains direct hypridle lifecycle startup" >&2
          exit 1
        fi

        find "$out" -type f -name '*.lua' -print0 | xargs -0 -n1 luac -p
        ${pkgs.coreutils}/bin/install -m 0444 \
          ${runtimeContract} "$out/.wahrwelt-runtime-contract"
        ${pkgs.diffutils}/bin/cmp -s \
          ${runtimeContract} "$out/.wahrwelt-runtime-contract"
      '';
in
{
  home.packages = [ end4SettingsLauncher ];

  xdg.configFile."hypr/end4" = {
    source = validatedHypr;
  };

  home.activation.guardWahrweltEnd4Ownership = lib.hm.dag.entryBefore [ "checkLinkTargets" ] ''
    ${end4OwnershipGuard}/bin/wahrwelt-end4-ownership-guard \
      "${config.xdg.configHome}/hypr/end4" \
      "${config.home.homeDirectory}/.local/state/home-manager/gcroots/current-home/home-files/.config/hypr/end4"
  '';
}
