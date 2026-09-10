package config

import (
	"os"
	"strings"
	"testing"
)

func TestCaelestiaLiveWallpapersIntegrationIsVendoredAndDefault(t *testing.T) {
	for _, path := range []string{
		"../../../../flake.nix",
		"../../../NixOS/flake.nix",
		"../../../NixOS/lib/preset-inputs.nix",
		"../../../NixOS/presets/desktop/flake.nix",
		"../../../NixOS/presets/developer/flake.nix",
		"../../../NixOS/presets/personal/flake.nix",
	} {
		source := readCaelestiaLiveWallpapersContractFile(t, path)
		if strings.Contains(source, "caelestia-live-wallpapers") || strings.Contains(source, "SunnydeuS/Caelestia-Live-Wallpapers-Integration") {
			t.Fatalf("%s must not retain the obsolete external live wallpaper input\n%s", path, source)
		}
	}

	options := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/modules/mysetup-options.nix")
	if strings.Contains(options, "caelestiaLiveWallpapers") {
		t.Fatalf("live wallpaper support is part of every desktop shell and must not require a feature flag\n%s", options)
	}

	hostVars := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/hosts/NixOS/host-vars.nix")
	if strings.Contains(hostVars, "caelestiaLiveWallpapers") {
		t.Fatalf("host variables must rely on the preset default instead of duplicating the feature flag\n%s", hostVars)
	}

	homeModule := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/home/caelestia/default.nix")
	for _, want := range []string{
		"wahrweltPkgs.caelestia-shell",
		"wahrweltPkgs.caelestia-cli",
	} {
		if !strings.Contains(homeModule, want) {
			t.Fatalf("Caelestia Home Manager integration is missing %q\n%s", want, homeModule)
		}
	}
	if strings.Contains(homeModule, "Live-Wallpapers") || strings.Contains(homeModule, "CAELESTIA_LIVE_WALLPAPERS_DIR") {
		t.Fatalf("live and static wallpapers must share ~/Pictures/Wallpapers\n%s", homeModule)
	}
}

func TestCaelestiaLiveWallpapersPackageIsStrictAndPrivate(t *testing.T) {
	pkg := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/home/caelestia/patches/package.nix")
	for _, want := range []string{
		"overridePythonAttrs",
		"--fuzz=0",
		"prev.qt6.qtmultimedia",
		"update-caelestia-live-thumbs",
		"inherit cli;",
		"shell = shellBase.overrideAttrs",
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
	if strings.Contains(pkg, "inherit thumbnailTool") {
		t.Fatalf("thumbnail helper must remain private to the patched shell\n%s", pkg)
	}

	provenance := readCaelestiaLiveWallpapersContractFile(t, "../../../NixOS/home/caelestia/patches/vendor/NOTICE.md")
	for _, want := range []string{
		"SunnydeuS/Caelestia-Live-Wallpapers-Integration",
		"3e2b9770e360c9159970b4ebbe6483cddd7529df",
		"GNU GPL-3.0-only",
	} {
		if !strings.Contains(provenance, want) {
			t.Fatalf("vendored Caelestia source is missing provenance %q\n%s", want, provenance)
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
