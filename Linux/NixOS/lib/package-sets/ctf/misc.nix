{ pkgs-stable }:

let
  # Upstream 25.5.26 currently fails its own filesystem handler pytest cases
  # on our pinned nixpkgs branch. Keep the tool available until nixpkgs picks
  # up a fixed release.
  unblobPatched = pkgs-stable.unblob.overrideAttrs (_: {
    doCheck = false;
    doInstallCheck = false;
  });
in
with pkgs-stable;
[
  above
  aria2
  axel
  bat
  cabextract
  cherrytree
  cryptsetup
  curl
  cyberchef
  dasel
  fd
  fq
  fzf
  gemini-cli
  git
  gron
  gtkhash
  htmlq
  jc
  jless
  jo
  joplin
  jq
  macchanger
  p7zip
  qemu
  rake
  ripgrep
  ripgrep-all
  rlwrap
  sqlitebrowser
  tmux
  tzdata
  unar
  unblobPatched
  unzip
  wgetpaste
  xmlstarlet
  xq
  yq-go
  zim
  zip
  zsh
  zsh-autosuggestions
  zsh-syntax-highlighting
]
