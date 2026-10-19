package dots

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
)

func writeNvimTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncNvimPreservesExistingConfig(t *testing.T) {
	for _, managed := range []bool{false, true} {
		name := "unmanaged"
		if managed {
			name = "managed"
		}
		t.Run(name, func(t *testing.T) {
			dotsSrc, home := t.TempDir(), t.TempDir()
			src := filepath.Join(dotsSrc, "nvim")
			dst := filepath.Join(home, ".config", "nvim")
			writeNvimTestFile(t, src, "lua/config/options.lua", "vim.opt.number = true\n")
			writeNvimTestFile(t, src, "lua/plugins/deleted.lua", "return {}\n")
			writeNvimTestFile(t, src, "lazy-lock.json", "{}\n")
			files := map[string]string{
				"lua/config/options.lua": "vim.opt.number = false\n",
				"lua/plugins/custom.lua": "return { 'my/plugin' }\n",
				"lazy-lock.json":         "{\"custom\": {\"commit\": \"local\"}}\n",
				"lazyvim.json":           "{\"extras\": [\"custom\"]}\n",
			}
			if managed {
				files[managedMarkerName] = managedMarkerWithSourceHash("nvim", "old-source")
			}
			for name, content := range files {
				writeNvimTestFile(t, dst, name, content)
			}
			runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
			if err := syncNvim(t.Context(), runner, dotsSrc, filepath.Dir(dst), ""); err != nil {
				t.Fatal(err)
			}
			for name, want := range files {
				got, err := os.ReadFile(filepath.Join(dst, name))
				if err != nil || string(got) != want {
					t.Errorf("existing %s changed: %q, %v", name, got, err)
				}
			}
			if _, err := os.Lstat(filepath.Join(dst, "lua/plugins/deleted.lua")); !os.IsNotExist(err) {
				t.Errorf("installer resurrected a deleted file: %v", err)
			}
			backups, err := filepath.Glob(dst + ".bak.*")
			if err != nil || len(backups) != 0 {
				t.Errorf("existing configuration must remain in place: %v, %v", backups, err)
			}
		})
	}
}

func TestSyncNvimRejectsDestinationSymlinks(t *testing.T) {
	for _, collision := range []string{".config", ".config/nvim"} {
		t.Run(collision, func(t *testing.T) {
			dotsSrc, home, outside := t.TempDir(), t.TempDir(), t.TempDir()
			writeNvimTestFile(t, filepath.Join(dotsSrc, "nvim"), "init.lua", "-- default\n")
			writeNvimTestFile(t, outside, "sentinel", "keep\n")
			link := filepath.Join(home, collision)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
			if err := syncNvim(t.Context(), runner, dotsSrc, filepath.Join(home, ".config"), ""); err == nil {
				t.Error("expected symlink collision error")
			}
			if target, err := os.Readlink(link); err != nil || target != outside {
				t.Errorf("symlink was replaced: %q, %v", target, err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
				t.Errorf("outside destination changed: %v, %v", entries, err)
			}
		})
	}
}

func TestSyncNvimBootstrapsOnlyAbsentTree(t *testing.T) {
	dotsSrc, home := t.TempDir(), t.TempDir()
	src, dst := filepath.Join(dotsSrc, "nvim"), filepath.Join(home, ".config", "nvim")
	writeNvimTestFile(t, src, "lua/config/options.lua", "vim.opt.number = true\n")
	runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
	if err := syncNvim(t.Context(), runner, dotsSrc, filepath.Dir(dst), ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "lua/config/options.lua"))
	if err != nil || string(got) != "vim.opt.number = true\n" {
		t.Fatalf("bootstrap failed: %q, %v", got, err)
	}
	if !validManagedMarker(filepath.Join(dst, managedMarkerName), "nvim") {
		t.Error("bootstrap did not record its marker")
	}
}

func TestSyncNvimPreservesExistingFileSymlink(t *testing.T) {
	dotsSrc, home := t.TempDir(), t.TempDir()
	dst := filepath.Join(home, ".config", "nvim")
	writeNvimTestFile(t, filepath.Join(dotsSrc, "nvim"), "lua/config/options.lua", "-- new default\n")
	outside := filepath.Join(t.TempDir(), "options.lua")
	writeNvimTestFile(t, filepath.Dir(outside), filepath.Base(outside), "-- external user file\n")
	link := filepath.Join(dst, "lua/config/options.lua")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
	if err := syncNvim(t.Context(), runner, dotsSrc, filepath.Dir(dst), ""); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(link); err != nil || target != outside {
		t.Errorf("existing link changed: %q, %v", target, err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "-- external user file\n" {
		t.Errorf("external user file changed: %q, %v", got, err)
	}
}

func TestSyncNvimRejectsSourceSymlinkBeforeCreatingConfig(t *testing.T) {
	dotsSrc, home := t.TempDir(), t.TempDir()
	src := filepath.Join(dotsSrc, "nvim")
	writeNvimTestFile(t, src, "init.lua", "-- init\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(src, "linked")); err != nil {
		t.Fatal(err)
	}
	runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
	if err := syncNvim(t.Context(), runner, dotsSrc, filepath.Join(home, ".config"), ""); err == nil {
		t.Fatal("expected source symlink rejection")
	}
	if _, err := os.Lstat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Errorf("config changed on failed preflight: %v", err)
	}
}

func TestSyncNvimDryRunDoesNotWrite(t *testing.T) {
	dotsSrc, home := t.TempDir(), t.TempDir()
	writeNvimTestFile(t, filepath.Join(dotsSrc, "nvim"), "init.lua", "-- init\n")
	runner := run.Runner{DryRun: true, Stdout: io.Discard, Stderr: io.Discard}
	if err := syncNvim(t.Context(), runner, dotsSrc, filepath.Join(home, ".config"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Errorf("dry run changed config: %v", err)
	}
}

func TestSyncNvimRejectsParentReplacement(t *testing.T) {
	for _, stage := range []string{"before-config-create", "before-publish", "after-publish"} {
		t.Run(stage, func(t *testing.T) {
			dotsSrc, base := t.TempDir(), t.TempDir()
			home, outside := filepath.Join(base, "home"), filepath.Join(base, "outside")
			writeNvimTestFile(t, home, "sentinel", "home\n")
			writeNvimTestFile(t, outside, "sentinel", "outside\n")
			writeNvimTestFile(t, filepath.Join(dotsSrc, "nvim"), "init.lua", "-- default\n")
			configDir := filepath.Join(home, ".config")
			detached := filepath.Join(base, "detached")
			hook := func(current string) error {
				if current != stage {
					return nil
				}
				target := configDir
				if stage == "before-config-create" {
					target = home
				}
				if err := os.Rename(target, detached); err != nil {
					return err
				}
				return os.Symlink(outside, target)
			}
			runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
			if err := syncNvimWithHook(t.Context(), runner, dotsSrc, configDir, "", hook); err == nil {
				t.Error("expected parent replacement refusal")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
				t.Errorf("outside directory changed: %v, %v", entries, err)
			}
			if _, err := os.Lstat(filepath.Join(detached, "nvim")); !os.IsNotExist(err) {
				t.Errorf("bootstrap was published into detached directory: %v", err)
			}
			if got, err := os.ReadFile(filepath.Join(outside, "sentinel")); err != nil || string(got) != "outside\n" {
				t.Errorf("outside content changed: %q, %v", got, err)
			}
		})
	}
}

func TestSyncNvimRollbackPreservesConcurrentConfig(t *testing.T) {
	dotsSrc, home, outside := t.TempDir(), t.TempDir(), t.TempDir()
	configDir := filepath.Join(home, ".config")
	detached := filepath.Join(home, "detached-config")
	writeNvimTestFile(t, filepath.Join(dotsSrc, "nvim"), "init.lua", "-- default\n")
	hook := func(stage string) error {
		if stage != "after-publish" {
			return nil
		}
		if err := os.Rename(filepath.Join(configDir, "nvim"), filepath.Join(configDir, "saved-bootstrap")); err != nil {
			return err
		}
		writeNvimTestFile(t, filepath.Join(configDir, "nvim"), "custom.lua", "-- concurrent user config\n")
		if err := os.Rename(configDir, detached); err != nil {
			return err
		}
		return os.Symlink(outside, configDir)
	}
	runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
	if err := syncNvimWithHook(t.Context(), runner, dotsSrc, configDir, "", hook); err == nil {
		t.Fatal("expected parent replacement refusal")
	}
	for path, want := range map[string]string{
		"nvim/custom.lua":          "-- concurrent user config\n",
		"saved-bootstrap/init.lua": "-- default\n",
	} {
		got, err := os.ReadFile(filepath.Join(detached, path))
		if err != nil || string(got) != want {
			t.Errorf("rollback changed %s: %q, %v", path, got, err)
		}
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Errorf("outside directory changed: %v, %v", entries, err)
	}
}

func TestSyncNvimRollbackRetainsEditedPublishedFile(t *testing.T) {
	dotsSrc, home, outside := t.TempDir(), t.TempDir(), t.TempDir()
	configDir := filepath.Join(home, ".config")
	detached := filepath.Join(home, "detached-config")
	writeNvimTestFile(t, filepath.Join(dotsSrc, "nvim"), "init.lua", "-- default\n")
	hook := func(stage string) error {
		if stage != "after-publish" {
			return nil
		}
		writeNvimTestFile(t, filepath.Join(configDir, "nvim"), "init.lua", "-- user's intervening edit\n")
		if err := os.Rename(configDir, detached); err != nil {
			return err
		}
		return os.Symlink(outside, configDir)
	}
	runner := run.Runner{Stdout: io.Discard, Stderr: io.Discard}
	err := syncNvimWithHook(t.Context(), runner, dotsSrc, configDir, "", hook)
	if err == nil {
		t.Fatal("expected parent replacement refusal")
	}
	recovered, globErr := filepath.Glob(filepath.Join(detached, ".nvim-bootstrap-*"))
	if globErr != nil || len(recovered) != 1 {
		t.Fatalf("expected one retained recovery directory, got %v, %v", recovered, globErr)
	}
	got, readErr := os.ReadFile(filepath.Join(recovered[0], "init.lua"))
	if readErr != nil || string(got) != "-- user's intervening edit\n" {
		t.Errorf("intervening user edit was lost: %q, %v", got, readErr)
	}
	if !strings.Contains(err.Error(), recovered[0]) {
		t.Errorf("error does not report actual recovery location: %v", err)
	}
}

func TestSyncNvimPreservesRuntimeState(t *testing.T) {
	dotsSrc := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dotsSrc, "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	var out bytes.Buffer
	runner := run.Runner{DryRun: true, Stdout: &out, Stderr: &out}
	if err := syncNvim(context.Background(), runner, dotsSrc, configDir, "tester"); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "rm -rf --") {
		t.Fatalf("neovim sync must not wipe runtime state, got:\n%s", out.String())
	}
}

func TestSyncNvimSkipsSyncWhenSourceAlreadyInstalled(t *testing.T) {
	dotsSrc := t.TempDir()
	src := filepath.Join(dotsSrc, "nvim")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "init.lua"), []byte("-- init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	dst := filepath.Join(configDir, "nvim")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "init.lua"), []byte("-- init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceHash, err := sourceTreeHash(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, ".wahrwelt-managed.json"), []byte(managedMarkerWithSourceHash("nvim", sourceHash)), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	runner := run.Runner{DryRun: true, Stdout: &out, Stderr: &out}
	if err := syncNvim(context.Background(), runner, dotsSrc, configDir, "tester"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, forbidden := range []string{"rsync", "rm -rf --"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("already-installed Neovim should skip %q, got:\n%s", forbidden, got)
		}
	}
}
