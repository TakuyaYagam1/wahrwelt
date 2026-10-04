{ config, ... }:

{
  nix = {
    settings = {
      experimental-features = [
        "nix-command"
        "flakes"
      ];
      # Optional private netrc for GitHub API auth during flake input updates.
      # Keep the file outside git, owned by root:root and chmod 0600.
      # Expected entries:
      #   machine github.com
      #     login <github-username>
      #     password <github-token>
      #   machine api.github.com
      #     login <github-username>
      #     password <github-token>
      netrc-file = "/etc/nix/netrc";
      download-buffer-size = 268435456;

      substituters = [
        "https://cache.nixos.org?priority=10"
        "https://nix-community.cachix.org"
        "https://hyprland.cachix.org"
        "https://quickshell.cachix.org"
        "https://numtide.cachix.org"
        "https://wahrwelt.cachix.org"
      ];
      trusted-substituters = [
        "https://nix-community.cachix.org"
        "https://hyprland.cachix.org"
        "https://quickshell.cachix.org"
        "https://numtide.cachix.org"
        "https://wahrwelt.cachix.org"
      ];
      trusted-public-keys = [
        "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
        "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
        "hyprland.cachix.org-1:a7pgxzMz7+chwVL3/pzj6jIBMioiJM7ypFP8PwtkuGc="
        "quickshell.cachix.org-1:tjWMR3PQd01gN6YtjSRUdHHHUgrSLFIgwqrCQjFXVOU="
        "numtide.cachix.org-1:2ps1kLBUWjxIneOy1Ik6cQjb41X0iXVXeHigGmycPPE="
        "wahrwelt.cachix.org-1:WLQwCcRx6rjWf9TRHfuj5NIvTc0M52jTz9uptJlwXhA="
      ];

      sandbox = true;
      max-jobs = config.wahrwelt.nix.maxJobs;
      cores = config.wahrwelt.nix.cores;
      auto-optimise-store = config.wahrwelt.host.autoOptimiseStore;
      warn-dirty = false;
    };

    extraOptions = ''
      warn-dirty = false
    '';

    # Persistent fallback when programs.nh.clean is disabled.
    gc = {
      automatic = config.wahrwelt.host.autoGarbageCollector && !config.programs.nh.clean.enable;
      persistent = true;
      dates = "weekly";
      options = "--delete-older-than ${config.wahrwelt.nix.gcRetention}";
    };

    daemonCPUSchedPolicy = "idle";
    daemonIOSchedClass = "idle";

    optimise = {
      automatic = config.wahrwelt.host.autoOptimiseStore;
      dates = [ "weekly" ];
    };
  };

  nixpkgs.config = {
    allowUnfree = true;
    permittedInsecurePackages = import ../lib/permitted-insecure-packages.nix;
    android_sdk.accept_license = true;
  };
}
