{
  developmentSets,
  homeSets,
  systemSets,
  ...
}:

{
  systemPackages = systemSets.developer;
  homePackages = homeSets.apiTools ++ homeSets.containers ++ homeSets.dev;
  developmentPackages = developmentSets.tools;
  fontPackages = [ ];
}
