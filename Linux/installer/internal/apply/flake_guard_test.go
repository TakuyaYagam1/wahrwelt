package apply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
)

func TestHostGuardRejectsChangesToPublishedFiles(t *testing.T) {
	for _, name := range thinPublishedFiles {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			path := filepath.Join(dest, name)
			if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
				t.Fatal(err)
			}
			guard, err := captureHostFiles(dest, LayoutThin)
			if err != nil {
				t.Fatal(err)
			}
			if err := guard(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("user edit"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := guard(); err == nil {
				t.Fatal("accepted user edit made during validation")
			}
		})
	}
}

func TestHostGuardPreservesNewFileAndRejectsLinks(t *testing.T) {
	for _, kind := range []string{"new", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dest := t.TempDir()
			guard, err := captureHostFiles(dest, LayoutThin)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dest, "flake.nix")
			other := filepath.Join(dest, "custom.nix")
			if err := os.WriteFile(other, []byte("user file"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "new":
				err = os.WriteFile(path, []byte("new custom flake"), 0o600)
			case "symlink":
				err = os.Symlink(other, path)
			case "hardlink":
				err = os.Link(other, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := guard(); err == nil {
				t.Fatal("accepted new file during validation")
			}
			if kind != "new" {
				if _, err := captureHostFiles(dest, LayoutThin); err == nil {
					t.Fatal("accepted linked configuration")
				}
			}
		})
	}
}

func TestApplyRejectsHostEditDuringBuild(t *testing.T) {
	repo, dest := fakeRepo(t)
	sources, err := paths.ResolveSources(repo)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dest, "configuration.nix")
	runner := &fakeRunner{failOn: func(name string, args []string) error {
		if handled, err := runValidatedStagingTestCommand(context.Background(), name, args...); handled {
			return err
		}
		if name == "sudo" && len(args) > 1 && args[0] == "nixos-rebuild" && args[1] == "dry-build" {
			return os.WriteFile(path, []byte("# user edit during build\n"), 0o644)
		}
		return nil
	}}
	opts := Options{Paths: paths.Options{RepoRoot: repo, NixOSDest: dest}, State: validState()}
	_, err = prepareStagedApply(context.Background(), runner, sources, t.TempDir(), opts, applyModes{LayoutThin, LockModeIndependent})
	if err == nil || !strings.Contains(err.Error(), "host configuration changed") {
		t.Fatalf("expected host edit conflict, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "# user edit during build\n" {
		t.Fatalf("lost user edit: %v", err)
	}
}

func TestApplyPreservesHostEditMadeDuringBackup(t *testing.T) {
	dest := t.TempDir()
	path := filepath.Join(dest, "flake.nix")
	guard, err := captureHostFiles(dest, LayoutThin)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{failOn: func(name string, args []string) error {
		if name == "sudo" && len(args) > 0 && args[0] == "cp" {
			return os.WriteFile(path, []byte("user flake"), 0o600)
		}
		return nil
	}}
	result, err := writeSystemConfiguration(context.Background(), runner, t.TempDir(), Options{Paths: paths.Options{NixOSDest: dest}}, LayoutThin, guard)
	if err == nil || !result.skipAutomaticRestore {
		t.Fatalf("expected conflict without broad restore, got %v / %v", result, err)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "rsync" {
			t.Fatal("published after concurrent user edit")
		}
	}
}
