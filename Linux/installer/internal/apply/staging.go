package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
)

func stageConfiguration(
	ctx context.Context,
	runner run.CommandRunner,
	src paths.Sources,
	staging string,
	state config.State,
	layout Layout,
	lockMode LockMode,
) error {
	if layout == LayoutThin {
		return stageThinConfiguration(src, staging, state, lockMode)
	}
	return stageFullConfiguration(ctx, runner, src, staging, state)
}

func stageFullConfiguration(ctx context.Context, runner run.CommandRunner, src paths.Sources, staging string, state config.State) error {
	if err := copyNixOS(ctx, runner, src.NixOS, staging, false); err != nil {
		return err
	}
	if err := ensureStagingWritable(staging); err != nil {
		return err
	}
	if err := copyAuxiliarySources(ctx, runner, src, staging); err != nil {
		return err
	}
	return writeGenerated(staging, state)
}

func stageThinConfiguration(src paths.Sources, staging string, state config.State, lockMode LockMode) error {
	if err := ensureStagingWritable(staging); err != nil {
		return err
	}
	if err := writeGenerated(staging, state, LayoutThin); err != nil {
		return err
	}
	if err := writeThinTemplates(staging, state, lockMode); err != nil {
		return err
	}
	_ = src
	return nil
}

func ensureStagingWritable(staging string) error {
	info, err := os.Stat(staging)
	if err != nil {
		return err
	}
	return os.Chmod(staging, info.Mode().Perm()|0o700)
}

func copyAuxiliarySources(ctx context.Context, runner run.CommandRunner, src paths.Sources, staging string) error {
	if err := copyTree(ctx, runner, src.Dots, filepath.Join(staging, "dots")); err != nil {
		return fmt.Errorf("stage dots: %w", err)
	}
	if err := copyTree(
		ctx,
		runner,
		src.Installer,
		filepath.Join(staging, "installer"),
		"--exclude=/bin/",
		"--exclude=/tmp/",
		"--exclude=/coverage.out",
		"--exclude=/coverage.html",
	); err != nil {
		return fmt.Errorf("stage installer: %w", err)
	}
	return nil
}

func copyTree(ctx context.Context, runner run.CommandRunner, src, dst string, extraExcludes ...string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a directory: %s", src)
	}
	args := make([]string, 0, 5+len(extraExcludes))
	args = append(args, "-a", "--delete", "--checksum")
	args = append(args, extraExcludes...)
	args = append(args, src+"/", dst+"/")
	return runner.Command(ctx, "rsync", args...)
}

func copyNixOS(ctx context.Context, runner run.CommandRunner, src, dst string, deleteExtra bool) error {
	args := []string{"-a", "--checksum"}
	if deleteExtra {
		args = append(args, "--delete")
	}
	args = append(args,
		"--exclude=/wahrwelt/",
		"--exclude=/mysetup/",
		"--exclude=/secrets/",
		"--exclude=hosts/NixOS/hardware-configuration.nix",
		"--exclude=hosts/NixOS/hashed-password.nix",
		"--exclude=hosts/NixOS/secrets/secrets.yaml",
		"--exclude=home/secrets/secrets.yaml",
		src+"/",
		dst+"/",
	)
	return runner.Command(ctx, "rsync", args...)
}

func copyHostHardware(staging, dest string, layout Layout) error {
	target := filepath.Join(staging, hardwareRel(layout))
	for _, source := range []string{
		filepath.Join(dest, "hardware-configuration.nix"),
		filepath.Join(dest, "hosts", "NixOS", "hardware-configuration.nix"),
	} {
		if _, err := os.Stat(source); err == nil {
			return copyFile(source, target)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return fmt.Errorf("hardware-configuration.nix not found; run sudo nixos-generate-config --root / first")
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, info.Mode().Perm())
}

func writeGenerated(staging string, state config.State, layout ...Layout) error {
	targetLayout := LayoutFull
	if len(layout) > 0 {
		targetLayout = layout[0]
	}
	hostVars, err := HostVarsNix(state)
	if err != nil {
		return err
	}
	files := map[string]string{
		hostVarsRel(targetLayout): hostVars,
	}
	for rel, content := range files {
		path := filepath.Join(staging, rel)
		if err := prepareGeneratedFile(path); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	return nil
}

func writeThinTemplates(staging string, state config.State, lockMode ...LockMode) error {
	targetLockMode := LockModeIndependent
	if len(lockMode) > 0 {
		targetLockMode = lockMode[0]
	}
	flake, err := FlakeNix(state, targetLockMode)
	if err != nil {
		return err
	}
	plan, err := PlanFlake(flake, state, nil)
	if err != nil {
		return err
	}
	files := map[string]string{
		"flake.nix":         flake,
		"configuration.nix": ConfigurationNix(),
		"home.nix":          HomeNix(),
	}
	for rel, content := range files {
		path := filepath.Join(staging, rel)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := prepareGeneratedFile(path); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, FlakeStateName), plan.State, 0o644); err != nil {
		return fmt.Errorf("write flake state: %w", err)
	}
	return writeUserDefaultTemplate(staging)
}

func writeUserDefaultTemplate(staging string) error {
	rel := filepath.Join("user", "default.nix")
	path := filepath.Join(staging, rel)
	exists, err := preservableUserDefault(path)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			exists, checkErr := preservableUserDefault(path)
			if checkErr != nil {
				return checkErr
			}
			if exists {
				return nil
			}
			return fmt.Errorf("user/default.nix disappeared while creating: %s", path)
		}
		return err
	}
	if _, err := file.WriteString(UserDefaultNix()); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return fmt.Errorf("set mode for %s: %w", rel, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", rel, err)
	}
	created, err := preservableUserDefault(path)
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("user/default.nix disappeared while writing: %s", path)
	}
	return nil
}

func preservableUserDefault(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	return false, fmt.Errorf("unsupported user/default.nix path: %s", path)
}

func prepareGeneratedFile(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if info, err := os.Stat(parent); err == nil {
		if err := os.Chmod(parent, info.Mode().Perm()|0o700); err != nil {
			return err
		}
	} else {
		return err
	}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite non-regular generated file: %s", path)
	}
	return os.Chmod(path, info.Mode().Perm()|0o600)
}
