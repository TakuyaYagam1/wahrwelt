{ preset }:

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
  full = preset == "full";

  coreInputs = {
    # Temporary compatibility pin: avoid Hyprland 0.56.1 Glaze packaging failure.
    nixpkgs.url = "github:NixOS/nixpkgs?rev=643809054d65fdd466a63e3155b8c498cb483c04";
    nixpkgs-stable.url = "github:NixOS/nixpkgs/nixos-26.05";
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    neovim-nightly-overlay = {
      url = "github:nix-community/neovim-nightly-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nixos-hardware.url = "github:NixOS/nixos-hardware/master";
    nix-snapd = {
      url = "github:nix-community/nix-snapd";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    sops-nix = {
      url = "github:Mic92/sops-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nix-index-database = {
      url = "github:nix-community/nix-index-database";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    stylix = {
      url = "github:danth/stylix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  desktopInputs = {
    caelestia-shell = {
      url = "github:caelestia-dots/shell/v2.4.0";
      inputs = {
        caelestia-cli.follows = "caelestia-cli";
        nixpkgs.follows = "nixpkgs";
        quickshell.follows = "quickshell";
      };
    };
    caelestia-cli = {
      url = "github:caelestia-dots/cli/v1.1.2";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    quickshell = {
      url = "github:outfoxxed/quickshell";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    noctalia = {
      url = "github:noctalia-dev/noctalia/v5.0.1";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    noctalia-shell = {
      url = "github:noctalia-dev/noctalia-shell/v4.7.7";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    end4-dotfiles = {
      url = "git+https://github.com/end-4/dots-hyprland?submodules=1";
      flake = false;
    };
    end4-pc = {
      url = "github:pctrade/end4-pC";
      flake = false;
    };
    zen-browser = {
      url = "github:youwen5/zen-browser-flake";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  developerInputs = {
    claude-code = {
      url = "github:sadjow/claude-code-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    codex = {
      url = "github:sadjow/codex-cli-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    codex-desktop-linux = {
      url = "github:ilysenko/codex-desktop-linux";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    kimi-code = {
      url = "github:MoonshotAI/kimi-code";
    };
  };

  personalInputs = {
    happ-nix = {
      url = "github:DaHL-gh/happ-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  fullInputs = {
    lanzaboote = {
      url = "github:nix-community/lanzaboote";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
in
coreInputs
// (if desktopOrMore then desktopInputs else { })
// (if developerOrMore then developerInputs else { })
// (if personalOrFull then personalInputs else { })
// (if full then fullInputs else { })
