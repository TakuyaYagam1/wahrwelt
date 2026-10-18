package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
)

const updateTestRevision = "0123456789abcdef0123456789abcdef01234567"

type updateBootstrapRunner struct {
	metadata string
	calls    []string
}

func (r *updateBootstrapRunner) IsDryRun() bool { return false }

func (r *updateBootstrapRunner) Command(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil
}

func (r *updateBootstrapRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	_ = r.Command(ctx, name, args...)
	if strings.Contains(strings.Join(args, " "), "flake metadata") {
		return r.metadata, nil
	}
	return "/nix/store/test-wahrwelt", nil
}

func updateFixture(t *testing.T) paths.Options {
	t.Helper()
	dest := t.TempDir()
	p := paths.Options{NixOSDest: dest, StatePath: filepath.Join(dest, "installer-state.json")}
	state := config.Default()
	if err := config.Save(p.StatePath, state); err != nil {
		t.Fatal(err)
	}
	flake, err := apply.FlakeNix(state, apply.LockModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "flake.nix"), []byte(flake), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRefreshUpdateUsesOneResolvedRevision(t *testing.T) {
	p := updateFixture(t)
	target := "github:TakuyaYagam1/wahrwelt/" + updateTestRevision + "?dir=Linux/NixOS"
	runner := &updateBootstrapRunner{metadata: fmt.Sprintf(`{"url":%q,"locked":{"rev":%q}}`, target, updateTestRevision)}
	if err := refreshUpdate(context.Background(), runner, p, []string{"nixpkgs"}, true, true); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("expected resolve, build and launch, got %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "metadata --refresh --json --no-write-lock-file") {
		t.Fatalf("source was not refreshed independently: %s", runner.calls[0])
	}
	if !strings.Contains(runner.calls[1], target+"#wahrwelt") {
		t.Fatalf("updater was not built from the resolved revision: %s", runner.calls[1])
	}
	for _, want := range []string{"--target-rev " + updateTestRevision, "--target-url " + target,
		"--dest " + p.NixOSDest, "--state " + p.StatePath, "--no-switch", "--dry-run", "-- nixpkgs"} {
		if !strings.Contains(runner.calls[2], want) {
			t.Fatalf("launch missing %q: %s", want, runner.calls[2])
		}
	}
}

func TestRefreshUpdateRejectsUnpinnedMetadata(t *testing.T) {
	p := updateFixture(t)
	runner := &updateBootstrapRunner{metadata: `{"url":"github:example/wahrwelt/main","locked":{}}`}
	if err := refreshUpdate(context.Background(), runner, p, nil, false, false); err == nil {
		t.Fatal("expected unresolved revision rejection")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("invalid target launched an updater: %v", runner.calls)
	}
}

func TestUpdateRejectsDifferentExecutableRevision(t *testing.T) {
	old := SourceRevision
	SourceRevision = strings.Repeat("a", 40)
	t.Cleanup(func() { SourceRevision = old })
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"update", "--target-rev", updateTestRevision, "--target-url", "github:example/repo", "--source-url", "github:example/repo"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "revision does not match") {
		t.Fatalf("expected executable revision mismatch, got %v", err)
	}
}

func TestUpdatePathsKeepsCustomDestinationState(t *testing.T) {
	p := paths.DefaultOptions()
	p.NixOSDest = t.TempDir()
	got, err := updatePaths(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatePath != filepath.Join(p.NixOSDest, "installer-state.json") {
		t.Fatalf("update selected another host's state: %s", got.StatePath)
	}
}

func TestUpdateMissingStateFailsClosed(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"update", "--dest", t.TempDir(), "--dry-run"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "state file does not exist") {
		t.Fatalf("expected missing installed state error, got %v", err)
	}
}
