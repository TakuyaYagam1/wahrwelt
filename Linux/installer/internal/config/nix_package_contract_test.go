package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestOmniRouterUsesConstrainedBuildSettings(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/pkgs/omnirouter.nix")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)

	for _, want := range []string{
		`OMNIROUTE_USE_TURBOPACK = "0";`,
		`OMNIROUTE_BUILD_MEMORY_MB = "8192";`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("OmniRouter must declare constrained build setting %q\n%s", want, source)
		}
	}
}

func TestGlancesSkipsSandboxIncompatibleUpstreamChecks(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/lib/package-sets/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)

	glancesOverride := regexp.MustCompile(`glances\.overridePythonAttrs\s*\(_:\s*\{\s*doCheck\s*=\s*false;\s*\}\)`)
	if !glancesOverride.MatchString(source) {
		t.Fatalf("glances must skip its sandbox-incompatible upstream integration checks\n%s", source)
	}
}

func TestApp2UnitUsesScdocCompatibleUpstreamRelease(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/lib/flake-overlays.nix")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)

	for _, want := range []string{
		"app2unitScdocCompatibilityOverlay",
		"app2unit = prev.app2unit.overrideAttrs",
		`version = "1.4.4";`,
		`repo = "app2unit";`,
		`rev = "v1.4.4";`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("app2unit must use a scdoc-compatible upstream release containing %q\n%s", want, source)
		}
	}

	moduleData, err := os.ReadFile("../../../NixOS/lib/flake-modules.nix")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moduleData), "overlays.app2unitScdocCompatibilityOverlay") {
		t.Fatalf("app2unit compatibility overlay is not installed in the host module\n%s", moduleData)
	}
}
