#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=Linux/dots/hypr/scripts/shell-runtime.sh
. "$script_dir/shell-runtime.sh"

if [ "$#" -gt 1 ]; then
  printf 'usage: %s [end4-profile]\n' "$0" >&2
  exit 2
fi

profile="${1:-}"
if [ -z "$profile" ]; then
  profile="$(wahrwelt_read_active_shell 2>/dev/null || true)"
fi

if wahrwelt_valid_end4_variant "$profile" && wahrwelt_end4_profile_running "$profile"; then
  if hyprctl dispatch 'hl.dsp.global("quickshell:lock")' >/dev/null; then
    exit 0
  fi
fi

exec hyprlock -c "$wahrwelt_hypr_runtime_dir/hyprlock.conf"
