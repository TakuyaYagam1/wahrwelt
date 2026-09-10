#!/usr/bin/env bash
# shellcheck disable=SC2034

wahrwelt_runtime_directory_token() {
  local path="$1"

  command -v python3 >/dev/null 2>&1 || return 1
  python3 -I -S - "$path" <<'PY'
import os
import stat
import sys

path = os.fsencode(sys.argv[1])
if not os.path.isabs(path):
    raise SystemExit("XDG_RUNTIME_DIR must be absolute")
if path.startswith(b"//") or os.path.normpath(path) != path:
    raise SystemExit("XDG_RUNTIME_DIR must use one canonical lexical path")
flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
try:
    fd = os.open(path, flags)
except OSError as error:
    raise SystemExit(f"cannot open XDG_RUNTIME_DIR without following links: {error}")
try:
    info = os.fstat(fd)
    visible = os.lstat(path)
    if not stat.S_ISDIR(info.st_mode):
        raise SystemExit("XDG_RUNTIME_DIR is not an ordinary directory")
    if (visible.st_dev, visible.st_ino) != (info.st_dev, info.st_ino):
        raise SystemExit("XDG_RUNTIME_DIR changed while validating")
    if info.st_uid != os.getuid():
        raise SystemExit("XDG_RUNTIME_DIR is not owned by the current user")
    if stat.S_IMODE(info.st_mode) != 0o700:
        raise SystemExit("XDG_RUNTIME_DIR mode is not 0700")
    print(f"{info.st_dev}:{info.st_ino}:{info.st_uid}:700")
finally:
    os.close(fd)
PY
}

wahrwelt_open_validated_runtime_directory() {
  local path="$1"
  local token pinned opened visible

  token="$(wahrwelt_runtime_directory_token "$path")" || return 1
  if declare -F wahrwelt_after_runtime_directory_token_hook >/dev/null 2>&1; then
    wahrwelt_after_runtime_directory_token_hook "$path" "$token" || return 1
  fi
  exec {wahrwelt_runtime_session_fd}<"$path" || return 1
  pinned="/proc/${BASHPID:-$$}/fd/$wahrwelt_runtime_session_fd"
  opened="$(stat -Lc '%d:%i:%u:%a' -- "$pinned" 2>/dev/null || true)"
  visible="$(stat -c '%d:%i:%u:%a' -- "$path" 2>/dev/null || true)"
  if [ "$opened" != "$token" ] || [ "$visible" != "$token" ] || [ -L "$path" ]; then
    exec {wahrwelt_runtime_session_fd}<&-
    wahrwelt_runtime_session_fd=""
    return 1
  fi
  wahrwelt_runtime_session_pinned_dir="$pinned"
  if declare -F wahrwelt_after_runtime_directory_pin_hook >/dev/null 2>&1; then
    wahrwelt_after_runtime_directory_pin_hook "$path" "$pinned" "$token" || {
      exec {wahrwelt_runtime_session_fd}<&-
      wahrwelt_runtime_session_fd=""
      wahrwelt_runtime_session_pinned_dir=""
      return 1
    }
  fi
}

wahrwelt_managed_regular_fd=""
wahrwelt_managed_regular_path=""
wahrwelt_managed_regular_identity=""
wahrwelt_managed_regular_marker_identity=""

# Create or open one managed regular leaf beneath a retained directory FD.
# Python performs the nofollow open and returns an inode token before Bash
# reopens the file without truncation. Only the retained /proc FD spelling is
# handed to callers, so later public-name replacement cannot redirect I/O.
wahrwelt_open_managed_regular_file() {
  local parent_fd="$1"
  local parent_pinned="$2"
  local name="$3"
  local kind="$4"
  local record identity created marker_identity extra opened_path opened visible

  wahrwelt_managed_regular_fd=""
  wahrwelt_managed_regular_path=""
  wahrwelt_managed_regular_identity=""
  wahrwelt_managed_regular_marker_identity=""
  command -v python3 >/dev/null 2>&1 || return 1
  record="$(
    python3 -I -S - "$parent_fd" "$name" "$kind" <<'PY'
import ctypes
import errno
import os
import re
import stat
import sys
import fcntl
import time

parent_fd = os.dup(int(sys.argv[1]))
name = sys.argv[2]
kind = sys.argv[3]
if not name or "/" in name or name in (".", ".."):
    raise SystemExit("invalid managed regular filename")
if not re.fullmatch(r"[A-Za-z0-9._-]+", kind):
    raise SystemExit("invalid managed regular kind")
marker = ".wahrwelt-owner." + name
flags = os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK
created = False

transaction_fd = os.open(
    ".", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent_fd
)
for attempt in range(200):
    try:
        fcntl.flock(transaction_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        break
    except BlockingIOError:
        if attempt == 199:
            raise OSError("managed regular ownership transaction stayed busy")
        time.sleep(0.005)

def present(entry):
    try:
        os.stat(entry, dir_fd=parent_fd, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False

def token(info):
    return f"{info.st_dev}:{info.st_ino}:{info.st_uid}:{stat.S_IMODE(info.st_mode):o}:{info.st_nlink}"

libc = ctypes.CDLL(None, use_errno=True)
linkat = libc.linkat
linkat.argtypes = [
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
]
linkat.restype = ctypes.c_int

try:
    for attempt in range(40):
        file_present = present(name)
        marker_present = present(marker)
        if file_present and marker_present:
            fd = os.open(name, flags, dir_fd=parent_fd)
            break
        if not file_present and not marker_present:
            try:
                fd = os.open(name, flags | os.O_CREAT | os.O_EXCL, 0o600, dir_fd=parent_fd)
                created = True
                break
            except FileExistsError:
                pass
        if attempt == 39:
            raise OSError("managed regular ownership marker collision")
        time.sleep(0.005)
    try:
        info = os.fstat(fd)
        visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
        mode = stat.S_IMODE(info.st_mode)
        if created:
            os.fchmod(fd, 0o600)
            info = os.fstat(fd)
            mode = stat.S_IMODE(info.st_mode)
        if (
            not stat.S_ISREG(info.st_mode)
            or not stat.S_ISREG(visible.st_mode)
            or info.st_uid != os.getuid()
            or info.st_nlink != 1
            or mode & 0o022
            or mode & 0o600 != 0o600
            or (visible.st_dev, visible.st_ino) != (info.st_dev, info.st_ino)
        ):
            raise OSError("managed regular file is not private and owner-controlled")
        file_token = token(info)
        marker_value = f"Wahrwelt managed regular v1\nkind={kind}\ninode={info.st_dev}:{info.st_ino}\n".encode()
        marker_flags = os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK
        if created:
            marker_fd = os.open(
                ".",
                os.O_WRONLY | os.O_TMPFILE | os.O_CLOEXEC,
                0o600,
                dir_fd=parent_fd,
            )
            offset = 0
            while offset < len(marker_value):
                offset += os.write(marker_fd, marker_value[offset:])
            os.fchmod(marker_fd, 0o600)
            os.fsync(marker_fd)
            if os.environ.get("WAHRWELT_TEST_MANAGED_MARKER_FAIL_BEFORE_LINK_KIND") == kind:
                raise OSError("injected managed marker publication failure")
            if linkat(marker_fd, b"", parent_fd, os.fsencode(marker), 0x1000) == 0:
                marker_created = True
                os.fsync(parent_fd)
            else:
                error = ctypes.get_errno()
                os.close(marker_fd)
                marker_fd = None
                if error != errno.EEXIST:
                    raise OSError(error, os.strerror(error))
                marker_fd = os.open(marker, marker_flags | os.O_RDONLY, dir_fd=parent_fd)
                marker_created = False
        else:
            marker_fd = os.open(marker, marker_flags | os.O_RDONLY, dir_fd=parent_fd)
            marker_created = False
        try:
            marker_info = os.fstat(marker_fd)
            marker_visible = os.stat(marker, dir_fd=parent_fd, follow_symlinks=False)
            if not marker_created:
                value = os.read(marker_fd, 4097)
                if value != marker_value:
                    raise OSError("managed regular ownership marker content mismatch")
            if (
                not stat.S_ISREG(marker_info.st_mode)
                or not stat.S_ISREG(marker_visible.st_mode)
                or marker_info.st_uid != os.getuid()
                or marker_info.st_nlink != 1
                or stat.S_IMODE(marker_info.st_mode) != 0o600
                or (marker_visible.st_dev, marker_visible.st_ino)
                != (marker_info.st_dev, marker_info.st_ino)
            ):
                raise OSError("managed regular ownership marker is unsafe")
            print(f"{file_token}\t{1 if created else 0}\t{token(marker_info)}")
        finally:
            os.close(marker_fd)
    finally:
        os.close(fd)
finally:
    os.close(parent_fd)
PY
  )" || return 1
  IFS=$'\t' read -r identity created marker_identity extra <<<"$record"
  [ -n "$identity" ] && { [ "$created" = 0 ] || [ "$created" = 1 ]; } &&
    [ -n "$marker_identity" ] && [ -z "${extra:-}" ] || return 1
  if declare -F wahrwelt_after_managed_regular_token_hook >/dev/null 2>&1; then
    wahrwelt_after_managed_regular_token_hook "$kind" "$parent_pinned/$name" "$identity" "$created" || return 1
  fi
  # Opening read-write has no truncation side effect. A raced symlink or other
  # replacement is rejected before this descriptor is exposed to any writer.
  exec {wahrwelt_managed_regular_fd}<>"$parent_pinned/$name" || return 1
  opened_path="/proc/${BASHPID:-$$}/fd/$wahrwelt_managed_regular_fd"
  opened="$(stat -Lc '%d:%i:%u:%a:%h' -- "$opened_path" 2>/dev/null || true)"
  visible="$(stat -c '%d:%i:%u:%a:%h' -- "$parent_pinned/$name" 2>/dev/null || true)"
  if [ "$opened" != "$identity" ] || [ "$visible" != "$identity" ] || [ -L "$parent_pinned/$name" ] ||
    ! python3 -I -S - "$parent_fd" "$name" "$kind" "$identity" "$marker_identity" <<'PY'; then
import os
import stat
import sys

parent_fd = int(sys.argv[1])
name = sys.argv[2]
kind = sys.argv[3]
expected_file = sys.argv[4]
expected_marker = sys.argv[5]
marker = ".wahrwelt-owner." + name

def token(info):
    return f"{info.st_dev}:{info.st_ino}:{info.st_uid}:{stat.S_IMODE(info.st_mode):o}:{info.st_nlink}"

file_fd = os.open(name, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent_fd)
marker_fd = None
try:
    info = os.fstat(file_fd)
    visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    if token(info) != expected_file or token(visible) != expected_file or not stat.S_ISREG(info.st_mode):
        raise OSError("managed regular changed before final proof")
    marker_fd = os.open(
        marker,
        os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK,
        dir_fd=parent_fd,
    )
    marker_info = os.fstat(marker_fd)
    marker_visible = os.stat(marker, dir_fd=parent_fd, follow_symlinks=False)
    value = os.read(marker_fd, 4097)
    expected_value = f"Wahrwelt managed regular v1\nkind={kind}\ninode={info.st_dev}:{info.st_ino}\n".encode()
    if (
        token(marker_info) != expected_marker
        or token(marker_visible) != expected_marker
        or not stat.S_ISREG(marker_info.st_mode)
        or value != expected_value
    ):
        raise OSError("managed regular ownership proof changed")
finally:
    if marker_fd is not None:
        os.close(marker_fd)
    os.close(file_fd)
PY
    exec {wahrwelt_managed_regular_fd}<&-
    wahrwelt_managed_regular_fd=""
    return 1
  fi
  wahrwelt_managed_regular_path="$opened_path"
  wahrwelt_managed_regular_identity="$identity"
  wahrwelt_managed_regular_marker_identity="$marker_identity"
}

# Adopt the one unmarked regular file created by pre-marker releases. The
# allowlist is deliberately exact: this is not a generic ownership claim.

wahrwelt_runtime_script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
wahrwelt_v1_to_v2_runtime_module="${WAHRWELT_V1_TO_V2_RUNTIME_MODULE:-$wahrwelt_runtime_script_dir/migrations/v1_to_v2/runtime.sh}"
wahrwelt_v1_to_v2_runtime_loaded=0

wahrwelt_load_v1_to_v2_runtime() {
  case "$wahrwelt_v1_to_v2_runtime_loaded" in
    1) return 0 ;;
    loading) return 1 ;;
  esac
  [ -r "$wahrwelt_v1_to_v2_runtime_module" ] || {
    printf 'Wahrwelt v1_to_v2 runtime module is unavailable: %s\n' "$wahrwelt_v1_to_v2_runtime_module" >&2
    return 1
  }
  wahrwelt_v1_to_v2_runtime_loaded=loading
  # shellcheck source=Linux/dots/hypr/scripts/migrations/v1_to_v2/runtime.sh
  # shellcheck disable=SC1090
  . "$wahrwelt_v1_to_v2_runtime_module" || {
    wahrwelt_v1_to_v2_runtime_loaded=0
    return 1
  }
  wahrwelt_v1_to_v2_runtime_loaded=1
}

# Load the pre-marker log adopter only when an unmarked node is visible.
wahrwelt_adopt_legacy_managed_regular_file() {
  local parent_pinned="$2"
  local name="$3"
  local marker="$parent_pinned/.wahrwelt-owner.$name"

  if { [ ! -e "$parent_pinned/$name" ] && [ ! -L "$parent_pinned/$name" ]; } ||
    [ -e "$marker" ] || [ -L "$marker" ]; then
    return 0
  fi
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_adopt_legacy_managed_regular_file "$@"
}

wahrwelt_config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
wahrwelt_runtime_session_public_dir="${XDG_RUNTIME_DIR:-}"
if [ -z "$wahrwelt_runtime_session_public_dir" ]; then
  printf 'Wahrwelt runtime ownership collision: XDG_RUNTIME_DIR is required; TMPDIR fallback is disabled\n' >&2
  exit 1
fi
wahrwelt_runtime_session_fd=""
wahrwelt_runtime_session_pinned_dir=""
if ! wahrwelt_open_validated_runtime_directory "$wahrwelt_runtime_session_public_dir"; then
  printf 'Wahrwelt runtime ownership collision: unsafe XDG_RUNTIME_DIR preserved: %s\n' \
    "$wahrwelt_runtime_session_public_dir" >&2
  exit 1
fi
wahrwelt_runtime_session_dir="$wahrwelt_runtime_session_pinned_dir"
wahrwelt_state_home="${XDG_STATE_HOME:-$HOME/.local/state}"
wahrwelt_hypr_dir="$wahrwelt_config_home/hypr"
wahrwelt_state_dir="$wahrwelt_state_home/wahrwelt"
wahrwelt_hypr_runtime_dir="$wahrwelt_state_dir/hypr-runtime"
wahrwelt_active_shell_state="$wahrwelt_state_dir/active-shell"
wahrwelt_end4_variant_state="$wahrwelt_state_dir/end4-variant"
wahrwelt_log_fd=""
wahrwelt_log_file=""
if ! wahrwelt_adopt_legacy_managed_regular_file \
  "$wahrwelt_runtime_session_fd" "$wahrwelt_runtime_session_dir" wahrwelt-shell.log shell-log; then
  printf 'Wahrwelt runtime ownership collision: unsafe pre-marker log preserved without mutation: %s/wahrwelt-shell.log\n' \
    "$wahrwelt_runtime_session_public_dir" >&2
  exit 1
fi
if ! wahrwelt_open_managed_regular_file \
  "$wahrwelt_runtime_session_fd" "$wahrwelt_runtime_session_dir" wahrwelt-shell.log shell-log; then
  printf 'Wahrwelt runtime ownership collision: managed log preserved without mutation: %s/wahrwelt-shell.log\n' \
    "$wahrwelt_runtime_session_public_dir" >&2
  exit 1
fi
if ! exec {wahrwelt_log_fd}<&"$wahrwelt_managed_regular_fd"; then
  printf 'Wahrwelt runtime ownership collision: managed log descriptor could not be retained\n' >&2
  exit 1
fi
wahrwelt_log_file="/proc/${BASHPID:-$$}/fd/$wahrwelt_log_fd"
exec {wahrwelt_managed_regular_fd}<&-
wahrwelt_managed_regular_fd=""
wahrwelt_default_shell_profile="caelestia"

wahrwelt_selector_pattern='((^|[ /])(qs|quickshell)([[:space:]].*)?-c[[:space:]]wahrwelt-shell-selector([[:space:]]|$))|quickshell/wahrwelt-shell-selector([/[:space:]]|$)'
wahrwelt_noctalia_v5_pattern='(^|/)(noctalia|\.noctalia-wrapped_?)([[:space:]]|$)'
wahrwelt_noctalia_v4_pattern='(^|[ /])noctalia-shell([[:space:]]|$)|share/noctalia-shell'
wahrwelt_caelestia_pattern='share/caelestia-shell|caelestia-shell|(^|[ /])caelestia[[:space:]]+shell([[:space:]]|$)'
wahrwelt_noctalia_v4_env_pattern='^QS_CONFIG_PATH=.*/share/noctalia-shell$'
wahrwelt_end4_official_env_pattern='^WAHRWELT_END4_PROFILE=end4$'
wahrwelt_end4_pc_env_pattern='^WAHRWELT_END4_PROFILE=end4-pc$'
wahrwelt_end4_env_pattern='^WAHRWELT_END4_PROFILE=(end4|end4-pc)$'

wahrwelt_user_name="${USER:-}"
if [ -z "$wahrwelt_user_name" ]; then
  wahrwelt_user_name="$(id -un 2>/dev/null || printf '%s' user)"
fi

wahrwelt_hypr_dir_path() {
  printf '%s' "$wahrwelt_hypr_dir"
}

wahrwelt_runtime_file() {
  printf '%s/%s' "$wahrwelt_hypr_runtime_dir" "$1"
}

wahrwelt_valid_shell_profile() {
  case "$1" in
    caelestia | noctalia | end4 | end4-pc) return 0 ;;
    *) return 1 ;;
  esac
}

wahrwelt_valid_end4_variant() {
  case "$1" in
    end4 | end4-pc) return 0 ;;
    *) return 1 ;;
  esac
}

wahrwelt_shell_family() {
  case "$1" in
    end4 | end4-pc) printf '%s' end4 ;;
    *) printf '%s' "$1" ;;
  esac
}

wahrwelt_shell_adapter() {
  case "$1" in
    noctalia) printf '%s' noctalia.keybinds ;;
    caelestia) printf '%s' caelestia.keybinds ;;
    end4 | end4-pc) printf '%s' end4-adapter ;;
    *) return 1 ;;
  esac
}

wahrwelt_shell_launcher_module() {
  case "$1" in
    noctalia) printf '%s' noctalia.launcher ;;
    caelestia) printf '%s' caelestia.launcher ;;
    end4 | end4-pc) printf '%s' end4.launcher ;;
    *) return 1 ;;
  esac
}

wahrwelt_end4_quickshell_config() {
  case "$1" in
    end4) printf '%s' ii ;;
    end4-pc) printf '%s' end4-pC ;;
    *) return 1 ;;
  esac
}

wahrwelt_end4_quickshell_path() {
  local qs_config

  qs_config="$(wahrwelt_end4_quickshell_config "$1")" || return 1
  printf '%s/quickshell/%s' "$wahrwelt_config_home" "$qs_config"
}

wahrwelt_read_end4_variant() {
  local path="${1:-$wahrwelt_end4_variant_state}"
  local stored=""

  if [ -f "$path" ]; then
    IFS= read -r stored <"$path" || stored=""
    if ! printf '%s\n' "$stored" | cmp -s - "$path"; then
      stored=""
    fi
  fi

  if wahrwelt_valid_end4_variant "$stored"; then
    printf '%s' "$stored"
  else
    printf '%s' end4
  fi
}

wahrwelt_noctalia_command() {
  if command -v noctalia >/dev/null 2>&1; then
    printf '%s' noctalia
    return 0
  fi

  if command -v noctalia-shell >/dev/null 2>&1; then
    printf '%s' noctalia-shell
    return 0
  fi

  return 1
}
wahrwelt_noctalia_daemon_flag() {
  local command_name

  command_name="$(wahrwelt_noctalia_command 2>/dev/null || true)"
  case "${command_name##*/}" in
    noctalia-shell)
      printf '%s' --daemonize
      ;;
    *)
      printf '%s' --daemon
      ;;
  esac
}

wahrwelt_noctalia_msg() {
  local command_name

  command_name="$(wahrwelt_noctalia_command 2>/dev/null || true)"
  [ -n "$command_name" ] || return 1
  "$command_name" msg "$@"
}

wahrwelt_noctalia_is_v4() {
  local command_name

  command_name="$(wahrwelt_noctalia_command 2>/dev/null || true)"
  [ "${command_name##*/}" = "noctalia-shell" ]
}

# v4 (noctalia-shell) uses `msg <target> <function>`; v5 (noctalia) uses a
# flat hyphenated `msg <command>` vocabulary. Translate stable semantic
# action names to whichever syntax the running binary understands.
wahrwelt_noctalia_action() {
  local action="$1"

  case "$action" in
    launcher-toggle)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg launcher toggle
      else
        wahrwelt_noctalia_msg panel-toggle launcher
      fi
      ;;
    session-menu-toggle)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg sessionMenu toggle
      else
        wahrwelt_noctalia_msg panel-toggle session
      fi
      ;;
    control-center-toggle)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg controlCenter toggle
      else
        wahrwelt_noctalia_msg panel-toggle control-center
      fi
      ;;
    notifications-clear)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg notifications dismissAll
      else
        wahrwelt_noctalia_msg notification-clear-active
      fi
      ;;
    settings-toggle)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg settings toggle
      else
        wahrwelt_noctalia_msg settings-toggle
      fi
      ;;
    lock)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg lockScreen lock
      else
        wahrwelt_noctalia_msg session lock
      fi
      ;;
    brightness-up)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg brightness increase
      else
        wahrwelt_noctalia_msg brightness-up
      fi
      ;;
    brightness-down)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg brightness decrease
      else
        wahrwelt_noctalia_msg brightness-down
      fi
      ;;
    clipboard-toggle)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg launcher clipboard
      else
        wahrwelt_noctalia_msg panel-toggle clipboard
      fi
      ;;
    emoji-toggle)
      if wahrwelt_noctalia_is_v4; then
        wahrwelt_noctalia_msg launcher emoji
      else
        wahrwelt_noctalia_msg panel-toggle launcher /emo
      fi
      ;;
    *)
      shift
      wahrwelt_noctalia_msg "$action" "$@"
      ;;
  esac
}

wahrwelt_pid_matches() {
  local pid="$1"
  local pattern="$2"

  [ -n "$pid" ] || return 1
  ps -p "$pid" -o args= 2>/dev/null | grep -E "$pattern" >/dev/null
}

wahrwelt_pid_has_env_regex() {
  local pid="$1"
  local regex="$2"
  local env_file

  [ -n "$pid" ] || return 1
  env_file="/proc/$pid/environ"
  [ -r "$env_file" ] || return 1
  grep -zqE "$regex" "$env_file" 2>/dev/null
}

wahrwelt_quickshell_pids() {
  pgrep -u "$wahrwelt_user_name" -f '(^|[ /])(qs-end4|qs|\.?quickshell(-wrapped)?)([[:space:]]|$)' 2>/dev/null || true
}

wahrwelt_noctalia_pids() {
  local pid

  {
    pgrep -u "$wahrwelt_user_name" -x noctalia 2>/dev/null || true
    pgrep -u "$wahrwelt_user_name" -f "$wahrwelt_noctalia_v5_pattern" 2>/dev/null || true
    pgrep -u "$wahrwelt_user_name" -f "$wahrwelt_noctalia_v4_pattern" 2>/dev/null || true
    for pid in $(wahrwelt_quickshell_pids); do
      if wahrwelt_pid_has_env_regex "$pid" "$wahrwelt_noctalia_v4_env_pattern"; then
        printf '%s\n' "$pid"
      fi
    done
  } | sort -u
}

wahrwelt_noctalia_running() {
  wahrwelt_noctalia_pids | grep . >/dev/null
}

wahrwelt_end4_profile_pids() {
  local profile="$1"
  local pattern pid

  case "$profile" in
    end4) pattern="$wahrwelt_end4_official_env_pattern" ;;
    end4-pc) pattern="$wahrwelt_end4_pc_env_pattern" ;;
    *) return 1 ;;
  esac

  for pid in $(wahrwelt_quickshell_pids); do
    if wahrwelt_pid_has_env_regex "$pid" "$pattern"; then
      printf '%s\n' "$pid"
    fi
  done
}

wahrwelt_end4_profile_running() {
  wahrwelt_end4_profile_pids "$1" | grep . >/dev/null
}

# Validate one PID/start-time/config token emitted by the exact Home Manager
# direct-End4 migration. Normal runtime detection never calls this path.

wahrwelt_pid_is_legacy_end4_upgrade_token() {
  [ -n "${1:-}" ] || return 1
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_pid_is_legacy_end4_upgrade_token "$@"
}

wahrwelt_legacy_end4_upgrade_pids() {
  [ -n "${1:-}" ] || return 0
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_legacy_end4_upgrade_pids "$@"
}

wahrwelt_cmdline_has_adjacent_args() {
  local cmdline_file="$1"
  local first="$2"
  local second="$3"
  local previous=""
  local argument

  [ -r "$cmdline_file" ] || return 1
  while IFS= read -r -d '' argument; do
    if [ "$previous" = "$first" ] && [ "$argument" = "$second" ]; then
      return 0
    fi
    previous="$argument"
  done <"$cmdline_file"
  return 1
}

wahrwelt_pid_has_adjacent_args() {
  local pid="$1"

  [ -n "$pid" ] || return 1
  wahrwelt_cmdline_has_adjacent_args "/proc/$pid/cmdline" "$2" "$3"
}

wahrwelt_detect_shell_adapter() {
  local path="$1"
  local marker=""

  [ -r "$path" ] || return 1
  IFS= read -r marker <"$path" || true
  case "$marker" in
    '-- Wahrwelt shell adapter: noctalia') printf '%s' noctalia ;;
    '-- Wahrwelt shell adapter: caelestia') printf '%s' caelestia ;;
    '-- Wahrwelt shell adapter: end4') printf '%s' end4 ;;
    '-- Wahrwelt shell adapter: end4-pc') printf '%s' end4-pc ;;
    *) return 1 ;;
  esac
}

wahrwelt_print_legacy_end4_entrypoint() {
  wahrwelt_valid_end4_variant "${1:-}" || return 1
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_print_legacy_end4_entrypoint "$@"
}

wahrwelt_print_canonical_runtime_entrypoint() {
  cat <<'EOF'
-- Wahrwelt canonical Hyprland runtime entrypoint
local home = os.getenv("HOME")
if home == nil then
    error("HOME is not set; cannot locate Wahrwelt Hyprland config")
end

local config_home = os.getenv("XDG_CONFIG_HOME") or (home .. "/.config")
local hypr_root = config_home .. "/hypr"
package.path = hypr_root .. "/?.lua;" .. hypr_root .. "/?/init.lua;" .. package.path
dofile(hypr_root .. "/user/hyprland.lua")
EOF
}

wahrwelt_print_legacy_user_entrypoint() {
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_print_legacy_user_entrypoint "$@"
}

wahrwelt_print_legacy_home_manager_user_entrypoint() {
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_print_legacy_home_manager_user_entrypoint "$@"
}

wahrwelt_print_home_manager_initial_entrypoint() {
  cat <<EOF
-- Active Hyprland profile: wahrwelt ($wahrwelt_default_shell_profile)
local home = os.getenv("HOME")
if home == nil then
    error("HOME is not set; cannot locate Wahrwelt Hyprland config")
end

local config_home = os.getenv("XDG_CONFIG_HOME") or (home .. "/.config")
local hypr_root = config_home .. "/hypr"
package.path = hypr_root .. "/?.lua;" .. hypr_root .. "/?/init.lua;" .. package.path
dofile(hypr_root .. "/user/hyprland.lua")
EOF
}

wahrwelt_print_stable_runtime_entrypoint() {
  local runtime_entrypoint="${1:-$wahrwelt_hypr_runtime_dir/hyprland.lua}"

  cat <<EOF
-- Generated by Wahrwelt: stable Hyprland Lua runtime entrypoint
local home = os.getenv("HOME")
if home == nil then
    error("HOME is not set; cannot locate Wahrwelt Hyprland runtime")
end

local config_home = os.getenv("XDG_CONFIG_HOME") or (home .. "/.config")
local hypr_root = config_home .. "/hypr"
package.path = hypr_root .. "/?.lua;" .. hypr_root .. "/?/init.lua;" .. package.path
dofile("$runtime_entrypoint")
EOF
}

wahrwelt_is_canonical_entrypoint() {
  local path="$1"

  [ -r "$path" ] || return 1
  wahrwelt_print_canonical_runtime_entrypoint | cmp -s - "$path" ||
    wahrwelt_print_home_manager_initial_entrypoint | cmp -s - "$path" ||
    wahrwelt_print_stable_runtime_entrypoint | cmp -s - "$path"
}

wahrwelt_is_legacy_user_entrypoint() {
  local path="${1:-}"

  [ -n "$path" ] && { [ -e "$path" ] || [ -L "$path" ]; } || return 1
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_is_legacy_user_entrypoint "$@"
}

wahrwelt_is_legacy_direct_end4_entrypoint() {
  local path="${1:-}"

  [ -n "$path" ] && { [ -e "$path" ] || [ -L "$path" ]; } || return 1
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_is_legacy_direct_end4_entrypoint "$@"
}

wahrwelt_read_active_shell() {
  local path="${1:-$wahrwelt_active_shell_state}"
  local stored=""

  if [ -f "$path" ]; then
    stored="$(tr -d '[:space:]' <"$path" 2>/dev/null || true)"
  fi

  if wahrwelt_valid_shell_profile "$stored"; then
    printf '%s' "$stored"
    return 0
  fi

  return 1
}

wahrwelt_lock_owner_running() {
  local owner_pid="$1"
  local owner_file="$2"
  local owner_name="$3"
  local owner_pattern="$4"
  local active_owner lock_dir

  lock_dir="$(dirname -- "$owner_file")"
  active_owner="$(wahrwelt_read_known_lock_field "$lock_dir" "${owner_file##*/}" 2>/dev/null || true)"
  [ "$active_owner" = "$owner_name" ] || return 1
  wahrwelt_pid_matches "$owner_pid" "$owner_pattern"
}

wahrwelt_path_identity() {
  local path="$1"

  [ -e "$path" ] || [ -L "$path" ] || return 1
  stat -Lc '%d:%i:%f:%s:%Y:%Z' -- "$path" 2>/dev/null
}

wahrwelt_lock_identity() {
  local path="$1"

  # /proc/<pid>/fd/<fd> is the deliberate descriptor-backed spelling used for
  # a directory we already pinned.  Every ordinary namespace path must still
  # reject a final symlink without following it.
  if [[ "$path" =~ ^/proc/[0-9]+/fd/[0-9]+$ ]]; then
    [ -d "$path" ] || return 1
  else
    [ -d "$path" ] && [ ! -L "$path" ] || return 1
  fi
  stat -Lc '%d:%i' -- "$path" 2>/dev/null
}

wahrwelt_opened_directory_identity() {
  local path="$1"

  [ -d "$path" ] || return 1
  stat -Lc '%d:%i' -- "$path" 2>/dev/null
}

wahrwelt_created_directory_name=""
wahrwelt_created_directory_identity=""
wahrwelt_created_directory_fd=""
wahrwelt_created_directory_path=""
# Create and pin a private directory beneath an already pinned parent. The
# creator process owns mkdir/open/fstat as one handoff and returns the exact
# inode token before the shell reopens it. A replacement is never chmodded or
# removed by this helper.
wahrwelt_create_pinned_private_directory() {
  local parent_fd="$1"
  local naming="$2"
  local value="$3"
  local kind="$4"
  local parent_pinned record name identity extra opened_path

  wahrwelt_created_directory_name=""
  wahrwelt_created_directory_identity=""
  wahrwelt_created_directory_fd=""
  wahrwelt_created_directory_path=""
  command -v python3 >/dev/null 2>&1 || return 1
  parent_pinned="/proc/${BASHPID:-$$}/fd/$parent_fd"
  record="$(
    python3 -I -S - "$parent_fd" "$naming" "$value" <<'PY'
import os
import secrets
import stat
import sys

parent_fd = os.dup(int(sys.argv[1]))
naming = sys.argv[2]
value = sys.argv[3]
try:
    parent_info = os.fstat(parent_fd)
    if not stat.S_ISDIR(parent_info.st_mode):
        raise OSError("private-directory parent is not a directory")
    if naming not in ("exact", "prefix"):
        raise OSError("invalid private-directory naming mode")
    if not value or "/" in value or value in (".", ".."):
        raise OSError("invalid private-directory name")
    names = (value,) if naming == "exact" else (
        value + secrets.token_hex(12) for _ in range(128)
    )
    for name in names:
        try:
            os.mkdir(name, 0o700, dir_fd=parent_fd)
        except FileExistsError:
            if naming == "exact":
                raise
            continue
        fd = os.open(
            name,
            os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
            dir_fd=parent_fd,
        )
        try:
            info = os.fstat(fd)
            visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
            if (
                not stat.S_ISDIR(info.st_mode)
                or not stat.S_ISDIR(visible.st_mode)
                or (visible.st_dev, visible.st_ino) != (info.st_dev, info.st_ino)
            ):
                raise OSError("private directory changed before creator handoff")
            os.fchmod(fd, 0o700)
            info = os.fstat(fd)
            print(f"{name}\t{info.st_dev}:{info.st_ino}")
            break
        finally:
            os.close(fd)
    else:
        raise OSError("private-directory creator exhausted names")
finally:
    os.close(parent_fd)
PY
  )" || return 1
  IFS=$'\t' read -r name identity extra <<<"$record"
  [ -n "$name" ] && [ -n "$identity" ] && [ -z "${extra:-}" ] || return 1
  case "$name" in
    "" | . | .. | */*) return 1 ;;
  esac
  if declare -F wahrwelt_after_private_directory_creator_hook >/dev/null 2>&1; then
    wahrwelt_after_private_directory_creator_hook "$kind" "$parent_pinned/$name" "$identity" || return 1
  fi
  exec {wahrwelt_created_directory_fd}<"$parent_pinned/$name" || return 1
  opened_path="/proc/${BASHPID:-$$}/fd/$wahrwelt_created_directory_fd"
  if [ "$(wahrwelt_opened_directory_identity "$opened_path" 2>/dev/null || true)" != "$identity" ] ||
    [ "$(wahrwelt_lock_identity "$parent_pinned/$name" 2>/dev/null || true)" != "$identity" ] ||
    [ "$(stat -Lc '%a:%u' -- "$opened_path" 2>/dev/null || true)" != "700:${UID}" ]; then
    exec {wahrwelt_created_directory_fd}<&-
    wahrwelt_created_directory_fd=""
    return 1
  fi
  wahrwelt_created_directory_name="$name"
  wahrwelt_created_directory_identity="$identity"
  wahrwelt_created_directory_path="$opened_path"
}

wahrwelt_private_state_directory_name=""
wahrwelt_private_state_directory_identity=""
wahrwelt_private_state_directory_fd=""
wahrwelt_private_state_directory_path=""
wahrwelt_private_state_directory_marker_identity=""

# Create or validate an application state directory directly below the pinned
# XDG runtime root. Existing nodes are never chmodded. The returned descriptor
# path remains authoritative if the public child name is replaced later.
wahrwelt_open_private_state_directory() {
  local name="$1"
  local kind="$2"
  local parent_fd="$wahrwelt_runtime_session_fd"
  local parent_pinned="$wahrwelt_runtime_session_dir"
  local record identity created marker_identity extra opened_path opened visible

  wahrwelt_private_state_directory_name=""
  wahrwelt_private_state_directory_identity=""
  wahrwelt_private_state_directory_fd=""
  wahrwelt_private_state_directory_path=""
  wahrwelt_private_state_directory_marker_identity=""
  command -v python3 >/dev/null 2>&1 || return 1
  record="$(
    python3 -I -S - "$parent_fd" "$name" "$kind" <<'PY'
import ctypes
import errno
import fcntl
import os
import re
import secrets
import stat
import sys
import time

parent_fd = os.dup(int(sys.argv[1]))
name = sys.argv[2]
kind = sys.argv[3]
if not name or "/" in name or name in (".", ".."):
    raise SystemExit("invalid private state directory name")
if not re.fullmatch(r"[A-Za-z0-9._-]+", kind):
    raise SystemExit("invalid private state directory kind")
marker = ".wahrwelt-state-owner"
created = False

transaction_fd = os.open(
    ".", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent_fd
)
for attempt in range(200):
    try:
        fcntl.flock(transaction_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        break
    except BlockingIOError:
        if attempt == 199:
            raise OSError("private state ownership transaction stayed busy")
        time.sleep(0.005)

def token(info):
    return f"{info.st_dev}:{info.st_ino}:{info.st_uid}:{stat.S_IMODE(info.st_mode):o}:{info.st_nlink}"

libc = ctypes.CDLL(None, use_errno=True)
renameat2 = libc.renameat2
renameat2.argtypes = [
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_uint,
]
renameat2.restype = ctypes.c_int

def open_existing():
    return os.open(
        name,
        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
        dir_fd=parent_fd,
    )

def validate_directory(fd, require_public):
    info = os.fstat(fd)
    if not stat.S_ISDIR(info.st_mode):
        raise OSError("private state node is not a directory")
    if require_public:
        visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
        if not stat.S_ISDIR(visible.st_mode) or (
            visible.st_dev,
            visible.st_ino,
        ) != (info.st_dev, info.st_ino):
            raise OSError("private state directory changed before validation")
    if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise OSError("private state directory is not owner-controlled mode 0700")
    return info

def create_unpublished_marker(fd, info):
    marker_value = (
        f"Wahrwelt private state v1\nkind={kind}\n"
        f"inode={info.st_dev}:{info.st_ino}\n"
    ).encode()
    marker_fd = os.open(
        marker,
        os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK,
        0o600,
        dir_fd=fd,
    )
    try:
        offset = 0
        while offset < len(marker_value):
            offset += os.write(marker_fd, marker_value[offset:])
        os.fchmod(marker_fd, 0o600)
        os.fsync(marker_fd)
    finally:
        os.close(marker_fd)

try:
    try:
        fd = open_existing()
    except FileNotFoundError:
        fd = None
        for _ in range(128):
            pending = f".wahrwelt-state-pending.{name}.{secrets.token_hex(12)}"
            try:
                os.mkdir(pending, 0o700, dir_fd=parent_fd)
            except FileExistsError:
                continue
            candidate_fd = os.open(
                pending,
                os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                dir_fd=parent_fd,
            )
            info = validate_directory(candidate_fd, False)
            create_unpublished_marker(candidate_fd, info)
            os.fsync(candidate_fd)
            if renameat2(
                parent_fd,
                os.fsencode(pending),
                parent_fd,
                os.fsencode(name),
                1,
            ) == 0:
                fd = candidate_fd
                created = True
                os.fsync(parent_fd)
                break
            error = ctypes.get_errno()
            os.close(candidate_fd)
            if error == errno.EEXIST:
                fd = open_existing()
                break
            raise OSError(error, os.strerror(error))
        if fd is None:
            raise OSError("private state directory staging names were exhausted")
    try:
        info = validate_directory(fd, True)
        marker_value = f"Wahrwelt private state v1\nkind={kind}\ninode={info.st_dev}:{info.st_ino}\n".encode()
        marker_flags = os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK
        marker_fd = os.open(marker, marker_flags | os.O_RDONLY, dir_fd=fd)
        try:
            marker_info = os.fstat(marker_fd)
            marker_visible = os.stat(marker, dir_fd=fd, follow_symlinks=False)
            if os.read(marker_fd, 4097) != marker_value:
                raise OSError("private state ownership marker content mismatch")
            if (
                not stat.S_ISREG(marker_info.st_mode)
                or not stat.S_ISREG(marker_visible.st_mode)
                or marker_info.st_uid != os.getuid()
                or marker_info.st_nlink != 1
                or stat.S_IMODE(marker_info.st_mode) != 0o600
                or (marker_visible.st_dev, marker_visible.st_ino)
                != (marker_info.st_dev, marker_info.st_ino)
            ):
                raise OSError("private state ownership marker is unsafe")
            print(f"{info.st_dev}:{info.st_ino}\t{1 if created else 0}\t{token(marker_info)}")
        finally:
            os.close(marker_fd)
    finally:
        os.close(fd)
finally:
    os.close(parent_fd)
PY
  )" || return 1
  IFS=$'\t' read -r identity created marker_identity extra <<<"$record"
  [ -n "$identity" ] && { [ "$created" = 0 ] || [ "$created" = 1 ]; } &&
    [ -n "$marker_identity" ] && [ -z "${extra:-}" ] || return 1
  if declare -F wahrwelt_after_private_state_directory_token_hook >/dev/null 2>&1; then
    wahrwelt_after_private_state_directory_token_hook \
      "$kind" "$parent_pinned/$name" "$identity" "$created" || return 1
  fi
  exec {wahrwelt_private_state_directory_fd}<"$parent_pinned/$name" || return 1
  opened_path="/proc/${BASHPID:-$$}/fd/$wahrwelt_private_state_directory_fd"
  opened="$(stat -Lc '%d:%i:%u:%a' -- "$opened_path" 2>/dev/null || true)"
  visible="$(stat -c '%d:%i:%u:%a' -- "$parent_pinned/$name" 2>/dev/null || true)"
  if [ "$opened" != "$identity:${UID}:700" ] || [ "$visible" != "$identity:${UID}:700" ] ||
    [ -L "$parent_pinned/$name" ] ||
    ! python3 -I -S - "$wahrwelt_private_state_directory_fd" "$kind" "$identity" "$marker_identity" <<'PY'; then
import os
import stat
import sys

directory_fd = int(sys.argv[1])
kind = sys.argv[2]
directory_identity = sys.argv[3]
expected_marker = sys.argv[4]
marker = ".wahrwelt-state-owner"

def token(info):
    return f"{info.st_dev}:{info.st_ino}:{info.st_uid}:{stat.S_IMODE(info.st_mode):o}:{info.st_nlink}"

directory_info = os.fstat(directory_fd)
if f"{directory_info.st_dev}:{directory_info.st_ino}" != directory_identity:
    raise OSError("private state directory identity changed")
fd = os.open(marker, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory_fd)
try:
    info = os.fstat(fd)
    visible = os.stat(marker, dir_fd=directory_fd, follow_symlinks=False)
    expected_value = (
        f"Wahrwelt private state v1\nkind={kind}\n"
        f"inode={directory_info.st_dev}:{directory_info.st_ino}\n"
    ).encode()
    if (
        token(info) != expected_marker
        or token(visible) != expected_marker
        or not stat.S_ISREG(info.st_mode)
        or os.read(fd, 4097) != expected_value
    ):
        raise OSError("private state ownership proof changed")
finally:
    os.close(fd)
PY
    exec {wahrwelt_private_state_directory_fd}<&-
    wahrwelt_private_state_directory_fd=""
    return 1
  fi
  wahrwelt_private_state_directory_name="$name"
  wahrwelt_private_state_directory_identity="$identity"
  wahrwelt_private_state_directory_path="$opened_path"
  wahrwelt_private_state_directory_marker_identity="$marker_identity"
}

wahrwelt_end4_upgrade_state_name="wahrwelt-end4-upgrade"

wahrwelt_open_end4_upgrade_state() {
  local path="$wahrwelt_runtime_session_dir/$wahrwelt_end4_upgrade_state_name"

  if [ -z "${requested_legacy_end4_upgrade_tokens:-}" ] &&
    [ ! -e "$path" ] && [ ! -L "$path" ]; then
    return 0
  fi
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_open_end4_upgrade_state "$@"
}

wahrwelt_read_end4_upgrade_tokens() {
  local path="$wahrwelt_runtime_session_dir/$wahrwelt_end4_upgrade_state_name"

  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    return 0
  fi
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_open_end4_upgrade_state || return 1
  wahrwelt_read_end4_upgrade_tokens "$@"
}

wahrwelt_merge_end4_upgrade_tokens() {
  [ -n "${1:-}" ] || return 0
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_open_end4_upgrade_state || return 1
  wahrwelt_merge_end4_upgrade_tokens "$@"
}

wahrwelt_remove_end4_upgrade_tokens() {
  [ -n "${1:-}" ] || return 0
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_open_end4_upgrade_state || return 1
  wahrwelt_remove_end4_upgrade_tokens "$@"
}

wahrwelt_adopt_legacy_private_state_directory() {
  local name="${1:-}"
  local kind="${2:-}"
  local path="$wahrwelt_runtime_session_dir/$name"
  local leaf marker

  case "$name:$kind" in
    wahrwelt-noctalia-launcher:noctalia-launcher-state | wahrwelt-recording:record-toggle-state | wahrwelt-shell-selector:shell-selector-state) ;;
    *) return 1 ;;
  esac
  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    return 0
  fi
  if [ -e "$path/.wahrwelt-state-owner" ] || [ -L "$path/.wahrwelt-state-owner" ]; then
    case "$name:$kind" in
      wahrwelt-noctalia-launcher:noctalia-launcher-state)
        for leaf in active interrupted; do
          marker="$path/.wahrwelt-owner.$leaf"
          if { [ -e "$path/$leaf" ] || [ -L "$path/$leaf" ]; } &&
            [ ! -e "$marker" ] && [ ! -L "$marker" ]; then
            wahrwelt_load_v1_to_v2_runtime || return 1
            wahrwelt_adopt_legacy_private_state_directory "$@"
            return
          fi
        done
        ;;
      wahrwelt-recording:record-toggle-state)
        for leaf in gpu-screen-recorder.pid gpu-screen-recorder.path gpu-screen-recorder.log; do
          marker="$path/.wahrwelt-owner.$leaf"
          if { [ -e "$path/$leaf" ] || [ -L "$path/$leaf" ]; } &&
            [ ! -e "$marker" ] && [ ! -L "$marker" ]; then
            wahrwelt_load_v1_to_v2_runtime || return 1
            wahrwelt_adopt_legacy_private_state_directory "$@"
            return
          fi
        done
        ;;
    esac
    return 0
  fi
  wahrwelt_load_v1_to_v2_runtime || return 1
  wahrwelt_adopt_legacy_private_state_directory "$@"
}

wahrwelt_lock_fs_helper_path() {
  local candidate="${WAHRWELT_FS_HELPER:-}"

  if [ -n "$candidate" ] && [ -x "$candidate" ]; then
    printf '%s' "$candidate"
    return 0
  fi
  command -v wahrwelt-fs-helper 2>/dev/null
}

# Run one exact argv under a kernel-owned abstract AF_UNIX lock. The helper
# process owns the socket and waits for the child, so exit or crash releases
# the lock without any filesystem cleanup. The inner child receives only a
# logical marker and never inherits the socket descriptor.
wahrwelt_enter_runtime_lock_v2() {
  local name="$1"
  local wait_ms="$2"
  local busy_status="$3"
  shift 3
  local helper status=0

  if [ "${WAHRWELT_RUNTIME_LOCK_V2:-}" = "$name" ] &&
    [ "${WAHRWELT_RUNTIME_LOCK_V2_ROOT:-}" = "$wahrwelt_runtime_session_public_dir" ]; then
    unset WAHRWELT_RUNTIME_LOCK_V2 WAHRWELT_RUNTIME_LOCK_V2_ROOT
    return 0
  fi
  [ "$#" -gt 0 ] || return 1
  helper="$(wahrwelt_lock_fs_helper_path)" || return 1
  "$helper" runtime-lock-run \
    --root "$wahrwelt_runtime_session_public_dir" \
    --name "$name" \
    --wait-ms "$wait_ms" \
    -- "$@" || status=$?
  if [ "$status" -eq 75 ]; then
    exit "$busy_status"
  fi
  exit "$status"
}
