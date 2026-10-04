{
  config,
  lib,
  wahrweltLib,
  pkgs,
  ...
}:

let
  cfg = config.wahrwelt;
  developerOrMore = wahrweltLib.presets.developerOrMore cfg;
  personal = wahrweltLib.presets.personal cfg;
  engineActive = developerOrMore || config.services.portainer.enable;
  useDocker = cfg.containers.engine == "docker";
  usePodman = cfg.containers.engine == "podman";
  dockerDaemonSettings = {
    inherit (wahrweltLib.defaults) dns;
    log-driver = "journald";
  };
  virtioWinIso = pkgs.runCommand "virtio-win.iso" { nativeBuildInputs = [ pkgs.xorriso ]; } ''
    xorriso -as mkisofs -iso-level 3 -J -R -V virtio-win -o "$out" ${pkgs.virtio-win}
  '';
in
{
  config = lib.mkMerge [
    (lib.mkIf engineActive {
      virtualisation = {
        docker = {
          # Keep the system daemon off; enabled container features use the user daemon.
          enable = lib.mkDefault false;
          daemon.settings = dockerDaemonSettings;
          rootless = {
            enable = useDocker;
            setSocketVariable = useDocker;
            daemon.settings = dockerDaemonSettings;
          };
        };

        podman = {
          enable = usePodman;
          dockerCompat = false;
          dockerSocket.enable = false;
          defaultNetwork.settings.dns_enabled = true;
        };
      };

      home-manager.users.${cfg.user.username} = {
        home.sessionVariables.DOCKER_HOST = lib.mkDefault (
          if useDocker then
            "unix://$XDG_RUNTIME_DIR/docker.sock"
          else
            "unix://$XDG_RUNTIME_DIR/podman/podman.sock"
        );
        home.packages = lib.mkIf usePodman (
          with pkgs;
          [
            docker-client
            podman-compose
            podman-desktop
          ]
        );
      };

      users.users.${cfg.user.username}.linger = true;
    })

    (lib.mkIf personal {
      environment = {
        systemPackages = with pkgs; [
          virt-manager
          virt-viewer
          virtio-win
          spice-vdagent
        ];
        etc."vm/virtio-win".source = pkgs.virtio-win;
        etc."vm/virtio-win.iso".source = virtioWinIso;
      };

      programs.virt-manager.enable = true;

      systemd.tmpfiles.rules = [
        "d ${cfg.user.homeDirectory}/VMShare 0750 ${cfg.user.username} users - -"
      ];

      virtualisation = {
        libvirtd = {
          enable = true;
          qemu = {
            package = pkgs.qemu_kvm;
            runAsRoot = true;
            swtpm.enable = true;
            vhostUserPackages = [ pkgs.virtiofsd ];
          };
        };

        spiceUSBRedirection.enable = true;

        virtualbox.host = {
          enable = true;
        };
      };

      systemd.services.libvirt-default-network = {
        description = "Ensure libvirt default network is active";
        wantedBy = [ "multi-user.target" ];
        requires = [ "libvirtd.service" ];
        after = [ "libvirtd.service" ];
        path = with pkgs; [
          gnugrep
          libvirt
        ];
        serviceConfig = {
          Type = "oneshot";
          RemainAfterExit = true;
        };
        script = ''
          virsh_qemu() {
            virsh -c qemu:///system "$@"
          }

          network_active() {
            virsh_qemu net-info default | grep -Eq '^Active:[[:space:]]+yes$'
          }

          if ! virsh_qemu net-info default >/dev/null 2>&1; then
            virsh_qemu net-define /var/lib/libvirt/qemu/networks/default.xml
          fi

          virsh_qemu net-autostart default

          if ! network_active; then
            virsh_qemu net-start default || true
          fi

          network_active
        '';
      };
    })
  ];
}
