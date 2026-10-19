package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNixCleanupModes(t *testing.T) {
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skip("nix is required to render the cleanup commands")
	}
	root, err := filepath.Abs("../../../NixOS")
	if err != nil {
		t.Fatal(err)
	}
	for _, retention := range []string{"14d", "30d"} {
		t.Run(retention, func(t *testing.T) {
			expression := fmt.Sprintf(`
let
  root = builtins.toPath %q;
  lib = {
    mkDefault = value: value;
    escapeShellArg = value: "'" + builtins.replaceStrings ["'"] ["'\\''"] value + "'";
  };
  wahrwelt = {
    host = { configDirectory = "/etc/nixos"; hostname = "NixOS"; };
    nix.gcRetention = %q;
  };
  helper = import (root + "/home/programs/nixos-helper.nix") {
    inherit lib wahrwelt;
    inputs = {};
    pkgs.writeShellApplication = args: args;
  };
  fish = import (root + "/home/programs/fish.nix") {};
  settings = (import (root + "/system/settings.nix") { inherit lib; config = {}; }).nix.settings;
in {
  script = (builtins.head helper.home.packages).text;
  cleanup = fish.programs.fish.shellAliases.cleanup;
  full = fish.programs.fish.shellAliases.clean-full;
  keepOutputs = settings.keep-outputs;
  keepDerivations = settings.keep-derivations;
}`, root, retention)
			output, err := exec.Command("nix", "eval", "--offline", "--impure", "--json", "--expr", expression).CombinedOutput()
			if err != nil {
				t.Fatalf("render cleanup: %v\n%s", err, output)
			}
			var rendered struct {
				Script          string
				Cleanup         string
				Full            string
				KeepOutputs     bool
				KeepDerivations bool
			}
			if err := json.Unmarshal(output, &rendered); err != nil {
				t.Fatal(err)
			}
			if rendered.Cleanup != "nixy gc" || rendered.Full != "nixy gc-full" {
				t.Fatalf("cleanup aliases select the wrong modes: %s, %s", rendered.Cleanup, rendered.Full)
			}
			if !rendered.KeepOutputs || !rendered.KeepDerivations {
				t.Fatal("automatic GC must preserve available build dependencies")
			}
			for _, mode := range []string{"gc", "gc-full"} {
				for _, failure := range []string{"none", "user", "root"} {
					t.Run(mode+"/"+failure, func(t *testing.T) {
						// Shell functions intercept every cleanup command. No host GC runs.
						stub := `set -euo pipefail
scope=user
nix-collect-garbage() {
  printf 'CALL %s gc %s\n' "$scope" "$*"
  if [[ "$scope" == "$FAIL_SCOPE" ]]; then return 17; fi
}
sudo() { local scope=root; "$@"; }
nix-store() { printf 'CALL %s store %s\n' "$scope" "$*"; }
`
						cmd := exec.Command("bash", "-s", "--", mode)
						cmd.Env = append(os.Environ(), "FAIL_SCOPE="+failure)
						cmd.Stdin = strings.NewReader(stub + rendered.Script)
						output, runErr := cmd.CombinedOutput()
						if (runErr != nil) != (failure != "none") {
							t.Fatalf("unexpected exit: %v\n%s", runErr, output)
						}
						var calls []string
						for line := range strings.SplitSeq(string(output), "\n") {
							if strings.HasPrefix(line, "CALL ") {
								calls = append(calls, line)
							}
						}
						args := "--delete-older-than " + retention + " --option keep-derivations true --option keep-outputs true"
						if mode == "gc-full" {
							args = "--delete-old --option keep-derivations true --option keep-outputs false"
						}
						want := []string{"CALL user gc " + args}
						if failure != "user" {
							want = append(want, "CALL root gc "+args)
						}
						if failure == "none" {
							want = append(want, "CALL root store --optimise")
						}
						if strings.Join(calls, "\n") != strings.Join(want, "\n") {
							t.Fatalf("cleanup calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
						}
					})
				}
			}
		})
	}
}
