package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBaseCLIPackages(t *testing.T) {
	for _, path := range []string{
		"../../../../flake.nix",
		"../../../NixOS/flake.nix",
		"../../../NixOS/lib/preset-inputs.nix",
		"../../../NixOS/presets/developer/flake.nix",
		"../../../NixOS/presets/personal/flake.nix",
	} {
		t.Run(path, func(t *testing.T) {
			source := readBaseCLIContractFile(t, path)
			for _, want := range []string{
				`url = "github:sadjow/claude-code-nix";`,
				`url = "github:sadjow/codex-cli-nix";`,
			} {
				if !strings.Contains(source, want) {
					t.Errorf("%s must retain the base CLI input %q", path, want)
				}
			}
			if strings.Contains(source, "kimi-code") {
				t.Errorf("%s must not include the secondary Kimi CLI input", path)
			}
		})
	}

	overlay := readBaseCLIContractFile(t, "../../../NixOS/lib/flake-overlays.nix")
	for _, want := range []string{
		"claude-code = inputs.claude-code.packages.${system}.default;",
		"codex = inputs.codex.packages.${system}.default;",
	} {
		if !strings.Contains(overlay, want) {
			t.Errorf("developer overlay must retain %q", want)
		}
	}
	if strings.Contains(overlay, "kimi-code") {
		t.Error("developer overlay must not expose the secondary Kimi CLI")
	}

	homePackages := readBaseCLIContractFile(t, "../../../NixOS/lib/package-sets/home.nix")
	devStart := strings.Index(homePackages, "dev = with pkgs; [")
	personalStart := strings.Index(homePackages, "personal = with pkgs; [")
	if devStart < 0 || personalStart < 0 || devStart >= personalStart {
		t.Fatal("could not isolate the developer package set")
	}
	devPackages := strings.Fields(homePackages[devStart:personalStart])
	for _, want := range []string{"claude-code", "codex"} {
		found := false
		for _, name := range devPackages {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("developer must install %s for personal to inherit it", want)
		}
	}
	if strings.Contains(homePackages, "kimi-code") {
		t.Error("home package sets must not install the secondary Kimi CLI")
	}
}

func TestFlakeLocksExcludeSecondaryCLI(t *testing.T) {
	for _, path := range []string{
		"../../../../flake.lock",
		"../../../NixOS/flake.lock",
		"../../../NixOS/presets/minimal/flake.lock",
		"../../../NixOS/presets/desktop/flake.lock",
		"../../../NixOS/presets/developer/flake.lock",
		"../../../NixOS/presets/personal/flake.lock",
	} {
		t.Run(path, func(t *testing.T) {
			source := readBaseCLIContractFile(t, path)
			if !json.Valid([]byte(source)) {
				t.Fatalf("%s must remain valid JSON", path)
			}
			for _, removed := range []string{"kimi-code", "MoonshotAI"} {
				if strings.Contains(source, removed) {
					t.Errorf("%s retains the secondary CLI dependency %q", path, removed)
				}
			}
		})
	}
}

func readBaseCLIContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
