{ pkgs }:

pkgs.python3.withPackages (ps: [
  ps.build
  ps.cffi
  ps.click
  ps."dbus-python"
  (
    if ps ? "kde-material-you-colors" then
      ps."kde-material-you-colors"
    else
      pkgs.kde-material-you-colors
  )
  ps.libsass
  ps.loguru
  ps."material-color-utilities"
  ps.materialyoucolor
  ps.numpy
  ps.pillow
  ps.psutil
  ps.pycairo
  ps.pygobject3
  ps.pywayland
  ps.setproctitle
  ps."setuptools-scm"
  ps.tqdm
  ps.wheel
  ps."pyproject-hooks"
  ps.opencv4
])
