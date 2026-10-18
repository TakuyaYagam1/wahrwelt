// Package hosttree stages host-local Nix files without rewriting their contents.
package hosttree

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type treeEntry struct {
	mode   os.FileMode
	size   int64
	digest [sha256.Size]byte
}

// Staging is a sibling of the installed tree, preserving relative paths which
// leave the tree. Symlinks are copied as links, never traversed or rewritten.
type Snapshot struct {
	entries map[string]treeEntry
	skip    []string
	tracked map[string]bool
}

// Copy preserves ordinary local files and symlinks without following links.
// Skip lists exact root names owned by a caller's separate configuration layer.
func Copy(source, target string, skip []string) (Snapshot, error) {
	tracked, err := sourceSet(source)
	if err != nil {
		return Snapshot{}, err
	}
	entries, err := inspectTree(source, target, skip, tracked)
	return Snapshot{entries: entries, skip: append([]string(nil), skip...), tracked: tracked}, err
}

func Verify(source string, snapshot Snapshot) error {
	original := snapshot.entries
	tracked, err := sourceSet(source)
	if err != nil {
		return err
	}
	if !sameSet(tracked, snapshot.tracked) {
		return errors.New("git source selection changed during update")
	}
	current, err := inspectTree(source, "", snapshot.skip, tracked)
	if err != nil {
		return err
	}
	if len(current) != len(original) {
		return errors.New("host configuration changed during update; installed files are unchanged")
	}
	for name, before := range original {
		if after, exists := current[name]; !exists || before != after {
			return fmt.Errorf("host configuration changed during update: %s", name)
		}
	}
	return nil
}

//nolint:gocyclo // One bounded tree walk keeps selection, inspection, and copying in the same order.
func inspectTree(source, target string, skip []string, tracked map[string]bool) (map[string]treeEntry, error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries := make(map[string]treeEntry)
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		_, selected := tracked[rel]
		if rel == ".git" || skipped(rel, skip) || tracked != nil && !selected {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return err
		}
		if tracked != nil && info.IsDir() && !tracked[rel] {
			return fmt.Errorf("git submodules in host configuration need an explicit path input: %s", rel)
		}
		destination := ""
		if target != "" {
			destination = filepath.Join(target, rel)
		}
		copied, err := inspectEntry(root, rel, info, destination)
		if err != nil {
			return err
		}
		entries[rel] = copied
		return nil
	})
	return entries, err
}

func inspectEntry(root *os.Root, rel string, info os.FileInfo, target string) (treeEntry, error) {
	entry := treeEntry{mode: info.Mode(), digest: sha256.Sum256(nil)}
	if info.IsDir() {
		if target != "" {
			return entry, os.Mkdir(target, 0o700)
		}
		return entry, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := root.Readlink(rel)
		if err != nil {
			return treeEntry{}, err
		}
		entry.size, entry.digest = int64(len(link)), sha256.Sum256([]byte(link))
		if target != "" {
			return entry, os.Symlink(link, target)
		}
		return entry, nil
	}
	if !info.Mode().IsRegular() {
		return treeEntry{}, fmt.Errorf("unsupported file in host configuration: %s", rel)
	}
	if strings.HasPrefix(filepath.Base(rel), ".wahrwelt-runtime-stage-") {
		return treeEntry{}, fmt.Errorf("unresolved transaction recovery file: %s", rel)
	}
	return inspectRegular(root, rel, info, target)
}

func inspectRegular(root *os.Root, rel string, info os.FileInfo, target string) (treeEntry, error) {
	file, err := root.OpenFile(rel, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return treeEntry{}, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !sameFileInfo(info, opened) {
		return treeEntry{}, fmt.Errorf("host file changed before staging: %s", rel)
	}
	digest := sha256.New()
	size, err := streamFile(file, target, info.Mode(), digest)
	if err != nil {
		return treeEntry{}, err
	}
	after, err := file.Stat()
	if err != nil || !sameFileInfo(info, after) || info.Size() != size {
		return treeEntry{}, fmt.Errorf("host file changed while staging: %s", rel)
	}
	visible, err := root.Lstat(rel)
	if err != nil || !sameFileInfo(info, visible) {
		return treeEntry{}, fmt.Errorf("host file changed after staging: %s", rel)
	}
	entry := treeEntry{mode: info.Mode(), size: size}
	copy(entry.digest[:], digest.Sum(nil))
	return entry, nil
}

func sameFileInfo(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

// File payloads may include local packages much larger than configuration files.
// Hash and copy through one fixed buffer instead of retaining content in memory.
func streamFile(source io.Reader, target string, mode os.FileMode, digest hash.Hash) (int64, error) {
	var output *os.File
	var destination io.Writer = digest
	if target != "" {
		var err error
		output, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0o600|mode.Perm()&0o111)
		if err != nil {
			return 0, err
		}
		destination = io.MultiWriter(digest, output)
	}
	size, err := io.CopyBuffer(destination, source, make([]byte, 32*1024))
	if output != nil {
		err = errors.Join(err, output.Close())
	}
	return size, err
}

func skipped(name string, skip []string) bool {
	for _, candidate := range skip {
		if name == candidate {
			return true
		}
	}
	return false
}

func sourceSet(path string) (map[string]bool, error) {
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			if dir != path {
				return nil, errors.New("host configuration is inside a larger Git repository; use a repository rooted at the configuration directory")
			}
			return trackedFiles(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if filepath.Dir(dir) == dir {
			return nil, nil
		}
	}
}

func trackedFiles(path string) (map[string]bool, error) {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "-c", "safe.directory="+path, "-C", path, "ls-files", "--cached", "-z")
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read tracked host configuration: %w", err)
	}
	tracked := make(map[string]bool)
	for _, name := range strings.Split(string(data), "\x00") {
		if name == "" {
			continue
		}
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return nil, errors.New("git returned an invalid tracked path")
		}
		tracked[name] = false
		for dir := filepath.Dir(name); dir != "."; dir = filepath.Dir(dir) {
			tracked[dir] = true
		}
	}
	if _, exists := tracked["flake.nix"]; !exists {
		return nil, errors.New("flake.nix must be tracked in the host Git repository")
	}
	return tracked, nil
}

func sameSet(left, right map[string]bool) bool {
	if (left == nil) != (right == nil) || len(left) != len(right) {
		return false
	}
	for name, directory := range left {
		if other, exists := right[name]; !exists || other != directory {
			return false
		}
	}
	return true
}
