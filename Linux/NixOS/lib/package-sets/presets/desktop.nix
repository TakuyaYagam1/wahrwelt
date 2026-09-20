{
  fontSets,
  homeSets,
  runtimeSets,
  systemSets,
  ...
}:

{
  systemPackages = systemSets.desktop;
  homePackages = runtimeSets.waylandTools ++ homeSets.media ++ homeSets.desktop ++ homeSets.misc;
  developmentPackages = [ ];
  fontPackages = fontSets.desktop;
}
