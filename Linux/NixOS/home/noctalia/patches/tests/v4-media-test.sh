#!/usr/bin/env bash
set -euo pipefail

webpinfo_bin=${WEBPINFO_BIN:-$(command -v webpinfo || true)}
test -n "$webpinfo_bin" || {
  printf 'webpinfo is required for the v4 WebP fixture test\n' >&2
  exit 1
}

fixture_dir=$(mktemp -d)
trap 'rm -r -- "$fixture_dir"' EXIT

printf '%s' 'UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoQABAAAgA0JaACdLoB+AADsAD+8MQL/yC5YXXI1/8gP+QH/ID/+PIAAAA=' \
  | base64 --decode > "$fixture_dir/static.webp"
printf '%s' 'UklGRqAAAABXRUJQVlA4WAoAAAACAAAADwAADwAAQU5JTQYAAAD/////AABBTk1GSAAAAAAAAAAAAA8AAA8AAGQAAAJWUDhMEAAAAC8PwAMABxD9r0D/AxHR/wBBTk1GRAAAAAAAAAAAAA8AAA8AAGQAAABWUDhMKwAAAC8PwAMAFyAQSNofeo0RmP/5Dw4Hito2Yvo+dJvk4dwMIvo/AfkIFMRRxREA' \
  | base64 --decode > "$fixture_dir/animated.webp"

static_animation=$("$webpinfo_bin" "$fixture_dir/static.webp" | grep -Fc 'Animation: 1' || true)
animated_animation=$("$webpinfo_bin" "$fixture_dir/animated.webp" | grep -Fc 'Animation: 1' || true)
test "$static_animation" -eq 0
test "$animated_animation" -eq 1

printf 'v4 WebP metadata fixture passed\n'
