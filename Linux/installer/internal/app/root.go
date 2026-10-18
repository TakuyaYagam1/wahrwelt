package app

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/cleanup"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/doctor"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/rollback"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/secrets"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/tui"
)

var Version = ""

func resolveVersion() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			rev := s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return "dev+" + rev
		}
	}
	return "dev"
}

func NewRootCommand() *cobra.Command {
	opts := Options{Options: paths.DefaultOptions()}

	root := &cobra.Command{
		Use:     "wahrwelt",
		Short:   "Catppuccin Macchiato TUI installer for Wahrwelt",
		Version: resolveVersion(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tui.Run(cmd.Context(), tui.Options{
				Paths:    opts.Options,
				DryRun:   opts.DryRun,
				Layout:   apply.Layout(opts.Layout),
				LockMode: apply.LockMode(opts.LockMode),
			})
		},
	}

	root.PersistentFlags().StringVar(&opts.RepoRoot, "repo", "", "Wahrwelt repository root")
	root.PersistentFlags().StringVar(&opts.NixOSDest, "dest", opts.NixOSDest, "NixOS destination directory")
	root.PersistentFlags().StringVar(&opts.StatePath, "state", opts.StatePath, "machine-local state file")
	root.PersistentFlags().StringVar(&opts.DraftPath, "draft", opts.DraftPath, "user draft state file")
	root.PersistentFlags().BoolVar(&opts.DryRun, "dry-run", false, "print actions without changing files")
	root.PersistentFlags().BoolVar(&opts.Yes, "yes", false, "skip confirmation prompts where safe")
	root.PersistentFlags().StringVar(&opts.Layout, "layout", "thin", "installed /etc/nixos layout: thin or full")
	root.PersistentFlags().StringVar(&opts.LockMode, "lock-mode", string(apply.LockModeIndependent), "thin flake lock ownership: independent or managed")

	root.AddCommand(tuiCommand(&opts))
	root.AddCommand(applyCommand(&opts))
	root.AddCommand(updateCommand(&opts))
	root.AddCommand(doctorCommand(&opts))
	root.AddCommand(cleanupCommand(&opts))
	root.AddCommand(rollbackCommand(&opts))
	root.AddCommand(printStateCommand(&opts))
	root.AddCommand(migrateCommand(&opts))

	return root
}

func rollbackCommand(opts *Options) *cobra.Command {
	var backupPath string
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Restore /etc/nixos from a previous Wahrwelt backup",
		Long: "Picks the most recent <dest>.bak.* directory (or --backup <path>) " +
			"and rsync --delete's it back into the destination. Does not roll the " +
			"active system generation; run `sudo nixos-rebuild switch --rollback` for that.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return rollback.Run(cmd.Context(), rollback.Options{
				Paths:  opts.Options,
				Backup: backupPath,
				DryRun: opts.DryRun,
				Yes:    opts.Yes,
			})
		},
	}
	cmd.Flags().StringVar(&backupPath, "backup", "", "explicit backup directory; defaults to the latest <dest>.bak.* sibling")
	return cmd
}

func tuiCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive installer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tui.Run(cmd.Context(), tui.Options{
				Paths:    opts.Options,
				DryRun:   opts.DryRun,
				Layout:   apply.Layout(opts.Layout),
				LockMode: apply.LockMode(opts.LockMode),
			})
		},
	}
}

func applyCommand(opts *Options) *cobra.Command {
	var noSwitch bool
	var sourceChannel string
	var userPasswordFile string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply saved state to /etc/nixos and user dotfiles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, loadedStateProof, err := loadApplyState(opts.Options, sourceChannel)
			if err != nil {
				return err
			}
			secretValues, err := secrets.LoadFromFiles(userPasswordFile)
			if err != nil {
				return err
			}
			return apply.Run(cmd.Context(), apply.Options{
				Paths:            opts.Options,
				State:            state,
				LoadedStateProof: loadedStateProof,
				Secrets:          secretValues,
				DryRun:           opts.DryRun,
				AssumeYes:        opts.Yes,
				SkipSwitch:       noSwitch,
				Layout:           apply.Layout(opts.Layout),
				LockMode:         apply.LockMode(opts.LockMode),
			})
		},
	}
	cmd.Flags().BoolVar(&noSwitch, "no-switch", false, "stop after dry-build without switching or writing activated state")
	cmd.Flags().StringVar(&sourceChannel, "source-channel", "", "override saved Wahrwelt channel: stable or development")
	cmd.Flags().StringVar(&userPasswordFile, "user-password-file", "", "read the initial user password from a private file")
	return cmd
}

func loadApplyState(pathOptions paths.Options, sourceChannel string) (config.State, apply.LoadedStateProof, error) {
	statePath, err := pathOptions.ExistingStatePath()
	if err != nil {
		return config.State{}, apply.LoadedStateProof{}, err
	}
	state, loadedStateProof, err := apply.LoadStateWithProof(statePath)
	if err != nil {
		return config.State{}, apply.LoadedStateProof{}, err
	}
	if sourceChannel != "" {
		if !config.IsSourceChannel(sourceChannel) {
			return config.State{}, apply.LoadedStateProof{}, fmt.Errorf("source channel must be stable or development")
		}
		state.Source.Channel = sourceChannel
	}
	return state, loadedStateProof, nil
}

func doctorCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check current Wahrwelt installation health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			statePath, err := opts.ExistingStatePath()
			if err != nil {
				return err
			}
			state, err := loadDoctorState(statePath)
			if err != nil {
				return err
			}
			return doctor.Run(cmd.Context(), doctor.Options{Paths: opts.Options, State: state})
		},
	}
}

func loadDoctorState(path string) (config.State, error) {
	state, err := config.Load(path)
	if err == nil {
		return state, nil
	}
	if _, writeErr := fmt.Fprintf(os.Stderr, "WARN could not load state %s: %v\n", path, err); writeErr != nil {
		return config.State{}, writeErr
	}
	return config.Default(), nil
}

func cleanupCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Clean safe managed leftovers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			statePath, err := opts.ExistingStatePath()
			if err != nil {
				return err
			}
			state, err := loadDoctorState(statePath)
			if err != nil {
				return err
			}
			return cleanup.Run(cmd.Context(), cleanup.Options{
				Paths:  opts.Options,
				State:  state,
				DryRun: opts.DryRun,
				Yes:    opts.Yes,
			})
		},
	}
}

func printStateCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "print-state",
		Short: "Print the saved machine state as JSON",
		RunE: func(_ *cobra.Command, _ []string) error {
			statePath, err := opts.ExistingStatePath()
			if err != nil {
				return err
			}
			state, err := config.LoadExisting(statePath)
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(os.Stdout, string(data))
			return err
		},
	}
}
