#!/usr/bin/env bash
set -uo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=Linux/dots/hypr/scripts/shell-runtime.sh
. "$script_dir/shell-runtime.sh"

original_argv=("$@")

requested_legacy_end4_upgrade_tokens=""
legacy_end4_upgrade_tokens=""
persist_end4_upgrade_only=0
case "${1:-}" in
  --legacy-direct-end4-upgrade-processes | --persist-end4-upgrade-processes)
    [ "$#" -ge 2 ] || {
      printf 'usage: %s [--legacy-direct-end4-upgrade-processes TOKENS] [PROFILE]\n' "$0" >&2
      exit 2
    }
    if [ "$1" = --persist-end4-upgrade-processes ]; then
      persist_end4_upgrade_only=1
    fi
    requested_legacy_end4_upgrade_tokens="$2"
    shift 2
    if [[ ! "$requested_legacy_end4_upgrade_tokens" =~ ^[1-9][0-9]*:[1-9][0-9]*:(ii|end4-pC)(,[1-9][0-9]*:[1-9][0-9]*:(ii|end4-pC))*$ ]]; then
      printf 'invalid direct End4 upgrade process provenance\n' >&2
      exit 2
    fi
    ;;
esac
[ "$persist_end4_upgrade_only" -eq 0 ] || [ "$#" -eq 0 ] || {
  printf 'usage: %s --persist-end4-upgrade-processes TOKENS\n' "$0" >&2
  exit 2
}
[ "$#" -le 1 ] || {
  printf 'usage: %s [--legacy-direct-end4-upgrade-processes TOKENS] [PROFILE]\n' "$0" >&2
  exit 2
}
wahrwelt_enter_runtime_lock_v2 \
  wahrwelt-shell-v2.lock 1500 1 "$0" "${original_argv[@]}"
requested_profile="${1:-}"
persistent_state_file="$wahrwelt_active_shell_state"
log_file="$wahrwelt_log_file"
hypr_runtime_dir="$wahrwelt_hypr_runtime_dir"
user_name="$wahrwelt_user_name"
selector_pattern="$wahrwelt_selector_pattern"
caelestia_pattern="$wahrwelt_caelestia_pattern"
selector_handle='__selector__'
caelestia_handle='__caelestia__'
caelestia_resizer_handle='__caelestia_resizer__'
noctalia_handle='__noctalia__'
end4_handle='__end4__'
end4_official_handle='__end4_official__'
end4_pc_handle='__end4_pc__'
end4_idle_handle='__end4_idle__'
end4_idle_config="$hypr_runtime_dir/hypridle.conf"
end4_env_pattern="$wahrwelt_end4_env_pattern"

# shellcheck source=Linux/dots/hypr/scripts/shell-runtime-env.sh
. "$script_dir/shell-runtime-env.sh"
# shellcheck source=Linux/dots/hypr/scripts/shell-profile-sync.sh
. "$script_dir/shell-profile-sync.sh"
# shellcheck source=Linux/dots/hypr/scripts/shell-process.sh
. "$script_dir/shell-process.sh"
# shellcheck source=Linux/dots/hypr/scripts/shell-transition-overlay.sh
. "$script_dir/shell-transition-overlay.sh"

if ! wahrwelt_open_end4_upgrade_state; then
  printf 'End4 upgrade process state ownership collision\n' >&2
  exit 1
fi
if [ -n "$requested_legacy_end4_upgrade_tokens" ]; then
  if ! legacy_end4_upgrade_tokens="$(
    wahrwelt_merge_end4_upgrade_tokens "$requested_legacy_end4_upgrade_tokens"
  )"; then
    printf 'Failed to persist End4 upgrade process provenance\n' >&2
    exit 1
  fi
elif ! legacy_end4_upgrade_tokens="$(wahrwelt_read_end4_upgrade_tokens)"; then
  printf 'Failed to read End4 upgrade process provenance\n' >&2
  exit 1
fi
if [ "$persist_end4_upgrade_only" -eq 1 ]; then
  exit 0
fi

prepare_runtime_environment

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$log_file"
}

resolve_profile() {
  local requested="$1"
  local stored=""

  if [ -n "$requested" ]; then
    printf '%s' "$requested"
    return 0
  fi

  if [ -f "$persistent_state_file" ]; then
    stored="$(tr -d '[:space:]' <"$persistent_state_file" 2>/dev/null || true)"
  fi

  if wahrwelt_valid_shell_profile "$stored"; then
    printf '%s' "$stored"
    return 0
  fi

  printf '%s' "$wahrwelt_default_shell_profile"
}

profile="$(resolve_profile "$requested_profile")"

if ! wahrwelt_valid_shell_profile "$profile"; then
  log "unknown shell before lock: $profile"
  exit 1
fi

hypr_dir() {
  wahrwelt_hypr_dir_path
}

runtime_bundle_snapshot_dir=""
state_snapshot_dir=""
switch_transaction_active=0
shell_processes_touched=0
profile_start_attempted=0
hypr_reload_started=0
previous=""
runtime_bundle_path_list=()
state_path_list=()
state_guard_path_list=("$persistent_state_file" "$wahrwelt_end4_variant_state")
runtime_transaction_fast_path=0
spotify_focus_guard_active=0
spotify_music_was_hidden=0
spotify_guard_addresses=()
spotify_focus_monitor_before=""
spotify_focus_window_before=""
spotify_focus_window_pid_before=""
shell_transition_restore_ready=0
shell_transition_old_shell_intact=1
shell_exit_signal_status=0

discard_switch_snapshots() {
  if [ -n "$runtime_bundle_snapshot_dir" ]; then
    if wahrwelt_fs_commit "$runtime_bundle_snapshot_dir"; then
      runtime_bundle_snapshot_dir=""
    else
      log "runtime transaction cleanup refused; exact recovery retained: $runtime_bundle_snapshot_dir"
    fi
  fi
  if [ -n "$state_snapshot_dir" ]; then
    if wahrwelt_fs_commit "$state_snapshot_dir"; then
      state_snapshot_dir=""
    else
      log "state transaction cleanup refused; exact recovery retained: $state_snapshot_dir"
    fi
  fi
}

cleanup_start_shell() {
  local exit_status=$?
  local spotify_wait_for_watcher=0
  local transition_restored=0
  local exit_signaled=0

  if [ "$shell_exit_signal_status" -ne 0 ]; then
    exit_status="$shell_exit_signal_status"
  fi
  trap - EXIT HUP INT QUIT TERM
  if [ "$exit_status" -gt 128 ] && [ "$exit_status" -le 192 ]; then
    exit_signaled=1
  fi
  if [ "$exit_signaled" -eq 1 ] &&
    { [ "$wahrwelt_shell_transition_active" -eq 1 ] ||
      [ "$wahrwelt_shell_transition_started" -eq 1 ]; }; then
    wahrwelt_shell_transition_abort_signal_safe
  fi
  [ "$shell_processes_touched" -eq 0 ] || spotify_wait_for_watcher=1
  if [ "$switch_transaction_active" -eq 1 ]; then
    if [ "$shell_processes_touched" -eq 1 ] && [ "$profile_start_attempted" -eq 1 ]; then
      cleanup_failed_profile_start "$profile"
      profile_start_attempted=0
    fi
    if rollback_switch_transaction; then
      if [ "$shell_processes_touched" -eq 1 ] && valid_profile "$previous"; then
        profile="$previous"
        if [ "$(wahrwelt_shell_family "$profile")" != end4 ]; then
          stop_end4_idle
        fi
        if start_profile_shell; then
          if [ "$hypr_reload_started" -eq 1 ]; then
            if reload_hypr; then
              transition_restored=1
            fi
          else
            transition_restored=1
          fi
        else
          log "failed to restart previous shell during transaction rollback; profile=$profile"
          cleanup_failed_profile_start "$profile"
        fi
      fi
      switch_transaction_active=0
      shell_processes_touched=0
      hypr_reload_started=0
      discard_switch_snapshots
    else
      log "shell transaction rollback failed on exit; preserving private snapshots for recovery"
    fi
  else
    discard_switch_snapshots
  fi
  if ! finish_spotify_focus_guard "$spotify_wait_for_watcher"; then
    log "failed to restore Spotify activation after shell transaction cleanup"
  fi
  if [ "$wahrwelt_shell_transition_active" -eq 1 ]; then
    if [ "$shell_transition_old_shell_intact" -eq 1 ]; then
      wahrwelt_shell_transition_wait_done ||
        log "failed to finish intact-shell transition; exact overlay was cleaned"
    elif [ "$transition_restored" -eq 1 ] || [ "$shell_transition_restore_ready" -eq 1 ]; then
      wahrwelt_shell_transition_wait_target_ready "$profile" ||
        log "restored shell was not ready before transition incoming phase"
      wahrwelt_shell_transition_wait_done ||
        log "failed to finish restored shell transition; exact overlay was cleaned"
    else
      wahrwelt_shell_transition_abort
    fi
  elif [ "$wahrwelt_shell_transition_started" -eq 1 ]; then
    wahrwelt_shell_transition_abort
  fi
}

handle_start_shell_signal() {
  shell_exit_signal_status="$1"
  exit "$shell_exit_signal_status"
}

trap cleanup_start_shell EXIT
trap 'handle_start_shell_signal 129' HUP
trap 'handle_start_shell_signal 130' INT
trap 'handle_start_shell_signal 131' QUIT
trap 'handle_start_shell_signal 143' TERM

begin_switch_transaction() {
  local planned_paths scavenge_error

  wahrwelt_capture_exact_path_guards "${state_guard_path_list[@]}" || return 1
  if ! scavenge_error="$(wahrwelt_fs_scavenge "$wahrwelt_runtime_session_public_dir" runtime 2>&1)"; then
    log "runtime scavenger preserved unknown or collided recovery: $scavenge_error"
  fi
  if ! scavenge_error="$(wahrwelt_fs_scavenge "$wahrwelt_runtime_session_public_dir" state 2>&1)"; then
    log "state scavenger preserved unknown or collided recovery: $scavenge_error"
  fi
  runtime_bundle_path_list=()
  runtime_transaction_fast_path=0
  if runtime_bundle_fast_path_ready; then
    runtime_transaction_fast_path=1
    if wahrwelt_valid_shell_profile "${previous:-}" && [ "$previous" != "$profile" ]; then
      planned_paths="$(runtime_profile_union_mutation_paths "$profile" "$previous")" || return 1
    else
      planned_paths="$(runtime_profile_mutation_paths)" || return 1
    fi
  else
    planned_paths="$(runtime_full_bundle_paths)" || return 1
  fi
  if [ -n "$planned_paths" ]; then
    mapfile -t runtime_bundle_path_list <<<"$planned_paths"
  fi
  if [ "${#runtime_bundle_path_list[@]}" -gt 0 ]; then
    runtime_bundle_snapshot_dir="$(
      wahrwelt_fs_begin runtime "$wahrwelt_runtime_session_public_dir" "${runtime_bundle_path_list[@]}"
    )" || return 1
  fi

  switch_transaction_active=1
}

restore_runtime_bundle() {
  [ "${#runtime_bundle_path_list[@]}" -gt 0 ] || return 0
  [ -n "$runtime_bundle_snapshot_dir" ] || return 1
  wahrwelt_fs_rollback "$runtime_bundle_snapshot_dir"
}

restore_original_state() {
  [ "${#state_path_list[@]}" -gt 0 ] || return 0
  [ -n "$state_snapshot_dir" ] || return 1
  wahrwelt_fs_rollback "$state_snapshot_dir"
}

rollback_switch_transaction() {
  local status=0

  restore_runtime_bundle || status=1
  restore_original_state || status=1
  return "$status"
}

wait_for_session() {
  local attempt
  for attempt in $(seq 1 40); do
    if [ -n "${WAYLAND_DISPLAY:-}" ] && [ -n "${HYPRLAND_INSTANCE_SIGNATURE:-}" ]; then
      if command -v hyprctl >/dev/null 2>&1; then
        hyprctl monitors >/dev/null 2>&1 && return 0
      else
        return 0
      fi
    fi
    sleep 0.25
  done
  log "session readiness timeout; continuing anyway"
  return 0
}

status_notifier_owner() {
  local reply owner

  command -v busctl >/dev/null 2>&1 || return 1
  reply="$(
    busctl --user --timeout=50ms call \
      org.freedesktop.DBus \
      /org/freedesktop/DBus \
      org.freedesktop.DBus \
      GetNameOwner \
      s org.kde.StatusNotifierWatcher 2>/dev/null
  )" || return 1
  owner="$(sed -n 's/^s "\(:[0-9][0-9]*\.[0-9][0-9]*\)"$/\1/p' <<<"$reply")"
  [ -n "$owner" ] || return 1
  printf '%s' "$owner"
}

wait_for_ready_status_notifier() {
  local attempt owner current readiness

  for attempt in $(seq 1 10); do
    owner="$(status_notifier_owner 2>/dev/null || true)"
    if [ -n "$owner" ]; then
      readiness="$(
        busctl --user --timeout=50ms get-property \
          "$owner" \
          /StatusNotifierWatcher \
          org.kde.StatusNotifierWatcher \
          IsStatusNotifierHostRegistered 2>/dev/null
      )" || readiness=""
      if [ "$readiness" = "b true" ]; then
        current="$(status_notifier_owner 2>/dev/null || true)"
        [ "$current" = "$owner" ] && return 0
      fi
    fi
    sleep 0.05
  done

  log "StatusNotifierWatcher readiness timeout"
  return 1
}

set_spotify_focus_on_activate() {
  local address="$1"
  local value="$2"

  hyprctl dispatch "hl.dsp.window.set_prop({ prop = \"focus_on_activate\", value = \"$value\", window = \"address:$address\" })" \
    >/dev/null 2>&1
}

begin_spotify_focus_guard() {
  local clients monitors active_window addresses address focus_monitor focus_window focus_window_pid
  local candidates=()

  if ! command -v hyprctl >/dev/null 2>&1 ||
    ! command -v jq >/dev/null 2>&1 ||
    ! command -v busctl >/dev/null 2>&1; then
    log "Spotify activation snapshot unavailable; continuing without focus guard"
    return 0
  fi
  if ! clients="$(hyprctl -j clients 2>/dev/null)" ||
    ! monitors="$(hyprctl -j monitors 2>/dev/null)" ||
    ! jq -e 'type == "array"' <<<"$clients" >/dev/null ||
    ! jq -e 'type == "array"' <<<"$monitors" >/dev/null; then
    log "Spotify activation snapshot unavailable; continuing without focus guard"
    return 0
  fi

  if jq -e 'any(.[]; (.specialWorkspace.name // "") == "special:music")' \
    <<<"$monitors" >/dev/null; then
    return 0
  fi

  addresses="$(
    jq -cer '[
      .[]
      | select((.class // "" | ascii_downcase) == "spotify")
      | select(.mapped != false)
      | select((.workspace.name // "") == "special:music")
      | (.address // "" | ascii_downcase)
    ] | unique' <<<"$clients"
  )" || {
    log "Spotify activation snapshot unavailable; continuing without focus guard"
    return 0
  }
  mapfile -t candidates < <(jq -r '.[]' <<<"$addresses")
  [ "${#candidates[@]}" -gt 0 ] || return 0

  for address in "${candidates[@]}"; do
    if [[ ! "$address" =~ ^0x[0-9a-f]+$ ]]; then
      log "Spotify activation snapshot contained an invalid address; continuing without focus guard"
      return 0
    fi
  done

  if ! active_window="$(hyprctl -j activewindow 2>/dev/null)" ||
    ! jq -e 'type == "object"' <<<"$active_window" >/dev/null; then
    log "Spotify activation snapshot unavailable; continuing without focus guard"
    return 0
  fi
  focus_monitor="$(
    jq -er '
      [.[] | select(.focused == true) | .name] as $names
      | if ($names | length) == 1 and ($names[0] | type) == "string" and ($names[0] | length) > 0 then
          $names[0]
        else
          error("focused monitor unavailable")
        end
    ' <<<"$monitors"
  )" || {
    log "Spotify activation snapshot unavailable; continuing without focus guard"
    return 0
  }
  if [[ ! "$focus_monitor" =~ ^[A-Za-z0-9_.-]+$ ]]; then
    log "Spotify activation snapshot contained an invalid monitor; continuing without focus guard"
    return 0
  fi
  focus_window="$(
    jq -er '
      if length == 0 then
        ""
      elif (.address | type) == "string" then
        (.address | ascii_downcase)
      else
        error("active window address unavailable")
      end
    ' <<<"$active_window"
  )" || {
    log "Spotify activation snapshot unavailable; continuing without focus guard"
    return 0
  }
  focus_window_pid=""
  if [ -n "$focus_window" ]; then
    if [[ ! "$focus_window" =~ ^0x[0-9a-f]+$ ]] ||
      ! focus_window_pid="$(
        jq -er '
          if (.pid | type) == "number" and .pid >= 1 and (.pid | floor) == .pid then
            .pid
          else
            error("active window pid unavailable")
          end
        ' <<<"$active_window"
      )" ||
      ! jq -e --arg address "$focus_window" --argjson pid "$focus_window_pid" '
        any(.[];
          ((.address // "" | ascii_downcase) == $address) and
          (.pid == $pid)
        )
      ' <<<"$clients" >/dev/null; then
      log "Spotify activation snapshot unavailable; continuing without focus guard"
      return 0
    fi
    for address in "${candidates[@]}"; do
      if [ "$focus_window" = "$address" ]; then
        log "Spotify activation snapshot focused the guarded window; continuing without focus guard"
        return 0
      fi
    done
  fi

  spotify_focus_monitor_before="$focus_monitor"
  spotify_focus_window_before="$focus_window"
  spotify_focus_window_pid_before="$focus_window_pid"
  spotify_music_was_hidden=1
  spotify_guard_addresses=()
  for address in "${candidates[@]}"; do
    spotify_guard_addresses+=("$address")
    spotify_focus_guard_active=1
    if ! set_spotify_focus_on_activate "$address" false; then
      log "failed to apply Spotify activation guard for address=$address"
      if ! finish_spotify_focus_guard 0; then
        log "failed to clean a partial Spotify activation guard"
        return 1
      fi
      log "continuing shell switch without Spotify activation guard"
      return 0
    fi
  done
}

finish_spotify_focus_guard() {
  local wait_for_watcher="${1:-0}"
  local clients address current_special_visible=0 same_music_window=0 status=0
  local clients_available=0
  local restore_window_setup='local restore_window = nil'
  local restore_window_check=false
  local live_addresses=()

  [ "$spotify_focus_guard_active" -eq 1 ] || return 0
  if [ "$wait_for_watcher" -eq 1 ]; then
    wait_for_ready_status_notifier || true
  fi

  if clients="$(hyprctl -j clients 2>/dev/null)" &&
    jq -e 'type == "array"' <<<"$clients" >/dev/null; then
    clients_available=1
    for address in "${spotify_guard_addresses[@]}"; do
      if jq -e --arg address "$address" '
        any(.[];
          ((.address // "" | ascii_downcase) == $address) and
          ((.class // "" | ascii_downcase) == "spotify")
        )
      ' <<<"$clients" >/dev/null; then
        live_addresses+=("$address")
        if jq -e --arg address "$address" '
          any(.[];
            ((.address // "" | ascii_downcase) == $address) and
            ((.class // "" | ascii_downcase) == "spotify") and
            ((.workspace.name // "") == "special:music")
          )
        ' <<<"$clients" >/dev/null; then
          same_music_window=1
        fi
      fi
    done
  else
    live_addresses=("${spotify_guard_addresses[@]}")
  fi

  if [ "$spotify_music_was_hidden" -eq 1 ] && [ "$same_music_window" -eq 1 ]; then
    if hyprctl -j monitors 2>/dev/null |
      jq -e 'any(.[]; (.specialWorkspace.name // "") == "special:music")' >/dev/null; then
      current_special_visible=1
    fi
    if [ "$current_special_visible" -eq 1 ]; then
      if [ -n "$spotify_focus_window_before" ]; then
        restore_window_setup="local restore_window = hl.get_window(\"address:$spotify_focus_window_before\")"
        restore_window_check="restore_window and restore_window.pid == $spotify_focus_window_pid_before and restore_window.monitor == restore_monitor"
      fi
      hyprctl eval "
        local function checked(dispatcher)
          local result = hl.dispatch(dispatcher)
          if result ~= nil and result.ok == false then
            error(result.error or \"dispatcher failed\")
          end
        end
        local restore_monitor = hl.get_monitor(\"$spotify_focus_monitor_before\")
        $restore_window_setup
        local hidden = false
        for _, monitor in ipairs(hl.get_monitors()) do
          local workspace = monitor.active_special_workspace
          if workspace and workspace.name == \"special:music\" then
            monitor:set_special_workspace({})
            hidden = true
          end
        end
        for _, monitor in ipairs(hl.get_monitors()) do
          local workspace = monitor.active_special_workspace
          if workspace and workspace.name == \"special:music\" then
            error(\"special:music remained visible after recovery\")
          end
        end
        if hidden and restore_monitor then
          checked(hl.dsp.focus({ monitor = restore_monitor }))
        end
        if hidden and $restore_window_check then
          checked(hl.dsp.focus({ window = restore_window }))
        end
      " >/dev/null 2>&1 || status=1
    fi
  fi

  for address in "${live_addresses[@]}"; do
    set_spotify_focus_on_activate "$address" unset || status=1
  done

  if [ "$clients_available" -eq 1 ] && [ "$status" -eq 0 ]; then
    spotify_guard_addresses=()
    spotify_focus_guard_active=0
    spotify_music_was_hidden=0
    spotify_focus_monitor_before=""
    spotify_focus_window_before=""
    spotify_focus_window_pid_before=""
  elif [ "$clients_available" -eq 0 ] && [ "$status" -eq 0 ]; then
    spotify_guard_addresses=()
    spotify_focus_guard_active=0
    spotify_music_was_hidden=0
    spotify_focus_monitor_before=""
    spotify_focus_window_before=""
    spotify_focus_window_pid_before=""
  fi

  return "$status"
}

clear_spotify_focus_guard_state() {
  spotify_guard_addresses=()
  spotify_focus_guard_active=0
  spotify_music_was_hidden=0
  spotify_focus_monitor_before=""
  spotify_focus_window_before=""
  spotify_focus_window_pid_before=""
}

finish_spotify_focus_guard_async() {
  local completion="${WAHRWELT_SPOTIFY_ASYNC_DONE:-}"

  if [ "$spotify_focus_guard_active" -ne 1 ]; then
    if [ -n "$completion" ]; then
      printf '%s\n' 'done' >"$completion"
    fi
    return 0
  fi
  if [ -n "$completion" ]; then
    printf '%s\n' started >"$completion.started"
  fi
  (
    if ! finish_spotify_focus_guard 1; then
      log "failed to restore Spotify activation after successful shell switch"
    fi
    if [ -n "$completion" ]; then
      printf '%s\n' 'done' >"$completion"
    fi
  ) </dev/null &
  clear_spotify_focus_guard_state
}

stop_caelestia() {
  log "stopping caelestia shell"

  if command -v caelestia >/dev/null 2>&1; then
    caelestia shell -k >/dev/null 2>&1 || true
  fi

  kill_matching_pids "$caelestia_handle" TERM
  wait_until_stopped "$caelestia_handle" >/dev/null 2>&1 || true
  stop_caelestia_resizer
}

stop_caelestia_resizer() {
  log "stopping caelestia resizer"
  kill_matching_pids "$caelestia_resizer_handle" TERM
  wait_until_stopped "$caelestia_resizer_handle" >/dev/null 2>&1 || true
}

stop_noctalia() {
  log "stopping noctalia shell"
  kill_matching_pids "$noctalia_handle" TERM
  wait_until_stopped "$noctalia_handle" >/dev/null 2>&1 || true
}

stop_shell_selector() {
  log "stopping shell selector"
  kill_matching_pids "$selector_handle" TERM
  wait_until_stopped "$selector_handle" >/dev/null 2>&1 || true
}

stop_end4() {
  log "stopping end4 shell"
  kill_matching_pids "$end4_handle" TERM
  wait_until_stopped "$end4_handle" >/dev/null 2>&1 || true
}

stop_end4_idle() {
  log "stopping end4 hypridle"
  kill_matching_pids "$end4_idle_handle" TERM
  wait_until_stopped "$end4_idle_handle" >/dev/null 2>&1 || true
}

stop_quickshells() {
  log "stopping existing shell instances"
  if command -v caelestia >/dev/null 2>&1; then
    timeout 0.25s caelestia shell -k >/dev/null 2>&1 || true
  fi
  stop_matching_group \
    "$selector_handle" "$caelestia_handle" "$caelestia_resizer_handle" \
    "$noctalia_handle" "$end4_handle" >/dev/null 2>&1 || true
}

stop_inactive_shells() {
  local requested="$1"
  local -a handles=("$selector_handle")

  case "$1" in
    caelestia)
      handles+=("$noctalia_handle" "$end4_handle")
      ;;
    noctalia)
      handles+=("$caelestia_handle" "$caelestia_resizer_handle" "$end4_handle")
      ;;
    end4 | end4-pc)
      handles+=("$caelestia_handle" "$caelestia_resizer_handle" "$noctalia_handle")
      ;;
  esac
  if [ "$(wahrwelt_shell_family "$requested")" != end4 ]; then
    handles+=("$end4_idle_handle")
  fi
  stop_matching_group "${handles[@]}" >/dev/null 2>&1
}

stop_all_shells_for_switch() {
  local requested="$1"
  local -a handles=(
    "$selector_handle" "$caelestia_handle" "$caelestia_resizer_handle"
    "$noctalia_handle" "$end4_handle"
  )

  log "stopping existing shell instances"
  if command -v caelestia >/dev/null 2>&1; then
    timeout 0.25s caelestia shell -k >/dev/null 2>&1 || true
  fi
  if [ "$(wahrwelt_shell_family "$requested")" != end4 ]; then
    handles+=("$end4_idle_handle")
  fi
  stop_matching_group "${handles[@]}" >/dev/null 2>&1
}

ensure_end4_idle() {
  local idle_config

  idle_config="$end4_idle_config"

  if [ ! -f "$idle_config" ]; then
    log "end4 hypridle config missing: $idle_config"
    return 1
  fi

  if command -v hypridle >/dev/null 2>&1; then
    start_with_retry "end4 hypridle" "$end4_idle_handle" hypridle -c "$idle_config" || return 1
    return 0
  fi

  log "hypridle command not found"
  return 1
}

reconcile_end4_video_backend() {
  local end4_quickshell_path="$1"
  local reconcile="$end4_quickshell_path/scripts/wallpapers/video-backend-reconcile.sh"

  if [ ! -x "$reconcile" ]; then
    log "End4 video backend reconciler missing: $reconcile"
    return 1
  fi

  XDG_CONFIG_HOME="$wahrwelt_config_home" \
    XDG_STATE_HOME="$wahrwelt_state_home" \
    "$reconcile"
}

guard_profile_spawn_visible_budget() {
  if wahrwelt_shell_transition_target_spawn_budget_available 1000000; then
    return 0
  fi

  log "shell transition visible budget expired before target spawn; profile=$profile"
  wahrwelt_shell_transition_abort
  return 1
}

start_profile_with_retry() {
  start_with_retry --before-attempt guard_profile_spawn_visible_budget "$@"
}

start_profile_shell() {
  case "$profile" in
    caelestia)
      dedupe_shell "caelestia" "$caelestia_handle" stop_caelestia || true
      dedupe_shell "caelestia resizer" "$caelestia_resizer_handle" stop_caelestia_resizer || true

      if command -v caelestia-shell >/dev/null 2>&1; then
        start_profile_with_retry \
          "caelestia-shell" "$caelestia_handle" caelestia-shell -d || return 1
      elif command -v caelestia >/dev/null 2>&1; then
        start_profile_with_retry \
          "caelestia" "$caelestia_handle" caelestia shell -d || return 1
      else
        log "caelestia command not found"
        return 1
      fi

      if command -v caelestia >/dev/null 2>&1 && ! is_running "$caelestia_resizer_handle"; then
        (launch_shell_process caelestia resizer -d >>"$log_file" 2>&1 &)
      fi
      ;;

    noctalia)
      dedupe_shell "noctalia" "$noctalia_handle" stop_noctalia || true

      local noctalia_cmd
      local noctalia_daemon_flag
      if noctalia_cmd="$(wahrwelt_noctalia_command)" && noctalia_daemon_flag="$(wahrwelt_noctalia_daemon_flag)"; then
        start_profile_with_retry \
          "noctalia" "$noctalia_handle" "$noctalia_cmd" "$noctalia_daemon_flag" || return 1
      else
        log "noctalia command not found"
        return 1
      fi
      ;;

    end4 | end4-pc)
      local end4_config end4_exact_handle end4_quickshell_path

      end4_config="$(wahrwelt_end4_quickshell_config "$profile")" || return 1
      if [ "$profile" = "end4-pc" ]; then
        end4_exact_handle="$end4_pc_handle"
      else
        end4_exact_handle="$end4_official_handle"
      fi

      ensure_end4_idle || return 1
      dedupe_shell "end4" "$end4_handle" stop_end4 || true

      # A single end4 family process may survive an unclean state update. Do
      # not start the other variant alongside it: replace it atomically.
      if is_running "$end4_handle" && ! is_running "$end4_exact_handle"; then
        log "replacing mismatched end4 variant with profile=$profile"
        stop_end4
      fi

      if command -v qs-end4 >/dev/null 2>&1; then
        end4_quickshell_path="$(wahrwelt_end4_quickshell_path "$profile")" || return 1
        start_profile_with_retry "end4 ($profile)" "$end4_exact_handle" \
          env \
          WAHRWELT_END4_PROFILE="$profile" \
          WAHRWELT_QS_CONFIG="$end4_quickshell_path" \
          qsConfig="$end4_quickshell_path" \
          ILLOGICAL_IMPULSE_DOTFILES_SOURCE="$wahrwelt_config_home" \
          ILLOGICAL_IMPULSE_VIRTUAL_ENV="$wahrwelt_state_home/quickshell/.venv" \
          qs-end4 -n -d -c "$end4_config" || return 1
        if ! reconcile_end4_video_backend "$end4_quickshell_path"; then
          log "End4 shell started without restoring the saved video backend; profile=$profile"
        fi
      else
        log "qs-end4 command not found"
        return 1
      fi
      ;;
  esac
}

cleanup_failed_profile_start() {
  local failed_profile="$1"

  case "$failed_profile" in
    caelestia) stop_caelestia ;;
    noctalia) stop_noctalia ;;
    end4 | end4-pc)
      stop_end4
      stop_end4_idle
      ;;
  esac
}

attempt_previous_fallback() {
  local failed_profile="$1"

  valid_profile "$previous" || return 1
  [ "$previous" != "$failed_profile" ] || return 1
  if ! rollback_switch_transaction; then
    log "failed to restore prior shell transaction before fallback profile=$previous"
    return 1
  fi

  profile="$previous"
  profile_start_attempted=0
  log "attempting fallback to previous profile=$profile"
  if [ "$(wahrwelt_shell_family "$profile")" != end4 ]; then
    stop_end4_idle
  fi
  if ! prepare_profile_or_fallback; then
    log "fallback preparation failed for profile=$profile"
    rollback_switch_transaction || log "failed to restore shell transaction after fallback preparation error"
    return 1
  fi
  profile_start_attempted=1
  if ! start_profile_shell; then
    log "fallback start failed for profile=$profile"
    cleanup_failed_profile_start "$profile"
    profile_start_attempted=0
    rollback_switch_transaction || log "failed to restore shell transaction after fallback start error"
    return 1
  fi

  if ! restore_runtime_bundle; then
    log "failed to restore prior runtime bundle after fallback profile=$profile"
    return 1
  fi
  if ! persist_profile; then
    log "failed to persist runtime shell state for fallback profile=$profile"
    restore_original_state || log "failed to restore prior shell state after fallback persistence error"
    return 1
  fi
  if ! restore_original_state; then
    log "failed to restore prior shell state after fallback profile=$profile"
    return 1
  fi

  hypr_reload_started=1
  if ! reload_hypr; then
    log "failed to reload Hyprland for fallback profile=$profile"
    return 1
  fi
  propagate_runtime_environment
  # The outer transition intentionally reports the requested profile failure
  # after a successful fallback. The v2 runtime lock kills all remaining
  # descendants on a non-zero exit, so this cleanup must finish in-process.
  if ! finish_spotify_focus_guard 1; then
    log "failed to restore Spotify activation after fallback shell switch"
  fi
  shell_transition_restore_ready=1
  hypr_reload_started=0
  profile_start_attempted=0
  shell_processes_touched=0
  switch_transaction_active=0
  discard_switch_snapshots
  return 0
}

valid_profile() {
  wahrwelt_valid_shell_profile "$1"
}

log "requested profile=$profile input=${requested_profile:-auto}"
wait_for_session

if [ -f "$persistent_state_file" ]; then
  previous="$(tr -d '[:space:]' <"$persistent_state_file" 2>/dev/null || true)"
fi

if [ -n "$requested_profile" ]; then
  stop_shell_selector
  if valid_profile "$previous" &&
    wahrwelt_shell_transition_profile_running "$previous"; then
    if ! wahrwelt_shell_transition_begin "$profile"; then
      log "shell transition capture unavailable; continuing without animation"
    fi
  else
    log "shell transition skipped because previous profile has no running process; previous=${previous:-absent}"
  fi
fi

if ! begin_switch_transaction; then
  log "unable to snapshot shell runtime transaction; profile=$profile"
  exit 1
fi

if ! prepare_profile_or_fallback; then
  log "aborting shell switch before stopping current shell; profile=$profile"
  rollback_switch_transaction || log "failed to restore shell transaction after preparation error"
  exit 1
fi

if ! begin_spotify_focus_guard; then
  log "aborting shell switch before stopping current shell; Spotify activation guard failed"
  exit 1
fi

if [ "$wahrwelt_shell_transition_active" -eq 1 ]; then
  if ! wahrwelt_shell_transition_wait_covered; then
    log "shell transition cover unavailable; disabling optional animation and continuing shell switch"
    if [ "$wahrwelt_shell_transition_active" -eq 1 ] ||
      [ "$wahrwelt_shell_transition_started" -eq 1 ]; then
      wahrwelt_shell_transition_abort
    fi
  fi
fi

if ! wahrwelt_shell_transition_bridge_budget_available 0; then
  log "shell transition bridge budget cannot cover destructive shell swap; current shell remains active"
  exit 1
fi

if [ -n "$legacy_end4_upgrade_tokens" ]; then
  shell_processes_touched=1
  shell_transition_old_shell_intact=0
  if ! cleanup_legacy_end4_processes; then
    log "aborting shell switch after pre-marker end4 cleanup failure; profile=$profile"
    exit 1
  fi
fi

shell_processes_touched=1
shell_transition_old_shell_intact=0
if [ "$previous" != "$profile" ] || [ -n "$requested_profile" ]; then
  if ! stop_all_shells_for_switch "$profile"; then
    log "aborting shell switch because an existing shell survived shutdown; profile=$profile"
    exit 1
  fi
else
  if ! stop_inactive_shells "$profile"; then
    log "aborting shell reuse because an inactive shell survived shutdown; profile=$profile"
    exit 1
  fi
fi

profile_start_attempted=1
if ! start_profile_shell; then
  failed_profile="$profile"
  log "shell start failed for profile=$failed_profile; active state was not changed"
  cleanup_failed_profile_start "$failed_profile"
  profile_start_attempted=0
  if attempt_previous_fallback "$failed_profile"; then
    exit 1
  fi
  rollback_switch_transaction || log "failed to restore shell transaction after start error"
  exit 1
fi

if ! persist_profile; then
  failed_profile="$profile"
  log "failed to persist runtime shell state for profile=$profile"
  if [ "$previous" != "$failed_profile" ]; then
    stop_quickshells
    cleanup_failed_profile_start "$failed_profile"
    profile_start_attempted=0
    if valid_profile "$previous" && attempt_previous_fallback "$failed_profile"; then
      exit 1
    fi
  fi
  rollback_switch_transaction || log "failed to restore shell transaction after persistence error"
  exit 1
fi
hypr_reload_started=1
if ! reload_hypr; then
  log "failed to reload Hyprland after runtime sync; rolling back transaction"
  exit 1
fi
propagate_runtime_environment
finish_spotify_focus_guard_async
if [ "$wahrwelt_shell_transition_active" -eq 1 ]; then
  wahrwelt_shell_transition_wait_target_ready "$profile" ||
    log "shell transition target was not ready before incoming phase"
  wahrwelt_shell_transition_wait_done ||
    log "shell transition completion failed; exact overlay was cleaned"
fi
switch_transaction_active=0
profile_start_attempted=0
shell_processes_touched=0
hypr_reload_started=0
discard_switch_snapshots
