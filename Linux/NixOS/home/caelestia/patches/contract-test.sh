#!/usr/bin/env bash
set -euo pipefail

shell_source=${1:?usage: contract-test.sh SHELL_SOURCE CLI_SOURCE PACKAGE_FILE [REALIZED_SHELL]}
cli_source=${2:?usage: contract-test.sh SHELL_SOURCE CLI_SOURCE PACKAGE_FILE [REALIZED_SHELL]}
package_file=${3:?usage: contract-test.sh SHELL_SOURCE CLI_SOURCE PACKAGE_FILE [REALIZED_SHELL]}
realized_shell=${4:-}

fail() {
    printf 'caelestia contract: %s\n' "$1" >&2
    exit 1
}

contains() {
    local file=$1
    local needle=$2
    local label=$3

    grep -Fq -- "$needle" "$file" || fail "$label"
}

absent() {
    local file=$1
    local needle=$2
    local label=$3

    if grep -Fq -- "$needle" "$file"; then
        fail "$label"
    fi
}

wallpaper_qml="$shell_source/modules/background/Wallpaper.qml"
service_qml="$shell_source/services/Wallpapers.qml"
wallpaper_py="$cli_source/src/caelestia/utils/wallpaper.py"

contains "$wallpaper_qml" 'AnimatedImage' \
    'background wallpaper must render animated images without a player'
contains "$wallpaper_qml" 'MediaPlayer' \
    'background wallpaper must own a media player for video files'
contains "$wallpaper_qml" 'VideoOutput' \
    'background wallpaper must route video frames to the output'
contains "$wallpaper_qml" 'PreserveAspectCrop' \
    'background wallpaper must crop live media to fill the output'
contains "$service_qml" 'Static' \
    'wallpaper selector must expose the Static filter'
contains "$service_qml" 'Live' \
    'wallpaper selector must expose the Live filter'
contains "$service_qml" 'All' \
    'wallpaper selector must expose the All filter'
contains "$service_qml" 'Paths.wallsdir' \
    'wallpaper discovery must use the shared Pictures/Wallpapers directory'
contains "$wallpaper_py" 'VIDEO_EXTENSIONS' \
    'CLI must classify video formats by content-aware extension rules'
contains "$wallpaper_py" 'is_animated_webp' \
    'CLI must distinguish animated WebP from static WebP'
contains "$wallpaper_py" 'wallpaper_path_path' \
    'CLI must persist one current wallpaper path for all outputs'

test -f "$package_file" || fail 'package.nix must be the Caelestia package entrypoint'
contains "$package_file" 'thumbnailTool' \
    'package must keep the thumbnail helper available internally'
contains "$package_file" 'inherit cli;' \
    'package must export the native CLI'
contains "$package_file" 'shell = shellBase.overrideAttrs' \
    'package must export the patched shell'
contains "$package_file" 'name = "update-caelestia-live-thumbs";' \
    'package helper name must match its executable output'
absent "$package_file" 'inherit thumbnailTool' \
    'thumbnail helper must remain private to the shell derivation'
contains "$package_file" '--fuzz=0' \
    'package patches must fail on context drift'

if [ -n "$realized_shell" ]; then
    realized_wallpapers="$realized_shell/share/caelestia-shell/services/Wallpapers.qml"
    test -f "$realized_wallpapers" || fail 'realized shell must contain patched Wallpapers.qml'

    helper_path=$(sed -n \
        's#^[[:space:]]*command: \["\([^" ]*/bin/update-caelestia-live-thumbs\)", root.wallpaperDirectory\]$#\1#p' \
        "$realized_wallpapers")
    test -n "$helper_path" || fail 'realized QML must contain the substituted thumbnail helper path'
    test -x "$helper_path" || fail 'substituted thumbnail helper path must be executable'

    expected_command="        command: [\"$helper_path\", root.wallpaperDirectory]"
    test "$(grep -Fxc -- "$expected_command" "$realized_wallpapers")" -eq 1 \
        || fail 'realized QML must use the exact executable thumbnail helper path'
    absent "$realized_wallpapers" '__CAELESTIA_THUMBNAIL_TOOL__' \
        'realized QML must not retain the thumbnail helper placeholder'
fi

printf '%s\n' 'caelestia contract: pass'
