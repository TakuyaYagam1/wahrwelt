{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.portainer;
  user = config.wahrwelt.user;
  useDocker = config.wahrwelt.containers.engine == "docker";
  engineService = if useDocker then "docker.service" else "podman.socket";
  enginePackage =
    if useDocker then
      config.virtualisation.docker.rootless.package
    else
      config.virtualisation.podman.package;
  engineCommand = lib.getExe enginePackage;
  engineArgs = if useDocker then "--host unix://$XDG_RUNTIME_DIR/docker.sock" else "--remote=false";
  socketPath = if useDocker then "docker.sock" else "podman/podman.sock";
  stopCommand = pkgs.writeShellScript "portainer-stop" ''
    engine="$1"
    inspection="$("$engine" ${engineArgs} inspect --format '{{.Id}} {{ index .Config.Labels "io.wahrwelt.service" }}' portainer 2>/dev/null || true)"
    read -r container_id label <<< "$inspection"
    if [[ "$container_id" =~ ^[[:xdigit:]]{12,64}$ && "$label" = "portainer" ]]; then
      "$engine" ${engineArgs} stop "$container_id" || true
    fi
  '';
  startCommand = pkgs.writeShellScript "portainer-start" ''
    engine="$1"
    if "$engine" ${engineArgs} inspect portainer >/dev/null 2>&1; then
      echo "Refusing to replace an existing container named portainer." >&2
      exit 42
    fi

    exec "$engine" ${engineArgs} run \
      --name=portainer \
      --label=io.wahrwelt.service=portainer \
      --rm \
      --publish=127.0.0.1:9443:9443 \
      --cap-drop=ALL \
      --security-opt=no-new-privileges \
      --read-only \
      --tmpfs=/tmp:rw,noexec,nosuid,size=64m \
      --volume="$XDG_RUNTIME_DIR/${socketPath}:/var/run/docker.sock" \
      --volume="$STATE_DIRECTORY:/data" \
      ${lib.escapeShellArg cfg.image}
  '';
in
{
  imports = [ ./virtualization.nix ];

  options.services.portainer = {
    enable = lib.mkEnableOption "Portainer container management UI";
    image = lib.mkOption {
      type = lib.types.str;
      default = "portainer/portainer-ce:2.45.1";
      description = "Portainer Server image used by the selected container engine.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${user.username}.linger = true;

    systemd.user.services.portainer = {
      description = "Portainer container management UI";
      unitConfig.ConditionUser = user.username;
      wantedBy = [ "default.target" ];
      requires = [ engineService ];
      after = [ engineService ];
      partOf = [ engineService ];
      serviceConfig = {
        Restart = "on-failure";
        RestartSec = "5s";
        RestartPreventExitStatus = "42";
        StateDirectory = "portainer";
        StateDirectoryMode = "0700";
        ExecStop = "${stopCommand} ${engineCommand}";
        ExecStart = "${startCommand} ${engineCommand}";
        TimeoutStopSec = "120s";
      };
    };
  };
}
