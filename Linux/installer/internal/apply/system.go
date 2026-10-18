package apply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
	"golang.org/x/sys/unix"
)

type systemWriteResult struct {
	BackupPath           string
	skipAutomaticRestore bool
}

type legacyUserMigrationCollisionError struct {
	path string
}

func closeFile(file *os.File) {
	if file != nil {
		_ = file.Close()
	}
}

func closeFileDirectory(directory interface{ Close() error }) {
	if directory != nil {
		_ = directory.Close()
	}
}

func currentEffectiveUID() uint32 {
	return uint32(os.Geteuid()) //nolint:gosec // Linux uid_t is an unsigned 32-bit value exposed by os as int.
}

func (e *legacyUserMigrationCollisionError) Error() string {
	return fmt.Sprintf("cannot migrate legacy user directory: target appeared during migration: %s", e.path)
}

func writeSystemConfiguration(ctx context.Context, runner run.CommandRunner, staging string, opts Options, layout Layout, guards ...func() error) (systemWriteResult, error) {
	var result systemWriteResult
	dest := opts.Paths.NixOSDest
	backupPath, err := backupExisting(ctx, runner, dest)
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath
	if err := verifySystemWrite(guards); err != nil {
		// A broad restore would overwrite the user edit being preserved.
		result.skipAutomaticRestore = true
		return result, err
	}
	migratedLegacyUser, err := migrateLegacyUserDirectoryWithResult(ctx, runner, dest)
	if err != nil {
		// The migration is the first destination mutation after the backup and
		// uses one no-copy atomic rename.  If it does not report success, a broad
		// rsync --delete rollback is both unnecessary and unsafe: it could erase a
		// canonical user tree or another file created concurrently after backup.
		result.skipAutomaticRestore = true
		return result, err
	}
	// A successful private -> user rename is a committed namespace migration.
	// The older backup has private/ and no user/, so a later broad restore would
	// delete the canonical tree and can erase data created after the rename.
	result.skipAutomaticRestore = migratedLegacyUser
	if layout == LayoutFull {
		if err := preserveHardware(ctx, runner, dest); err != nil {
			return result, err
		}
	}
	if err := verifySystemWrite(guards); err != nil {
		result.skipAutomaticRestore = true
		return result, err
	}
	if err := syncToEtc(ctx, runner, staging, dest, layout); err != nil {
		return result, err
	}
	if err := writeStagedUserDefault(ctx, runner, staging, dest, layout); err != nil {
		return result, err
	}
	if err := writeStagedSecrets(ctx, runner, staging, dest, layout); err != nil {
		return result, err
	}
	if err := writeStagedHashedPassword(ctx, runner, staging, dest, opts.Secrets, layout); err != nil {
		return result, err
	}
	return result, nil
}

func verifySystemWrite(guards []func() error) error {
	for _, verify := range guards {
		if verify != nil {
			if err := verify(); err != nil {
				return err
			}
		}
	}
	return nil
}

func syncToEtc(ctx context.Context, runner run.CommandRunner, staging, dest string, layout Layout) error {
	return runner.Command(ctx, "sudo", syncToEtcArgs(staging, dest, layout)...)
}

func syncToEtcArgs(staging, dest string, layout ...Layout) []string {
	targetLayout := LayoutThin
	if len(layout) > 0 {
		targetLayout = layout[0]
	}
	if targetLayout == LayoutFull {
		return syncToEtcFullArgs(staging, dest)
	}
	args := []string{
		"rsync", "-a", "--checksum", "--chmod=u+w",
		"--exclude=/installer-state.json",
		"--exclude=.wahrwelt-installer-recovery-*/",
		"--exclude=/user/",
		"--exclude=/wahrwelt/",
		"--exclude=/mysetup/",
		"--exclude=/secrets/",
		"--exclude=/hashed-password.nix",
	}
	for _, name := range thinPublishedFiles {
		args = append(args, "--include=/"+name)
	}
	return append(args, "--exclude=/*", staging+"/", dest+"/")
}

func syncToEtcFullArgs(staging, dest string) []string {
	return []string{
		"rsync", "-a", "--delete", "--checksum", "--chmod=u+w",
		"--exclude=/installer-state.json",
		"--exclude=.wahrwelt-installer-recovery-*/",
		"--exclude=/user/",
		"--exclude=/wahrwelt/",
		"--exclude=/mysetup/",
		"--exclude=/secrets/",
		"--exclude=hardware-configuration.nix",
		"--exclude=hosts/NixOS/hardware-configuration.nix",
		"--exclude=hosts/NixOS/hashed-password.nix",
		"--exclude=hosts/NixOS/secrets/secrets.yaml",
		"--exclude=home/secrets/secrets.yaml",
		staging + "/",
		dest + "/",
	}
}

func lockStagingFlake(ctx context.Context, runner run.CommandRunner, staging string, layout Layout, lockMode LockMode) error {
	if layout != LayoutThin {
		return nil
	}
	args := []string{
		"--extra-experimental-features",
		"nix-command flakes",
		"flake",
		"update",
	}
	if lockMode == LockModeManaged {
		args = append(args, "wahrwelt")
	}
	args = append(args, "--flake", "path:.")
	if runner.IsDryRun() {
		return runner.Command(ctx, "nix", args...)
	}
	pinnedRunner, ok := runner.(run.PinnedDirectoryOutputRunner)
	if !ok {
		return fmt.Errorf("flake lock runner cannot use a pinned staging directory")
	}
	stagingDirectory, err := openValidationDirectory(staging)
	if err != nil {
		return err
	}
	defer closeFile(stagingDirectory)
	_, err = pinnedRunner.OutputInPinnedDirectory(ctx, stagingDirectory, "nix", args...)
	return err
}

func preserveHardware(ctx context.Context, runner run.CommandRunner, dest string) error {
	rootHW := filepath.Join(dest, "hardware-configuration.nix")
	hostHW := filepath.Join(dest, "hosts/NixOS/hardware-configuration.nix")
	if _, err := os.Stat(rootHW); err == nil {
		return runner.Command(ctx, "sudo", "install", "-D", "-m", "644", rootHW, hostHW)
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(hostHW); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return fmt.Errorf("hardware-configuration.nix not found; run sudo nixos-generate-config --root / first")
}

type pinnedRegularFile struct {
	file *os.File
	info os.FileInfo
}

const privilegedPublishTimeout = 10 * time.Second

func privilegedPythonPath() (string, error) {
	configured := strings.TrimSpace(os.Getenv("WAHRWELT_PRIVILEGED_PYTHON"))
	if configured != "" {
		path, err := trustedPrivilegedExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("untrusted privileged python %s: %w", configured, err)
		}
		return path, nil
	}
	candidates := []string{"/run/current-system/sw/bin/python3", "/usr/bin/python3"}
	if path, err := exec.LookPath("python3"); err == nil {
		candidates = append(candidates, path)
	}
	var failures []error
	for _, candidate := range candidates {
		path, err := trustedPrivilegedExecutable(candidate)
		if err == nil {
			return path, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", candidate, err))
	}
	return "", fmt.Errorf("locate trusted python3 for privileged directory pinning: %w", errors.Join(failures...))
}

func privilegedFSHelperPath() (string, error) {
	configured := strings.TrimSpace(os.Getenv("WAHRWELT_PRIVILEGED_FS_HELPER"))
	if configured != "" {
		path, err := trustedPrivilegedExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("untrusted privileged filesystem helper %s: %w", configured, err)
		}
		return path, nil
	}
	candidates := []string{
		"/run/current-system/sw/bin/wahrwelt-fs-helper",
		"/nix/var/nix/profiles/default/bin/wahrwelt-fs-helper",
	}
	if path, err := exec.LookPath("wahrwelt-fs-helper"); err == nil {
		candidates = append(candidates, path)
	}
	var failures []error
	for _, candidate := range candidates {
		path, err := trustedPrivilegedExecutable(candidate)
		if err == nil {
			return path, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", candidate, err))
	}
	return "", fmt.Errorf("locate trusted privileged filesystem helper: %w", errors.Join(failures...))
}

//nolint:gocyclo // Trust validation is intentionally an explicit, linear checklist for auditability.
func trustedPrivilegedExecutable(path string) (string, error) {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = absolute
	}
	path = filepath.Clean(path)
	if !trustedExecutableInvocationLocation(path) {
		return "", fmt.Errorf("invocation path is outside trusted system executable locations")
	}
	var invocation unix.Stat_t
	if err := unix.Lstat(path, &invocation); err != nil {
		return "", err
	}
	if invocation.Uid != 0 || (invocation.Mode&unix.S_IFMT != unix.S_IFREG && invocation.Mode&unix.S_IFMT != unix.S_IFLNK) {
		return "", fmt.Errorf("invocation path is not a root-owned executable or symlink")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	var executable unix.Stat_t
	if err := unix.Stat(resolved, &executable); err != nil {
		return "", err
	}
	if executable.Mode&unix.S_IFMT != unix.S_IFREG || executable.Mode&0o111 == 0 {
		return "", fmt.Errorf("not an executable regular file")
	}
	if executable.Uid != 0 || executable.Mode&0o022 != 0 {
		return "", fmt.Errorf("executable is not root-owned and immutable to non-root writers")
	}
	if strings.HasPrefix(resolved, "/nix/store/") {
		// Keep the trusted invocation symlink. Multi-call tools such as nix-store
		// select their mode from argv[0] and break if replaced by the final `nix`
		// binary target after validation.
		return path, nil
	}
	for parent := filepath.Dir(resolved); ; parent = filepath.Dir(parent) {
		var directory unix.Stat_t
		if err := unix.Lstat(parent, &directory); err != nil {
			return "", err
		}
		if directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Uid != 0 || directory.Mode&0o022 != 0 {
			return "", fmt.Errorf("executable ancestor is not root-owned and immutable: %s", parent)
		}
		if parent == string(filepath.Separator) {
			break
		}
	}
	return path, nil
}

func trustedExecutableInvocationLocation(path string) bool {
	if strings.HasPrefix(path, "/nix/store/") {
		return true
	}
	switch filepath.Dir(path) {
	case "/run/current-system/sw/bin", "/nix/var/nix/profiles/default/bin", "/usr/bin", "/bin":
		return true
	default:
		return false
	}
}

// privilegedPinnedDirectoryCreatorScript creates a candidate below an already
// pinned parent and captures its identity before returning its public name to
// the shell helper. The shell must reopen the entry and compare this token
// before the first write. A replacement is therefore reported and preserved.
const privilegedPinnedDirectoryCreatorScript = `
create_pinned_directory() {
	directory_parent_fd=$1
	directory_prefix=$2
	directory_python=$3
	created=$("$directory_python" -c '
import os
import secrets
import stat
import sys

parent = os.open(sys.argv[1], os.O_RDONLY | os.O_DIRECTORY)
prefix = sys.argv[2]
try:
    for _ in range(128):
        name = prefix + secrets.token_hex(8)
        try:
            os.mkdir(name, 0o700, dir_fd=parent)
        except FileExistsError:
            continue
        before = os.stat(name, dir_fd=parent, follow_symlinks=False)
        opened = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        try:
            actual = os.fstat(opened)
            if not stat.S_ISDIR(before.st_mode):
                raise RuntimeError("created candidate is not a directory")
            if (before.st_dev, before.st_ino) != (actual.st_dev, actual.st_ino):
                raise RuntimeError("created candidate changed before pin")
            print(f"{name} {actual.st_dev}:{actual.st_ino}")
        finally:
            os.close(opened)
        break
    else:
        raise RuntimeError("cannot allocate directory candidate")
finally:
    os.close(parent)
' "$directory_parent_fd" "$directory_prefix")
	created_name=${created%% *}
	created_id=${created#* }
	case "$created_name" in
		''|.|..|*/*) printf '%s\n' "unsafe created directory name" >&2; exit 2 ;;
	esac
	preopen_id=$(stat -c '%d:%i' -- "$directory_parent_fd/$created_name" 2>/dev/null || true)
	if [ -L "$directory_parent_fd/$created_name" ] || [ ! -d "$directory_parent_fd/$created_name" ] ||
		[ "$preopen_id" != "$created_id" ]; then
		printf '%s\n' "created directory changed before privileged open: $created_name; replacement preserved" >&2
		exit 1
	fi
	exec 8<"$directory_parent_fd/$created_name"
	opened_id=$(stat -Lc '%d:%i' -- /proc/self/fd/8)
	visible_id=$(stat -c '%d:%i' -- "$directory_parent_fd/$created_name" 2>/dev/null || true)
	if [ -L "$directory_parent_fd/$created_name" ] || [ ! -d "$directory_parent_fd/$created_name" ] ||
		[ "$opened_id" != "$created_id" ] || [ "$visible_id" != "$created_id" ]; then
		printf '%s\n' "created directory changed before privileged open: $created_name; replacement preserved" >&2
		exit 1
	fi
}
`

// privilegedPublishScript creates the only privileged temporary payload inside
// a root-private, same-parent quarantine.  It deliberately never removes a
// public target pathname: an unexpected node is either left at the target or
// retained in the quarantine for recovery.  The quarantine prefix is excluded
// from both rsync modes so a failed exchange cannot be erased by a later sync.
const privilegedPublishScript = `
set -eu

kind=$1
parent_fd=$2
target_name=$3
source_fd=$4
expected_fd=$5
parent_display=$6
python_bin=$7

case "$kind" in
	state|user-default|hashed-password|password-hash) ;;
	*) printf '%s\n' "unsupported privileged publish kind: $kind" >&2; exit 2 ;;
esac
case "$target_name" in
	''|.|..|*/*) printf '%s\n' "unsafe privileged publish target name" >&2; exit 2 ;;
esac

` + privilegedPinnedDirectoryCreatorScript + `

# The caller supplies descriptors rather than mutable namespace paths. Every
# move remains on the destination filesystem and --no-copy can fail closed.
umask 077
create_pinned_directory "$parent_fd" ".wahrwelt-installer-recovery-" "$python_bin"
quarantine_name=$created_name
quarantine_id=$created_id
quarantine_fd=/proc/self/fd/8
cd -- "$quarantine_fd"

# Only a root-owned, non-writable parent lets us remove the quarantine by
# pathname without a user-space unlink race.  For custom writable parents an
# empty successful quarantine is harmless; a mismatch always keeps payload.
remove_quarantine=false
parent_owner=$(stat -Lc %u -- "$parent_fd")
parent_mode=$(stat -Lc %a -- "$parent_fd")
if [ "$parent_owner" = 0 ] && [ "$(( (0$parent_mode) & 0022 ))" -eq 0 ]; then
	remove_quarantine=true
fi
cleanup_quarantine() {
	if [ "$remove_quarantine" = true ]; then
		cd -- "$parent_fd"
		rmdir -- "$parent_fd/$quarantine_name"
	fi
}
discard_owned_payload() {
	# In an actor-writable namespace no pathname deletion can prove that the
	# entry still names our inode. Retain the known payload and directory.
	if [ "$remove_quarantine" = true ]; then
		cd -- "$quarantine_fd"
		rm -f -- payload
		cleanup_quarantine
	fi
}
preserve_payload() {
	printf '%s\n' "privileged publish collision; recovery payload preserved in $parent_display/$quarantine_name/payload" >&2
	exit 1
}
fail() {
	discard_owned_payload
	printf '%s\n' "$1" >&2
	exit 1
}
collision() {
	discard_owned_payload
	printf '%s\n' "$1" >&2
	exit 17
}

payload_mode=644
case "$kind" in hashed-password|password-hash) payload_mode=600 ;; esac
install -m "$payload_mode" -- "$source_fd" payload
exec 9<payload
payload_id=$(stat -Lc '%d:%i' -- /proc/self/fd/9)

case "$kind" in
user-default)
	# Link the root-private, pinned payload, not a mutable public temporary
	# pathname.  ln without -f is create-if-absent; a concurrent winner remains.
	if ! ln -L -T -- /proc/self/fd/9 "$parent_fd/$target_name"; then
		if [ -e "$parent_fd/$target_name" ] || [ -L "$parent_fd/$target_name" ]; then
			collision "user/default.nix target appeared during publication"
		fi
		fail "publish user/default.nix"
	fi
	if [ -L "$parent_fd/$target_name" ] || [ ! -f "$parent_fd/$target_name" ]; then
		fail "user/default.nix target changed during publication"
	fi
	target_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name")
	if [ "$target_id" != "$payload_id" ]; then
		fail "user/default.nix target changed during publication"
	fi
	discard_owned_payload
	;;
state|hashed-password|password-hash)
	if [ "$expected_fd" = - ]; then
		# Publish the exact opened payload inode. A mutable quarantine pathname is
		# never used as the source of a fresh public target.
		if ! ln -L -T -- /proc/self/fd/9 "$parent_fd/$target_name"; then
			fail "$kind target appeared during publication"
		fi
		if [ -L "$parent_fd/$target_name" ] || [ ! -f "$parent_fd/$target_name" ]; then
			# The payload was moved out of quarantine.  Do not remove the target.
			cleanup_quarantine
			printf '%s\n' "$kind target changed during publication" >&2
			exit 1
		fi
		target_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name")
		if [ "$target_id" != "$payload_id" ]; then
			cleanup_quarantine
			printf '%s\n' "$kind target changed during publication" >&2
			exit 1
		fi
		discard_owned_payload
		exit 0
	fi

	expected_id=$(stat -Lc '%d:%i' -- "$expected_fd")
	if [ -L "$parent_fd/$target_name" ] || [ ! -f "$parent_fd/$target_name" ]; then
		fail "$kind target changed before publication"
	fi
	current_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name")
	if [ "$current_id" != "$expected_id" ]; then
		fail "$kind target changed before publication"
	fi
	if ! mv --exchange -T --no-copy -- payload "$parent_fd/$target_name"; then
		fail "replace $kind target"
	fi

	# If the target changed after the preflight, its unknown node is now safely
	# inside the private quarantine.  Try exactly one rollback; if that exchange
	# races too, leave every unknown node intact for recovery.
	displaced_id=$(stat -c '%d:%i' -- payload)
	if [ "$displaced_id" = "$expected_id" ]; then
		target_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name" 2>/dev/null || true)
		if [ ! -L "$parent_fd/$target_name" ] && [ -f "$parent_fd/$target_name" ] &&
			[ "$target_id" = "$payload_id" ]; then
			discard_owned_payload
			exit 0
		fi
		# The exchange source was replaced after its identity check. Restore the
		# exact expected target pair once and retain the unknown replacement.
		unexpected_id=$target_id
		if [ -n "$unexpected_id" ] && [ "$(stat -c '%d:%i' -- payload 2>/dev/null || true)" = "$expected_id" ] &&
			mv --exchange -T --no-copy -- payload "$parent_fd/$target_name"; then
			restored_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name" 2>/dev/null || true)
			retained_id=$(stat -c '%d:%i' -- payload 2>/dev/null || true)
			if [ "$restored_id" = "$expected_id" ] && [ "$retained_id" = "$unexpected_id" ]; then
				preserve_payload
			fi
		fi
		preserve_payload
	fi
	if ! mv --exchange -T --no-copy -- payload "$parent_fd/$target_name"; then
		preserve_payload
	fi
	if [ "$(stat -c '%d:%i' -- payload)" != "$payload_id" ]; then
		preserve_payload
	fi
	discard_owned_payload
	printf '%s\n' "$kind target changed during publication" >&2
	exit 1
esac
`

func writeState(ctx context.Context, runner run.CommandRunner, path string, state config.State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ordinaryStatePathParent(path); err != nil {
		return err
	}
	if runner.IsDryRun() {
		expectedTarget, err := pinExpectedStateTarget(path)
		if err != nil {
			return err
		}
		if expectedTarget != nil {
			defer closeFile(expectedTarget.file)
		}
		source, _, cleanupSource, err := createPinnedStateSource(state)
		if err != nil {
			return err
		}
		defer cleanupSource()
		return publishPinnedWithCleanupContext(ctx, runner, "state", nil, path, source, expectedTarget)
	}
	parent, err := openPinnedParentDirectory(path, "state")
	if err != nil {
		return err
	}
	defer closeFile(parent)
	expectedTarget, err := pinExpectedStateTargetAt(parent, filepath.Base(path), path)
	if err != nil {
		return err
	}
	if expectedTarget != nil {
		defer closeFile(expectedTarget.file)
	}
	source, expectedData, cleanupSource, err := createPinnedStateSource(state)
	if err != nil {
		return err
	}
	defer cleanupSource()
	// Keep the privileged handoff anchored to the already retained destination
	// parent.  This is a no-op for the ordinary parent but preserves the command
	// contract without reopening the mutable public pathname.
	if err := runner.Command(ctx, "sudo", "mkdir", "-p", "--", fileDescriptorPath(parent)); err != nil {
		return err
	}
	if err := publishPinnedWithCleanupContext(ctx, runner, "state", parent, path, source, expectedTarget); err != nil {
		return err
	}
	if err := verifyPublishedStateAt(parent, filepath.Base(path), path, expectedTarget, expectedData); err != nil {
		return err
	}
	return verifyPinnedDirectoryVisible(parent, filepath.Dir(path), "state")
}

func pinExpectedStateTarget(path string) (*pinnedRegularFile, error) {
	targetInfo, err := regularFileOrAbsent(path, "state")
	if err != nil || targetInfo == nil {
		return nil, err
	}
	expectedTarget, err := openPinnedRegularFile(path, "state")
	if err != nil {
		return nil, err
	}
	if !sameRegularFile(expectedTarget.info, targetInfo) {
		closeFile(expectedTarget.file)
		return nil, fmt.Errorf("state target changed before publication: %s", path)
	}
	return expectedTarget, nil
}

func pinExpectedStateTargetAt(parent *os.File, name, display string) (*pinnedRegularFile, error) {
	expectedTarget, exists, err := openPinnedRegularFileAt(parent, name, display, "state")
	if err != nil || !exists {
		return nil, err
	}
	return expectedTarget, nil
}

func createPinnedStateSource(state config.State) (*pinnedRegularFile, []byte, func(), error) {
	state.SchemaVersion = config.SchemaVersion
	expectedData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal state: %w", err)
	}
	expectedData = append(expectedData, '\n')
	source, cleanup, err := createSealedPinnedSource("wahrwelt-state", expectedData)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create anonymous state source: %w", err)
	}
	return source, expectedData, cleanup, nil
}

func createSealedPinnedSource(name string, data []byte) (*pinnedRegularFile, func(), error) {
	fd, err := unix.MemfdCreate(name, unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, nil, err
	}
	file, fileErr := fileFromUnixDescriptor(fd, name)
	if fileErr != nil {
		_ = unix.Close(fd)
		return nil, nil, fmt.Errorf("create sealed source file handle: %w", fileErr)
	}
	fail := func(err error) (*pinnedRegularFile, func(), error) {
		closeFile(file)
		return nil, nil, err
	}
	if _, err := file.Write(data); err != nil {
		return fail(fmt.Errorf("write sealed source: %w", err))
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("sync sealed source: %w", err))
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fail(fmt.Errorf("rewind sealed source: %w", err))
	}
	seals := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, seals); err != nil {
		return fail(fmt.Errorf("seal source: %w", err))
	}
	info, err := file.Stat()
	if err != nil {
		return fail(fmt.Errorf("stat sealed source: %w", err))
	}
	source := &pinnedRegularFile{file: file, info: info}
	cleanup := func() {
		closeFile(source.file)
	}
	return source, cleanup, nil
}

func verifyPublishedStateAt(parent *os.File, name, display string, expectedTarget *pinnedRegularFile, expectedData []byte) error {
	published, exists, err := openPinnedRegularFileAt(parent, name, display, "state")
	if err != nil {
		return fmt.Errorf("verify published state: %w", err)
	}
	if !exists {
		return fmt.Errorf("verify published state: state target is absent: %s", display)
	}
	defer closeFile(published.file)
	if expectedTarget != nil && sameRegularFile(published.info, expectedTarget.info) {
		return fmt.Errorf("published state target was not replaced: %s", display)
	}
	actualData, err := readPinnedRegularFile(published)
	if err != nil {
		return fmt.Errorf("read published state: %w", err)
	}
	if !bytes.Equal(actualData, expectedData) {
		return fmt.Errorf("published state content changed during publication: %s", display)
	}
	return nil
}

func openPinnedRegularFile(path, label string) (*pinnedRegularFile, error) {
	// O_PATH obtains a descriptor without opening a FIFO for reading.  fstat is
	// sufficient for the type check and the descriptor can still be dereferenced
	// by the single privileged helper through /proc/<pid>/fd.
	parent, err := openPinnedParentDirectory(path, label)
	if err != nil {
		return nil, err
	}
	defer closeFile(parent)
	file, exists, err := openPinnedRegularFileAt(parent, filepath.Base(path), path, label)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &os.PathError{Op: "open", Path: path, Err: unix.ENOENT}
	}
	return file, nil
}

//nolint:gocyclo // The before/open/after inode bridge is intentionally kept contiguous for auditability.
func openPinnedRegularFileAt(parent *os.File, name, display, label string) (*pinnedRegularFile, bool, error) {
	if filepath.Base(name) != name || name == "." || name == ".." || name == "" {
		return nil, false, fmt.Errorf("unsafe %s entry name: %q", label, name)
	}
	parentDescriptor, err := checkedFileDescriptor(parent, label+" parent")
	if err != nil {
		return nil, false, err
	}
	var before unix.Stat_t
	if err := unix.Fstatat(parentDescriptor, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, &os.PathError{Op: "lstat", Path: display, Err: err}
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, false, fmt.Errorf("unsupported %s path: %s", label, display)
	}
	fd, err := unix.Openat(parentDescriptor, name, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, &os.PathError{Op: "open", Path: display, Err: err}
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return nil, false, &os.PathError{Op: "fstat", Path: display, Err: err}
	}
	if before.Dev != opened.Dev || before.Ino != opened.Ino || opened.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("%s path changed while pinning: %s", label, display)
	}
	file, err := fileFromUnixDescriptor(fd, display)
	if err != nil {
		_ = unix.Close(fd)
		return nil, false, err
	}
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("open %s file: %s", label, display)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, false, fmt.Errorf("unsupported %s path: %s", label, display)
	}
	return &pinnedRegularFile{file: file, info: info}, true, nil
}

func openPinnedParentDirectory(path, label string) (*os.File, error) {
	parent := filepath.Clean(filepath.Dir(path))
	if !filepath.IsAbs(parent) {
		absolute, err := filepath.Abs(parent)
		if err != nil {
			return nil, fmt.Errorf("resolve %s parent path: %w", label, err)
		}
		parent = filepath.Clean(absolute)
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: "/", Err: err}
	}
	for _, component := range strings.Split(strings.TrimPrefix(parent, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		next, openErr := unix.Openat(fd, component, unix.O_PATH|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			_ = unix.Close(fd)
			return nil, &os.PathError{Op: "open", Path: parent, Err: openErr}
		}
		if closeErr := unix.Close(fd); closeErr != nil {
			_ = unix.Close(next)
			return nil, fmt.Errorf("close %s parent descriptor: %w", label, closeErr)
		}
		fd = next
	}
	file, err := fileFromUnixDescriptor(fd, parent)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open %s parent directory: %s", label, parent)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("unsupported %s parent path: %s", label, parent)
	}
	return file, nil
}

func verifyPinnedDirectoryVisible(directory *os.File, path, label string) error {
	visible, err := openPinnedParentDirectory(filepath.Join(path, ".wahrwelt-visibility-probe"), label+" visibility")
	if err != nil {
		return fmt.Errorf("%s parent changed during publication: %s: %w", label, path, err)
	}
	defer closeFile(visible)
	expectedInfo, err := directory.Stat()
	if err != nil {
		return fmt.Errorf("stat pinned %s parent: %w", label, err)
	}
	visibleInfo, err := visible.Stat()
	if err != nil {
		return fmt.Errorf("stat visible %s parent: %w", label, err)
	}
	if !os.SameFile(expectedInfo, visibleInfo) {
		return fmt.Errorf("%s parent changed during publication: %s", label, path)
	}
	return nil
}

func checkedFileDescriptor(file *os.File, label string) (int, error) {
	descriptor := file.Fd()
	if uint64(descriptor) > uint64(math.MaxInt) {
		return 0, fmt.Errorf("%s descriptor exceeds platform int range", label)
	}
	return int(descriptor), nil
}

func fileFromUnixDescriptor(descriptor int, path string) (*os.File, error) {
	if descriptor < 0 {
		return nil, fmt.Errorf("invalid negative descriptor for %s", path)
	}
	return os.NewFile(uintptr(descriptor), path), nil
}

func regularFileOrAbsent(path, label string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported %s path: %s", label, path)
	}
	return info, nil
}

func sameRegularFile(current, expected os.FileInfo) bool {
	return current != nil && expected != nil && current.Mode().IsRegular() && os.SameFile(current, expected)
}

func ordinaryStatePathParent(path string) error {
	return ordinaryPathParent(path, "state")
}

func ordinaryPathParent(path, label string) error {
	for parent := filepath.Clean(filepath.Dir(path)); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("unsupported %s parent path: %s", label, parent)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return nil
		}
	}
}

func publishPinnedRegularFile(ctx context.Context, runner run.CommandRunner, kind string, parent *os.File, target string, source, expected *pinnedRegularFile) error {
	pythonPath, err := privilegedPythonPath()
	if err != nil {
		return err
	}
	parentFD := "-"
	sourceFD := "-"
	expectedFD := "-"
	if parent != nil {
		parentFD = fileDescriptorPath(parent)
	}
	if source != nil && source.file != nil {
		sourceFD = fileDescriptorPath(source.file)
	}
	if expected != nil && expected.file != nil {
		expectedFD = fileDescriptorPath(expected.file)
	}
	err = runner.Command(ctx, "sudo", "sh", "-c", privilegedPublishScript, "--",
		kind, parentFD, filepath.Base(target), sourceFD, expectedFD, filepath.Dir(target), pythonPath)
	if parent != nil {
		runtime.KeepAlive(parent)
	}
	if source != nil && source.file != nil {
		runtime.KeepAlive(source.file)
	}
	if expected != nil && expected.file != nil {
		runtime.KeepAlive(expected.file)
	}
	return err
}

func fileDescriptorPath(file *os.File) string {
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), file.Fd())
}

func readPinnedRegularFile(file *pinnedRegularFile) ([]byte, error) {
	if file == nil || file.file == nil {
		return nil, fmt.Errorf("missing pinned regular file")
	}
	return os.ReadFile(fileDescriptorPath(file.file))
}

func samePinnedContent(left, right *pinnedRegularFile) (bool, error) {
	leftData, err := readPinnedRegularFile(left)
	if err != nil {
		return false, err
	}
	rightData, err := readPinnedRegularFile(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftData, rightData), nil
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), privilegedPublishTimeout)
}

func publishPinnedWithCleanupContext(ctx context.Context, runner run.CommandRunner, kind string, parent *os.File, target string, source, expected *pinnedRegularFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	publishCtx, cancel := cleanupContext(ctx)
	defer cancel()
	err := publishPinnedRegularFile(publishCtx, runner, kind, parent, target, source, expected)
	if outerErr := ctx.Err(); outerErr != nil {
		if err != nil {
			return errors.Join(outerErr, err)
		}
		return outerErr
	}
	return err
}

func migrateLegacyUserDirectory(ctx context.Context, runner run.CommandRunner, dest string) error {
	_, err := migrateLegacyUserDirectoryWithResult(ctx, runner, dest)
	return err
}

type pinnedDirectoryEntry struct {
	file *os.File
	info os.FileInfo
}

const privilegedMigrateLegacyUserScript = `
set -eu

` + privilegedPinnedDirectoryCreatorScript + `

parent_fd=$1
source_name=$2
target_name=$3
expected_fd=$4
parent_display=$5
python_bin=$6

case "$source_name:$target_name" in
	private:user) ;;
	*) printf '%s\n' "unsafe legacy user migration names" >&2; exit 2 ;;
esac

expected_id=$(stat -Lc '%d:%i' -- "$expected_fd")
parent_id=$(stat -Lc '%d:%i' -- "$parent_fd")
pinned_parent_display=$(readlink -f -- "$parent_fd" || true)
[ -n "$pinned_parent_display" ] || pinned_parent_display=$parent_display

visible_parent_matches() {
	[ ! -L "$parent_display" ] && [ -d "$parent_display" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_display" 2>/dev/null || true)" = "$parent_id" ]
}
source_matches() {
	[ ! -L "$parent_fd/$source_name" ] && [ -d "$parent_fd/$source_name" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_fd/$source_name" 2>/dev/null || true)" = "$expected_id" ]
}
target_absent() {
	[ ! -e "$parent_fd/$target_name" ] && [ ! -L "$parent_fd/$target_name" ]
}
target_matches_expected() {
	[ ! -L "$parent_fd/$target_name" ] && [ -d "$parent_fd/$target_name" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_fd/$target_name" 2>/dev/null || true)" = "$expected_id" ]
}
expected_recovery_path() {
	readlink -- "$expected_fd" 2>/dev/null || printf '%s/%s' "$pinned_parent_display" "$target_name"
}
retain_target() {
	label=$1
	umask 077
	create_pinned_directory "$parent_fd" ".wahrwelt-installer-recovery-user-" "$python_bin"
	recovery_name=$created_name
	recovery_id=$created_id
	recovery_fd=/proc/self/fd/8
	if mv -T --no-copy --update=none-fail -- "$parent_fd/$target_name" "$recovery_fd/payload"; then
		moved_id=$(stat -c '%d:%i' -- "$recovery_fd/payload" 2>/dev/null || true)
		visible_recovery_id=$(stat -c '%d:%i' -- "$parent_fd/$recovery_name" 2>/dev/null || true)
		if [ "$visible_recovery_id" = "$recovery_id" ]; then
			recovery_display=$pinned_parent_display/$recovery_name/payload
		else
			recovery_display=$(readlink -- "$recovery_fd" 2>/dev/null || printf '%s' "$recovery_fd")/payload
		fi
		printf '%s\n' "$label; recovery retained at $recovery_display (inode $moved_id)" >&2
	else
		printf '%s\n' "$label; recovery remains at $pinned_parent_display/$target_name" >&2
	fi
	exit 1
}
restore_expected_source() {
	if ! target_matches_expected; then
		printf '%s\n' "legacy user migration target changed; replacement preserved at $pinned_parent_display/$target_name; expected recovery retained at $(expected_recovery_path)" >&2
		exit 1
	fi
	if mv -T --no-copy --update=none-fail -- "$parent_fd/$target_name" "$parent_fd/$source_name"; then
		if source_matches && target_absent; then
			return 0
		fi
	fi
	retain_target "legacy user migration rollback was refused"
}

if ! source_matches; then
	printf '%s\n' "legacy user source changed before privileged migration" >&2
	exit 1
fi
if ! target_absent; then
	printf '%s\n' "legacy user target appeared before privileged migration: $parent_display/$target_name" >&2
	exit 17
fi
if ! visible_parent_matches; then
	printf '%s\n' "legacy user parent changed before privileged migration: $parent_display" >&2
	exit 1
fi

if ! mv -T --no-copy --update=none-fail -- "$parent_fd/$source_name" "$parent_fd/$target_name"; then
	if ! target_absent; then
		printf '%s\n' "legacy user target appeared during privileged migration: $parent_display/$target_name" >&2
		exit 17
	fi
	printf '%s\n' "legacy user source changed during privileged migration" >&2
	exit 1
fi

moved_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name" 2>/dev/null || true)
if [ "$moved_id" != "$expected_id" ]; then
	printf '%s\n' "unexpected legacy user target replacement preserved at $pinned_parent_display/$target_name; expected recovery retained at $(expected_recovery_path)" >&2
	exit 1
fi
if [ -e "$parent_fd/$source_name" ] || [ -L "$parent_fd/$source_name" ]; then
	retain_target "legacy user source name gained a second owner during privileged migration"
fi
if ! visible_parent_matches; then
	restore_expected_source
	printf '%s\n' "legacy user parent changed; exact source restored through pinned parent" >&2
	exit 1
fi
if ! target_matches_expected; then
	printf '%s\n' "legacy user target changed after privileged migration; replacement preserved at $pinned_parent_display/$target_name; expected recovery retained at $(expected_recovery_path)" >&2
	exit 1
fi
exit 0
`

func openPinnedDirectoryEntry(parent *os.File, name, display string) (*pinnedDirectoryEntry, bool, error) {
	return openPinnedDirectoryEntryAt(parent, name, display, "legacy user")
}

func openPinnedDirectoryEntryAt(parent *os.File, name, display, label string) (*pinnedDirectoryEntry, bool, error) {
	if filepath.Base(name) != name || name == "." || name == ".." || name == "" {
		return nil, false, fmt.Errorf("unsafe %s entry name: %q", label, name)
	}
	parentFD, err := checkedFileDescriptor(parent, label+" parent")
	if err != nil {
		return nil, false, err
	}
	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, &os.PathError{Op: "lstat", Path: display, Err: err}
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, false, fmt.Errorf("unsupported %s path: %s", label, display)
	}
	fd, err := unix.Openat(parentFD, name, unix.O_PATH|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, &os.PathError{Op: "open", Path: display, Err: err}
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return nil, false, &os.PathError{Op: "fstat", Path: display, Err: err}
	}
	if before.Dev != opened.Dev || before.Ino != opened.Ino {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("%s path changed while pinning: %s", label, display)
	}
	file, err := fileFromUnixDescriptor(fd, display)
	if err != nil {
		_ = unix.Close(fd)
		return nil, false, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, false, err
	}
	return &pinnedDirectoryEntry{file: file, info: info}, true, nil
}

func verifyPinnedDirectoryEntry(parent *os.File, name, display, label string, expected *pinnedDirectoryEntry) error {
	if expected == nil || expected.file == nil || expected.info == nil {
		return fmt.Errorf("missing pinned %s directory: %s", label, display)
	}
	current, exists, err := openPinnedDirectoryEntryAt(parent, name, display, label)
	if err != nil {
		return err
	}
	if current != nil {
		defer closeFile(current.file)
	}
	if !exists || current.info == nil || !os.SameFile(expected.info, current.info) {
		return fmt.Errorf("%s directory changed during publication: %s", label, display)
	}
	return nil
}

func migrateLegacyUserDirectoryWithResult(ctx context.Context, runner run.CommandRunner, dest string) (bool, error) {
	legacyDir := filepath.Join(dest, "private")
	userDir := filepath.Join(dest, "user")
	parent, err := openPinnedParentDirectory(userDir, "legacy user")
	if err != nil {
		return false, err
	}
	defer closeFile(parent)
	legacy, legacyExists, err := openPinnedDirectoryEntry(parent, filepath.Base(legacyDir), legacyDir)
	if err != nil {
		return false, err
	}
	if legacy != nil {
		defer closeFile(legacy.file)
	}
	user, userExists, err := openPinnedDirectoryEntry(parent, filepath.Base(userDir), userDir)
	if err != nil {
		return false, err
	}
	if user != nil {
		defer closeFile(user.file)
	}
	if !legacyExists {
		return false, nil
	}
	if userExists {
		return false, fmt.Errorf("cannot migrate legacy user directory: target already exists: %s", userDir)
	}
	parentFD := fileDescriptorPath(parent)
	expectedFD := fileDescriptorPath(legacy.file)
	pythonPath, err := privilegedPythonPath()
	if err != nil {
		return false, err
	}
	publishCtx, cancel := cleanupContext(ctx)
	defer cancel()
	err = runner.Command(
		publishCtx,
		"sudo", "sh", "-c", privilegedMigrateLegacyUserScript, "--",
		parentFD,
		filepath.Base(legacyDir),
		filepath.Base(userDir),
		expectedFD,
		dest,
		pythonPath,
	)
	runtime.KeepAlive(parent)
	runtime.KeepAlive(legacy.file)
	if err != nil {
		currentTarget, targetExists, targetErr := openPinnedDirectoryEntry(
			parent,
			filepath.Base(userDir),
			userDir,
		)
		if currentTarget != nil {
			closeFile(currentTarget.file)
		}
		if targetErr != nil {
			return false, fmt.Errorf("inspect legacy user migration target: %w", targetErr)
		}
		if targetExists {
			return false, errors.Join(&legacyUserMigrationCollisionError{path: userDir}, err)
		}
		return false, fmt.Errorf("migrate legacy user directory: %w", err)
	}
	return true, nil
}

// privilegedRemoveLegacyStateScript moves the leaf through a root-private
// quarantine before checking its pinned identity.  It never deletes the public
// pathname, so a node that wins the pre-cleanup race is restored or retained in
// the managed recovery quarantine instead of being mistaken for legacy state.
const privilegedRemoveLegacyStateScript = `
set -eu

parent_fd=$1
target_name=$2
expected_fd=$3
grandparent_fd=$4
parent_name=$5
expected_parent_fd=$6
parent_display=$7
python_bin=$8

case "$target_name" in
	''|.|..|*/*) printf '%s\n' "unsafe legacy state target name" >&2; exit 2 ;;
esac
case "$parent_name" in
	''|.|..|*/*) printf '%s\n' "unsafe legacy state parent name" >&2; exit 2 ;;
esac

expected_parent_id=$(stat -Lc '%d:%i' -- "$expected_parent_fd")
expected_id=$(stat -Lc '%d:%i' -- "$expected_fd")
visible_parent_matches() {
	[ ! -L "$parent_display" ] && [ -d "$parent_display" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_display" 2>/dev/null || true)" = "$expected_parent_id" ]
}
visible_leaf_matches() {
	[ ! -L "$parent_display/$target_name" ] && [ -f "$parent_display/$target_name" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_display/$target_name" 2>/dev/null || true)" = "$expected_id" ]
}
if ! visible_parent_matches || ! visible_leaf_matches; then
	printf '%s\n' "legacy state parent or leaf changed before cleanup: $parent_display/$target_name" >&2
	exit 1
fi

` + privilegedPinnedDirectoryCreatorScript + `

umask 077
create_pinned_directory "$parent_fd" ".wahrwelt-installer-recovery-" "$python_bin"
quarantine_name=$created_name
quarantine_id=$created_id
quarantine_fd=/proc/self/fd/8
cd -- "$quarantine_fd"

# Pathname removal is safe only beneath root-owned, non-writable directories.
# Custom/untrusted parents retain an empty managed quarantine on success rather
# than risk removing a replacement after a separate identity check.
remove_quarantine=false
can_prune_parent=false
parent_owner=$(stat -Lc %u -- "$parent_fd")
parent_mode=$(stat -Lc %a -- "$parent_fd")
grandparent_owner=$(stat -Lc %u -- "$grandparent_fd")
grandparent_mode=$(stat -Lc %a -- "$grandparent_fd")
if [ "$parent_owner" = 0 ] && [ "$(( (0$parent_mode) & 0022 ))" -eq 0 ]; then
	remove_quarantine=true
	if [ "$grandparent_owner" = 0 ] && [ "$(( (0$grandparent_mode) & 0022 ))" -eq 0 ]; then
		can_prune_parent=true
	fi
fi
cleanup_quarantine() {
	if [ "$remove_quarantine" = true ]; then
		cd -- "$parent_fd"
		rmdir -- "$parent_fd/$quarantine_name"
	fi
}
preserve_payload() {
	printf '%s\n' "legacy state changed during cleanup; recovery payload preserved in $parent_display/$quarantine_name/payload" >&2
	exit 1
}
restore_or_preserve() {
	if mv -T --no-copy --update=none-fail -- payload "$parent_fd/$target_name"; then
		cleanup_quarantine
		printf '%s\n' "legacy state changed during cleanup" >&2
		exit 1
	fi
	preserve_payload
}
prune_parent_if_safe() {
	if [ "$can_prune_parent" != true ]; then
		return
	fi
	if [ ! -d "$grandparent_fd/$parent_name" ] || [ -L "$grandparent_fd/$parent_name" ]; then
		return
	fi
	current_parent_id=$(stat -c '%d:%i' -- "$grandparent_fd/$parent_name")
	if [ "$current_parent_id" != "$expected_parent_id" ]; then
		return
	fi
	# rmdir only succeeds for the exact empty legacy directory.  A concurrent
	# user file makes it fail harmlessly and leaves the directory intact.
	rmdir -- "$grandparent_fd/$parent_name" || true
}

if ! visible_parent_matches || ! visible_leaf_matches; then
	cleanup_quarantine
	printf '%s\n' "legacy state parent or leaf changed before cleanup: $parent_display/$target_name" >&2
	exit 1
fi
if ! mv -T --no-copy --update=none-fail -- "$parent_fd/$target_name" payload; then
	cleanup_quarantine
	printf '%s\n' "legacy state disappeared before cleanup" >&2
	exit 1
fi
actual_id=$(stat -c '%d:%i' -- payload)
if [ "$actual_id" != "$expected_id" ]; then
	printf '%s\n' "legacy state payload was replaced; unknown payload retained at $parent_display/$quarantine_name/payload; expected recovery retained at $(readlink -- "$expected_fd" 2>/dev/null || printf '%s' "$parent_display/$target_name")" >&2
	exit 1
fi
if ! visible_parent_matches; then
	restore_or_preserve
fi
if [ "$remove_quarantine" = true ]; then
	cd -- "$quarantine_fd"
	rm -f -- payload
fi
if ! visible_parent_matches || [ -e "$parent_display/$target_name" ] || [ -L "$parent_display/$target_name" ]; then
	printf '%s\n' "legacy state namespace changed after cleanup: $parent_display/$target_name" >&2
	exit 1
fi
cleanup_quarantine
prune_parent_if_safe
`

func removePinnedLegacyState(ctx context.Context, runner run.CommandRunner, parent, grandparent *os.File, path string, expected *pinnedRegularFile) error {
	if parent == nil || grandparent == nil || expected == nil || expected.file == nil {
		return fmt.Errorf("missing pinned legacy state handles: %s", path)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	pythonPath, err := privilegedPythonPath()
	if err != nil {
		return err
	}
	err = runner.Command(cleanupCtx, "sudo", "sh", "-c", privilegedRemoveLegacyStateScript, "--",
		fileDescriptorPath(parent), filepath.Base(path), fileDescriptorPath(expected.file),
		fileDescriptorPath(grandparent), filepath.Base(filepath.Dir(path)), fileDescriptorPath(parent), filepath.Dir(path), pythonPath)
	runtime.KeepAlive(parent)
	runtime.KeepAlive(grandparent)
	runtime.KeepAlive(expected.file)
	if outerErr := ctx.Err(); outerErr != nil {
		if err != nil {
			return errors.Join(outerErr, err)
		}
		return outerErr
	}
	return err
}

type pinnedLegacyStateTarget struct {
	path        string
	parent      *os.File
	grandparent *os.File
	expected    *pinnedRegularFile
}

type legacyStateExpectation struct {
	normalized []byte
	loadedPath string
	loadedInfo os.FileInfo
}

// LoadStateWithProof reads a regular state file through a pinned descriptor and
// returns an opaque snapshot that can later prove ownership of a legacy path.
func LoadStateWithProof(path string) (config.State, LoadedStateProof, error) {
	return loadStateWithProof(path, stateProofHooks{})
}

type stateProofHooks struct {
	beforeStrictRead func() error
	afterStrictRead  func() error
}

//nolint:gocyclo // The proof transaction keeps every pinned-read and revalidation boundary in one auditable sequence.
func loadStateWithProof(path string, hooks stateProofHooks) (config.State, LoadedStateProof, error) {
	var zero config.State
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		absolute, err := filepath.Abs(clean)
		if err != nil {
			return zero, LoadedStateProof{}, fmt.Errorf("resolve loaded state path: %w", err)
		}
		clean = filepath.Clean(absolute)
	}
	if err := ordinaryStatePathParent(clean); err != nil {
		return zero, LoadedStateProof{}, err
	}
	parent, err := openPinnedParentDirectory(clean, "loaded state")
	if err != nil {
		return zero, LoadedStateProof{}, err
	}
	defer closeFile(parent)
	pinned, exists, err := openPinnedRegularFileAt(parent, filepath.Base(clean), clean, "loaded state")
	if err != nil {
		return zero, LoadedStateProof{}, err
	}
	if !exists {
		return zero, LoadedStateProof{}, fmt.Errorf("state file does not exist %s: %w", clean, os.ErrNotExist)
	}
	defer closeFile(pinned.file)

	initialSnapshot, err := validatePinnedGeneratedStateOwnership(pinned)
	if err != nil {
		return zero, LoadedStateProof{}, err
	}
	state, err := config.LoadExisting(fileDescriptorPath(pinned.file))
	if err != nil {
		return zero, LoadedStateProof{}, err
	}
	normalized, err := normalizedInstallerState(state)
	if err != nil {
		return zero, LoadedStateProof{}, fmt.Errorf("normalize loaded installer state: %w", err)
	}
	afterPermissiveRead, err := validatePinnedGeneratedStateOwnership(pinned)
	if err != nil {
		return zero, LoadedStateProof{}, err
	}
	if !sameGeneratedStateSnapshot(initialSnapshot, afterPermissiveRead) {
		return zero, LoadedStateProof{}, fmt.Errorf("loaded state changed during permissive read: %s", clean)
	}
	if hooks.beforeStrictRead != nil {
		if err := hooks.beforeStrictRead(); err != nil {
			return zero, LoadedStateProof{}, fmt.Errorf("before strict state read: %w", err)
		}
	}
	generatedState, afterStrictRead, strictErr := loadPinnedGeneratedState(pinned, &afterPermissiveRead)
	stablePartial := strictErr != nil && config.IsGeneratedStateShapeError(strictErr)
	if strictErr != nil && !stablePartial {
		return zero, LoadedStateProof{}, strictErr
	}
	if !stablePartial {
		generatedNormalized, err := normalizedInstallerState(generatedState)
		if err != nil {
			return zero, LoadedStateProof{}, fmt.Errorf("normalize generated installer state: %w", err)
		}
		if !bytes.Equal(generatedNormalized, normalized) {
			return zero, LoadedStateProof{}, fmt.Errorf("loaded state content changed during ownership validation: %s", clean)
		}
	}
	if hooks.afterStrictRead != nil {
		if err := hooks.afterStrictRead(); err != nil {
			return zero, LoadedStateProof{}, fmt.Errorf("after strict state read: %w", err)
		}
	}
	revalidated, exists, err := openPinnedRegularFileAt(parent, filepath.Base(clean), clean, "loaded state")
	if err != nil {
		return zero, LoadedStateProof{}, err
	}
	if !exists {
		return zero, LoadedStateProof{}, fmt.Errorf("loaded state changed after read: %s", clean)
	}
	defer closeFile(revalidated.file)
	if !sameRegularFile(revalidated.info, pinned.info) {
		return zero, LoadedStateProof{}, fmt.Errorf("loaded state changed after read: %s", clean)
	}
	revalidatedState, _, revalidationErr := loadPinnedGeneratedState(revalidated, &afterStrictRead)
	if stablePartial {
		if revalidationErr == nil {
			return zero, LoadedStateProof{}, fmt.Errorf("loaded partial state became generated during revalidation: %s", clean)
		}
		if !config.IsGeneratedStateShapeError(revalidationErr) {
			return zero, LoadedStateProof{}, fmt.Errorf("revalidate partial installer state: %w", revalidationErr)
		}
	} else {
		if revalidationErr != nil {
			return zero, LoadedStateProof{}, fmt.Errorf("revalidate loaded installer state: %w", revalidationErr)
		}
		revalidatedData, err := normalizedInstallerState(revalidatedState)
		if err != nil {
			return zero, LoadedStateProof{}, fmt.Errorf("normalize revalidated installer state: %w", err)
		}
		if !bytes.Equal(revalidatedData, normalized) {
			return zero, LoadedStateProof{}, fmt.Errorf("loaded state content changed after read: %s", clean)
		}
	}
	if err := verifyPinnedDirectoryVisible(parent, filepath.Dir(clean), "loaded state"); err != nil {
		return zero, LoadedStateProof{}, err
	}
	if stablePartial {
		// Stable draft and partial canonical state stays loadable, but it cannot
		// authorize legacy cleanup. If this path is legacy, apply preflight sees
		// the empty proof and fails closed before the first live mutation.
		return state, LoadedStateProof{}, nil
	}
	return state, LoadedStateProof{
		path:       clean,
		normalized: append([]byte(nil), normalized...),
		info:       pinned.info,
	}, nil
}

func preflightLegacyStatePathsWithExpectation(ctx context.Context, statePaths []string, expectation legacyStateExpectation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	targets, err := pinValidatedLegacyStateTargets(statePaths, expectation)
	closePinnedLegacyStateTargets(targets)
	return err
}

func cleanupLegacyStatePaths(ctx context.Context, runner run.CommandRunner, statePaths []string, current config.State) error {
	expectation, err := legacyStateExpectationFromState(current)
	if err != nil {
		return err
	}
	return cleanupLegacyStatePathsWithExpectation(ctx, runner, statePaths, expectation)
}

func cleanupLegacyStatePathsWithExpectation(ctx context.Context, runner run.CommandRunner, statePaths []string, expectation legacyStateExpectation) error {
	targets, err := pinValidatedLegacyStateTargets(statePaths, expectation)
	if err != nil {
		return err
	}
	defer closePinnedLegacyStateTargets(targets)
	for _, target := range targets {
		if err := removePinnedLegacyState(ctx, runner, target.parent, target.grandparent, target.path, target.expected); err != nil {
			return fmt.Errorf("remove legacy state %s: %w", target.path, err)
		}
	}
	return nil
}

func legacyStateExpectationFromState(current config.State) (legacyStateExpectation, error) {
	currentData, err := normalizedInstallerState(current)
	if err != nil {
		return legacyStateExpectation{}, fmt.Errorf("normalize current installer state: %w", err)
	}
	return legacyStateExpectation{normalized: currentData}, nil
}

func pinValidatedLegacyStateTargets(statePaths []string, expectation legacyStateExpectation) ([]pinnedLegacyStateTarget, error) {
	targets := make([]pinnedLegacyStateTarget, 0, len(statePaths))
	fail := func(err error) ([]pinnedLegacyStateTarget, error) {
		closePinnedLegacyStateTargets(targets)
		return nil, err
	}
	for _, path := range statePaths {
		if err := ordinaryStatePathParent(path); err != nil {
			return fail(err)
		}
		parentPath := filepath.Dir(path)
		grandparentFile, err := openPinnedParentDirectory(parentPath, "legacy state grandparent")
		if err != nil {
			return fail(err)
		}
		parentEntry, parentExists, err := openPinnedDirectoryEntryAt(
			grandparentFile,
			filepath.Base(parentPath),
			parentPath,
			"legacy state parent",
		)
		if err != nil {
			_ = grandparentFile.Close()
			return fail(err)
		}
		if !parentExists {
			_ = grandparentFile.Close()
			continue
		}
		expected, stateExists, err := openPinnedRegularFileAt(
			parentEntry.file,
			filepath.Base(path),
			path,
			"legacy state",
		)
		if err != nil {
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			return fail(err)
		}
		if !stateExists {
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			continue
		}
		if len(expectation.normalized) == 0 {
			_ = expected.file.Close()
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			return fail(fmt.Errorf("legacy state ownership collision at %s: no validated loaded state proof", path))
		}
		legacyData, err := normalizedPinnedLegacyState(expected)
		if err != nil {
			_ = expected.file.Close()
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			return fail(fmt.Errorf("legacy state ownership collision at %s: unrecognized installer state: %w", path, err))
		}
		if !bytes.Equal(legacyData, expectation.normalized) {
			_ = expected.file.Close()
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			return fail(fmt.Errorf("legacy state ownership collision at %s: installer state diverges from the loaded state snapshot", path))
		}
		if filepath.Clean(path) == expectation.loadedPath && !sameRegularFile(expected.info, expectation.loadedInfo) {
			_ = expected.file.Close()
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			return fail(fmt.Errorf("legacy state ownership collision at %s: loaded state inode changed before cleanup", path))
		}
		if err := verifyPinnedDirectoryVisible(parentEntry.file, parentPath, "legacy state"); err != nil {
			_ = expected.file.Close()
			_ = parentEntry.file.Close()
			_ = grandparentFile.Close()
			return fail(err)
		}
		targets = append(targets, pinnedLegacyStateTarget{path: path, parent: parentEntry.file, grandparent: grandparentFile, expected: expected})
	}
	return targets, nil
}

func closePinnedLegacyStateTargets(targets []pinnedLegacyStateTarget) {
	for _, target := range targets {
		closeFile(target.parent)
		closeFile(target.grandparent)
		if target.expected != nil {
			closeFile(target.expected.file)
		}
	}
}

func normalizedPinnedLegacyState(expected *pinnedRegularFile) ([]byte, error) {
	state, _, err := loadPinnedGeneratedState(expected, nil)
	if err != nil {
		return nil, err
	}
	return normalizedInstallerState(state)
}

func loadPinnedGeneratedState(expected *pinnedRegularFile, baseline *unix.Stat_t) (config.State, unix.Stat_t, error) {
	var zero config.State
	var emptySnapshot unix.Stat_t
	if expected == nil || expected.file == nil {
		return zero, emptySnapshot, fmt.Errorf("missing pinned installer state")
	}
	before, err := validatePinnedGeneratedStateOwnership(expected)
	if err != nil {
		return zero, before, err
	}
	if baseline != nil && !sameGeneratedStateSnapshot(*baseline, before) {
		return zero, before, fmt.Errorf("installer state changed before strict ownership validation")
	}
	state, loadErr := config.LoadGeneratedExisting(fileDescriptorPath(expected.file))
	after, afterErr := validatePinnedGeneratedStateOwnership(expected)
	if afterErr != nil {
		return zero, after, afterErr
	}
	if !sameGeneratedStateSnapshot(before, after) {
		return zero, after, fmt.Errorf("installer state changed while ownership was validated")
	}
	if loadErr != nil {
		return zero, after, loadErr
	}
	return state, after, nil
}

func validatePinnedGeneratedStateOwnership(expected *pinnedRegularFile) (unix.Stat_t, error) {
	var snapshot unix.Stat_t
	descriptor, err := checkedFileDescriptor(expected.file, "installer state")
	if err != nil {
		return snapshot, err
	}
	if err := unix.Fstat(descriptor, &snapshot); err != nil {
		return snapshot, fmt.Errorf("stat pinned installer state: %w", err)
	}
	if snapshot.Mode&unix.S_IFMT != unix.S_IFREG {
		return snapshot, fmt.Errorf("installer state must remain a regular file")
	}
	if snapshot.Nlink != 1 {
		return snapshot, fmt.Errorf("installer state has unsupported hardlink count %d", snapshot.Nlink)
	}
	if snapshot.Size < 0 || snapshot.Size > config.MaxGeneratedStateBytes {
		return snapshot, fmt.Errorf("installer state exceeds the %d byte ownership limit", config.MaxGeneratedStateBytes)
	}
	return snapshot, nil
}

func sameGeneratedStateSnapshot(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev &&
		before.Ino == after.Ino &&
		before.Mode == after.Mode &&
		before.Nlink == after.Nlink &&
		before.Size == after.Size &&
		before.Mtim == after.Mtim &&
		before.Ctim == after.Ctim
}

func normalizedInstallerState(state config.State) ([]byte, error) {
	state = config.Migrate(state)
	state.SchemaVersion = config.SchemaVersion
	return json.Marshal(state)
}
