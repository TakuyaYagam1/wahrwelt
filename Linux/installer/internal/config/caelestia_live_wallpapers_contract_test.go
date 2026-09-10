package config

import (
	"os"
	"strings"
	"testing"
)

func TestCaelestiaLiveWallpapersIntegrationIsPinnedAndOptional(t *testing.T) {
	const input = `url = "github:SunnydeuS/Caelestia-Live-Wallpapers-Integration/3e2b9770e360c9159970b4ebbe6483cddd7529df";`
	for _, path := range []string{
		"../../../../flake.nix",
		"../../../NixOS/flake.nix",
		"../../../NixOS/lib/preset-inputs.nix",
		"../../../NixOS/presets/desktop/flake.nix",
		"../../../NixOS/presets/developer/flake.nix",
		"../../../NixOS/presets/personal/flake.nix",
	} {
		source := readCaelestiaLiveWallpapersContractFile(t, path)
		if !strings.Contains(source, input) || !strings.Contains(source, "flake = false;") {
			t.Fatalf("%s must pin the non-flake live wallpaper source\n%s", path, source)
		}
	}

	minimal := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/presets/minimal/flake.nix")
	if strings.Contains(minimal, "caelestia-live-wallpapers") {
		t.Fatalf("minimal preset must not include the desktop-only integration\n%s", minimal)
	}

	options := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/modules/mysetup-options.nix")
	if !strings.Contains(options, "caelestiaLiveWallpapers = boolOption false;") {
		t.Fatalf("live wallpapers must default to disabled\n%s", options)
	}

	homeModule := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/home/caelestia/default.nix")
	for _, want := range []string{
		"wahrwelt.features.caelestiaLiveWallpapers",
		"wahrweltPkgs.caelestia-live-shell",
		"wahrweltPkgs.caelestia-live-cli",
	} {
		if !strings.Contains(homeModule, want) {
			t.Fatalf("Caelestia Home Manager integration is missing %q\n%s", want, homeModule)
		}
	}
	if strings.Contains(homeModule, "Live-Wallpapers") || strings.Contains(homeModule, "CAELESTIA_LIVE_WALLPAPERS_DIR") {
		t.Fatalf("live and static wallpapers must share ~/Pictures/Wallpapers\n%s", homeModule)
	}
}

func TestCaelestiaLiveWallpapersPackageAvoidsMutableSystemPaths(t *testing.T) {
	pkg := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/pkgs/caelestia-live-wallpapers.nix")
	for _, want := range []string{
		"overridePythonAttrs",
		"patchPhase = (old.patchPhase or \"\")",
		"prev.qt6.qtmultimedia",
		"update-caelestia-live-thumbs",
		"command: [\"${thumbnailTool}/bin/update-caelestia-live-thumbs\"",
		"readonly property list<string> videoExtensions",
		"function isVideoPath(path: string): bool",
		"if (filterMode === 1 || filterMode === 2)",
		"path: Paths.wallsdir",
	} {
		if !strings.Contains(pkg, want) {
			t.Fatalf("Caelestia live wallpaper package is missing %q\n%s", want, pkg)
		}
	}
	for _, forbidden := range []string{"/usr/lib", "/etc/xdg"} {
		if strings.Contains(pkg, forbidden) {
			t.Fatalf("Caelestia live wallpaper package must not write to %q\n%s", forbidden, pkg)
		}
	}
}

func TestCaelestiaUpdaterBuildsSelectedLiveWallpaperPackages(t *testing.T) {
	workflow := readCaelestiaLiveWallpapersContractFile(t, "../../../../.github/workflows/update-caelestia.yml")
	for _, want := range []string{
		"programs.caelestia.package",
		"programs.caelestia.cli.package",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("Caelestia updater must build %q\n%s", want, workflow)
		}
	}
}

func readCaelestiaLiveWallpapersContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
