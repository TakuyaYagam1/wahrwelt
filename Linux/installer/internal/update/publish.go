package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/fsowner"
	"golang.org/x/sys/unix"
)

const journalPrefix = ".update-"

func lockUpdates(dest string) (string, func(), error) {
	root, err := journalDirectory(dest)
	if err != nil {
		return "", nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, "lock"), os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return "", nil, errors.New("ownership collision: update lock is not a private file")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return "", nil, errors.New("another update is running for this configuration")
	}
	return root, func() { _ = file.Close() }, nil
}

func journalDirectory(dest string) (string, error) {
	root := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+"-updates")
	created := false
	if err := os.Mkdir(root, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := canonicalDirectory(root); err != nil {
		return "", err
	}
	dir, err := fsowner.OpenDirectory(root)
	if err != nil {
		return "", err
	}
	identity := dir.Identity()
	_ = dir.Close()
	if identity.Mode&0o777 != 0o700 || identity.UID != currentUID() {
		return "", errors.New("ownership collision: update journal directory is not private and owned")
	}
	marker := filepath.Join(root, "destination")
	if created {
		if err := writeExclusive(marker, []byte(dest+"\n")); err != nil {
			return "", err
		}
		if err := syncDirectory(root); err != nil {
			return "", err
		}
		if err := syncDirectory(filepath.Dir(root)); err != nil {
			return "", err
		}
	}
	owned, err := readSnapshot(marker, false)
	if err != nil || string(owned.data) != dest+"\n" || owned.identity.UID != currentUID() || owned.mode != 0o600 {
		return "", errors.New("ownership collision: update directory has no matching destination marker")
	}
	return root, nil
}

func recoverUpdates(root, dest string, readOnly bool) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), journalPrefix) {
			continue
		}
		if readOnly {
			return errors.New("interrupted update needs recovery; rerun without --no-switch")
		}
		path := filepath.Join(root, entry.Name())
		if err := fsowner.Recover(path, func() error { return syncDirectory(dest) }); err != nil {
			return fmt.Errorf("interrupted update needs recovery before continuing; preserved %s: %w", path, err)
		}
	}
	return syncDirectory(root)
}

type writeFile func(string, string, []byte, os.FileMode) error

func publish(root string, installed *installation, lock []byte, write writeFile) error {
	contents := map[string][]byte{
		"flake.nix":          []byte(installed.plan.Content),
		"flake.lock":         lock,
		apply.FlakeStateName: installed.plan.State,
	}
	names := []string{"flake.nix", "flake.lock", apply.FlakeStateName}
	var targets []string
	expected := make(map[string]fsowner.Identity)
	for _, name := range names {
		if unchanged(installed.files[name], contents[name]) {
			continue
		}
		path := filepath.Join(installed.dest, name)
		targets = append(targets, path)
		expected[path] = installed.files[name].identity
	}
	if len(targets) == 0 {
		return nil
	}
	tx, err := fsowner.BeginExpected(root, journalPrefix, targets, expected)
	if err != nil {
		return err
	}
	if err := verifyOriginals(installed, names); err != nil {
		return rollback(tx, installed.dest, err)
	}
	if err := backup(root, installed, names); err != nil {
		return rollback(tx, installed.dest, err)
	}
	for _, target := range targets {
		name := filepath.Base(target)
		mode := installed.files[name].mode
		if !installed.files[name].exists {
			mode = 0o644
		}
		if err := write(tx, target, contents[name], mode); err != nil {
			return rollback(tx, installed.dest, err)
		}
	}
	if err := syncDirectory(installed.dest); err != nil {
		return rollback(tx, installed.dest, err)
	}
	if err := fsowner.Commit(tx); err != nil {
		return fmt.Errorf("configuration was published but journal cleanup needs recovery at %s: %w", tx, err)
	}
	return syncDirectory(root)
}

func verifyOriginals(installed *installation, names []string) error {
	for _, name := range names {
		if err := installed.files[name].verify(filepath.Join(installed.dest, name)); err != nil {
			return err
		}
	}
	return nil
}

func rollback(tx, dest string, original error) error {
	if err := fsowner.Rollback(tx); err != nil {
		return errors.Join(original, fmt.Errorf("rollback needs manual recovery at %s: %w", tx, err))
	}
	if err := syncDirectory(dest); err != nil {
		return errors.Join(original, fmt.Errorf("sync rollback recovery at %s: %w", tx, err))
	}
	if err := fsowner.Commit(tx); err != nil {
		return errors.Join(original, fmt.Errorf("rollback completed but journal cleanup failed at %s: %w", tx, err))
	}
	return original
}

func backup(root string, installed *installation, names []string) error {
	dir, err := os.MkdirTemp(root, "backup-")
	if err != nil {
		return err
	}
	var absent []string
	for _, name := range names {
		file := installed.files[name]
		if !file.exists {
			absent = append(absent, name)
			continue
		}
		if err := writeExclusive(filepath.Join(dir, name), file.data); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(struct {
		Version     int      `json:"version"`
		Destination string   `json:"destination"`
		Absent      []string `json:"absent"`
	}{Version: 1, Destination: installed.dest, Absent: absent}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeExclusive(filepath.Join(dir, "backup.json"), append(data, '\n')); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	return syncDirectory(root)
}

func syncDirectory(path string) error {
	dir, err := fsowner.OpenDirectory(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func currentUID() uint32 {
	return uint32(os.Geteuid()) //nolint:gosec // Linux uid_t is the unsigned representation of the effective uid.
}
