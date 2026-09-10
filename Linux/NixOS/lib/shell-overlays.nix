{ inputs }:

let
  shellPackagesFor =
    {
      prev,
      system ? prev.stdenv.hostPlatform.system,
    }:
    let
      caelestiaPackages = import ../home/caelestia/patches/package.nix {
        inherit inputs prev system;
      };
    in
    {
      caelestia-cli = caelestiaPackages.cli;
      caelestia-shell = caelestiaPackages.shell;
      noctalia = inputs.noctalia.packages.${system}.default;
      quickshell = inputs.quickshell.packages.${system}.default;
    };

  shellPackagesOverlay =
    _final: prev:
    let
      shellPackages = shellPackagesFor { inherit prev; };
      wahrweltPackages = (prev.wahrwelt or (prev.mysetup or { })) // shellPackages;
    in
    shellPackages
    // {
      wahrwelt = wahrweltPackages;
      mysetup = wahrweltPackages;
    };

  valkeyNoCheckOverlay = _final: prev: {
    valkey = prev.valkey.overrideAttrs (_: {
      # Noctalia pulls Valkey through its graph; upstream checks are flaky on this pinned nixpkgs.
      doCheck = false;
    });
  };
in
{
  inherit
    shellPackagesFor
    shellPackagesOverlay
    valkeyNoCheckOverlay
    ;
}
