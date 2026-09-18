{
  config,
  pkgs,
  inputs,
  lib,
  modulesPath,
  wahrwelt,
  wahrweltLib,
  ...
}:

let
  desktopOrMore = wahrweltLib.presets.desktopOrMore wahrwelt;
  developerOrMore = wahrweltLib.presets.developerOrMore wahrwelt;
  wahrweltPkgs = pkgs.wahrwelt or (pkgs.mysetup or { });
  homeLibs = import ./lib { inherit lib pkgs; };
  avatarSource =
    if builtins.pathExists ./avatar.jpg then ./avatar.jpg else ../themes/sddm-theme/icons/logo.png;
  wallpaperSource = ../Wallpapers;
  wallpaperNames = lib.attrNames (
    lib.filterAttrs (name: type: type == "regular" && !(lib.hasPrefix "preview-" name)) (
      builtins.readDir wallpaperSource
    )
  );
  seedWallpaperFiles = lib.concatMapStringsSep "\n" (
    name:
    let
      source = "${wallpaperSource}/${name}";
    in
    ''
      wallpaper_name=${lib.escapeShellArg name}
      seed_if_missing "$WALLS_DST/$wallpaper_name" ${lib.escapeShellArg source} || exit $?
    ''
  ) wallpaperNames;
  generatedConfigFiles = [
    "foot/foot.ini"
    "btop/btop.conf"
    "gtk-3.0/gtk.css"
    "gtk-4.0/gtk.css"
    "cava/config"
    "qt5ct/qt5ct.conf"
    "qt6ct/qt6ct.conf"
  ];
  coreImports = [
    inputs.stylix.homeModules.stylix
    ./stylix.nix
    ./theming.nix
    ./programs/boot-theme.nix
    ./migrations/v1_to_v2/user-paths.nix
    ./programs/git.nix
    ./programs/fish.nix
    ./programs/shell-tools.nix
    ./programs/foot.nix
    ./programs/btop.nix
    ./programs/starship.nix
    ./programs/cava.nix
    ./programs/fastfetch.nix
    ./programs/mimeapps.nix
    ./programs/nightshift.nix
    ./programs/nix-index.nix
    ./programs/nixos-helper.nix
    ./programs/thunar.nix
    ./programs/uwsm.nix
    ./programs/virtualbox.nix
    ./programs/yazi.nix
    ./programs/vesktop.nix
    ./programs/packages.nix
  ];
  developerImports = [
    inputs.codex-desktop-linux.homeManagerModules.default
    ./programs/codex-desktop.nix
  ];
  shellImports = [
    inputs.caelestia-shell.homeManagerModules.default
    (
      if wahrwelt.noctalia.version == "v4" then
        inputs.noctalia-shell.homeModules.default
      else
        inputs.noctalia.homeModules.default
    )
    ./shells
    ./caelestia
    ./noctalia
    ./end4
  ];
in
{
  disabledModules = lib.optionals (builtins.pathExists "${modulesPath}/programs/noctalia.nix") [
    "programs/noctalia.nix"
  ];

  _module.args.homeLibs = homeLibs;

  imports =
    coreImports
    ++ lib.optionals desktopOrMore shellImports
    ++ lib.optionals developerOrMore developerImports;

  home = {
    inherit (wahrwelt.user) username homeDirectory;
    inherit (wahrwelt.host) stateVersion;

    # AccountsService consumers (GNOME/KDE) read this; SDDM uses /etc/sddm/faces/ instead.
    file.".face".source = avatarSource;

    activation.copyWallpapers = lib.mkIf wahrwelt.wallpapers.enable (
      lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        WALLS_DST="${config.home.homeDirectory}/Pictures/Wallpapers"
        ${homeLibs.dotfiles.mutableJsonShellHelpers}

        ensure_real_directory "${config.home.homeDirectory}/Pictures" || exit $?
        ensure_real_directory "$WALLS_DST" || exit $?
        ${seedWallpaperFiles}
      ''
    );
  };

  programs.neovim = {
    enable = true;
    package = wahrweltPkgs.neovim or pkgs.neovim;
    withRuby = true;
    withPython3 = true;
    plugins = [
      (pkgs.symlinkJoin {
        name = "nvim-treesitter-parsers";
        paths = pkgs.vimPlugins.nvim-treesitter.withAllGrammars.dependencies;
      })
    ];
    initLua = ''
      require("config.lazy")
    '';
  };

  # Caelestia ships its own bar - block any upstream waybar enable.
  programs.waybar.enable = lib.mkForce false;

  # Keep GTK4 inheriting the GTK theme unless Stylix defines it explicitly.
  gtk.gtk4.theme = lib.mkDefault config.gtk.theme;

  # When HM uses the system package set, Stylix must not install package overlays
  # inside the HM evaluation as well.
  stylix = {
    overlays.enable = false;
    targets.neovim.enable = false;
    targets.rofi.enable = false;
  };

  xdg.configFile = lib.genAttrs generatedConfigFiles (_: {
    force = true;
  });
}
