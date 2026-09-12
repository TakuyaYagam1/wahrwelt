{ ... }:

{
  programs.fish = {
    enable = true;

    shellInit = ''
      fish_add_path --global --move \
        /run/wrappers/bin \
        /run/current-system/sw/bin \
        /etc/profiles/per-user/$USER/bin \
        $HOME/.nix-profile/bin \
        /nix/var/nix/profiles/default/bin
    '';

    shellAliases = {
      ls = "eza --icons --group-directories-first -1";
      ll = "eza -la --icons --group-directories-first";
      la = "eza -a --icons --group-directories-first";
      lt = "eza --tree --level=2 --icons";

      cleanup = "sudo nix-collect-garbage -d && nix-collect-garbage && nix store optimise && sudo nix-store --gc --print-dead && sudo nix-store --gc";
      optimize = "sudo nix-store --optimise";
      logout = "systemctl restart display-manager.service";
      suspend = "systemctl suspend";
      hibernate = "systemctl hibernate";
      reboot = "systemctl reboot";
      poweroff = "systemctl poweroff";

      docker = "podman";
      dc = "podman-compose";
      dps = "podman ps";
      dpsa = "podman ps -a";
      di = "podman images";
      drm = "podman rm";
      drmi = "podman rmi";

      k = "kubectl";
      kgp = "kubectl get pods";
      kgs = "kubectl get services";
      kgd = "kubectl get deployments";
      kdp = "kubectl describe pod";
      kl = "kubectl logs";

      c = "clear";
      q = "exit";
      ".." = "cd ..";
      "..." = "cd ../..";
      h = "history";
      bottles = "flatpak run com.usebottles.bottles";
      randhex = "openssl rand -hex 32";
    };

    shellAbbrs = {
      lg = "lazygit";
      ld = "lazydocker";
      gd = "git diff";
      ga = "git add .";
      gc = "git commit -am";
      gl = "git log";
      gs = "git status";
      gst = "git stash";
      gsp = "git stash pop";
      gp = "git push";
      gpl = "git pull";
      gsw = "git switch";
      gsm = "git switch main";
      gb = "git branch";
      gbd = "git branch -d";
      gco = "git checkout";
      gsh = "git show";
      l = "ls";
    };

    interactiveShellInit = ''
      if test -f ~/.local/state/caelestia/sequences.txt
          cat ~/.local/state/caelestia/sequences.txt
      end

      # Pinned in hex so Caelestia's muted ANSI palette
      # doesn't make paths/commands blend with the background.
      set -g fish_color_command       B3BCFD --bold
      set -g fish_color_keyword        A178FF --bold
      set -g fish_color_param          E8D3DE
      set -g fish_color_option         FFDCF2
      set -g fish_color_quote          FFDCF2
      set -g fish_color_redirection    ADA0ED --bold
      set -g fish_color_end            A178FF
      set -g fish_color_operator       FFDCF2
      set -g fish_color_escape         44DEF5
      set -g fish_color_autosuggestion 6C6F85
      set -g fish_color_comment        7F7596 --italics
      set -g fish_color_error          FF6B6B --bold
      set -g fish_color_match          --background=4B3F6B
      set -g fish_color_search_match   --background=4B3F6B
      set -g fish_color_selection      --background=35223E
      set -g fish_color_history_current --bold
      set -g fish_pager_color_progress 6C6F85
      set -g fish_pager_color_prefix   B3BCFD --bold
      set -g fish_pager_color_completion E8D3DE
      set -g fish_pager_color_description 7F7596

      # Smart aliases (only if tool exists - avoid breaking `cat` in recovery)
      command -v bat >/dev/null 2>&1 && alias cat='bat'
      command -v rg  >/dev/null 2>&1 && alias grep='rg'
      command -v fd  >/dev/null 2>&1 && alias find='fd'

      # Mark the prompt line for foot's jump-to-prompt feature
      function mark_prompt_start --on-event fish_prompt
          echo -en "\e]133;A\e\\"
      end
    '';

    functions = {
      fish_greeting = ''
        # TAAG font: Doom
        set_color brcyan
        printf '%s\n' ' _    _   ___   _   _ ______  _    _  _____  _      _____'
        printf '%s\n' '| |  | | / _ \ | | | || ___ \| |  | ||  ___|| |    |_   _|'
        printf '%s\n' '| |  | |/ /_\ \| |_| || |_/ /| |  | || |__  | |      | |'
        printf '%s\n' '| |/\| ||  _  ||  _  ||    / | |/\| ||  __| | |      | |'
        printf '%s\n' '\  /\  /| | | || | | || |\ \ \  /\  /| |___ | |____  | |'
        printf '%s\n' ' \/  \/ \_| |_/\_| |_/\_| \_| \/  \/ \____/ \_____/  \_/'
        set_color normal
        set -l wahrwelt_banner_width 58
        set -l fastfetch_width 37
        set -l fastfetch_padding (math --scale=0 "($wahrwelt_banner_width - $fastfetch_width) / 2")
        command -v fastfetch >/dev/null 2>&1 && fastfetch --key-padding-left $fastfetch_padding
      '';

      nixos-switch = ''
        /run/wrappers/bin/sudo /run/current-system/sw/bin/nixos-rebuild switch --impure --flake /etc/nixos#NixOS $argv
      '';

      nixos-update = ''
        set -l old_pwd $PWD

        cd /etc/nixos
        or return $status

        /run/wrappers/bin/sudo /run/current-system/sw/bin/nix flake update $argv
        set -l update_status $status
        if test $update_status -ne 0
            cd "$old_pwd"
            return $update_status
        end

        /run/wrappers/bin/sudo /run/current-system/sw/bin/nixos-rebuild switch --impure --option max-jobs auto --option cores 0 --flake .#NixOS
        set -l rebuild_status $status
        cd "$old_pwd"
        return $rebuild_status
      '';

      nixos-install = ''
        nixos-update $argv
      '';

      wifi-connect = ''
        set iface $argv[1]
        set ssid $argv[2]
        set pass $argv[3]
        /run/wrappers/bin/sudo nmcli dev wifi connect "$ssid" password "$pass"
      '';
    };
  };
}
