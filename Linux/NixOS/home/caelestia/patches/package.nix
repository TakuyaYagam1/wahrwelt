{
  inputs,
  prev,
  system,
}:

let
  patchRoot = ./.;
  vendorRoot = "${patchRoot}/vendor";
  shellPatch = "${patchRoot}/shell.patch";
  cliPatch = "${patchRoot}/cli.patch";

  baseCli = inputs.caelestia-cli.packages.${system}.default;
  baseShell = inputs.caelestia-shell.packages.${system}.with-cli;

  thumbnailPython = prev.python3.withPackages (pythonPackages: [ pythonPackages.pillow ]);
  thumbnailTool = prev.writeShellApplication {
    name = "update-caelestia-live-thumbs";
    runtimeInputs = [ prev.ffmpeg ];
    text = ''
      exec ${thumbnailPython}/bin/python3 ${vendorRoot}/update-caelestia-live-thumbs "$@"
    '';
  };

  cli = baseCli.overridePythonAttrs (old: {
    nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ prev.patch ];
    propagatedBuildInputs = (old.propagatedBuildInputs or [ ]) ++ [ prev.ffmpeg ];
    patchPhase = (old.patchPhase or "") + ''
      valid_image_old='    return path.is_file() and path.suffix in [".jpg", ".jpeg", ".png", ".webp", ".tif", ".tiff", ".gif"]'
      valid_image_new='    return path.is_file() and path.suffix.lower() in [".jpg", ".jpeg", ".png", ".webp", ".tif", ".tiff", ".gif"]'
      valid_image_old_count=$(grep -Fxc -- "$valid_image_old" src/caelestia/utils/wallpaper.py || true)
      valid_image_new_count=$(grep -Fxc -- "$valid_image_new" src/caelestia/utils/wallpaper.py || true)
      if [ "$((valid_image_old_count + valid_image_new_count))" -ne 1 ]; then
        echo "unsupported Caelestia CLI is_valid_image implementation" >&2
        exit 1
      fi
      if [ "$valid_image_new_count" -eq 1 ]; then
        substituteInPlace src/caelestia/utils/wallpaper.py \
          --replace-fail "$valid_image_new" "$valid_image_old"
      fi
      patch --batch --forward --fuzz=0 --no-backup-if-mismatch --reject-file=- -p1 < ${cliPatch}
      test "$(grep -Fxc 'wallpapers_dir: Path = Path.home() / "Pictures" / "Wallpapers"' src/caelestia/utils/paths.py)" -eq 1
      test "$(grep -Fxc 'from caelestia.utils.video_cache import (' src/caelestia/utils/wallpaper.py)" -eq 1
      test "$(grep -Fxc '    return path.is_file() and path.suffix.lower() in VALID_WALLPAPER_EXTENSIONS' src/caelestia/utils/wallpaper.py)" -eq 1
      test "$(grep -Fxc 'def is_animated_webp(path: str | Path) -> bool:' src/caelestia/utils/video_cache.py)" -eq 1
      test "$(grep -Fxc 'VIDEO_EXTENSIONS = frozenset({".mp4", ".webm", ".mkv", ".mov", ".avi"})' src/caelestia/utils/video_cache.py)" -eq 1
    '';
  });

  shellBase = baseShell.override {
    caelestia-cli = cli;
  };
in
{
  inherit cli;

  shell = shellBase.overrideAttrs (old: {
    nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ prev.patch ];
    buildInputs = (old.buildInputs or [ ]) ++ [ prev.qt6.qtmultimedia ];
    postPatch = (old.postPatch or "") + ''
      patch --batch --forward --fuzz=0 --no-backup-if-mismatch --reject-file=- -p1 < ${shellPatch}
      test -z "$(find . -name '*.orig' -print -quit)"
      test "$(grep -Fxc '    readonly property string wallpaperDirectory: Paths.wallsdir' services/Wallpapers.qml)" -eq 1
      test "$(grep -Fxc '    readonly property list<string> filterModes: ["Static", "Live", "All"]' services/Wallpapers.qml)" -eq 1
      test "$(grep -Fxc '                loops: MediaPlayer.Infinite' modules/background/Wallpaper.qml)" -eq 1
      test "$(grep -Fxc '                fillMode: VideoOutput.PreserveAspectCrop' modules/background/Wallpaper.qml)" -eq 1
      test "$(grep -Fxc '                    muted: true' modules/background/Wallpaper.qml)" -eq 1
      test "$(grep -Fxc '        command: ["__CAELESTIA_THUMBNAIL_TOOL__/bin/update-caelestia-live-thumbs", root.wallpaperDirectory]' services/Wallpapers.qml)" -eq 1
      substituteInPlace services/Wallpapers.qml \
        --replace-fail '__CAELESTIA_THUMBNAIL_TOOL__' '${thumbnailTool}'
      test "$(grep -Fxc '        command: ["${thumbnailTool}/bin/update-caelestia-live-thumbs", root.wallpaperDirectory]' services/Wallpapers.qml)" -eq 1
      test "$(grep -Fxc '        command: ["__CAELESTIA_THUMBNAIL_TOOL__/bin/update-caelestia-live-thumbs", root.wallpaperDirectory]' services/Wallpapers.qml)" -eq 0
      test -x '${thumbnailTool}/bin/update-caelestia-live-thumbs'
    '';
  });
}
