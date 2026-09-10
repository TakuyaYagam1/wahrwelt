#!/usr/bin/env bash
set -euo pipefail

# switchwall.sh remains the sole video backend. This helper only replays its
# persisted monitor restore script after QuickShell restart or monitor changes.
config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
config_file="$config_home/illogical-impulse/config.json"
restore_script="$config_home/hypr/custom/scripts/__restore_video_wallpaper.sh"
runtime_dir="${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}"
lock_dir="$runtime_dir/end4-video-backend-reconcile.lock"

if ! mkdir "$lock_dir" 2>/dev/null; then
  if [ -r "$lock_dir/pid" ] && ! kill -0 "$(<"$lock_dir/pid")" 2>/dev/null; then
    rm -f -- "$lock_dir/pid"
    rmdir "$lock_dir" 2>/dev/null || exit 0
    mkdir "$lock_dir" 2>/dev/null || exit 0
  else
    exit 0
  fi
fi
printf '%s\n' "$$" >"$lock_dir/pid"
cleanup_lock() {
  rm -f -- "$lock_dir/pid"
  rmdir -- "$lock_dir" 2>/dev/null || true
}
trap cleanup_lock EXIT

kill_existing_video_backend() {
  if command -v pkill >/dev/null 2>&1; then
    pkill -f -9 mpvpaper || true
  fi
}

wallpaper=""
if [ -f "$config_file" ] && command -v jq >/dev/null 2>&1; then
  wallpaper="$(jq -r '.background.wallpaperPath // empty' "$config_file" 2>/dev/null || true)"
fi

case "${wallpaper,,}" in
  *.mp4|*.webm|*.mkv|*.mov|*.avi)
    [ -f "$wallpaper" ] || {
      kill_existing_video_backend
      exit 0
    }
    if [ -x "$restore_script" ]; then
      "${BASH:-bash}" "$restore_script"
    else
      kill_existing_video_backend
    fi
    ;;
  *)
    kill_existing_video_backend
    ;;
esac
