{
  end4Lib,
  inputs,
  pkgs,
  ...
}:

let
  dotfilesSource = inputs.end4-pc;
  inherit (end4Lib) dotfilesLib pythonEnv;
  quickshellPatcher = ./quickshell-pc.py;
  liveWallpaperPatches = ./.;
  liveWallpaperPatcher = "${liveWallpaperPatches}/live-wallpapers.py";
  liveWallpaperPatch = "${liveWallpaperPatches}/live-wallpapers-pc.patch";
  updaterNotice = "Wahrwelt manages end4-pC updates through the flake input";

  patchedQuickshellPC =
    pkgs.runCommand "end4-pc-quickshell-patched"
      {
        buildInputs = [
          pkgs.bash
          pkgs.ffmpeg
          pkgs.imagemagick
          pkgs.jq
          pkgs.patch
          pythonEnv
        ];
      }
      ''
        cp -r ${dotfilesSource} $out
        chmod -R +w $out

        find $out -name '*.py' -print0 | xargs -0 sed -i 's|^#!.*ILLOGICAL_IMPULSE_VIRTUAL_ENV.*|#!/usr/bin/env python3|'
        sed -i 's|/dev/pts/\*|/dev/pts/* 2>/dev/null|' $out/scripts/colors/applycolor.sh

        ${pythonEnv}/bin/python ${quickshellPatcher} "$out" ${pkgs.lib.escapeShellArg updaterNotice}
        ${pythonEnv}/bin/python ${liveWallpaperPatcher} "$out" \
          --variant pc --patch ${liveWallpaperPatch}

        check_wallpaper_replace() {
          local file="$1"
          local needle="$2"
          local count
          count=$(grep -F -- "$needle" "$file" | wc -l)
          if [ "$count" -ne 1 ]; then
            echo "live wallpaper runtime anchor expected once: $needle ($count)" >&2
            exit 1
          fi
        }
        check_wallpaper_replace "$out/scripts/wallpapers/first-frame-thumbnails.sh" 'python3 - "$1"'
        check_wallpaper_replace "$out/scripts/wallpapers/first-frame-thumbnails.sh" 'ffmpeg -v error'
        check_wallpaper_replace "$out/scripts/wallpapers/first-frame-thumbnails.sh" 'magick "'
        substituteInPlace $out/scripts/wallpapers/first-frame-thumbnails.sh \
          --replace-fail 'python3 - "$1"' '${pythonEnv}/bin/python - "$1"' \
          --replace-fail 'ffmpeg -v error' '${pkgs.ffmpeg}/bin/ffmpeg -v error' \
          --replace-fail 'magick "' '${pkgs.imagemagick}/bin/magick "'

        substituteInPlace $out/modules/common/Appearance.qml \
          --replace-fail 'property real contentTransparency: Config?.options.appearance.transparency.automatic ? autoContentTransparency : Config?.options.appearance.transparency.contentTransparency' \
                         'property real contentTransparency: Config?.options.appearance.transparency.enable ? (Config?.options.appearance.transparency.automatic ? autoContentTransparency : Config?.options.appearance.transparency.contentTransparency) : 0'

        substituteInPlace $out/services/SessionWarnings.qml \
          --replace-fail 'root.downloadRunning = (exitCode === 0);' \
                         'root.downloadRunning = false;'

        test -f "$out/shell.qml"
        patchShebangs $out

        ${pkgs.bash}/bin/bash \
          ${dotfilesLib.dotsRoot}/hypr/scripts/tests/end4-artifact-test.sh \
          --quickshell "$out"
      '';
in
{
  # Both end-4 variants read the user-owned dynamic configuration from
  # ~/.config/illogical-impulse/config.json.
  xdg.configFile."quickshell/end4-pC" = {
    force = true;
    source = "${patchedQuickshellPC}";
  };
}
