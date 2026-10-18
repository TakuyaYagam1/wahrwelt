package apply

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
)

func testFlakeTemplate(t *testing.T, state config.State) string {
	t.Helper()
	text, err := FlakeNix(state, LockModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func testFlakePlan(t *testing.T, text string, state config.State, previous []byte) FlakePlan {
	t.Helper()
	plan, err := PlanFlake(text, state, previous)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPlanFlakeMigratesZenWithoutLosingUserSource(t *testing.T) {
	state := config.Default()
	text := testFlakeTemplate(t, state)
	text = strings.Replace(text, "github:0xc000022070/zen-browser-flake", "github:youwen5/zen-browser-flake", 1)
	text = strings.Replace(text, "      inputs.home-manager.follows = \"home-manager\";\n", "", 1)
	customInput := "    private = {\n      url = \"git+ssh://example.test/private\"; # user input\n      inputs.nixpkgs.follows = \"nixpkgs\";\n    };\n"
	text = strings.Replace(text, "  inputs = {\n", "  inputs = {\n"+customInput, 1)
	customModules := `extraModules = [ ./configuration.nix inputs.private.nixosModules.default ]; # keep this`
	text = strings.Replace(text, `extraModules = [ ./configuration.nix ];`, customModules, 1)
	customOutput := `      packages.x86_64-linux.note = ''
# preset: desktop
mysetup ${"literal ${"nested"}"}
'';
`
	text = strings.Replace(text, "      nixosConfigurations.", customOutput+"      nixosConfigurations.", 1)
	plan := testFlakePlan(t, text, state, nil)
	for _, source := range []string{customInput, customModules, customOutput} {
		if !strings.Contains(plan.Content, source) {
			t.Fatalf("custom source changed: %q\n%s", source, plan.Content)
		}
	}
	doc, err := parseFlake(plan.Content)
	if err != nil {
		t.Fatal(err)
	}
	values := doc.values()
	if values["inputs.zen-browser.url"] != `"github:0xc000022070/zen-browser-flake"` || values["inputs.zen-browser.inputs.home-manager.follows"] != `"home-manager"` {
		t.Fatalf("Zen input was not migrated: %#v", values)
	}
	if strings.Contains(string(plan.State), "example.test") || strings.Contains(string(plan.State), "inputs.private") {
		t.Fatal("baseline must only contain template values, not private user source")
	}
	second := testFlakePlan(t, plan.Content, state, plan.State)
	if second.Content != plan.Content || len(second.Changes) != 0 || string(second.State) != string(plan.State) {
		t.Fatal("repeated reconciliation must be byte-identical")
	}
}

func TestPlanFlakePreservesUserOverridesAndManualDeletion(t *testing.T) {
	state := config.Default()
	first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
	text := strings.Replace(first.Content, `"github:NixOS/nixpkgs/nixos-unstable"`, `"github:example/nixpkgs/pin"`, 1)
	text = strings.Replace(text, "      inputs.home-manager.follows = \"home-manager\";\n", "", 1)
	second := testFlakePlan(t, text, state, first.State)
	if second.Content != text || len(second.Changes) != 0 {
		t.Fatal("unchanged upstream fields must preserve manual changes and deletion")
	}
	third := testFlakePlan(t, second.Content, state, second.State)
	if third.Content != text {
		t.Fatal("manual deletion was lost on a later run")
	}
}

func TestPlanFlakePreservesPinnedWahrweltURL(t *testing.T) {
	state := config.Default()
	text := testFlakeTemplate(t, state)
	pinned := "github:example/wahrwelt/abcdef?dir=Linux/NixOS/presets/personal"
	text = strings.Replace(text, config.WahrweltPresetFlakeURL(state.Source.Channel, state.Packages.Preset), pinned, 1)
	plan := testFlakePlan(t, text, state, nil)
	if plan.Content != text {
		t.Fatal("custom Wahrwelt pin changed while adopting legacy wrapper")
	}
	url, err := CurrentWahrweltURL(plan.Content)
	if err != nil || url != pinned {
		t.Fatalf("source extraction: got %q, %v", url, err)
	}
}

func TestPlanFlakePreservesManuallySelectedKnownChannel(t *testing.T) {
	state := config.Default()
	text := testFlakeTemplate(t, state)
	text = strings.Replace(text, config.WahrweltPresetFlakeURL(state.Source.Channel, state.Packages.Preset), config.WahrweltPresetFlakeURL(config.SourceChannelDevelopment, state.Packages.Preset), 1)
	plan := testFlakePlan(t, text, state, nil)
	if plan.Content != text {
		t.Fatal("first adoption must preserve the user's known channel override")
	}
}

func TestPlanFlakeUpdateKeepsHostSource(t *testing.T) {
	state := config.Default()
	first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
	for _, source := range []string{
		config.WahrweltPresetFlakeURL(config.SourceChannelDevelopment, state.Packages.Preset),
		"github:TakuyaYagam1/wahrwelt/dev?dir=Linux/NixOS",
		"github:example/private-config/abcdef?dir=Linux/NixOS/presets/personal",
	} {
		text := strings.Replace(first.Content, config.WahrweltPresetFlakeURL(state.Source.Channel, state.Packages.Preset), source, 1)
		for _, previous := range [][]byte{nil, first.State} {
			plan, err := PlanFlakeUpdate(text, state, previous)
			if err != nil {
				t.Fatal(err)
			}
			url, err := CurrentWahrweltURL(plan.Content)
			if err != nil || url != source {
				t.Fatalf("update changed %q to %q: %v", source, url, err)
			}
			if strings.Contains(string(plan.State), source) {
				t.Fatalf("custom source must not become framework-owned baseline: %s", plan.State)
			}
		}
	}
	state.Source.Channel = config.SourceChannelDevelopment
	plan, err := PlanFlakeUpdate(first.Content, state, first.State)
	if err != nil {
		t.Fatal(err)
	}
	url, err := CurrentWahrweltURL(plan.Content)
	if err != nil || url != config.WahrweltPresetFlakeURL(config.SourceChannelStable, state.Packages.Preset) {
		t.Fatalf("ordinary update followed state channel over host source: %q, %v", url, err)
	}
}

func TestPlanFlakeRejectsConflictingChanges(t *testing.T) {
	state := config.Default()
	first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
	current := strings.Replace(first.Content, `extraModules = [ ./configuration.nix ];`, `extraModules = [ ./mine.nix ];`, 1)
	var baseline flakeBaseline
	if err := json.Unmarshal(first.State, &baseline); err != nil {
		t.Fatal(err)
	}
	baseline.Fields["host.extraModules"] = `[ ./previous.nix ]`
	previous, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PlanFlake(current, state, previous); err == nil || !strings.Contains(err.Error(), "host.extraModules") {
		t.Fatalf("wanted actionable conflict, got %v", err)
	}
}

func TestPlanFlakeRejectsNewInputNameCollision(t *testing.T) {
	state := config.Default()
	first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
	current := strings.Replace(first.Content, "  inputs = {", `  inputs = {
    lanzaboote.url = "github:example/custom-boot";`, 1)
	state.Features.SecureBoot = true
	if _, err := PlanFlake(current, state, first.State); err == nil || !strings.Contains(err.Error(), "inputs.lanzaboote.url") {
		t.Fatalf("wanted input name collision, got %v", err)
	}
}

func TestPlanFlakeRetainsInputUsedByUser(t *testing.T) {
	for name, fragment := range map[string]string{
		"output":             `packages.x86_64-linux.browser = inputs.zen-browser.packages.x86_64-linux.default;`,
		"custom follows":     `inputs.private = { url = "github:example/private"; inputs.zen.follows = "zen-browser"; };`,
		"nested follows":     `inputs.private = { url = "github:example/private"; inputs.zen.follows = "zen-browser/nixpkgs"; };`,
		"indented follows":   `inputs.private = { url = "github:example/private"; inputs.zen.follows = ''zen-browser''; };`,
		"custom input field": `inputs.zen-browser.inputs.private.follows = "private";`,
		"dynamic lookup":     `packages.x86_64-linux.browser = inputs.${(import ./choice.nix)};`,
		"passed inputs":      `packages.x86_64-linux.browser = (import ./build.nix) inputs;`,
		"empty user set":     `inputs.zen-browser.inputs.user = {};`,
	} {
		t.Run(name, func(t *testing.T) {
			state := config.Default()
			first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
			current := strings.Replace(first.Content, "  description =", "  "+fragment+"\n  description =", 1)
			if strings.HasPrefix(fragment, "packages.") {
				current = strings.Replace(first.Content, "      nixosConfigurations.", "      "+fragment+"\n      nixosConfigurations.", 1)
			}
			state.Packages.Preset = "minimal"
			plan := testFlakePlan(t, current, state, first.State)
			if !strings.Contains(plan.Content, `url = "github:0xc000022070/zen-browser-flake"`) || !strings.Contains(plan.Content, fragment) {
				t.Fatalf("input needed by user was removed:\n%s", plan.Content)
			}
			second := testFlakePlan(t, plan.Content, state, plan.State)
			if second.Content != plan.Content {
				t.Fatal("retained user input changed on the next run")
			}
		})
	}
}

func TestPlanFlakeRemovesOnlyUnchangedOwnedInputs(t *testing.T) {
	state := config.Default()
	first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
	state.Packages.Preset = "minimal"
	second := testFlakePlan(t, first.Content, state, first.State)
	if strings.Contains(second.Content, "zen-browser") {
		t.Fatal("unreferenced owned input should have been removed")
	}
	state.Packages.Preset = "desktop"
	third := testFlakePlan(t, second.Content, state, second.State)
	if !strings.Contains(third.Content, "zen-browser") {
		t.Fatal("later release must be able to add the input back")
	}
	fourth := testFlakePlan(t, third.Content, state, third.State)
	if fourth.Content != third.Content {
		t.Fatal("multi-version progression was not idempotent")
	}
}

func TestPlanFlakeRejectsUnknownOrDamagedBaseline(t *testing.T) {
	state := config.Default()
	text := testFlakeTemplate(t, state)
	for _, previous := range []string{
		`{"version":2,"fields":{}}`, `{"version":1}`, `{"version":1,"fields":{},"private":true}`,
		`{"version":1,"fields":{}} {}`, `{"version":1,"fields":{"outputs":"bad"}}`,
		`{"version":1,"version":1,"fields":{}}`,
		`{"version":1,"fields":{"inputs.a.url":"one","inputs.a.url":"two"}}`,
	} {
		if _, err := PlanFlake(text, state, []byte(previous)); err == nil {
			t.Fatalf("accepted unsupported baseline %s", previous)
		}
	}
}

func TestPlanFlakeRejectsUnsupportedManagedSyntax(t *testing.T) {
	for _, text := range []string{
		`{ inputs = import ./inputs.nix; }`,
		`{ inputs = { inherit wahrwelt; }; }`,
		`{ inputs.wahrwelt.url = "one"; inputs.wahrwelt.url = "two"; }`,
		`{ inputs.wahrwelt = { url = "one"; }; inputs.wahrwelt.url = "two"; }`,
		`{ inputs.wahrwelt.url = "one"; inputs.wahrwelt.url.extra = "two"; }`,
		`{ inputs = { ${name}.url = "one"; }; }`,
		`{ outputs = { wahrwelt, ... }: { host = wahrwelt.lib.mkWahrweltHost args; }; }`,
		`{ outputs = { wahrwelt, ... }: { host = wahrwelt.lib.mkWahrweltHost { inherit extraModules; }; }; }`,
		`{ inputs.wahrwelt.url = "unterminated; }`,
	} {
		if _, err := PlanFlake(text, config.Default(), nil); err == nil {
			t.Fatalf("accepted unsupported managed syntax: %s", text)
		}
	}
}

func TestPlanFlakePreservesComplexHostExpression(t *testing.T) {
	state := config.Default()
	text := testFlakeTemplate(t, state)
	value := `extraModules = with inputs; let custom = { text = ''literal ''${thing}''; }; in [ ./configuration.nix ];`
	text = strings.Replace(text, `extraModules = [ ./configuration.nix ];`, value, 1)
	plan := testFlakePlan(t, text, state, nil)
	if plan.Content != text {
		t.Fatal("custom host expression was rewritten")
	}
}

func TestCurrentWahrweltURLRejectsComputedAndAmbiguousSources(t *testing.T) {
	for _, text := range []string{
		`{ inputs.wahrwelt.url = base + "/main"; }`,
		`{ inputs.wahrwelt.url = "github:${owner}/wahrwelt"; }`,
		`{ inputs.wahrwelt.url = "one"; inputs.mysetup.url = "two"; }`,
		`{ inputs.wahrwelt.follows = "another"; }`,
	} {
		if _, err := CurrentWahrweltURL(text); err == nil {
			t.Fatalf("accepted ambiguous source: %s", text)
		}
	}
}

func TestPlanFlakeUpdatesLiteralHostname(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		state := config.Default()
		state.Host.Hostname = "before"
		first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
		var previous []byte
		if tracked {
			previous = first.State
		}
		state.Host.Hostname = "after"
		plan := testFlakePlan(t, first.Content, state, previous)
		if !strings.Contains(plan.Content, `hostname = "after";`) {
			t.Fatalf("hostname was not updated (tracked=%t)", tracked)
		}
	}
}

func TestPlanFlakePreservesDynamicHostnameUntilConflict(t *testing.T) {
	state := config.Default()
	text := strings.Replace(testFlakeTemplate(t, state), `hostname = "NixOS";`, `hostname = (import ./host-vars.nix).host.hostname;`, 1)
	first := testFlakePlan(t, text, state, nil)
	if first.Content != text {
		t.Fatal("dynamic hostname expression should be preserved")
	}
	state.Host.Hostname = "changed"
	if _, err := PlanFlake(first.Content, state, first.State); err == nil || !strings.Contains(err.Error(), "binding.hostname") {
		t.Fatalf("wanted a hostname conflict, got %v", err)
	}
}

func TestPlanFlakeDoesNotRenameUnrelatedLegacyNames(t *testing.T) {
	text := `{
  inputs.mysetup.url = "github:TakuyaYagam1/MySetup?dir=Linux/NixOS";
  outputs = { mysetup, ... }: {
    unrelated.mysetup.value = true;
    nixosConfigurations.host = mysetup.lib.mkMySetupHost {};
  };
}`
	if _, err := PlanFlake(text, config.Default(), nil); err == nil || !strings.Contains(err.Error(), "mysetup") {
		t.Fatalf("ambiguous user-owned legacy name must fail rather than be renamed: %v", err)
	}
}

func TestPlanFlakeRenamesOnlyLegacyFollowReferences(t *testing.T) {
	text := `{
  inputs.mysetup.url = "github:TakuyaYagam1/MySetup?dir=Linux/NixOS";
  inputs.private = {
    url = "github:example/private";
    inputs.framework.follows = "mysetup";
    inputs.nixpkgs.follows = "mysetup/nixpkgs";
  };
  outputs = { mysetup, ... }: {
    note = "mysetup";
    nixosConfigurations.host = mysetup.lib.mkMySetupHost {};
  };
}`
	plan := testFlakePlan(t, text, config.Default(), nil)
	for _, want := range []string{`inputs.framework.follows = "wahrwelt";`, `inputs.nixpkgs.follows = "wahrwelt/nixpkgs";`, `note = "mysetup";`} {
		if !strings.Contains(plan.Content, want) {
			t.Fatalf("missing preserved or migrated value %q", want)
		}
	}
}

func TestFlakeMetadataUsesOnlyHeaderComments(t *testing.T) {
	state := config.Default()
	text := testFlakeTemplate(t, state)
	text = strings.Replace(text, "  inputs = {", "  /*\n# lock mode: managed\n# preset: minimal\n*/\n  inputs = {", 1)
	text = strings.Replace(text, "      nixosConfigurations.", "      note = ''\n# lock mode: managed\n# preset: minimal\n'';\n      nixosConfigurations.", 1)
	first := testFlakePlan(t, text, state, nil)
	second := testFlakePlan(t, first.Content, state, first.State)
	if first.Content != text || second.Content != text {
		t.Fatal("comment-like user text changed the reconciliation metadata")
	}
	mode, err := CurrentLockMode(text)
	if err != nil || mode != LockModeIndependent {
		t.Fatalf("unexpected lock mode %q, %v", mode, err)
	}
	conflicting := strings.Replace(text, "  # lock mode: independent", "  # lock mode: independent\n  # lock mode: managed", 1)
	if _, err := CurrentLockMode(conflicting); err == nil {
		t.Fatal("conflicting actual header comments must be rejected")
	}
}

func TestPlanFlakeRejectsEmptyNewInputCollision(t *testing.T) {
	state := config.Default()
	first := testFlakePlan(t, testFlakeTemplate(t, state), state, nil)
	text := strings.Replace(first.Content, "  inputs = {", "  inputs = {\n    lanzaboote = {};", 1)
	state.Features.SecureBoot = true
	if _, err := PlanFlake(text, state, first.State); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected empty user input collision, got %v", err)
	}
}

func TestPlanFlakeProducesParsableNix(t *testing.T) {
	nix, err := exec.LookPath("nix-instantiate")
	if err != nil {
		t.Skip("nix-instantiate is unavailable")
	}
	for _, mode := range []LockMode{LockModeIndependent, LockModeManaged} {
		state := config.Default()
		text, err := FlakeNix(state, mode)
		if err != nil {
			t.Fatal(err)
		}
		var previous []byte
		for _, preset := range []string{"minimal", "desktop", "developer", "personal"} {
			state.Packages.Preset = preset
			state.Features.SecureBoot = preset != "minimal"
			plan := testFlakePlan(t, text, state, previous)
			command := exec.CommandContext(t.Context(), nix, "--parse", "-")
			command.Stdin = bytes.NewBufferString(plan.Content)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("Nix rejected %s/%s plan: %v\n%s\n%s", mode, preset, err, output, plan.Content)
			}
			text, previous = plan.Content, plan.State
		}
	}
}
