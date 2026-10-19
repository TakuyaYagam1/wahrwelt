_:

let
  ansi = code: builtins.fromJSON ''"\u001b[${code}m"'';
in
{
  programs.fastfetch = {
    enable = true;
    settings = {
      "$schema" = "https://github.com/fastfetch-cli/fastfetch/raw/dev/doc/json_schema.json";
      logo = null;
      display = {
        separator = "  ";
        color = "white";
        constants = [
          (ansi "37")
          (ansi "38;5;16")
          (ansi "38;5;17")
          (ansi "38;5;18")
        ];
      };
      modules = [
        "break"
        {
          type = "custom";
          key = "╭───────────────────────────────────╮";
        }
        {
          type = "kernel";
          key = "│ {$2}{$1}  kernel";
          format = "{$2}{release>22}{$1} │";
        }
        {
          type = "uptime";
          key = "│   uptime";
          format = "{formatted>22} │";
        }
        {
          type = "shell";
          key = "│ {$2}{$1}  shell ";
          format = "{$2}{pretty-name>22}{$1} │";
        }
        {
          type = "command";
          key = "│ {$3}{$1}  mem   ";
          text = ''free -m | awk 'NR==2{printf "%.2f GiB / %.2f GiB",$3/1024,$2/1024}' '';
          format = "{$3}{>22}{$1} │";
        }
        {
          type = "packages";
          key = "│   pkgs  ";
          format = "{all>22} │";
        }
        {
          type = "command";
          key = "│ {$2}{$1}  user  ";
          text = "echo $USER";
          format = "{$2}{>22}{$1} │";
        }
        {
          type = "command";
          key = "│   hname ";
          text = "hostnamectl hostname";
          format = "{>22} │";
        }
        {
          type = "os";
          key = "│ {$4}󰻀{$1}  distro";
          format = "{$4}{pretty-name>22}{$1} │";
        }
        {
          type = "custom";
          key = "╰───────────────────────────────────╯";
        }
        "break"
      ];
    };
  };
}
