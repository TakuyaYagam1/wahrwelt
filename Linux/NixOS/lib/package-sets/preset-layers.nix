{
  developmentSets,
  fontSets,
  homeSets,
  runtimeSets,
  systemSets,
}:

let
  args = {
    inherit
      developmentSets
      fontSets
      homeSets
      runtimeSets
      systemSets
      ;
  };
  deltas = {
    minimal = import ./presets/minimal.nix args;
    desktop = import ./presets/desktop.nix args;
    developer = import ./presets/developer.nix args;
    personal = import ./presets/personal.nix args;
  };
  extend = previous: delta: {
    systemPackages = previous.systemPackages ++ delta.systemPackages;
    homePackages = previous.homePackages ++ delta.homePackages;
    developmentPackages = previous.developmentPackages ++ delta.developmentPackages;
    fontPackages = previous.fontPackages ++ delta.fontPackages;
  };
  presets = rec {
    inherit (deltas) minimal;
    desktop = extend minimal deltas.desktop;
    developer = extend desktop deltas.developer;
    personal = extend developer deltas.personal;
  };
  forPreset = name: presets.${name} or (throw "Unknown Wahrwelt package preset '${name}'");
in
{
  inherit deltas forPreset presets;
}
