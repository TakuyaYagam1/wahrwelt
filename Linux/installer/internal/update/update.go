// Package update changes an installed wrapper without running the installer.
package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/fsowner"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/hosttree"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
)

type Options struct {
	Paths          paths.Options
	DryRun         bool
	SkipSwitch     bool
	Inputs         []string
	TargetRevision string
	TargetURL      string
	SourceURL      string
	Runner         run.CommandRunner
	Output         io.Writer
}

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var inputPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(/[A-Za-z][A-Za-z0-9_-]*)*$`)

type installation struct {
	dest      string
	statePath string
	state     config.State
	stateFile snapshot
	files     map[string]snapshot
	plan      apply.FlakePlan
}

// Run prepares and builds the replacement before publishing its three owned
// files. A failed activation retains the built generation and backup for repair.
func Run(ctx context.Context, opts Options) error {
	if opts.Runner == nil {
		opts.Runner = run.New(false)
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.DryRun {
		installed, err := loadInstallation(opts)
		if err != nil {
			return err
		}
		for _, change := range installed.plan.Changes {
			if _, err := fmt.Fprintln(opts.Output, change); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(opts.Output, "Plan checked; installed files are unchanged. Lock updates and build run when applying the update.")
		return err
	}
	if !revisionPattern.MatchString(opts.TargetRevision) {
		return errors.New("update requires the exact Wahrwelt source revision; run wahrwelt update through its launcher")
	}
	if !opts.SkipSwitch && os.Geteuid() != 0 {
		return errors.New("switching the installed system requires root; run wahrwelt update through its sudo launcher")
	}
	return runUpdate(ctx, opts)
}

func loadInstallation(opts Options) (*installation, error) {
	dest, err := filepath.Abs(opts.Paths.NixOSDest)
	if err != nil {
		return nil, err
	}
	if err := canonicalDirectory(dest); err != nil {
		return nil, err
	}
	statePath, err := opts.Paths.ExistingStatePath()
	if err != nil {
		return nil, err
	}
	stateFile, err := readSnapshot(statePath, false)
	if err != nil {
		return nil, err
	}
	state, err := config.LoadGeneratedExisting(statePath)
	if err != nil {
		return nil, fmt.Errorf("load installed state (drafts and defaults are not accepted): %w", err)
	}
	if err := config.Validate(state); err != nil {
		return nil, fmt.Errorf("invalid installed state: %w", err)
	}
	if err := stateFile.verify(statePath); err != nil {
		return nil, err
	}
	files := make(map[string]snapshot, 3)
	for _, name := range []string{"flake.nix", "flake.lock", apply.FlakeStateName} {
		file, readErr := readSnapshot(filepath.Join(dest, name), name != "flake.nix")
		if readErr != nil {
			return nil, readErr
		}
		files[name] = file
	}
	plan, err := apply.PlanFlakeUpdate(string(files["flake.nix"].data), state, files[apply.FlakeStateName].data)
	if err != nil {
		return nil, err
	}
	return &installation{dest: dest, statePath: statePath, state: state, stateFile: stateFile, files: files, plan: plan}, nil
}

func runUpdate(ctx context.Context, opts Options) error {
	dest, err := filepath.Abs(opts.Paths.NixOSDest)
	if err != nil {
		return err
	}
	if err := canonicalDirectory(dest); err != nil {
		return err
	}
	journalRoot, release, err := lockUpdates(dest)
	if err != nil {
		return err
	}
	defer release()
	if err := recoverUpdates(journalRoot, dest, opts.SkipSwitch); err != nil {
		return err
	}
	// Recovery may restore an interrupted update, so inspect again under the lock.
	installed, err := loadInstallation(opts)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(installed.dest), "."+filepath.Base(installed.dest)+"-update-")
	if err != nil {
		return fmt.Errorf("create sibling staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	buildRoot, err := os.MkdirTemp(filepath.Dir(installed.dest), "."+filepath.Base(installed.dest)+"-build-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(buildRoot) }()
	result, lock, err := prepareCandidate(ctx, opts, installed, stage, filepath.Join(buildRoot, "result"))
	if err != nil {
		return err
	}
	if opts.SkipSwitch {
		return nil
	}
	if err := publish(journalRoot, installed, lock, fsowner.WriteOwned); err != nil {
		return err
	}
	return activate(ctx, opts.Runner, result)
}

func prepareCandidate(ctx context.Context, opts Options, installed *installation, stage, outLink string) (string, []byte, error) {
	tree, err := hosttree.Copy(installed.dest, stage, nil)
	if err != nil {
		return "", nil, err
	}
	if err := verifyOriginals(installed, []string{"flake.nix", "flake.lock", apply.FlakeStateName}); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "flake.nix"), []byte(installed.plan.Content), 0o600); err != nil {
		return "", nil, err
	}
	if installed.files["flake.lock"].exists {
		if err := os.WriteFile(filepath.Join(stage, "flake.lock"), installed.files["flake.lock"].data, 0o600); err != nil {
			return "", nil, err
		}
	}
	if err := lockCandidate(ctx, opts, installed, stage); err != nil {
		return "", nil, err
	}
	lock, err := readSnapshot(filepath.Join(stage, "flake.lock"), false)
	if err != nil {
		return "", nil, err
	}
	if err := verifyLock(lock.data, opts, installed.plan.Content); err != nil {
		return "", nil, err
	}
	result, err := buildCandidate(ctx, opts.Runner, stage, installed.state.Host.Hostname, outLink)
	if err != nil {
		return "", nil, err
	}
	if err := hosttree.Verify(installed.dest, tree); err != nil {
		return "", nil, err
	}
	if err := installed.stateFile.verify(installed.statePath); err != nil {
		return "", nil, fmt.Errorf("installed state changed during update: %w", err)
	}
	if err := verifyOriginals(installed, []string{"flake.nix", "flake.lock", apply.FlakeStateName}); err != nil {
		return "", nil, err
	}
	return result, lock.data, nil
}

func lockCandidate(ctx context.Context, opts Options, installed *installation, stage string) error {
	url, err := apply.CurrentWahrweltURL(installed.plan.Content)
	if err != nil {
		return err
	}
	if opts.SourceURL != "" {
		original, sourceErr := apply.CurrentWahrweltURL(string(installed.files["flake.nix"].data))
		if sourceErr != nil || original != opts.SourceURL {
			return errors.New("wahrwelt input changed since the update launcher resolved its revision; restart the update")
		}
	}
	pinned, err := pinnedURL(url, opts.TargetURL, opts.TargetRevision)
	if err != nil {
		return err
	}
	args := []string{"--extra-experimental-features", "nix-command flakes", "flake", "update"}
	lockMode, err := apply.CurrentLockMode(installed.plan.Content)
	if err != nil {
		return err
	}
	for _, input := range opts.Inputs {
		if !inputPattern.MatchString(input) {
			return fmt.Errorf("invalid update input name %q", input)
		}
	}
	if len(opts.Inputs) > 0 {
		args = append(args, opts.Inputs...)
		if !contains(opts.Inputs, "wahrwelt") {
			args = append(args, "wahrwelt")
		}
	} else if lockMode == apply.LockModeManaged {
		args = append(args, "wahrwelt")
	}
	args = append(args, "--flake", "path:"+stage, "--override-input", "wahrwelt", pinned)
	return opts.Runner.Command(ctx, "nix", args...)
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func buildCandidate(ctx context.Context, runner run.CommandRunner, stage, hostname, outLink string) (string, error) {
	result, err := runner.Output(ctx, "nix", "--extra-experimental-features", "nix-command flakes", "build",
		"--impure", "--no-update-lock-file", "--no-write-lock-file", "--out-link", outLink, "--print-out-paths",
		"path:"+stage+"#nixosConfigurations."+hostname+".config.system.build.toplevel")
	if err != nil {
		return "", fmt.Errorf("build prepared configuration; installed files are unchanged: %w", err)
	}
	result = strings.TrimSpace(result)
	if !strings.HasPrefix(result, "/nix/store/") || strings.ContainsAny(result, "\r\n \t") || filepath.Clean(result) != result {
		return "", errors.New("nix did not return one system store path")
	}
	return result, nil
}

func activate(ctx context.Context, runner run.CommandRunner, result string) error {
	if err := runner.Command(ctx, "nix-env", "--profile", "/nix/var/nix/profiles/system", "--set", result); err != nil {
		return fmt.Errorf("configuration saved, but system profile update failed; backup is retained: %w", err)
	}
	if err := runner.Command(ctx, filepath.Join(result, "bin/switch-to-configuration"), "switch"); err != nil {
		return fmt.Errorf("configuration saved, but activation failed; previous system generation and configuration backup are retained: %w", err)
	}
	return nil
}

func unchanged(old snapshot, content []byte) bool {
	return bytes.Equal(old.data, content) && old.exists
}
