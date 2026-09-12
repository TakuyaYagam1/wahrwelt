{
  config,
  lib,
  pkgs,
  pkgs-stable,
  inputs,
  ...
}:

let
  packageSets = import ../lib/package-sets.nix {
    inherit
      lib
      pkgs
      pkgs-stable
      inputs
      ;
  };
in
{
  imports = [
    ../services/omnirouter.nix
    ../services/portainer.nix
    ../services/observability.nix
  ];

  config = lib.mkIf config.wahrwelt.features.ctfTools {
    environment.systemPackages = lib.flatten (lib.attrValues packageSets.ctf);
    programs.wireshark.enable = true;
  };
}
