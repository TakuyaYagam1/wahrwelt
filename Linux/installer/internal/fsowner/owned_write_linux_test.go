//go:build linux

package fsowner

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWriteOwnedPreservesRegularFileOwnershipAndMode(t *testing.T) {
	testWriteOwned(t, os.Geteuid(), os.Getegid())
}

func TestWriteOwnedPreservesAnotherOwnerWhenPrivileged(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("different ownership requires root")
	}
	testWriteOwned(t, 12345, 12346)
}

func testWriteOwned(t *testing.T, uid, gid int) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(target, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(target, uid, gid); err != nil {
		t.Fatal(err)
	}
	tx, err := Begin(root, ".update-", []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOwned(tx, target, []byte("after"), 0o640); err != nil {
		t.Fatal(err)
	}
	assertOwner(t, target, uid, gid, 0o640)
	if err := Rollback(tx); err != nil {
		t.Fatal(err)
	}
	assertOwner(t, target, uid, gid, 0o640)
	if data, err := os.ReadFile(target); err != nil || string(data) != "before" {
		t.Fatal("rollback lost original content")
	}
	if err := WriteOwned(tx, target, []byte("updated again"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Commit(tx); err != nil {
		t.Fatal(err)
	}
	assertOwner(t, target, uid, gid, 0o640)
}

func TestWriteOwnedNewFileBelongsToUpdater(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "wahrwelt-flake.json")
	tx, err := Begin(root, ".update-", []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOwned(tx, target, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(tx); err != nil {
		t.Fatal(err)
	}
	assertOwner(t, target, os.Geteuid(), os.Getegid(), 0o644)
}

func assertOwner(t *testing.T, path string, uid, gid int, mode uint32) {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Uid != uint32(uid) || stat.Gid != uint32(gid) || stat.Mode&0o777 != mode {
		t.Fatalf("file owner/mode = %d:%d %o, want %d:%d %o", stat.Uid, stat.Gid, stat.Mode&0o777, uid, gid, mode)
	}
}
