package update

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/fsowner"
	"golang.org/x/sys/unix"
)

const maxFileBytes = 16 << 20

type snapshot struct {
	exists   bool
	identity fsowner.Identity
	mode     os.FileMode
	data     []byte
}

func canonicalDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if resolved != path {
		return fmt.Errorf("ownership collision: directory has symlinked parents: %s", path)
	}
	dir, err := fsowner.OpenDirectory(path)
	if err != nil {
		return err
	}
	return dir.Close()
}

func readSnapshot(path string, allowAbsent bool) (snapshot, error) {
	if err := canonicalDirectory(filepath.Dir(path)); err != nil {
		return snapshot{}, err
	}
	dir, err := fsowner.OpenDirectory(filepath.Dir(path))
	if err != nil {
		return snapshot{}, err
	}
	defer func() { _ = dir.Close() }()
	before, err := dir.Inspect(filepath.Base(path))
	if allowAbsent && errors.Is(err, os.ErrNotExist) {
		return snapshot{identity: fsowner.Identity{Kind: fsowner.KindAbsent}, mode: 0o600}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	if before.Identity.Kind != fsowner.KindRegular || before.Identity.Links != 1 {
		return snapshot{}, fmt.Errorf("ownership collision: expected an ordinary single-link file: %s", path)
	}
	return readOrdinarySnapshot(path, dir, before.Identity)
}

func readOrdinarySnapshot(path string, dir *fsowner.Directory, identity fsowner.Identity) (snapshot, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return snapshot{}, err
	}
	defer func() { _ = file.Close() }()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return snapshot{}, err
	}
	if stat.Dev != identity.Device || stat.Ino != identity.Inode || stat.Nlink != identity.Links {
		return snapshot{}, fmt.Errorf("ownership collision: file changed before reading: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return snapshot{}, err
	}
	if len(data) > maxFileBytes {
		return snapshot{}, fmt.Errorf("configuration file exceeds %d bytes: %s", maxFileBytes, path)
	}
	after, err := dir.Inspect(filepath.Base(path))
	info, statErr := file.Stat()
	if err != nil || statErr != nil || after.Identity != identity || info.Size() != int64(len(data)) {
		return snapshot{}, fmt.Errorf("ownership collision: file changed while reading: %s", path)
	}
	return snapshot{exists: true, identity: identity, mode: info.Mode().Perm(), data: data}, nil
}

func (s snapshot) verify(path string) error {
	current, err := readSnapshot(path, !s.exists)
	if err != nil {
		return err
	}
	if current.exists != s.exists || current.identity != s.identity || !bytes.Equal(current.data, s.data) {
		return fmt.Errorf("ownership collision: configuration changed during update: %s", path)
	}
	return nil
}
