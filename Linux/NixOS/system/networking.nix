{
  config,
  wahrweltLib,
  pkgs,
  ...
}:

{
  networking = {
    hostName = config.wahrwelt.host.hostname;

    networkmanager = {
      enable = true;
      wifi = {
        powersave = false;
        macAddress = "preserve";
      };
      plugins = with pkgs; [
        networkmanager-openvpn
        networkmanager-openconnect
      ];
    };

    nameservers = wahrweltLib.defaults.dns;

    firewall = {
      enable = true;
      allowedTCPPorts = [ ];
      allowedUDPPorts = [ ];
      logRefusedConnections = true;
      logRefusedPackets = false;
      checkReversePath = "strict";
    };
  };

  boot.kernel.sysctl = {
    "net.core.rmem_max" = 7500000;
    "net.core.wmem_max" = 7500000;

    "net.ipv4.tcp_syncookies" = 1;

    # Ignore ICMP redirects (prevents MITM via routing manipulation)
    "net.ipv4.conf.all.accept_redirects" = 0;
    "net.ipv4.conf.default.accept_redirects" = 0;
    "net.ipv6.conf.all.accept_redirects" = 0;

    # Restrict dmesg to root (prevents info leakage about kernel/hardware)
    "kernel.dmesg_restrict" = 1;

    # Required for rootless Docker and Podman user namespaces
    "kernel.unprivileged_userns_clone" = 1;
  };
}
