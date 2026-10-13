//go:build linux

package fsowner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"golang.org/x/sys/unix"
)

func TestTransactionCommitRemovesPrivateStagesAndJournal(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	target := filepath.Join(root, "runtime", "shell-keybinds.lua")
	mustWritePrivate(t, target, "old\n", 0o644)

	tx, err := Begin(filepath.Join(root, "session"), ".runtime-rollback-", []string{target})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := Write(tx, target, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Commit(tx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if got := mustRead(t, target); got != "new\n" {
		t.Fatalf("target = %q, want new payload", got)
	}
	assertNoRuntimeResidue(t, root)
}

func TestBeginExpectedRejectsReplacementBeforeJournal(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	target := filepath.Join(root, "runtime", "password-hash")
	mustWritePrivate(t, target, "old\n", 0o600)
	parent, err := OpenDirectory(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	oldState, err := inspectFileState(parent.fd, filepath.Base(target))
	_ = parent.Close()
	if err != nil {
		t.Fatal(err)
	}
	replacement := target + ".replacement"
	mustWritePrivate(t, replacement, "new\n", 0o600)
	if err := os.Rename(replacement, target); err != nil {
		t.Fatal(err)
	}

	_, err = BeginExpected(
		filepath.Join(root, "session"),
		".password-hash-v1-",
		[]string{target},
		map[string]Identity{target: oldState.Identity},
	)
	if err == nil || !strings.Contains(err.Error(), "changed before transaction") {
		t.Fatalf("BeginExpected() error = %v, want replacement rejection", err)
	}
	if got := mustRead(t, target); got != "new\n" {
		t.Fatalf("replacement changed: %q", got)
	}
	assertNoRuntimeResidue(t, root)
}

func TestTransactionRollbackRestoresExactOriginalThenCleans(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	target := filepath.Join(root, "runtime", "shell-launcher.lua")
	mustWritePrivate(t, target, "original\n", 0o600)
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := Begin(filepath.Join(root, "session"), ".runtime-rollback-", []string{target})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := Write(tx, target, []byte("replacement\n"), 0o644); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Rollback(tx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	after, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("rollback did not restore the exact original inode")
	}
	if got := mustRead(t, target); got != "original\n" {
		t.Fatalf("target = %q, want original payload", got)
	}
	if got := after.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	if err := Write(tx, target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("unchanged Write() after rollback error = %v", err)
	}
	if err := Write(tx, target, []byte("fallback\n"), 0o644); err != nil {
		t.Fatalf("fallback Write() after rollback error = %v", err)
	}
	if err := Rollback(tx); err != nil {
		t.Fatalf("second Rollback() error = %v", err)
	}
	if err := Commit(tx); err != nil {
		t.Fatalf("Commit() after rollback error = %v", err)
	}
	assertNoRuntimeResidue(t, root)
}

func TestTransactionRollbackRestoresAbsence(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	target := filepath.Join(root, "runtime", "active-shell")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}

	tx, err := Begin(filepath.Join(root, "session"), ".state-switch-rollback-", []string{target})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := Write(tx, target, []byte("noctalia\n"), 0o644); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Rollback(tx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target after rollback error = %v, want absence", err)
	}
	if err := Commit(tx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	assertNoRuntimeResidue(t, root)
}

func TestTransactionRemoveAndRollbackRestoresSymlinkInode(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	target := filepath.Join(root, "runtime", "legacy.conf")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("managed-target", target); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := Begin(filepath.Join(root, "session"), ".runtime-rollback-", []string{target})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := Remove(tx, target); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed target error = %v, want absence", err)
	}
	if err := Rollback(tx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	after, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("rollback did not restore exact symlink inode")
	}
	if link, err := os.Readlink(target); err != nil || link != "managed-target" {
		t.Fatalf("restored link = %q, err=%v", link, err)
	}
	if err := Commit(tx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	assertNoRuntimeResidue(t, root)
}

func TestTransactionPreservesConcurrentTargetReplacement(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	target := filepath.Join(root, "runtime", "shell-keybinds.lua")
	mustWritePrivate(t, target, "old\n", 0o644)

	tx, err := Begin(filepath.Join(root, "session"), ".runtime-rollback-", []string{target})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := Write(tx, target, []byte("managed\n"), 0o644); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	replacement := target + ".concurrent"
	mustWritePrivate(t, replacement, "concurrent\n", 0o644)
	if err := os.Rename(replacement, target); err != nil {
		t.Fatal(err)
	}

	if err := Rollback(tx); err == nil || !strings.Contains(err.Error(), "ownership collision") {
		t.Fatalf("Rollback() error = %v, want ownership collision", err)
	}
	if err := Commit(tx); err == nil || !strings.Contains(err.Error(), "ownership collision") {
		t.Fatalf("Commit() error = %v, want ownership collision", err)
	}
	if got := mustRead(t, target); got != "concurrent\n" {
		t.Fatalf("concurrent winner changed: %q", got)
	}
	if _, err := os.Lstat(tx); err != nil {
		t.Fatalf("recovery journal was not retained: %v", err)
	}
}

func TestScavengeOnlyProcessesSelfIdentifyingJournals(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	session := filepath.Join(root, "session")
	target := filepath.Join(root, "runtime", "shell-launcher.lua")
	mustWritePrivate(t, target, "old\n", 0o644)

	tx, err := Begin(session, ".runtime-rollback-", []string{target})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := Write(tx, target, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	unknown := filepath.Join(session, ".runtime-rollback-unknown")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWritePrivate(t, filepath.Join(unknown, "user-data"), "keep\n", 0o600)

	result, err := Scavenge(session, []string{".runtime-rollback-"})
	if err != nil {
		t.Fatalf("Scavenge() error = %v", err)
	}
	if result.Cleaned != 1 || result.Preserved != 1 {
		t.Fatalf("Scavenge() = %+v, want one cleaned and one preserved", result)
	}
	if got := mustRead(t, target); got != "old\n" {
		t.Fatalf("active transaction was not rolled back: %q", got)
	}
	if got := mustRead(t, filepath.Join(unknown, "user-data")); got != "keep\n" {
		t.Fatalf("unknown recovery changed: %q", got)
	}
	assertNoGlob(t, root, ".wahrwelt-runtime-stage-*")
}

func TestBeginRejectsSymlinkTransactionRoot(t *testing.T) {
	t.Parallel()

	root := privateTempDir(t)
	realRoot := filepath.Join(root, "real-session")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(root, "session")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "runtime", "active-shell")
	mustWritePrivate(t, target, "caelestia\n", 0o644)

	if _, err := Begin(linkedRoot, ".runtime-rollback-", []string{target}); err == nil {
		t.Fatal("Begin() accepted a symlink transaction root")
	}
	entries, err := os.ReadDir(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink root gained entries: %v", entries)
	}
}

func TestTransactionRejectsAmbiguousJSON(t *testing.T) {
	t.Parallel()

	for _, leaf := range []string{markerName, journalName} {
		for _, corruption := range []struct {
			name string
			edit func(string) string
		}{
			{"duplicate version", func(data string) string { return `{"version":2,` + data[1:] }},
			{"invalid UTF-8", func(data string) string { return data[:len(data)-1] + ",\"extra\":\"\xff\"}" }},
		} {
			t.Run(leaf+"/"+corruption.name, func(t *testing.T) {
				t.Parallel()
				txPath, err := Begin(privateTempDir(t), ".runtime-rollback-", nil)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(txPath, leaf)
				mustWritePrivate(t, path, corruption.edit(mustRead(t, path)), 0o600)
				if err := Commit(txPath); err == nil {
					t.Fatal("Commit() accepted ambiguous JSON and removed the journal")
				}
				if _, err := os.Lstat(path); err != nil {
					t.Fatalf("invalid journal was not preserved: %v", err)
				}
			})
		}
	}
}

func TestTransactionReadsLegacyEmptyJournal(t *testing.T) {
	t.Parallel()

	txPath, err := Begin(privateTempDir(t), ".runtime-rollback-", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Existing v1 journals use null for nil entries, not the v2 default [].
	const legacyJournal = `{"version":1,"phase":"active","entries":null}`
	path := filepath.Join(txPath, journalName)
	if got := mustRead(t, path); got != legacyJournal {
		t.Fatalf("empty journal = %s, want historical representation %s", got, legacyJournal)
	}
	// Transactions created before the UUID migration must remain recoverable.
	legacyPath := filepath.Join(filepath.Dir(txPath), ".runtime-rollback-123-000011112222333344445555")
	if err := os.Rename(txPath, legacyPath); err != nil {
		t.Fatal(err)
	}
	if err := Commit(legacyPath); err != nil {
		t.Fatalf("Commit() of legacy v1 journal: %v", err)
	}
}

func TestTransactionRejectsUnsafeJSONLeaves(t *testing.T) {
	t.Parallel()

	for _, leaf := range []string{markerName, journalName} {
		for _, unsafeLeaf := range []struct {
			name  string
			setup func(*testing.T, string)
		}{
			{"public mode", func(t *testing.T, path string) {
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			}},
			{"hardlink", func(t *testing.T, path string) {
				if err := os.Link(path, filepath.Join(t.TempDir(), "linked")); err != nil {
					t.Fatal(err)
				}
			}},
			{"symlink", func(t *testing.T, path string) {
				original := filepath.Join(t.TempDir(), "original")
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, path); err != nil {
					t.Fatal(err)
				}
			}},
			{"too large", func(t *testing.T, path string) {
				mustWritePrivate(t, path, mustRead(t, path)+strings.Repeat(" ", 4<<20), 0o600)
			}},
			{"fifo", func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}},
		} {
			t.Run(leaf+"/"+unsafeLeaf.name, func(t *testing.T) {
				t.Parallel()
				txPath, err := Begin(privateTempDir(t), ".runtime-rollback-", nil)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(txPath, leaf)
				unsafeLeaf.setup(t, path)
				if err := Commit(txPath); err == nil {
					t.Fatal("Commit() accepted unsafe JSON leaf")
				}
				if _, err := os.Lstat(path); err != nil {
					t.Fatalf("unsafe leaf was not preserved: %v", err)
				}
			})
		}
	}
}

func TestRandomNameUsesUniqueUUIDv4Suffix(t *testing.T) {
	t.Parallel()

	const prefix = ".runtime-rollback-"
	wantPrefix := fmt.Sprintf("%s%d-", prefix, os.Getpid())
	seen := make(map[string]bool)
	for range 100 {
		name := randomName(prefix)
		suffix, ok := strings.CutPrefix(name, wantPrefix)
		if !ok {
			t.Fatalf("transaction name %q lost prefix and pid", name)
		}
		id, err := uuid.Parse(suffix)
		if err != nil {
			t.Fatalf("transaction name %q has invalid UUID suffix: %v", name, err)
		}
		if id[6]>>4 != 4 || id[8]>>6 != 2 {
			t.Fatalf("transaction name %q does not use a random RFC 9562 v4 UUID", name)
		}
		if seen[name] {
			t.Fatalf("duplicate transaction name %q", name)
		}
		seen[name] = true
	}
}
