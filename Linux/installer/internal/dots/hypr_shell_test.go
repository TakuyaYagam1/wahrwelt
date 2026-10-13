package dots

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/shellruntime"
)

func TestWriteShellLauncherConfigReplacesExistingSymlink(t *testing.T) {
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	path := shellruntime.RuntimeFile(home, "shell-profile.lua")
	target := filepath.Join(home, "store-shell-profile.lua")
	writeTestFile(t, target, "old target\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if err := writeShellLauncherConfig(path, hyprDir); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("shell launcher should be rewritten as a regular file: %s", path)
	}
	assertFileContains(t, path, filepath.Join(hyprDir, "scripts", "start-shell.sh"))
	assertFileContains(t, target, "old target")
}

func TestWriteHyprRuntimeShellStateRendersCanonicalProfiles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")

	var canonicalEntrypoint string
	for _, profile := range shellruntime.ProfileSpecs {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			home := t.TempDir()
			hyprDir := filepath.Join(home, ".config", "hypr")
			writeRuntimeProfileAssets(t, home, hyprDir, profile)
			statePath := shellruntime.ActiveShellStatePath(home)
			writeTestFile(t, statePath, profile.ID+"\n")

			if err := writeHyprRuntimeShellState(home, hyprDir); err != nil {
				t.Fatal(err)
			}

			entrypoint := readTestFile(t, shellruntime.RuntimeFile(home, "hyprland.lua"))
			if canonicalEntrypoint == "" {
				canonicalEntrypoint = entrypoint
			} else if entrypoint != canonicalEntrypoint {
				t.Fatalf("runtime hyprland.lua differs from other profiles\n%s", entrypoint)
			}
			if strings.Count(entrypoint, `dofile(hypr_root .. "/user/hyprland.lua")`) != 1 {
				t.Fatalf("canonical entrypoint must load user adapter exactly once\n%s", entrypoint)
			}
			for _, forbidden := range []string{"shell-profile.lua", "end4/hyprland.lua", "end4_root"} {
				if strings.Contains(entrypoint, forbidden) {
					t.Fatalf("canonical entrypoint contains forbidden direct load %q\n%s", forbidden, entrypoint)
				}
			}

			launcherModule := strings.TrimSuffix(filepath.ToSlash(profile.Launcher), ".lua")
			launcherModule = strings.ReplaceAll(launcherModule, "/", ".")
			assertFileContains(t, shellruntime.RuntimeFile(home, "shell-launcher.lua"), `require("`+launcherModule+`")`)

			keybinds := readTestFile(t, shellruntime.RuntimeFile(home, "shell-keybinds.lua"))
			if first, _, _ := strings.Cut(keybinds, "\n"); first != shellruntime.AdapterMarker(profile.ID) {
				t.Fatalf("unexpected adapter marker: got %q want %q", first, shellruntime.AdapterMarker(profile.ID))
			}
			if shellruntime.IsEnd4Profile(profile.ID) {
				quickshellPath := filepath.Join(home, ".config", "quickshell", profile.QuickshellConfig)
				want := `require("end4-adapter").load({ profile = "` + profile.ID + `", quickshell_config = "` + quickshellPath + `" })`
				if keybinds != shellruntime.AdapterMarker(profile.ID)+"\n"+want+"\n" {
					t.Fatalf("unexpected exact End4 adapter args\n%s", keybinds)
				}
				assertFileContains(t, shellruntime.RuntimeFile(home, "hyprlock.conf"), filepath.Join(hyprDir, "end4", "hyprlock.conf"))
				if _, err := os.Lstat(shellruntime.End4VariantStatePath(home)); !os.IsNotExist(err) {
					t.Fatalf("runtime rendering unexpectedly selected an End4 variant: %v", err)
				}
			} else {
				assertFileContains(t, shellruntime.RuntimeFile(home, "shell-keybinds.lua"), `require("`+profile.Adapter+`")`)
				assertFileContains(t, shellruntime.RuntimeFile(home, "hyprlock.conf"), "profile: shell-managed")
			}
			if got := strings.TrimSpace(readTestFile(t, statePath)); got != profile.ID {
				t.Fatalf("active shell = %q, want %q", got, profile.ID)
			}
		})
	}
}

func TestEnd4VariantRuntimeDiffIsAdapterOnly(t *testing.T) {
	t.Parallel()

	official, ok := shellruntime.ProfileByID(shellruntime.End4)
	if !ok {
		t.Fatal("End4 Official profile is missing")
	}
	pc, ok := shellruntime.ProfileByID(shellruntime.End4PC)
	if !ok {
		t.Fatal("End4 pC profile is missing")
	}
	home := "/test/home"
	hyprDir := filepath.Join(home, ".config", "hypr")
	officialLock := runtimeLockStackPublications(home, hyprDir, official.ID)
	pcLock := runtimeLockStackPublications(home, hyprDir, pc.ID)
	officialPayloads := []string{
		shellLauncherBindingsConfig(official),
		shellKeybindsConfig(hyprDir, official),
		officialLock[0].content,
		officialLock[1].content,
	}
	pcPayloads := []string{
		shellLauncherBindingsConfig(pc),
		shellKeybindsConfig(hyprDir, pc),
		pcLock[0].content,
		pcLock[1].content,
	}

	differences := 0
	for index := range officialPayloads {
		if officialPayloads[index] != pcPayloads[index] {
			differences++
		}
	}
	if differences != 1 {
		t.Fatalf("End4 variant runtime differences = %d, want adapter-only difference\nOfficial: %#v\npC: %#v", differences, officialPayloads, pcPayloads)
	}
	if officialPayloads[1] == pcPayloads[1] {
		t.Fatal("End4 variants unexpectedly share the exact adapter payload")
	}
}

func TestHistoricalEnd4SharedRuntimePayloadsRemainMigratable(t *testing.T) {
	t.Parallel()

	home := "/test/home"
	hyprDir := filepath.Join(home, ".config", "hypr")
	for _, testCase := range []struct {
		name    string
		path    string
		content string
	}{
		{
			name:    "launcher",
			path:    shellruntime.RuntimeFile(home, "shell-launcher.lua"),
			content: "-- Active shell launcher profile: end4-pc\nrequire(\"end4.launcher\")\n",
		},
		{
			name:    "lock",
			path:    shellruntime.RuntimeFile(home, "hyprlock.conf"),
			content: runtimeSourceConfig(filepath.Join(hyprDir, "end4", "hyprlock.conf"), "Active Hyprlock profile: end4-pc"),
		},
		{
			name:    "idle",
			path:    shellruntime.RuntimeFile(home, "hypridle.conf"),
			content: runtimeSourceConfig(filepath.Join(hyprDir, "end4", "hypridle.conf"), "Active Hypridle profile: end4-pc"),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			publication := runtimePublication{path: testCase.path, content: "canonical replacement\n", mode: 0o644}
			state := runtimePathState{snapshot: runtimePathSnapshot{
				path:    testCase.path,
				kind:    runtimeSnapshotRegular,
				content: []byte(testCase.content),
				mode:    0o644,
			}}
			if err := validateDirectEnd4RuntimePublicationState(home, hyprDir, publication, state); err != nil {
				t.Fatalf("historical End4 pC runtime payload is not migratable: %v", err)
			}
		})
	}
}

func TestShellProfileSyncMirrorsGoRuntimeRendering(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	t.Setenv("XDG_STATE_HOME", "")
	script, err := filepath.Abs("../../../dots/hypr/scripts/shell-profile-sync.sh")
	if err != nil {
		t.Fatal(err)
	}
	runtimeScript, err := filepath.Abs("../../../dots/hypr/scripts/shell-runtime.sh")
	if err != nil {
		t.Fatal(err)
	}
	fsHelper := filepath.Join(t.TempDir(), "wahrwelt-fs-helper")
	buildHelper := exec.Command("go", "build", "-o", fsHelper, "../../cmd/wahrwelt-fs-helper")
	if output, err := buildHelper.CombinedOutput(); err != nil {
		t.Fatalf("build filesystem helper: %v\n%s", err, output)
	}

	for _, profile := range shellruntime.ProfileSpecs {
		profile := profile
		t.Run(profile.ID, func(t *testing.T) {
			home := t.TempDir()
			runtimeDir := filepath.Join(home, "run")
			if err := os.Mkdir(runtimeDir, 0o700); err != nil {
				t.Fatal(err)
			}
			hyprDir := filepath.Join(home, ".config", "hypr")
			writeRuntimeProfileAssets(t, home, hyprDir, profile)
			writeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n")
			if err := writeHyprRuntimeShellState(home, hyprDir); err != nil {
				t.Fatal(err)
			}

			paths := make([]string, 0, len(shellruntime.RuntimeFiles)+1)
			for _, name := range shellruntime.RuntimeFiles {
				paths = append(paths, shellruntime.RuntimeFile(home, name))
			}
			paths = append(paths, filepath.Join(hyprDir, "hyprland.lua"))
			want := make(map[string]string, len(paths))
			for _, path := range paths {
				want[path] = readTestFile(t, path)
			}
			for _, path := range paths {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command(bash, "-c", `
set -eu
. "$1"
config_home="$wahrwelt_config_home"
hypr_runtime_dir="$wahrwelt_hypr_runtime_dir"
profile="$2"
log() { :; }
hypr_dir() { wahrwelt_hypr_dir_path; }
. "$3"
mapfile -t runtime_bundle_path_list < <(runtime_bundle_paths)
runtime_bundle_snapshot_dir="$(
  wahrwelt_fs_begin runtime "$wahrwelt_runtime_session_public_dir" "${runtime_bundle_path_list[@]}"
)"
sync_runtime_shell_files
wahrwelt_fs_commit "$runtime_bundle_snapshot_dir"
`, "bash", runtimeScript, profile.ID, script)
			cmd.Env = append(os.Environ(),
				"HOME="+home,
				"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
				"XDG_RUNTIME_DIR="+runtimeDir,
				"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
				"WAHRWELT_FS_HELPER="+fsHelper,
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("shell runtime sync failed: %v\n%s", err, output)
			}

			for _, path := range paths {
				if got := readTestFile(t, path); got != want[path] {
					t.Fatalf("shell rendering drifted from Go for %s\nGo:\n%s\nShell:\n%s", path, want[path], got)
				}
			}
		})
	}
}

func TestWriteHyprRuntimeShellStatePreservesRememberedEnd4VariantForNonEnd4(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Noctalia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeTestFile(t, shellruntime.ActiveShellStatePath(home), shellruntime.Noctalia+"\n")
	writeTestFile(t, shellruntime.End4VariantStatePath(home), shellruntime.End4PC+"\n")

	if err := writeHyprRuntimeShellState(home, hyprDir); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, shellruntime.End4VariantStatePath(home)); got != shellruntime.End4PC+"\n" {
		t.Fatalf("remembered End4 variant changed across non-End4 transition: %q", got)
	}
}

func TestWriteHyprRuntimeShellStateDoesNotPublishShellSelectionState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.DefaultProfile)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)

	if err := writeHyprRuntimeShellState(home, hyprDir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{shellruntime.ActiveShellStatePath(home), shellruntime.End4VariantStatePath(home)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("fresh runtime rendering published shell selection state at %s: %v", path, err)
		}
	}

	rememberedActive := shellruntime.End4PC + "\n"
	rememberedVariant := shellruntime.End4 + "\n"
	writeModeTestFile(t, shellruntime.ActiveShellStatePath(home), rememberedActive, 0o600)
	writeModeTestFile(t, shellruntime.End4VariantStatePath(home), rememberedVariant, 0o640)
	end4Profile, _ := shellruntime.ProfileByID(shellruntime.End4PC)
	writeRuntimeProfileAssets(t, home, hyprDir, end4Profile)
	injected := errors.New("injected runtime rendering failure")
	err := writeHyprRuntimeShellStateWithHook(home, hyprDir, func(_, _ string) error { return injected })
	if !errors.Is(err, injected) {
		t.Fatalf("runtime error = %v, want injected rendering failure", err)
	}
	assertTestFileState(t, shellruntime.ActiveShellStatePath(home), rememberedActive, 0o600)
	assertTestFileState(t, shellruntime.End4VariantStatePath(home), rememberedVariant, 0o640)
}

func TestWriteHyprRuntimeShellStateValidatesBeforeMutation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	if err := os.Remove(filepath.Join(hyprDir, filepath.FromSlash(profile.Keybinds))); err != nil {
		t.Fatal(err)
	}
	statePath := shellruntime.ActiveShellStatePath(home)
	runtimePath := shellruntime.RuntimeFile(home, "hyprland.lua")
	writeTestFile(t, statePath, shellruntime.Caelestia+"\n")
	writeTestFile(t, runtimePath, "existing runtime\n")

	err := writeHyprRuntimeShellState(home, hyprDir)
	if err == nil || !strings.Contains(err.Error(), "shell keybind profile missing") {
		t.Fatalf("expected preflight asset error, got %v", err)
	}
	if got := readTestFile(t, runtimePath); got != "existing runtime\n" {
		t.Fatalf("runtime changed before validation completed: %q", got)
	}
	if got := readTestFile(t, statePath); got != shellruntime.Caelestia+"\n" {
		t.Fatalf("active state changed after failed generation: %q", got)
	}
}

func TestWriteHyprRuntimeShellStateRollsBackEveryMutationFault(t *testing.T) {
	injected := errors.New("injected runtime publication fault")
	profile, _ := shellruntime.ProfileByID(shellruntime.End4)
	operationCount := len(buildRuntimeTransactionPlan("/test/home", "/test/home/.config/hypr", profile).paths())

	for faultIndex := range operationCount {
		t.Run(fmt.Sprintf("operation-%02d", faultIndex+1), func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", "")
			home := t.TempDir()
			hyprDir := filepath.Join(home, ".config", "hypr")
			writeRuntimeProfileAssets(t, home, hyprDir, profile)
			writeTestFile(t, shellruntime.ActiveShellStatePath(home), shellruntime.End4+"\n")

			plan := buildRuntimeTransactionPlan(home, hyprDir, profile)
			initializeRuntimeTransactionBaseline(t, plan, home)
			writeModeTestFile(t, shellruntime.ActiveShellStatePath(home), shellruntime.End4+"\n", 0o600)
			writeModeTestFile(t, shellruntime.End4VariantStatePath(home), shellruntime.End4PC+"\n", 0o640)
			before := snapshotRuntimePlan(t, plan)

			operation := 0
			err := writeHyprRuntimeShellStateWithHook(home, hyprDir, func(_, _ string) error {
				if operation == faultIndex {
					return injected
				}
				operation++
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatalf("runtime error = %v, want injected failure at operation %d", err, faultIndex+1)
			}
			assertRuntimePlanSnapshot(t, plan, before)
		})
	}
}

func TestWriteHyprRuntimeShellStateLeavesSelectionStateUntouchedOnRuntimeFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.End4PC)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeModeTestFile(t, shellruntime.RuntimeFile(home, "hyprland.lua"), shellruntime.CanonicalEntrypoint(), 0o600)
	writeModeTestFile(t, shellruntime.RuntimeFile(home, "shell-keybinds.lua"), shellruntime.AdapterMarker(shellruntime.End4PC)+"\n", 0o600)
	writeModeTestFile(t, shellruntime.ActiveShellStatePath(home), "invalid-active\n", 0o600)
	writeModeTestFile(t, shellruntime.End4VariantStatePath(home), shellruntime.End4+"\n", 0o640)

	injected := errors.New("injected runtime write fault")
	err := writeHyprRuntimeShellStateWithHook(home, hyprDir, func(operation, path string) error {
		if operation == runtimeMutationWrite && path == shellruntime.RuntimeFile(home, "shell-keybinds.lua") {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) {
		t.Fatalf("runtime error = %v, want runtime write failure", err)
	}
	assertTestFileState(t, shellruntime.ActiveShellStatePath(home), "invalid-active\n", 0o600)
	assertTestFileState(t, shellruntime.End4VariantStatePath(home), shellruntime.End4+"\n", 0o640)
}

func TestWriteHyprRuntimeShellStateLeavesStateSymlinksUntouched(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.End4)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	activeTarget := filepath.Join(home, "user-state", "active-shell")
	variantTarget := filepath.Join(home, "user-state", "end4-variant")
	writeModeTestFile(t, activeTarget, shellruntime.End4+"\n", 0o600)
	writeModeTestFile(t, variantTarget, shellruntime.End4PC+"\n", 0o640)
	for path, target := range map[string]string{
		shellruntime.ActiveShellStatePath(home): activeTarget,
		shellruntime.End4VariantStatePath(home): variantTarget,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}

	err := writeHyprRuntimeShellState(home, hyprDir)
	if err != nil {
		t.Fatalf("runtime error = %v, want state symlinks left outside runtime transaction", err)
	}
	for path, wantTarget := range map[string]string{
		shellruntime.ActiveShellStatePath(home): activeTarget,
		shellruntime.End4VariantStatePath(home): variantTarget,
	} {
		gotTarget, err := os.Readlink(path)
		if err != nil || gotTarget != wantTarget {
			t.Fatalf("state symlink was not restored for %s: got %q want %q err=%v", path, gotTarget, wantTarget, err)
		}
	}
}

func TestWriteHyprRuntimeShellStateLeavesAbsentVariantAbsent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.End4PC)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeModeTestFile(t, shellruntime.RuntimeFile(home, "hyprland.lua"), shellruntime.CanonicalEntrypoint(), 0o600)
	writeModeTestFile(t, shellruntime.RuntimeFile(home, "shell-keybinds.lua"), shellruntime.AdapterMarker(shellruntime.End4PC)+"\n", 0o600)
	writeModeTestFile(t, shellruntime.ActiveShellStatePath(home), "invalid-active\n", 0o600)

	err := writeHyprRuntimeShellState(home, hyprDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(shellruntime.End4VariantStatePath(home)); !os.IsNotExist(err) {
		t.Fatalf("absent remembered variant was published by runtime rendering: %v", err)
	}
	assertTestFileState(t, shellruntime.ActiveShellStatePath(home), "invalid-active\n", 0o600)
}

func TestWriteHyprRuntimeShellStatePreflightsAllPathCollisions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeModeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n", 0o600)
	legacyPath := filepath.Join(hyprDir, "hyprland.conf")
	writeModeTestFile(t, legacyPath, "legacy bytes\n", 0o640)
	collision := shellruntime.RuntimeFile(home, "shell-keybinds.lua")
	if err := os.MkdirAll(collision, 0o755); err != nil {
		t.Fatal(err)
	}

	err := writeHyprRuntimeShellState(home, hyprDir)
	if err == nil || !strings.Contains(err.Error(), "non-regular shell runtime collision") {
		t.Fatalf("expected transaction path collision, got %v", err)
	}
	assertTestFileState(t, legacyPath, "legacy bytes\n", 0o640)
	assertTestFileState(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n", 0o600)
	info, statErr := os.Lstat(collision)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("runtime collision changed: info=%v err=%v", info, statErr)
	}
}

func TestRemoveLegacyRuntimeFileRejectsUnknownRegularAndSymlink(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "hyprland.conf")
			var wantTarget string
			switch kind {
			case "regular":
				writeModeTestFile(t, path, "user-owned legacy runtime\n", 0o600)
			case "symlink":
				wantTarget = filepath.Join(root, "user-owned-target")
				writeModeTestFile(t, wantTarget, "user-owned symlink target\n", 0o600)
				if err := os.Symlink(wantTarget, path); err != nil {
					t.Fatal(err)
				}
			}

			err := removeLegacyRuntimeFile(path)
			if err == nil || !strings.Contains(err.Error(), "unowned legacy Hyprland runtime collision") {
				t.Fatalf("expected ownership collision, got %v", err)
			}
			if kind == "regular" {
				assertTestFileState(t, path, "user-owned legacy runtime\n", 0o600)
				return
			}
			if got, readErr := os.Readlink(path); readErr != nil || got != wantTarget {
				t.Fatalf("unknown symlink changed: target=%q err=%v", got, readErr)
			}
		})
	}
}

func TestWriteHyprRuntimeShellStateRejectsUnknownTopLevelEntrypoint(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", "")
			home := t.TempDir()
			hyprDir := filepath.Join(home, ".config", "hypr")
			profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
			writeRuntimeProfileAssets(t, home, hyprDir, profile)
			writeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n")
			path := filepath.Join(hyprDir, "hyprland.lua")
			var wantTarget string
			switch kind {
			case "regular":
				writeModeTestFile(t, path, "user-owned entrypoint\n", 0o600)
			case "symlink":
				wantTarget = filepath.Join(home, "user-owned-entrypoint")
				writeModeTestFile(t, wantTarget, "user-owned symlink target\n", 0o600)
				if err := os.Symlink(wantTarget, path); err != nil {
					t.Fatal(err)
				}
			}

			err := writeHyprRuntimeShellState(home, hyprDir)
			if err == nil || !strings.Contains(err.Error(), "unowned top-level Hyprland runtime collision") {
				t.Fatalf("expected top-level ownership collision, got %v", err)
			}
			if kind == "regular" {
				assertTestFileState(t, path, "user-owned entrypoint\n", 0o600)
				return
			}
			if got, readErr := os.Readlink(path); readErr != nil || got != wantTarget {
				t.Fatalf("unknown top-level symlink changed: target=%q err=%v", got, readErr)
			}
		})
	}
}

func TestWriteHyprRuntimeShellStatePreservesActiveHomeManagerTopLevelEntrypoint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n")
	topLevel, target, want := prepareActiveHomeManagerTopLevelEntrypoint(t, home, hyprDir)
	before, err := os.Lstat(topLevel)
	if err != nil {
		t.Fatal(err)
	}

	if err := writeHyprRuntimeShellState(home, hyprDir); err != nil {
		t.Fatalf("active Home Manager top-level entrypoint was rejected: %v", err)
	}
	if got, err := os.Readlink(topLevel); err != nil || got != target {
		t.Fatalf("active Home Manager top-level entrypoint changed: target=%q err=%v", got, err)
	}
	after, err := os.Lstat(topLevel)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("active Home Manager top-level entrypoint identity changed: before=%v after=%v err=%v", before, after, err)
	}
	if got := readTestFile(t, target); got != want {
		t.Fatalf("active Home Manager store entrypoint changed: %q", got)
	}
}

func TestWriteHyprRuntimeShellStatePreservesFailedHomeManagerGenerationRuntimeLinks(t *testing.T) {
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skipf("nix is unavailable: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n")

	plan := buildRuntimeTransactionPlan(home, hyprDir, profile)
	filesTree := filepath.Join(t.TempDir(), "home-manager-files")
	managedRoot := filepath.Join(filesTree, ".config", "hypr")
	wantTargets := make(map[string]string, len(shellruntime.RuntimeFiles))
	for _, publication := range plan.publications {
		if filepath.Dir(publication.path) != hyprDir {
			continue
		}
		writeModeTestFile(
			t,
			filepath.Join(managedRoot, filepath.Base(publication.path)),
			publication.content,
			0o444,
		)
	}
	storeRoot := addHomeManagerFilesNixStorePath(t, filesTree)
	for _, name := range shellruntime.RuntimeFiles {
		path := filepath.Join(hyprDir, name)
		target := filepath.Join(storeRoot, ".config", "hypr", name)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		wantTargets[path] = target
	}

	if err := writeHyprRuntimeShellState(home, hyprDir); err != nil {
		t.Fatalf("failed Home Manager generation runtime links were rejected: %v", err)
	}
	for path, target := range wantTargets {
		if got, err := os.Readlink(path); err != nil || got != target {
			t.Fatalf("failed-generation runtime link changed at %s: target=%q err=%v", path, got, err)
		}
	}
}

func TestManagedUserNamespaceRuntimeTargetsAcceptsFailedHomeManagerGenerationEntrypoint(t *testing.T) {
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skipf("nix is unavailable: %v", err)
	}
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	stable := stableRuntimeSourceConfig(
		shellruntime.RuntimeFile(home, "hyprland.lua"),
		"Wahrwelt stable Hyprland entrypoint.",
	)
	filesTree := filepath.Join(t.TempDir(), "home-manager-files")
	managedTarget := filepath.Join(filesTree, ".config", "hypr", "hyprland.lua")
	writeModeTestFile(t, managedTarget, stable, 0o444)
	filesStore := addHomeManagerFilesNixStorePath(t, filesTree)
	failedGenerationTarget := filepath.Join(filesStore, ".config", "hypr", "hyprland.lua")
	if err := os.MkdirAll(hyprDir, 0o755); err != nil {
		t.Fatal(err)
	}
	topLevel := filepath.Join(hyprDir, "hyprland.lua")
	if err := os.Symlink(failedGenerationTarget, topLevel); err != nil {
		t.Fatal(err)
	}

	if _, err := managedUserNamespaceRuntimeTargets(home, hyprDir); err != nil {
		t.Fatalf("failed Home Manager generation entrypoint was rejected: %v", err)
	}
	if got, err := os.Readlink(topLevel); err != nil || got != failedGenerationTarget {
		t.Fatalf("failed-generation entrypoint changed: target=%q err=%v", got, err)
	}
}

func TestManagedUserNamespaceRuntimeTargetsRejectsFailedGenerationEntrypointWithWrongPayload(t *testing.T) {
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skipf("nix is unavailable: %v", err)
	}
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	filesTree := filepath.Join(t.TempDir(), "home-manager-files")
	managedTarget := filepath.Join(filesTree, ".config", "hypr", "hyprland.lua")
	const unknown = "-- private runtime in a store-shaped tree\n"
	writeModeTestFile(t, managedTarget, unknown, 0o444)
	filesStore := addHomeManagerFilesNixStorePath(t, filesTree)
	failedGenerationTarget := filepath.Join(filesStore, ".config", "hypr", "hyprland.lua")
	if err := os.MkdirAll(hyprDir, 0o755); err != nil {
		t.Fatal(err)
	}
	topLevel := filepath.Join(hyprDir, "hyprland.lua")
	if err := os.Symlink(failedGenerationTarget, topLevel); err != nil {
		t.Fatal(err)
	}

	_, err := managedUserNamespaceRuntimeTargets(home, hyprDir)
	if err == nil || !strings.Contains(err.Error(), "unowned top-level Hypr runtime collision") {
		t.Fatalf("unknown failed-generation payload was accepted: %v", err)
	}
	if got, err := os.Readlink(topLevel); err != nil || got != failedGenerationTarget {
		t.Fatalf("unknown failed-generation entrypoint changed: target=%q err=%v", got, err)
	}
}

func TestManagedUserNamespaceRuntimeTargetsRejectsStoreLinkToMutablePayload(t *testing.T) {
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skipf("nix is unavailable: %v", err)
	}
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	stable := stableRuntimeSourceConfig(
		shellruntime.RuntimeFile(home, "hyprland.lua"),
		"Wahrwelt stable Hyprland entrypoint.",
	)
	mutablePayload := filepath.Join(t.TempDir(), "mutable-hyprland.lua")
	writeModeTestFile(t, mutablePayload, stable, 0o644)
	filesTree := filepath.Join(t.TempDir(), "home-manager-files")
	managedTarget := filepath.Join(filesTree, ".config", "hypr", "hyprland.lua")
	if err := os.MkdirAll(filepath.Dir(managedTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mutablePayload, managedTarget); err != nil {
		t.Fatal(err)
	}
	filesStore := addHomeManagerFilesNixStorePath(t, filesTree)
	storeTarget := filepath.Join(filesStore, ".config", "hypr", "hyprland.lua")
	if err := os.MkdirAll(hyprDir, 0o755); err != nil {
		t.Fatal(err)
	}
	topLevel := filepath.Join(hyprDir, "hyprland.lua")
	if err := os.Symlink(storeTarget, topLevel); err != nil {
		t.Fatal(err)
	}

	_, err := managedUserNamespaceRuntimeTargets(home, hyprDir)
	if err == nil || !strings.Contains(err.Error(), "unowned top-level Hypr runtime collision") {
		t.Fatalf("store link to mutable payload was accepted: %v", err)
	}
	if got, err := os.Readlink(topLevel); err != nil || got != storeTarget {
		t.Fatalf("mutable store-shaped entrypoint changed: target=%q err=%v", got, err)
	}
}

func TestManagedUserNamespaceRuntimeTargetsRejectsMutableCurrentHomeAlias(t *testing.T) {
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	stable := stableRuntimeSourceConfig(
		shellruntime.RuntimeFile(home, "hyprland.lua"),
		"Wahrwelt stable Hyprland entrypoint.",
	)
	generation := filepath.Join(home, "mutable-home-manager-generation")
	managedTarget := filepath.Join(generation, "home-files", ".config", "hypr", "hyprland.lua")
	writeModeTestFile(t, managedTarget, stable, 0o444)
	currentHome := filepath.Join(home, ".local", "state", "home-manager", "gcroots", "current-home")
	if err := os.MkdirAll(filepath.Dir(currentHome), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(generation, currentHome); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hyprDir, 0o755); err != nil {
		t.Fatal(err)
	}
	topLevel := filepath.Join(hyprDir, "hyprland.lua")
	if err := os.Symlink(managedTarget, topLevel); err != nil {
		t.Fatal(err)
	}

	_, err := managedUserNamespaceRuntimeTargets(home, hyprDir)
	if err == nil || !strings.Contains(err.Error(), "unowned top-level Hypr runtime collision") {
		t.Fatalf("mutable current-home alias was accepted: %v", err)
	}
}

func TestDirectEnd4MigrationAcceptsFailedHomeManagerGenerationAsset(t *testing.T) {
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skipf("nix is unavailable: %v", err)
	}
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	const content = "-- managed variables\n"
	filesTree := filepath.Join(t.TempDir(), "home-manager-files")
	managedTarget := filepath.Join(filesTree, ".config", "hypr", "variables.lua")
	writeModeTestFile(t, managedTarget, content, 0o444)
	filesStore := addHomeManagerFilesNixStorePath(t, filesTree)
	storeTarget := filepath.Join(filesStore, ".config", "hypr", "variables.lua")
	publicationPath := filepath.Join(hyprDir, "variables.lua")
	if err := os.MkdirAll(hyprDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeTarget, publicationPath); err != nil {
		t.Fatal(err)
	}
	state, err := snapshotRuntimePathState(publicationPath)
	if err != nil {
		t.Fatal(err)
	}
	publication := runtimePublication{path: publicationPath, content: content, mode: 0o644}
	asset := directEnd4MigrationAsset{sourceRel: "variables.lua", destRel: "variables.lua", mode: 0o644}

	got, ok := directEnd4MigrationHomeManagerSymlinkContent(home, publication, state, asset)
	if !ok || got != content {
		t.Fatalf("failed-generation End4 asset classification = (%q, %v), want exact managed content", got, ok)
	}
}

func TestWriteHyprRuntimeShellStateRejectsConcurrentReplacementOfPreservedHomeManagerEntrypoint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n")
	topLevel, _, _ := prepareActiveHomeManagerTopLevelEntrypoint(t, home, hyprDir)
	const winner = "concurrent private entrypoint\n"
	currentHome := filepath.Join(home, ".local", "state", "home-manager", "gcroots", "current-home")
	newGeneration := filepath.Join(home, ".concurrent-home-manager-generation")
	if err := os.MkdirAll(filepath.Join(newGeneration, "home-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	replaced := false

	err := writeHyprRuntimeShellStateWithHook(home, hyprDir, func(_, _ string) error {
		if replaced {
			return nil
		}
		replaced = true
		if err := os.Remove(topLevel); err != nil {
			return err
		}
		if err := os.WriteFile(topLevel, []byte(winner), 0o600); err != nil {
			return err
		}
		if err := os.Remove(currentHome); err != nil {
			return err
		}
		return os.Symlink(newGeneration, currentHome)
	})
	if err == nil || !strings.Contains(err.Error(), "preserved Home Manager shell runtime changed before commit") {
		t.Fatalf("concurrent Home Manager replacement error = %v", err)
	}
	assertTestFileState(t, topLevel, winner, 0o600)
	if got, err := os.Readlink(currentHome); err != nil || got != newGeneration {
		t.Fatalf("concurrent Home Manager generation changed: target=%q err=%v", got, err)
	}
}

func TestWriteHyprRuntimeShellStatePreservesImmutableLinkAcrossConcurrentHomeManagerGenerationSwitch(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	profile, _ := shellruntime.ProfileByID(shellruntime.Caelestia)
	writeRuntimeProfileAssets(t, home, hyprDir, profile)
	writeTestFile(t, shellruntime.ActiveShellStatePath(home), profile.ID+"\n")
	topLevel, target, _ := prepareActiveHomeManagerTopLevelEntrypoint(t, home, hyprDir)
	before, err := os.Lstat(topLevel)
	if err != nil {
		t.Fatal(err)
	}
	currentHome := filepath.Join(home, ".local", "state", "home-manager", "gcroots", "current-home")
	newGeneration := filepath.Join(home, ".concurrent-home-manager-generation")
	if err := os.MkdirAll(filepath.Join(newGeneration, "home-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	switched := false

	err = writeHyprRuntimeShellStateWithHook(home, hyprDir, func(_, _ string) error {
		if switched {
			return nil
		}
		switched = true
		if err := os.Remove(currentHome); err != nil {
			return err
		}
		return os.Symlink(newGeneration, currentHome)
	})
	if err != nil {
		t.Fatalf("immutable Home Manager link was rejected after current generation switched: %v", err)
	}
	after, statErr := os.Lstat(topLevel)
	if statErr != nil || !os.SameFile(before, after) {
		t.Fatalf("preserved top-level identity changed: before=%v after=%v err=%v", before, after, statErr)
	}
	if got, err := os.Readlink(topLevel); err != nil || got != target {
		t.Fatalf("preserved top-level target changed: target=%q err=%v", got, err)
	}
	if got, err := os.Readlink(currentHome); err != nil || got != newGeneration {
		t.Fatalf("concurrent Home Manager generation changed: target=%q err=%v", got, err)
	}
}

func prepareActiveHomeManagerTopLevelEntrypoint(t *testing.T, home, hyprDir string) (topLevel, target, payload string) {
	t.Helper()
	generation := filepath.Join(home, ".home-manager-generation")
	payload = stableRuntimeSourceConfig(
		shellruntime.RuntimeFile(home, "hyprland.lua"),
		"Wahrwelt stable Hyprland entrypoint.",
	)
	homeFiles, target := addHomeManagerFilesStoreLeaf(t, "hyprland.lua", payload)
	if err := os.MkdirAll(generation, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(homeFiles, filepath.Join(generation, "home-files")); err != nil {
		t.Fatal(err)
	}
	currentHome := filepath.Join(home, ".local", "state", "home-manager", "gcroots", "current-home")
	if err := os.MkdirAll(filepath.Dir(currentHome), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(generation, currentHome); err != nil {
		t.Fatal(err)
	}
	topLevel = filepath.Join(hyprDir, "hyprland.lua")
	if err := os.Symlink(target, topLevel); err != nil {
		t.Fatal(err)
	}
	return topLevel, target, payload
}

func TestHomeManagerFixtureIgnoresNixStderrWarnings(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	const want = "/nix/store/00000000000000000000000000000000-home-manager-files"
	script := "#!" + sh + "\nprintf '%s\\n' 'warning: SQLite database is busy' >&2\nprintf '%s\\n' '" + want + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if got := addHomeManagerFilesNixStorePath(t, t.TempDir()); got != want {
		t.Fatalf("store path = %q, want %q", got, want)
	}
}

func addHomeManagerFilesNixStorePath(t *testing.T, source string) string {
	t.Helper()
	const name = "home-manager-files"
	command := exec.Command(
		"nix",
		"--extra-experimental-features",
		"nix-command",
		"store",
		"add-path",
		"--name",
		name,
		source,
	)
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("add runtime fixture to the Nix store: %v\n%s\n%s", err, output, stderr.String())
	}
	storePath := strings.TrimSpace(string(output))
	if !strings.HasPrefix(storePath, "/nix/store/") || !strings.HasSuffix(storePath, "-"+name) {
		t.Fatalf("unexpected runtime fixture store path: %q", storePath)
	}
	return storePath
}

func addHomeManagerFilesStoreLeaf(t *testing.T, relative, content string) (string, string) {
	t.Helper()
	filesTree := filepath.Join(t.TempDir(), "home-manager-files")
	leaf := filepath.Join(filesTree, ".config", "hypr", filepath.FromSlash(relative))
	writeModeTestFile(t, leaf, content, 0o444)
	storeRoot := addHomeManagerFilesNixStorePath(t, filesTree)
	return storeRoot, filepath.Join(storeRoot, ".config", "hypr", filepath.FromSlash(relative))
}

func TestRuntimeTransactionRollbackPreservesConcurrentWinner(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "runtime.lua")
			injected := errors.New("injected post-write failure")
			tx, err := beginRuntimeFileTransaction([]string{path}, func(operation, gotPath string) error {
				if operation != runtimeMutationWrite || gotPath != path {
					t.Fatalf("unexpected mutation hook: %s %s", operation, gotPath)
				}
				if err := os.Remove(path); err != nil {
					return err
				}
				if kind == "regular" {
					return errors.Join(os.WriteFile(path, []byte("concurrent winner\n"), 0o600), injected)
				}
				target := filepath.Join(root, "concurrent-target")
				if err := os.WriteFile(target, []byte("concurrent symlink target\n"), 0o600); err != nil {
					return err
				}
				if err := os.Symlink(target, path); err != nil {
					return err
				}
				return injected
			})
			if err != nil {
				t.Fatal(err)
			}

			err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
				if err := os.WriteFile(path, []byte("transaction-owned result\n"), 0o644); err != nil {
					return runtimePathState{}, err
				}
				return snapshotRuntimePathState(path)
			})
			if !errors.Is(err, injected) {
				t.Fatalf("transaction error = %v, want injected failure", err)
			}
			if kind == "regular" {
				assertTestFileState(t, path, "concurrent winner\n", 0o600)
				return
			}
			if info, statErr := os.Lstat(path); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("concurrent symlink winner was replaced: info=%v err=%v", info, statErr)
			}
		})
	}
}

func TestRuntimeTransactionRollbackRetainsOriginalRecoveryForConcurrentWinner(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.lua")
	writeModeTestFile(t, path, "prior managed runtime\n", 0o640)
	injected := errors.New("injected post-write failure")
	tx, err := beginRuntimeFileTransaction([]string{path}, func(operation, gotPath string) error {
		if operation != runtimeMutationWrite || gotPath != path {
			t.Fatalf("unexpected mutation hook: %s %s", operation, gotPath)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("concurrent winner\n"), 0o600); err != nil {
			return err
		}
		return injected
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.close()

	err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
		if err := os.WriteFile(path, []byte("transaction-owned result\n"), 0o644); err != nil {
			return runtimePathState{}, err
		}
		return snapshotRuntimePathState(path)
	})
	if !errors.Is(err, injected) || !strings.Contains(err.Error(), "recovery retained at") {
		t.Fatalf("transaction error = %v, want injected failure with retained recovery", err)
	}
	assertTestFileState(t, path, "concurrent winner\n", 0o600)
	recovery := tx.recovery[path]
	if recovery == nil || recovery.retained == "" {
		t.Fatal("original runtime recovery was not retained")
	}
	assertTestFileState(t, recovery.retained, "prior managed runtime\n", 0o640)
}

func TestRuntimeTransactionDoesNotBlessWinnerBetweenPublicationAndOwnershipRecord(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.lua")
	injected := errors.New("injected failure after ownership record")
	tx, err := beginRuntimeFileTransaction([]string{path}, func(_, _ string) error { return injected })
	if err != nil {
		t.Fatal(err)
	}
	defer tx.close()

	err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
		owned, err := publishRuntimeRegularFile(path, []byte("transaction-owned result\n"), 0o644, tx.states[path], tx.parents[filepath.Dir(path)])
		if err != nil {
			return runtimePathState{}, err
		}
		if err := os.Remove(path); err != nil {
			return runtimePathState{}, err
		}
		if err := os.WriteFile(path, []byte("winner between publication and record\n"), 0o600); err != nil {
			return runtimePathState{}, err
		}
		return owned, nil
	})
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error = %v, want injected failure", err)
	}
	assertTestFileState(t, path, "winner between publication and record\n", 0o600)
}

func TestRuntimeTransactionPinnedParentRejectsSwapWithoutWritingVictim(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "runtime-parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "runtime.lua")
	tx, err := beginRuntimeFileTransaction([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.close()
	victim := filepath.Join(root, "victim")
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	movedParent := filepath.Join(root, "pinned-parent-recovery")
	if err := os.Rename(parent, movedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, parent); err != nil {
		t.Fatal(err)
	}

	err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
		return publishRuntimeRegularFile(path, []byte("must not reach victim\n"), 0o644, tx.states[path], tx.parents[filepath.Dir(path)])
	})
	if err == nil || !strings.Contains(err.Error(), "parent directory changed") {
		t.Fatalf("expected pinned-parent rejection, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(victim, "runtime.lua")); !os.IsNotExist(err) {
		t.Fatalf("parent swap wrote outside runtime namespace: %v", err)
	}
}

func TestRuntimeTransactionPinsMissingParentAncestorAtBegin(t *testing.T) {
	root := t.TempDir()
	ancestor := filepath.Join(root, "runtime-ancestor")
	if err := os.Mkdir(ancestor, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(ancestor, "missing", "runtime-parent")
	path := filepath.Join(parent, "runtime.lua")
	tx, err := beginRuntimeFileTransaction([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.close()

	victim := filepath.Join(root, "victim")
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	movedAncestor := filepath.Join(root, "pinned-missing-ancestor-recovery")
	if err := os.Rename(ancestor, movedAncestor); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, ancestor); err != nil {
		t.Fatal(err)
	}

	plan := runtimeTransactionPlan{publications: []runtimePublication{{path: path}}}
	err = tx.pinPublicationParents(plan)
	if err == nil || !strings.Contains(err.Error(), "parent directory changed") {
		t.Fatalf("expected begin-time anchor rejection, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(victim, "missing", "runtime-parent", "runtime.lua")); !os.IsNotExist(err) {
		t.Fatalf("missing-parent ancestor swap wrote outside runtime namespace: %v", err)
	}
}

func TestRuntimeTransactionPinsBeforeSnapshotForAbsentTarget(t *testing.T) {
	for _, scenario := range []string{"existing-parent", "missing-parent-ancestor"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			ancestor := filepath.Join(root, "runtime-ancestor")
			if err := os.Mkdir(ancestor, 0o755); err != nil {
				t.Fatal(err)
			}
			parent := ancestor
			if scenario == "missing-parent-ancestor" {
				parent = filepath.Join(ancestor, "missing", "runtime-parent")
			}
			path := filepath.Join(parent, "runtime.lua")
			movedAncestor := filepath.Join(root, "pinned-runtime-ancestor")
			victim := filepath.Join(root, "victim")
			if err := os.Mkdir(victim, 0o755); err != nil {
				t.Fatal(err)
			}

			_, err := beginRuntimeFileTransactionWithHooks([]string{path}, nil, nil, func(gotPath string) error {
				if gotPath != path {
					t.Fatalf("begin hook path = %s, want %s", gotPath, path)
				}
				if err := os.Rename(ancestor, movedAncestor); err != nil {
					return err
				}
				if err := os.Symlink(victim, ancestor); err != nil {
					return err
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "parent directory changed") {
				t.Fatalf("expected pin-before-snapshot rejection, got %v", err)
			}
			if _, err := os.Lstat(filepath.Join(victim, "runtime.lua")); !os.IsNotExist(err) {
				t.Fatalf("begin-time parent swap created a victim runtime path: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(victim, "missing", "runtime-parent", "runtime.lua")); !os.IsNotExist(err) {
				t.Fatalf("begin-time ancestor swap created a victim runtime path: %v", err)
			}
		})
	}
}

func TestRuntimeTransactionRejectsLegacyPathAppearingBelowMissingParent(t *testing.T) {
	root := t.TempDir()
	hyprDir := filepath.Join(root, "hypr")
	if err := os.Mkdir(hyprDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hyprDir, "wahrwelt", "hyprland.conf")
	unknown := "concurrent legacy runtime\n"
	plan := runtimeTransactionPlan{removals: []string{path}}
	tx, err := beginRuntimeFileTransactionWithHooks(plan.paths(), nil, nil, func(gotPath string) error {
		if gotPath != path {
			t.Fatalf("begin hook path = %s, want %s", gotPath, path)
		}
		if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(unknown), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = tx.pinPublicationParents(plan)
	err = tx.finish(err)
	if err == nil || !strings.Contains(err.Error(), "changed while pinning parent") {
		t.Fatalf("late legacy path error = %v, want ownership collision", err)
	}
	assertTestFileState(t, path, unknown, 0o600)
}

func TestRuntimeRegularReplacementPreservesExternalHardlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.lua")
	alias := filepath.Join(root, "external-alias.lua")
	writeModeTestFile(t, path, "prior shared bytes\n", 0o640)
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(alias)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := beginRuntimeFileTransaction([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
		return publishRuntimeRegularFile(path, []byte("new managed bytes\n"), 0o644, tx.states[path], tx.parents[filepath.Dir(path)])
	})
	err = tx.finish(err)
	if err != nil {
		t.Fatal(err)
	}
	assertTestFileState(t, alias, "prior shared bytes\n", 0o640)
	assertTestFileState(t, path, "new managed bytes\n", 0o644)
	after, err := os.Lstat(alias)
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("external hardlink inode changed")
	}
	if os.SameFile(after, published) {
		t.Fatal("managed runtime replacement reused the externally linked inode")
	}
	assertOnlyRuntimeRecoveryResidue(t, root, 1)
}

func TestRuntimeReplacementRestoresExactPairAfterCandidateReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.lua")
	writeModeTestFile(t, path, "prior managed bytes\n", 0o640)
	expected, err := snapshotRuntimePathState(path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openPinnedRuntimeDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.close()
	unknown := "candidate basename replacement\n"
	var recoveryPath string
	_, err = replaceRuntimeRegularAtWithExchangeHook(
		directory,
		filepath.Base(path),
		path,
		expected,
		[]byte("new managed bytes\n"),
		0o644,
		true,
		func(candidatePath string) error {
			recoveryPath = candidatePath
			if err := os.Remove(candidatePath); err != nil {
				return err
			}
			return os.WriteFile(candidatePath, []byte(unknown), 0o600)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "exact pair restored") {
		t.Fatalf("candidate replacement error = %v, want exact pair rollback", err)
	}
	assertTestFileState(t, path, "prior managed bytes\n", 0o640)
	assertTestFileState(t, recoveryPath, unknown, 0o600)
}

func TestRuntimeCreatedDirectoryCleanupPreservesConcurrentReplacement(t *testing.T) {
	for _, scenario := range []string{"replacement", "concurrent-content"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "runtime-parent")
			path := filepath.Join(parent, "runtime.lua")
			tx, err := beginRuntimeFileTransaction([]string{path}, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan := runtimeTransactionPlan{publications: []runtimePublication{{path: path}}}
			if err := tx.pinPublicationParents(plan); err != nil {
				t.Fatal(err)
			}
			winner := filepath.Join(parent, "winner")
			switch scenario {
			case "replacement":
				recovery := filepath.Join(root, "transaction-created-parent")
				if err := os.Rename(parent, recovery); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0o755); err != nil {
					t.Fatal(err)
				}
			case "concurrent-content":
			}
			writeModeTestFile(t, winner, "concurrent winner\n", 0o600)
			injected := errors.New("injected precommit failure")
			err = tx.finish(injected)
			if !errors.Is(err, injected) {
				t.Fatalf("finish error = %v, want injected failure", err)
			}
			assertTestFileState(t, winner, "concurrent winner\n", 0o600)
		})
	}
}

func TestRuntimeDirectoryCandidateReplacementIsNeverAdopted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime-parent", "runtime.lua")
	tx, err := beginRuntimeFileTransaction([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := tx.anchors[filepath.Dir(path)]
	if parent == nil {
		t.Fatal("missing begin-time runtime parent anchor")
	}
	var candidatePath string
	var ownedRecovery string
	_, err = tx.createAndJournalRuntimeDirectoryWithHook(parent, "runtime-parent", filepath.Dir(path), func(path string) error {
		candidatePath = path
		ownedRecovery = filepath.Join(root, "transaction-created-directory")
		if err := os.Rename(path, ownedRecovery); err != nil {
			return err
		}
		return os.Mkdir(path, 0o711)
	})
	err = tx.finish(err)
	if err == nil || !strings.Contains(err.Error(), "transaction recovery retained at "+ownedRecovery) {
		t.Fatalf("candidate replacement error = %v, want exact retained recovery", err)
	}
	unknown, err := os.Stat(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := unknown.Mode().Perm(), os.FileMode(0o711); got != want {
		t.Fatalf("unknown candidate mode = %o, want %o", got, want)
	}
	owned, err := os.Stat(ownedRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := owned.Mode().Perm(), os.FileMode(0o755); got != want {
		t.Fatalf("transaction-owned recovery mode = %o, want %o", got, want)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("unknown candidate was published at the runtime parent: %v", err)
	}
}

func TestRuntimeDirectoryCandidateReplacementBeforePinIsNeverAdopted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime-parent", "runtime.lua")
	tx, err := beginRuntimeFileTransaction([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := tx.anchors[filepath.Dir(path)]
	if parent == nil {
		t.Fatal("missing begin-time runtime parent anchor")
	}
	var candidatePath string
	var ownedRecovery string
	_, err = tx.createAndJournalRuntimeDirectoryWithHooks(
		parent,
		"runtime-parent",
		filepath.Dir(path),
		func(createdPath string) error {
			candidatePath = createdPath
			ownedRecovery = filepath.Join(root, "created-directory-before-pin")
			if err := os.Rename(createdPath, ownedRecovery); err != nil {
				return err
			}
			return os.Mkdir(createdPath, 0o711)
		},
		nil,
	)
	err = tx.finish(err)
	if err == nil || !strings.Contains(err.Error(), "changed before pin") {
		t.Fatalf("candidate pre-pin replacement error = %v, want creator-token rejection", err)
	}
	unknown, err := os.Stat(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := unknown.Mode().Perm(), os.FileMode(0o711); got != want {
		t.Fatalf("unknown candidate mode = %o, want %o", got, want)
	}
	owned, err := os.Stat(ownedRecovery)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := owned.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("created candidate recovery mode = %o, want %o", got, want)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("unknown candidate was published at the runtime parent: %v", err)
	}
}

func TestRuntimeRemovalQuarantinesRacedUnknownInsteadOfDeleting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hyprland.conf")
	originalRecovery := filepath.Join(root, "original-managed.conf")
	writeModeTestFile(t, path, "known managed bytes\n", 0o640)
	state, err := snapshotRuntimePathState(path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openPinnedRuntimeDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.close()
	quarantine, err := quarantineRuntimePathStateWithHook(path, state, directory, true, func() error {
		if err := os.Rename(path, originalRecovery); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("raced unknown bytes\n"), 0o600)
	})
	if err == nil || !strings.Contains(err.Error(), "restored unknown entry") {
		t.Fatalf("expected raced removal rejection, got quarantine=%v err=%v", quarantine, err)
	}
	assertTestFileState(t, path, "raced unknown bytes\n", 0o600)
	assertTestFileState(t, originalRecovery, "known managed bytes\n", 0o640)
	assertNoRuntimeTransactionResidue(t, root)
}

func TestRuntimeTransactionJournalFailureRollsBackVisibleMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.lua")
	injected := errors.New("injected journal failure")
	tx, err := beginRuntimeFileTransactionWithJournalHook([]string{path}, nil, func(stage runtimeJournalStage, gotPath string) error {
		if stage == runtimeJournalMutation && gotPath == path {
			return injected
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
		return publishRuntimeRegularFile(path, []byte("must roll back\n"), 0o644, tx.states[path], tx.parents[filepath.Dir(path)])
	})
	err = tx.finish(err)
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error = %v, want journal failure", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("journal failure left a visible runtime mutation: %v", err)
	}
	assertOnlyRuntimeRecoveryResidue(t, root, 1)
}

func TestRuntimeDirectoryJournalFailureRemovesExactCreatedChain(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "missing", "runtime-parent")
	path := filepath.Join(parent, "runtime.lua")
	injected := errors.New("injected directory journal failure")
	tx, err := beginRuntimeFileTransactionWithJournalHook([]string{path}, nil, func(stage runtimeJournalStage, _ string) error {
		if stage == runtimeJournalDirectory {
			return injected
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := runtimeTransactionPlan{publications: []runtimePublication{{path: path}}}
	err = tx.pinPublicationParents(plan)
	err = tx.finish(err)
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error = %v, want directory journal failure", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("directory journal failure left a created chain: %v", err)
	}
	assertOnlyRuntimeRecoveryResidue(t, root, 1)
}

func TestRuntimeTransactionRollbackUsesPinnedParentAfterCanonicalSwap(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "runtime-parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "runtime.lua")
	writeModeTestFile(t, path, "prior managed runtime\n", 0o640)
	injected := errors.New("injected post-publication parent swap")
	tx, err := beginRuntimeFileTransaction([]string{path}, func(operation, gotPath string) error {
		if operation != runtimeMutationWrite || gotPath != path {
			t.Fatalf("unexpected mutation hook: %s %s", operation, gotPath)
		}
		movedParent := filepath.Join(root, "pinned-parent-after-publication")
		if err := os.Rename(parent, movedParent); err != nil {
			return err
		}
		if err := os.Mkdir(parent, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("concurrent canonical winner\n"), 0o600); err != nil {
			return err
		}
		return injected
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.close()

	err = tx.mutate(runtimeMutationWrite, path, func() (runtimePathState, error) {
		return publishRuntimeRegularFile(path, []byte("transaction-owned result\n"), 0o644, tx.states[path], tx.parents[filepath.Dir(path)])
	})
	if err == nil || !strings.Contains(err.Error(), "after write") {
		t.Fatalf("transaction error = %v, want post-publication rollback failure", err)
	}
	assertTestFileState(t, filepath.Join(root, "pinned-parent-after-publication", "runtime.lua"), "prior managed runtime\n", 0o640)
	assertTestFileState(t, path, "concurrent canonical winner\n", 0o600)
}

func TestRemoveLegacyRuntimeFileAcceptsExactGeneratedPayloadAndProvenSymlink(t *testing.T) {
	home := t.TempDir()
	hyprDir := filepath.Join(home, ".config", "hypr")
	paths := legacyHyprlandRuntimePaths(home, hyprDir)
	regular := paths[0]
	payloads := knownLegacyRuntimePayloads(regular, home, hyprDir)
	if len(payloads) == 0 {
		t.Fatal("missing generated legacy payload fixture")
	}
	writeModeTestFile(t, regular, payloads[0], 0o640)
	state, err := snapshotRuntimePathState(regular)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openPinnedDirectory(filepath.Dir(regular))
	if err != nil {
		t.Fatal(err)
	}
	defer directory.close()
	if _, err := removeLegacyRuntimeFileWithState(regular, state, directory, home, hyprDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(regular); !os.IsNotExist(err) {
		t.Fatalf("known generated legacy regular remains: %v", err)
	}

	target := paths[5]
	targetPayloads := knownLegacyRuntimePayloads(target, home, hyprDir)
	if len(targetPayloads) == 0 {
		t.Fatal("missing generated legacy target fixture")
	}
	writeModeTestFile(t, target, targetPayloads[0], 0o640)
	link := paths[1]
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	state, err = snapshotRuntimePathState(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := removeLegacyRuntimeFileWithState(link, state, directory, home, hyprDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("known generated legacy symlink remains: %v", err)
	}
	assertTestFileState(t, target, targetPayloads[0], 0o640)
}

func initializeRuntimeTransactionBaseline(t *testing.T, plan runtimeTransactionPlan, home string) {
	t.Helper()
	hyprDir := filepath.Join(home, ".config", "hypr")
	legacy := make(map[string]bool, len(legacyHyprlandRuntimePaths(home, hyprDir)))
	for _, path := range legacyHyprlandRuntimePaths(home, hyprDir) {
		legacy[path] = true
	}
	for index, path := range plan.paths() {
		if path == shellruntime.ActiveShellStatePath(home) || path == shellruntime.End4VariantStatePath(home) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if path == filepath.Join(hyprDir, "hyprland.lua") {
			writeModeTestFile(t, path, knownTopLevelRuntimeEntrypoints(home)[0], 0o600|os.FileMode(index%8))
			continue
		}
		if legacy[path] {
			payloads := knownLegacyRuntimePayloads(path, home, hyprDir)
			if len(payloads) == 0 {
				t.Fatalf("missing known legacy fixture for %s", path)
			}
			writeModeTestFile(t, path, payloads[0], 0o600|os.FileMode(index%8))
			continue
		}
		switch index % 2 {
		case 0:
			writeModeTestFile(t, path, fmt.Sprintf("old bytes %d\n", index), 0o600|os.FileMode(index%8))
		case 1:
			// Absence is part of the rollback contract.
		}
	}
}

func snapshotRuntimePlan(t *testing.T, plan runtimeTransactionPlan) map[string]runtimePathSnapshot {
	t.Helper()
	snapshots := make(map[string]runtimePathSnapshot, len(plan.paths()))
	for _, path := range plan.paths() {
		if _, ok := snapshots[path]; ok {
			continue
		}
		snapshot, err := snapshotRuntimePath(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshots[path] = snapshot
	}
	return snapshots
}

func assertRuntimePlanSnapshot(t *testing.T, plan runtimeTransactionPlan, want map[string]runtimePathSnapshot) {
	t.Helper()
	for _, path := range plan.paths() {
		got, err := snapshotRuntimePath(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want[path]) {
			t.Fatalf("runtime path was not rolled back: %s\ngot:  %#v\nwant: %#v", path, got, want[path])
		}
	}
}

func assertNoRuntimeTransactionResidue(t *testing.T, root string) {
	t.Helper()
	var residue []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".wahrwelt-runtime-") {
			residue = append(residue, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(residue) != 0 {
		t.Fatalf("shell runtime transaction residue remains: %v", residue)
	}
}

func assertOnlyRuntimeRecoveryResidue(t *testing.T, root string, minimum int) {
	t.Helper()
	var recoveries []string
	var transient []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasPrefix(entry.Name(), ".wahrwelt-runtime-") {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".wahrwelt-runtime-recovery-") {
			recoveries = append(recoveries, path)
			return nil
		}
		transient = append(transient, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transient) != 0 {
		t.Fatalf("transient shell runtime transaction residue remains: %v", transient)
	}
	if len(recoveries) < minimum {
		t.Fatalf("shell runtime recoveries = %v, want at least %d", recoveries, minimum)
	}
}

func writeModeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertTestFileState(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if got := readTestFile(t, path); got != content {
		t.Fatalf("%s content = %q, want %q", path, got, content)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("%s mode = %s, want %s", path, info.Mode(), mode)
	}
}

func writeRuntimeProfileAssets(t *testing.T, home, hyprDir string, profile shellruntime.Profile) {
	t.Helper()
	files := []string{
		filepath.Join(hyprDir, "user", "hyprland.lua"),
		filepath.Join(hyprDir, "scripts", "start-shell.sh"),
		filepath.Join(hyprDir, filepath.FromSlash(profile.Launcher)),
	}
	if shellruntime.IsEnd4Profile(profile.ID) {
		files = append(files,
			filepath.Join(hyprDir, "end4-adapter.lua"),
			filepath.Join(hyprDir, "end4", "hyprland.lua"),
			filepath.Join(hyprDir, "end4", "hyprlock.conf"),
			filepath.Join(hyprDir, "end4", "hypridle.conf"),
			filepath.Join(home, ".config", "quickshell", profile.QuickshellConfig, "shell.qml"),
		)
	} else {
		files = append(files, filepath.Join(hyprDir, filepath.FromSlash(profile.Keybinds)))
	}
	for _, path := range files {
		writeTestFile(t, path, "-- test asset\n")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	got := readTestFile(t, path)
	if !strings.Contains(got, want) {
		t.Fatalf("%s missing %q\n%s", path, want, got)
	}
}
