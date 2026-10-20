{
  inputs,
  system,
  preset ? "full",
}:

let
  desktopOrMore = builtins.elem preset [
    "desktop"
    "developer"
    "personal"
    "full"
  ];
  developerOrMore = builtins.elem preset [
    "developer"
    "personal"
    "full"
  ];
  personalOrFull = builtins.elem preset [
    "personal"
    "full"
  ];
  shellOverlays = import ./shell-overlays.nix { inherit inputs; };

  flakePackagesOverlay =
    _final: prev:
    let
      corePackages = {
        neovim = inputs.neovim-nightly-overlay.packages.${system}.default;
      };
      desktopPackages =
        if desktopOrMore then shellOverlays.shellPackagesFor { inherit prev system; } else { };
      developerPackages =
        if developerOrMore then
          {
            claude-desktop = prev.callPackage ../pkgs/claude-desktop.nix { };
            claude-code = inputs.claude-code.packages.${system}.default;
            codex = inputs.codex.packages.${system}.default;
          }
        else
          { };
      personalPackages =
        if personalOrFull then
          {
            happ = prev.callPackage ../pkgs/happ.nix {
              happ = inputs.happ-nix.packages.${system}.happ;
            };
          }
        else
          { };
      flakePackages = corePackages // desktopPackages // developerPackages // personalPackages;
      wahrweltPackages = (prev.wahrwelt or (prev.mysetup or { })) // flakePackages;
    in
    flakePackages
    // {
      wahrwelt = wahrweltPackages;
      mysetup = wahrweltPackages;
    };
in
rec {
  inherit flakePackagesOverlay;
  inherit (shellOverlays) valkeyNoCheckOverlay;

  omnirouterFromWahrweltOverlay = _final: prev: {
    omnirouter =
      inputs.wahrwelt.packages.${system}.omnirouter or (prev.callPackage ../pkgs/omnirouter.nix { });
  };

  omnirouterFromMySetupOverlay = omnirouterFromWahrweltOverlay;

  pipxTestCompatibilityOverlay = _final: prev: {
    pipx = prev.pipx.overridePythonAttrs (old: {
      disabledTests = (old.disabledTests or [ ]) ++ [
        "test_fix_package_name"
        "test_parse_specifier_for_metadata"
      ];
      disabledTestPaths = (old.disabledTestPaths or [ ]) ++ [
        "tests/test_inject.py"
      ];
    });
  };

  app2unitScdocCompatibilityOverlay = _final: prev: {
    # app2unit 1.4.2's manpage uses nested scdoc inline formatting, which
    # scdoc 1.11 rejects. Upstream fixed the document in 1.4.4.
    app2unit = prev.app2unit.overrideAttrs (_: {
      version = "1.4.4";
      src = prev.fetchFromGitHub {
        owner = "Vladimir-csp";
        repo = "app2unit";
        rev = "v1.4.4";
        hash = "sha256-TIY+/9ekGub+10uyqXy5aYU+2NLysMtaQnD1PIjBCFA=";
      };
    });
  };

  # aggregateModules needs a certificate for every external kernel module.
  kernelModuleSigningOverlay = final: prev: {
    kernelPackagesExtensions = (prev.kernelPackagesExtensions or [ ]) ++ [
      (kernelFinal: kernelPrev: {
        virtualbox = kernelPrev.virtualbox.overrideAttrs (old: {
          makeFlags =
            (old.makeFlags or [ ])
            ++ prev.lib.optionals
              (!(prev.lib.any (prev.lib.hasPrefix "cmd_sign=") (old.makeFlags or [ ])))
              kernelFinal.kernelModuleMakeFlags;
        });

        vmware = kernelPrev.vmware.overrideAttrs (old: {
          nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ final.kmodigest ];
          # Sign after fixup, which strips uncompressed kernel modules.
          postFixup = (old.postFixup or "") + ''
            for module in "$out"/lib/modules/${kernelFinal.kernel.modDirVersion}/misc/*.ko; do
              test -f "$module" || continue
              if test ! -f "$module.cer"; then
                kmodigest "$module"
              fi
            done
          '';
        });
      })
    ];
  };
}
