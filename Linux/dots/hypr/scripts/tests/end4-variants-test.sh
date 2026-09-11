#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2329
set -euo pipefail

scripts_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
installer_dir="$(CDPATH='' cd -- "$scripts_dir/../../../installer" && pwd)"
test_root="$(mktemp -d)"
original_home="$HOME"
lock_test_pid=""
env_match_pid=""
trap '[ -z "$lock_test_pid" ] || kill "$lock_test_pid" 2>/dev/null || true; [ -z "$env_match_pid" ] || kill "$env_match_pid" 2>/dev/null || true; rm -rf -- "$test_root"' EXIT

export HOME="$test_root/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_STATE_HOME="$HOME/.local/state"
export XDG_RUNTIME_DIR="$test_root/runtime"
mkdir -p "$HOME" "$XDG_RUNTIME_DIR"
chmod 0700 "$XDG_RUNTIME_DIR"
export WAHRWELT_FS_HELPER="$test_root/wahrwelt-fs-helper"
(cd "$installer_dir" && HOME="$original_home" go build -o "$WAHRWELT_FS_HELPER" ./cmd/wahrwelt-fs-helper)

# shellcheck source=Linux/dots/hypr/scripts/shell-runtime.sh
. "$scripts_dir/shell-runtime.sh"

assert_eq() {
  local want="$1"
  local got="$2"
  local message="$3"

  if [ "$got" != "$want" ]; then
    printf 'FAIL: %s: got %q, want %q\n' "$message" "$got" "$want" >&2
    exit 1
  fi
}

assert_matches() {
  local pattern="$1"
  local value="$2"
  local message="$3"

  if ! printf '%s\n' "$value" | grep -Eq -- "$pattern"; then
    printf 'FAIL: %s: %q does not match %q\n' "$message" "$value" "$pattern" >&2
    exit 1
  fi
}

assert_not_matches() {
  local pattern="$1"
  local value="$2"
  local message="$3"

  if printf '%s\n' "$value" | grep -Eq -- "$pattern"; then
    printf 'FAIL: %s: %q unexpectedly matches %q\n' "$message" "$value" "$pattern" >&2
    exit 1
  fi
}

# Runtime state must never be updated in place through a hardlink whose other
# name lies outside the managed runtime tree.
hardlink_root="$test_root/runtime-hardlink"
hardlink_outside="$test_root/runtime-hardlink-outside"
hardlink_target="$hardlink_root/runtime-state"
mkdir -p "$hardlink_root"
printf '%s\n' outside-bytes >"$hardlink_outside"
ln -- "$hardlink_outside" "$hardlink_target"

wahrwelt_valid_shell_profile end4-pc
assert_eq end4 "$(wahrwelt_shell_family end4)" "Official family"
assert_eq end4 "$(wahrwelt_shell_family end4-pc)" "pC family"
assert_eq ii "$(wahrwelt_end4_quickshell_config end4)" "Official config"
assert_eq end4-pC "$(wahrwelt_end4_quickshell_config end4-pc)" "pC config"
assert_eq "$XDG_CONFIG_HOME/quickshell/ii" "$(wahrwelt_end4_quickshell_path end4)" "Official runtime path"
assert_eq "$XDG_CONFIG_HOME/quickshell/end4-pC" "$(wahrwelt_end4_quickshell_path end4-pc)" "pC runtime path"
assert_eq end4 "$(wahrwelt_read_end4_variant)" "missing state fallback"
assert_matches "$wahrwelt_end4_official_env_pattern" "WAHRWELT_END4_PROFILE=end4" "Official exact process marker"
assert_matches "$wahrwelt_end4_pc_env_pattern" "WAHRWELT_END4_PROFILE=end4-pc" "pC exact process marker"
assert_not_matches "$wahrwelt_end4_env_pattern" "qsConfig=$XDG_CONFIG_HOME/quickshell/ii" "generic qsConfig does not identify End4"
assert_not_matches "$wahrwelt_end4_env_pattern" "ILLOGICAL_IMPULSE_DOTFILES_SOURCE=$XDG_CONFIG_HOME" "generic End4 variable does not identify End4"
assert_not_matches "$wahrwelt_end4_env_pattern" "WAHRWELT_END4_PROFILE=caelestia" "Caelestia with stale generic End4 variables is not End4"

printf -v env_padding '%080000d' 0
env_match_ready="$test_root/env-match-ready"
env_match_program="$(readlink -e -- "$(command -v sleep)")"
env -i \
  WAHRWELT_END4_PROFILE=end4-pc \
  "ZZZ_PADDING=$env_padding" \
  WAHRWELT_TEST_ENV_READY="$env_match_ready" \
  "$(command -v bash)" -c '
    : >"$WAHRWELT_TEST_ENV_READY"
    exec "$1" 30
  ' _ "$(command -v sleep)" &
env_match_pid=$!
env_match_post_exec=0
# The child publishes ready immediately before exec. Verify /proc/exe as well
# so the environment reader never races the address-space replacement.
for _ in $(seq 1 500); do
  if [ -e "$env_match_ready" ] &&
    [ "$(readlink -e -- "/proc/$env_match_pid/exe" 2>/dev/null || true)" = "$env_match_program" ]; then
    env_match_post_exec=1
    break
  fi
  command sleep 0.01
done
if [ "$env_match_post_exec" -ne 1 ]; then
  printf 'FAIL: large environment fixture did not reach post-exec readiness\n' >&2
  exit 1
fi
if ! wahrwelt_pid_has_env_regex "$env_match_pid" "$wahrwelt_end4_pc_env_pattern"; then
  printf 'FAIL: exact End4 marker before a large environment payload was not detected under pipefail\n' >&2
  exit 1
fi
kill "$env_match_pid" 2>/dev/null || true
wait "$env_match_pid" 2>/dev/null || true
env_match_pid=""
unset env_padding env_match_program env_match_post_exec

legacy_entrypoint="$test_root/legacy-hyprland.lua"
legacy_fixture_dir="$scripts_dir/../../../NixOS/home/migrations/v1_to_v2/hypr-runtime"
for variant in end4 end4-pc; do
  cp -- "$legacy_fixture_dir/$variant.lua" "$legacy_entrypoint"
  wahrwelt_is_legacy_direct_end4_entrypoint "$legacy_entrypoint" "$XDG_CONFIG_HOME"

  {
    printf '%s\n' '-- prefix'
    cat "$legacy_fixture_dir/$variant.lua"
  } >"$legacy_entrypoint"
  if wahrwelt_is_legacy_direct_end4_entrypoint "$legacy_entrypoint" "$XDG_CONFIG_HOME"; then
    printf 'FAIL: prefixed %s legacy payload unexpectedly matched\n' "$variant" >&2
    exit 1
  fi

  {
    cat "$legacy_fixture_dir/$variant.lua"
    printf '%s\n' '-- suffix'
  } >"$legacy_entrypoint"
  if wahrwelt_is_legacy_direct_end4_entrypoint "$legacy_entrypoint" "$XDG_CONFIG_HOME"; then
    printf 'FAIL: suffixed %s legacy payload unexpectedly matched\n' "$variant" >&2
    exit 1
  fi

  legacy_text="$(cat "$legacy_fixture_dir/$variant.lua")"
  printf '%s' "$legacy_text" >"$legacy_entrypoint"
  if wahrwelt_is_legacy_direct_end4_entrypoint "$legacy_entrypoint" "$XDG_CONFIG_HOME"; then
    printf 'FAIL: %s legacy payload without final newline unexpectedly matched\n' "$variant" >&2
    exit 1
  fi
done
printf 'dofile("%s/hypr/end4/hyprland.lua")\n' "$XDG_CONFIG_HOME" >"$legacy_entrypoint"
if wahrwelt_is_legacy_direct_end4_entrypoint "$legacy_entrypoint" "$XDG_CONFIG_HOME"; then
  printf 'FAIL: unsupported absolute End4 shorthand unexpectedly matched\n' >&2
  exit 1
fi
printf '%s\n' '-- Active Hyprland profile: end4' 'dofile(end4_root .. "/hyprland.lua")' >"$legacy_entrypoint"
if wahrwelt_is_legacy_direct_end4_entrypoint "$legacy_entrypoint" "$XDG_CONFIG_HOME"; then
  printf 'FAIL: truncated End4 payload unexpectedly matched\n' >&2
  exit 1
fi

legacy_user_entrypoint="$test_root/legacy-user-hyprland.lua"
for payload in runtime home-manager; do
  case "$payload" in
    runtime) wahrwelt_print_legacy_user_entrypoint >"$legacy_user_entrypoint" ;;
    home-manager) wahrwelt_print_legacy_home_manager_user_entrypoint >"$legacy_user_entrypoint" ;;
  esac
  wahrwelt_is_legacy_user_entrypoint "$legacy_user_entrypoint"
  legacy_user_text="$(cat "$legacy_user_entrypoint")"
  for form in prefix suffix missing-newline arbitrary-root; do
    case "$form" in
      prefix) printf '%s\n%s\n' '-- prefix' "$legacy_user_text" >"$legacy_user_entrypoint" ;;
      suffix) printf '%s\n%s\n' "$legacy_user_text" '-- suffix' >"$legacy_user_entrypoint" ;;
      missing-newline) printf '%s' "$legacy_user_text" >"$legacy_user_entrypoint" ;;
      arbitrary-root) printf '%s\n' 'dofile("/tmp/arbitrary/wahrwelt/hyprland.lua")' >"$legacy_user_entrypoint" ;;
    esac
    if wahrwelt_is_legacy_user_entrypoint "$legacy_user_entrypoint"; then
      printf 'FAIL: non-legacy-user %s %s payload unexpectedly matched\n' "$payload" "$form" >&2
      exit 1
    fi
  done
done

canonical_entrypoint="$test_root/canonical-hyprland.lua"
for payload in runtime home-manager stable; do
  case "$payload" in
    runtime) wahrwelt_print_canonical_runtime_entrypoint >"$canonical_entrypoint" ;;
    home-manager) wahrwelt_print_home_manager_initial_entrypoint >"$canonical_entrypoint" ;;
    stable) wahrwelt_print_stable_runtime_entrypoint >"$canonical_entrypoint" ;;
  esac
  wahrwelt_is_canonical_entrypoint "$canonical_entrypoint"
done

wahrwelt_print_canonical_runtime_entrypoint >"$canonical_entrypoint"
canonical_text="$(cat "$canonical_entrypoint")"
for form in prefix suffix missing-newline absolute-shorthand arbitrary-root; do
  case "$form" in
    prefix) printf '%s\n%s\n' '-- prefix' "$canonical_text" >"$canonical_entrypoint" ;;
    suffix) printf '%s\n%s\n' "$canonical_text" '-- suffix' >"$canonical_entrypoint" ;;
    missing-newline) printf '%s' "$canonical_text" >"$canonical_entrypoint" ;;
    absolute-shorthand) printf 'dofile("%s/hypr/wahrwelt/hyprland.lua")\n' "$XDG_CONFIG_HOME" >"$canonical_entrypoint" ;;
    arbitrary-root) printf '%s\n' 'dofile("/tmp/arbitrary/wahrwelt/hyprland.lua")' >"$canonical_entrypoint" ;;
  esac
  if wahrwelt_is_canonical_entrypoint "$canonical_entrypoint"; then
    printf 'FAIL: non-canonical %s payload unexpectedly matched\n' "$form" >&2
    exit 1
  fi
done

selector_script="$scripts_dir/shell-selector.sh"
if ! grep -Fq 'if wahrwelt_is_canonical_entrypoint "$entrypoint_path"; then' "$selector_script" ||
  ! grep -Fq 'if wahrwelt_is_legacy_user_entrypoint "$entrypoint_path"; then' "$selector_script" ||
  ! grep -Fq 'if wahrwelt_is_legacy_direct_end4_entrypoint "$entrypoint_path" "$config_home"; then' "$selector_script"; then
  printf 'FAIL: shell selector does not delegate entrypoint recognition to exact payload matchers\n' >&2
  exit 1
fi
if grep -Eq 'grep .*wahrwelt/hyprland|grep .*end4/hyprland' "$selector_script"; then
  printf 'FAIL: shell selector retains line or substring entrypoint recognition\n' >&2
  exit 1
fi

start_shell="$scripts_dir/start-shell.sh"
# shellcheck disable=SC2016
for marker in \
  'WAHRWELT_END4_PROFILE="$profile"' \
  'WAHRWELT_QS_CONFIG="$end4_quickshell_path"' \
  'qsConfig="$end4_quickshell_path"' \
  'ILLOGICAL_IMPULSE_DOTFILES_SOURCE="$wahrwelt_config_home"' \
  'ILLOGICAL_IMPULSE_VIRTUAL_ENV="$wahrwelt_state_home/quickshell/.venv"' \
  'reconcile_end4_video_backend "$end4_quickshell_path"'; do
  if ! grep -Fq -- "$marker" "$start_shell"; then
    printf 'FAIL: start-shell local End4 launch environment missing %s\n' "$marker" >&2
    exit 1
  fi
done

if ! awk '
  /start_profile_with_retry "end4 \(\$profile\)"/ { launch = NR }
  /reconcile_end4_video_backend "\$end4_quickshell_path"/ { reconcile = NR }
  END { exit !(launch > 0 && reconcile > launch) }
' "$start_shell"; then
  printf 'FAIL: End4 startup does not reconcile the saved video after the target process is ready\n' >&2
  exit 1
fi

if ! grep -Fq 'ensure_end4_idle || return 1' "$start_shell"; then
  printf 'FAIL: End4 startup does not propagate managed hypridle failure\n' >&2
  exit 1
fi
if grep -Fq 'ensure_end4_idle || true' "$start_shell"; then
  printf 'FAIL: End4 startup still ignores managed hypridle failure\n' >&2
  exit 1
fi

start_profile_function="$test_root/start-profile-shell.sh"
awk '
  /^start_profile_shell\(\) \{$/ { capture = 1 }
  capture { print }
  capture && /^}$/ { exit }
' "$start_shell" >"$start_profile_function"
if ! grep -Fqx 'start_profile_shell() {' "$start_profile_function"; then
  printf 'FAIL: could not extract actual start_profile_shell implementation\n' >&2
  exit 1
fi

if (
  # shellcheck source=/dev/null
  . "$start_profile_function"
  profile=end4
  end4_handle=end4-family
  end4_official_handle=end4-official
  end4_pc_handle=end4-pc
  log() { :; }
  ensure_end4_idle() { return 1; }
  dedupe_shell() { return 0; }
  is_running() { return 1; }
  command() { return 0; }
  start_with_retry() { return 0; }
  start_profile_shell
); then
  printf 'FAIL: actual End4 start_profile_shell accepted managed hypridle failure\n' >&2
  exit 1
fi

selector_handle=selector
caelestia_handle=caelestia
caelestia_resizer_handle=caelestia-resizer
noctalia_handle=noctalia
end4_handle=end4-family
end4_official_handle=end4-official
end4_pc_handle=end4-pc
end4_idle_handle=end4-idle
end4_idle_config="$wahrwelt_hypr_runtime_dir/hypridle.conf"
user_name="$wahrwelt_user_name"
selector_pattern="$wahrwelt_selector_pattern"
caelestia_pattern="$wahrwelt_caelestia_pattern"
end4_env_pattern="$wahrwelt_end4_env_pattern"
fake_process_env="WAHRWELT_END4_PROFILE=caelestia
qsConfig=$XDG_CONFIG_HOME/quickshell/ii
ILLOGICAL_IMPULSE_DOTFILES_SOURCE=$XDG_CONFIG_HOME"

wahrwelt_quickshell_pids() {
  printf '%s\n' 4242
}

wahrwelt_pid_has_env_regex() {
  printf '%s\n' "$fake_process_env" | grep -qE -- "$2"
}

# shellcheck source=Linux/dots/hypr/scripts/shell-process.sh
. "$scripts_dir/shell-process.sh"

assert_eq "" "$(matching_pids "$end4_handle")" "Caelestia with stale generic End4 variables stays outside End4 family"
fake_process_env="WAHRWELT_END4_PROFILE=end4-pc
qsConfig=$XDG_CONFIG_HOME/quickshell/end4-pC"
assert_eq 4242 "$(matching_pids "$end4_pc_handle")" "exact pC process marker identifies variant"

fake_quickshell_pid_list=$'5101\n5102\n5103\n5104\n5105\n5106\n5107\n5109'
wahrwelt_quickshell_pids() {
  printf '%s\n' "$fake_quickshell_pid_list"
}
wahrwelt_pid_has_env_regex() {
  local process_env=""

  case "$1" in
    5101 | 5102 | 5106 | 5107 | 5109) process_env="" ;;
    5103) process_env='WAHRWELT_END4_PROFILE=end4' ;;
    5104) process_env='WAHRWELT_END4_PROFILE=end4-pc' ;;
    5105) process_env="qsConfig=$XDG_CONFIG_HOME/quickshell/ii
ILLOGICAL_IMPULSE_DOTFILES_SOURCE=$XDG_CONFIG_HOME" ;;
  esac
  printf '%s\n' "$process_env" | grep -qE -- "$2"
}
wahrwelt_pid_has_adjacent_args() {
  local config=""

  [ "$2" = -c ] || return 1
  case "$1" in
    5101 | 5103 | 5109) config=ii ;;
    5102 | 5104) config=end4-pC ;;
    5105) config=caelestia ;;
    5106) config=custom-shell ;;
    5107) config=ii-extra ;;
  esac
  [ "$config" = "$3" ]
}
wahrwelt_pid_is_legacy_end4_upgrade_token() {
  local token="$1"
  local pid="${token%%:*}"

  case "$token" in
    5101:101:ii | 5102:102:end4-pC) ;;
    *) return 1 ;;
  esac
  printf '%s\n' "$fake_quickshell_pid_list" | grep -Fqx -- "$pid"
}

assert_eq "" "$(wahrwelt_legacy_end4_upgrade_pids "")" \
  "normal runtime never scans unmarked QuickShell argv without positive upgrade provenance"
assert_eq $'5101\n5102' "$(wahrwelt_legacy_end4_upgrade_pids '5101:101:ii,5102:102:end4-pC')" \
  "positive migration provenance binds cleanup to exact historical process identities"
assert_eq $'5103\n5104' "$(matching_pids "$end4_handle")" \
  "canonical End4 runtime still uses only the exact environment marker"

killed_legacy_pids=""
kill() {
  local pid="$2"

  killed_legacy_pids+="${killed_legacy_pids:+$'\n'}$pid"
  fake_quickshell_pid_list="$(printf '%s\n' "$fake_quickshell_pid_list" | grep -vx -- "$pid" || true)"
}
sleep() {
  :
}
log() {
  :
}
wahrwelt_open_end4_upgrade_state
legacy_end4_upgrade_tokens="$(
  wahrwelt_merge_end4_upgrade_tokens '5101:101:ii,5102:102:end4-pC'
)"
cleanup_legacy_end4_processes
assert_eq $'5101\n5102' "$(printf '%s\n' "$killed_legacy_pids" | sort -u)" \
  "upgrade cleanup stops both historical End4 variants"
assert_eq "" "$legacy_end4_upgrade_tokens" "upgrade cleanup consumes process provenance exactly once"
cleanup_legacy_end4_processes
assert_eq $'5101\n5102' "$(printf '%s\n' "$killed_legacy_pids" | sort -u)" \
  "consumed upgrade provenance cannot kill a later process"
if ! printf '%s\n' "$fake_quickshell_pid_list" | grep -Fqx 5109; then
  printf 'FAIL: unrelated unmarked qs -c ii process was killed during proven upgrade cleanup\n' >&2
  exit 1
fi

fake_quickshell_pid_list+=$'\n5108'
wahrwelt_pid_has_env_regex() {
  local process_env=""

  case "$1" in
    5103 | 5108) process_env='WAHRWELT_END4_PROFILE=end4' ;;
    5104) process_env='WAHRWELT_END4_PROFILE=end4-pc' ;;
    5105) process_env="qsConfig=$XDG_CONFIG_HOME/quickshell/ii
ILLOGICAL_IMPULSE_DOTFILES_SOURCE=$XDG_CONFIG_HOME" ;;
  esac
  printf '%s\n' "$process_env" | grep -qE -- "$2"
}
wahrwelt_pid_has_adjacent_args() {
  local config=""

  [ "$2" = -c ] || return 1
  case "$1" in
    5103 | 5108 | 5109) config=ii ;;
    5104) config=end4-pC ;;
    5105) config=caelestia ;;
    5106) config=custom-shell ;;
    5107) config=ii-extra ;;
  esac
  [ "$config" = "$3" ]
}
assert_eq "" "$(wahrwelt_legacy_end4_upgrade_pids "$legacy_end4_upgrade_tokens")" \
  "cleanup does not retain a permanent argv recognizer"
assert_eq $'5103\n5108' "$(matching_pids "$end4_official_handle")" \
  "historical process replacement leaves only exact-marked Official End4 instances"

unset -f kill log sleep wahrwelt_pid_is_legacy_end4_upgrade_token

proc_fixture="$test_root/proc"
for pid in 7101 7102 7103 7104 7105; do
  mkdir -p "$proc_fixture/$pid"
done
printf '%s\0' hypridle --verbose -c "$end4_idle_config" --debug >"$proc_fixture/7101/cmdline"
printf '%s\0' hypridle -c "${end4_idle_config}.unmanaged" >"$proc_fixture/7102/cmdline"
printf '%s\0' hypridle "--label=-c" "$end4_idle_config" -c "$test_root/other.conf" >"$proc_fixture/7103/cmdline"
printf '%s\0' hypridle -c "prefix${end4_idle_config}" >"$proc_fixture/7104/cmdline"
printf '%s\0' hypridle --debug --socket test -c "$end4_idle_config" --verbose >"$proc_fixture/7105/cmdline"

managed_idle_pids="$({
  pgrep() {
    printf '%s\n' 7101 7102 7103 7104 7105
  }
  wahrwelt_pid_has_adjacent_args() {
    wahrwelt_cmdline_has_adjacent_args "$proc_fixture/$1/cmdline" "$2" "$3"
  }
  matching_pids "$end4_idle_handle"
})"
assert_eq $'7101\n7105' "$managed_idle_pids" "only exact adjacent End4 hypridle argv matches the stop handle"

mkdir -p "$(dirname -- "$wahrwelt_end4_variant_state")"
printf '%s\n' end4-pc >"$wahrwelt_end4_variant_state"
assert_eq end4-pc "$(wahrwelt_read_end4_variant)" "remembered pC variant"

printf '%s\n' '../../arbitrary-command' >"$wahrwelt_end4_variant_state"
assert_eq end4 "$(wahrwelt_read_end4_variant)" "invalid state fallback"

printf '%s' end4-pc >"$wahrwelt_end4_variant_state"
assert_eq end4 "$(wahrwelt_read_end4_variant)" "missing newline fallback"

printf 'e n d 4-pc\n' >"$wahrwelt_end4_variant_state"
assert_eq end4 "$(wahrwelt_read_end4_variant)" "embedded whitespace fallback"

printf 'end4-pc\n\n' >"$wahrwelt_end4_variant_state"
assert_eq end4 "$(wahrwelt_read_end4_variant)" "extra line fallback"

printf '%s\n' end4-pc >"$wahrwelt_end4_variant_state"

hypr_runtime_dir="$wahrwelt_hypr_runtime_dir"
persistent_state_file="$wahrwelt_active_shell_state"
profile=end4-pc

log() {
  :
}

hypr_dir() {
  wahrwelt_hypr_dir_path
}

# shellcheck source=Linux/dots/hypr/scripts/shell-profile-sync.sh
. "$scripts_dir/shell-profile-sync.sh"

persist_profile
assert_eq end4-pc "$(tr -d '[:space:]' <"$persistent_state_file")" "active profile persistence"
assert_eq end4-pc "$(tr -d '[:space:]' <"$wahrwelt_end4_variant_state")" "variant persistence"

profile=noctalia
persist_profile
assert_eq noctalia "$(tr -d '[:space:]' <"$persistent_state_file")" "non-end4 active persistence"
assert_eq end4-pc "$(tr -d '[:space:]' <"$wahrwelt_end4_variant_state")" "non-end4 preserves remembered variant"

printf '%s\n' 'prior active state' 'with exact bytes' >"$persistent_state_file"
chmod 0600 "$persistent_state_file"
variant_prior="$test_root/prior-end4-variant"
printf '%s\n' end4 >"$variant_prior"
rm -f -- "$wahrwelt_end4_variant_state"
ln -s -- "$variant_prior" "$wahrwelt_end4_variant_state"
split_state_write_attempt=0
wahrwelt_after_runtime_publication_hook() {
  case "$1" in
    "$wahrwelt_end4_variant_state" | "$persistent_state_file") ;;
    *) return 0 ;;
  esac
  split_state_write_attempt=$((split_state_write_attempt + 1))
  if [ "$split_state_write_attempt" -eq 2 ]; then
    return 1
  fi
  return 0
}
profile=end4-pc
if persist_profile; then
  printf 'FAIL: split-state commit failure unexpectedly succeeded\n' >&2
  exit 1
fi
assert_eq $'prior active state\nwith exact bytes' "$(cat "$persistent_state_file")" "active state bytes roll back after second commit failure"
assert_eq 600 "$(stat -c %a "$persistent_state_file")" "active state mode rolls back after second commit failure"
if [ ! -L "$wahrwelt_end4_variant_state" ]; then
  printf 'FAIL: remembered End4 variant symlink was not restored\n' >&2
  exit 1
fi
assert_eq "$variant_prior" "$(readlink -- "$wahrwelt_end4_variant_state")" "remembered variant symlink target rolls back"
unset -f wahrwelt_after_runtime_publication_hook

printf '%s\n' 'prior active transaction state' >"$persistent_state_file"
rm -f -- "$wahrwelt_end4_variant_state"
printf '%s\n' end4-pc >"$wahrwelt_end4_variant_state"
profile=end4
wahrwelt_after_runtime_publication_hook() {
  local path="$1"

  [ "$path" = "$persistent_state_file" ] || return 0
  mv -- "$path" "$test_root/transaction-owned-active-state"
  printf '%s\n' 'concurrent active-state winner' >"$path"
  chmod 0600 "$path"
}
if persist_profile; then
  printf 'FAIL: concurrent active-state winner unexpectedly committed\n' >&2
  exit 1
fi
unset -f wahrwelt_after_runtime_publication_hook
assert_eq 'concurrent active-state winner' "$(tr -d '\n' <"$persistent_state_file")" \
  "state rollback preserves winner swapped between publication and ownership record"
if ! find "$XDG_RUNTIME_DIR" -maxdepth 1 -type d -name '.state-switch-rollback-*' -print -quit | grep -q .; then
  printf 'FAIL: state concurrent winner did not retain rollback recovery\n' >&2
  exit 1
fi

mkdir -p "$wahrwelt_hypr_dir"
hm_generation="$test_root/current-home-generation"
hm_end4="$hm_generation/home-files/.config/hypr/end4"
hm_end4_artifact="$test_root/hm-end4-artifact"
hm_gcroot="$HOME/.local/state/home-manager/gcroots/current-home"
mkdir -p "$hm_end4_artifact" "$(dirname -- "$hm_end4")" "$(dirname -- "$hm_gcroot")" "$wahrwelt_config_home/quickshell"
printf '%s\n' '-- exact HM End4 source' >"$hm_end4_artifact/hyprland.lua"
ln -s -- "$hm_end4_artifact" "$hm_end4"
ln -s -- "$hm_generation" "$hm_gcroot"

printf '%s\n' collision >"$wahrwelt_hypr_dir/end4"
profile=end4
if validate_end4_profile_tree; then
  printf 'FAIL: unknown end4 collision unexpectedly passed validation\n' >&2
  exit 1
fi
assert_eq collision "$(tr -d '[:space:]' <"$wahrwelt_hypr_dir/end4")" "end4 collision remains untouched"
mv -- "$wahrwelt_hypr_dir/end4" "$test_root/end4-file-collision"

mkdir -p "$wahrwelt_hypr_dir/end4"
for name in hyprland.lua hyprlock.conf hypridle.conf launcher.lua; do
  printf '%s\n' "unknown $name" >"$wahrwelt_hypr_dir/end4/$name"
done
if validate_end4_profile_tree; then
  printf 'FAIL: unknown end4 directory with expected files unexpectedly passed ownership validation\n' >&2
  exit 1
fi
assert_eq "unknown hyprland.lua" "$(tr -d '\n' <"$wahrwelt_hypr_dir/end4/hyprland.lua")" "unknown end4 directory remains untouched"
mv -- "$wahrwelt_hypr_dir/end4" "$test_root/end4-directory-collision"

ln -s -- "$test_root/missing-home-manager-source" "$wahrwelt_hypr_dir/end4"
if validate_end4_profile_tree; then
  printf 'FAIL: broken End4 target unexpectedly passed ownership validation\n' >&2
  exit 1
fi
assert_eq "$test_root/missing-home-manager-source" "$(readlink -- "$wahrwelt_hypr_dir/end4")" "broken End4 link remains untouched"
mv -- "$wahrwelt_hypr_dir/end4" "$test_root/end4-broken-link"

wrong_generation="$test_root/wrong-home-generation/.config/hypr/end4"
mkdir -p "$wrong_generation"
printf '%s\n' '-- wrong generation' >"$wrong_generation/hyprland.lua"
ln -s -- "$wrong_generation" "$wahrwelt_hypr_dir/end4"
if validate_end4_profile_tree; then
  printf 'FAIL: wrong-generation End4 link unexpectedly passed ownership validation\n' >&2
  exit 1
fi
assert_eq "$wrong_generation" "$(readlink -- "$wahrwelt_hypr_dir/end4")" "wrong-generation End4 link remains untouched"
mv -- "$wahrwelt_hypr_dir/end4" "$test_root/end4-wrong-generation-link"

untrusted_root="$test_root/untrusted-home-manager-files"
untrusted_end4="$untrusted_root/.config/hypr/end4"
untrusted_quickshell="$untrusted_root/.config/quickshell/ii"
mkdir -p "$untrusted_end4" "$untrusted_quickshell"
printf '%s\n' '-- self-controlled suffix source' >"$untrusted_end4/hyprland.lua"
ln -s -- "$untrusted_quickshell" "$wahrwelt_config_home/quickshell/ii"
ln -s -- "$untrusted_end4" "$wahrwelt_hypr_dir/end4"
if validate_end4_profile_tree; then
  printf 'FAIL: suffix-shaped QuickShell source unexpectedly proved End4 ownership\n' >&2
  exit 1
fi
assert_eq "$untrusted_end4" "$(readlink -- "$wahrwelt_hypr_dir/end4")" "untrusted suffix-shaped End4 link remains untouched"
mv -- "$wahrwelt_hypr_dir/end4" "$test_root/end4-untrusted-source-link"

ln -s -- "$hm_end4" "$wahrwelt_hypr_dir/end4"
validate_end4_profile_tree

fake_bin="$test_root/fake-bin"
lock_log="$test_root/lock.log"
mkdir -p "$fake_bin" "$(dirname -- "$wahrwelt_active_shell_state")"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf "%s\n" "$WAHRWELT_LOCK_TEST_PID"' \
  >"$fake_bin/pgrep"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf "hyprctl %s\n" "$*" >>"$WAHRWELT_LOCK_TEST_LOG"' \
  >"$fake_bin/hyprctl"
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'printf "hyprlock %s\n" "$*" >>"$WAHRWELT_LOCK_TEST_LOG"' \
  >"$fake_bin/hyprlock"
chmod 0755 "$fake_bin/pgrep" "$fake_bin/hyprctl" "$fake_bin/hyprlock"

env WAHRWELT_END4_PROFILE=end4 sleep 30 &
lock_test_pid=$!
lock_marker_ready=0
for _ in $(seq 1 50); do
  if tr '\0' '\n' <"/proc/$lock_test_pid/environ" 2>/dev/null | grep -Fqx 'WAHRWELT_END4_PROFILE=end4'; then
    lock_marker_ready=1
    break
  fi
  command sleep 0.01
done
assert_eq 1 "$lock_marker_ready" "exact End4 lock fixture publishes its process marker"
printf '%s\n' end4 >"$wahrwelt_active_shell_state"
: >"$lock_log"
PATH="$fake_bin:$PATH" \
  WAHRWELT_LOCK_TEST_PID="$lock_test_pid" \
  WAHRWELT_LOCK_TEST_LOG="$lock_log" \
  "$scripts_dir/lock-active.sh"
assert_eq "hyprctl dispatch global quickshell:lock" "$(tr -d '\n' <"$lock_log")" "active exact End4 marker uses native lock"

: >"$lock_log"
PATH="$fake_bin:$PATH" \
  WAHRWELT_LOCK_TEST_PID="$lock_test_pid" \
  WAHRWELT_LOCK_TEST_LOG="$lock_log" \
  "$scripts_dir/lock-active.sh" end4-pc
assert_eq "hyprlock -c $wahrwelt_hypr_runtime_dir/hyprlock.conf" "$(tr -d '\n' <"$lock_log")" "mismatched End4 marker uses hyprlock"

kill "$lock_test_pid" 2>/dev/null || true
wait "$lock_test_pid" 2>/dev/null || true
lock_test_pid=""

start_lock_fixture="$test_root/start-shell-lock"
mkdir -p "$start_lock_fixture/runtime" "$start_lock_fixture/state"
cp -- "$scripts_dir/start-shell.sh" "$start_lock_fixture/start-shell.sh"
chmod 0755 "$start_lock_fixture/start-shell.sh"
printf '%s\n' \
  'wahrwelt_runtime_session_dir="$WAHRWELT_START_LOCK_FIXTURE/runtime"' \
  'wahrwelt_runtime_session_public_dir="$WAHRWELT_START_LOCK_FIXTURE/runtime"' \
  'wahrwelt_active_shell_state="$WAHRWELT_START_LOCK_FIXTURE/state/active-shell"' \
  'wahrwelt_log_file="$WAHRWELT_START_LOCK_FIXTURE/start-shell.log"' \
  'wahrwelt_hypr_runtime_dir="$WAHRWELT_START_LOCK_FIXTURE/state/hypr-runtime"' \
  'wahrwelt_end4_variant_state="$WAHRWELT_START_LOCK_FIXTURE/state/end4-variant"' \
  'wahrwelt_user_name=tester' \
  'wahrwelt_selector_pattern=selector' \
  'wahrwelt_caelestia_pattern=caelestia' \
  'wahrwelt_end4_env_pattern=end4' \
  'wahrwelt_default_shell_profile=end4' \
  'wahrwelt_valid_shell_profile() { [ "$1" = end4 ]; }' \
  'wahrwelt_open_end4_upgrade_state() { :; }' \
  'wahrwelt_merge_end4_upgrade_tokens() {' \
  '  printf "%s" "$1" >"$WAHRWELT_START_LOCK_FIXTURE/durable-tokens"' \
  '  printf "%s" "$1"' \
  '}' \
  'wahrwelt_read_end4_upgrade_tokens() {' \
  '  [ -f "$WAHRWELT_START_LOCK_FIXTURE/durable-tokens" ] || return 0' \
  '  cat "$WAHRWELT_START_LOCK_FIXTURE/durable-tokens"' \
  '}' \
  'wahrwelt_remove_end4_upgrade_tokens() {' \
  '  : >"$WAHRWELT_START_LOCK_FIXTURE/durable-tokens"' \
  '}' \
  'wahrwelt_enter_runtime_lock_v2() { :; }' \
  >"$start_lock_fixture/shell-runtime.sh"
printf '%s\n' 'prepare_runtime_environment() { :; }' \
  >"$start_lock_fixture/shell-runtime-env.sh"
printf '%s\n' \
  'wahrwelt_fs_scavenge() { :; }' \
  'runtime_bundle_fast_path_ready() { return 1; }' \
  'runtime_full_bundle_paths() { :; }' \
  'wahrwelt_capture_exact_path_guards() { :; }' \
  'prepare_profile_or_fallback() { :; }' \
  >"$start_lock_fixture/shell-profile-sync.sh"
printf '%s\n' \
  'kill_matching_pids() {' \
  '  printf "kill:%s:%s\n" "$1" "$2" >>"$WAHRWELT_START_LOCK_FIXTURE/lifecycle-events"' \
  '}' \
  'wait_until_stopped() {' \
  '  printf "wait:%s\n" "$1" >>"$WAHRWELT_START_LOCK_FIXTURE/lifecycle-events"' \
  '}' \
  'cleanup_legacy_end4_processes() {' \
  '  printf "%s\n" legacy-cleanup >>"$WAHRWELT_START_LOCK_FIXTURE/lifecycle-events"' \
  '  printf "%s" "$legacy_end4_upgrade_tokens" >"$WAHRWELT_START_LOCK_FIXTURE/cleanup-tokens"' \
  '  legacy_end4_upgrade_tokens="$(wahrwelt_remove_end4_upgrade_tokens "$legacy_end4_upgrade_tokens")"' \
  '  switch_transaction_active=0' \
  '  exit 0' \
  '}' \
  >"$start_lock_fixture/shell-process.sh"
printf '%s\n' \
  'wahrwelt_shell_transition_started=0' \
  'wahrwelt_shell_transition_active=0' \
  'wahrwelt_shell_transition_begin() {' \
  '  printf "%s\n" transition-begin >>"$WAHRWELT_START_LOCK_FIXTURE/lifecycle-events"' \
  '  return 1' \
  '}' \
  'wahrwelt_shell_transition_wait_covered() { return 0; }' \
  'wahrwelt_shell_transition_bridge_budget_available() {' \
  '  printf "bridge-budget:%s\n" "${1:-0}" >>"$WAHRWELT_START_LOCK_FIXTURE/lifecycle-events"' \
  '}' \
  'wahrwelt_shell_transition_wait_target_ready() { return 0; }' \
  'wahrwelt_shell_transition_wait_done() { return 0; }' \
  'wahrwelt_shell_transition_abort() { :; }' \
  'wahrwelt_shell_transition_abort_signal_safe() { :; }' \
  >"$start_lock_fixture/shell-transition-overlay.sh"

: >"$start_lock_fixture/start-shell.log"
if ! WAHRWELT_START_LOCK_FIXTURE="$start_lock_fixture" \
  "$start_lock_fixture/start-shell.sh" \
  --persist-end4-upgrade-processes 5099:99:end4-pC; then
  printf 'FAIL: persist-only End4 upgrade provenance mode touched shell lifecycle\n' >&2
  exit 1
fi
if [ "$(cat "$start_lock_fixture/durable-tokens" 2>/dev/null || true)" != '5099:99:end4-pC' ]; then
  printf 'FAIL: persist-only End4 upgrade provenance was not durable before lifecycle\n' >&2
  exit 1
fi
: >"$start_lock_fixture/durable-tokens"

printf '%s' '5101:101:ii' >"$start_lock_fixture/durable-tokens"
: >"$start_lock_fixture/lifecycle-events"

: >"$start_lock_fixture/start-shell.log"
start_lock_stderr="$start_lock_fixture/start-shell.stderr"
if ! PATH="$fake_bin:$PATH" WAHRWELT_LOCK_TEST_LOG="$lock_log" \
  WAHRWELT_START_LOCK_FIXTURE="$start_lock_fixture" \
  WAYLAND_DISPLAY=wayland-1 HYPRLAND_INSTANCE_SIGNATURE=test \
  "$start_lock_fixture/start-shell.sh" end4 2>"$start_lock_stderr"; then
  printf 'FAIL: argumentless retry did not resume durable End4 upgrade cleanup\n' >&2
  exit 1
fi
if grep -Eiq 'No such file|command not found|unbound variable' "$start_lock_stderr"; then
  printf 'FAIL: start-shell lock fixture passed through an unintended runtime error:\n' >&2
  sed 's/^/  /' "$start_lock_stderr" >&2
  exit 1
fi
if [ "$(cat "$start_lock_fixture/cleanup-tokens" 2>/dev/null || true)" != '5101:101:ii' ]; then
  printf 'FAIL: argumentless retry did not load exact durable End4 provenance\n' >&2
  exit 1
fi
if [ -s "$start_lock_fixture/durable-tokens" ]; then
  printf 'FAIL: successful argumentless retry did not consume durable provenance\n' >&2
  exit 1
fi
assert_eq $'kill:__selector__:TERM\nwait:__selector__\nbridge-budget:0\nlegacy-cleanup' \
  "$(cat "$start_lock_fixture/lifecycle-events")" \
  'start-shell lock fixture skipped a wallpaper-only transition and reached provenance cleanup'

printf 'OK end4 runtime variants\n'
