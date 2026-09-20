{
  lib,
  wahrwelt,
  wahrweltLib,
  ...
}:

{
  programs.btop = lib.mkIf (wahrweltLib.presets.desktopOrMore wahrwelt) {
    enable = true;
    settings = {
      theme_background = false;
      truecolor = true;
      force_tty = false;
      vim_keys = false;
      rounded_corners = true;
      graph_symbol = "braille";
      graph_symbol_cpu = "default";
      graph_symbol_gpu = "default";
      graph_symbol_mem = "default";
      graph_symbol_net = "default";
      graph_symbol_proc = "default";
      shown_boxes = "cpu mem net proc";
      update_ms = 2000;
      proc_sorting = "cpu lazy";
      proc_tree = false;
      presets = "cpu:1:default,proc:0:default cpu:0:default,mem:0:default,net:0:default cpu:0:block,net:0:tty";
    };
  };
}
