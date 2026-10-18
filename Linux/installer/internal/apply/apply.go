package apply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/defaults"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/dots"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/fsowner"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/hosttree"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
	"golang.org/x/sys/unix"
)

type Options struct {
	Paths            paths.Options
	State            config.State
	LoadedStateProof LoadedStateProof
	Secrets          config.Secrets
	DryRun           bool
	AssumeYes        bool
	SkipSwitch       bool
	Layout           Layout
	LockMode         LockMode
	Runner           run.CommandRunner
}

// LoadedStateProof can only be created by LoadStateWithProof. Its fields stay
// private so apply callers cannot assert ownership of arbitrary legacy state.
type LoadedStateProof struct {
	path       string
	normalized []byte
	info       os.FileInfo
}

type applyModes struct {
	layout   Layout
	lockMode LockMode
}

func normalizeApplyModes(layout Layout, lockMode LockMode) (applyModes, error) {
	normalizedLayout, err := normalizeLayout(layout)
	if err != nil {
		return applyModes{}, err
	}
	normalizedLockMode, err := normalizeLockMode(lockMode)
	if err != nil {
		return applyModes{}, err
	}
	return applyModes{layout: normalizedLayout, lockMode: normalizedLockMode}, nil
}

func printApplyHeader(src paths.Sources, dest string, modes applyModes) {
	fmt.Println("== Wahrwelt apply ==")
	fmt.Printf("source: %s\n", src.RepoRoot)
	fmt.Printf("target: %s\n", dest)
	fmt.Printf("layout: %s\n", modes.layout)
	if modes.layout == LayoutThin {
		fmt.Printf("lock mode: %s\n", modes.lockMode)
	}
}

func Run(ctx context.Context, opts Options) error {
	if err := config.Validate(opts.State); err != nil {
		return err
	}
	modes, err := normalizeApplyModes(opts.Layout, opts.LockMode)
	if err != nil {
		return err
	}
	src, err := paths.ResolveSources(opts.Paths.RepoRoot)
	if err != nil {
		return err
	}

	runner := opts.Runner
	if runner == nil {
		runner = run.New(opts.DryRun)
	}
	printApplyHeader(src, opts.Paths.NixOSDest, modes)
	if err := authenticateRootAccessBeforeStaging(ctx, runner, opts.Paths.NixOSDest); err != nil {
		return err
	}

	workspace, err := createStagingWorkspace()
	if err != nil {
		return fmt.Errorf("create staging: %w", err)
	}
	runErr := runStagedApply(ctx, runner, src, workspace, opts, modes)
	cleanupErr := workspace.cleanup()
	if cleanupErr != nil {
		cleanupErr = workspace.retainedCleanupError(cleanupErr)
	}
	workspace.close()
	if cleanupErr != nil {
		if runErr != nil {
			return errors.Join(runErr, cleanupErr)
		}
		return cleanupErr
	}
	if runErr != nil {
		return runErr
	}
	fmt.Println("Wahrwelt apply finished without errors")
	return nil
}

func authenticateRootAccessBeforeStaging(ctx context.Context, runner run.CommandRunner, destination string) error {
	if runner.IsDryRun() || filepath.Clean(destination) != "/etc/nixos" {
		return nil
	}
	fmt.Println("root access is required; authenticate sudo before staging")
	if err := runner.Command(ctx, "sudo", "-v"); err != nil {
		return fmt.Errorf("authenticate root access before staging: %w", err)
	}
	return nil
}

func runStagedApply(ctx context.Context, runner run.CommandRunner, src paths.Sources, workspace *stagingWorkspace, opts Options, modes applyModes) error {
	validated, err := prepareStagedApply(ctx, runner, src, workspace.runtimePath, opts, modes, workspace.verifyVisible)
	if err != nil {
		return err
	}
	defer validated.close()
	if opts.SkipSwitch {
		fmt.Println("dry-build passed; --no-switch set, stopping before /etc/nixos or dotfile writes")
		return nil
	}
	if err := workspace.verifyVisible(); err != nil {
		return fmt.Errorf("staging ownership changed before publication; /etc/nixos was not modified: %w", err)
	}
	if err := validated.verify(); err != nil {
		return fmt.Errorf("validated staging became unavailable before publication; /etc/nixos was not modified: %w", err)
	}
	legacyStatePaths, canonicalStatePath := legacyStatePathsForApply(opts)
	var legacyStateExpectation legacyStateExpectation
	if canonicalStatePath {
		legacyStateExpectation, err = legacyStateExpectationForApply(opts, legacyStatePaths)
		if err != nil {
			return fmt.Errorf("prepare legacy installer state cleanup proof; live configuration was not modified: %w", err)
		}
		if err := preflightLegacyStatePathsWithExpectation(ctx, legacyStatePaths, legacyStateExpectation); err != nil {
			return fmt.Errorf("preflight legacy installer state cleanup; live configuration was not modified: %w", err)
		}
	}
	result, err := writeSystemConfiguration(ctx, runner, validated.path, opts, modes.layout, validated.verify)
	if err != nil {
		return handleSystemWriteFailure(ctx, runner, opts.Paths.NixOSDest, result, err)
	}
	if err := dots.Apply(ctx, dots.Options{
		Sources: src,
		State:   opts.State,
		DryRun:  opts.DryRun,
		Runner:  runner,
	}); err != nil {
		return handleSystemWriteFailure(ctx, runner, opts.Paths.NixOSDest, result, err)
	}
	switched, err := switchSystem(ctx, runner, opts)
	if err != nil {
		printRollbackHint(result.BackupPath, opts.Paths.NixOSDest)
		return err
	}
	if !switched {
		fmt.Println("state not written because system was not activated")
		return nil
	}
	if err := writeState(ctx, runner, opts.Paths.StatePath, opts.State); err != nil {
		return err
	}
	if canonicalStatePath {
		if err := cleanupLegacyStatePathsWithExpectation(ctx, runner, legacyStatePaths, legacyStateExpectation); err != nil {
			return err
		}
	}
	return pruneOwnedNixOSBackups(ctx, runner, opts.Paths.NixOSDest)
}

func legacyStatePathsForApply(opts Options) ([]string, bool) {
	destination := filepath.Clean(opts.Paths.NixOSDest)
	canonical := filepath.Join(destination, filepath.Base(paths.DefaultStatePath))
	if filepath.Clean(opts.Paths.StatePath) != canonical {
		return nil, false
	}
	return []string{
		filepath.Join(destination, "wahrwelt", "state.json"),
		filepath.Join(destination, "mysetup", "state.json"),
	}, true
}

func legacyStateExpectationForApply(opts Options, legacyStatePaths []string) (legacyStateExpectation, error) {
	proof := opts.LoadedStateProof
	if proof.path == "" && proof.info == nil && len(proof.normalized) == 0 {
		return legacyStateExpectation{}, nil
	}
	if proof.path == "" || proof.info == nil || !proof.info.Mode().IsRegular() || len(proof.normalized) == 0 {
		return legacyStateExpectation{}, fmt.Errorf("invalid loaded state proof")
	}
	loadedPath := filepath.Clean(proof.path)
	allowed := loadedPath == filepath.Clean(opts.Paths.StatePath)
	for _, candidate := range legacyStatePaths {
		allowed = allowed || loadedPath == filepath.Clean(candidate)
	}
	if !allowed {
		return legacyStateExpectation{}, fmt.Errorf("loaded state proof path is outside the canonical migration set: %s", loadedPath)
	}
	return legacyStateExpectation{
		normalized: append([]byte(nil), proof.normalized...),
		loadedPath: loadedPath,
		loadedInfo: proof.info,
	}, nil
}

func prepareStagedApply(ctx context.Context, runner run.CommandRunner, src paths.Sources, staging string, opts Options, modes applyModes, verifyBeforeSnapshot ...func() error) (*validatedStaging, error) {
	verifyHost, err := captureHostFiles(opts.Paths.NixOSDest, modes.layout)
	if err != nil {
		return nil, err
	}
	if err := stageConfiguration(ctx, runner, src, staging, opts.State, modes.layout, modes.lockMode); err != nil {
		return nil, err
	}
	if err := prepareStagingHostLocal(ctx, runner, staging, opts.Paths.NixOSDest, opts.State, opts.Secrets, modes.layout); err != nil {
		return nil, err
	}
	extraFiles, err := stageHostExtras(staging, opts.Paths.NixOSDest, modes.layout)
	if err != nil {
		return nil, err
	}
	if err := lockStagingFlake(ctx, runner, staging, modes.layout, modes.lockMode); err != nil {
		return nil, err
	}
	for _, verify := range verifyBeforeSnapshot {
		if verify == nil {
			continue
		}
		if err := verify(); err != nil {
			return nil, fmt.Errorf("staging ownership changed before validation: %w", err)
		}
	}
	validated, err := createValidatedStaging(ctx, runner, staging)
	if err != nil {
		return nil, fmt.Errorf("create immutable staging candidate; /etc/nixos was not modified: %w", err)
	}
	if err := dryBuildSystem(ctx, runner, validated.path, opts.State.Host.Hostname); err != nil {
		validated.close()
		return nil, fmt.Errorf("dry-build failed; /etc/nixos was not modified: %w", err)
	}
	validated.verifyHost = func() error {
		if verifyHost != nil {
			if err := verifyHost(); err != nil {
				return err
			}
		}
		if extraFiles != nil {
			return hosttree.Verify(opts.Paths.NixOSDest, *extraFiles)
		}
		return nil
	}
	if err := validated.verify(); err != nil {
		validated.close()
		return nil, err
	}
	return validated, nil
}

type validatedStaging struct {
	path       string
	gcRootDir  *os.File
	verifyHost func() error
}

func (v *validatedStaging) close() {
	if v != nil && v.gcRootDir != nil {
		_ = v.gcRootDir.Close()
		v.gcRootDir = nil
	}
}

func (v *validatedStaging) verify() error {
	if v == nil || v.path == "" {
		return fmt.Errorf("missing validated staging candidate")
	}
	if v.verifyHost != nil {
		if err := v.verifyHost(); err != nil {
			return err
		}
	}
	if v.gcRootDir == nil {
		return nil
	}
	if err := validateImmutableStagingPath(v.path); err != nil {
		return err
	}
	rootPath := filepath.Join(fileDescriptorPath(v.gcRootDir), "root")
	target, err := os.Readlink(rootPath)
	if err != nil {
		return fmt.Errorf("inspect staging GC root: %w", err)
	}
	if target != v.path {
		return fmt.Errorf("staging GC root changed: got %q want %q", target, v.path)
	}
	runtime.KeepAlive(v.gcRootDir)
	return nil
}

func createValidatedStaging(ctx context.Context, runner run.CommandRunner, staging string) (*validatedStaging, error) {
	if runner.IsDryRun() {
		return &validatedStaging{path: staging}, nil
	}
	nixPath, err := trustedValidationCommand("WAHRWELT_VALIDATION_NIX", "nix")
	if err != nil {
		return nil, err
	}
	nixStorePath, err := trustedValidationCommand("WAHRWELT_VALIDATION_NIX_STORE", "nix-store")
	if err != nil {
		return nil, err
	}
	pinnedRunner, ok := runner.(run.PinnedDirectoryOutputRunner)
	if !ok {
		return nil, fmt.Errorf("validation runner cannot ingest a pinned staging directory")
	}
	stagingDirectory, err := openValidationDirectory(staging)
	if err != nil {
		return nil, err
	}
	defer closeFile(stagingDirectory)
	// A path flake is copied into the Nix store during the existing dry-build.
	// Materialising it first does not add a new secret class to the store; it
	// makes the exact already-store-visible tree explicit so validation and
	// publication cannot observe different same-UID-writable staging bytes.
	output, err := pinnedRunner.OutputInPinnedDirectory(ctx, stagingDirectory, nixPath,
		"--extra-experimental-features", "nix-command",
		"store", "add-path",
		"--name", "wahrwelt-validated-system",
		".",
	)
	if err != nil {
		return nil, err
	}
	snapshot := strings.TrimSpace(output)
	if strings.ContainsAny(snapshot, "\r\n") {
		return nil, fmt.Errorf("nix returned multiple staging paths")
	}
	if err := validateImmutableStagingPath(snapshot); err != nil {
		return nil, err
	}

	rootDir, err := createPinnedGCRootDirectory(staging)
	if err != nil {
		return nil, err
	}
	candidate := &validatedStaging{path: snapshot, gcRootDir: rootDir}
	rootPath := filepath.Join(fileDescriptorPath(rootDir), "root")
	if err := runner.Command(ctx, nixStorePath, "--add-root", rootPath, "--indirect", "-r", snapshot); err != nil {
		candidate.close()
		return nil, fmt.Errorf("register staging GC root: %w", err)
	}
	if err := candidate.verify(); err != nil {
		candidate.close()
		return nil, err
	}
	return candidate, nil
}

func openValidationDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open pinned staging directory: %w", err)
	}
	file, err := fileFromUnixDescriptor(fd, path)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("pinned staging path is not a directory: %s", path)
	}
	return file, nil
}

func trustedValidationCommand(environment, name string) (string, error) {
	candidate := strings.TrimSpace(os.Getenv(environment))
	if candidate == "" {
		var err error
		candidate, err = exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("locate trusted %s for staging validation: %w", name, err)
		}
	}
	path, err := trustedPrivilegedExecutable(candidate)
	if err != nil {
		return "", fmt.Errorf("untrusted %s for staging validation %s: %w", name, candidate, err)
	}
	return path, nil
}

func validateImmutableStagingPath(path string) error {
	clean := filepath.Clean(path)
	if path == "" || clean != path || filepath.Dir(clean) != "/nix/store" {
		return fmt.Errorf("nix returned unsafe staging store path %q", path)
	}
	groups, err := os.Getgroups()
	if err != nil {
		return fmt.Errorf("inspect caller groups for staging store trust: %w", err)
	}
	for _, current := range []string{"/nix", "/nix/store", clean} {
		var info unix.Stat_t
		if err := unix.Lstat(current, &info); err != nil {
			return fmt.Errorf("inspect staging store path %s: %w", current, err)
		}
		if info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Uid != 0 || writableByCurrentIdentity(info, groups) {
			return fmt.Errorf("staging store path is not a root-owned immutable directory: %s", current)
		}
	}
	return nil
}

func writableByCurrentIdentity(info unix.Stat_t, groups []int) bool {
	if os.Geteuid() == 0 {
		return false
	}
	if info.Mode&0o002 != 0 {
		return true
	}
	if int64(os.Geteuid()) == int64(info.Uid) {
		return info.Mode&0o200 != 0
	}
	if info.Mode&0o020 == 0 {
		return false
	}
	for _, group := range groups {
		if int64(group) == int64(info.Gid) {
			return true
		}
	}
	return false
}

func createPinnedGCRootDirectory(staging string) (*os.File, error) {
	path, err := os.MkdirTemp(staging, ".wahrwelt-validation-root-")
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file, err := fileFromUnixDescriptor(fd, path)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !before.IsDir() || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("staging GC-root directory changed while pinning: %s", path)
	}
	return file, nil
}

func handleSystemWriteFailure(ctx context.Context, runner run.CommandRunner, destination string, result systemWriteResult, cause error) error {
	if !result.skipAutomaticRestore {
		return handlePreSwitchError(ctx, runner, destination, result.BackupPath, cause)
	}
	if result.BackupPath == "" {
		return cause
	}
	return fmt.Errorf("%w; skipped automatic /etc/nixos restore because a broad restore could remove concurrent canonical user data; backup retained at %s", cause, result.BackupPath)
}

func createStagingDir() (string, error) {
	workspace, err := createStagingWorkspace()
	if err != nil {
		return "", err
	}
	workspace.close()
	return workspace.path, nil
}

type stagingWorkspace struct {
	path          string
	runtimePath   string
	name          string
	containerName string
	base          *os.File
	parent        *os.File
	directory     *os.File
	baseStat      unix.Stat_t
	parentStat    unix.Stat_t
	dirStat       unix.Stat_t
}

func (w *stagingWorkspace) close() {
	if w == nil {
		return
	}
	closeFile(w.directory)
	closeFile(w.parent)
	closeFile(w.base)
	w.directory = nil
	w.parent = nil
	w.base = nil
}

func (w *stagingWorkspace) ownedRecoveryPath() string {
	if w == nil || w.directory == nil {
		return "<unavailable>"
	}
	path, err := os.Readlink(fileDescriptorPath(w.directory))
	if err != nil || path == "" {
		return "<unavailable>"
	}
	return path
}

func (w *stagingWorkspace) retainedCleanupError(cause error) error {
	return fmt.Errorf("staging retained at FD-resolved owned path %s because exact cleanup was refused: %w", w.ownedRecoveryPath(), cause)
}

func (w *stagingWorkspace) verifyContainerVisible() error {
	if w == nil || w.base == nil || w.parent == nil {
		return fmt.Errorf("missing pinned staging container")
	}
	baseFD, err := checkedFileDescriptor(w.base, "staging base")
	if err != nil {
		return err
	}
	var baseCurrent unix.Stat_t
	if err := unix.Fstat(baseFD, &baseCurrent); err != nil {
		return err
	}
	if baseCurrent.Dev != w.baseStat.Dev || baseCurrent.Ino != w.baseStat.Ino {
		return fmt.Errorf("staging base descriptor identity changed")
	}
	var container unix.Stat_t
	if err := unix.Fstatat(baseFD, w.containerName, &container, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("staging outer container changed: %w", err)
	}
	if container.Mode&unix.S_IFMT != unix.S_IFDIR || container.Dev != w.parentStat.Dev || container.Ino != w.parentStat.Ino {
		return fmt.Errorf("staging outer container changed; replacement retained")
	}
	return nil
}

func (w *stagingWorkspace) removeEmptyContainer() error {
	if err := w.verifyContainerVisible(); err != nil {
		return err
	}
	base, err := fsowner.OpenDirectory(w.base.Name())
	if err != nil {
		return err
	}
	defer closeFileDirectory(base)
	entry, err := base.Inspect(w.containerName)
	if err != nil {
		return err
	}
	if entry.Identity.Device != w.parentStat.Dev ||
		entry.Identity.Inode != w.parentStat.Ino ||
		entry.Identity.Kind != fsowner.KindDirectory ||
		entry.Identity.UID != w.parentStat.Uid ||
		entry.Identity.Mode&0o7777 != w.parentStat.Mode&0o7777 {
		return fmt.Errorf("staging outer container metadata changed; replacement retained")
	}
	return base.RemoveDirectory(w.containerName, entry.Identity, fsowner.RemoveOptions{
		UID:          w.parentStat.Uid,
		RequireEmpty: true,
		SameDevice:   true,
	})
}

func (w *stagingWorkspace) verifyVisible() error {
	if err := w.verifyContainerVisible(); err != nil {
		return err
	}
	parentFD, err := checkedFileDescriptor(w.parent, "staging parent")
	if err != nil {
		return err
	}
	var tree unix.Stat_t
	if err := unix.Fstatat(parentFD, w.name, &tree, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("staging tree changed: %w", err)
	}
	if tree.Mode&unix.S_IFMT != unix.S_IFDIR || tree.Dev != w.dirStat.Dev || tree.Ino != w.dirStat.Ino {
		return fmt.Errorf("staging tree changed; replacement retained")
	}
	return nil
}

func sameStagingDirectoryIdentity(identity fsowner.Identity, expected unix.Stat_t) bool {
	return identity.Device == expected.Dev &&
		identity.Inode == expected.Ino &&
		identity.Kind == fsowner.KindDirectory &&
		identity.UID == expected.Uid &&
		identity.Mode&0o7777 == expected.Mode&0o7777
}

func (w *stagingWorkspace) cleanup() error {
	if w == nil || w.base == nil || w.parent == nil || w.directory == nil {
		return fmt.Errorf("missing pinned staging workspace")
	}
	if err := w.verifyVisible(); err != nil {
		return err
	}
	parent, err := fsowner.OpenDirectory(w.parent.Name())
	if err != nil {
		return err
	}
	defer closeFileDirectory(parent)
	parentIdentity := parent.Identity()
	if !sameStagingDirectoryIdentity(parentIdentity, w.parentStat) {
		return fmt.Errorf("staging parent metadata changed; tree retained")
	}
	entry, err := parent.Inspect(w.name)
	if err != nil {
		return err
	}
	if !sameStagingDirectoryIdentity(entry.Identity, w.dirStat) {
		return fmt.Errorf("staging tree metadata changed; tree retained")
	}
	if err := parent.RemoveDirectory(w.name, entry.Identity, fsowner.RemoveOptions{
		UID:        w.dirStat.Uid,
		Recursive:  true,
		SameDevice: true,
	}); err != nil {
		return err
	}
	if err := parent.Sync(); err != nil {
		return err
	}
	parentFD, fdErr := checkedFileDescriptor(w.parent, "staging parent")
	if fdErr != nil {
		return fdErr
	}
	var visible unix.Stat_t
	if err := unix.Fstatat(parentFD, w.name, &visible, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return fmt.Errorf("privileged cleanup reported success but staging name remains")
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	return w.removeEmptyContainer()
}

func createStagingWorkspace() (*stagingWorkspace, error) {
	base := stagingBaseDir()
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	if err := scavengeEmptyStagingContainers(base); err != nil {
		return nil, err
	}
	baseFD, err := unix.Open(base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	baseDirectory, err := fileFromUnixDescriptor(baseFD, base)
	if err != nil {
		_ = unix.Close(baseFD)
		return nil, err
	}
	var baseStat unix.Stat_t
	if err := unix.Fstat(baseFD, &baseStat); err != nil {
		closeFile(baseDirectory)
		return nil, err
	}
	containerName, container, containerStat, err := createPinnedStagingChild(baseDirectory, base, ".wahrwelt-workspace-")
	if err != nil {
		closeFile(baseDirectory)
		return nil, err
	}
	containerPath := filepath.Join(base, containerName)
	name, directory, dirStat, err := createPinnedStagingChild(container, containerPath, defaults.StagingTempPattern)
	if err != nil {
		closeFile(container)
		closeFile(baseDirectory)
		return nil, err
	}
	return &stagingWorkspace{
		path:          filepath.Join(containerPath, name),
		runtimePath:   fileDescriptorPath(directory),
		name:          name,
		containerName: containerName,
		base:          baseDirectory,
		parent:        container,
		directory:     directory,
		baseStat:      baseStat,
		parentStat:    containerStat,
		dirStat:       dirStat,
	}, nil
}

func scavengeEmptyStagingContainers(base string) error {
	directory, err := fsowner.OpenDirectory(base)
	if err != nil {
		return err
	}
	defer closeFileDirectory(directory)
	entries, err := directory.List(isKnownStagingContainerName)
	if err != nil {
		return err
	}
	uid := currentEffectiveUID()
	for _, entry := range entries {
		if entry.Identity.Kind != fsowner.KindDirectory ||
			entry.Identity.UID != uid || entry.Identity.Mode&0o7777 != 0o700 {
			continue
		}
		// Crash recoveries or concurrent workspaces are nonempty and therefore
		// fail closed. Only historical success residue is removed.
		removeErr := directory.RemoveDirectory(entry.Name, entry.Identity, fsowner.RemoveOptions{
			UID:          uid,
			RequireEmpty: true,
			SameDevice:   true,
		})
		if removeErr != nil && !strings.Contains(removeErr.Error(), " is not empty") {
			return fmt.Errorf("scavenge empty staging container %s: %w", entry.Name, removeErr)
		}
	}
	return nil
}

func isKnownStagingContainerName(name string) bool {
	const prefix = ".wahrwelt-workspace-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	if len(suffix) == 9 || len(suffix) == 10 {
		for _, char := range suffix {
			if char < '0' || char > '9' {
				return false
			}
		}
		return true
	}
	if len(suffix) != 32 {
		return false
	}
	for _, char := range suffix {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func createPinnedStagingChild(parent *os.File, displayParent, pattern string) (string, *os.File, unix.Stat_t, error) {
	return createPinnedStagingChildWithBarrier(parent, displayParent, pattern, nil)
}

//nolint:gocyclo // The linear creator-token sequence is intentionally kept in one auditable transaction.
func createPinnedStagingChildWithBarrier(parent *os.File, displayParent, pattern string, afterCreate func(name string, created unix.Stat_t) error) (string, *os.File, unix.Stat_t, error) {
	parentFD, err := checkedFileDescriptor(parent, "staging parent")
	if err != nil {
		return "", nil, unix.Stat_t{}, err
	}
	prefix := strings.TrimSuffix(pattern, "*")
	var name string
	var created unix.Stat_t
	allocated := false
	for attempt := 0; attempt < 128; attempt++ {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", nil, unix.Stat_t{}, err
		}
		name = prefix + hex.EncodeToString(random)
		if err := unix.Mkdirat(parentFD, name, 0o700); err != nil {
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			return "", nil, unix.Stat_t{}, err
		}
		if err := unix.Fstatat(parentFD, name, &created, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return "", nil, unix.Stat_t{}, err
		}
		if created.Mode&unix.S_IFMT != unix.S_IFDIR {
			return "", nil, unix.Stat_t{}, fmt.Errorf("created staging entry is not a directory: %s", filepath.Join(displayParent, name))
		}
		allocated = true
		break
	}
	if !allocated {
		return "", nil, unix.Stat_t{}, fmt.Errorf("cannot allocate staging directory below %s", displayParent)
	}
	if afterCreate != nil {
		if err := afterCreate(name, created); err != nil {
			return "", nil, unix.Stat_t{}, err
		}
	}
	directoryFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", nil, unix.Stat_t{}, err
	}
	directory, err := fileFromUnixDescriptor(directoryFD, filepath.Join(displayParent, name))
	if err != nil {
		_ = unix.Close(directoryFD)
		return "", nil, unix.Stat_t{}, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(directoryFD, &opened); err != nil {
		closeFile(directory)
		return "", nil, unix.Stat_t{}, err
	}
	if created.Dev != opened.Dev || created.Ino != opened.Ino {
		closeFile(directory)
		return "", nil, unix.Stat_t{}, fmt.Errorf("created staging directory changed before exact open: %s; replacement retained", filepath.Join(displayParent, name))
	}
	var visible unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &visible, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		closeFile(directory)
		return "", nil, unix.Stat_t{}, err
	}
	if visible.Mode&unix.S_IFMT != unix.S_IFDIR || visible.Dev != opened.Dev || visible.Ino != opened.Ino {
		closeFile(directory)
		return "", nil, unix.Stat_t{}, fmt.Errorf("created staging directory changed after exact open: %s; replacement retained", filepath.Join(displayParent, name))
	}
	return name, directory, opened, nil
}

func stagingBaseDir() string {
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" && !isUnderPath(cacheDir, os.TempDir()) {
		return filepath.Join(cacheDir, "wahrwelt", "staging")
	}
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" && !isUnderPath(homeDir, os.TempDir()) {
		return filepath.Join(homeDir, ".cache", "wahrwelt", "staging")
	}
	return filepath.Join("/var/tmp", "wahrwelt-"+strconv.Itoa(os.Getuid()), "staging")
}

func isUnderPath(path, parent string) bool {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		cleanPath = filepath.Clean(path)
	}
	cleanParent, err := filepath.Abs(parent)
	if err != nil {
		cleanParent = filepath.Clean(parent)
	}
	rel, err := filepath.Rel(cleanParent, cleanPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
