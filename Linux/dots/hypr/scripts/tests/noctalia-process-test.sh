#!/usr/bin/env bash
set -euo pipefail

tests_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
scripts_dir="$(dirname -- "$tests_dir")"
test_root="$(mktemp -d)"
wrapped_pid=""

fail() {
  printf 'noctalia process test failed: %s\n' "$*" >&2
  exit 1
}

process_is_live() {
  local pid="$1"
  local state

  [ -r "/proc/$pid/stat" ] || return 1
  state="$(awk '{ print $3 }' "/proc/$pid/stat" 2>/dev/null || true)"
  [ -n "$state" ] && [ "$state" != Z ]
}

cleanup() {
  if [ -n "$wrapped_pid" ] && process_is_live "$wrapped_pid"; then
    kill -TERM "$wrapped_pid" >/dev/null 2>&1 || true
    wait "$wrapped_pid" 2>/dev/null || true
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$test_root/home" "$test_root/runtime" "$test_root/state"
chmod 0700 "$test_root/runtime"

bash -c 'exec -a "$1" bash -c "$2"' \
  bash \
  "$test_root/.noctalia-wrapped_" \
  'trap "exit 0" TERM; while :; do sleep 0.05 & wait "$!" || true; done' &
wrapped_pid=$!

detected="$({
  XDG_RUNTIME_DIR="$test_root/runtime" \
    XDG_STATE_HOME="$test_root/state" \
    HOME="$test_root/home" \
    USER="$(id -un)" \
    bash -c '. "$1"; wahrwelt_noctalia_pids' bash "$scripts_dir/shell-runtime.sh"
} | sort -u)"

printf '%s\n' "$detected" | grep -Fx -- "$wrapped_pid" >/dev/null ||
  fail "Nix wrapped Noctalia process was not detected: $wrapped_pid"

printf 'noctalia process test passed\n'
