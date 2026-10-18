package hosttree

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCopyPreservesCustomTreeAndRelativeLinks(t *testing.T) {
	parent := t.TempDir()
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	local := mkdir(t, source, "local")
	put(t, filepath.Join(local, "custom.nix"), "{ networking.firewall.enable = true; }\n")
	put(t, filepath.Join(parent, "outside"), "external context")
	if err := os.Symlink("../../outside", filepath.Join(local, "linked")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Copy(source, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "local", "linked"))
	if err != nil || string(data) != "external context" {
		t.Fatalf("relative external context changed: %q, %v", data, err)
	}
	if err := Verify(source, snapshot); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(local, "custom.nix"), "{ networking.firewall.enable = false; }\n")
	if err := Verify(source, snapshot); err == nil {
		t.Fatal("missed changed custom module")
	}
}

func TestCopySkipDoesNotOverwriteExistingManagedFiles(t *testing.T) {
	parent := t.TempDir()
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	put(t, filepath.Join(source, "flake.nix"), "installed")
	put(t, filepath.Join(target, "flake.nix"), "planned")
	put(t, filepath.Join(source, "custom.nix"), "preserve")
	if _, err := Copy(source, target, []string{"flake.nix"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "flake.nix"))
	if err != nil || string(data) != "planned" {
		t.Fatal("overwrote the managed candidate")
	}
	data, err = os.ReadFile(filepath.Join(target, "custom.nix"))
	if err != nil || string(data) != "preserve" {
		t.Fatal("lost the custom file")
	}
}

func TestGitCopyExcludesUntrackedAndIgnoredSecrets(t *testing.T) {
	parent := t.TempDir()
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	git(t, source, "init", "-q")
	put(t, filepath.Join(source, "flake.nix"), "{ outputs = {self}: {}; }")
	put(t, filepath.Join(source, ".gitignore"), "ignored-secret\n")
	put(t, filepath.Join(source, "ignored-secret"), "must stay local")
	put(t, filepath.Join(source, "untracked-secret"), "must also stay local")
	local := mkdir(t, source, "local modules")
	put(t, filepath.Join(local, "custom module.nix"), "{ test = true; }")
	git(t, source, "add", "flake.nix", ".gitignore", "local modules/custom module.nix")
	snapshot, err := Copy(source, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ignored-secret", "untracked-secret", ".git"} {
		if _, err := os.Lstat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Errorf("copied Git-excluded path %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "local modules", "custom module.nix")); err != nil {
		t.Fatal(err)
	}
	if err := Verify(source, snapshot); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "untracked-secret")
	if err := Verify(source, snapshot); err == nil {
		t.Fatal("missed changed Git source selection")
	}
}

func TestAncestorGitRepositoryRequiresExplicitContext(t *testing.T) {
	parent := t.TempDir()
	git(t, parent, "init", "-q")
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	if _, err := Copy(source, target, nil); err == nil || !strings.Contains(err.Error(), "larger Git repository") {
		t.Fatalf("got %v, want unsupported context rejection", err)
	}
}

func TestSpecialFileIsRejectedWithoutReading(t *testing.T) {
	parent := t.TempDir()
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	if err := unix.Mkfifo(filepath.Join(source, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Copy(source, target, nil); err == nil {
		t.Fatal("accepted FIFO")
	}
}

func TestExistingTargetFileIsNotOverwritten(t *testing.T) {
	parent := t.TempDir()
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	put(t, filepath.Join(source, "custom.nix"), "source")
	put(t, filepath.Join(target, "custom.nix"), "other owner")
	if _, err := Copy(source, target, nil); err == nil {
		t.Fatal("overwrote a target ownership collision")
	}
	data, _ := os.ReadFile(filepath.Join(target, "custom.nix"))
	if string(data) != "other owner" {
		t.Fatal("changed target before rejecting collision")
	}
}

func TestCopyStreamsLargeLocalPayloadAndDetectsLaterChange(t *testing.T) {
	parent := t.TempDir()
	source := mkdir(t, parent, "host")
	target := mkdir(t, parent, "stage")
	const payloadSize = 20 << 20
	path := filepath.Join(source, "local-package.jar")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate(payloadSize); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int64{0, 17 << 20, payloadSize - 16} {
		if _, err := file.WriteAt([]byte("local payload"), offset); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := Copy(source, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(source, snapshot); err != nil {
		t.Fatal(err)
	}
	copied, err := os.Open(filepath.Join(target, "local-package.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(target, snapshot); err != nil {
		t.Fatalf("copied payload hash differs: %v", err)
	}
	defer copied.Close()
	info, err := copied.Stat()
	if err != nil || info.Size() != payloadSize {
		t.Fatalf("copied payload size differs: %v", err)
	}
	buffer := make([]byte, len("local payload"))
	for _, offset := range []int64{0, 17 << 20, payloadSize - 16} {
		if _, err := copied.ReadAt(buffer, offset); err != nil || !bytes.Equal(buffer, []byte("local payload")) {
			t.Fatalf("payload lost at offset %d: %q, %v", offset, buffer, err)
		}
	}
	if _, err := file.WriteAt([]byte("changed"), 17<<20); err != nil {
		t.Fatal(err)
	}
	if err := Verify(source, snapshot); err == nil {
		t.Fatal("missed a change beyond the old per-file limit")
	}
}

func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	path := filepath.Join(parent, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, path string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fixture failed: %s: %v", out, err)
	}
}
