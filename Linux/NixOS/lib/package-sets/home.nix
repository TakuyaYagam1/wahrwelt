{
  pkgs,
  pkgs-stable ? pkgs,
}:

{
  apiTools = with pkgs; [
    yaak
    sqlit-tui
    ngrok
  ];
  containers = [ ];
  desktop = with pkgs; [
    firefox
    spotify
    telegram-desktop
    vesktop
    libreoffice-qt6-fresh
    zathura
    loupe
    mpv
    obsidian
  ];
  dev = with pkgs; [
    vscode
    claude-desktop
    claude-code
    codex
    kimi-code
  ];
  personal = with pkgs; [
    happ
    wpsoffice
    onlyoffice-desktopeditors
    anytype
    insomnia
    dbeaver-bin
    pkgs-stable.pgbadger
    warp-terminal
    termius
    jetbrains.datagrip
    podman-desktop
    code-cursor
    zed-editor-fhs
    qtcreator
    antigravity-ide
    gemini-cli
    ollama
    opencode
    opencode-claude-auth
    opencode-desktop
    flutter
    android-studio-full
    android-tools
    scrcpy
    jython
  ];
  games = with pkgs; [
    lutris
    heroic
  ];
  media = with pkgs; [
    cava
    libcava
    aubio
    gpu-screen-recorder
    pwvucontrol
    pavucontrol
    pamixer
  ];
  misc = with pkgs; [
    libqalculate
    qalculate-gtk
    bulky
    nemo
    app2unit
    cmatrix
    tmux
    herdr
    zellij
    drawing
    ksnip
    pinta
  ];
}
