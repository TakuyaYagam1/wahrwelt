#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
nixos_dir="$repo_root/Linux/NixOS"
codex_tmp="${CODEX_TMP_DIR:-$HOME/.codex/.tmp}"
mkdir -p "$codex_tmp"
chmod 0700 "$codex_tmp"
test_root="$(mktemp -d "$codex_tmp/nix-hardening.XXXXXX")"
trap 'rm -rf "$test_root"' EXIT

status=0

fail() {
    printf 'FAIL nix hardening contract: %s\n' "$*" >&2
    status=1
}

# The Nix expression must receive shell variables literally.
# shellcheck disable=SC2016
lazydocker_probe="$(
    WAHRWELT_NIXOS_DIR="$nixos_dir" nix build \
        --no-link \
        --no-write-lock-file \
        --impure \
        --print-out-paths \
        --expr '
          let
            nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR";
            flake = builtins.getFlake ("path:" + nixosDir);
            pkgs = import flake.inputs.nixpkgs {
              system = "x86_64-linux";
              config.allowUnfree = true;
            };
            fakeLazydocker = pkgs.writeShellScriptBin "lazydocker"
              "printf \"%s\\n\" \"$DOCKER_HOST\"";
            dev = import (nixosDir + "/lib/package-sets/dev.nix") {
              pkgs = pkgs // { lazydocker = fakeLazydocker; };
            };
            candidates = builtins.filter
              (package: (package.pname or (package.name or "")) == "lazydocker")
              dev.devTools;
          in
          if builtins.length candidates != 1 then
            builtins.throw "developer package set must have exactly one lazydocker"
          else if (builtins.head candidates).outPath != fakeLazydocker.outPath then
            builtins.throw "developer package set must use the native lazydocker package"
          else
            builtins.head candidates
        '
)"

lazydocker_default_host="$(
    env -u DOCKER_HOST XDG_RUNTIME_DIR=/run/user/4242 \
        "$lazydocker_probe/bin/lazydocker"
)"
if [[ -n "$lazydocker_default_host" ]]; then
    fail "Lazydocker injects a Docker endpoint when DOCKER_HOST is unset"
fi

lazydocker_override_host="$(
    DOCKER_HOST=tcp://127.0.0.1:2375 XDG_RUNTIME_DIR=/run/user/4242 \
        "$lazydocker_probe/bin/lazydocker"
)"
if [[ "$lazydocker_override_host" != "tcp://127.0.0.1:2375" ]]; then
    fail "Lazydocker does not preserve an explicit Docker endpoint"
fi

require_text() {
    local file="$1"
    local pattern="$2"
    local label="$3"

    if ! grep -Fq -- "$pattern" "$file"; then
        fail "$label"
    fi
}

reject_text() {
    local path="$1"
    local pattern="$2"
    local label="$3"

    if rg -q --fixed-strings -- "$pattern" "$path"; then
        fail "$label"
    fi
}

seed_helpers="$test_root/seed-helpers.sh"
end4_app_seed="$test_root/end4-app-seed.sh"
legacy_password_module="$test_root/legacy-password.nix"
cat > "$legacy_password_module" <<'EOF'
{ config, ... }:

{
  users.users.${config.wahrwelt.user.username}.initialHashedPassword = "synthetic-bootstrap-hash";
}
EOF
chmod 0600 "$legacy_password_module"

# The single-quoted expression intentionally preserves Nix interpolation.
# shellcheck disable=SC2016
WAHRWELT_NIXOS_DIR="$nixos_dir" nix eval --no-write-lock-file --impure --raw --expr '
  let
    nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR";
    flake = builtins.getFlake ("path:" + nixosDir);
    pkgs = import flake.inputs.nixpkgs {
      system = "x86_64-linux";
      config.allowUnfree = true;
    };
  in
  (import (nixosDir + "/home/lib/dotfiles.nix") {
    inherit (pkgs) lib;
    inherit pkgs;
  }).mutableJsonShellHelpers
' > "$seed_helpers"

# Realize the exact helper derivation embedded in both rendered activation
# scripts. `nix eval` registers the string context but does not build it.
# shellcheck disable=SC2016
WAHRWELT_NIXOS_DIR="$nixos_dir" nix build \
    --no-link \
    --no-write-lock-file \
    --impure \
    --expr '
      let
        nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR";
        flake = builtins.getFlake ("path:" + nixosDir);
        pkgs = import flake.inputs.nixpkgs {
          system = "x86_64-linux";
          config.allowUnfree = true;
        };
      in
      (import (nixosDir + "/home/lib/dotfiles.nix") {
        inherit (pkgs) lib;
        inherit pkgs;
      }).mutableSeedHelper
    ' >/dev/null

# The single-quoted expression intentionally preserves Nix interpolation.
# shellcheck disable=SC2016
WAHRWELT_NIXOS_DIR="$nixos_dir" nix eval --no-write-lock-file --impure --raw --expr '
  let
    nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR";
    flake = builtins.getFlake ("path:" + nixosDir);
    defaults = import (nixosDir + "/hosts/NixOS/host-vars.nix");
    username = defaults.user.username;
    hostname = defaults.host.hostname;
  in
  flake.nixosConfigurations.${hostname}.config.home-manager.users.${username}.home.activation.end4SeedAppConfig.data
' > "$end4_app_seed"

printf '{"owner":"user"}\n' > "$test_root/user.json"
printf '{"seeded":true}\n' > "$test_root/seed.json"
ln -s "$test_root/user.json" "$test_root/config.json"
export DRY_RUN_CMD=

if (
    set -euo pipefail
    # shellcheck disable=SC1090
    source "$seed_helpers"
    seed_json_object "$test_root/config.json" "$test_root/seed.json" "" '.default //= true'
) >/dev/null 2>&1; then
    fail "mutable JSON seed followed an unowned symlink"
fi
if ! jq -e '. == {"owner":"user"}' "$test_root/user.json" >/dev/null; then
    fail "mutable JSON seed changed an unowned symlink target"
fi

rm "$test_root/config.json"
printf '{"owner":"user"}\n' > "$test_root/config.json"
if (
    set -euo pipefail
    # shellcheck disable=SC1090
    source "$seed_helpers"
    seed_json_object "$test_root/config.json" "$test_root/seed.json" "" 'invalid jq filter'
) >/dev/null 2>&1; then
    fail "mutable JSON seed swallowed a jq filter failure"
fi

rm "$test_root/config.json"
if (
    set -euo pipefail
    # shellcheck disable=SC1090
    source "$seed_helpers"
    seed_json_object "$test_root/config.json" "$test_root/missing-seed.json" "" '.default //= true'
) >/dev/null 2>&1; then
    fail "mutable JSON seed swallowed an initial copy failure"
fi

printf '%s\n' 'not-json' > "$test_root/config.json"
if (
    set -euo pipefail
    # shellcheck disable=SC1090
    source "$seed_helpers"
    seed_json_object "$test_root/config.json" "$test_root/missing-seed.json" "" '.default //= true'
) >/dev/null 2>&1; then
    fail "mutable JSON seed swallowed an invalid JSON replacement failure"
fi

run_end4_app_seed() {
    local seed_home="$1"

    HOME="$seed_home" DRY_RUN_CMD='' bash "$end4_app_seed"
}

fresh_home="$test_root/end4-fresh"
mkdir -p "$fresh_home"
run_end4_app_seed "$fresh_home"
for seeded in \
    "$fresh_home/.config/kdeglobals" \
    "$fresh_home/.config/kitty" \
    "$fresh_home/.config/fuzzel" \
    "$fresh_home/.local/share/konsole/Profile 1.profile"; do
    if [[ ! -e "$seeded" || -L "$seeded" ]]; then
        fail "End4 fresh seed did not create a mutable owned path: $seeded"
    fi
done

printf '%s\n' 'user-kdeglobals' > "$fresh_home/.config/kdeglobals"
chmod 0444 "$fresh_home/.config/kdeglobals"
kde_before="$(stat -c '%d:%i:%a:%s' "$fresh_home/.config/kdeglobals")"
kde_hash_before="$(sha256sum "$fresh_home/.config/kdeglobals")"
run_end4_app_seed "$fresh_home"
if [[ "$(stat -c '%d:%i:%a:%s' "$fresh_home/.config/kdeglobals")" != "$kde_before" ]] ||
    [[ "$(sha256sum "$fresh_home/.config/kdeglobals")" != "$kde_hash_before" ]]; then
    fail "End4 repeat seed changed an existing read-only regular kdeglobals"
fi

symlink_home="$test_root/end4-unrelated-symlink"
mkdir -p "$symlink_home/.config"
unrelated_store_file="$(readlink -f -- "$(command -v true)")"
ln -s "$unrelated_store_file" "$symlink_home/.config/kdeglobals"
if run_end4_app_seed "$symlink_home" >/dev/null 2>&1; then
    fail "End4 seed accepted an unrelated Nix store symlink"
fi
if [[ ! -L "$symlink_home/.config/kdeglobals" ]] ||
    [[ "$(readlink "$symlink_home/.config/kdeglobals")" != "$unrelated_store_file" ]]; then
    fail "End4 seed changed an unrelated Nix store symlink"
fi

broken_home="$test_root/end4-broken-symlink"
mkdir -p "$broken_home/.config"
ln -s /nix/store/00000000000000000000000000000000-missing "$broken_home/.config/kdeglobals"
if run_end4_app_seed "$broken_home" >/dev/null 2>&1; then
    fail "End4 seed accepted a broken symlink"
fi
if [[ ! -L "$broken_home/.config/kdeglobals" ]]; then
    fail "End4 seed removed a broken symlink"
fi

directory_home="$test_root/end4-directory-collision"
mkdir -p "$directory_home/.config/kdeglobals"
if run_end4_app_seed "$directory_home" >/dev/null 2>&1; then
    fail "End4 seed accepted a directory at the kdeglobals file target"
fi
if [[ ! -d "$directory_home/.config/kdeglobals" ]]; then
    fail "End4 seed changed a directory collision"
fi

reject_text "$nixos_dir" 'allowInsecurePredicate' \
    "Nix package policy still accepts every insecure package"
reject_text "$nixos_dir/home/end4/seed/apps.nix" '2>/dev/null || true' \
    "End4 app seed still swallows copy or permission failures"
reject_text "$nixos_dir/home/lib/dotfiles.nix" 'drop_store_symlink' \
    "fresh Home Manager activation retains a broad Nix store symlink deletion path"
reject_text "$nixos_dir/home/end4/seed/apps.nix" "\$DRY_RUN_CMD chmod" \
    "End4 app seed still changes permissions on existing user files"
# This is a literal shell fragment in the generated Home Manager activation.
# shellcheck disable=SC2016
require_text "$nixos_dir/home/end4/seed/apps.nix" 'seed_directory_if_missing "$kitty_target"' \
    "End4 app seed does not preserve an existing Kitty directory"
require_text "$nixos_dir/home/end4/default.nix" '../migrations/v1_to_v2/end4-app-seed.nix' \
    "old End4 Home Manager link recognition is not isolated in v1_to_v2"
require_text "$nixos_dir/home/migrations/v1_to_v2/link-guard.py" \
    'r"^/nix/store/[0-9abcdfghijklmnpqrsvwxyz]{32}-home-manager-files/(.+)$"' \
    "End4 v1_to_v2 migration does not require an exact old Home Manager generation"
require_text "$nixos_dir/lib/mk-host.nix" 'permittedInsecurePackages' \
    "stable package policy has no explicit insecure package allowlist"
reject_text "$nixos_dir/programs/gaming.nix" 'openFirewall = true' \
    "Steam firewall ports are open by default"
require_text "$nixos_dir/services/observability.nix" 'listenAddress = "127.0.0.1";' \
    "Prometheus services are not explicitly bound to loopback"
require_text "$nixos_dir/services/observability.nix" 'http_listen_address = "127.0.0.1";' \
    "Loki HTTP is not explicitly bound to loopback"
require_text "$nixos_dir/services/observability.nix" '/var/lib/wahrwelt/grafana' \
    "Grafana secret is not stored outside the grafana-writable data directory"
require_text "$nixos_dir/services/observability.nix" 'StateDirectory = "wahrwelt/grafana";' \
    "Grafana secret parent is not systemd-owned"
reject_text "$nixos_dir/services/observability.nix" 'openssl rand -hex 32 >' \
    "Grafana root service still redirects through a grafana-controlled filename"
require_text "$nixos_dir/services/grafana-secret-key.py" 'os.O_TMPFILE' \
    "Grafana secret is not prepared as an unnamed inode"
require_text "$nixos_dir/services/grafana-secret-key.py" 'AT_EMPTY_PATH' \
    "Grafana secret does not use atomic no-clobber publication"
require_text "$nixos_dir/services/grafana-secret-key.py" 'os.O_NOFOLLOW' \
    "Grafana secret helper can follow a symlink collision"
reject_text "$nixos_dir" 'firefox-legacy' \
    "legacy Firefox remains in the Wahrwelt NixOS source tree"
reject_text "$nixos_dir" 'firefoxLegacy' \
    "legacy Firefox feature gate remains in the Wahrwelt NixOS source tree"
reject_text "$nixos_dir/home/programs/fish.nix" 'chrome-922' \
    "legacy Chrome MCP launchers remain in Fish"
reject_text "$nixos_dir/lib/package-sets" 'google-chrome' \
    "Google Chrome remains in a Wahrwelt package set"
reject_text "$nixos_dir/lib/package-sets/home.nix" 'chromium' \
    "Chromium remains in the home package set"
reject_text "$nixos_dir/lib/package-sets/ctf/misc.nix" 'chromium' \
    "Chromium remains in the CTF package set"
reject_text "$nixos_dir/services/portainer.nix" '@sha256:' \
    "Portainer image uses a digest suffix that the version-only updater does not manage"
require_text "$repo_root/.github/workflows/update-portainer.yml" \
    'system.config.services.portainer.image' \
    "Portainer updater does not evaluate the selected service image option"
reject_text "$repo_root/.github/workflows/update-portainer.yml" \
    'virtualisation.oci-containers.containers.portainer.image' \
    "Portainer updater still targets the old rootful OCI container image"
portainer_image="$(sed -nE \
    's/^[[:space:]]*default = "(portainer\/portainer-ce:[^"]+)";.*/\1/p' \
    "$nixos_dir/services/portainer.nix")"
if [[ ! "$portainer_image" =~ ^portainer/portainer-ce:[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail "Portainer image is not a single updater-managed release tag: $portainer_image"
fi
reject_text "$nixos_dir/lib/hosts.nix" 'hashed-password.nix' \
    "host evaluation still imports a password hash from the flake tree"
require_text "$nixos_dir/lib/hosts.nix" '.wahrwelt-password-hash-enabled' \
    "installer host has no non-secret password enable marker"
require_text "$nixos_dir/lib/hosts.nix" '/etc/wahrwelt/hashed-password' \
    "installer host does not use the external password file contract"
require_text "$nixos_dir/lib/preset-flake.nix" './migrations/v1_to_v2/' \
    "legacy hashedPassword compatibility is not isolated in a versioned adapter"
reject_text "$nixos_dir/system/wahrwelt-migration.nix" 'nix flake update' \
    "boot migration still performs a network-dependent flake update"
require_text "$nixos_dir/system/wahrwelt-migration.nix" '--offline' \
    "boot migration validation is not explicitly offline"
require_text "$nixos_dir/system/wahrwelt-migration.nix" '--no-overwrite-dir --sparse -xpf -' \
    "boot migration copy changes the private staging root metadata"
require_text "$nixos_dir/system/wahrwelt-migration.nix" 'require_private_created_directory' \
    "boot migration does not revalidate staging ownership and mode before cleanup"
reject_text "$nixos_dir/system/wahrwelt-migration.nix" "cp -a \"\$destination/.\" \"\$stage_pinned/\"" \
    "boot migration still copies source root metadata onto the private staging root"
require_text "$nixos_dir/system/wahrwelt-migration.nix" './migrations/v1_to_v2/brand.py' \
    "brand recognizer is not physically versioned as v1_to_v2"
require_text "$nixos_dir/system/wahrwelt-migration.nix" \
    "publish-password \"\$destination\" \"\$password_hash_target\"" \
    "v1_to_v2 does not externalize the exact legacy password before rebuilding"
require_text "$nixos_dir/system/wahrwelt-migration.nix" \
    "inputs.wahrwelt.packages.\${pkgs.stdenv.hostPlatform.system}.wahrwelt}/bin/wahrwelt-fs-helper" \
    "v1_to_v2 cleanup does not use the packaged canonical filesystem helper"
if [[ "$(grep -Fc -- "\"\$fs_helper\" remove-migration-temporary" \
    "$nixos_dir/system/wahrwelt-migration.nix")" -ne 2 ]]; then
    fail "v1_to_v2 success path does not clean both exact migration temporaries"
fi
require_text "$nixos_dir/system/wahrwelt-migration.nix" \
    "--kind staging --name \"\$stage_name\" --expected \"\$stage_token\"" \
    "v1_to_v2 staging cleanup lacks the exact name and identity contract"
require_text "$nixos_dir/system/wahrwelt-migration.nix" \
    "--kind namespace --name \"\$namespace_snapshot_name\" --expected \"\$namespace_snapshot_token\"" \
    "v1_to_v2 namespace cleanup lacks the exact name and identity contract"
reject_text "$nixos_dir/system/wahrwelt-migration.nix" "-iname '*mysetup*'" \
    "brand migration still renames paths outside its ownership set"
reject_text "$nixos_dir/system/wahrwelt-migration.nix" 'grep -RIl' \
    "brand migration still rewrites text outside its ownership set"

for removed in \
    "$nixos_dir/home/secrets/default.nix" \
    "$nixos_dir/hosts/NixOS/default.nix" \
    "$nixos_dir/hosts/NixOS/secrets/sops.nix"; do
    if [[ -e "$removed" || -L "$removed" ]]; then
        fail "dead unimported module remains: $removed"
    fi
done

if ((status != 0)); then
    exit "$status"
fi

# The single-quoted Nix expression intentionally preserves Nix interpolation.
# shellcheck disable=SC2016
WAHRWELT_NIXOS_DIR="$nixos_dir" \
WAHRWELT_REPO_ROOT="$repo_root" \
WAHRWELT_LEGACY_PASSWORD_MODULE="$legacy_password_module" \
nix eval --no-write-lock-file --impure --json --expr '
  let
    nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR";
    repoRoot = builtins.getEnv "WAHRWELT_REPO_ROOT";
    flake = builtins.getFlake ("path:" + repoRoot);
    system = "x86_64-linux";
    pkgs = import flake.inputs.nixpkgs {
      inherit system;
      config.allowUnfree = true;
    };
    defaults = import (nixosDir + "/hosts/NixOS/host-vars.nix");
    username = defaults.user.username;
    hostname = defaults.host.hostname;
    legacyPasswordModule = builtins.toPath (builtins.getEnv "WAHRWELT_LEGACY_PASSWORD_MODULE");
    rootModule = _: {
      fileSystems."/" = {
        device = "none";
        fsType = "tmpfs";
      };
    };
    baseHost = flake.lib.mkWahrweltHost {
      inherit system hostname;
      hostVars = defaults;
      extraModules = [ rootModule ];
    };
    base = baseHost.config;
    hostVarsForPreset = preset: defaults // {
      packages = defaults.packages // { inherit preset; };
    };
    mkHostConfig = hostname: hostVars: homeExtraModules:
      (flake.lib.mkWahrweltHost {
        inherit system hostname hostVars homeExtraModules;
        extraModules = [ rootModule ];
      }).config;
    presetConfigs = builtins.listToAttrs (map (preset: {
      name = preset;
      value = let
        host = mkHostConfig "wahrwelt-${preset}-container-check" (hostVarsForPreset preset) [];
      in {
        rootfulDocker = host.virtualisation.docker.enable;
        rootlessDocker = host.virtualisation.docker.rootless.enable;
        dockerSocketVariable = host.virtualisation.docker.rootless.setSocketVariable;
        podman = host.virtualisation.podman.enable;
        podmanCompat = host.virtualisation.podman.dockerCompat;
        podmanSocket = host.virtualisation.podman.dockerSocket.enable;
        dockerHost = host.home-manager.users.${username}.home.sessionVariables.DOCKER_HOST or null;
        linger = host.users.users.${username}.linger;
        dockerGroup = builtins.elem "docker" host.users.users.${username}.extraGroups;
      };
    }) [ "minimal" "desktop" "developer" "personal" ]);
    passwordHost = flake.lib.mkWahrweltHost {
      inherit system;
      hostname = "wahrwelt-password-check";
      hostVars = defaults;
      hashedPasswordFile = "/etc/wahrwelt/hashed-password";
      extraModules = [ rootModule ];
    };
    legacyWahrweltHost = flake.lib.mkWahrweltHost {
      inherit system;
      hostname = "wahrwelt-legacy-password-check";
      hostVars = defaults;
      hashedPassword = legacyPasswordModule;
      extraModules = [ rootModule ];
    };
    legacyMySetupHost = flake.lib.mkMySetupHost {
      inherit system;
      hostname = "mysetup-legacy-password-check";
      hostVars = defaults;
      hashedPassword = legacyPasswordModule;
      extraModules = [ rootModule ];
    };
    wahrweltCollision = builtins.tryEval (
      (flake.lib.mkWahrweltHost {
        inherit system;
        hostname = "wahrwelt-password-collision";
        hostVars = defaults;
        hashedPassword = legacyPasswordModule;
        hashedPasswordFile = "/etc/wahrwelt/hashed-password";
        extraModules = [ rootModule ];
      }).config.system.build.toplevel.drvPath
    );
    mysetupCollision = builtins.tryEval (
      (flake.lib.mkMySetupHost {
        inherit system;
        hostname = "mysetup-password-collision";
        hostVars = defaults;
        hashedPassword = legacyPasswordModule;
        hashedPasswordFile = "/etc/wahrwelt/hashed-password";
        extraModules = [ rootModule ];
      }).config.system.build.toplevel.drvPath
    );
    enabledVars = defaults // {
      features = defaults.features // {
        ctfTools = true;
        observability = true;
        portainer = true;
      };
    };
    enabledHost = flake.lib.mkWahrweltHost {
      inherit system;
      hostname = "wahrwelt-hardening-check";
      hostVars = enabledVars;
      extraModules = [ rootModule ];
    };
    enabled = enabledHost.config;
    podmanVars = defaults // {
      packages = defaults.packages // { preset = "developer"; };
      containers = (defaults.containers or {}) // { engine = "podman"; };
      features = defaults.features // { portainer = true; };
    };
    podmanConfig = mkHostConfig "wahrwelt-podman-container-check" podmanVars [];
    podmanHomePackages = map pkgs.lib.getName podmanConfig.home-manager.users.${username}.home.packages;
    minimalPortainerVars = defaults // {
      packages = defaults.packages // { preset = "minimal"; };
      features = defaults.features // { portainer = true; };
    };
    minimalPortainerConfig = mkHostConfig "wahrwelt-minimal-portainer-check" minimalPortainerVars [];
    portainerDockerService = enabled.systemd.user.services.portainer;
    portainerPodmanService = podmanConfig.systemd.user.services.portainer;
    explicitDockerHost = "tcp://127.0.0.1:2375";
    explicitDockerHostConfig = (flake.lib.mkWahrweltHost {
      inherit system;
      hostname = "wahrwelt-docker-host-override-check";
      hostVars = defaults;
      extraModules = [ rootModule ];
      homeExtraModules = [
        {
          home.sessionVariables.DOCKER_HOST = explicitDockerHost;
        }
      ];
    }).config;
  in {
    passwordFileDefault = base.users.users.${username}.hashedPasswordFile;
    passwordFileEnabled = passwordHost.config.users.users.${username}.hashedPasswordFile;
    legacyWahrweltPassword = legacyWahrweltHost.config.users.users.${username}.initialHashedPassword;
    legacyMySetupPassword = legacyMySetupHost.config.users.users.${username}.initialHashedPassword;
    wahrweltPasswordCollisionAccepted = wahrweltCollision.success;
    mysetupPasswordCollisionAccepted = mysetupCollision.success;
    steamRemotePlay = base.programs.steam.remotePlay.openFirewall;
    steamDedicatedServer = base.programs.steam.dedicatedServer.openFirewall;
    steamTransfers = base.programs.steam.localNetworkGameTransfers.openFirewall;
    prometheus = enabled.services.prometheus.listenAddress;
    nodeExporter = enabled.services.prometheus.exporters.node.listenAddress;
    lokiHttp = enabled.services.loki.configuration.server.http_listen_address;
    lokiGrpc = enabled.services.loki.configuration.server.grpc_listen_address;
    portainerImage = enabled.services.portainer.image;
    presetContainers = presetConfigs;
    defaultDocker = base.virtualisation.docker.enable;
    defaultRootlessDocker = base.virtualisation.docker.rootless.enable;
    defaultDockerSocketVariable = base.virtualisation.docker.rootless.setSocketVariable;
    defaultPodmanCompat = base.virtualisation.podman.dockerCompat;
    defaultPodmanSocket = base.virtualisation.podman.dockerSocket.enable;
    defaultDockerHost = base.home-manager.users.${username}.home.sessionVariables.DOCKER_HOST or null;
    portainerDocker = enabled.virtualisation.docker.enable;
    portainerRootlessDocker = enabled.virtualisation.docker.rootless.enable;
    portainerPodmanCompat = enabled.virtualisation.podman.dockerCompat;
    portainerPodmanSocket = enabled.virtualisation.podman.dockerSocket.enable;
    portainerDockerHost = enabled.home-manager.users.${username}.home.sessionVariables.DOCKER_HOST or null;
    portainerDockerRequires = portainerDockerService.requires;
    portainerDockerAfter = portainerDockerService.after;
    portainerDockerPartOf = portainerDockerService.partOf;
    portainerDockerStateDirectory = portainerDockerService.serviceConfig.StateDirectory;
    portainerDockerStateDirectoryMode = portainerDockerService.serviceConfig.StateDirectoryMode;
    portainerDockerRestartPreventExitStatus = portainerDockerService.serviceConfig.RestartPreventExitStatus;
    portainerDockerStart = portainerDockerService.serviceConfig.ExecStart;
    portainerDockerStartContext = builtins.getContext portainerDockerService.serviceConfig.ExecStart;
    portainerDockerStop = portainerDockerService.serviceConfig.ExecStop;
    portainerDockerStopContext = builtins.getContext portainerDockerService.serviceConfig.ExecStop;
    portainerPodmanRequires = portainerPodmanService.requires;
    portainerPodmanAfter = portainerPodmanService.after;
    portainerPodmanPartOf = portainerPodmanService.partOf;
    portainerPodmanStateDirectory = portainerPodmanService.serviceConfig.StateDirectory;
    portainerPodmanStateDirectoryMode = portainerPodmanService.serviceConfig.StateDirectoryMode;
    portainerPodmanStart = portainerPodmanService.serviceConfig.ExecStart;
    portainerPodmanStartContext = builtins.getContext portainerPodmanService.serviceConfig.ExecStart;
    portainerOciContainer = enabled.virtualisation.oci-containers.containers.portainer or null;
    podmanOverrideRootfulDocker = podmanConfig.virtualisation.docker.enable;
    podmanOverrideRootlessDocker = podmanConfig.virtualisation.docker.rootless.enable;
    podmanOverride = podmanConfig.virtualisation.podman.enable;
    podmanOverrideDockerCompat = podmanConfig.virtualisation.podman.dockerCompat;
    podmanOverrideDockerSocket = podmanConfig.virtualisation.podman.dockerSocket.enable;
    podmanOverrideDockerHost = podmanConfig.home-manager.users.${username}.home.sessionVariables.DOCKER_HOST or null;
    podmanOverrideLinger = podmanConfig.users.users.${username}.linger;
    podmanDockerClient = builtins.elem (pkgs.lib.getName pkgs.docker-client) podmanHomePackages;
    podmanComposeClient = builtins.elem "podman-compose" podmanHomePackages;
    podmanDesktopClient = builtins.elem "podman-desktop" podmanHomePackages;
    podmanHomePackages = podmanHomePackages;
    minimalPortainerRootfulDocker = minimalPortainerConfig.virtualisation.docker.enable;
    minimalPortainerRootlessDocker = minimalPortainerConfig.virtualisation.docker.rootless.enable;
    minimalPortainerPodman = minimalPortainerConfig.virtualisation.podman.enable;
    minimalPortainerDockerHost = minimalPortainerConfig.home-manager.users.${username}.home.sessionVariables.DOCKER_HOST or null;
    minimalPortainerLinger = minimalPortainerConfig.users.users.${username}.linger;
    minimalPortainerRequires = minimalPortainerConfig.systemd.user.services.portainer.requires;
    minimalPortainerStart = minimalPortainerConfig.systemd.user.services.portainer.serviceConfig.ExecStart;
    explicitDockerHost = explicitDockerHostConfig.home-manager.users.${username}.home.sessionVariables.DOCKER_HOST;
    trustedUsers = base.nix.settings.trusted-users;
    primaryUserExtraGroups = base.users.users.${username}.extraGroups;
    sudoRules = base.security.sudo.extraRules;
    wahrweltPackage = flake.packages.${system}.wahrwelt.pname;
    mysetupPackage = flake.packages.${system}.mysetup.pname;
    migrationExecStart = base.systemd.services.wahrwelt-v1-to-v2-migration.serviceConfig.ExecStart;
  }
' > "$test_root/evaluated.json"

if ! jq -e --arg expected_portainer_image "$portainer_image" '
  .passwordFileDefault == null and
  .passwordFileEnabled == "/etc/wahrwelt/hashed-password" and
  .legacyWahrweltPassword == "synthetic-bootstrap-hash" and
  .legacyMySetupPassword == "synthetic-bootstrap-hash" and
  .wahrweltPasswordCollisionAccepted == false and
  .mysetupPasswordCollisionAccepted == false and
  .steamRemotePlay == false and
  .steamDedicatedServer == false and
  .steamTransfers == false and
  .prometheus == "127.0.0.1" and
  .nodeExporter == "127.0.0.1" and
  .lokiHttp == "127.0.0.1" and
  .lokiGrpc == "127.0.0.1" and
  .portainerImage == $expected_portainer_image and
  .defaultDocker == false and
  .defaultRootlessDocker == true and
  .defaultDockerSocketVariable == true and
  .defaultPodmanCompat == false and
  .defaultPodmanSocket == false and
  .defaultDockerHost == "unix://$XDG_RUNTIME_DIR/docker.sock" and
  .portainerDocker == false and
  .portainerRootlessDocker == true and
  .portainerPodmanCompat == false and
  .portainerPodmanSocket == false and
  .portainerDockerHost == "unix://$XDG_RUNTIME_DIR/docker.sock" and
  .portainerDockerRequires == ["docker.service"] and
  .portainerDockerAfter == ["docker.service"] and
  .portainerDockerPartOf == ["docker.service"] and
  .portainerDockerStateDirectory == "portainer" and
  .portainerDockerStateDirectoryMode == "0700" and
  .portainerDockerRestartPreventExitStatus == "42" and
  (.portainerDockerStart | split(" ")[0] | endswith("-portainer-start")) and
  (.portainerDockerStartContext | keys | any(endswith("-portainer-start.drv"))) and
  (.portainerDockerStopContext | keys | any(endswith("-portainer-stop.drv"))) and
  .portainerPodmanRequires == ["podman.socket"] and
  .portainerPodmanAfter == ["podman.socket"] and
  .portainerPodmanPartOf == ["podman.socket"] and
  .portainerPodmanStateDirectory == "portainer" and
  .portainerPodmanStateDirectoryMode == "0700" and
  (.portainerPodmanStart | split(" ")[0] | endswith("-portainer-start")) and
  (.portainerPodmanStartContext | keys | any(endswith("-portainer-start.drv"))) and
  .portainerOciContainer == null and
  .podmanOverrideRootfulDocker == false and
  .podmanOverrideRootlessDocker == false and
  .podmanOverride == true and
  .podmanOverrideDockerCompat == false and
  .podmanOverrideDockerSocket == false and
  .podmanOverrideDockerHost == "unix://$XDG_RUNTIME_DIR/podman/podman.sock" and
  .podmanOverrideLinger == true and
  .podmanDockerClient == true and
  .podmanComposeClient == true and
  .podmanDesktopClient == true and
  ([.podmanHomePackages[] | select(. == "docker" or . == "podman-compose" or . == "podman-desktop")] | length == 3) and
  .minimalPortainerRootfulDocker == false and
  .minimalPortainerRootlessDocker == true and
  .minimalPortainerPodman == false and
  .minimalPortainerDockerHost == "unix://$XDG_RUNTIME_DIR/docker.sock" and
  .minimalPortainerLinger == true and
  .minimalPortainerRequires == ["docker.service"] and
  (.minimalPortainerStart | split(" ")[0] | endswith("-portainer-start")) and
  .explicitDockerHost == "tcp://127.0.0.1:2375" and
  .presetContainers.minimal.rootfulDocker == false and
  .presetContainers.minimal.rootlessDocker == false and
  .presetContainers.minimal.podman == false and
  .presetContainers.minimal.dockerHost == null and
  (.presetContainers.minimal.linger // false) == false and
  .presetContainers.minimal.dockerGroup == false and
  .presetContainers.desktop.rootfulDocker == false and
  .presetContainers.desktop.rootlessDocker == false and
  .presetContainers.desktop.podman == false and
  .presetContainers.desktop.dockerHost == null and
  (.presetContainers.desktop.linger // false) == false and
  .presetContainers.desktop.dockerGroup == false and
  .presetContainers.developer.rootfulDocker == false and
  .presetContainers.developer.rootlessDocker == true and
  .presetContainers.developer.dockerSocketVariable == true and
  .presetContainers.developer.podman == false and
  .presetContainers.developer.podmanCompat == false and
  .presetContainers.developer.podmanSocket == false and
  .presetContainers.developer.dockerHost == "unix://$XDG_RUNTIME_DIR/docker.sock" and
  .presetContainers.developer.linger == true and
  .presetContainers.developer.dockerGroup == false and
  .presetContainers.personal.rootfulDocker == false and
  .presetContainers.personal.rootlessDocker == true and
  .presetContainers.personal.dockerSocketVariable == true and
  .presetContainers.personal.podman == false and
  .presetContainers.personal.podmanCompat == false and
  .presetContainers.personal.podmanSocket == false and
  .presetContainers.personal.dockerHost == "unix://$XDG_RUNTIME_DIR/docker.sock" and
  .presetContainers.personal.linger == true and
  .presetContainers.personal.dockerGroup == false and
  (.trustedUsers | length > 0 and all(. == "root")) and
  (.primaryUserExtraGroups | index("docker")) == null and
  ([.sudoRules[].commands[].options[]?] | index("NOPASSWD")) == null and
  .wahrweltPackage == "wahrwelt" and
  .mysetupPackage == "wahrwelt" and
  (.migrationExecStart | startswith("/nix/store/"))
' "$test_root/evaluated.json" >/dev/null; then
    jq . "$test_root/evaluated.json" >&2
    fail "evaluated Nix hardening contract does not match"
fi

docker_start_drv="$(jq -r '.portainerDockerStartContext | keys[] | select(endswith("-portainer-start.drv"))' "$test_root/evaluated.json")"
docker_stop_drv="$(jq -r '.portainerDockerStopContext | keys[] | select(endswith("-portainer-stop.drv"))' "$test_root/evaluated.json")"
podman_start_drv="$(jq -r '.portainerPodmanStartContext | keys[] | select(endswith("-portainer-start.drv"))' "$test_root/evaluated.json")"
if [[ -z "$docker_start_drv" || -z "$docker_stop_drv" || -z "$podman_start_drv" ]]; then
    fail "could not resolve generated Portainer start/stop helper derivations"
elif nix build --no-link --no-write-lock-file --impure \
    "$docker_start_drv^out" "$docker_stop_drv^out" "$podman_start_drv^out" >/dev/null; then
    docker_start="$(jq -r '.portainerDockerStart | split(" ")[0]' "$test_root/evaluated.json")"
    docker_stop="$(jq -r '.portainerDockerStop | split(" ")[0]' "$test_root/evaluated.json")"
    podman_start="$(jq -r '.portainerPodmanStart | split(" ")[0]' "$test_root/evaluated.json")"
    # shellcheck disable=SC2016
    require_text "$docker_start" '--host unix://$XDG_RUNTIME_DIR/docker.sock' \
        "Docker Portainer start helper does not select its rootless socket"
    # shellcheck disable=SC2016
    require_text "$docker_start" '$XDG_RUNTIME_DIR/docker.sock:/var/run/docker.sock' \
        "Docker Portainer start helper does not share the selected engine socket"
    require_text "$docker_start" '--cap-drop=ALL' \
        "Docker Portainer start helper does not drop all capabilities"
    # shellcheck disable=SC2016
    require_text "$podman_start" '$XDG_RUNTIME_DIR/podman/podman.sock:/var/run/docker.sock' \
        "Podman Portainer start helper does not mount its selected socket"

    fake_engine="$test_root/fake-engine"
    engine_trace="$test_root/fake-engine.trace"
    expected_trace="$test_root/fake-engine.expected"
    cat > "$fake_engine" <<'EOF'
#!/usr/bin/env bash
set -eu
printf '%s\0' "$@" >> "$FAKE_ENGINE_TRACE"
for arg in "$@"; do
    if [[ "$arg" == inspect ]]; then
        if [[ -n "$FAKE_INSPECT_OUTPUT" ]]; then
            printf '%s\n' "$FAKE_INSPECT_OUTPUT"
        fi
        exit "${FAKE_INSPECT_STATUS:-0}"
    fi
    if [[ "$arg" == stop ]]; then
        exit 0
    fi
    if [[ "$arg" == run ]]; then
        exit 0
    fi
done
exit 64
EOF
    chmod 0700 "$fake_engine"

    expected_id='0123456789abcdef0123456789abcdef'
    expected_template='{{.Id}} {{ index .Config.Labels "io.wahrwelt.service" }}'
    run_portainer_stop_case() {
        local case_name="$1"
        local inspection_output="$2"
        local inspection_status="$3"
        local managed_id="$4"

        : > "$engine_trace"
        : > "$expected_trace"
        if ! FAKE_ENGINE_TRACE="$engine_trace" \
            FAKE_INSPECT_OUTPUT="$inspection_output" \
            FAKE_INSPECT_STATUS="$inspection_status" \
            XDG_RUNTIME_DIR=/run/user/4242 \
            "$docker_stop" "$fake_engine"; then
            fail "Portainer ExecStop helper failed for $case_name container"
            return
        fi

        printf '%s\0' --host 'unix:///run/user/4242/docker.sock' \
            inspect --format "$expected_template" portainer >> "$expected_trace"
        if [[ -n "$managed_id" ]]; then
            printf '%s\0' --host 'unix:///run/user/4242/docker.sock' \
                stop "$managed_id" >> "$expected_trace"
        fi
        if ! cmp -s "$expected_trace" "$engine_trace"; then
            fail "Portainer ExecStop issued unexpected Docker CLI arguments for $case_name container"
        fi
    }

    run_portainer_stop_case missing '' 1 ''
    run_portainer_stop_case foreign "$expected_id another-service" 0 ''
    run_portainer_stop_case malformed 'short portainer' 0 ''
    run_portainer_stop_case managed "$expected_id portainer" 0 "$expected_id"

    run_portainer_start_case() {
        local case_name="$1"
        local inspection_status="$2"
        local expected_status="$3"
        local expect_run="$4"
        local command_status

        : > "$engine_trace"
        : > "$expected_trace"
        if FAKE_ENGINE_TRACE="$engine_trace" \
            FAKE_INSPECT_OUTPUT='' \
            FAKE_INSPECT_STATUS="$inspection_status" \
            XDG_RUNTIME_DIR=/run/user/4242 \
            STATE_DIRECTORY=/run/user/4242/portainer \
            "$docker_start" "$fake_engine" >/dev/null 2>&1; then
            command_status=0
        else
            command_status=$?
        fi
        if [[ "$command_status" -ne "$expected_status" ]]; then
            fail "Portainer ExecStart returned $command_status for $case_name container, expected $expected_status"
        fi

        printf '%s\0' --host 'unix:///run/user/4242/docker.sock' inspect portainer >> "$expected_trace"
        if [[ "$expect_run" == true ]]; then
            printf '%s\0' --host 'unix:///run/user/4242/docker.sock' run \
                --name=portainer \
                --label=io.wahrwelt.service=portainer \
                --rm \
                --publish=127.0.0.1:9443:9443 \
                --cap-drop=ALL \
                --security-opt=no-new-privileges \
                --read-only \
                --tmpfs=/tmp:rw,noexec,nosuid,size=64m \
                --volume=/run/user/4242/docker.sock:/var/run/docker.sock \
                --volume=/run/user/4242/portainer:/data \
                "$portainer_image" >> "$expected_trace"
        fi
        if ! cmp -s "$expected_trace" "$engine_trace"; then
            fail "Portainer ExecStart issued unexpected Docker CLI arguments for $case_name container"
        fi
    }

    run_portainer_start_case collision 0 42 false
    run_portainer_start_case missing 1 0 true
else
    fail "could not realize generated Portainer helper scripts"
fi

if ((status != 0)); then
    exit "$status"
fi

printf 'OK nix hardening contract\n'
