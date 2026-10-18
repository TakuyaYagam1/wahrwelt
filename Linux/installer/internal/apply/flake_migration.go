package apply

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
)

// FlakeStateName stores the previous framework values, never user source text.
const FlakeStateName = "wahrwelt-flake.json"

// FlakePlan contains a proposed source file and its next reconciliation baseline.
type FlakePlan struct {
	Content string
	State   []byte
	Changes []string
}

type flakeBaseline struct {
	Version int               `json:"version"`
	Fields  map[string]string `json:"fields"`
}

// PlanFlake updates framework fields while preserving independent user edits.
// All work is in memory. Callers must validate the plan before committing it.
func PlanFlake(text string, state config.State, previous []byte) (FlakePlan, error) {
	return planFlake(text, state, previous, false)
}

// PlanFlakeUpdate keeps the host's selected source while refreshing managed
// fields. Choosing a different channel or preset source belongs to reconfigure.
func PlanFlakeUpdate(text string, state config.State, previous []byte) (FlakePlan, error) {
	if _, err := CurrentWahrweltURL(text); err != nil {
		return FlakePlan{}, err
	}
	return planFlake(text, state, previous, true)
}

func planFlake(text string, state config.State, previous []byte, keepSource bool) (FlakePlan, error) {
	original := text
	if len(previous) == 0 {
		var err error
		text, err = migrateFlakeBrand(text)
		if err != nil {
			return FlakePlan{}, err
		}
	}
	current, err := parseFlake(text)
	if err != nil {
		return FlakePlan{}, err
	}
	if err := validateFlakePaths(current); err != nil {
		return FlakePlan{}, err
	}
	lockMode, err := CurrentLockMode(text)
	if err != nil {
		return FlakePlan{}, err
	}
	generated, err := FlakeNix(state, lockMode)
	if err != nil {
		return FlakePlan{}, err
	}
	desired, err := parseFlake(generated)
	if err != nil {
		return FlakePlan{}, fmt.Errorf("framework flake template: %w", err)
	}
	baseline, err := readFlakeBaseline(previous)
	if err != nil {
		return FlakePlan{}, err
	}
	if len(previous) == 0 {
		baseline, err = legacyFlakeBaseline(current, state, lockMode)
		if err != nil {
			return FlakePlan{}, err
		}
	}
	plan, err := finishFlakePlan(current, desired, baseline, state, keepSource)
	if err == nil && original != text {
		plan.Changes = append([]string{"rename MySetup wrapper to Wahrwelt"}, plan.Changes...)
	}
	return plan, err
}

func finishFlakePlan(current, desired *flakeDocument, baseline flakeBaseline, state config.State, keepSource bool) (FlakePlan, error) {
	next := desired.values()
	for key := range next {
		if strings.HasPrefix(key, "binding.") && current.leaves[key] == nil {
			delete(next, key)
		}
	}
	if current.sets["host"] == nil {
		for key := range next {
			if strings.HasPrefix(key, "host.") {
				delete(next, key)
			}
		}
	}
	if err := checkFlakeInputCollisions(current, baseline.Fields, next); err != nil {
		return FlakePlan{}, err
	}
	changes, err := mergeFlakePlanValues(current.values(), baseline.Fields, next, keepSource)
	if err != nil {
		return FlakePlan{}, err
	}
	retainReferencedInputs(current, changes)
	edits, labels, err := current.planEdits(changes)
	if err != nil {
		return FlakePlan{}, err
	}
	result, err := applyFlakeEdits(current.text, edits)
	if err != nil {
		return FlakePlan{}, err
	}
	result = updateFlakeComments(result, state)
	if _, err := parseFlake(result); err != nil {
		return FlakePlan{}, fmt.Errorf("planned flake failed syntax checks: %w", err)
	}
	encoded, err := json.MarshalIndent(flakeBaseline{Version: 1, Fields: next}, "", "  ")
	if err != nil {
		return FlakePlan{}, err
	}
	return FlakePlan{Content: result, State: append(encoded, '\n'), Changes: labels}, nil
}

func mergeFlakePlanValues(current, baseline, desired map[string]string, keepSource bool) (map[string]*string, error) {
	if !keepSource {
		return mergeFlakeValues(current, baseline, desired)
	}
	// Excluding source from both sides prevents updates from adopting a custom
	// pin as framework-owned baseline data or switching the selected channel.
	old, next := make(map[string]string, len(baseline)), make(map[string]string, len(desired))
	for path, value := range baseline {
		if path != "inputs.wahrwelt.url" {
			old[path] = value
		}
	}
	for path, value := range desired {
		if path != "inputs.wahrwelt.url" {
			next[path] = value
		}
	}
	return mergeFlakeValues(current, old, next)
}

func checkFlakeInputCollisions(current *flakeDocument, baseline, desired map[string]string) error {
	for path := range desired {
		name := inputName(path)
		if name == "" || path != "inputs."+name+".url" || current.leaves[path] != nil {
			continue
		}
		prefix := "inputs." + name + "."
		known := false
		for field := range baseline {
			if strings.HasPrefix(field, prefix) {
				known = true
			}
		}
		if !known && current.sets["inputs."+name] != nil {
			return fmt.Errorf("flake.nix conflict at inputs.%s: a new framework input collides with an existing user set", name)
		}
	}
	return nil
}

// CurrentWahrweltURL reads the host's source choice without evaluating its flake.
func CurrentWahrweltURL(text string) (string, error) {
	doc, err := parseFlake(text)
	if err != nil {
		return "", err
	}
	if err := validateFlakePaths(doc); err != nil {
		return "", err
	}
	var url string
	for _, name := range []string{"wahrwelt", "mysetup"} {
		binding := doc.leaves["inputs."+name+".url"]
		if binding == nil {
			continue
		}
		value, err := strconv.Unquote(doc.text[binding.valueStart:binding.valueEnd])
		if err != nil || strings.Contains(value, "${") || value == "" || url != "" {
			return "", fmt.Errorf("wahrwelt input must have one literal URL; check inputs.%s.url", name)
		}
		url = value
	}
	if url == "" {
		return "", fmt.Errorf("flake.nix has no literal inputs.wahrwelt.url")
	}
	return url, nil
}

func migrateGeneratedThinFlake(text string, state config.State) (string, bool, error) {
	plan, err := PlanFlake(text, state, nil)
	return plan.Content, plan.Content != text, err
}

func readFlakeBaseline(data []byte) (flakeBaseline, error) {
	baseline := flakeBaseline{}
	if len(data) == 0 {
		return flakeBaseline{Version: 1, Fields: map[string]string{}}, nil
	}
	if len(data) > 2*1024*1024 {
		return baseline, fmt.Errorf("%s exceeds the size limit", FlakeStateName)
	}
	if err := jsonv2.Unmarshal(data, &baseline, jsonv2.RejectUnknownMembers(true)); err != nil {
		return baseline, fmt.Errorf("read %s: %w", FlakeStateName, err)
	}
	if baseline.Version != 1 || baseline.Fields == nil {
		return baseline, fmt.Errorf("unsupported %s version %d", FlakeStateName, baseline.Version)
	}
	for path, value := range baseline.Fields {
		if !validFlakeBaselinePath(path) || strings.TrimSpace(value) == "" {
			return baseline, fmt.Errorf("invalid managed field %q in %s", path, FlakeStateName)
		}
		if _, err := scanFlake(value); err != nil {
			return baseline, fmt.Errorf("invalid value for %s in %s", path, FlakeStateName)
		}
	}
	return baseline, nil
}

func validFlakeBaselinePath(path string) bool {
	if path == "description" {
		return true
	}
	parts := strings.Split(path, ".")
	if len(parts) < 2 || (parts[0] != "inputs" && parts[0] != "host" && parts[0] != "binding") {
		return false
	}
	for _, part := range parts[1:] {
		if nixIdentifierPattern.FindString(part) != part || part == "" {
			return false
		}
	}
	return true
}

// Missing map keys represent deletion, so manual removals survive repeat runs.
func mergeFlakeValues(current, baseline, desired map[string]string) (map[string]*string, error) {
	changes := map[string]*string{}
	keys := make(map[string]bool, len(baseline)+len(desired))
	for key := range baseline {
		keys[key] = true
	}
	for key := range desired {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		old, oldOK := baseline[key]
		now, nowOK := current[key]
		want, wantOK := desired[key]
		upstreamSame := oldOK == wantOK && (!oldOK || flakeValueEqual(old, want))
		currentSame := nowOK == wantOK && (!nowOK || flakeValueEqual(now, want))
		if upstreamSame || currentSame {
			continue
		}
		if oldOK != nowOK || (oldOK && !flakeValueEqual(old, now)) {
			return nil, fmt.Errorf("flake.nix conflict at %s: both your value and Wahrwelt changed; reconcile this field before updating", key)
		}
		if wantOK {
			changes[key] = &want
		} else {
			changes[key] = nil
		}
	}
	return changes, nil
}

func legacyFlakeBaseline(current *flakeDocument, state config.State, mode LockMode) (flakeBaseline, error) {
	oldState := state
	if binding := current.leaves["binding.hostname"]; binding != nil {
		if value, err := strconv.Unquote(current.text[binding.valueStart:binding.valueEnd]); err == nil && !strings.Contains(value, "${") {
			oldState.Host.Hostname = value
		}
	}
	metadata, err := flakeMetadata(current.text)
	if err != nil {
		return flakeBaseline{}, err
	}
	if value := metadata["preset"]; value != "" {
		oldState.Packages.Preset = value
	}
	oldState.Features.SecureBoot = current.leaves["inputs.lanzaboote.url"] != nil
	template, err := FlakeNix(oldState, mode)
	if err != nil {
		return flakeBaseline{}, err
	}
	previous, err := parseFlake(template)
	if err != nil {
		return flakeBaseline{}, err
	}
	fields := previous.values()
	actual := current.values()
	for key := range fields {
		if _, exists := actual[key]; !exists {
			delete(fields, key)
		}
	}
	known := map[string][]string{
		"description":               {"Host-local MySetup NixOS wrapper", "Host-local Wahrwelt NixOS wrapper"},
		"inputs.wahrwelt.url":       legacyWahrweltURLs(),
		"inputs.zen-browser.url":    {"github:youwen5/zen-browser-flake"},
		"inputs.nixpkgs-stable.url": {"github:NixOS/nixpkgs/nixos-25.11", "github:NixOS/nixpkgs/nixos-25.05"},
	}
	for path, values := range known {
		if value, err := strconv.Unquote(actual[path]); err == nil && slices.Contains(values, value) {
			fields[path] = actual[path]
		}
	}
	addRetiredFlakeFields(actual, fields)
	return flakeBaseline{Version: 1, Fields: fields}, nil
}

func legacyWahrweltURLs() []string {
	var urls []string
	for _, url := range config.KnownWahrweltFlakeURLs() {
		if !strings.Contains(url, "/presets/") {
			urls = append(urls, url)
		}
	}
	return urls
}

func addRetiredFlakeFields(actual, fields map[string]string) {
	retired := map[string][]string{
		"noctalia":               {"github:noctalia-dev/noctalia", "github:noctalia-dev/noctalia/v5.0.0-beta1"},
		"noctalia-shell":         {"github:noctalia-dev/noctalia-shell/v4.7.7"},
		"caelestia-shell":        {"github:caelestia-dots/shell"},
		"caelestia-cli":          {"github:caelestia-dots/cli"},
		"zapret-discord-youtube": {"github:kartavkun/zapret-discord-youtube"},
		"templ":                  {"github:a-h/templ"},
	}
	for name, urls := range retired {
		prefix := "inputs." + name + "."
		url, err := strconv.Unquote(actual[prefix+"url"])
		if err != nil || !slices.Contains(urls, url) || !retiredInputRecognized(actual, prefix) {
			continue
		}
		for key, value := range actual {
			if strings.HasPrefix(key, prefix) {
				fields[key] = value
			}
		}
		key := "inputs.wahrwelt.inputs." + name + ".follows"
		if flakeValueEqual(actual[key], strconv.Quote(name)) {
			fields[key] = actual[key]
		}
	}
}

func retiredInputRecognized(actual map[string]string, prefix string) bool {
	for key, value := range actual {
		if !strings.HasPrefix(key, prefix) || key == prefix+"url" {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, prefix+"inputs."), ".follows")
		target, err := strconv.Unquote(value)
		if err != nil || !strings.HasPrefix(key, prefix+"inputs.") || !strings.HasSuffix(key, ".follows") {
			return false
		}
		if !slices.Contains([]string{"nixpkgs", "nixpkgs-unstable", "caelestia-cli", "quickshell"}, name) {
			return false
		}
		stable := name == "nixpkgs" && target == "nixpkgs-stable"
		unstable := name == "nixpkgs-unstable" && target == "nixpkgs"
		if target != name && !stable && !unstable {
			return false
		}
	}
	return true
}

func migrateFlakeBrand(text string) (string, error) {
	doc, err := parseFlake(text)
	if err != nil {
		return "", err
	}
	binding := doc.leaves["inputs.mysetup.url"]
	if binding == nil {
		return text, nil
	}
	url, err := strconv.Unquote(text[binding.valueStart:binding.valueEnd])
	if err != nil || !slices.Contains(config.KnownWahrweltFlakeURLs(), url) {
		return "", fmt.Errorf("legacy mysetup input has a custom source; rename it to wahrwelt explicitly before updating")
	}
	for key := range doc.leaves {
		if strings.HasPrefix(key, "inputs.wahrwelt.") {
			return "", fmt.Errorf("both mysetup and wahrwelt inputs exist; resolve the name collision before updating")
		}
	}
	edits, err := legacyBrandEdits(doc)
	if err != nil {
		return "", err
	}
	return applyFlakeEdits(text, edits)
}

func legacyBrandEdits(doc *flakeDocument) ([]flakeEdit, error) {
	definitions := legacyBrandDefinitions(doc)
	var edits []flakeEdit
	for index, token := range doc.tokens {
		if strings.Contains(token.text, "${") && strings.Contains(token.text, "mysetup") {
			return nil, fmt.Errorf("legacy mysetup reference inside a string needs manual renaming")
		}
		if token.text == "mkMySetupHost" && index >= 4 && doc.tokens[index-4].text == "mysetup" && doc.tokens[index-2].text == "lib" {
			edits = append(edits, flakeEdit{token.start, token.end, "mkWahrweltHost"})
			continue
		}
		if replacement, ok := legacyFollowReplacement(doc, token); ok {
			edits = append(edits, flakeEdit{token.start, token.end, replacement})
			continue
		}
		if token.text != "mysetup" && token.text != `"mysetup"` {
			continue
		}
		edit, err := legacyNameEdit(doc.tokens, index, definitions[token.start])
		if err != nil {
			return nil, err
		}
		if edit != nil {
			edits = append(edits, *edit)
		}
	}
	return edits, nil
}

func legacyNameEdit(tokens []flakeToken, index int, definition bool) (*flakeEdit, error) {
	token := tokens[index]
	reference := index+1 < len(tokens) && tokens[index+1].text == "." && (index == 0 || tokens[index-1].text != ".")
	if reference && !definition && legacyNameAssigned(tokens[index:]) {
		return nil, fmt.Errorf("custom mysetup attribute near byte %d needs manual reconciliation", token.start)
	}
	selection := index >= 2 && tokens[index-1].text == "." && tokens[index-2].text == "inputs"
	if !definition && !reference && !selection {
		if token.text == `"mysetup"` {
			return nil, nil
		}
		return nil, fmt.Errorf("ambiguous mysetup name near byte %d; rename this reference manually", token.start)
	}
	replacement := "wahrwelt"
	if token.text == `"mysetup"` {
		replacement = `"wahrwelt"`
	}
	return &flakeEdit{token.start, token.end, replacement}, nil
}

func legacyFollowReplacement(doc *flakeDocument, token flakeToken) (string, bool) {
	value, err := strconv.Unquote(token.text)
	if err != nil || (value != "mysetup" && !strings.HasPrefix(value, "mysetup/")) {
		return "", false
	}
	for path, binding := range doc.leaves {
		if strings.HasSuffix(path, ".follows") && binding.valueStart == token.start && binding.valueEnd == token.end {
			return strconv.Quote("wahrwelt" + strings.TrimPrefix(value, "mysetup")), true
		}
	}
	return "", false
}

func legacyNameAssigned(tokens []flakeToken) bool {
	for index := 1; index < len(tokens); index += 2 {
		if tokens[index].text == "=" {
			return true
		}
		if tokens[index].text != "." || index+1 >= len(tokens) {
			return false
		}
	}
	return false
}

func legacyBrandDefinitions(doc *flakeDocument) map[int]bool {
	definitions := map[int]bool{}
	for path, binding := range doc.nodes {
		if path != "inputs.mysetup" && !strings.HasPrefix(path, "inputs.mysetup.") {
			continue
		}
		for _, token := range doc.tokens {
			if token.start >= binding.start && token.end <= binding.valueStart && (token.text == "mysetup" || token.text == `"mysetup"`) {
				definitions[token.start] = true
			}
		}
	}
	for _, binding := range doc.sets[""].bindings {
		if binding.path != "outputs" {
			continue
		}
		for _, token := range doc.tokens[binding.first:binding.last] {
			if token.text == ":" {
				break
			}
			if token.text == "mysetup" {
				definitions[token.start] = true
			}
		}
	}
	return definitions
}

func updateFlakeComments(text string, state config.State) string {
	comments, err := flakeHeaderComments(text)
	if err != nil {
		return text
	}
	var edits []flakeEdit
	for _, comment := range comments {
		trimmed := strings.TrimSpace(comment.text)
		suffix := comment.text[len(trimmed):]
		if trimmed == "# Generated by MySetup installer." {
			edits = append(edits, flakeEdit{comment.start, comment.end, "# Generated by Wahrwelt installer." + suffix})
		}
		if value, found := strings.CutPrefix(trimmed, "# preset: "); found && config.IsPackagePreset(value) {
			edits = append(edits, flakeEdit{comment.start, comment.end, "# preset: " + state.Packages.Preset + suffix})
		}
	}
	result, err := applyFlakeEdits(text, edits)
	if err != nil {
		return text
	}
	return result
}

// CurrentLockMode reads installer metadata only from actual header comments.
func CurrentLockMode(text string) (LockMode, error) {
	metadata, err := flakeMetadata(text)
	if err != nil {
		return "", err
	}
	if metadata["lock mode"] == string(LockModeManaged) {
		return LockModeManaged, nil
	}
	return LockModeIndependent, nil
}

func flakeMetadata(text string) (map[string]string, error) {
	comments, err := flakeHeaderComments(text)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	for _, comment := range comments {
		key, value, found := strings.Cut(strings.TrimPrefix(strings.TrimSpace(comment.text), "# "), ": ")
		if !found || (key != "lock mode" && key != "preset") {
			continue
		}
		if previous, exists := metadata[key]; exists && previous != value {
			return nil, fmt.Errorf("conflicting %s comments in flake.nix header", key)
		}
		if key == "lock mode" && value != string(LockModeManaged) && value != string(LockModeIndependent) {
			return nil, fmt.Errorf("unsupported lock mode %q in flake.nix", value)
		}
		if key == "preset" && !config.IsPackagePreset(value) {
			return nil, fmt.Errorf("unsupported preset %q in flake.nix", value)
		}
		metadata[key] = value
	}
	return metadata, nil
}

func flakeHeaderComments(text string) ([]flakeToken, error) {
	tokens, err := scanFlake(text)
	if err != nil {
		return nil, err
	}
	if len(tokens) < 2 || tokens[0].text != "{" {
		return nil, fmt.Errorf("flake.nix must start with a literal attribute set")
	}
	var comments []flakeToken
	for i := 0; i < tokens[1].start; {
		switch {
		case strings.HasPrefix(text[i:], "/*"):
			i = nixBlockCommentEnd(text, i)
		case text[i] == '#':
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				end = len(text) - i
			}
			comments = append(comments, flakeToken{text[i : i+end], i, i + end})
			i += end
		default:
			i++
		}
	}
	return comments, nil
}

func validateFlakePaths(doc *flakeDocument) error {
	for path := range doc.leaves {
		for ancestor := path; strings.Contains(ancestor, "."); {
			ancestor = ancestor[:strings.LastIndexByte(ancestor, '.')]
			if doc.leaves[ancestor] != nil {
				return fmt.Errorf("conflicting managed attribute paths %s and %s", ancestor, path)
			}
		}
	}
	return nil
}
