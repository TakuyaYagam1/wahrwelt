#!/usr/bin/env bash
set -euo pipefail

qml_file=${1:?usage: test-wallpaper-lifecycle.sh WALLPAPER_QML}

fail() {
    printf 'caelestia lifecycle: %s\n' "$1" >&2
    exit 1
}

contains() {
    local needle=$1
    local label=$2

    grep -Fq -- "$needle" "$qml_file" || fail "$label"
}

absent() {
    local needle=$1
    local label=$2

    if grep -Fq -- "$needle" "$qml_file"; then
        fail "$label"
    fi
}

contains 'function restartLivePlayers(): void {' \
    'background must explicitly restart an existing live loader'
contains 'onSourceChanged: {' \
    'background must handle source transitions explicitly'
contains '        stopLivePlayers();' \
    'source transitions must stop the previous live player'
contains 'Qt.callLater(() => {' \
    'source changes must restart after loader bindings update'
contains '            restartLivePlayers();' \
    'source transitions must restart after stopping the previous player'
contains 'if (root.videoSource && videoLoader.item && videoLoader.item.startPlayback)' \
    'video-to-video transitions must restart the video player'
contains 'if (root.liveSource && !root.videoSource && animatedLoader.item && animatedLoader.item.startPlayback)' \
    'animated-image transitions must restart AnimatedImage'
contains 'function stopPlayback(): void {' \
    'live components must expose a stop operation'
contains 'function startPlayback(): void {' \
    'live components must expose a start operation'
test "$(grep -Fc 'Component.onCompleted: startPlayback()' "$qml_file")" -eq 2 \
    || fail 'both live component types must start when their loader is created'
test "$(grep -Fc 'source: root.source' "$qml_file")" -eq 2 \
    || fail 'both live component types must retain the root source binding'
absent 'source = "";' \
    'AnimatedImage source binding must not be broken during stop'
absent 'player.source = "";' \
    'MediaPlayer source binding must not be broken during stop'

printf '%s\n' 'caelestia lifecycle: pass'
