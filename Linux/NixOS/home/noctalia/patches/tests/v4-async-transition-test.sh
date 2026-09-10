#!/usr/bin/env bash
set -euo pipefail

source_root=${1:?usage: v4-async-transition-test.sh SOURCE_ROOT}
service="$source_root/Services/UI/WallpaperService.qml"
background="$source_root/Modules/Background/Background.qml"

require_text() {
  local needle=$1
  grep -Fq -- "$needle" "$background" || {
    printf 'missing v4 async transition contract: %s\n' "$needle" >&2
    exit 1
  }
}

# The WebP probe is asynchronous. The background must observe its completion
# and re-evaluate the same path that was provisionally rendered as static.
require_text 'function onMediaKindChanged(path)'
require_text 'WallpaperService.mediaKindChanged.connect(onMediaKindChanged)'
require_text 'function reEvaluateWallpaperKind(path)'
require_text 'if (_pathStr(persisted) !== _pathStr(path))'
require_text 'setWallpaperImmediate(path)'
require_text 'futureWallpaper = path'

python3 - "$service" "$background" <<'PY'
from pathlib import Path
import sys


service_text = Path(sys.argv[1]).read_text()
background_text = Path(sys.argv[2]).read_text()


def block(text, marker):
    start = text.index(marker)
    opening = text.index("{", start)
    depth = 0
    quote = None
    escaped = False
    line_comment = False
    block_comment = False
    index = opening
    while index < len(text):
        char = text[index]
        next_char = text[index + 1] if index + 1 < len(text) else ""
        if line_comment:
            if char == "\n":
                line_comment = False
        elif block_comment:
            if char == "*" and next_char == "/":
                block_comment = False
                index += 1
        elif quote is not None:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = None
        elif char == "/" and next_char == "/":
            line_comment = True
            index += 1
        elif char == "/" and next_char == "*":
            block_comment = True
            index += 1
        elif char in "'\"" or char == chr(96):
            quote = char
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return text[start : index + 1]
        index += 1
    raise AssertionError(f"unterminated block: {marker}")


def ordered(text, *needles):
    positions = [text.index(needle) for needle in needles]
    assert positions == sorted(positions), (needles, positions)


probe = block(service_text, "function isLiveWallpaper(path)")
completed = block(background_text, "Component.onCompleted: {")
destruction = block(background_text, "Component.onDestruction: {")
helper = block(background_text, "function isCurrentWallpaperRequest(path, generation)")
initial = block(background_text, "function setWallpaperInitial(")
initial_cache = block(
    initial,
    "ImageCacheService.getLarge(wallpaperPath, targetWidth, targetHeight, function",
)
reevaluate = block(background_text, "function reEvaluateWallpaperKind(path)")
request = block(background_text, "function requestPreprocessedWallpaper(originalPath)")
request_cache = block(
    request,
    "ImageCacheService.getLarge(originalPath, targetWidth, targetHeight, function",
)
display_scales = block(background_text, "function onDisplayScalesChanged()")
debounce_timer = block(background_text, "id: debounceTimer")
startup_timer = block(background_text, "id: startupTransitionTimer")
startup = block(background_text, "function performStartupTransition(")
execute_startup = block(background_text, "function _executeStartupTransition()")
immediate = block(background_text, "function setWallpaperImmediate(source)")
live_branch = immediate[
    immediate.index("if (WallpaperService.isLiveWallpaper(source))") :
    immediate.index("stopLiveWallpaperBackend();")
]

ordered(
    probe,
    'mediaKinds[path] = "pending";',
    'const command = ["webpinfo", path];',
    "processObject.exited.connect",
    'mediaKinds[path] = animated ? "live" : "static";',
    "mediaKindChanged(path);",
)
assert "WallpaperService.mediaKindChanged.connect(onMediaKindChanged);" in completed
assert "WallpaperService.mediaKindChanged.disconnect(onMediaKindChanged);" in destruction

# Every asynchronous cache completion is fenced before it can publish a
# cached/static path. The fence itself checks both the generation and persisted
# source, not just a local transition string.
assert "generation !== wallpaperGeneration" in helper
assert "WallpaperService.getWallpaper(modelData.name)" in helper
assert "_pathStr(persisted) === _pathStr(path)" in helper
for callback in (initial_cache, request_cache):
    guard = callback.index("if (!isCurrentWallpaperRequest(")
    cached_assignment = callback.index("futureWallpaper = cachedPath")
    assert guard < cached_assignment, (guard, cached_assignment)
    assert "requestGeneration" in callback[guard : cached_assignment]
assert "if (!isCurrentWallpaperRequest(currentPath, requestGeneration))" in display_scales

# The startup and debounce timers carry the same token/path fence. Promotion
# cancels both delayed paths before selecting the live backend.
assert "const requestGeneration = startupTransitionGeneration;" in execute_startup
assert "const requestPath = startupTransitionPath;" in execute_startup
assert "if (!isCurrentWallpaperRequest(requestPath, requestGeneration))" in execute_startup
assert "startupTransitionGeneration = requestGeneration;" in startup
assert "startupTransitionPath = _pathStr(sourcePath);" in startup
assert "const requestGeneration = debounceGeneration;" in debounce_timer
assert "const requestPath = debouncePath;" in debounce_timer
assert "if (!isCurrentWallpaperRequest(requestPath, requestGeneration))" in debounce_timer
ordered(
    reevaluate,
    "wallpaperGeneration += 1;",
    "startupTransitionTimer.stop();",
    "debounceTimer.stop();",
    "futureWallpaper = path;",
    "setWallpaperImmediate(path);",
)

# Deterministic extracted-real-function ordering for the regression:
# initial request -> probe promotion -> stale cache callback.
animated_path = "/home/test/Pictures/Wallpapers/animated.webp"
cached_png = "/home/test/.cache/wallpapers/large/animated.png"
events = [
    ("initial request", initial.index("const requestGeneration")),
    ("probe promotion", reevaluate.index("wallpaperGeneration += 1;")),
    ("stale cache callback", initial_cache.index("if (!isCurrentWallpaperRequest(")),
]
assert [name for name, _ in events] == [
    "initial request",
    "probe promotion",
    "stale cache callback",
]

# The promotion's real branch owns the final path/backend. The stale callback
# can only reach the cached PNG assignment after the generation fence, so it
# cannot replace animated.webp or re-enable the static shader.
assert reevaluate.index("futureWallpaper = path;") < reevaluate.index("setWallpaperImmediate(path);")
for marker in (
    'currentWallpaper.source = "";',
    'nextWallpaper.source = "";',
    "shaderLoader.active = false;",
    "livePlayer.source = source;",
    "livePlayer.play();",
):
    assert marker in live_branch, marker
assert cached_png.endswith(".png")
assert animated_path.endswith("animated.webp")
assert "cachedPath" not in live_branch
print(
    "v4 async ordering passed: initial -> probe promotion -> stale cache ignored; "
    f"final path={animated_path}, backend=live, cached path={cached_png} rejected"
)
PY

printf 'v4 async WebP transition regression contract passed\n'
