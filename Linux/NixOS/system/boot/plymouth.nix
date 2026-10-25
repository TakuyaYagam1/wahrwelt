{
  config,
  wahrweltLib,
  pkgs,
  lib,
  ...
}:

let
  plymouthLogo = wahrweltLib.bootTheme.resolveLogo {
    homeDirectory = config.wahrwelt.user.homeDirectory;
    service = "plymouth";
    default = ../../themes/plymouth-theme/assets/logo.png;
  };

  plymouthTheme = pkgs.stdenv.mkDerivation {
    pname = "meowrch-plymouth-theme";
    version = "1.0";

    src = lib.cleanSource ../../themes/plymouth-theme;

    nativeBuildInputs = [ pkgs.imagemagick ];

    dontBuild = true;

    installPhase = ''
      dest=$out/share/plymouth/themes/meowrch
      mkdir -p $dest/assets

      cp -r assets/* $dest/assets/
      cp meowrch.plymouth $dest/
      cp meowrch.script $dest/

      convert ${plymouthLogo} $dest/assets/logo.png

      substituteInPlace $dest/meowrch.plymouth \
        --replace "/usr/share/plymouth/themes/meowrch/assets" "$dest/assets" \
        --replace "/usr/share/plymouth/themes/meowrch/meowrch.script" "$dest/meowrch.script"
    '';
  };
in
{
  boot = {
    plymouth = {
      enable = true;
      theme = "meowrch";
      themePackages = [ plymouthTheme ];
    };

    kernelParams = [
      "quiet"
      "splash"
      "udev.log_level=3"
    ];
  };
}
