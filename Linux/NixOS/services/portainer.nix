{
  config,
  lib,
  ...
}:
let
  cfg = config.services.portainer;
in
{
  options.services.portainer = {
    enable = lib.mkEnableOption "Portainer Docker management UI";
  };

  config = lib.mkIf cfg.enable {
    virtualisation = {
      docker.enable = true;

      oci-containers = {
        backend = "docker";

        containers.portainer = {
          image = "portainer/portainer-ce:2.45.1";
          autoStart = true;
          ports = [
            "127.0.0.1:9443:9443"
          ];
          volumes = [
            "/var/run/docker.sock:/var/run/docker.sock"
            "/var/lib/portainer:/data"
          ];
        };
      };
    };

    systemd.tmpfiles.rules = [
      "d /var/lib/portainer 0700 root root - -"
    ];
  };
}
