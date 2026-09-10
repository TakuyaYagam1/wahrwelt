{
  inputs,
  pkgs,
  system,
}:

let
  patchRoot = ./.;

  addV4Checks = old: {
    patches = (old.patches or [ ]) ++ [ (patchRoot + /v4.patch) ];
    patchFlags = (old.patchFlags or [ "-p1" ]) ++ [ "--fuzz=0" ];
    prePatch = (old.prePatch or "") + ''
      set -e
      check_marker() {
        file="$1"
        needle="$2"
        expected="$3"
        found=$(grep -Fc -- "$needle" "$file" || true)
        test "$found" -eq "$expected" || {
          echo "noctalia v4 patch drift: $file marker '$needle' count $found, expected $expected" >&2
          exit 1
        }
      }
      check_marker Services/UI/WallpaperService.qml 'function _scanDirectoryInternal' 1
      check_marker Services/UI/WallpaperService.qml 'property var wallpaperLists' 1
      check_marker Modules/Panels/Wallpaper/WallpaperPanel.qml 'function selectItem(path, isDirectory)' 1
      check_marker Modules/Background/Background.qml 'function setWallpaperImmediate(source)' 1
      check_marker Services/UI/ImageCacheService.qml 'function startThumbnailProcessing' 1
      check_marker Services/Theming/AppThemeService.qml 'TemplateProcessor.processWallpaperColors(wp, mode);' 1
    '';
    postPatch = (old.postPatch or "") + ''
      check_marker() {
        file="$1"
        needle="$2"
        expected="$3"
        found=$(grep -Fc -- "$needle" "$file" || true)
        test "$found" -eq "$expected" || {
          echo "noctalia v4 patch post-check failed: $file marker '$needle' count $found, expected $expected" >&2
          exit 1
        }
      }
      check_marker Services/UI/WallpaperService.qml 'mediaFilterOptions: ["All", "Static", "Live"]' 1
      check_marker Modules/Panels/Wallpaper/WallpaperPanel.qml 'selectItem(path, undefined)' 1
      check_marker Services/UI/WallpaperService.qml 'String.fromCharCode(10)' 1
      check_marker Services/UI/WallpaperService.qml 'const command = ["webpinfo", path];' 1
      check_marker Modules/Background/Background.qml 'function stopLiveWallpaperBackend()' 1
      check_marker Modules/Background/Background.qml 'stopLiveWallpaperBackend();' 2
      check_marker Modules/Background/Background.qml 'WallpaperService.mediaKindChanged.connect(onMediaKindChanged);' 1
      check_marker Modules/Background/Background.qml 'function reEvaluateWallpaperKind(path)' 1
      check_marker Modules/Background/Background.qml 'loops: MediaPlayer.Infinite' 1
      check_marker Services/UI/ImageCacheService.qml 'ffmpeg first-frame extraction' 1
      ${pkgs.bash}/bin/bash ${patchRoot}/tests/v4-contract.sh "$PWD"
      PATH=${pkgs.python3}/bin:$PATH ${pkgs.bash}/bin/bash ${patchRoot}/tests/v4-async-transition-test.sh "$PWD"
    '';
    preFixup = (old.preFixup or "") + ''
      qtWrapperArgs+=(--prefix PATH : ${pkgs.ffmpeg}/bin:${pkgs.libwebp}/bin)
    '';
    postFixup = (old.postFixup or "") + ''
      grep -Fq -- '${pkgs.libwebp}/bin' "$out/bin/noctalia-shell" || {
        echo "noctalia v4 runtime check failed: webpinfo is missing from PATH wrapper" >&2
        exit 1
      }
    '';
  };

  addV5Checks = old: {
    patches = (old.patches or [ ]) ++ [ (patchRoot + /v5.patch) ];
    patchFlags = (old.patchFlags or [ "-p1" ]) ++ [ "--fuzz=0" ];
    prePatch = (old.prePatch or "") + ''
      set -e
      check_marker() {
        file="$1"
        needle="$2"
        expected="$3"
        found=$(grep -Fc -- "$needle" "$file" || true)
        test "$found" -eq "$expected" || {
          echo "noctalia v5 patch drift: $file marker '$needle' count $found, expected $expected" >&2
          exit 1
        }
      }
      check_marker meson.build "'src/shell/wallpaper/wallpaper.cpp'" 1
      check_marker src/shell/wallpaper/panel/wallpaper_scanner.cpp 'DirectoryScanner::isImagePath' 2
      check_marker src/shell/wallpaper/wallpaper.cpp 'void Wallpaper::syncInstances()' 1
      check_marker src/theme/theme_service.cpp 'auto image = loadAndResize(wallpaperPath, *scheme);' 1
    '';
    postPatch = (old.postPatch or "") + ''
      check_marker() {
        file="$1"
        needle="$2"
        expected="$3"
        found=$(grep -Fc -- "$needle" "$file" || true)
        test "$found" -eq "$expected" || {
          echo "noctalia v5 patch post-check failed: $file marker '$needle' count $found, expected $expected" >&2
          exit 1
        }
      }
      check_marker src/shell/wallpaper/live_wallpaper_controller.cpp 'mpvpaper' 2
      check_marker src/shell/wallpaper/live_wallpaper_controller.h 'virtual bool isRunning() const = 0;' 1
      check_marker src/shell/wallpaper/live_wallpaper_controller.cpp '::kill(*m_pid, 0)' 1
      check_marker src/shell/wallpaper/live_wallpaper_controller.cpp 'errno == EPERM' 1
      check_marker src/shell/wallpaper/live_wallpaper_controller.cpp 'second.process->isRunning()' 1
      check_marker src/shell/wallpaper/wallpaper_media.cpp 'ffmpeg' 1
      check_marker src/shell/wallpaper/wallpaper_media.cpp 'byteCount' 3
      check_marker src/shell/wallpaper/wallpaper_media.cpp 'WEBP_FF_FRAME_COUNT' 1
      check_marker src/shell/wallpaper/wallpaper_media.cpp 'if (lowerExtension(path) == ".webp")' 1
      check_marker tests/wallpaper_media_test.cpp 'kindForPath(animatedPath)' 1
      check_marker tests/live_wallpaper_controller_test.cpp 'processes[0]->simulateCrash();' 1
      check_marker tests/live_wallpaper_controller_test.cpp 'processes[1]->simulateExit();' 1
      check_marker tests/live_wallpaper_controller_test.cpp 'failNextLaunch = true;' 1
      check_marker src/shell/wallpaper/panel/wallpaper_panel.cpp 'MediaFilter::Live' 3
      check_marker meson.build 'src/shell/wallpaper/live_wallpaper_controller.cpp' 1
    '';
    postFixup = (old.postFixup or "") + ''
      wrapProgram $out/bin/noctalia --prefix PATH : ${pkgs.ffmpeg}/bin:${pkgs.mpvpaper}/bin
      grep -Fq -- '${pkgs.mpvpaper}/bin' "$out/bin/noctalia" || {
        echo "noctalia v5 runtime check failed: mpvpaper is missing from PATH wrapper" >&2
        exit 1
      }
    '';
  };

  v4Base = inputs.noctalia-shell.packages.${system}.default;
  v5Base = inputs.noctalia.packages.${system}.default;
in
{
  v4 = v4Base.overrideAttrs addV4Checks;
  v5 = v5Base.overrideAttrs addV5Checks;
}
