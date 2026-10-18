package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/update"
)

// SourceRevision identifies the source that supplied this executable's migrations.
// Nix supplies it at build time, independently of the installed host's lock.
var SourceRevision string

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var inputNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)

type updateTarget struct {
	URL    string `json:"url"`
	Locked struct {
		Revision string `json:"rev"`
	} `json:"locked"`
}

func updateCommand(opts *Options) *cobra.Command {
	var noSwitch bool
	var revision, targetURL, sourceURL string
	cmd := &cobra.Command{
		Use:   "update [inputs...]",
		Short: "Update Wahrwelt while preserving host customizations",
		Args: func(_ *cobra.Command, args []string) error {
			for _, name := range args {
				if !inputNamePattern.MatchString(name) {
					return fmt.Errorf("invalid input name %q", name)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := updatePaths(opts.Options)
			if err != nil {
				return err
			}
			if revision != "" || targetURL != "" || sourceURL != "" {
				if !revisionPattern.MatchString(revision) || revision != SourceRevision || targetURL == "" || sourceURL == "" {
					return fmt.Errorf("update revision does not match this executable; run wahrwelt update without internal target flags")
				}
				return update.Run(cmd.Context(), update.Options{
					Paths: p, DryRun: opts.DryRun, SkipSwitch: noSwitch, Inputs: args,
					TargetRevision: revision, TargetURL: targetURL, SourceURL: sourceURL,
				})
			}
			// A local checkout can inspect a migration before its first publication.
			if opts.DryRun && opts.RepoRoot != "" {
				return update.Run(cmd.Context(), update.Options{Paths: p, DryRun: true, Inputs: args})
			}
			return refreshUpdate(cmd.Context(), run.New(false), p, args, noSwitch, opts.DryRun)
		},
	}
	cmd.Flags().BoolVar(&noSwitch, "no-switch", false, "build and validate without publishing or switching")
	cmd.Flags().StringVar(&revision, "target-rev", "", "resolved update revision")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "resolved update reference")
	cmd.Flags().StringVar(&sourceURL, "source-url", "", "expected installed source reference")
	for _, flag := range []string{"target-rev", "target-url", "source-url"} {
		_ = cmd.Flags().MarkHidden(flag)
	}
	return cmd
}

func updatePaths(p paths.Options) (paths.Options, error) {
	dest, err := filepath.Abs(p.NixOSDest)
	if err != nil {
		return p, err
	}
	p.NixOSDest = dest
	if p.StatePath == paths.DefaultStatePath && dest != "/etc/nixos" {
		p.StatePath = filepath.Join(dest, "installer-state.json")
	}
	statePath, err := p.ExistingStatePath()
	if err != nil {
		return p, err
	}
	p.StatePath, err = filepath.Abs(statePath)
	return p, err
}

func installedUpdateSource(p paths.Options) (string, error) {
	if _, err := config.LoadExisting(p.StatePath); err != nil {
		return "", err
	}
	flakePath := filepath.Join(p.NixOSDest, "flake.nix")
	info, err := os.Lstat(flakePath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("host flake is not an ordinary file: %s", flakePath)
	}
	data, err := os.ReadFile(flakePath)
	if err != nil {
		return "", err
	}
	return apply.CurrentWahrweltURL(string(data))
}

func refreshUpdate(ctx context.Context, runner run.CommandRunner, p paths.Options, inputs []string, noSwitch, dryRun bool) error {
	sourceURL, err := installedUpdateSource(p)
	if err != nil {
		return err
	}
	target, err := resolveUpdateTarget(ctx, runner, sourceURL)
	if err != nil {
		return err
	}
	return runResolvedUpdate(ctx, runner, p, inputs, target, sourceURL, noSwitch, dryRun)
}

func resolveUpdateTarget(ctx context.Context, runner run.CommandRunner, sourceURL string) (updateTarget, error) {
	metadata, err := runner.Output(ctx, "nix", "--extra-experimental-features", "nix-command flakes",
		"flake", "metadata", "--refresh", "--json", "--no-write-lock-file", sourceURL)
	if err != nil {
		return updateTarget{}, fmt.Errorf("resolve Wahrwelt update source: %w", err)
	}
	var target updateTarget
	if err := json.Unmarshal([]byte(metadata), &target); err != nil {
		return updateTarget{}, fmt.Errorf("decode update source: %w", err)
	}
	if !revisionPattern.MatchString(target.Locked.Revision) || target.URL == "" || !strings.Contains(target.URL, target.Locked.Revision) {
		return updateTarget{}, fmt.Errorf("update source must resolve to an immutable Git revision")
	}
	return target, nil
}

func runResolvedUpdate(ctx context.Context, runner run.CommandRunner, p paths.Options, inputs []string, target updateTarget, sourceURL string, noSwitch, dryRun bool) error {
	buildRoot, err := os.MkdirTemp("", "wahrwelt-updater-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(buildRoot) }()
	packagePath, err := runner.Output(ctx, "nix", "--extra-experimental-features", "nix-command flakes",
		"build", "--out-link", filepath.Join(buildRoot, "result"), "--print-out-paths", "--no-update-lock-file", target.URL+"#wahrwelt")
	if err != nil {
		return fmt.Errorf("build Wahrwelt updater: %w", err)
	}
	packagePath = strings.TrimSpace(packagePath)
	if strings.ContainsAny(packagePath, "\n\r") || !strings.HasPrefix(packagePath, "/nix/store/") || filepath.Clean(packagePath) != packagePath {
		return fmt.Errorf("updater build did not return one Nix store path")
	}
	executable := filepath.Join(packagePath, "bin", "wahrwelt")
	args := []string{"update", "--dest", p.NixOSDest, "--state", p.StatePath,
		"--target-rev", target.Locked.Revision, "--target-url", target.URL, "--source-url", sourceURL}
	if noSwitch {
		args = append(args, "--no-switch")
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, "--")
	args = append(args, inputs...)
	if os.Geteuid() != 0 && !dryRun {
		args = append([]string{"--preserve-env=SSH_AUTH_SOCK", "--", executable}, args...)
		executable = "sudo"
	}
	return runner.Command(ctx, executable, args...)
}
