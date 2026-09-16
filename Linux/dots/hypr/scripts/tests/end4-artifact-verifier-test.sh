#!/usr/bin/env bash
set -euo pipefail

tests_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
verifier="$tests_dir/end4-artifact-test.sh"
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

printf 'OK End4 artifact lifecycle verifier fixtures\n'
