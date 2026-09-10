#!/usr/bin/env bash
set -euo pipefail

size="x-large"
directory=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --size)
      [ "$#" -ge 2 ] || exit 64
      size="$2"
      shift 2
      ;;
    --directory)
      [ "$#" -ge 2 ] || exit 64
      directory="$2"
      shift 2
      ;;
    *)
      exit 64
      ;;
  esac
done

[ -d "$directory" ] || exit 0
case "$size" in
  normal) pixels=128 ;;
  large) pixels=256 ;;
  x-large) pixels=512 ;;
  xx-large) pixels=1024 ;;
  *) exit 64 ;;
esac

cache_home="${XDG_CACHE_HOME:-$HOME/.cache}"
cache_dir="$cache_home/thumbnails/$size"
mkdir -p "$cache_dir"

thumbnail_name() {
  python3 - "$1" <<'PY'
import hashlib
import sys
from urllib.parse import quote

path = sys.argv[1]
encoded = "/".join(quote(part, safe="") for part in path.split("/"))
print(hashlib.md5(f"file://{encoded}".encode(), usedforsecurity=False).hexdigest() + ".png")
PY
}

for file in "$directory"/*; do
  [ -f "$file" ] || continue
  case "${file##*.}" in
    gif|GIF|webp|WEBP|mp4|MP4|webm|WEBM|mkv|MKV|mov|MOV|avi|AVI) ;;
    *) continue ;;
  esac
  output="$cache_dir/$(thumbnail_name "$file")"
  [ -s "$output" ] && continue
  temporary="$output.tmp.$$.png"
  lower_file="${file,,}"
  if [[ "$lower_file" =~ \.(mp4|webm|mkv|mov|avi)$ ]]; then
    ffmpeg -v error -y -i "$file" -frames:v 1 -vf "scale='min($pixels,iw)':-2" -f image2 "$temporary" || true
  else
    magick "${file}[0]" -auto-orient -thumbnail "${pixels}x${pixels}>" "$temporary" || true
  fi
  if [ -s "$temporary" ]; then
    mv -f -- "$temporary" "$output"
  else
    rm -f -- "$temporary"
  fi
done
