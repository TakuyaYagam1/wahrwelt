#!/usr/bin/env bash
set -euo pipefail

source_root=${1:?usage: v4-contract.sh SOURCE_ROOT}
service="$source_root/Services/UI/WallpaperService.qml"
panel="$source_root/Modules/Panels/Wallpaper/WallpaperPanel.qml"
background="$source_root/Modules/Background/Background.qml"
cache="$source_root/Services/UI/ImageCacheService.qml"

require_text() {
  local file=$1
  local needle=$2
  if ! grep -Fq -- "$needle" "$file"; then
    printf 'missing v4 contract: %s in %s\n' "$needle" "$file" >&2
    exit 1
  fi
}

for format in '.gif' '.webp' '.mp4' '.webm' '.mkv' '.mov' '.avi'; do
  require_text "$service" "$format"
done

require_text "$service" 'Static'
require_text "$service" 'Live'
require_text "$service" 'All'
require_text "$service" 'animated WebP'
require_text "$service" 'String.fromCharCode(10)'
require_text "$service" 'webpinfo'
require_text "$panel" 'mediaFilter'
require_text "$panel" 'selectItem(path, undefined)'
require_text "$background" 'MediaPlayer'
require_text "$background" 'function stopLiveWallpaperBackend'
require_text "$background" 'stopLiveWallpaperBackend();'
require_text "$background" 'VideoOutput.PreserveAspectCrop'
require_text "$background" 'loops: MediaPlayer.Infinite'
require_text "$cache" 'ffmpeg'
require_text "$cache" 'first-frame'

printf 'v4 live wallpaper contract passed\n'
