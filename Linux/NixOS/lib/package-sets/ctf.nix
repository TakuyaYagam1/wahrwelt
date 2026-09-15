{
  pkgs,
  pkgs-stable,
  inputs ? null,
  system ? pkgs-stable.stdenv.hostPlatform.system,
}:

let
  importCategory = name: import (./ctf + "/${name}.nix") { inherit pkgs-stable; };
in
{
  ctfCloud = importCategory "cloud";
  ctfCrypto = importCategory "crypto";
  ctfForensics = importCategory "forensics";
  ctfHardware = importCategory "hardware";
  ctfMisc = import ./ctf/misc.nix { inherit pkgs pkgs-stable; };
  ctfMobile = importCategory "mobile";
  ctfNetwork = importCategory "network";
  ctfOsint = importCategory "osint";
  ctfPwn = importCategory "pwn";
  ctfReverse = importCategory "reverse";
  ctfStego = importCategory "stego";
  ctfWeb = import ./ctf/web.nix { inherit pkgs-stable inputs system; };
}
