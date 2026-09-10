{
  end4Lib,
  inputs,
  pkgs,
  ...
}:

let
  dotfilesSource = inputs.end4-dotfiles;
  inherit (end4Lib) dotfilesLib pythonEnv;
  quickshellPatcher = ./quickshell.py;
  liveWallpaperPatches = ./.;
  liveWallpaperPatcher = "${liveWallpaperPatches}/live-wallpapers.py";
  liveWallpaperPatch = "${liveWallpaperPatches}/live-wallpapers.patch";

  patchedQuickshell =
    pkgs.runCommand "end4-quickshell-patched"
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
        cp -r ${dotfilesSource}/dots/.config/quickshell $out
        chmod -R +w $out

        find $out -name '*.py' -print0 | xargs -0 sed -i 's|^#!.*ILLOGICAL_IMPULSE_VIRTUAL_ENV.*|#!/usr/bin/env python3|'
        sed -i 's|/dev/pts/\*|/dev/pts/* 2>/dev/null|' $out/ii/scripts/colors/applycolor.sh

        substituteInPlace $out/ii/modules/settings/About.qml \
          --replace-fail 'Quickshell.iconPath("illogical-impulse")' 'Quickshell.iconPath("nix-snowflake")'

        ${pythonEnv}/bin/python ${quickshellPatcher} "$out"
        ${pythonEnv}/bin/python ${liveWallpaperPatcher} "$out" \
          --variant official --patch ${liveWallpaperPatch}

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
        check_wallpaper_replace "$out/ii/scripts/wallpapers/first-frame-thumbnails.sh" 'python3 - "$1"'
        check_wallpaper_replace "$out/ii/scripts/wallpapers/first-frame-thumbnails.sh" 'ffmpeg -v error'
        check_wallpaper_replace "$out/ii/scripts/wallpapers/first-frame-thumbnails.sh" 'magick "'
        substituteInPlace $out/ii/scripts/wallpapers/first-frame-thumbnails.sh \
          --replace-fail 'python3 - "$1"' '${pythonEnv}/bin/python - "$1"' \
          --replace-fail 'ffmpeg -v error' '${pkgs.ffmpeg}/bin/ffmpeg -v error' \
          --replace-fail 'magick "' '${pkgs.imagemagick}/bin/magick "'

        substituteInPlace $out/ii/modules/common/Appearance.qml \
          --replace-fail 'property real contentTransparency: Config?.options.appearance.transparency.automatic ? autoContentTransparency : Config?.options.appearance.transparency.contentTransparency' \
                         'property real contentTransparency: Config?.options.appearance.transparency.enable ? (Config?.options.appearance.transparency.automatic ? autoContentTransparency : Config?.options.appearance.transparency.contentTransparency) : 0'

        substituteInPlace $out/ii/services/SessionWarnings.qml \
          --replace-fail 'root.downloadRunning = (exitCode === 0);' \
                         'root.downloadRunning = false;'

        patchShebangs $out

        ${pkgs.bash}/bin/bash \
          ${dotfilesLib.dotsRoot}/hypr/scripts/tests/end4-artifact-test.sh \
          --quickshell "$out"
      '';
in
{
  xdg.configFile."quickshell/ii" = {
    force = true;
    source = "${patchedQuickshell}/ii";
  };
}
