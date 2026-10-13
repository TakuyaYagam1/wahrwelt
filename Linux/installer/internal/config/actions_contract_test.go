package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGitHubActionsUseVersionTags(t *testing.T) {
	workflowPaths, err := filepath.Glob("../../../../.github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(workflowPaths) == 0 {
		t.Fatal("no GitHub Actions workflows found")
	}

	usesAction := regexp.MustCompile(`(?m)^\s*uses:\s+([^\s@]+)@([^\s#]+)`)
	versionTag := regexp.MustCompile(`^v[0-9]+(?:\.[0-9]+){0,2}$`)

	for _, path := range workflowPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)

		for _, match := range usesAction.FindAllStringSubmatch(source, -1) {
			action, ref := match[1], match[2]
			if !versionTag.MatchString(ref) {
				t.Fatalf("%s must use a version tag for %s, got %q", path, action, ref)
			}
		}
	}
}

func TestLintingUsesNixToolchainWithoutApt(t *testing.T) {
	workflow, err := os.ReadFile("../../../../.github/workflows/linting.yml")
	if err != nil {
		t.Fatal(err)
	}
	source := string(workflow)

	for _, forbidden := range []string{"apt-get", "apt install", "sudo apt"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("linting workflow must not depend on APT mirrors: found %q\n%s", forbidden, source)
		}
	}

	for _, required := range []string{
		"timeout-minutes:",
		"nix profile add",
		"nixpkgs#fish",
		"nixpkgs#shellcheck",
		"nixpkgs#jq",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("linting workflow must provide its toolchain through Nix: missing %q\n%s", required, source)
		}
	}
}

func TestInstallerUsesLatestGoInNixAndModuleVersionInCI(t *testing.T) {
	packages, err := os.ReadFile("../../../NixOS/lib/flake-packages.nix")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(packages), "wahrwelt = flakePkgs.buildGoLatestModule {") {
		t.Fatal("installer Nix build must use go_latest, not nixpkgs' older default Go")
	}
	workflow, err := os.ReadFile("../../../../.github/workflows/linting.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "go-version-file: Linux/installer/go.mod") {
		t.Fatal("installer CI must derive its Go version from go.mod")
	}
}

func TestLintingBuildsVersionedV1ToV2MigrationUnit(t *testing.T) {
	workflow, err := os.ReadFile("../../../../.github/workflows/linting.yml")
	if err != nil {
		t.Fatal(err)
	}
	source := string(workflow)
	currentUnit := `nixosConfigurations.NixOS.config.systemd.units.\"wahrwelt-v1-to-v2-migration.service\".unit`
	if !strings.Contains(source, currentUnit) {
		t.Fatalf("linting workflow does not build the current migration unit %q\n%s", currentUnit, source)
	}
	if strings.Contains(source, `wahrwelt-brand-migration.service`) {
		t.Fatalf("linting workflow still builds the removed brand migration unit\n%s", source)
	}
}

func TestPresetFlakeUpdaterUsesDirectoryFlakeReferences(t *testing.T) {
	workflow, err := os.ReadFile("../../../../.github/workflows/update-flake.yml")
	if err != nil {
		t.Fatal(err)
	}
	source := string(workflow)

	for _, want := range []string{
		`update_flake_without_codex "Linux/NixOS/presets/${preset}"`,
		`nix flake update "${input_names[@]}"`,
		`nix eval --raw --no-write-lock-file`,
		`"path:${GITHUB_WORKSPACE}?dir=Linux/NixOS/presets/${preset}#checks.x86_64-linux.preset-host.drvPath"`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("preset updater must use directory flake reference %q\n%s", want, source)
		}
	}

	if strings.Contains(source, `nix flake check --no-build --no-write-lock-file "$preset_flake"`) {
		t.Fatalf("preset checks must evaluate preset-host.drvPath instead of checking unbuilt outputs\n%s", source)
	}
}

func TestFlakeUpdaterValidatesEnd4PCIntegration(t *testing.T) {
	workflow, err := os.ReadFile("../../../../.github/workflows/update-flake.yml")
	if err != nil {
		t.Fatal(err)
	}
	source := string(workflow)

	for _, want := range []string{
		"Verify end4-pC input ownership",
		`'.nodes.root.inputs["end4-pc"]'`,
		"Linux/NixOS/presets/minimal/flake.lock",
		"Linux/NixOS/presets/desktop/flake.lock",
		"Linux/NixOS/presets/developer/flake.lock",
		"Linux/NixOS/presets/personal/flake.lock",
		"Build end4-pC Quickshell config",
		`xdg.configFile."quickshell/end4-pC".source`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("flake updater must validate the end4-pC integration: missing %q\n%s", want, source)
		}
	}
}

func TestLocalCheckValidatesEnd4PCSources(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	source := string(makefile)

	for _, want := range []string{
		"quickshell-pc.py",
		`xdg.configFile.\"quickshell/end4-pC\".source`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("local check must validate end4-pC sources: missing %q\n%s", want, source)
		}
	}
}
