#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../../.." && pwd)"

WAHRWELT_NIXOS_DIR="$repo_root/Linux/NixOS" \
  nix eval --no-write-lock-file --impure --raw --file "$script_dir/nix-package-preset-test.nix"

printf '\nOK cumulative package presets and desktop application contract\n'
