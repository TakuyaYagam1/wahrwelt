package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
)

func runUpdate(ctx context.Context, s *session) error {
	state, args, err := installedUpdate(s)
	if err != nil {
		return err
	}
	confirm := false
	action := "Update"
	if s.dryRun {
		action = "Preview"
	}
	if err := newForm(
		huh.NewConfirm().
			Title(updateSection).
			Description(fmt.Sprintf("Update %s using its saved settings. Custom inputs and local modules are preserved. Changes made in this installer session are applied separately with Apply.", state.Host.Hostname)).
			Affirmative(action).
			Negative("Back").
			Value(&confirm),
	).Run(); err != nil {
		return err
	}
	if !confirm {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find installer executable: %w", err)
	}
	if err := run.New(false).Command(ctx, executable, args...); err != nil {
		return err
	}
	if s.dryRun {
		return showNote("Update preview complete", "The proposed update was shown in the command output.")
	}
	return showNote("Update complete", "The installed system has been updated.")
}

func installedUpdate(s *session) (config.State, []string, error) {
	opts := s.paths
	dest, err := filepath.Abs(opts.NixOSDest)
	if err != nil {
		return config.State{}, nil, err
	}
	if opts.StatePath == paths.DefaultStatePath && dest != "/etc/nixos" {
		opts.StatePath = filepath.Join(dest, "installer-state.json")
	}
	statePath, err := opts.ExistingStatePath()
	if err != nil {
		return config.State{}, nil, err
	}
	state, err := config.LoadGeneratedExisting(statePath)
	if err != nil {
		return config.State{}, nil, fmt.Errorf("load installed system settings: %w", err)
	}
	if err := config.Validate(state); err != nil {
		return config.State{}, nil, fmt.Errorf("invalid installed system settings: %w", err)
	}
	args := []string{"update", "--dest", dest, "--state", statePath}
	if s.dryRun {
		args = append(args, "--dry-run")
	}
	return state, args, nil
}
