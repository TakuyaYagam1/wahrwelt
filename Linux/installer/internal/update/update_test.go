package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/fsowner"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
)

const testRevision = "1111111111111111111111111111111111111111"

type fixture struct {
	t       *testing.T
	opts    Options
	runner  *fakeRunner
	initial map[string][]byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "nixos")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	state := config.Default()
	state.User.Username = "tester"
	state.User.HomeDirectory = "/home/tester"
	state.Git.Username = "Tester"
	state.Git.Email = "tester@example.com"
	state.Packages.Preset = "personal"
	statePath := filepath.Join(dest, "installer-state.json")
	if err := config.Save(statePath, state); err != nil {
		t.Fatal(err)
	}
	text, err := apply.FlakeNix(state, apply.LockModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	text = strings.Replace(text, "github:0xc000022070/zen-browser-flake", "github:youwen5/zen-browser-flake", 1)
	text = strings.Replace(text, "  inputs = {", "  inputs = {\n    custom.url = \"path:../private-flake\";", 1)
	write(t, filepath.Join(dest, "flake.nix"), []byte(text))
	write(t, filepath.Join(dest, "flake.lock"), []byte("{\"version\":7,\"nodes\":{},\"root\":\"root\"}\n"))
	write(t, filepath.Join(dest, "custom.nix"), []byte("{ services.openssh.enable = true; }\n"))
	runner := &fakeRunner{t: t, revision: testRevision}
	f := &fixture{t: t, runner: runner, opts: Options{
		Paths:          paths.Options{NixOSDest: dest, StatePath: statePath},
		TargetRevision: testRevision,
		TargetURL:      "github:TakuyaYagam1/wahrwelt/" + testRevision + "?dir=Linux/NixOS/presets/personal",
		SourceURL:      config.WahrweltPresetFlakeURL(state.Source.Channel, state.Packages.Preset),
		Runner:         runner,
		Output:         io.Discard,
	}}
	f.initial = f.contents()
	return f
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) contents() map[string][]byte {
	f.t.Helper()
	result := make(map[string][]byte)
	entries, err := os.ReadDir(f.opts.Paths.NixOSDest)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			data, err := os.ReadFile(filepath.Join(f.opts.Paths.NixOSDest, entry.Name()))
			if err != nil {
				f.t.Fatal(err)
			}
			result[entry.Name()] = data
		}
	}
	return result
}

func (f *fixture) assertUnchanged() {
	f.t.Helper()
	after := f.contents()
	if len(after) != len(f.initial) {
		f.t.Fatalf("file count changed: got %d, want %d", len(after), len(f.initial))
	}
	for name, before := range f.initial {
		if string(before) != string(after[name]) {
			f.t.Errorf("installed file changed: %s", name)
		}
	}
}

type fakeRunner struct {
	t           *testing.T
	revision    string
	commands    [][]string
	buildError  error
	beforeBuild func()
	stage       string
}

func (r *fakeRunner) IsDryRun() bool { return false }

func (r *fakeRunner) Command(_ context.Context, name string, args ...string) error {
	r.commands = append(r.commands, append([]string{name}, args...))
	if name != "nix" || !contains(args, "update") {
		return nil
	}
	for index, arg := range args {
		if arg == "--flake" {
			r.stage = strings.TrimPrefix(args[index+1], "path:")
		}
	}
	write(r.t, filepath.Join(r.stage, "flake.lock"), testLock(r.revision, "personal"))
	return nil
}

func (r *fakeRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	r.commands = append(r.commands, append([]string{name}, args...))
	if r.beforeBuild != nil {
		r.beforeBuild()
	}
	if r.buildError != nil {
		return "", r.buildError
	}
	return "/nix/store/00000000000000000000000000000000-nixos-system", nil
}

func testLock(revision, preset string) []byte {
	return []byte(`{"version":7,"root":"root","nodes":{"root":{"inputs":{"wahrwelt":"wahrwelt"}},"wahrwelt":{"locked":{"type":"github","owner":"TakuyaYagam1","repo":"wahrwelt","rev":"` + revision + `","dir":"Linux/NixOS/presets/` + preset + `"}}}}`)
}

func TestDryRunDoesNotWriteOrRunNix(t *testing.T) {
	f := newFixture(t)
	f.opts.DryRun = true
	if err := Run(t.Context(), f.opts); err != nil {
		t.Fatal(err)
	}
	f.assertUnchanged()
	if len(f.runner.commands) != 0 {
		t.Fatal("dry run invoked a command")
	}
	entries, err := os.ReadDir(filepath.Dir(f.opts.Paths.NixOSDest))
	if err != nil || len(entries) != 1 {
		t.Fatalf("dry run created staging or journal files: %v", err)
	}
}

func TestBuildFailurePreservesInstalledFiles(t *testing.T) {
	f := newFixture(t)
	f.runner.buildError = errors.New("fixture build failure")
	if err := runUpdate(t.Context(), f.opts); err == nil || !strings.Contains(err.Error(), "fixture build failure") {
		t.Fatalf("got %v, want build failure", err)
	}
	f.assertUnchanged()
}

func TestNoSwitchBuildsWithoutPublishing(t *testing.T) {
	f := newFixture(t)
	f.opts.SkipSwitch = true
	if err := Run(t.Context(), f.opts); err != nil {
		t.Fatal(err)
	}
	f.assertUnchanged()
	if len(f.runner.commands) != 2 || !contains(f.runner.commands[1], "build") {
		t.Fatalf("expected lock and build only, got %v", f.runner.commands)
	}
}

func TestUpdatePublishesOnlyOwnedFilesAndIsRepeatable(t *testing.T) {
	f := newFixture(t)
	if err := runUpdate(t.Context(), f.opts); err != nil {
		t.Fatal(err)
	}
	after := f.contents()
	if !strings.Contains(string(after["flake.nix"]), "github:0xc000022070/zen-browser-flake") ||
		!strings.Contains(string(after["flake.nix"]), "custom.url = \"path:../private-flake\"") {
		t.Fatal("migration failed to change Zen while retaining the custom input")
	}
	for _, name := range []string{"custom.nix", "installer-state.json"} {
		if string(after[name]) != string(f.initial[name]) {
			t.Fatalf("update changed %s", name)
		}
	}
	if len(after[apply.FlakeStateName]) == 0 {
		t.Fatal("ownership state was not published")
	}
	info, err := os.Stat(filepath.Join(f.opts.Paths.NixOSDest, apply.FlakeStateName))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("new baseline mode must be 0644: %v", err)
	}
	if err := runUpdate(t.Context(), f.opts); err != nil {
		t.Fatal(err)
	}
	for name, before := range after {
		if string(f.contents()[name]) != string(before) {
			t.Fatalf("repeated update changed %s", name)
		}
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(f.opts.Paths.NixOSDest), ".nixos-updates", "backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("want one backup for the one publication, got %v: %v", backups, err)
	}
	data, err := os.ReadFile(filepath.Join(backups[0], "flake.nix"))
	if err != nil || string(data) != string(f.initial["flake.nix"]) {
		t.Fatal("backup did not retain the original wrapper")
	}
}

func TestPublicationFailureRollsBackExactFiles(t *testing.T) {
	f := newFixture(t)
	installed, err := loadInstallation(f.opts)
	if err != nil {
		t.Fatal(err)
	}
	root, release, err := lockUpdates(installed.dest)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	writes := 0
	err = publish(root, installed, testLock(testRevision, "personal"), func(tx, target string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return errors.New("fixture write failure")
		}
		return fsowner.Write(tx, target, data, mode)
	})
	if err == nil || !strings.Contains(err.Error(), "fixture write failure") {
		t.Fatalf("got %v, want write failure", err)
	}
	f.assertUnchanged()
}

func TestConcurrentCustomChangePreventsPublication(t *testing.T) {
	f := newFixture(t)
	f.runner.beforeBuild = func() {
		write(t, filepath.Join(f.opts.Paths.NixOSDest, "custom.nix"), []byte("{ services.openssh.enable = false; }\n"))
	}
	err := runUpdate(t.Context(), f.opts)
	if err == nil || !strings.Contains(err.Error(), "changed during update") {
		t.Fatalf("got %v, want concurrent change rejection", err)
	}
	for _, name := range []string{"flake.nix", "flake.lock", "installer-state.json"} {
		if string(f.contents()[name]) != string(f.initial[name]) {
			t.Fatalf("concurrent change caused publication of %s", name)
		}
	}
}

func TestInstalledStateChangePreventsPublication(t *testing.T) {
	f := newFixture(t)
	f.runner.beforeBuild = func() {
		data := append([]byte(nil), f.initial["installer-state.json"]...)
		var state map[string]any
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		state["host"].(map[string]any)["hostname"] = "another-host"
		data, _ = json.Marshal(state)
		write(t, f.opts.Paths.StatePath, data)
	}
	if err := runUpdate(t.Context(), f.opts); err == nil {
		t.Fatal("accepted changed installed state")
	}
	if string(f.contents()["flake.nix"]) != string(f.initial["flake.nix"]) {
		t.Fatal("published flake after installed state changed")
	}
}

func TestIncorrectLockRevisionStopsBeforeBuild(t *testing.T) {
	f := newFixture(t)
	f.runner.revision = strings.Repeat("2", 40)
	if err := runUpdate(t.Context(), f.opts); err == nil || !strings.Contains(err.Error(), "updater revision") {
		t.Fatalf("got %v, want revision rejection", err)
	}
	f.assertUnchanged()
	if len(f.runner.commands) != 1 {
		t.Fatal("built an unexpected revision")
	}
}

func TestUnknownJournalDirectoryIsPreserved(t *testing.T) {
	f := newFixture(t)
	root, release, err := lockUpdates(f.opts.Paths.NixOSDest)
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(root, ".update-unknown")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(unknown, "keep"), []byte("user data"))
	release()
	if err := runUpdate(t.Context(), f.opts); err == nil || !strings.Contains(err.Error(), "needs recovery") {
		t.Fatalf("got %v, want unknown journal rejection", err)
	}
	if data, err := os.ReadFile(filepath.Join(unknown, "keep")); err != nil || string(data) != "user data" {
		t.Fatal("unknown journal contents were changed")
	}
	f.assertUnchanged()
}

func TestInterruptedPublicationRecoversBeforePlanning(t *testing.T) {
	f := newFixture(t)
	installed, err := loadInstallation(f.opts)
	if err != nil {
		t.Fatal(err)
	}
	root, release, err := lockUpdates(installed.dest)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installed.dest, "flake.nix")
	tx, err := fsowner.Begin(root, journalPrefix, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if err := fsowner.Write(tx, target, []byte("invalid interrupted wrapper"), 0o600); err != nil {
		t.Fatal(err)
	}
	release()
	f.runner.buildError = errors.New("fixture build failure")
	if err := runUpdate(t.Context(), f.opts); err == nil || !strings.Contains(err.Error(), "fixture build failure") {
		t.Fatalf("recovery did not reach build: %v", err)
	}
	f.assertUnchanged()
}

func TestRejectsPartialInstalledState(t *testing.T) {
	f := newFixture(t)
	write(t, f.opts.Paths.StatePath, []byte(`{"host":{"hostname":"NixOS"}}`))
	f.opts.DryRun = true
	if err := Run(t.Context(), f.opts); err == nil {
		t.Fatal("accepted draft-like state")
	}
}

func TestOrdinaryUpdatePreservesActualChannelOverSavedState(t *testing.T) {
	f := newFixture(t)
	current := strings.Replace(string(f.initial["flake.nix"]), "/wahrwelt/main?", "/wahrwelt/dev?", 1)
	write(t, filepath.Join(f.opts.Paths.NixOSDest, "flake.nix"), []byte(current))
	f.opts.SourceURL = strings.Replace(f.opts.SourceURL, "/wahrwelt/main?", "/wahrwelt/dev?", 1)
	if err := runUpdate(t.Context(), f.opts); err != nil {
		t.Fatal(err)
	}
	updated := string(f.contents()["flake.nix"])
	if !strings.Contains(updated, f.opts.SourceURL) || strings.Contains(updated, "/wahrwelt/main?") {
		t.Fatal("ordinary update changed the actual source channel to saved state")
	}
}

func TestRejectsSymlinkedManagedFile(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.opts.Paths.NixOSDest, "flake.lock")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("custom.nix", path); err != nil {
		t.Fatal(err)
	}
	f.opts.DryRun = true
	if err := Run(t.Context(), f.opts); err == nil {
		t.Fatal("accepted symlinked managed file")
	}
}
