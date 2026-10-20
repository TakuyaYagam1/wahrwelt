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

        appearance_qml="$out/modules/common/Appearance.qml"
        content_expression='Config?.options.appearance.transparency.automatic ? autoContentTransparency : Config?.options.appearance.transparency.contentTransparency'
        plain_line="property real contentTransparency: $content_expression"
        glass_line="property real contentTransparency: Math.max(glassContentTransparency, $content_expression)"
        if grep -Fxq "    $glass_line" "$appearance_qml"; then
          expected_line="$glass_line"
          replacement_line="property real contentTransparency: Config?.options.appearance.transparency.enable ? Math.max(glassContentTransparency, $content_expression) : 0"
        elif grep -Fxq "    $plain_line" "$appearance_qml"; then
          expected_line="$plain_line"
          replacement_line="property real contentTransparency: Config?.options.appearance.transparency.enable ? ($content_expression) : 0"
        else
          echo "unsupported end4-pC contentTransparency expression" >&2
          exit 1
        fi
        substituteInPlace "$appearance_qml" --replace-fail "$expected_line" "$replacement_line"

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
