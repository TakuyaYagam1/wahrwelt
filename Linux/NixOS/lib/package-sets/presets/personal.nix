{
  developmentSets,
  homeSets,
  systemSets,
  ...
}:

{
  systemPackages = systemSets.personal ++ systemSets.personalStable;
  homePackages = homeSets.personal ++ homeSets.games;
  developmentPackages = developmentSets.personalTools;
  fontPackages = [ ];
}
