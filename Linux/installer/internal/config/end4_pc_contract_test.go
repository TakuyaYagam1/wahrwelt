package config

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestEnd4PCInputIsOwnedByDesktopAndLargerPresets(t *testing.T) {
	for _, path := range []string{
		"../../../../flake.nix",
		"../../../NixOS/flake.nix",
		"../../../NixOS/lib/preset-inputs.nix",
		"../../../NixOS/presets/desktop/flake.nix",
		"../../../NixOS/presets/developer/flake.nix",
		"../../../NixOS/presets/personal/flake.nix",
	} {
		source := readContractFile(t, path)
		for _, want := range []string{
			"end4-pc",
			"github:pctrade/end4-pC",
			"flake = false;",
		} {
			if !strings.Contains(source, want) {
				t.Fatalf("%s must own the end4-pC non-flake input: missing %q", path, want)
			}
		}
	}

	minimal := readContractFile(t, "../../../NixOS/presets/minimal/flake.nix")
	if strings.Contains(minimal, "end4-pc") || strings.Contains(minimal, "end4-pC") {
		t.Fatalf("minimal preset must not include end4-pC\n%s", minimal)
	}
}

func TestEnd4PCQuickshellIsImmutableAndSharesEnd4Config(t *testing.T) {
	module := readContractFile(t, "../../../NixOS/home/end4/patches/quickshell-pc.nix")

	for _, want := range []string{
		`inputs.end4-pc`,
		`xdg.configFile."quickshell/end4-pC"`,
		`source = "${patchedQuickshellPC}";`,
		`~/.config/illogical-impulse/config.json`,
		`Wahrwelt manages end4-pC updates through the flake input`,
	} {
		if !strings.Contains(module, want) {
			t.Fatalf("end4-pC module must provide immutable shared-config integration: missing %q\n%s", want, module)
		}
	}

	if strings.Contains(module, "git clone https://github.com/pctrade/end4-pC") {
		t.Fatalf("the installed end4-pC source must not retain its mutable self-updater\n%s", module)
	}
}

func TestEnd4PCIdleControlsStayNixManaged(t *testing.T) {
	patcher := readContractFile(t, "../../../NixOS/home/end4/patches/quickshell-pc.py")
	for _, want := range []string{
		"patch_managed_hypridle_settings",
		"Idle timers are managed by Wahrwelt through NixOS configuration",
		`visible: false`,
		`pkill -x hypridle`,
	} {
		if !strings.Contains(patcher, want) {
			t.Fatalf("end4-pC patcher must keep idle settings under Nix ownership: missing %q\n%s", want, patcher)
		}
	}

	seed := readContractFile(t, "../../../NixOS/home/end4/seed/config.nix")
	if strings.Contains(seed, ".hyprland.idle") {
		t.Fatalf("end4 config seed must not rewrite user-owned idle settings\n%s", seed)
	}
}

func TestEnd4HyprPatchDefersInputGesturesAndQsConfigToAdapter(t *testing.T) {
	settings := readContractFile(t, "../../../NixOS/home/end4/settings.nix")
	for _, forbidden := range []string{
		"workspaceSwipeFingers",
		"keyboardLayouts",
		"keyboardToggle",
	} {
		if strings.Contains(settings, forbidden) {
			t.Fatalf("end4 settings must not duplicate canonical input or gesture ownership: found %q\n%s", forbidden, settings)
		}
	}

	module := readContractFile(t, "../../../NixOS/home/end4/patches/hypr.nix")
	for _, forbidden := range []string{
		`--replace-fail '        kb_layout = "us",'`,
		`settings.keyboard`,
		`settings.gestures.workspaceSwipeFingers`,
		`--replace-fail 'hl.env("qsConfig", "ii")'`,
		`dofile(config_home .. "/hypr/shell-common-rules.lua")`,
		`cat >`,
		`<<'EOF'`,
		`$out/monitors.conf`,
	} {
		if strings.Contains(module, forbidden) {
			t.Fatalf("end4 Hypr patch retained temporary ownership or heredoc %q\n%s", forbidden, module)
		}
	}
	for _, want := range []string{
		`hyprEnvPrelude = pkgs.writeText`,
		`hypridleConf = pkgs.writeText`,
		`customGeneralLua = pkgs.writeText`,
		`customRulesLua = pkgs.writeText`,
		`customKeybindsLua = pkgs.writeText`,
		`source = validatedHypr;`,
		`home.activation.guardWahrweltEnd4Ownership`,
		`lib.hm.dag.entryBefore [ "checkLinkTargets" ]`,
		`builtins.readFile ./end4-ownership-guard.sh`,
		`missing app-aware close overlay`,
		`contains a direct common-rules hook`,
		`strict_patch_two_lines "$out/hyprland/keybinds.lua"`,
		`Wahrwelt start-shell owns end4 QuickShell restart lifecycle`,
		`hypr/scripts/tests/end4-artifact-test.sh`,
		`contains direct hypridle lifecycle startup`,
	} {
		if !strings.Contains(module, want) {
			t.Fatalf("end4 Hypr patch is missing validated artifact contract %q\n%s", want, module)
		}
	}
	if !regexp.MustCompile(`validatedHypr\s*=\s*pkgs\.runCommand`).MatchString(module) {
		t.Fatalf("end4 Hypr patch is missing the validated artifact derivation\n%s", module)
	}
	end4SourceStart := strings.Index(module, `xdg.configFile."hypr/end4"`)
	if end4SourceStart < 0 {
		t.Fatalf("Home Manager End4 source block is missing\n%s", module)
	}
	end4SourceBlock := module[end4SourceStart:]
	end4SourceBlock = strings.SplitN(end4SourceBlock, "};", 2)[0]
	if strings.Contains(end4SourceBlock, "force = true;") {
		t.Fatalf("Home Manager End4 tree must fail closed on ownership collisions\n%s", end4SourceBlock)
	}
	launcherStart := strings.Index(module, `end4SettingsLauncher = pkgs.writeShellApplication`)
	launcherEnd := strings.Index(module, `patchedHypr =`)
	if launcherStart < 0 || launcherEnd <= launcherStart {
		t.Fatalf("managed End4 settings launcher block is unavailable\n%s", module)
	}
	launcher := module[launcherStart:launcherEnd]
	for _, want := range []string{
		`active_profile_file="$state_home/wahrwelt/active-shell"`,
		`end4-pc)`,
		`end4)`,
	} {
		if !strings.Contains(launcher, want) {
			t.Fatalf("managed End4 settings launcher is missing %q\n%s", want, launcher)
		}
	}
	for _, forbidden := range []string{`case ${qsConfig}`, `systemsettings`} {
		if strings.Contains(launcher, forbidden) {
			t.Fatalf("managed End4 settings launcher retained fallback %q\n%s", forbidden, launcher)
		}
	}
}

func TestHyprHomeManagerOwnsAdapterAndSeedsOnlyDefault(t *testing.T) {
	shells := readContractFile(t, "../../../NixOS/home/shells/default.nix")
	defaultTemplate := `defaultHyprUserConfig = pkgs.writeText "wahrwelt-hypr-default" ''
    local wahrwelt = require("lib.wahrwelt")

    wahrwelt.optional_require("wahrwelt.execs")
    wahrwelt.optional_require("wahrwelt.general")
    wahrwelt.optional_require("wahrwelt.rules")
    wahrwelt.optional_require("wahrwelt.keybinds")`
	if !strings.Contains(shells, defaultTemplate) {
		t.Fatalf("Home Manager default.lua seed must keep the exact canonical template\n%s", shells)
	}
	for _, want := range []string{
		`"hypr/end4-adapter.lua"`,
		`source = dotsRoot + "/hypr/end4-adapter.lua";`,
		`"hypr/user/hyprland.lua"`,
		`activate-user-dir`,
		`"${defaultHyprUserConfig}"`,
		`-- Wahrwelt shell adapter: ${defaultProfile.id}`,
		`require("${defaultProfile.adapter}")`,
		`"$activation_helper" activate-runtime-dir`,
		`"$hypr_runtime_source"`,
		`wahrwelt_v1_to_v2_direct_end4_evidence`,
		`"${../migrations/v1_to_v2/hypr-runtime/end4.lua}"`,
		`"${../migrations/v1_to_v2/hypr-runtime/end4-pc.lua}"`,
		`pkgs.python3`,
	} {
		if !strings.Contains(shells, want) {
			t.Fatalf("Home Manager Hypr ownership contract missing %q\n%s", want, shells)
		}
	}
	for _, forbidden := range []string{
		`"hypr/user/default.lua" =`,
		`optional_require("user.`,
		`user/keybinds.lua`,
		`user/local.lua`,
		`pruneLegacyHyprlandRuntime`,
		`pruneWahrweltHyprBackups`,
		`backupTargets`,
		`legacyWahrweltRuntime =`,
		`legacyHomeManagerWahrweltRuntime =`,
		`legacySeededWahrweltRuntime =`,
		`legacySeededUserRuntime =`,
	} {
		if strings.Contains(shells, forbidden) {
			t.Fatalf("Home Manager must preserve user Lua ownership: found %q\n%s", forbidden, shells)
		}
	}
	canonicalStart := strings.Index(shells, `canonicalHyprRuntime = pkgs.writeText`)
	canonicalEnd := strings.Index(shells, `defaultHyprUserConfig = pkgs.writeText`)
	if canonicalStart < 0 || canonicalEnd <= canonicalStart {
		t.Fatalf("Home Manager canonical runtime block is unavailable\n%s", shells)
	}
	if strings.Contains(shells[canonicalStart:canonicalEnd], `dofile(runtime_root .. "/shell-profile.lua")`) {
		t.Fatalf("canonical runtime retained the historical shell-profile side effect\n%s", shells[canonicalStart:canonicalEnd])
	}
	migration := readContractFile(t, "../../../NixOS/home/migrations/v1_to_v2/user-paths.nix")
	if !strings.Contains(migration, `transitionHyprRuntime = ./hypr-runtime/user-namespace-transition.lua;`) {
		t.Fatalf("v1 to v2 migration lost the historical namespace transition payload\n%s", migration)
	}

	environment := readContractFile(t, "../../../NixOS/home/end4/environment.nix")
	for _, forbidden := range []string{"home.sessionVariables", "systemd.user.sessionVariables"} {
		if strings.Contains(environment, forbidden) {
			t.Fatalf("End4-only variables must stay local to qs-end4: found %q\n%s", forbidden, environment)
		}
	}

	if _, err := os.Stat("../../../NixOS/home/end4/seed/runtime-repair.nix"); !os.IsNotExist(err) {
		t.Fatalf("retired End4 runtime repair module must be absent, err=%v", err)
	}
}

func TestHyprHomeManagerMigrationExplicitlyPrecedesPrepareAndLinkChecks(t *testing.T) {
	migration := readContractFile(t, "../../../NixOS/home/migrations/v1_to_v2/user-paths.nix")
	shells := readContractFile(t, "../../../NixOS/home/shells/default.nix")
	if !strings.Contains(migration, `migrateWahrweltUserPaths = lib.hm.dag.entryBefore [ "checkLinkTargets" ]`) {
		t.Fatalf("Hypr migration must remain before Home Manager link checks\n%s", migration)
	}
	prepareAfterMigration := regexp.MustCompile(`prepareWahrweltHyprDirectory\s*=\s*lib\.hm\.dag\.entryBetween\s*\[\s*"checkLinkTargets"\s*\]\s*\[\s*"migrateWahrweltUserPaths"\s*\]`)
	if !prepareAfterMigration.MatchString(shells) {
		t.Fatalf("Hypr prepare must explicitly depend on migration and precede link checks\n%s", shells)
	}
	liveAfterLinks := regexp.MustCompile(`liveSyncHyprShell\s*=\s*lib\.hm\.dag\.entryAfter\s*\[\s*"seedHyprShellRuntime"\s*"linkGeneration"\s*\]`)
	if !liveAfterLinks.MatchString(shells) {
		t.Fatalf("live Hypr reload must explicitly follow runtime seed and Home Manager link publication\n%s", shells)
	}
}

func TestEnd4V1AppSeedUsesFDQuarantineForExactHistoricalLinks(t *testing.T) {
	migration := readContractFile(t, "../../../NixOS/home/migrations/v1_to_v2/end4-app-seed.nix")
	guard := readContractFile(t, "../../../NixOS/home/migrations/v1_to_v2/link-guard.py")
	for _, want := range []string{
		`${./link-guard.py}`,
		`check "$target" "$relative"`,
		`quarantine "$target" "$relative"`,
		`Wahrwelt v1_to_v2 End4 link recovery retained`,
	} {
		if !strings.Contains(migration, want) {
			t.Fatalf("End4 v1 app migration is missing %q\n%s", want, migration)
		}
	}
	for _, forbidden := range []string{`rm -f`, `readlink -- "$target"`, `stat -c`} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("End4 v1 app migration retained path-based link removal %q\n%s", forbidden, migration)
		}
	}
	for _, want := range []string{
		`".config/kitty": "directory"`,
		`".config/fuzzel": "directory"`,
		`".config/kdeglobals": "regular"`,
		`".local/share/konsole/Profile 1.profile": "regular"`,
		`".config/quickshell/ii/shell.qml"`,
		`".config/hypr/end4/hyprland.conf"`,
		`rename_noreplace(parent_fd, target_name, recovery_fd, "legacy-link")`,
	} {
		if !strings.Contains(guard, want) {
			t.Fatalf("versioned link guard is missing End4 app ownership proof %q\n%s", want, guard)
		}
	}
}

func TestHomeManagerRuntimeActivationMigratesOnlyExactKnownEnd4Forms(t *testing.T) {
	helper := "../../../NixOS/home/shells/runtime-activation.sh"
	legacyDir := "../../../NixOS/home/migrations/v1_to_v2/hypr-runtime"
	canonical := filepath.Join(t.TempDir(), "canonical.lua")
	canonicalContent := "-- canonical Wahrwelt runtime\n"
	if err := os.WriteFile(canonical, []byte(canonicalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	legacyPaths := []string{
		filepath.Join(legacyDir, "end4.lua"),
		filepath.Join(legacyDir, "end4-pc.lua"),
	}
	for _, legacy := range legacyPaths {
		t.Run(filepath.Base(legacy), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "hyprland.lua")
			data, err := os.ReadFile(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, data, 0o600); err != nil {
				t.Fatal(err)
			}
			runRuntimeActivation(t, nil, helper, "migrate-known-runtime", target, canonical, legacyPaths[0], legacyPaths[1])
			if got := readContractFile(t, target); got != canonicalContent {
				t.Fatalf("known End4 runtime was not migrated: %q", got)
			}
			info, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o644 {
				t.Fatalf("migrated runtime mode = %s, want 0644", info.Mode())
			}
		})
	}

	unknown := filepath.Join(t.TempDir(), "hyprland.lua")
	legacy, err := os.ReadFile(legacyPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	unknownContent := string(legacy) + "-- user-owned lookalike\n"
	if err := os.WriteFile(unknown, []byte(unknownContent), 0o600); err != nil {
		t.Fatal(err)
	}
	unknownCmd := exec.Command("bash", helper, "migrate-known-runtime", unknown, canonical, legacyPaths[0], legacyPaths[1])
	unknownOutput, unknownErr := unknownCmd.CombinedOutput()
	if unknownErr == nil || !strings.Contains(string(unknownOutput), "ownership collision") {
		t.Fatalf("unknown regular runtime did not fail closed: err=%v\n%s", unknownErr, unknownOutput)
	}
	if got := readContractFile(t, unknown); got != unknownContent {
		t.Fatalf("unknown lookalike was changed: %q", got)
	}

	for _, kind := range []string{"symlink", "broken-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "hyprland.lua")
			linkTarget := filepath.Join(dir, "user-runtime.lua")
			if kind == "symlink" {
				if err := os.WriteFile(linkTarget, legacy, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(linkTarget, target); err != nil {
				t.Fatal(err)
			}
			linkCmd := exec.Command("bash", helper, "migrate-known-runtime", target, canonical, legacyPaths[0], legacyPaths[1])
			linkOutput, linkErr := linkCmd.CombinedOutput()
			if linkErr == nil || !strings.Contains(string(linkOutput), "ownership collision") {
				t.Fatalf("managed runtime symlink did not fail closed: err=%v\n%s", linkErr, linkOutput)
			}
			if got, err := os.Readlink(target); err != nil || got != linkTarget {
				t.Fatalf("runtime symlink changed: target=%q err=%v", got, err)
			}
		})
	}

	directory := filepath.Join(t.TempDir(), "hyprland.lua")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", helper, "migrate-known-runtime", directory, canonical, legacyPaths[0], legacyPaths[1])
	if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "Refusing non-regular Wahrwelt Hyprland runtime collision") {
		t.Fatalf("directory collision must fail closed: err=%v output=%s", err, output)
	}
}

func TestHomeManagerRuntimeMigrationUsesCandidateExchange(t *testing.T) {
	helper := readContractFile(t, "../../../NixOS/home/shells/runtime-activation.sh")
	for _, want := range []string{
		`exec python3 -I -S - "$@"`,
		"fcntl.F_SETLEASE",
		"fcntl.F_WRLCK",
		"lease_break_requested = True",
		"signal.pthread_sigmask",
		"os.O_TMPFILE",
		"AT_EMPTY_PATH",
		"RENAME_EXCHANGE",
		"renameat2",
		"materialize_anonymous",
		"recognized legacy runtime has external hardlinks",
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("runtime migration helper missing inode-safe primitive %q\n%s", want, helper)
		}
	}
	if strings.Count(helper, "st_nlink != 1") < 3 {
		t.Fatalf("runtime migration helper must recheck link count immediately before mutation\n%s", helper)
	}
	for _, forbidden := range []string{"os.ftruncate", `mv -fT -- "$tmp" "$target"`} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("runtime migration helper retained unsafe in-place mutation %q\n%s", forbidden, helper)
		}
	}
}

func TestHomeManagerRuntimeMigrationIgnoresPythonPathSitecustomize(t *testing.T) {
	helper := "../../../NixOS/home/shells/runtime-activation.sh"
	legacyDir := "../../../NixOS/home/migrations/v1_to_v2/hypr-runtime"
	legacyPaths := []string{
		filepath.Join(legacyDir, "end4.lua"),
		filepath.Join(legacyDir, "end4-pc.lua"),
	}
	legacy, err := os.ReadFile(legacyPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	canonical := filepath.Join(dir, "canonical.lua")
	target := filepath.Join(dir, "hyprland.lua")
	pythonShim := filepath.Join(dir, "python-shim")
	if err := os.Mkdir(pythonShim, 0o755); err != nil {
		t.Fatal(err)
	}
	forbiddenSitecustomize := `import os

def forbidden_ftruncate(_fd, _length):
    raise RuntimeError("sitecustomize must be ignored")

os.ftruncate = forbidden_ftruncate
`
	if err := os.WriteFile(filepath.Join(pythonShim, "sitecustomize.py"), []byte(forbiddenSitecustomize), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("-- canonical Wahrwelt runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	runRuntimeActivation(
		t,
		[]string{"PYTHONPATH=" + pythonShim, "PYTHONDONTWRITEBYTECODE=1"},
		helper,
		"migrate-known-runtime",
		target,
		canonical,
		legacyPaths[0],
		legacyPaths[1],
	)
	if got := readContractFile(t, target); got != "-- canonical Wahrwelt runtime\n" {
		t.Fatalf("isolated runtime migration did not write canonical content: %q", got)
	}
}

func TestHomeManagerRuntimeActivationPreservesFinalMigrationRaceWinners(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			legacyPaths, legacy := runtimeLegacyFixtures(t)
			dir := t.TempDir()
			canonical := filepath.Join(dir, "canonical.lua")
			target := filepath.Join(dir, "hyprland.lua")
			linkTarget := filepath.Join(dir, "user-runtime.lua")
			if err := os.WriteFile(canonical, []byte("-- canonical Wahrwelt runtime\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, legacy, 0o600); err != nil {
				t.Fatal(err)
			}

			process := startActivationBarrier(
				t,
				"MIGRATION",
				"migrate-known-runtime",
				target,
				canonical,
				legacyPaths[0],
				legacyPaths[1],
			)
			savedLegacy := filepath.Join(dir, "saved-legacy.lua")
			if err := os.Rename(target, savedLegacy); err != nil {
				t.Fatal(err)
			}
			if kind == "regular" {
				if err := os.WriteFile(target, []byte("-- unknown regular race winner\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(linkTarget, []byte("-- linked user runtime\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(linkTarget, target); err != nil {
					t.Fatal(err)
				}
			}
			output := process.releaseExpectFailure(t)
			if !strings.Contains(output, "ownership collision") {
				t.Fatalf("migration race did not fail as an ownership collision\n%s", output)
			}

			if kind == "regular" {
				if got := readContractFile(t, target); got != "-- unknown regular race winner\n" {
					t.Fatalf("unknown regular migration race winner changed: %q", got)
				}
				info, err := os.Lstat(target)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("unknown regular migration race winner mode changed: %s", info.Mode())
				}
				return
			}

			if got, err := os.Readlink(target); err != nil || got != linkTarget {
				t.Fatalf("unknown symlink migration race winner changed: target=%q err=%v", got, err)
			}
			if got := readContractFile(t, target); got != "-- linked user runtime\n" {
				t.Fatalf("unknown symlink migration race target content changed: %q", got)
			}
			if got := readContractFile(t, savedLegacy); got != string(legacy) {
				t.Fatalf("displaced legacy runtime changed: %q", got)
			}
		})
	}
}

func TestHomeManagerRuntimeActivationPreservesSeparateProcessOpenWriter(t *testing.T) {
	helper := "../../../NixOS/home/shells/runtime-activation.sh"
	legacyDir := "../../../NixOS/home/migrations/v1_to_v2/hypr-runtime"
	legacyPaths := []string{
		filepath.Join(legacyDir, "end4.lua"),
		filepath.Join(legacyDir, "end4-pc.lua"),
	}
	legacy, err := os.ReadFile(legacyPaths[0])
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	canonical := filepath.Join(dir, "canonical.lua")
	target := filepath.Join(dir, "hyprland.lua")
	if err := os.WriteFile(canonical, []byte("-- canonical Wahrwelt runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	// This test process owns a writable fd while the activation helper runs in
	// a separate child process. The kernel must refuse the helper's write lease.
	externalWriter, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", helper, "migrate-known-runtime", target, canonical, legacyPaths[0], legacyPaths[1])
	output, activationErr := cmd.CombinedOutput()
	if activationErr == nil || !strings.Contains(string(output), "ownership collision") {
		_ = externalWriter.Close()
		t.Fatalf("busy recognized legacy runtime did not fail closed: err=%v\n%s", activationErr, output)
	}
	if got := readContractFile(t, target); got != string(legacy) {
		_ = externalWriter.Close()
		t.Fatalf("runtime changed while an external writable fd was open: %q", got)
	}

	unknownContent := []byte("-- unknown separate-fd writer content\n")
	if err := externalWriter.Truncate(0); err != nil {
		_ = externalWriter.Close()
		t.Fatal(err)
	}
	if _, err := externalWriter.WriteAt(unknownContent, 0); err != nil {
		_ = externalWriter.Close()
		t.Fatal(err)
	}
	if err := externalWriter.Sync(); err != nil {
		_ = externalWriter.Close()
		t.Fatal(err)
	}
	if err := externalWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readContractFile(t, target); got != string(unknownContent) {
		t.Fatalf("unknown separate-fd writer content was overwritten: %q", got)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("separate-fd writer target mode changed: %s", info.Mode())
	}
}

func TestHomeManagerDefaultSeedUsesExclusivePublishAndPreservesRaceWinner(t *testing.T) {
	helper := "../../../NixOS/home/shells/runtime-activation.sh"
	dir := t.TempDir()
	source := filepath.Join(dir, "default-source.lua")
	sourceContent := "-- canonical default\n"
	if err := os.WriteFile(source, []byte(sourceContent), 0o600); err != nil {
		t.Fatal(err)
	}

	created := filepath.Join(dir, "created.lua")
	runRuntimeActivation(t, nil, helper, "seed-exclusive", source, created, "Wahrwelt user config")
	if got := readContractFile(t, created); got != sourceContent {
		t.Fatalf("exclusive seed content = %q", got)
	}
	if info, err := os.Lstat(created); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("exclusive seed mode: info=%v err=%v", info, err)
	}
	createdBefore, err := os.Stat(created)
	if err != nil {
		t.Fatal(err)
	}
	contentBefore, err := os.ReadFile(created)
	if err != nil {
		t.Fatal(err)
	}
	hashBefore := sha256.Sum256(contentBefore)

	runRuntimeActivation(t, nil, helper, "seed-exclusive", source, created, "Wahrwelt user config")
	createdAfter, err := os.Stat(created)
	if err != nil {
		t.Fatal(err)
	}
	contentAfter, err := os.ReadFile(created)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(createdBefore, createdAfter) {
		t.Fatal("repeated activation replaced the existing default.lua inode")
	}
	if hashAfter := sha256.Sum256(contentAfter); hashAfter != hashBefore {
		t.Fatalf("repeated activation changed the default.lua hash: before=%x after=%x", hashBefore, hashAfter)
	}
	if string(contentAfter) != string(contentBefore) {
		t.Fatalf("repeated activation changed default.lua content: before=%q after=%q", contentBefore, contentAfter)
	}

	for _, kind := range []string{"regular", "symlink", "broken-symlink"} {
		t.Run(kind, func(t *testing.T) {
			raceDir := t.TempDir()
			target := filepath.Join(raceDir, "default.lua")
			linkTarget := filepath.Join(raceDir, "user.lua")
			if kind == "symlink" {
				if err := os.WriteFile(linkTarget, []byte("-- linked user config\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runSeedExclusiveAtBarrier(t, helper, source, target, true, func() {
				switch kind {
				case "regular":
					if err := os.WriteFile(target, []byte("-- raced user config\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "symlink", "broken-symlink":
					if err := os.Symlink(linkTarget, target); err != nil {
						t.Fatal(err)
					}
				}
			})

			if kind == "regular" {
				if got := readContractFile(t, target); got != "-- raced user config\n" {
					t.Fatalf("race-winning regular file changed: %q", got)
				}
				return
			}
			if got, err := os.Readlink(target); err != nil || got != linkTarget {
				t.Fatalf("race-winning symlink changed: target=%q err=%v", got, err)
			}
		})
	}
}

func runRuntimeActivation(t *testing.T, env []string, helper string, args ...string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{helper}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime activation helper failed: %v\n%s", err, output)
	}
}
