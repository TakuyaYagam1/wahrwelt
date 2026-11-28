{
  inputs,
  wahrweltLib,
  overlays,
  pkgs-stable,
}:

rec {
  wahrweltModule = import ../modules/mysetup-stack.nix;
  mysetupModule = wahrweltModule;

  overlaysModule = _: {
    nixpkgs.overlays = [
      overlays.flakePackagesOverlay
      overlays.valkeyNoCheckOverlay
      overlays.omnirouterFromWahrweltOverlay
      overlays.pipxTestCompatibilityOverlay
      overlays.app2unitScdocCompatibilityOverlay
    ];
  };

  homeManagerModule = import ./flake-home-module.nix {
    inherit
      inputs
      wahrweltLib
      pkgs-stable
      ;
  };
}
