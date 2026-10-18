//go:build linux

package fsowner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"uuid"

	"golang.org/x/sys/unix"
)

const (
	markerName  = ".wahrwelt-fsowner-v1"
	journalName = ".wahrwelt-transaction-v1.json"
)

type fileState struct {
	Identity Identity `json:"identity"`
	Digest   string   `json:"digest,omitempty"`
}

type journalEntry struct {
	Path          string     `json:"path"`
	Parent        Identity   `json:"parent"`
	Original      fileState  `json:"original"`
	Current       fileState  `json:"current"`
	Recovery      string     `json:"recovery,omitempty"`
	RecoveryState *fileState `json:"recoveryState,omitempty"`
}

type transactionJournal struct {
	Version int            `json:"version"`
	Phase   string         `json:"phase"`
	Entries []journalEntry `json:"entries"`
}

type transactionMarker struct {
	Version     int      `json:"version"`
	UID         uint32   `json:"uid"`
	Root        Identity `json:"root"`
	Transaction Identity `json:"transaction"`
	Prefix      string   `json:"prefix"`
}

type transaction struct {
	path, name string
	root, dir  *Directory
	marker     transactionMarker
	journal    transactionJournal
}

type ScavengeResult struct {
	Cleaned, Preserved int
	Issues             []string
}

// Begin snapshots exact regular-file identities or absence into one private,
// marker-owned journal. Contents remain in exact renameat2 recovery inodes.
func Begin(rootPath, prefix string, targets []string) (string, error) {
	return begin(rootPath, prefix, targets, nil)
}

// BeginExpected starts a transaction only when every target still has the
// exact no-follow identity supplied by the caller. The subsequent Write also
// checks the complete snapshotted state, including its digest.
func BeginExpected(rootPath, prefix string, targets []string, expected map[string]Identity) (string, error) {
	if len(expected) != len(targets) {
		return "", errors.New("expected transaction identities do not match target count")
	}
	return begin(rootPath, prefix, targets, expected)
}

//nolint:gocyclo // The marker creation and identity snapshots form one fail-closed transaction state machine.
func begin(rootPath, prefix string, targets []string, expected map[string]Identity) (string, error) {
	if err := validatePrefix(prefix); err != nil {
		return "", err
	}
	root, err := ensurePrivateDirectory(rootPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	uid := effectiveUID()
	if root.identity.UID != uid || root.identity.Mode&0o077 != 0 {
		return "", fmt.Errorf("ownership collision: transaction root %s is not private", root.path)
	}
	name, err := mkdirRandomAt(root.fd, prefix)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root.path, name)
	dir, err := OpenDirectory(path)
	if err != nil {
		return "", err
	}
	created := true
	defer func() {
		_ = dir.Close()
		if created {
			_ = root.RemoveDirectory(name, dir.identity, RemoveOptions{UID: uid, Recursive: true, SameDevice: true})
		}
	}()
	journal := transactionJournal{Version: 1, Phase: "active"}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		absolute, absErr := filepath.Abs(target)
		if absErr != nil || absolute != filepath.Clean(target) {
			return "", fmt.Errorf("transaction target must be an absolute clean path: %s", target)
		}
		if _, exists := seen[absolute]; exists {
			return "", fmt.Errorf("duplicate transaction target: %s", absolute)
		}
		seen[absolute] = struct{}{}
		parent, openErr := OpenDirectory(filepath.Dir(absolute))
		if openErr != nil {
			return "", openErr
		}
		state, inspectErr := inspectFileState(parent.fd, filepath.Base(absolute))
		parentIdentity := parent.identity
		_ = parent.Close()
		if inspectErr != nil {
			return "", fmt.Errorf("snapshot %s: %w", absolute, inspectErr)
		}
		if expected != nil {
			wanted, ok := expected[absolute]
			if !ok || state.Identity != wanted {
				return "", fmt.Errorf("ownership collision: target %s changed before transaction", absolute)
			}
		}
		journal.Entries = append(journal.Entries, journalEntry{Path: absolute, Parent: parentIdentity, Original: state, Current: state})
	}
	marker := transactionMarker{1, uid, root.identity, dir.identity, prefix}
	if err := writeJSONAt(dir.fd, markerName, marker); err != nil {
		return "", err
	}
	if err := writeJSONAt(dir.fd, journalName, journal); err != nil {
		return "", err
	}
	created = false
	return path, nil
}

func Matches(target string, content []byte, mode fs.FileMode) (bool, error) {
	parent, err := OpenDirectory(filepath.Dir(target))
	if err != nil {
		return false, err
	}
	defer func() { _ = parent.Close() }()
	state, err := inspectFileState(parent.fd, filepath.Base(target))
	return err == nil && state.Identity.Kind == KindRegular &&
		state.Identity.Mode&0o777 == uint32(mode.Perm()) && state.Digest == digestBytes(content), err
}

// Write replaces one unchanged transaction member. A second replacement must
// first rollback or start a new transaction.
func Write(transactionPath, target string, content []byte, mode fs.FileMode) error {
	return write(transactionPath, target, content, mode, false)
}

//nolint:gocyclo // Exchange, recovery, and journal publication are one auditable transaction state machine.
func write(transactionPath, target string, content []byte, mode fs.FileMode, preserveOwner bool) error {
	tx, err := loadTransaction(transactionPath)
	if err != nil {
		return err
	}
	defer tx.close()
	entry := tx.entry(target)
	if entry == nil || tx.journal.Phase != "active" {
		return fmt.Errorf("target is outside an active transaction: %s", target)
	}
	parent, current, err := openTrackedTarget(*entry)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	if !sameFileState(current, entry.Original) {
		return fmt.Errorf("ownership collision: target %s was already replaced", target)
	}
	if current.Identity.Kind == KindRegular && current.Identity.Mode&0o777 == uint32(mode.Perm()) && current.Digest == digestBytes(content) {
		return nil
	}
	if entry.Recovery != "" {
		if entry.RecoveryState == nil {
			return fmt.Errorf("ownership collision: target %s has an invalid recovery", target)
		}
		if err := unlinkFileState(parent.fd, entry.Recovery, *entry.RecoveryState, false); err != nil {
			return fmt.Errorf("cleanup prior rolled-back result for %s: %w", target, err)
		}
		entry.Recovery, entry.RecoveryState = "", nil
		if err := tx.save(); err != nil {
			return err
		}
	}
	var candidate string
	var candidateState fileState
	if preserveOwner && current.Identity.Kind == KindRegular {
		candidate, candidateState, err = createOwnedCandidate(parent.fd, filepath.Base(target), current.Identity, content, mode)
	} else {
		candidate, candidateState, err = createCandidate(parent.fd, content, mode)
	}
	if err != nil {
		return err
	}
	base := filepath.Base(target)
	if current.Identity.Kind == KindAbsent {
		err = unix.Renameat2(parent.fd, candidate, parent.fd, base, unix.RENAME_NOREPLACE)
	} else {
		err = unix.Renameat2(parent.fd, base, parent.fd, candidate, unix.RENAME_EXCHANGE)
	}
	if err != nil {
		_ = unlinkFileState(parent.fd, candidate, candidateState, true)
		return fmt.Errorf("publish %s: %w", target, err)
	}
	published, inspectErr := inspectFileState(parent.fd, base)
	if inspectErr != nil || !sameFileState(published, candidateState) {
		return fmt.Errorf("ownership collision: published target %s changed", target)
	}
	entry.Current = published
	if current.Identity.Kind != KindAbsent {
		moved, movedErr := inspectFileState(parent.fd, candidate)
		if movedErr != nil || !sameFileState(moved, current) {
			return fmt.Errorf("ownership collision: recovery changed for %s", target)
		}
		entry.Recovery, entry.RecoveryState = candidate, &moved
	}
	if err := tx.save(); err != nil {
		if current.Identity.Kind == KindAbsent {
			_ = unix.Renameat2(parent.fd, base, parent.fd, candidate, unix.RENAME_NOREPLACE)
		} else {
			_ = unix.Renameat2(parent.fd, base, parent.fd, candidate, unix.RENAME_EXCHANGE)
		}
		_ = unlinkFileState(parent.fd, candidate, candidateState, true)
		return err
	}
	return nil
}

// Remove hides one unchanged transaction member while retaining its exact
// inode for rollback.
func Remove(transactionPath, target string) error {
	tx, err := loadTransaction(transactionPath)
	if err != nil {
		return err
	}
	defer tx.close()
	entry := tx.entry(target)
	if entry == nil || tx.journal.Phase != "active" || entry.Recovery != "" || !sameFileState(entry.Current, entry.Original) {
		return fmt.Errorf("target is outside an unchanged active transaction: %s", target)
	}
	parent, current, err := openTrackedTarget(*entry)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	if current.Identity.Kind == KindAbsent {
		return nil
	}
	stage := randomName(".wahrwelt-runtime-stage-")
	if err := unix.Renameat2(parent.fd, filepath.Base(target), parent.fd, stage, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("remove tracked target %s: %w", target, err)
	}
	entry.Current, entry.Recovery, entry.RecoveryState = absentState(), stage, &current
	if err := tx.save(); err != nil {
		_ = unix.Renameat2(parent.fd, stage, parent.fd, filepath.Base(target), unix.RENAME_NOREPLACE)
		return err
	}
	return nil
}

//nolint:gocyclo // Rollback intentionally serializes every recovery state transition and verification.
func Rollback(transactionPath string) error {
	tx, err := loadTransaction(transactionPath)
	if err != nil {
		return err
	}
	defer tx.close()
	if tx.journal.Phase != "active" {
		return errors.New("transaction is already committing")
	}
	for index := range tx.journal.Entries {
		entry := &tx.journal.Entries[index]
		parent, current, openErr := openTrackedTarget(*entry)
		if openErr != nil {
			return openErr
		}
		if sameFileState(current, entry.Original) {
			_ = parent.Close()
			continue
		}
		base := filepath.Base(entry.Path)
		if entry.Original.Identity.Kind == KindAbsent {
			stage := randomName(".wahrwelt-runtime-stage-")
			if err := unix.Renameat2(parent.fd, base, parent.fd, stage, unix.RENAME_NOREPLACE); err != nil {
				_ = parent.Close()
				return fmt.Errorf("rollback %s: %w", entry.Path, err)
			}
			entry.Current, entry.Recovery, entry.RecoveryState = absentState(), stage, &current
		} else {
			if entry.Recovery == "" || entry.RecoveryState == nil || !sameFileState(*entry.RecoveryState, entry.Original) {
				_ = parent.Close()
				return fmt.Errorf("ownership collision: original recovery is unavailable for %s", entry.Path)
			}
			recovery, recoveryErr := inspectFileState(parent.fd, entry.Recovery)
			if recoveryErr != nil || !sameFileState(recovery, entry.Original) {
				_ = parent.Close()
				return fmt.Errorf("ownership collision: recovery changed for %s", entry.Path)
			}
			if current.Identity.Kind == KindAbsent {
				if err := unix.Renameat2(parent.fd, entry.Recovery, parent.fd, base, unix.RENAME_NOREPLACE); err != nil {
					_ = parent.Close()
					return fmt.Errorf("rollback %s: %w", entry.Path, err)
				}
				entry.Current, entry.Recovery, entry.RecoveryState = entry.Original, "", nil
			} else {
				if err := unix.Renameat2(parent.fd, base, parent.fd, entry.Recovery, unix.RENAME_EXCHANGE); err != nil {
					_ = parent.Close()
					return fmt.Errorf("rollback %s: %w", entry.Path, err)
				}
				entry.Current, entry.RecoveryState = entry.Original, &current
			}
		}
		if err := tx.save(); err != nil {
			_ = parent.Close()
			return err
		}
		_ = parent.Close()
	}
	return nil
}

//nolint:gocyclo // Commit intentionally serializes journal phase changes and exact recovery cleanup.
func Commit(transactionPath string) error {
	tx, err := loadTransaction(transactionPath)
	if err != nil {
		return err
	}
	defer tx.close()
	if tx.journal.Phase == "active" {
		for _, entry := range tx.journal.Entries {
			parent, _, openErr := openTrackedTarget(entry)
			if parent != nil {
				_ = parent.Close()
			}
			if openErr != nil {
				return openErr
			}
		}
		tx.journal.Phase = "committing"
		if err := tx.save(); err != nil {
			return err
		}
	}
	for index := range tx.journal.Entries {
		entry := &tx.journal.Entries[index]
		if entry.Recovery == "" || entry.RecoveryState == nil {
			continue
		}
		parent, openErr := OpenDirectory(filepath.Dir(entry.Path))
		if openErr != nil {
			return openErr
		}
		removeErr := unlinkFileState(parent.fd, entry.Recovery, *entry.RecoveryState, true)
		_ = parent.Close()
		if removeErr != nil {
			return fmt.Errorf("cleanup retained recovery for %s: %w", entry.Path, removeErr)
		}
		entry.Recovery, entry.RecoveryState = "", nil
		if err := tx.save(); err != nil {
			return err
		}
	}
	if err := tx.verifyContents(); err != nil {
		return err
	}
	journal, err := tx.dir.Inspect(journalName)
	if err != nil {
		return err
	}
	marker, err := tx.dir.Inspect(markerName)
	if err != nil {
		return err
	}
	if err := tx.dir.RemoveRegular(journalName, journal.Identity, RemoveOptions{UID: tx.marker.UID}); err != nil {
		return err
	}
	if err := tx.dir.RemoveRegular(markerName, marker.Identity, RemoveOptions{UID: tx.marker.UID}); err != nil {
		return err
	}
	return tx.root.RemoveDirectory(tx.name, tx.marker.Transaction, RemoveOptions{UID: tx.marker.UID, RequireEmpty: true})
}

// Scavenge touches only self-identifying v1 journals. Unknown matching
// directories are reported and preserved.
func Scavenge(rootPath string, prefixes []string) (ScavengeResult, error) {
	root, err := OpenDirectory(rootPath)
	if err != nil {
		return ScavengeResult{}, err
	}
	defer func() { _ = root.Close() }()
	entries, err := root.List(func(name string) bool {
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	})
	if err != nil {
		return ScavengeResult{}, err
	}
	result := ScavengeResult{}
	for _, entry := range entries {
		path := filepath.Join(root.path, entry.Name)
		tx, loadErr := loadTransaction(path)
		if loadErr != nil || tx.marker.Transaction != entry.Identity {
			if tx != nil {
				tx.close()
			}
			result.Preserved++
			result.Issues = append(result.Issues, fmt.Sprintf("preserved %s: %v", path, loadErr))
			continue
		}
		phase := tx.journal.Phase
		tx.close()
		if phase == "active" {
			loadErr = Rollback(path)
		}
		if loadErr == nil {
			loadErr = Commit(path)
		}
		if loadErr != nil {
			result.Preserved++
			result.Issues = append(result.Issues, fmt.Sprintf("preserved %s: %v", path, loadErr))
		} else {
			result.Cleaned++
		}
	}
	return result, nil
}

func loadTransaction(path string) (*transaction, error) {
	absolute, _ := filepath.Abs(path)
	root, err := OpenDirectory(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	dir, err := OpenDirectory(absolute)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	tx := &transaction{path: absolute, name: filepath.Base(absolute), root: root, dir: dir}
	if tx.marker, err = dir.readJSON[transactionMarker](markerName); err != nil {
		tx.close()
		return nil, fmt.Errorf("not a marker-owned v1 transaction: %w", err)
	}
	if tx.journal, err = dir.readJSON[transactionJournal](journalName); err != nil {
		tx.close()
		return nil, fmt.Errorf("invalid marker-owned v1 journal: %w", err)
	}
	if tx.marker.Version != 1 || tx.journal.Version != 1 || tx.marker.UID != effectiveUID() ||
		!sameObject(tx.marker.Root, root.identity) || !sameObject(tx.marker.Transaction, dir.identity) ||
		(tx.journal.Phase != "active" && tx.journal.Phase != "committing") || !strings.HasPrefix(tx.name, tx.marker.Prefix) {
		tx.close()
		return nil, errors.New("ownership collision: transaction marker does not match pinned directories")
	}
	if err := tx.verifyContents(); err != nil {
		tx.close()
		return nil, err
	}
	return tx, nil
}

func (tx *transaction) entry(path string) *journalEntry {
	for index := range tx.journal.Entries {
		if tx.journal.Entries[index].Path == path {
			return &tx.journal.Entries[index]
		}
	}
	return nil
}

func (tx *transaction) save() error {
	temporary := randomName(".journal-next-")
	if err := writeJSONAt(tx.dir.fd, temporary, tx.journal); err != nil {
		return err
	}
	if err := unix.Renameat(tx.dir.fd, temporary, tx.dir.fd, journalName); err != nil {
		_ = unix.Unlinkat(tx.dir.fd, temporary, 0)
		return fmt.Errorf("replace transaction journal: %w", err)
	}
	return unix.Fsync(tx.dir.fd)
}

func (tx *transaction) verifyContents() error {
	entries, err := tx.dir.List(nil)
	if err != nil || len(entries) != 2 || entries[0].Name != markerName || entries[1].Name != journalName {
		return fmt.Errorf("ownership collision: transaction %s contains unknown entries", tx.path)
	}
	return nil
}

func (tx *transaction) close() { _ = tx.dir.Close(); _ = tx.root.Close() }

func openTrackedTarget(entry journalEntry) (*Directory, fileState, error) {
	parent, err := OpenDirectory(filepath.Dir(entry.Path))
	if err != nil {
		return nil, fileState{}, err
	}
	if !sameObject(parent.identity, entry.Parent) {
		_ = parent.Close()
		return nil, fileState{}, fmt.Errorf("ownership collision: parent changed for %s", entry.Path)
	}
	current, err := inspectFileState(parent.fd, filepath.Base(entry.Path))
	if err != nil || !sameFileState(current, entry.Current) {
		_ = parent.Close()
		return nil, fileState{}, fmt.Errorf("ownership collision: target changed for %s", entry.Path)
	}
	return parent, current, nil
}

func inspectFileState(parentFD int, name string) (fileState, error) {
	identity, err := identityAt(parentFD, name)
	if errors.Is(err, unix.ENOENT) {
		return absentState(), nil
	}
	if err != nil {
		return fileState{}, err
	}
	if identity.Kind == KindSymlink {
		buffer := make([]byte, 4096)
		n, readErr := unix.Readlinkat(parentFD, name, buffer)
		after, visibleErr := identityAt(parentFD, name)
		if readErr != nil || visibleErr != nil || after != identity {
			return fileState{}, errors.New("symlink changed during inspection")
		}
		return fileState{Identity: identity, Digest: digestBytes(buffer[:n])}, nil
	}
	if identity.Kind != KindRegular || identity.Links != 1 {
		return fileState{}, errors.New("target is not an ordinary private regular file, symlink, or absent")
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fileState{}, err
	}
	data, readErr := readFD(fd, 16<<20)
	after, statErr := identityFromFD(fd)
	_ = unix.Close(fd)
	visible, visibleErr := identityAt(parentFD, name)
	if readErr != nil || statErr != nil || visibleErr != nil || after != identity || visible != identity {
		return fileState{}, errors.New("regular file changed during inspection")
	}
	return fileState{Identity: identity, Digest: digestBytes(data)}, nil
}

func createCandidate(parentFD int, content []byte, mode fs.FileMode) (string, fileState, error) {
	name := randomName(".wahrwelt-runtime-stage-")
	fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", fileState{}, err
	}
	ok := false
	defer func() {
		_ = unix.Close(fd)
		if !ok {
			_ = unix.Unlinkat(parentFD, name, 0)
		}
	}()
	for written := 0; written < len(content); {
		n, writeErr := unix.Write(fd, content[written:])
		if writeErr != nil {
			return "", fileState{}, writeErr
		}
		written += n
	}
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return "", fileState{}, err
	}
	if err := unix.Fsync(fd); err != nil {
		return "", fileState{}, err
	}
	state, err := inspectFileState(parentFD, name)
	ok = err == nil
	return name, state, err
}

func unlinkFileState(parentFD int, name string, expected fileState, allowAbsent bool) error {
	current, err := inspectFileState(parentFD, name)
	if allowAbsent && current.Identity.Kind == KindAbsent {
		return nil
	}
	if err != nil || !sameFileState(current, expected) {
		return errors.New("ownership collision: retained recovery changed")
	}
	return unix.Unlinkat(parentFD, name, 0)
}

func ensurePrivateDirectory(path string) (*Directory, error) {
	if directory, err := OpenDirectory(path); err == nil {
		return directory, nil
	}
	parent, err := OpenDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	if err := unix.Mkdirat(parent.fd, filepath.Base(path), 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, err
	}
	return OpenDirectory(path)
}

func mkdirRandomAt(parentFD int, prefix string) (string, error) {
	for range 32 {
		name := randomName(prefix)
		if err := unix.Mkdirat(parentFD, name, 0o700); err == nil {
			return name, nil
		} else if !errors.Is(err, unix.EEXIST) {
			return "", err
		}
	}
	return "", errors.New("exhausted private transaction names")
}

func randomName(prefix string) string {
	return prefix + strconv.Itoa(os.Getpid()) + "-" + uuid.NewV4().String()
}

func validatePrefix(prefix string) error {
	if len(prefix) < 2 || len(prefix) > 80 || prefix[0] != '.' || strings.ContainsAny(prefix, "/\x00") {
		return fmt.Errorf("invalid transaction prefix %q", prefix)
	}
	for _, char := range prefix {
		if char != '.' && char != '-' && char != '_' && (char < '0' || char > '9') &&
			(char < 'A' || char > 'Z') && (char < 'a' || char > 'z') {
			return fmt.Errorf("invalid transaction prefix %q", prefix)
		}
	}
	return nil
}

func absentState() fileState { return fileState{Identity: Identity{Kind: KindAbsent}} }
func sameFileState(left, right fileState) bool {
	return left.Identity == right.Identity && left.Digest == right.Digest
}
func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func readFD(fd, limit int) ([]byte, error) {
	result, buffer := make([]byte, 0, 4096), make([]byte, 32*1024)
	for len(result) <= limit {
		n, err := unix.Read(fd, buffer)
		result = append(result, buffer[:n]...)
		if err != nil || n == 0 {
			return result, err
		}
	}
	return nil, errors.New("owned file exceeds size limit")
}

func writeJSONAt(parentFD int, name string, value any) error {
	// Keep the v1 disk representation, including null slices and omitempty.
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	for written := 0; written < len(data); {
		n, writeErr := unix.Write(fd, data[written:])
		if writeErr != nil {
			return writeErr
		}
		written += n
	}
	return unix.Fsync(fd)
}

func (d *Directory) readJSON[T transactionMarker | transactionJournal](name string) (T, error) {
	var value T
	fd, err := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return value, err
	}
	identity, inspectErr := identityFromFD(fd)
	data, readErr := readFD(fd, 4<<20)
	_ = unix.Close(fd)
	if inspectErr != nil || readErr != nil || identity.Kind != KindRegular || identity.Links != 1 ||
		identity.UID != effectiveUID() || identity.Mode&0o777 != 0o600 {
		return value, errors.New("journal leaf is not a private owned regular file")
	}
	// Transaction metadata must not silently accept duplicate keys or invalid UTF-8.
	if err := jsonv2.Unmarshal(data, &value); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

func effectiveUID() uint32 {
	return uint32(os.Geteuid()) //nolint:gosec // Linux uid_t is an unsigned 32-bit value exposed by os as int.
}
