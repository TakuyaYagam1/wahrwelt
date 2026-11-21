{ pkgs-stable }:

let
  cryptoPython = pkgs-stable.python3.withPackages (
    ps: with ps; [
      gmpy2
      pycryptodome
      sympy
      z3-solver
    ]
  );
  cryptoPythonBin = pkgs-stable.writeShellScriptBin "crypto-python" ''
    exec ${cryptoPython}/bin/python3 "$@"
  '';
in
with pkgs-stable;
[
  aesfix
  aeskeyfind
  bkcrack
  bruteforce-luks
  bruteforce-salted-openssl
  bruteforce-wallet
  brutespray
  ccrypt
  cewl
  cryptoPythonBin
  crowbar
  crunch
  fcrackzip
  hash_extender
  hash-identifier
  hashpump
  hashcat
  hashcat-utils
  hashid
  hashrat
  thc-hydra
  john
  johnny
  maskprocessor
  medusa
  ophcrack
  pack
  padbuster
  pdfcrack
  rsmangler
  seclists
  truecrack
  wordlists
  xortool
  z3
]
