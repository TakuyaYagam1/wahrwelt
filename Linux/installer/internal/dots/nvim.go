package dots

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
	"golang.org/x/sys/unix"
)

func syncNvim(ctx context.Context, runner run.CommandRunner, dotsSrc, configDir, username string) error {
	return syncNvimWithHook(ctx, runner, dotsSrc, configDir, username, nil)
}

type nvimBootstrapHook func(stage string) error

func syncNvimWithHook(ctx context.Context, runner run.CommandRunner, dotsSrc, configDir, username string, hook nvimBootstrapHook) error {
	src := filepath.Join(dotsSrc, "nvim")
	if info, err := os.Lstat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("neovim source is not a real directory: %s", src)
	}
	dst := filepath.Join(configDir, "nvim")
	if err := validateNvimDirectoryPath(dst); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		// A source hash is not a per-file baseline. Home Manager reconciles
		// existing trees, including edited files and intentional deletions.
		fmt.Printf("Preserving existing Neovim config in %s; updates are handled by Home Manager\n", dst)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := validateNvimSource(src); err != nil {
		return err
	}
	if runner.IsDryRun() {
		fmt.Printf("Would bootstrap absent Neovim config in %s\n", dst)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return bootstrapNvim(ctx, runner, src, configDir, username, hook)
}

func bootstrapNvim(ctx context.Context, runner run.CommandRunner, src, configDir, username string, hook nvimBootstrapHook) error {
	configDir, err := filepath.Abs(configDir)
	if err != nil {
		return err
	}
	root, err := pinOrdinaryDirectory("/")
	if err != nil {
		return err
	}
	defer root.close()
	home, err := pinDirectoryBeneath(root, strings.TrimPrefix(filepath.Dir(configDir), "/"))
	if err != nil {
		return fmt.Errorf("pin Neovim home: %w", err)
	}
	defer home.close()
	if hook != nil {
		if err := hook("before-config-create"); err != nil {
			return err
		}
	}
	if err := home.verifyVisible(); err != nil {
		return err
	}
	configName := filepath.Base(configDir)
	if err := unix.Mkdirat(home.descriptor, configName, 0o755); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("create Neovim config parent: %w", err)
	}
	parent, err := pinDirectoryBeneath(home, configName)
	if err != nil {
		return fmt.Errorf("pin Neovim config parent: %w", err)
	}
	defer parent.close()
	if err := home.verifyVisible(); err != nil {
		return err
	}
	// Pin the parent for publication and cleanup. RENAME_NOREPLACE also
	// refuses a file or symlink that appears while preparing the bootstrap.
	bootstrap, err := createPinnedRecoveryDirectory(parent, ".nvim-bootstrap-")
	if err != nil {
		return fmt.Errorf("prepare Neovim bootstrap: %w", err)
	}
	defer bootstrap.close()
	keepBootstrap := false
	defer func() {
		// Once published, files inside the directory may have user edits even
		// when its root inode is unchanged. Retain all postpublication recovery.
		if !keepBootstrap && bootstrap.verifyVisible() == nil {
			_ = os.RemoveAll(parent.child(bootstrap.relative))
		}
	}()
	if err := prepareNvimBootstrap(ctx, runner, src, bootstrap, username); err != nil {
		return err
	}
	keepBootstrap, err = publishNvimBootstrap(ctx, home, parent, bootstrap, hook)
	return err
}

func prepareNvimBootstrap(ctx context.Context, runner run.CommandRunner, src string, bootstrap *migrationPinnedDirectory, username string) error {
	temp := bootstrap.procPath()
	if err := os.CopyFS(temp, os.DirFS(src)); err != nil {
		return fmt.Errorf("copy Neovim bootstrap: %w", err)
	}
	if err := ensureUserWritableTree(ctx, runner, temp, username); err != nil {
		return err
	}
	sourceHash, err := sourceTreeHash(src, nil)
	if err != nil {
		return err
	}
	return writeMarkerWithOwnerAndSourceHash(ctx, runner, filepath.Join(temp, managedMarkerName), "nvim", username, sourceHash)
}

func publishNvimBootstrap(ctx context.Context, home, parent, bootstrap *migrationPinnedDirectory, hook nvimBootstrapHook) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if hook != nil {
		if err := hook("before-publish"); err != nil {
			return false, err
		}
	}
	if err := verifyNvimParents(home, parent); err != nil {
		return false, err
	}
	if err := bootstrap.verifyVisible(); err != nil {
		return false, err
	}
	if err := unix.Renameat2(parent.descriptor, bootstrap.relative, parent.descriptor, "nvim", unix.RENAME_NOREPLACE); err != nil {
		return false, fmt.Errorf("publish absent Neovim config: %w", err)
	}
	var publishErr error
	if hook != nil {
		publishErr = hook("after-publish")
	}
	if publishErr == nil {
		publishErr = verifyNvimParents(home, parent)
	}
	if publishErr != nil {
		return true, errors.Join(publishErr, rollbackNvimBootstrap(parent, bootstrap))
	}
	return true, nil
}

func verifyNvimParents(home, parent *migrationPinnedDirectory) error {
	if err := home.verifyVisible(); err != nil {
		return err
	}
	return parent.verifyVisible()
}

func rollbackNvimBootstrap(parent, bootstrap *migrationPinnedDirectory) error {
	visible, err := os.Lstat(parent.child("nvim"))
	if err != nil || !os.SameFile(visible, bootstrap.info) {
		return fmt.Errorf("neovim bootstrap changed; retaining configuration for recovery at %s/nvim", parent.path)
	}
	if err := unix.Renameat2(parent.descriptor, "nvim", parent.descriptor, bootstrap.relative, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("retain Neovim bootstrap after parent change: %w", err)
	}
	if err := bootstrap.verifyVisible(); err != nil {
		// A concurrent replacement won the race after Lstat. Restore it when
		// possible; never delete it or overwrite a new occupant of nvim.
		restoreErr := unix.Renameat2(parent.descriptor, bootstrap.relative, parent.descriptor, "nvim", unix.RENAME_NOREPLACE)
		return errors.Join(fmt.Errorf("neovim configuration changed during rollback: %w", err), restoreErr)
	}
	recoveryPath, err := os.Readlink(bootstrap.procPath())
	if err != nil {
		return fmt.Errorf("neovim bootstrap retained from %s; resolve current recovery location: %w", bootstrap.path, err)
	}
	return fmt.Errorf("neovim bootstrap retained for recovery at %s", recoveryPath)
}

func validateNvimDirectoryPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && !info.IsDir() {
			return fmt.Errorf("neovim destination ownership collision (expected a real directory): %s", current)
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func validateNvimSource(src string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("neovim bootstrap source is not a regular file or directory: %s", path)
		}
		return nil
	})
}
