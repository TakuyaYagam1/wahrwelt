{
  lib,
  stdenv,
  fetchurl,
  dpkg,
  autoPatchelfHook,
  makeWrapper,
  wrapGAppsHook3,
  alsa-lib,
  at-spi2-atk,
  at-spi2-core,
  atk,
  cairo,
  cups,
  dbus,
  expat,
  fontconfig,
  freetype,
  glib,
  gtk3,
  libdrm,
  libGL,
  libglvnd,
  libnotify,
  libpulseaudio,
  libsecret,
  libuuid,
  libxkbcommon,
  mesa,
  nspr,
  nss,
  pango,
  pipewire,
  systemd,
  vulkan-loader,
  libayatana-appindicator,
  libseccomp,
  libcap_ng,
  libx11,
  libxcb,
  libxcomposite,
  libxdamage,
  libxext,
  libxfixes,
  libxrandr,
  libxtst,
  libxshmfence,
  trash-cli,
  xdg-utils,
}:

let
  metadata = import ./claude-desktop-source.nix;
  source =
    metadata.sources.${stdenv.hostPlatform.system}
      or (throw "claude-desktop: unsupported system ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "claude-desktop";
  inherit (metadata) version;

  src = fetchurl {
    inherit (source) url hash;
  };

  nativeBuildInputs = [
    dpkg
    autoPatchelfHook
    makeWrapper
    wrapGAppsHook3
  ];

  buildInputs = [
    alsa-lib
    at-spi2-atk
    at-spi2-core
    atk
    cairo
    cups
    dbus
    expat
    fontconfig
    freetype
    glib
    gtk3
    libdrm
    libGL
    libglvnd
    libnotify
    libpulseaudio
    libsecret
    libuuid
    libxkbcommon
    mesa
    nspr
    nss
    pango
    pipewire
    systemd
    vulkan-loader
    libayatana-appindicator
    libseccomp
    libcap_ng
    stdenv.cc.cc.lib
    libx11
    libxcb
    libxcomposite
    libxdamage
    libxext
    libxfixes
    libxrandr
    libxtst
    libxshmfence
  ];

  dontWrapGApps = true;

  unpackPhase = ''
    runHook preUnpack
    dpkg-deb --fsys-tarfile $src | tar -x --no-same-owner --no-same-permissions
    runHook postUnpack
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p $out/lib $out/share
    cp -r usr/lib/claude-desktop $out/lib/
    cp -r usr/share/applications $out/share/
    if [ -d usr/share/icons ]; then
      cp -r usr/share/icons $out/share/
    fi

    # A setuid sandbox cannot retain its mode in the Nix store. Electron falls
    # back to Chromium's unprivileged user-namespace sandbox on NixOS.
    rm -f $out/lib/claude-desktop/chrome-sandbox

    runHook postInstall
  '';

  postFixup = ''
    makeWrapper $out/lib/claude-desktop/claude-desktop $out/bin/claude-desktop \
      "''${gappsWrapperArgs[@]}" \
      --prefix PATH : ${
        lib.makeBinPath [
          glib
          trash-cli
          xdg-utils
        ]
      } \
      --prefix LD_LIBRARY_PATH : ${
        lib.makeLibraryPath [
          libglvnd
          libGL
          mesa
          vulkan-loader
        ]
      } \
      --add-flags "--ozone-platform-hint=auto --password-store=gnome-libsecret"

    for desktopFile in $out/share/applications/*.desktop; do
      substituteInPlace "$desktopFile" \
        --replace-quiet "Exec=claude-desktop" "Exec=$out/bin/claude-desktop"
    done
  '';

  meta = {
    description = "Claude Desktop for Linux, repackaged from Anthropic's official deb";
    homepage = "https://claude.ai";
    downloadPage = "https://support.claude.com/en/articles/10065433-install-claude-desktop";
    license = lib.licenses.unfree;
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
    platforms = lib.attrNames metadata.sources;
    mainProgram = "claude-desktop";
  };
}
