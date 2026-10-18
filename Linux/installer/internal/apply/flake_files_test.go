package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/hosttree"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
)

func TestInstalledFlakeReconciliationKeepsCustomInputAndBaseline(t *testing.T) {
	dest, staging := t.TempDir(), t.TempDir()
	state := config.Default()
	flake, err := FlakeNix(state, LockModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	flake = strings.Replace(flake, "github:0xc000022070/zen-browser-flake", "github:youwen5/zen-browser-flake", 1)
	custom := "    local-tools = { url = \"path:./tools\"; flake = false; }; # user input\n"
	flake = strings.Replace(flake, "  inputs = {\n", "  inputs = {\n"+custom, 1)
	if err := os.WriteFile(filepath.Join(dest, "flake.nix"), []byte(flake), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stageThinConfiguration(paths.Sources{}, staging, state, LockModeIndependent); err != nil {
		t.Fatal(err)
	}
	if copied, err := copyExistingThinFlake(dest, filepath.Join(staging, "flake.nix"), state); err != nil || !copied {
		t.Fatalf("reconcile wrapper: copied=%v err=%v", copied, err)
	}
	updated, err := os.ReadFile(filepath.Join(staging, "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), custom) || strings.Contains(string(updated), "youwen5") {
		t.Fatal("custom input was lost or old Zen source was retained")
	}
	baseline, err := readFlakeState(staging)
	if err != nil || len(baseline) == 0 {
		t.Fatalf("missing baseline: %v", err)
	}
	if strings.Contains(string(baseline), "local-tools") {
		t.Fatal("baseline claimed a user-owned input")
	}
	plan, err := PlanFlake(string(updated), state, baseline)
	if err != nil || plan.Content != string(updated) {
		t.Fatalf("second run changed a reconciled wrapper: %v", err)
	}
}

func TestCustomHostFilesAreStagedButNeverPublishedByInstaller(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Fatal("rsync is required for installer publication tests")
	}
	dest, staging := t.TempDir(), t.TempDir()
	state := config.Default()
	flake, err := FlakeNix(state, LockModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "flake.nix"), []byte(flake), 0o644); err != nil {
		t.Fatal(err)
	}
	customPath := filepath.Join(dest, "custom.nix")
	custom := []byte("{ ... }: { services.openssh.enable = true; }\n")
	if err := os.WriteFile(customPath, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stageThinConfiguration(paths.Sources{}, staging, state, LockModeIndependent); err != nil {
		t.Fatal(err)
	}
	snapshot, err := stageHostExtras(staging, dest, LayoutThin)
	if err != nil || snapshot == nil {
		t.Fatalf("stage custom files: %v", err)
	}
	staged, err := os.ReadFile(filepath.Join(staging, "custom.nix"))
	if err != nil || string(staged) != string(custom) {
		t.Fatalf("custom module missing from validation: %v", err)
	}
	if err := hosttree.Verify(dest, *snapshot); err != nil {
		t.Fatal(err)
	}
	// Publication must ignore custom files even when staging differs.
	if err := os.WriteFile(filepath.Join(staging, "custom.nix"), []byte("different\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := syncToEtcArgs(staging, dest, LayoutThin)
	if output, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("thin publication: %v\n%s", err, output)
	}
	data, err := os.ReadFile(customPath)
	if err != nil || string(data) != string(custom) {
		t.Fatalf("publication replaced a custom module: %v", err)
	}
	info, err := os.Stat(customPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("publication changed custom module permissions: %v", err)
	}
}
