{
  inputs,
  prev,
  system,
}:

let
  source = inputs.caelestia-live-wallpapers;
  integrationRoot = "${source}/Live Wallpaper Tool";
  baseCli = inputs.caelestia-cli.packages.${system}.default;
  baseShell = inputs.caelestia-shell.packages.${system}.with-cli;

  thumbnailPython = prev.python3.withPackages (pythonPackages: [ pythonPackages.pillow ]);
  thumbnailTool = prev.writeShellApplication {
    name = "update-caelestia-live-thumbs";
    runtimeInputs = [
      prev.ffmpeg
      prev.xdg-user-dirs
    ];
    text = ''
      exec ${thumbnailPython}/bin/python3 "${integrationRoot}/bin/update-caelestia-live-thumbs" "$@"
    '';
  };

  cli = baseCli.overridePythonAttrs (old: {
    patchPhase = (old.patchPhase or "") + ''
      cp "${integrationRoot}/python/wallpaper.py" src/caelestia/utils/wallpaper.py
      cp "${integrationRoot}/python/video_cache.py" src/caelestia/utils/video_cache.py
      substituteInPlace src/caelestia/utils/wallpaper.py \
        --replace-fail 'from video_cache import' 'from caelestia.utils.video_cache import'
    '';

    propagatedBuildInputs = (old.propagatedBuildInputs or [ ]) ++ [ prev.ffmpeg ];
  });

  shellBase = baseShell.override {
    caelestia-cli = cli;
    extraRuntimeDeps = [ thumbnailTool ];
  };

  shell = shellBase.overrideAttrs (old: {
    postPatch = (old.postPatch or "") + ''
      cp -R --no-preserve=mode,ownership,timestamps "${integrationRoot}/qml/." .
      substituteInPlace services/Wallpapers.qml \
        --replace-fail \
          'command: ["bash", "-c", `"''${Paths.home}/.local/bin/update-caelestia-live-thumbs" "''${Paths.wallsdir}" "''${liveWallpapers.path}"`]' \
          'command: ["${thumbnailTool}/bin/update-caelestia-live-thumbs", Paths.wallsdir, liveWallpapers.path]'
    '';

    buildInputs = (old.buildInputs or [ ]) ++ [ prev.qt6.qtmultimedia ];
  });
in
{
  inherit cli shell thumbnailTool;
}
