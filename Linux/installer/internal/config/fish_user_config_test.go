//go:build integration

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fishUserModule struct {
	ShellInitLast string
	Aliases       map[string]string
	Functions     map[string]string
	Guarded       []string
	Mutable       map[string]struct {
		Policy    string
		Validator string
		Contents  string
	}
}

func TestFishUserConfigOverridesDefaults(t *testing.T) {
	module := renderFishUserModule(t)
	home, configDir := fishUserHome(t)
	writeFishUserFixture(t, filepath.Join(configDir, "user/config.fish"), `alias ls 'echo personal-alias'
set -g fish_color_command personal-color
function nixos-update
    echo personal-config-function
end
`)
	writeFishUserFixture(t, filepath.Join(configDir, "user/functions/nixos-update.fish"), "function nixos-update; echo personal-file-function; end\n")
	script := fishUserStartup(module) + `
ls
echo $fish_color_command
nixos-update
`
	output, err := runFishUserScript(t, home, configDir, script)
	if err != nil {
		t.Fatalf("load personal configuration: %v\n%s", err, output)
	}
	if output != "personal-alias\npersonal-color\npersonal-config-function\n" {
		t.Fatalf("personal configuration did not run after generated defaults: %q", output)
	}
}

func TestFishUserFunctionsAndCompletionsTakePrecedence(t *testing.T) {
	module := renderFishUserModule(t)
	home, configDir := fishUserHome(t)
	writeFishUserFixture(t, filepath.Join(configDir, "user/functions/additional_function.fish"), "function additional_function; echo personal-update; end\n")
	writeFishUserFixture(t, filepath.Join(configDir, "user/functions/personal_function.fish"), "function personal_function; echo personal-function; end\n")
	writeFishUserFixture(t, filepath.Join(configDir, "completions/wahrwelt-test.fish"), "complete -c wahrwelt-test -f -a default-completion\n")
	writeFishUserFixture(t, filepath.Join(configDir, "user/completions/wahrwelt-test.fish"), "complete -c wahrwelt-test -f -a personal-completion\n")
	for name, body := range module.Functions {
		writeFishUserFixture(t, filepath.Join(configDir, "functions", name+".fish"), "function "+name+"\n"+body+"\nend\n")
	}
	// Force Home Manager's generated function to autoload before the user hook.
	// Merely testing a new function would miss stale autoload-cache precedence.
	script := "set -p fish_function_path " + fishUserQuote(filepath.Join(configDir, "functions")) + "\n" +
		"set -p fish_complete_path " + fishUserQuote(filepath.Join(configDir, "completions")) + "\n" +
		"functions --query additional_function; or exit 90\n" + module.ShellInitLast + `
additional_function
personal_function
complete -C 'wahrwelt-test '
`
	output, err := runFishUserScript(t, home, configDir, script)
	if err != nil {
		t.Fatalf("load user function/completion directories: %v\n%s", err, output)
	}
	if output != "personal-update\npersonal-function\npersonal-completion\n" {
		t.Fatalf("user functions/completions did not win: %q", output)
	}
}

func TestFishUserConfigErrorsRemainVisible(t *testing.T) {
	module := renderFishUserModule(t)
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "explicit failure", content: "echo personal-failure >&2\nreturn 17\n", want: "personal-failure"},
		{name: "syntax error", content: "if\n", want: "user/config.fish"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, configDir := fishUserHome(t)
			writeFishUserFixture(t, filepath.Join(configDir, "user/config.fish"), tc.content)
			startup := filepath.Join(home, "startup.fish")
			writeFishUserFixture(t, startup, fishUserStartup(module))
			output, err := runFishUserScript(t, home, configDir, "source "+fishUserQuote(startup)+"\nexit $status\n")
			if err == nil || !strings.Contains(output, tc.want) {
				t.Fatalf("personal configuration error was swallowed: err=%v, output=%q", err, output)
			}
			if tc.name == "explicit failure" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 17 {
					t.Fatalf("personal config exit status changed: %v", err)
				}
			}
		})
	}
}

func TestFishUserConfigMissingIsAllowed(t *testing.T) {
	module := renderFishUserModule(t)
	home, configDir := fishUserHome(t)
	output, err := runFishUserScript(t, home, configDir, module.ShellInitLast+"\nexit $status\n")
	if err != nil || output != "" {
		t.Fatalf("missing optional user configuration: err=%v, output=%q", err, output)
	}
}

func TestFishUserFilesAreSeededAndGeneratedFilesAreGuarded(t *testing.T) {
	module := renderFishUserModule(t)
	for _, path := range []string{"fish/user/config.fish", "fish/user/functions/.keep", "fish/user/completions/.keep"} {
		file, ok := module.Mutable[path]
		if !ok || file.Policy != "seed" {
			t.Errorf("%s must be initialized once without merging personal changes", path)
		}
	}
	if module.Mutable["fish/user/config.fish"].Validator != "fish" {
		t.Error("the initial user config must be checked as Fish syntax")
	}
	for _, path := range []string{
		"fish/config.fish", "fish/functions/nixos-update.fish", "fish/functions/additional_function.fish",
		"fish/completions/additional_completion.fish", "fish/conf.d/plugin-additional-plugin.fish",
	} {
		if !slices.Contains(module.Guarded, path) {
			t.Errorf("generated file is missing replacement protection: %s", path)
		}
	}
	for _, path := range module.Guarded {
		if strings.HasPrefix(path, "fish/user/") {
			t.Errorf("personal file must remain writable: %s", path)
		}
	}
}

func renderFishUserModule(t *testing.T) fishUserModule {
	t.Helper()
	for _, binary := range []string{"fish", "nix"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skip(binary + " is required for Fish configuration integration tests")
		}
	}
	path, err := filepath.Abs("../../../NixOS/home/programs/fish.nix")
	if err != nil {
		t.Fatal(err)
	}
	expression := fmt.Sprintf(`let
  module = import (builtins.toPath %q) { inherit config; };
  config.programs.fish = module.programs.fish // {
    functions = module.programs.fish.functions // { additional_function = "echo additional"; };
    completions.additional_completion = "complete -c additional -f";
    plugins = [ { name = "additional-plugin"; } ];
  };
in {
  ShellInitLast = module.programs.fish.shellInitLast or "";
  Aliases = module.programs.fish.shellAliases;
  Functions = config.programs.fish.functions;
  Guarded = module.wahrwelt.guardedFiles or [];
  Mutable = builtins.mapAttrs (_: entry: {
    inherit (entry) policy validator;
    contents = builtins.readFile entry.source;
  }) (module.wahrwelt.mutableFiles or {});
}`, path)
	output, err := exec.Command("nix", "eval", "--offline", "--impure", "--json", "--expr", expression).CombinedOutput()
	if err != nil {
		t.Fatalf("evaluate Fish module: %v\n%s", err, output)
	}
	var module fishUserModule
	if err := json.Unmarshal(output, &module); err != nil {
		t.Fatal(err)
	}
	return module
}

func fishUserHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	// A path containing spaces also proves the hook honors XDG_CONFIG_HOME.
	configDir := filepath.Join(home, "custom config", "fish")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "wahrwelt-test"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home, configDir
}

func fishUserStartup(module fishUserModule) string {
	// Home Manager puts aliases and interactiveShellInit before shellInitLast.
	var startup strings.Builder
	for name, body := range module.Aliases {
		fmt.Fprintf(&startup, "alias %s %s\n", fishUserQuote(name), fishUserQuote(body))
	}
	startup.WriteString("set -g fish_color_command generated-color\n")
	startup.WriteString(module.ShellInitLast)
	return startup.String()
}

func runFishUserScript(t *testing.T, home, configDir, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("fish", "--no-config", "--interactive", "--command", script)
	cmd.Env = []string{
		"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Dir(configDir),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, "state"), "XDG_CONFIG_DIRS=" + filepath.Join(home, "config-dirs"),
		// No host command (especially an update/activation command) is reachable.
		"PATH=" + filepath.Join(home, "bin"), "TERM=xterm-256color",
	}
	cmd.Dir = home
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeFishUserFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fishUserQuote(value string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(value) + "'"
}
