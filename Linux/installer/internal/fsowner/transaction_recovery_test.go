//go:build linux

package fsowner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverBarrierRunsAfterRollbackBeforeJournalCleanup(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "flake.nix")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx, err := Begin(root, ".update-", []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(tx, target, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := Recover(tx, func() error {
		called = true
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "before" {
			t.Fatalf("barrier ran before rollback: %q, %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(tx, journalName)); err != nil {
			t.Fatal("journal disappeared before barrier")
		}
		return errors.New("fixture sync failure")
	}); err == nil {
		t.Fatal("ignored barrier failure")
	}
	if !called {
		t.Fatal("barrier was not called")
	}
	if _, err := os.Stat(tx); err != nil {
		t.Fatal("failed barrier removed recovery journal")
	}
	if err := Recover(tx, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tx); !os.IsNotExist(err) {
		t.Fatal("successful recovery did not clean journal")
	}
}

func TestRecoverCommittingTransactionKeepsPublishedContent(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := Begin(root, ".update-", []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(path, target, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx, err := loadTransaction(path)
	if err != nil {
		t.Fatal(err)
	}
	tx.journal.Phase = "committing"
	if err := tx.save(); err != nil {
		t.Fatal(err)
	}
	tx.close()
	if err := Recover(path, func() error {
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "after" {
			t.Fatal("committing recovery rolled back published content")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverRequiresBarrierAndPreservesUnknownDirectory(t *testing.T) {
	path := t.TempDir()
	if err := Recover(path, nil); err == nil {
		t.Fatal("accepted missing durability barrier")
	}
	called := false
	if err := Recover(path, func() error { called = true; return nil }); err == nil {
		t.Fatal("accepted unowned directory")
	}
	if called {
		t.Fatal("called barrier for an unowned directory")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("removed unowned directory")
	}
}
