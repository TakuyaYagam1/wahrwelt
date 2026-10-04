#!/usr/bin/env bash
set -euo pipefail

tests_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
verifier="$tests_dir/end4-artifact-test.sh"
repo_root="$(CDPATH='' cd -- "$tests_dir/../../../../.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

make_fixture() {
  local name="$1"
  local payload="$2"
  local fixture="$test_root/$name"

  mkdir -p "$fixture/hyprland" "$fixture/wahrwelt"
  printf '%s\n' '-- fixture' >"$fixture/hyprland.lua"
  printf '%s\n' \
    'hl.dsp.global("quickshell:searchToggleRelease")' \
    'hl.dsp.global("quickshell:panelFamilyCycle")' \
    'hl.dsp.global("quickshell:sidebarRightToggle")' \
    'hl.bind("SUPER + L", hl.dsp.exec_cmd("/home/user/.config/hypr/scripts/lock-active.sh"))' \
    "$payload" >"$fixture/hyprland/keybinds.lua"
  printf '%s\n' 'run /scripts/close-active.sh' >"$fixture/wahrwelt/keybinds.lua"
  printf '%s\n' 'lock_cmd = /hypr/scripts/lock-active.sh' >"$fixture/hypridle.conf"
  printf '%s' "$fixture"
}

assert_rejected() {
  local name="$1"
  local payload="$2"
  local fixture output

  fixture="$(make_fixture "$name" "$payload")"
  if output="$(bash "$verifier" "$fixture" 2>&1)"; then
    fail "$name lifecycle fixture unexpectedly passed"
  fi
  case "$output" in
    *"outside start-shell.sh"*) ;;
    *) fail "$name failed for the wrong reason: $output" ;;
  esac
}

assert_allowed() {
  local name="$1"
  local payload="$2"
  local fixture output

  fixture="$(make_fixture "$name" "$payload")"
  if ! output="$(bash "$verifier" "$fixture" 2>&1)"; then
    fail "$name legitimate fixture was rejected: $output"
  fi
}

assert_lock_rejected() {
  local name="$1"
  local payload="$2"
  local fixture output

  fixture="$(make_fixture "$name" "$payload")"
  if output="$(bash "$verifier" "$fixture" 2>&1)"; then
    fail "$name unsafe lock fixture unexpectedly passed"
  fi
  case "$output" in
    *"enter logind before the native lock dispatcher"*) ;;
    *) fail "$name failed for the wrong reason: $output" ;;
  esac
}

assert_switchwall_command_pair() {
  local name="$1"
  local expectation="$2"
  local restore_command="$3"
  local runtime_command="$4"
  local flags_declaration="${5:-}"
  local fixture="$test_root/$name.switchwall.sh"
  local function_file="$test_root/switchwall-contract-function.sh"

  sed -n '/^switchwall_has_supported_video_commands() {/,/^}/p' "$verifier" >"$function_file"
  if [ ! -s "$function_file" ]; then
    fail "could not load switchwall video command contract helper"
  fi
  if [ -n "$flags_declaration" ]; then
    printf '%s\n' "$flags_declaration" "$restore_command" "$runtime_command" >"$fixture"
  else
    printf '%s\n' "$restore_command" "$runtime_command" >"$fixture"
  fi

  if [ "$expectation" = accept ]; then
    if ! bash -e -c 'source "$1"; switchwall_has_supported_video_commands "$2"' \
      end4-switchwall-contract "$function_file" "$fixture"; then
      fail "$name supported mpvpaper command pair was rejected"
    fi
  elif bash -e -c 'source "$1"; switchwall_has_supported_video_commands "$2"' \
    end4-switchwall-contract "$function_file" "$fixture"; then
    fail "$name unsupported mpvpaper command pair was accepted"
  fi
}

make_video_backend_fixture() {
  local name="$1"
  local use_helper="$2"
  local fixture="$test_root/$name"

  mkdir -p "$fixture/scripts/colors" "$fixture/scripts/wallpapers" "$fixture/scripts/lib"
  cp "$repo_root/Linux/NixOS/home/end4/patches/video-backend-reconcile.sh" \
    "$fixture/scripts/wallpapers/video-backend-reconcile.sh"
  chmod 0755 "$fixture/scripts/wallpapers/video-backend-reconcile.sh"
  if [ "$use_helper" = 1 ]; then
    cat >"$fixture/scripts/lib/config.sh" <<'EOF'
config_json_update() {
  :
}
EOF
  fi
  cat >"$fixture/scripts/colors/switchwall.sh" <<'SWITCHWALL'
#!/usr/bin/env bash

SCRIPT_DIR="$(cd "$(dirname "$BASH_SOURCE")" && pwd)"
if [ "$END4_TEST_USE_CONFIG_HELPER" = 1 ]; then
  source "$SCRIPT_DIR/../lib/config.sh"
else
  config_json_update() {
    :
  }
fi

if [ -z "$XDG_CONFIG_HOME" ]; then
  XDG_CONFIG_HOME="$HOME/.config"
fi
SHELL_CONFIG_FILE="$XDG_CONFIG_HOME/illogical-impulse/config.json"
RESTORE_SCRIPT_DIR="$XDG_CONFIG_HOME/hypr/custom/scripts"
RESTORE_SCRIPT="$RESTORE_SCRIPT_DIR/__restore_video_wallpaper.sh"
VIDEO_OPTS="no-audio loop hwdec=auto scale=bilinear interpolation=no video-sync=display-resample panscan=1.0 video-scale-x=1.0 video-scale-y=1.0 video-align-x=0.5 video-align-y=0.5 load-scripts=no"
MPVPAPER_FLAGS="$END4_TEST_MPVPAPER_FLAGS"

create_restore_script() {
  local video_path="$1"
  local escaped_video_path escaped_video_opts
  config_json_update "$SHELL_CONFIG_FILE" --arg path "$video_path" \
    '.background.wallpaperPath = $path' || return
  printf -v escaped_video_path '%q' "$video_path"
  printf -v escaped_video_opts '%q' "$VIDEO_OPTS"
  cat >"$RESTORE_SCRIPT" <<EOF
#!$BASH
for monitor in \$(hyprctl monitors -j | jq -r '.[] | .name'); do
  mpvpaper $MPVPAPER_FLAGS -o $escaped_video_opts "\$monitor" $escaped_video_path &
  sleep 0.1
done
EOF
  chmod 0755 "$RESTORE_SCRIPT"
}

main() {
  :
}

main "$@"
SWITCHWALL
  chmod 0755 "$fixture/scripts/colors/switchwall.sh"
  printf '%s' "$fixture"
}

assert_video_backend_fixture() {
  local name="$1"
  local use_helper="$2"
  local mpvpaper_flags="$3"
  local fixture function_file output

  fixture="$(make_video_backend_fixture "$name" "$use_helper")"
  if [ "$use_helper" = 1 ] && [ ! -f "$fixture/scripts/lib/config.sh" ]; then
    fail "$name fixture is missing its relative config helper"
  fi
  if [ "$use_helper" = 0 ] && [ -e "$fixture/scripts/lib/config.sh" ]; then
    fail "$name legacy fixture unexpectedly contains the config helper"
  fi
  function_file="$test_root/video-backend-function.sh"
  sed -n '/^run_end4_video_backend_simulation() {/,/^}/p' "$verifier" >"$function_file"
  if [ ! -s "$function_file" ]; then
    fail "video backend fixture could not load the verifier simulation"
  fi
  if ! output="$(END4_TEST_USE_CONFIG_HELPER="$use_helper" \
    END4_TEST_MPVPAPER_FLAGS="$mpvpaper_flags" \
    bash -e -c 'source "$1"; run_end4_video_backend_simulation "$2" "$3"' \
    end4-video-backend-test "$function_file" \
    "$fixture/scripts/wallpapers/video-backend-reconcile.sh" \
    "$fixture/scripts/colors/switchwall.sh" 2>&1)"; then
    fail "$name video backend fixture failed: $output"
  fi
  case "$output" in
    *"No such file or directory"* | *"command not found"*)
      fail "$name reported an unavailable relative helper: $output"
      ;;
  esac
}

# shellcheck disable=SC2016
assert_rejected current-restart \
  'hl.bind("CTRL + SUPER + R", hl.dsp.exec_cmd("killall ydotool qs quickshell; qs -c $qsConfig &"))'
# shellcheck disable=SC2016
assert_rejected pkill-quickshell \
  'hl.bind("CTRL + SUPER + R", hl.dsp.exec_cmd("pkill quickshell; quickshell -c $qsConfig &"))'
assert_rejected direct-qs \
  'hl.exec_cmd("qs -c ii")'
assert_rejected direct-qs-path \
  'hl.exec_cmd("qs -p /tmp/welcome.qml")'
assert_rejected direct-quickshell-path \
  'hl.exec_cmd("quickshell -p /tmp/settings.qml")'
assert_rejected quoted-bare-qs \
  'hl.exec_cmd("qs")'
assert_rejected quoted-bare-quickshell \
  'hl.exec_cmd("quickshell")'
assert_rejected quoted-bare-hypridle \
  'hl.exec_cmd("hypridle")'
assert_rejected qml-bare-qs-array \
  'Quickshell.execDetached(["qs"]);'
assert_rejected qml-shell-wrapper-qs \
  'Quickshell.execDetached(["bash", "-c", "qs"]);'
assert_rejected qml-env-wrapper-quickshell \
  'Quickshell.execDetached(["env", "quickshell"]);'
assert_rejected qml-shell-wrapper-hypridle \
  'Quickshell.execDetached(["sh", "-c", "hypridle"]);'
assert_rejected start-shell-comment-bypass \
  'hl.exec_cmd("qs -c ii") -- managed by scripts/start-shell.sh'
assert_rejected ipc-comment-bypass \
  'hl.exec_cmd("qs -c ii") -- use ipc call for notifications'
assert_rejected start-shell-string-bypass \
  'hl.bind("SUPER + R", hl.exec_cmd("qs -c ii"), "managed by scripts/start-shell.sh")'
assert_rejected ipc-string-bypass \
  'hl.bind("SUPER + R", hl.exec_cmd("qs -c ii"), "use ipc call for notifications")'
assert_rejected allowed-launch-string-bypass \
  'hl.bind("SUPER + R", hl.exec_cmd("qs -c ii"), "qs -c note ipc call harmless")'
assert_allowed ipc-call \
  'hl.exec_cmd("qs -c ii ipc call notificationService dismissAll")'
assert_allowed qml-ipc-call \
  'Quickshell.execDetached(["qs", "-c", Quickshell.env("qsConfig"), "ipc", "call", "sidebarRight", "toggle"]);'
assert_lock_rejected logind-lock \
  'hl.bind("SUPER + L", hl.dsp.exec_cmd("loginctl lock-session"))'
assert_rejected qml-unmanaged-official-native-settings \
  'Quickshell.execDetached(["qs", "-n", "-p", Quickshell.shellPath("settings.qml")]);'
assert_rejected qml-native-settings-env-path \
  'Quickshell.execDetached(["qs", "-n", "-p", Quickshell.env("qsConfig") + "/settings.qml"]);'
assert_rejected qml-native-settings-wrong-file \
  'Quickshell.execDetached(["qs", "-n", "-p", Quickshell.shellPath("welcome.qml")]);'
assert_rejected qml-direct-path \
  'Quickshell.execDetached(["qs", "-p", Quickshell.shellPath("welcome.qml")]);'
assert_rejected qml-multiline-direct-path \
  $'Quickshell.execDetached([\n  "qs",\n  "-p",\n  Quickshell.shellPath("welcome.qml")\n]);'
assert_rejected qml-intervening-flags \
  $'Quickshell.execDetached([\n  "qs", "-n", "-d", "-c", "ii", "ipc", "call", "sidebarRight", "toggle"\n]);'
assert_rejected shell-intervening-flags \
  'hl.exec_cmd("qs --daemon -c ii ipc call sidebarRight toggle")'
assert_rejected shell-default-launch \
  'hl.exec_cmd("qs &")'
assert_rejected shell-semicolon-launch \
  'hl.exec_cmd("qs; echo launched")'
assert_rejected shell-newline-launch \
  $'hl.exec_cmd([[qs\n]])'
# shellcheck disable=SC2016
assert_rejected shell-command-substitution-kill \
  'hl.exec_cmd("kill $(pgrep quickshell)")'
assert_rejected shell-hypridle-launch \
  'hl.exec_cmd("hypridle --config /tmp/upstream.conf &")'
assert_rejected qml-hypridle-launch \
  'Quickshell.execDetached(["hypridle", "--config", "/tmp/upstream.conf"]);'
assert_rejected qml-multiline-kill \
  $'Quickshell.execDetached([\n  "killall",\n  "ydotool",\n  "qs",\n  "quickshell"\n]);'
assert_rejected shell-multiline-kill \
  $'hl.exec_cmd("killall ydotool \\\nqs quickshell; qs -c ii ipc call TEST_ALIVE")'
assert_allowed managed-start \
  'hl.exec_cmd("/home/user/.config/hypr/scripts/start-shell.sh end4")'
assert_allowed lifecycle-comment \
  '-- upstream used to run: killall quickshell; hypridle &'
assert_allowed quoted-lifecycle-comment \
  '-- upstream used to call hl.exec_cmd("qs")'

extensionless_fixture="$(make_fixture extensionless-helper '')"
printf '%s\n' '#!/bin/sh' 'qs &' >"$extensionless_fixture/helper"
chmod 0755 "$extensionless_fixture/helper"
if output="$(bash "$verifier" "$extensionless_fixture" 2>&1)"; then
  fail "extensionless helper lifecycle fixture unexpectedly passed"
fi
case "$output" in
  *"outside start-shell.sh"*) ;;
  *) fail "extensionless helper failed for the wrong reason: $output" ;;
esac

assert_video_backend_fixture video-backend-with-helper 1 '-p -a max'
assert_video_backend_fixture video-backend-legacy 0 ''

# shellcheck disable=SC2016
assert_switchwall_command_pair legacy-video-commands accept \
  'mpvpaper -o "$VIDEO_OPTS" "\$monitor" $escaped_video_path &' \
  'mpvpaper -o "$VIDEO_OPTS" "$monitor" "$video_path" &'
# shellcheck disable=SC2016
assert_switchwall_command_pair flagged-video-commands accept \
  'mpvpaper $MPVPAPER_FLAGS -o "$VIDEO_OPTS" "\$monitor" $escaped_video_path &' \
  'mpvpaper $MPVPAPER_FLAGS -o "$VIDEO_OPTS" "$monitor" "$video_path" &' \
  'MPVPAPER_FLAGS="-p -a max"'
# shellcheck disable=SC2016
assert_switchwall_command_pair spaced-flagged-video-commands accept \
  'mpvpaper  $MPVPAPER_FLAGS   -o  "$VIDEO_OPTS"  "\$monitor"  $escaped_video_path  &' \
  'mpvpaper  $MPVPAPER_FLAGS   -o  "$VIDEO_OPTS"  "$monitor"  "$video_path"  &' \
  'MPVPAPER_FLAGS="-p -a max"'
# shellcheck disable=SC2016
assert_switchwall_command_pair mixed-video-command-flags reject \
  'mpvpaper $MPVPAPER_FLAGS -o "$VIDEO_OPTS" "\$monitor" $escaped_video_path &' \
  'mpvpaper -o "$VIDEO_OPTS" "$monitor" "$video_path" &' \
  'MPVPAPER_FLAGS="-p -a max"'
# shellcheck disable=SC2016
assert_switchwall_command_pair reordered-video-runtime-argv reject \
  'mpvpaper -o "$VIDEO_OPTS" "\$monitor" $escaped_video_path &' \
  'mpvpaper -o "$VIDEO_OPTS" "$video_path" "$monitor" &'
# shellcheck disable=SC2016
assert_switchwall_command_pair comment-only-video-commands reject \
  '# mpvpaper -o "$VIDEO_OPTS" "\$monitor" $escaped_video_path &' \
  '# mpvpaper -o "$VIDEO_OPTS" "$monitor" "$video_path" &'

printf 'OK End4 artifact lifecycle verifier fixtures\n'
