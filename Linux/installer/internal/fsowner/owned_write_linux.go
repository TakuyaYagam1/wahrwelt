//go:build linux

package fsowner

import (
	"errors"
	"io/fs"

	"golang.org/x/sys/unix"
)

// WriteOwned retains an existing regular file's uid and gid while publishing its
// replacement. New files belong to the updater, as with Write.
func WriteOwned(transactionPath, target string, content []byte, mode fs.FileMode) error {
	return write(transactionPath, target, content, mode, true)
}

func createOwnedCandidate(parentFD int, original string, expected Identity, content []byte, mode fs.FileMode) (string, fileState, error) {
	var owner unix.Stat_t
	if err := unix.Fstatat(parentFD, original, &owner, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", fileState{}, err
	}
	if identityFromStat(&owner) != expected {
		return "", fileState{}, errors.New("ownership collision: original file changed before preserving ownership")
	}
	name, state, err := createCandidate(parentFD, content, mode)
	if err != nil {
		return "", fileState{}, err
	}
	created := state.Identity
	success := false
	defer func() {
		if !success {
			removeOwnedCandidate(parentFD, name, created, owner.Uid)
		}
	}()
	if err := chownCandidate(parentFD, name, state.Identity, owner.Uid, owner.Gid, mode); err != nil {
		return "", fileState{}, err
	}
	var after unix.Stat_t
	if err := unix.Fstatat(parentFD, original, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", fileState{}, err
	}
	if identityFromStat(&after) != expected || after.Gid != owner.Gid {
		return "", fileState{}, errors.New("ownership collision: original ownership changed during update")
	}
	state, err = inspectFileState(parentFD, name)
	if err != nil {
		return "", fileState{}, err
	}
	if state.Identity.UID != owner.Uid || state.Digest != digestBytes(content) {
		return "", fileState{}, errors.New("ownership collision: replacement changed while preserving ownership")
	}
	success = true
	return name, state, nil
}

func removeOwnedCandidate(parentFD int, name string, created Identity, uid uint32) {
	current, err := inspectFileState(parentFD, name)
	if err != nil || current.Identity.Device != created.Device || current.Identity.Inode != created.Inode || current.Identity.Kind != KindRegular {
		return
	}
	if current.Identity.UID == created.UID || current.Identity.UID == uid {
		_ = unlinkFileState(parentFD, name, current, false)
	}
}

func chownCandidate(parentFD int, name string, expected Identity, uid, gid uint32, mode fs.FileMode) error {
	fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	identity, err := identityFromFD(fd)
	if err != nil {
		return err
	}
	if identity != expected {
		return errors.New("ownership collision: replacement changed before preserving ownership")
	}
	if err := unix.Fchown(fd, int(uid), int(gid)); err != nil {
		return err
	}
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return err
	}
	return unix.Fsync(fd)
}
