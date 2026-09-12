{
  host = {
    hostname = "NixOS";
    stateVersion = "26.05";
    configDirectory = "/etc/nixos";
    autoGarbageCollector = true;
    autoOptimiseStore = true;
  };

  user = {
    username = "user";
    fullName = "user";
    homeDirectory = "/home/user";
  };

  locale = {
    timeZone = "Europe/Moscow";
    defaultLocale = "en_US.UTF-8";
    extraLocale = "ru_RU.UTF-8";
    consoleKeyMap = "us";
    weatherLocation = "Moscow";
  };

  git = {
    username = "user";
    email = "user@example.com";
  };

  packages = {
    preset = "personal";
  };

  noctalia = {
    version = "v5";
  };

  hardware = {
    gpu = "amd";
  };

  features = {
    secureBoot = false;
    ctfTools = false;
    omnirouter = false;
    portainer = false;
    observability = false;
  };

  nix = {
    gcRetention = "14d";
    maxJobs = 4;
    cores = 4;
    swapSizeMiB = 32 * 1024;
    zram = {
      enable = true;
      memoryPercent = 50;
    };
  };

  hypr = {
    keyboardLayouts = "us,ru";
    keyboardToggle = "grp:alt_shift_toggle";
    windowOpacity = "0.85";
  };

  display = {
    monitorName = "eDP-1";
    monitorMode = "preferred";
    monitorPosition = "0x0";
    monitorScale = "1";
    extraMonitors = [ ];
  };

  wallpapers = {
    enable = true;
  };
}
