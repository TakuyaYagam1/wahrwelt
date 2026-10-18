package apply

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var thinPublishedFiles = []string{
	"flake.nix", "flake.lock", FlakeStateName, "host-vars.nix",
	"configuration.nix", "home.nix", "hardware-configuration.nix",
	".wahrwelt-password-hash-enabled",
}

type hostFileSnapshot struct {
	exists bool
	stat   unix.Stat_t
	digest [sha256.Size]byte
}

func readHostFileSnapshot(path string) (hostFileSnapshot, error) {
	if err := ordinaryPathParent(path, "host configuration"); err != nil {
		return hostFileSnapshot{}, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return hostFileSnapshot{}, nil
	}
	if err != nil {
		return hostFileSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return hostFileSnapshot{}, fmt.Errorf("host configuration must be an ordinary file: %s", path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return hostFileSnapshot{}, err
	}
	defer closeFile(file)
	var before, after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return hostFileSnapshot{}, err
	}
	const maxBytes = 16 << 20
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Size > maxBytes {
		return hostFileSnapshot{}, fmt.Errorf("unsupported host configuration file: %s", path)
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, maxBytes+1))
	if err != nil {
		return hostFileSnapshot{}, err
	}
	if err := unix.Lstat(path, &after); err != nil {
		return hostFileSnapshot{}, err
	}
	if size != before.Size || !sameHostFileStat(before, after) {
		return hostFileSnapshot{}, fmt.Errorf("host configuration changed while reading: %s", path)
	}
	return hostFileSnapshot{exists: true, stat: before, digest: [sha256.Size]byte(hash.Sum(nil))}, nil
}

func sameHostFileStat(before, after unix.Stat_t) bool {
	return sameGeneratedStateSnapshot(before, after) && before.Uid == after.Uid && before.Gid == after.Gid
}

// Capture before staging so edits made during a build cannot be overwritten by
// the older candidate. Missing entries are guarded too, including first install.
func captureHostFiles(dest string, layout Layout) (func() error, error) {
	if layout != LayoutThin {
		return nil, nil
	}
	snapshots := make(map[string]hostFileSnapshot, len(thinPublishedFiles))
	for _, name := range thinPublishedFiles {
		snapshot, err := readHostFileSnapshot(filepath.Join(dest, name))
		if err != nil {
			return nil, err
		}
		snapshots[name] = snapshot
	}
	return func() error {
		for _, name := range thinPublishedFiles {
			path := filepath.Join(dest, name)
			current, err := readHostFileSnapshot(path)
			if err != nil {
				return err
			}
			expected := snapshots[name]
			if current.exists != expected.exists || current.digest != expected.digest || !sameHostFileStat(current.stat, expected.stat) {
				return fmt.Errorf("host configuration changed during validation; preserving current file: %s", path)
			}
		}
		return nil
	}, nil
}
