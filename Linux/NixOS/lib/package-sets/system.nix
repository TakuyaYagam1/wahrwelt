{
  pkgs,
  pkgs-stable ? pkgs,
}:

let
  hyprlandQtUtils =
    if pkgs ? hyprland-guiutils then
      [ pkgs.hyprland-guiutils ]
    else
      pkgs.lib.optionals (pkgs ? hyprland-qtutils) [ pkgs.hyprland-qtutils ];
in
{
  systemBase = with pkgs; [
    kdePackages.qt6ct
    kdePackages.breeze-icons
    adwaita-icon-theme
    hicolor-icon-theme
    papirus-icon-theme
    material-design-icons
    wl-clipboard
    xclip
    acpi
    powertop
    cifs-utils
    nfs-utils
    dos2unix
    ethtool
    expect
    mc
    plocate
    screen
    vim
    git
    kitty
    fish
    bash
    curl
    wget
    lsof
    nettools
    iproute2
    bind
    bat
    procs
    brightnessctl
    ddcutil
    lm_sensors
    inotify-tools
    sqlite
    imagemagick
    file
    unzip
    zip
    rar
    p7zip
    libarchive
    trash-cli
    cloc
    sbctl
    efibootmgr
    tree
    shellcheck
    jq
    ripgrep
    fd
    fzf
    eza
    zoxide
    atuin
    htop
    # Glances 4.5.5's localhost HTTP/XML-RPC integration tests are not
    # sandbox-safe in the current nixpkgs build environment.
    (glances.overridePythonAttrs (_: {
      doCheck = false;
    }))
  ];
  systemDesktop =
    (with pkgs; [
      neovim
      btop
      neohtop
      steam-run
      networkmanagerapplet
      libnotify
      qbittorrent
    ])
    ++ hyprlandQtUtils;
  systemDeveloper = with pkgs; [
    docker-compose
    podman-compose
    yazi
    delta
  ];
  systemPersonal = with pkgs; [
    hysteria
    v2rayn
    clash-meta
    clash-nyanpasu
    mihomo
    sing-box
    sing-geoip
  ];
  systemPersonalStable = with pkgs-stable; [
    (wineWow64Packages.staging.override {
      waylandSupport = true;
    })
    winetricks
    protontricks
    wineWow64Packages.stable
  ];
}
