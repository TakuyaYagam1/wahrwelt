package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/hosttree"
)

func readFlakeState(root string) ([]byte, error) {
	path := filepath.Join(root, FlakeStateName)
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("flake state must be an ordinary file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Size() != int64(len(data)) {
		return nil, fmt.Errorf("flake state changed while reading: %s", path)
	}
	return data, nil
}

// Extra host files are available during evaluation but excluded from thin
// publication. Their contents and permissions remain owned by the user.
func stageHostExtras(staging, dest string, layout Layout) (*hosttree.Snapshot, error) {
	if layout != LayoutThin {
		return nil, nil
	}
	_, err := os.Lstat(filepath.Join(dest, "flake.nix"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return nil, err
	}
	skip := []string{"installer-state.json", "wahrwelt", "mysetup", "private", "secrets", "hashed-password.nix"}
	for _, entry := range entries {
		skip = append(skip, entry.Name())
	}
	snapshot, err := hosttree.Copy(dest, staging, skip)
	if err != nil {
		return nil, fmt.Errorf("stage custom host files: %w", err)
	}
	return &snapshot, nil
}
