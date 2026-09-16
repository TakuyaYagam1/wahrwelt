package dots

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSharedHyprKeybindsDoNotContainShellSpecificBindings(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/hyprland/keybinds.lua")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"caelestia clipboard",
		"caelestia emoji",
		"caelestia record",
		"caelestia resizer pip",
		"noctalia msg",
		"app2unit -- $terminal",
		`wahrwelt.hypr .. "/scripts/screenshot.sh full"`,
		"$hypr/scripts/record-toggle.sh",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("shared Hypr keybinds must stay shell-neutral; found %q\n%s", forbidden, text)
		}
	}
	if strings.Contains(text, "load_runtime") {
		t.Fatalf("shared Hypr keybinds must not own runtime layer ordering\n%s", text)
	}
	entrypointData, err := os.ReadFile("../../../dots/hypr/hyprland.lua")
	if err != nil {
		t.Fatal(err)
	}
	entrypoint := string(entrypointData)
	for _, want := range []string{
		`wahrwelt.load_runtime("shell-profile.lua")`,
		`wahrwelt.load_runtime("shell-launcher.lua")`,
		`wahrwelt.load_runtime("shell-keybinds.lua")`,
	} {
		if !strings.Contains(entrypoint, want) {
			t.Fatalf("canonical Hyprland entrypoint must own runtime order: missing %q\n%s", want, entrypoint)
		}
	}
}

func TestShellKeybindProfilesUseExpectedLaunchers(t *testing.T) {
	caelestia, err := os.ReadFile("../../../dots/hypr/caelestia/keybinds.lua")
	if err != nil {
		t.Fatal(err)
	}
	caelestiaLauncher, err := os.ReadFile("../../../dots/hypr/caelestia/launcher.lua")
	if err != nil {
		t.Fatal(err)
	}
	noctalia, err := os.ReadFile("../../../dots/hypr/noctalia/keybinds.lua")
	if err != nil {
		t.Fatal(err)
	}
	noctaliaLauncher, err := os.ReadFile("../../../dots/hypr/noctalia/launcher.lua")
	if err != nil {
		t.Fatal(err)
	}
	common, err := os.ReadFile("../../../dots/hypr/shell-common-keybinds.lua")
	if err != nil {
		t.Fatal(err)
	}
	caelestiaProfile := string(caelestia) + string(common)
	noctaliaProfile := string(noctalia) + string(common)
	for _, want := range []string{
		"restore-lock.sh caelestia",
		"shell-selector.sh toggle",
		`"app2unit -- " .. v.terminal`,
		`"app2unit -- " .. v.zed`,
		`wahrwelt.hypr .. "/scripts/screenshot.sh full"`,
		"caelestia clipboard",
	} {
		if !strings.Contains(caelestiaProfile, want) {
			t.Fatalf("caelestia profile missing %q\n%s", want, caelestiaProfile)
		}
	}
	for _, want := range []string{
		`wahrwelt.bind_dispatch("SUPER + SUPER_L", "global caelestia:launcher")`,
		`wahrwelt.bind_dispatch("SUPER + mouse:272", "global caelestia:launcherInterrupt", { non_consuming = true })`,
		`wahrwelt.bind_dispatch("SUPER + mouse_down", "global caelestia:launcherInterrupt", { non_consuming = true })`,
	} {
		if !strings.Contains(string(caelestiaLauncher), want) {
			t.Fatalf("caelestia launcher profile missing %q\n%s", want, string(caelestiaLauncher))
		}
	}
	for _, want := range []string{
		"noctalia-msg.sh",
		"restore-lock.sh noctalia",
		"shell-selector.sh toggle",
		`"app2unit -- " .. v.terminal`,
		`"app2unit -- " .. v.zed`,
		`wahrwelt.hypr .. "/scripts/screenshot.sh full"`,
	} {
		if !strings.Contains(noctaliaProfile, want) {
			t.Fatalf("noctalia profile missing %q\n%s", want, noctaliaProfile)
		}
	}
	for _, want := range []string{
		"noctalia-launcher.sh press",
		"noctalia-launcher.sh interrupt",
		"noctalia-launcher.sh release",
	} {
		if !strings.Contains(string(noctaliaLauncher), want) {
			t.Fatalf("noctalia launcher profile missing %q\n%s", want, string(noctaliaLauncher))
		}
	}
}

func TestAntigravityKeybindUsesInstalledExecutable(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/variables.lua")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `antigravity = "antigravity-ide"`) {
		t.Fatalf("Antigravity keybind must use the installed antigravity-ide executable\n%s", string(data))
	}

	docs, err := os.ReadFile("../../../keybinds.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs), "| `Super+Shift+A` | Antigravity IDE (`antigravity-ide`) |") {
		t.Fatalf("keybinds.md must document the installed Antigravity IDE executable\n%s", string(docs))
	}
}

func TestCodexDesktopLauncherUsesCodexAppName(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/home/programs/codex-desktop.nix")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`"Name=ChatGPT Community"`,
		`"Name=Codex App"`,
		`"Icon=codex-desktop"`,
		`codexDesktopIcon = "${codexDesktopBase}/share/icons/hicolor/256x256/apps/codex-desktop.png";`,
		`"Icon=${codexDesktopIcon}"`,
		`"${codexDesktopBase}/bin/codex-desktop"`,
		`"$out/bin/codex-desktop"`,
		"package = codexDesktopPackage;",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Codex Desktop launcher rename contract missing %q\n%s", want, text)
		}
	}
}

func TestHyprKeybindFilesDoNotContainUnexpectedDuplicateChords(t *testing.T) {
	allowed := map[string]bool{
		"hyprland/keybinds.lua|CTRL+SUPER+ALT+Backslash": true,
		"noctalia/launcher.lua|SUPER+SUPER_L":            true,
	}

	for _, rel := range []string{
		"hyprland/keybinds.lua",
		"caelestia/keybinds.lua",
		"caelestia/launcher.lua",
		"noctalia/keybinds.lua",
		"noctalia/launcher.lua",
		"end4/keybinds.lua",
		"shell-common-keybinds.lua",
		"shell-workspace-keybinds.lua",
	} {
		t.Run(rel, func(t *testing.T) {
			assertNoUnexpectedDuplicateHyprBinds(t, rel, allowed)
		})
	}
}

func TestEnd4PreservesWahrweltSpotifyWindowBehavior(t *testing.T) {
	keybindsData, err := os.ReadFile("../../../dots/hypr/end4/keybinds.lua")
	if err != nil {
		t.Fatal(err)
	}
	keybinds := string(keybindsData)
	if !strings.Contains(keybinds, `wahrwelt.bind_exec(v.kbCloseWindow, wahrwelt.hypr .. "/scripts/close-active.sh")`) {
		t.Fatalf("end4 must use Wahrwelt's app-aware close handler\n%s", keybinds)
	}
	if strings.Contains(keybinds, `wahrwelt.bind_dispatch(v.kbCloseWindow, "killactive")`) {
		t.Fatalf("end4 must not bypass the app-aware close handler with killactive\n%s", keybinds)
	}
	if !strings.Contains(keybinds, `"/user/default.lua"`) {
		t.Fatalf("end4 keybind editor must target the canonical user Lua namespace\n%s", keybinds)
	}

	commonRulesData, err := os.ReadFile("../../../dots/hypr/shell-common-rules.lua")
	if err != nil {
		t.Fatal(err)
	}
	commonRules := string(commonRulesData)
	for _, want := range []string{
		`spotify`,
		`initial_title = "Spotify( Free)?"`,
		`workspace = "special:music"`,
	} {
		if !strings.Contains(commonRules, want) {
			t.Fatalf("shared Spotify window rules missing %q\n%s", want, commonRules)
		}
	}

	canonicalRulesData, err := os.ReadFile("../../../dots/hypr/hyprland/rules.lua")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonicalRulesData), `require("shell-common-rules")`) {
		t.Fatalf("canonical Hyprland rules must load Wahrwelt's shared window rules\n%s", canonicalRulesData)
	}

	hyprPatchData, err := os.ReadFile("../../../NixOS/home/end4/patches/hypr.nix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hyprPatchData), `dofile(config_home .. "/hypr/shell-common-rules.lua")`) {
		t.Fatalf("End4 artifact must not load shared rules a second time\n%s", hyprPatchData)
	}
}

func TestShellProfilesMatchRuntimeScriptContract(t *testing.T) {
	profilesData, err := os.ReadFile("../../../NixOS/home/shells/profiles.nix")
	if err != nil {
		t.Fatal(err)
	}
	runtimeData, err := os.ReadFile("../../../dots/hypr/scripts/shell-runtime.sh")
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile("../shellruntime/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		DefaultProfile string `json:"defaultProfile"`
		Profiles       []struct {
			ID string `json:"id"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}

	profiles := manifestProfileIDs(manifest.Profiles)
	if !strings.Contains(string(profilesData), "ordered = shellRuntimeManifest.profiles;") {
		t.Fatalf("profiles.nix must source ordered profile metadata from shell runtime manifest\n%s", string(profilesData))
	}
	runtime := string(runtimeData)
	for _, profile := range profiles {
		pattern := regexp.MustCompile(`(\|\s*` + regexp.QuoteMeta(profile) + `\b)|(\b` + regexp.QuoteMeta(profile) + `\s*\|)`)
		if !pattern.MatchString(runtime) {
			t.Fatalf("shell-runtime.sh valid profile case is missing %q\n%s", profile, runtime)
		}
	}

	if !strings.Contains(string(profilesData), "inherit (shellRuntimeManifest) defaultProfile;") {
		t.Fatalf("profiles.nix must source defaultProfile from shell runtime manifest\n%s", string(profilesData))
	}
	if !strings.Contains(runtime, `wahrwelt_default_shell_profile="`+manifest.DefaultProfile+`"`) {
		t.Fatalf("shell-runtime.sh default profile drifted from manifest default %q\n%s", manifest.DefaultProfile, runtime)
	}
}

func manifestProfileIDs(profiles []struct {
	ID string `json:"id"`
}) []string {
	ids := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		ids = append(ids, profile.ID)
	}
	return ids
}

func TestFreshHyprAndSelectorAssetsUseOnlyCanonicalBrand(t *testing.T) {
	legacyRuntimeMatcher := filepath.Clean("../../../dots/hypr/scripts/shell-profile-sync.sh")
	for _, root := range []string{
		"../../../dots/hypr",
		"../../../NixOS/home/shells/quickshell/wahrwelt-shell-selector",
	} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if strings.Contains(strings.ToLower(entry.Name()), "mysetup") {
				t.Errorf("fresh managed asset keeps legacy name: %s", path)
			}
			if entry.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := strings.ToLower(string(data))
			if strings.Contains(text, "mysetup") && filepath.Clean(path) != legacyRuntimeMatcher {
				t.Errorf("fresh managed asset keeps legacy text: %s", path)
			}
			if filepath.Clean(path) == legacyRuntimeMatcher &&
				(strings.Count(text, "mysetup") != 1 || !strings.Contains(text, "for namespace in mysetup wahrwelt; do")) {
				t.Errorf("runtime migration must allow only the exact legacy namespace matcher: %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoctaliaLauncherScriptIsGuarded(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/scripts/noctalia-launcher.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"active_file=",
		"interrupt_file=",
		"wahrwelt_enter_runtime_lock_v2",
		"wahrwelt-noctalia-launcher-v2.lock",
		"wahrwelt-noctalia-launcher",
		"wahrwelt_open_managed_regular_file",
		"wahrwelt_noctalia_action launcher-toggle",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("noctalia launcher wrapper missing %q\n%s", want, text)
		}
	}
}

func TestShellSelectorScriptTracksFocusedMonitorAndActiveShell(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/scripts/shell-selector.sh")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile("../../../dots/hypr/scripts/shell-runtime.sh")
	if err != nil {
		t.Fatal(err)
	}
	selectorText := string(data)
	helperText := string(helper)
	text := selectorText + helperText
	for _, want := range []string{
		"wahrwelt_open_private_state_directory wahrwelt-shell-selector shell-selector-state",
		"wahrwelt_enter_runtime_lock_v2",
		"wahrwelt-shell-selector-v2.lock",
		"kernel-owned abstract AF_UNIX lock",
		"wahrwelt_lock_fs_helper_path()",
		`"$helper" runtime-lock-run`,
		`--root "$wahrwelt_runtime_session_public_dir"`,
		`--name "$name"`,
		`--wait-ms "$wait_ms"`,
		"selector_name=\"wahrwelt-shell-selector\"",
		"WAHRWELT_SHELL_SELECTOR_MONITOR",
		"WAHRWELT_ACTIVE_SHELL",
		"WAHRWELT_SHELL_SELECTOR_MONITOR",
		"WAHRWELT_ACTIVE_SHELL",
		"wait_for_selector_spawn()",
		"detect_shell_from_processes()",
		"detect_shell_from_entrypoint()",
		"wahrwelt_noctalia_running",
		"^WAHRWELT_END4_PROFILE=end4$",
		"^WAHRWELT_END4_PROFILE=end4-pc$",
		"wahrwelt_detect_shell_adapter",
		"user/hyprland.lua",
		"hyprctl monitors -j",
		"active-shell",
		"start-shell.sh",
		"qs -c \"$selector_name\"",
		"switch_shell()",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("shell selector script missing %q\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		`state_dir=`,
		`lock_dir=`,
		`lock_pid_file=`,
		`lock_owner_file=`,
		`lock_identity=`,
		"acquire_lock()",
		"cleanup_lock()",
		"trap cleanup_lock",
	} {
		if strings.Contains(selectorText, forbidden) {
			t.Fatalf("shell selector still contains filesystem lock state %q\n%s", forbidden, selectorText)
		}
	}
}

func TestRecordToggleScriptUsesLockAndPidValidation(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/scripts/record-toggle.sh")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile("../../../dots/hypr/scripts/shell-runtime.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + string(helper)
	for _, want := range []string{
		"wahrwelt_enter_runtime_lock_v2",
		"wahrwelt-record-toggle-v2.lock",
		"wahrwelt_open_private_state_directory wahrwelt-recording record-toggle-state",
		"wahrwelt_open_managed_regular_file",
		`pid_file="/proc/`,
		"wahrwelt-record-toggle",
		`ps -p "$pid" -o args=`,
		"gpu-screen-recorder",
		"WAHRWELT_RECORD_TARGET",
		"WAHRWELT_RECORD_TARGET",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("record toggle script missing %q\n%s", want, text)
		}
	}
}

func TestStartShellScriptCleansDuplicateProfiles(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/scripts/start-shell.sh")
	if err != nil {
		t.Fatal(err)
	}
	startText := string(data)
	text := startText
	for _, helper := range []string{
		"shell-runtime.sh",
		"shell-process.sh",
		"shell-profile-sync.sh",
		"shell-runtime-env.sh",
	} {
		data, err := os.ReadFile(filepath.Join("../../../dots/hypr/scripts", helper))
		if err != nil {
			t.Fatal(err)
		}
		text += string(data)
	}
	for _, want := range []string{
		"wahrwelt_enter_runtime_lock_v2",
		"wahrwelt-shell-v2.lock",
		"shell-runtime.sh",
		"shell-process.sh",
		"shell-profile-sync.sh",
		"shell-runtime-env.sh",
		"wahrwelt_pid_matches",
		"wahrwelt_noctalia_pids()",
		"wahrwelt_noctalia_daemon_flag()",
		`pgrep -u "$wahrwelt_user_name" -x noctalia`,
		"running_count()",
		"dedupe_shell()",
		"cleanup_legacy_end4_processes()",
		"wahrwelt_pid_is_legacy_end4_upgrade_token()",
		"wahrwelt_legacy_end4_upgrade_pids()",
		"wahrwelt_open_end4_upgrade_state()",
		"wahrwelt_merge_end4_upgrade_tokens",
		"wahrwelt_remove_end4_upgrade_tokens",
		"wahrwelt-end4-upgrade",
		"sync_runtime_shell_files()",
		"validate_profile_ready()",
		"prepare_profile_or_fallback()",
		"require_file()",
		"aborting shell switch before stopping current shell",
		"persistent_state_file=",
		"selector_pattern=",
		"caelestia_resizer_handle=",
		"WAHRWELT_END4_PROFILE=\"$profile\"",
		"WAHRWELT_QS_CONFIG=\"$end4_quickshell_path\"",
		"ILLOGICAL_IMPULSE_DOTFILES_SOURCE=\"$wahrwelt_config_home\"",
		"^WAHRWELT_END4_PROFILE=(end4|end4-pc)$",
		"end4_idle_handle=",
		"stop_shell_selector()",
		"stop_caelestia_resizer()",
		"stop_end4_idle()",
		"hypr_runtime_dir=",
		"runtime_file()",
		"shell-keybinds.lua",
		`hypridle -c "$idle_config"`,
		"hypr_supports_lua_runtime()",
		"running Hyprland is older than 0.55",
		"hyprctl reload",
		"user_systemd_scope_available()",
		"launch_shell_process()",
		"exec systemd-run",
		`"$setsid_command" --fork "$@"`,
		`(launch_shell_process "$@" >>"$log_file" 2>&1 &)`,
		`(launch_shell_process caelestia resizer -d >>"$log_file" 2>&1 &)`,
		`dedupe_shell "caelestia"`,
		`dedupe_shell "caelestia resizer"`,
		`dedupe_shell "noctalia"`,
		`--legacy-direct-end4-upgrade-processes`,
		`--persist-end4-upgrade-processes`,
		`^[1-9][0-9]*:[1-9][0-9]*:(ii|end4-pC)`,
		`if [ -n "$legacy_end4_upgrade_tokens" ]; then`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("start-shell script missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "qs kill --any-display") {
		t.Fatalf("start-shell script must not kill every quickshell instance\n%s", text)
	}
	if strings.Contains(text, `pkill -u "$user_name" -x hypridle`) {
		t.Fatalf("start-shell script must only stop the managed end4 hypridle instance\n%s", text)
	}
	if strings.Contains(text, `("$@" >>"$log_file" 2>&1 &)`) ||
		strings.Contains(text, `(caelestia resizer -d >>"$log_file" 2>&1 &)`) {
		t.Fatalf("shell processes must not inherit the runtime-lock process group\n%s", text)
	}
	if strings.Contains(text, `(^|[ /])noctalia([[:space:]]|$)`) {
		t.Fatalf("noctalia process detection must not match bare profile arguments\n%s", text)
	}
	if strings.Contains(startText, "legacy_end4_handle=") {
		t.Fatalf("direct End4 argv recognition must not be a permanent runtime handle\n%s", startText)
	}
	if strings.Contains(string(data), "shell-end4-overrides.sh") {
		t.Fatalf("start-shell must not source the retired End4 runtime override helper\n%s", data)
	}
	durableOpenIndex := strings.Index(startText, "if ! wahrwelt_open_end4_upgrade_state; then")
	durableMergeIndex := strings.Index(startText, "wahrwelt_merge_end4_upgrade_tokens \"$requested_legacy_end4_upgrade_tokens\"")
	prepareRuntimeIndex := strings.Index(startText, "\nprepare_runtime_environment\n")
	acquireLockIndex := strings.Index(startText, "wahrwelt_enter_runtime_lock_v2")
	if durableOpenIndex < 0 || durableMergeIndex < 0 || prepareRuntimeIndex < 0 || acquireLockIndex < 0 ||
		acquireLockIndex >= durableOpenIndex || durableOpenIndex >= durableMergeIndex || durableMergeIndex >= prepareRuntimeIndex {
		t.Fatalf("start-shell must acquire the v2 process lock before durably merging End4 upgrade provenance and preparing runtime")
	}

	beginIndex := strings.LastIndex(startText, "if ! begin_switch_transaction; then")
	prepareIndex := strings.LastIndex(startText, "if ! prepare_profile_or_fallback; then")
	legacyCleanupIndex := strings.LastIndex(startText, "if ! cleanup_legacy_end4_processes; then")
	legacyTouchedIndex := -1
	if legacyCleanupIndex >= 0 {
		legacyTouchedIndex = strings.LastIndex(startText[:legacyCleanupIndex], "shell_processes_touched=1")
	}
	stopIndex := strings.LastIndex(startText, `if [ "$previous" != "$profile" ] || [ -n "$requested_profile" ]; then`)
	startIndex := strings.LastIndex(startText, "if ! start_profile_shell; then")
	persistIndex := strings.LastIndex(startText, "if ! persist_profile; then")
	commitIndex := strings.LastIndex(startText, "switch_transaction_active=0")
	reloadIndex := strings.LastIndex(startText, "if ! reload_hypr; then")
	propagateIndex := strings.LastIndex(startText, "\npropagate_runtime_environment\n")
	for name, index := range map[string]int{
		"begin":          beginIndex,
		"prepare":        prepareIndex,
		"legacy touched": legacyTouchedIndex,
		"legacy cleanup": legacyCleanupIndex,
		"stop":           stopIndex,
		"start":          startIndex,
		"persist":        persistIndex,
		"commit":         commitIndex,
		"reload":         reloadIndex,
		"propagate":      propagateIndex,
	} {
		if index < 0 {
			t.Fatalf("start-shell script missing %s ordering marker\n%s", name, text)
		}
	}
	if beginIndex >= prepareIndex || prepareIndex >= legacyTouchedIndex || legacyTouchedIndex >= legacyCleanupIndex || legacyCleanupIndex >= stopIndex || stopIndex >= startIndex ||
		startIndex >= persistIndex || persistIndex >= reloadIndex || reloadIndex >= propagateIndex ||
		propagateIndex >= commitIndex {
		t.Fatalf("start-shell main flow must snapshot, prepare, clean legacy End4, stop, start, persist, reload, propagate, then commit")
	}
}

func TestShellProfileSyncDoesNotMutateHomeManagerEnd4OrTopLevelLinks(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/scripts/shell-profile-sync.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{
		`ln -s -- "$source" "$target"`,
		`rm -rf -- "$target"`,
		`$dir/monitors.conf`,
		`$dir/workspaces.conf`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("shell profile sync contains forbidden End4 mutation %q\n%s", forbidden, text)
		}
	}
	for _, want := range []string{
		"validate_end4_profile_tree()",
		"end4 profile path is not a Home Manager symlink",
		`[ -L "$source" ] || return 1`,
		`^[0-9a-df-np-sv-z]{32}-home-manager-files$`,
		"-- Wahrwelt shell adapter: $profile",
		`require(\"$adapter\").load({ profile = \"$profile\", quickshell_config = \"$quickshell_path\" })`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("shell profile sync missing contract %q\n%s", want, text)
		}
	}
}

func TestRestoreLockScriptStartsProfileBeforeLocking(t *testing.T) {
	data, err := os.ReadFile("../../../dots/hypr/scripts/restore-lock.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"shell-runtime.sh",
		"wahrwelt_valid_shell_profile",
		`"$script_dir/start-shell.sh" "$profile"`,
		"wait_for_profile()",
		`hyprctl dispatch 'hl.dsp.global("caelestia:lock")'`,
		"wahrwelt_noctalia_action lock",
		`exec "$script_dir/lock-active.sh" "$profile"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("restore-lock script missing %q\n%s", want, text)
		}
	}
}

func TestManagedEnd4LockHelperUsesExactProfileMarker(t *testing.T) {
	helperData, err := os.ReadFile("../../../dots/hypr/scripts/lock-active.sh")
	if err != nil {
		t.Fatal(err)
	}
	helper := string(helperData)
	for _, want := range []string{
		"shell-runtime.sh",
		"wahrwelt_read_active_shell",
		"wahrwelt_valid_end4_variant",
		"wahrwelt_end4_profile_running",
		`hyprctl dispatch 'hl.dsp.global("quickshell:lock")'`,
		`exec hyprlock -c "$wahrwelt_hypr_runtime_dir/hyprlock.conf"`,
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("managed lock helper missing %q\n%s", want, helper)
		}
	}
	if strings.Contains(helper, "pidof") {
		t.Fatalf("managed lock helper must not use generic process names\n%s", helper)
	}

	patch := readTestFile(t, "../../../NixOS/home/end4/patches/hypr.nix")
	for _, want := range []string{
		`$lock_cmd = ${lib.escapeShellArg "${config.xdg.configHome}/hypr/scripts/lock-active.sh"}`,
		`after_sleep_cmd = hyprctl dispatch 'hl.dsp.global("quickshell:lockFocus")'`,
		"retained generic process-name lock fallback",
	} {
		if !strings.Contains(patch, want) {
			t.Fatalf("End4 hypridle contract missing %q\n%s", want, patch)
		}
	}
	if strings.Contains(patch, "pidof qs quickshell hyprlock") {
		t.Fatalf("End4 hypridle retained generic pidof fallback\n%s", patch)
	}
}

func TestEnd4HyprPatchDisablesUpstreamShellLifecycle(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/home/end4/patches/hypr.nix")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"Wahrwelt start-shell owns end4 QuickShell",
		"Wahrwelt start-shell owns end4 hypridle",
		"managed lock helper",
		`/^    hl\.exec_cmd("qs -c \$qsConfig")$/`,
		`/^    hl\.exec_cmd("hypridle")$/`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("end4 dotfile patch missing lifecycle guard %q\n%s", want, text)
		}
	}
}

func TestEnd4QuickshellPatchUsesNixOSLogo(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/home/end4/patches/quickshell.nix")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `Quickshell.iconPath("nix-snowflake")`) {
		t.Fatalf("end4 quickshell patch missing NixOS logo replacement\n%s", text)
	}
}

func TestShellBrandingUsesNixOSLogoOutsideSelector(t *testing.T) {
	caelestia, err := os.ReadFile("../../../NixOS/home/caelestia/general.nix")
	if err != nil {
		t.Fatal(err)
	}
	selector, err := os.ReadFile("../../../NixOS/home/shells/quickshell/wahrwelt-shell-selector/shell.qml")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`logo = "nix-snowflake";`,
	} {
		if !strings.Contains(string(caelestia), want) {
			t.Fatalf("caelestia branding missing %q\n%s", want, string(caelestia))
		}
	}
	for _, want := range []string{
		`assets/noctalia.svg`,
		`assets/caelestia.svg`,
		`assets/illogical-impulse.svg`,
	} {
		if !strings.Contains(string(selector), want) {
			t.Fatalf("shell selector must keep upstream logos; missing %q\n%s", want, string(selector))
		}
	}
}

func TestCaelestiaActivationSeedsShellJSONWithoutReplacingUserFiles(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/home/caelestia/default.nix")
	if err != nil {
		t.Fatal(err)
	}
	helpers, err := os.ReadFile("../../../NixOS/home/lib/dotfiles.nix")
	if err != nil {
		t.Fatal(err)
	}
	seedHelper, err := os.ReadFile("../../../NixOS/home/lib/mutable-seed.py")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + string(helpers) + string(seedHelper)
	for _, want := range []string{
		`seed_json_object "$HOME/.config/caelestia/shell.json"`,
		`seed-json-object "$HOME" "$target" "$source"`,
		`os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK`,
		`existing JSON requires a managed transform and was preserved`,
		`publish_file_if_absent(`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("caelestia activation missing create-if-absent shell.json ownership contract %q\n%s", want, text)
		}
	}
}

func TestCaelestiaActivationPreservesDisabledTransparency(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/home/caelestia/default.nix")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	if strings.Contains(text, `.appearance.transparency.enabled //= true`) {
		t.Fatalf("caelestia transparency seed must not treat false as missing\n%s", text)
	}
	for _, want := range []string{
		`${dotfilesLib.mkBoolDefault ".appearance.transparency.enabled" true} |`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("caelestia activation missing transparency false-preserving seed %q\n%s", want, text)
		}
	}
}

func TestEnd4ActivationPreservesDisabledTransparency(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/home/end4/seed/config.nix")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	if strings.Contains(text, `.appearance.transparency.enable //= true`) {
		t.Fatalf("end4 transparency seed must not treat false as missing\n%s", text)
	}
	for _, want := range []string{
		`${dotfilesLib.mkBoolDefault ".appearance.transparency.enable" true} |`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("end4 activation missing transparency false-preserving seed %q\n%s", want, text)
		}
	}
}

func TestShellSeedFiltersDoNotUseJQTrueFallback(t *testing.T) {
	for _, path := range []string{
		"../../../NixOS/home/caelestia/default.nix",
		"../../../NixOS/home/end4/seed/config.nix",
		"../../../NixOS/home/noctalia/default.nix",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `//= true`) {
			t.Fatalf("seed filters must use mkBoolDefault for true boolean defaults, found //= true in %s\n%s", path, string(data))
		}
	}
}

func TestWindowOpacityIsNotMultipliedByFoot(t *testing.T) {
	footData, err := os.ReadFile("../../../NixOS/home/programs/foot.nix")
	if err != nil {
		t.Fatal(err)
	}
	rulesData, err := os.ReadFile("../../../dots/hypr/hyprland/rules.lua")
	if err != nil {
		t.Fatal(err)
	}

	foot := string(footData)
	rules := string(rulesData)
	if !strings.Contains(foot, `alpha = lib.mkForce 1.0;`) {
		t.Fatalf("Foot must leave window opacity to Hyprland instead of multiplying it\n%s", foot)
	}
	if !strings.Contains(rules, `opacity = tostring(v.windowOpacity) .. " override"`) {
		t.Fatalf("Hyprland global window opacity rule is missing\n%s", rules)
	}
	opaqueFootRule := regexp.MustCompile(`class = "[^"]*\bfoot\b[^"]*".*opaque = true`)
	if opaqueFootRule.MatchString(rules) {
		t.Fatalf("Foot must use the same blur and opacity path as other windows\n%s", rules)
	}
}

func TestObservabilityGrafanaUsesPersistentSecretFile(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/services/observability.nix")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile("../../../NixOS/services/grafana-secret-key.py")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + string(helper)
	for _, want := range []string{
		`grafanaSecretDirectory = "/var/lib/wahrwelt/grafana";`,
		`grafanaSecretKeyPath = "${grafanaSecretDirectory}/secret_key";`,
		`pkgs.writeText "wahrwelt-grafana-secret-key.py"`,
		`os.O_RDWR | os.O_TMPFILE | os.O_CLOEXEC`,
		`os.urandom(32).hex().encode("ascii")`,
		`security.secret_key = "$__file{${grafanaSecretKeyPath}}";`,
		`before = [ "grafana.service" ];`,
		`requiredBy = [ "grafana.service" ];`,
		`Type = "oneshot";`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("observability Grafana config missing persistent secret contract %q\n%s", want, text)
		}
	}
}

func TestInstallerPackageProvidesXKeyboardRules(t *testing.T) {
	data, err := os.ReadFile("../../../NixOS/lib/flake-packages.nix")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`--set WAHRWELT_XKB_RULES_DIR ${flakePkgs.xkeyboard_config}/share/X11/xkb/rules`,
		`--set WAHRWELT_XKB_RULES_DIR ${flakePkgs.xkeyboard_config}/share/X11/xkb/rules`,
		`xkeyboard_config`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("wahrwelt package missing XKB rules runtime contract %q\n%s", want, text)
		}
	}
}

func TestWsactionGroupModeShiftsFishArgv(t *testing.T) {
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("fish is not installed")
	}
	bin := t.TempDir()
	callFile := filepath.Join(t.TempDir(), "hyprctl-call")
	writeScript(t, filepath.Join(bin, "hyprctl"), `if [ "$1" = "activeworkspace" ]; then
  printf '{"id":%s}\n' "$ACTIVE_WS"
  exit 0
fi
printf '%s\n' "$*" > "$CALL_FILE"
`)
	writeScript(t, filepath.Join(bin, "jq"), `printf '%s\n' "$ACTIVE_WS"`)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("CALL_FILE", callFile)

	script, err := filepath.Abs("../../../dots/hypr/scripts/wsaction.fish")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"10": `dispatch hl.dsp.focus({ workspace = "10" })`,
		"20": `dispatch hl.dsp.focus({ workspace = "10" })`,
		"23": `dispatch hl.dsp.focus({ workspace = "3" })`,
	}
	for activeWS, want := range cases {
		t.Run(activeWS, func(t *testing.T) {
			t.Setenv("ACTIVE_WS", activeWS)
			if err := os.Remove(callFile); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}

			cmd := exec.Command("fish", "--no-config", script, "-g", "workspace", "1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("wsaction.fish failed: %v\n%s", err, string(output))
			}
			data, err := os.ReadFile(callFile)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(data)); got != want {
				t.Fatalf("unexpected hyprctl dispatch call: got %q want %q", got, want)
			}
		})
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertNoUnexpectedDuplicateHyprBinds(t *testing.T, rel string, allowed map[string]bool) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../../dots/hypr", rel))
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]int{}
	bindRe := regexp.MustCompile(`^(?:wahrwelt\.)?(?:bind_[a-z]+|bind)\("([^"]+)"`)
	for lineNo, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		match := bindRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		key := strings.Join(strings.Fields(match[1]), "")
		allowKey := rel + "|" + key
		if previous, ok := seen[key]; ok && !allowed[allowKey] {
			t.Fatalf("unexpected duplicate Hypr bind in %s: line %d duplicates line %d for %s\n%s", rel, lineNo+1, previous, key, string(data))
		}
		seen[key] = lineNo + 1
	}
}
