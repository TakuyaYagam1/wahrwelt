#!/usr/bin/env bash
set -euo pipefail

mode="hypr"
if [ "${1:-}" = "--quickshell" ]; then
  mode="quickshell"
  shift
fi

artifact="${1:-}"
if [ -z "$artifact" ] || [ ! -d "$artifact" ]; then
  printf 'usage: %s [--quickshell] END4_ARTIFACT\n' "$0" >&2
  exit 64
fi

if [ "$mode" = "hypr" ]; then
  for file in hyprland.lua hyprland/keybinds.lua wahrwelt/keybinds.lua hypridle.conf; do
    if [ ! -f "$artifact/$file" ]; then
      printf 'FAIL: realized End4 artifact is missing %s\n' "$file" >&2
      exit 1
    fi
  done
elif [ ! -f "$artifact/shell.qml" ] && [ ! -f "$artifact/ii/shell.qml" ]; then
  printf 'FAIL: realized End4 QuickShell artifact is missing shell.qml\n' >&2
  exit 1
fi

check_live_wallpaper_contract() {
  local variant="$1"
  local root="$2"
  local service="$root/services/Wallpapers.qml"
  local selector="$root/modules/ii/wallpaperSelector/WallpaperSelectorContent.qml"
  local directory_item="$root/modules/ii/wallpaperSelector/WallpaperDirectoryItem.qml"
  local background="$root/modules/ii/background/Background.qml"
  local surface="$root/modules/ii/background/LiveWallpaperSurface.qml"
  local switchwall="$root/scripts/colors/switchwall.sh"
  local media_index="$root/scripts/wallpapers/media-index.py"
  local first_frame="$root/scripts/wallpapers/first-frame-thumbnails.sh"
  local reconcile="$root/scripts/wallpapers/video-backend-reconcile.sh"
  local format

  # shellcheck disable=SC2016
  if grep -Fq '"${validateDirProc.nicePath}"' "$service" ||
    grep -Fq "'\${root.effectiveDirectory}'" "$service"; then
    printf 'FAIL: End4 %s wallpaper service interpolates user paths into bash -c: %s\n' \
      "$variant" "$service" >&2
    exit 1
  fi
  # shellcheck disable=SC2016
  for expected in \
    'end4-validate-directory' \
    'end4-thumbnail-generation' \
    '$1' \
    '$5'; do
    if ! grep -Fq "$expected" "$service"; then
      printf 'FAIL: End4 %s wallpaper service is missing safe argv anchor %s: %s\n' \
        "$variant" "$expected" "$service" >&2
      exit 1
    fi
  done

  for expected in \
    'orderFilePath' \
    'orderMap' \
    'saveCustomOrder' \
    'Config.setNestedValue' \
    'time_rev' \
    'name_rev' \
    'size_rev' \
    '"--sort"' \
    '"--custom-order"'; do
    if ! grep -Fq "$expected" "$service"; then
      printf 'FAIL: End4 %s pC sorting contract is missing %s: %s\n' \
        "$variant" "$expected" "$service" >&2
      exit 1
    fi
  done

  if [ ! -x "$reconcile" ]; then
    printf 'FAIL: End4 %s video backend reconciliation helper is missing: %s\n' \
      "$variant" "$reconcile" >&2
    exit 1
  fi
  # shellcheck disable=SC2016
  for expected in \
    'reconcileVideoScriptPath' \
    'reconcileVideoBackend' \
    'onScreensChanged' \
    'target: Config' \
    'onReadyChanged' \
    'Config.ready' \
    'Component.onCompleted' \
    'XDG_CONFIG_HOME' \
    '__restore_video_wallpaper.sh' \
    '"${BASH:-bash}" "$restore_script"'; do
    if ! grep -Fq "$expected" "$service" "$reconcile"; then
      printf 'FAIL: End4 %s video restart/hotplug reconciliation is missing %s\n' \
        "$variant" "$expected" >&2
      exit 1
    fi
  done

  for format in jpg jpeg png webp gif mp4 webm mkv mov avi; do
    if ! grep -Fq "\"$format\"" "$service"; then
      printf 'FAIL: End4 %s wallpaper contract is missing format %s: %s\n' \
        "$variant" "$format" "$service" >&2
      exit 1
    fi
  done

  for filter in Static Live All; do
    if ! grep -Fq "\"$filter\"" "$selector"; then
      printf 'FAIL: End4 %s wallpaper selector is missing %s filter: %s\n' \
        "$variant" "$filter" "$selector" >&2
      exit 1
    fi
  done

  for expected in \
    'Component.onCompleted: Qt.callLater(root.updateThumbnails)' \
    'onEffectiveDirectoryChanged' \
    'Wallpapers.generateThumbnail(thumbnailSizeName, Wallpapers.effectiveDirectory)'; do
    if ! grep -Fq "$expected" "$selector"; then
      printf 'FAIL: End4 %s wallpaper selector does not generate thumbnails for the active directory: missing %s in %s\n' \
        "$variant" "$expected" "$selector" >&2
      exit 1
    fi
  done

  for expected in \
    'media-index.py' \
    'first-frame-thumbnails.sh' \
    'animated_webp' \
    'content' \
    'setMediaFilter' \
    'wallpaperModel'; do
    if ! grep -Fq "$expected" "$service"; then
      printf 'FAIL: End4 %s wallpaper service is missing %s: %s\n' \
        "$variant" "$expected" "$service" >&2
      exit 1
    fi
  done

  if [ ! -x "$media_index" ] || [ ! -x "$first_frame" ] ||
    ! grep -Fq 'ANMF' "$media_index" ||
    ! grep -Fq 'VP8X' "$media_index"; then
    printf 'FAIL: End4 %s wallpaper service lacks content-based animated WebP classifier: %s\n' \
      "$variant" "$media_index" >&2
    exit 1
  fi

  for expected in \
    'AnimatedImage' \
    'stopLivePlayback' \
    'source = ""' \
    'switchwall.sh owns video playback through mpvpaper'; do
    if ! grep -Fq "$expected" "$surface"; then
      printf 'FAIL: End4 %s wallpaper renderer is missing %s: %s\n' \
        "$variant" "$expected" "$surface" >&2
      exit 1
    fi
  done

  if grep -Eq 'MediaPlayer|VideoOutput|AudioOutput|videoPlayer' "$surface"; then
    printf 'FAIL: End4 %s video has a second QML playback backend: %s\n' \
      "$variant" "$surface" >&2
    exit 1
  fi
  for expected in \
    'mpvpaper -o' \
    'no-audio loop' \
    'panscan=1.0' \
    'kill_existing_mpvpaper' \
    'remove_restore'; do
    if ! grep -Fq "$expected" "$switchwall"; then
      printf 'FAIL: End4 %s existing video backend is missing %s: %s\n' \
        "$variant" "$expected" "$switchwall" >&2
      exit 1
    fi
  done
  for expected in \
    'RESTORE_SHELL=' \
    'printf -v escaped_video_path' \
    "\$escaped_video_path"; do
    if ! grep -Fq "$expected" "$switchwall"; then
      printf 'FAIL: End4 %s restore generator is missing safe video serialization anchor %s: %s\n' \
        "$variant" "$expected" "$switchwall" >&2
      exit 1
    fi
  done
  if ! grep -Fq 'visible: active && bgRoot.wallpaperIsAnimated' "$background"; then
    printf 'FAIL: End4 %s video backend ownership is not delegated to mpvpaper: %s\n' \
      "$variant" "$background" >&2
    exit 1
  fi

  for expected in \
    'Quickshell.screens' \
    'Config.options.background.wallpaperPath' \
    'LiveWallpaperSurface'; do
    if ! grep -Fq "$expected" "$background"; then
      printf 'FAIL: End4 %s background is missing shared monitor playback contract %s: %s\n' \
        "$variant" "$expected" "$background" >&2
      exit 1
    fi
  done

  if ! grep -Fq 'ThumbnailImage' "$directory_item" ||
    ! grep -Fq 'generateThumbnail: false' "$directory_item"; then
    printf 'FAIL: End4 %s wallpaper grid does not use cached thumbnail-only tiles: %s\n' \
      "$variant" "$directory_item" >&2
    exit 1
  fi
  if grep -Fq 'MediaPlayer' "$directory_item"; then
    printf 'FAIL: End4 %s wallpaper grid attempts playback in a tile: %s\n' \
      "$variant" "$directory_item" >&2
    exit 1
  fi

  if ! grep -Fq 'source: root.mediaKind === "static" ? root.path : ""' "$surface" ||
    ! grep -Fq 'source: root.fallbackPath.length > 0 ? root.fallbackPath' "$surface" ||
    grep -Fq 'sourcePath: root.fallbackPath.length > 0 ? root.fallbackPath' "$surface"; then
    printf 'FAIL: End4 %s live renderer decodes media with the wrong image backend: %s\n' \
      "$variant" "$surface" >&2
    exit 1
  fi

  run_end4_safe_argv_simulation "$service"
  run_end4_metadata_filter_contract "$service"
  run_end4_media_sort_simulation "$media_index"
  run_end4_video_backend_simulation "$reconcile" "$switchwall"
}

run_end4_safe_argv_simulation() {
  local service="$1"
  local fixture marker dangerous result
  fixture="$(mktemp -d)"
  marker="$fixture/executed"
  dangerous="$fixture/space and 'single quote' \$(touch \"$marker\"); echo injected"

  # shellcheck disable=SC2016
  result="$(bash -c 'if [ -d "$1" ]; then printf dir; elif [ -f "$1" ]; then printf file; else printf invalid; fi' \
    end4-validate-directory "$dangerous")"
  if [ "$result" != invalid ] || [ -e "$marker" ]; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 argv simulation executed validate path: %s\n' "$service" >&2
    exit 1
  fi

  # shellcheck disable=SC2016
  result="$(bash -c 'printf "%s|%s|%s|%s|%s" "$1" "$2" "$3" "$4" "$5"' \
    end4-thumbnail-generation "$dangerous" normal "$dangerous" "$dangerous" "$dangerous")"
  if [ "$result" != "$dangerous|normal|$dangerous|$dangerous|$dangerous" ] || [ -e "$marker" ]; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 argv simulation executed thumbnail path: %s\n' "$service" >&2
    exit 1
  fi
  rm -rf -- "$fixture"
  printf 'OK End4 safe argv paths: %s\n' "$service"
}

run_end4_metadata_filter_contract() {
  local service="$1"
  local filter_block
  for expected in \
    'function mediaKindForEntry(entry)' \
    'entry.mediaKind' \
    'root.mediaKindForEntry(item)'; do
    if ! grep -Fq "$expected" "$service"; then
      printf 'FAIL: End4 metadata filter regression guard is missing %s: %s\n' \
        "$expected" "$service" >&2
      exit 1
    fi
  done
  filter_block="$(sed -n '/function matchesFilter(item)/,/^    }/p' "$service")"
  if ! grep -Fq 'const kind = root.mediaKindForEntry(item)' <<<"$filter_block" ||
    grep -Fq 'root.metadata' <<<"$filter_block"; then
    printf 'FAIL: End4 metadata filter still reads stale root.metadata: %s\n' "$service" >&2
    exit 1
  fi
  printf 'OK End4 metadata filter uses fresh index entries: %s\n' "$service"
}

run_end4_media_sort_simulation() {
  local media_index="$1"
  local fixture kind_fixture order_file custom_order mode expected actual
  fixture="$(mktemp -d)"
  kind_fixture="$fixture/content"
  order_file="$fixture/wallpaper_order.json"
  mkdir -p "$kind_fixture"
  printf 'RIFF\x1e\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00' >"$kind_fixture/animated.webp"
  printf 'RIFF\x1e\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00' >"$kind_fixture/static.webp"
  if [ "$($media_index --file "$kind_fixture/animated.webp" | jq -r '.mediaKind')" != animated_webp ] ||
    [ "$($media_index --file "$kind_fixture/static.webp" | jq -r '.mediaKind')" != static ]; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 WebP content classification did not preserve static versus animated media\n' >&2
    exit 1
  fi
  rm -rf -- "$kind_fixture"
  printf 'x' >"$fixture/wall 2.png"
  printf 'xx' >"$fixture/wall 1.png"
  printf 'xxx' >"$fixture/wall 10.png"
  touch -d '2020-01-01 00:00:00' "$fixture/wall 1.png"
  touch -d '2021-01-01 00:00:00' "$fixture/wall 2.png"
  touch -d '2022-01-01 00:00:00' "$fixture/wall 10.png"
  printf '%s\n' '{"'"$fixture"'":["wall 2.png","wall 1.png"]}' >"$order_file"

  for mode in time time_rev name name_rev size size_rev custom; do
    case "$mode" in
      time) expected='wall 10.png|wall 2.png|wall 1.png' ;;
      time_rev) expected='wall 1.png|wall 2.png|wall 10.png' ;;
      name) expected='wall 1.png|wall 2.png|wall 10.png' ;;
      name_rev) expected='wall 10.png|wall 2.png|wall 1.png' ;;
      size) expected='wall 10.png|wall 1.png|wall 2.png' ;;
      size_rev) expected='wall 2.png|wall 1.png|wall 10.png' ;;
      custom) expected='wall 2.png|wall 1.png|wall 10.png' ;;
    esac
    if [ "$mode" = custom ]; then
      custom_order="$(jq -c --arg directory "$fixture" '.[$directory]' "$order_file")"
      actual="$($media_index --directory "$fixture" --sort "$mode" --custom-order "$custom_order" | jq -r '[.[].fileName] | join("|")')"
    else
      actual="$($media_index --directory "$fixture" --sort "$mode" --custom-order '[]' | jq -r '[.[].fileName] | join("|")')"
    fi
    if [ "$actual" != "$expected" ]; then
      rm -rf -- "$fixture"
      printf 'FAIL: End4 media sort mode %s returned %s, expected %s\n' \
        "$mode" "$actual" "$expected" >&2
      exit 1
    fi
  done
  rm -rf -- "$fixture"
  printf 'OK End4 media sort modes and custom order: %s\n' "$media_index"
}

run_end4_video_backend_simulation() {
  local reconcile="$1"
  local switchwall="$2"
  local fixture fakebin config_home runtime_dir restore_script generated_switchwall log monitors marker dangerous_video
  fixture="$(mktemp -d)"
  fakebin="$fixture/bin"
  config_home="$fixture/config"
  runtime_dir="$fixture/runtime"
  restore_script="$config_home/hypr/custom/scripts/__restore_video_wallpaper.sh"
  generated_switchwall="$fixture/switchwall.sh"
  log="$fixture/backend.log"
  monitors="$fixture/monitors.json"
  marker="$fixture/command-substitution-ran"
  dangerous_video="$fixture/space and 'single' \"double\" \$(touch \"\$END4_TEST_MARKER\"); echo injected.mp4"
  mkdir -p "$fakebin" "$runtime_dir" "$(dirname "$restore_script")" "$config_home/illogical-impulse"
  : >"$dangerous_video"
  printf '%s\n' '[{"name":"eDP-1"}]' >"$monitors"
  jq -cn --arg path "$dangerous_video" '{background:{wallpaperPath:$path}}' \
    >"$config_home/illogical-impulse/config.json"
  # shellcheck disable=SC2016
  printf '%s\n' "#!${BASH}" 'printf "pkill:%s\n" "$*" >>"$END4_TEST_LOG"' >"$fakebin/pkill"
  # shellcheck disable=SC2016
  printf '%s\n' "#!${BASH}" 'cat "$END4_TEST_MONITORS"' >"$fakebin/hyprctl"
  # shellcheck disable=SC2016
  printf '%s\n' "#!${BASH}" 'printf "mpvpaper:%s\n" "$*" >>"$END4_TEST_LOG"' >"$fakebin/mpvpaper"
  # shellcheck disable=SC2016
  printf '%s\n' "#!${BASH}" 'exit 0' >"$fakebin/sleep"
  cp "$switchwall" "$generated_switchwall"
  chmod 0755 "$fakebin/pkill" "$fakebin/hyprctl" "$fakebin/mpvpaper" "$fakebin/sleep" "$generated_switchwall"
  sed -i 's/^main "\$@"/# main "\$@"/' "$generated_switchwall"
  # shellcheck disable=SC2016
  XDG_CONFIG_HOME="$config_home" HOME="$fixture/home" "$BASH" -c \
    'source "$1"; create_restore_script "$2"' \
    end4-generated-restore "$generated_switchwall" "$dangerous_video"
  if [ "$(head -n1 "$restore_script")" != "#!${BASH}" ] || [ -e "$marker" ]; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 generated restore script has an unsafe or non-store shell path\n' >&2
    exit 1
  fi

  PATH="$fakebin:$PATH" XDG_CONFIG_HOME="$config_home" XDG_RUNTIME_DIR="$runtime_dir" END4_TEST_LOG="$log" END4_TEST_MONITORS="$monitors" END4_TEST_MARKER="$marker" "$reconcile"
  if [ -e "$marker" ] || ! grep -Fq 'mpvpaper:-o no-audio loop' "$log" || ! grep -Fq "$dangerous_video" "$log"; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 generated restore script executed or lost a quoted video path\n' >&2
    exit 1
  fi
  printf '%s\n' '[{"name":"eDP-1"},{"name":"HDMI-A-1"}]' >"$monitors"
  PATH="$fakebin:$PATH" XDG_CONFIG_HOME="$config_home" XDG_RUNTIME_DIR="$runtime_dir" END4_TEST_LOG="$log" END4_TEST_MONITORS="$monitors" END4_TEST_MARKER="$marker" "$reconcile"
  if [ "$(grep -Fc 'mpvpaper:-o no-audio loop' "$log")" -lt 2 ] ||
    [ "$(grep -Fc 'eDP-1' "$log")" -lt 2 ] ||
    [ "$(grep -Fc 'HDMI-A-1' "$log")" -lt 1 ]; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 video backend restart/hotplug reconciliation did not replay all monitors\n' >&2
    exit 1
  fi

  jq -cn --arg path "$fixture/static.png" '{background:{wallpaperPath:$path}}' \
    >"$config_home/illogical-impulse/config.json"
  before_mpvpaper="$(grep -Fc 'mpvpaper:' "$log" || true)"
  before_pkill="$(grep -Fc 'pkill:' "$log" || true)"
  PATH="$fakebin:$PATH" XDG_CONFIG_HOME="$config_home" XDG_RUNTIME_DIR="$runtime_dir" END4_TEST_LOG="$log" END4_TEST_MONITORS="$monitors" END4_TEST_MARKER="$marker" "$reconcile"
  after_mpvpaper="$(grep -Fc 'mpvpaper:' "$log" || true)"
  after_pkill="$(grep -Fc 'pkill:' "$log" || true)"
  if [ "$after_mpvpaper" -ne "$before_mpvpaper" ] ||
    [ "$after_pkill" -ne $((before_pkill + 1)) ]; then
    rm -rf -- "$fixture"
    printf 'FAIL: End4 static switch did not clean the existing video backend\n' >&2
    exit 1
  fi
  rm -rf -- "$fixture"
  printf 'OK End4 video backend restart/hotplug cleanup: %s\n' "$reconcile"
}

if [ "$mode" = "quickshell" ]; then
  if [ -f "$artifact/ii/services/Wallpapers.qml" ]; then
    check_live_wallpaper_contract Official "$artifact/ii"
  else
    check_live_wallpaper_contract pC "$artifact"
  fi
fi

tests_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
python3 "$tests_dir/end4-artifact-lifecycle.py" "$artifact"
if [ -f "$artifact/ii/settings.qml" ] ||
  [ -f "$artifact/modules/ii/settings/Settings.qml" ] ||
  [ -f "$artifact/hyprland/variables.lua" ]; then
  python3 "$tests_dir/end4-native-settings.py" "$artifact"
fi

if [ "$mode" = "hypr" ]; then
  if ! grep -Fq '/scripts/close-active.sh' "$artifact/wahrwelt/keybinds.lua"; then
    printf 'FAIL: realized End4 artifact is missing the app-aware close binding\n' >&2
    exit 1
  fi

  if grep -R -Fq 'shell-common-rules.lua' "$artifact" ||
    grep -R -Fq 'require("shell-common-rules")' "$artifact"; then
    printf 'FAIL: realized End4 artifact loads shared rules directly\n' >&2
    exit 1
  fi

  for contract in \
    'quickshell:searchToggleRelease' \
    'quickshell:panelFamilyCycle' \
    'quickshell:sidebarRightToggle'; do
    if ! grep -Fq "$contract" "$artifact/hyprland/keybinds.lua"; then
      printf 'FAIL: realized End4 artifact is missing IPC contract %s\n' "$contract" >&2
      exit 1
    fi
  done
  if ! grep -Fq '/hypr/scripts/lock-active.sh' "$artifact/hypridle.conf"; then
    printf 'FAIL: realized End4 artifact is missing the managed lock contract\n' >&2
    exit 1
  fi
fi

printf 'OK realized End4 %s artifact %s\n' "$mode" "$artifact"
