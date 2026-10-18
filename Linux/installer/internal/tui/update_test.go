package tui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
)

func TestInstalledUpdateUsesSavedSettings(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "installer-state.json")
	installed := config.Default()
	installed.Host.Hostname = "installed-host"
	if err := config.Save(statePath, installed); err != nil {
		t.Fatal(err)
	}
	s := &session{
		state: config.Default(),
		paths: paths.Options{NixOSDest: dir, StatePath: statePath},
	}
	s.state.Host.Hostname = "edited-host"
	state, args, err := installedUpdate(s)
	if err != nil {
		t.Fatal(err)
	}
	if state.Host.Hostname != installed.Host.Hostname {
		t.Fatalf("update used %q instead of installed host %q", state.Host.Hostname, installed.Host.Hostname)
	}
	want := []string{"update", "--dest", dir, "--state", statePath}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("update args = %v, want %v", args, want)
	}
	if s.state.Host.Hostname != "edited-host" {
		t.Fatal("preparing an update replaced the session edits")
	}
	if isDirtySection(updateSection) {
		t.Fatal("update action must not save a draft")
	}
}

func TestInstalledUpdateRejectsDraftWithoutInstalledState(t *testing.T) {
	dir := t.TempDir()
	draftPath := filepath.Join(dir, "draft.json")
	if err := config.Save(draftPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s := &session{
		state: config.Default(),
		paths: paths.Options{
			NixOSDest: dir,
			StatePath: filepath.Join(dir, "installer-state.json"),
			DraftPath: draftPath,
		},
	}
	if _, _, err := installedUpdate(s); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("update without installed state = %v, want missing state error", err)
	}
}

func TestInstalledUpdatePassesDryRunToUpdater(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "installer-state.json")
	if err := config.Save(statePath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s := &session{
		paths:  paths.Options{NixOSDest: dir, StatePath: statePath},
		dryRun: true,
	}
	_, args, err := installedUpdate(s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"update", "--dest", dir, "--state", statePath, "--dry-run"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("preview args = %v, want %v", args, want)
	}
}

func TestInstalledUpdateRejectsIncompleteState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "installer-state.json")
	if err := os.WriteFile(statePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &session{
		state: config.Default(),
		paths: paths.Options{NixOSDest: dir, StatePath: statePath},
	}
	if _, _, err := installedUpdate(s); err == nil {
		t.Fatal("update accepted incomplete saved settings")
	}
}

func TestInstalledUpdateUsesStateUnderCustomDestination(t *testing.T) {
	dest := t.TempDir()
	statePath := filepath.Join(dest, "installer-state.json")
	installed := config.Default()
	installed.Host.Hostname = "custom-host"
	if err := config.Save(statePath, installed); err != nil {
		t.Fatal(err)
	}
	s := &session{
		state: config.Default(),
		paths: paths.Options{NixOSDest: dest, StatePath: paths.DefaultStatePath},
	}
	state, args, err := installedUpdate(s)
	if err != nil {
		t.Fatal(err)
	}
	if state.Host.Hostname != installed.Host.Hostname {
		t.Fatalf("update loaded %q, want %q", state.Host.Hostname, installed.Host.Hostname)
	}
	want := []string{"update", "--dest", dest, "--state", statePath}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("update args = %v, want %v", args, want)
	}
}
