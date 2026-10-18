//go:build linux

package fsowner

import (
	"errors"
	"fmt"
)

// Recover restores an active transaction, or finishes one whose commit already
// began. The caller's durability barrier runs before recovery files are removed.
// A failed barrier leaves the journal available for a later retry.
func Recover(transactionPath string, beforeCommit func() error) error {
	if beforeCommit == nil {
		return errors.New("transaction recovery requires a durability barrier")
	}
	tx, err := loadTransaction(transactionPath)
	if err != nil {
		return err
	}
	phase := tx.journal.Phase
	tx.close()
	if phase == "active" {
		if err := Rollback(transactionPath); err != nil {
			return err
		}
	}
	if err := beforeCommit(); err != nil {
		return fmt.Errorf("sync recovered transaction before cleanup: %w", err)
	}
	return Commit(transactionPath)
}
