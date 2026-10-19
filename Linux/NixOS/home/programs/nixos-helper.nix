{
  wahrwelt,
  lib,
  pkgs,
  inputs,
  ...
}:

let
  cfg = wahrwelt.host.configDirectory;
  host = wahrwelt.host.hostname;
  installer = inputs.self.packages.${pkgs.stdenv.hostPlatform.system}.wahrwelt;

  nixy = pkgs.writeShellApplication {
    name = "nixy";
    runtimeInputs = [
      installer
    ]
    ++ (with pkgs; [
      fzf
      git
      coreutils
      nix
      nvd
    ]);
    text = ''
      CFG="${cfg}"
      HOST="${host}"
      # Array (not string) preserves args with spaces; safe under set -u.
      if [ "$#" -gt 1 ]; then
        EXTRA=( "''${@:2}" )
      else
        EXTRA=()
      fi

      rebuild() {
        (cd "$CFG" && sudo nixos-rebuild switch --flake ".#$HOST" "''${EXTRA[@]}")
      }

      test_cmd() {
        (cd "$CFG" && sudo nixos-rebuild test --flake ".#$HOST" "''${EXTRA[@]}")
      }

      boot_cmd() {
        (cd "$CFG" && sudo nixos-rebuild boot --flake ".#$HOST" "''${EXTRA[@]}")
      }

      lock_cmd() {
        (cd "$CFG" && sudo nix flake update "''${EXTRA[@]}")
      }

      update() {
        wahrwelt update --dest "$CFG" --state "$CFG/installer-state.json" "''${EXTRA[@]}"
      }

      gc() {
        local retention=${lib.escapeShellArg (wahrwelt.nix.gcRetention or "14d")}
        echo "==> keeping build cache and generations newer than $retention"
        nix-collect-garbage --delete-older-than "$retention" --option keep-derivations true --option keep-outputs true
        sudo nix-collect-garbage --delete-older-than "$retention" --option keep-derivations true --option keep-outputs true
        sudo nix-store --optimise
      }

      gc_full() {
        echo "==> removing old generations and unused build cache"
        nix-collect-garbage --delete-old --option keep-derivations true --option keep-outputs false
        sudo nix-collect-garbage --delete-old --option keep-derivations true --option keep-outputs false
        sudo nix-store --optimise
      }

      listgen() {
        sudo nix-env -p /nix/var/nix/profiles/system --list-generations
      }

      diff_cmd() {
        local current previous
        current="/run/current-system"
        previous="$(sudo nix-env -p /nix/var/nix/profiles/system --list-generations \
          | awk 'END{print $1}')"
        nvd diff "$current" "/nix/var/nix/profiles/system-$previous-link" || true
      }

      ui() {
        choice="$(printf '%s\n' \
          "󰑓 rebuild" \
          "󰐊 test" \
          "󰚰 update" \
          " lock" \
          " gc" \
          "󰍜 boot" \
          " listgen" \
          " diff" \
          | fzf --prompt="nixy> " --height=40% --border --reverse)"
        [ -z "$choice" ] && exit 0
        case "$choice" in
          *rebuild*) rebuild ;;
          *test*)    test_cmd ;;
          *update*)  update ;;
          *lock*)    lock_cmd ;;
          *gc*)      gc ;;
          *boot*)    boot_cmd ;;
          *listgen*) listgen ;;
          *diff*)    diff_cmd ;;
        esac
      }

      case "''${1:-ui}" in
        rebuild) rebuild ;;
        test)    test_cmd ;;
        update)  update ;;
        lock)    lock_cmd ;;
        gc)      gc ;;
        gc-full) gc_full ;;
        boot)    boot_cmd ;;
        listgen) listgen ;;
        diff)    diff_cmd ;;
        ui|"")   ui ;;
        *)       echo "unknown: $1" >&2; exit 2 ;;
      esac
    '';
  };
in
{
  home.packages = [
    nixy
    installer
  ];
}
