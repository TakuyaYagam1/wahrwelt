package secrets

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
)

const MaxSecretFileBytes = 64 * 1024

func LoadFromFiles(userPasswordFile string) (config.Secrets, error) {
	userPassword, err := readFile(userPasswordFile)
	if err != nil {
		return config.Secrets{}, fmt.Errorf("read user password file: %w", err)
	}
	return config.Secrets{UserPassword: userPassword}, nil
}

func readFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	file, err := openNoFollow(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("secret file must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("secret file must be a regular file: %s", path)
	}
	if info.Size() > MaxSecretFileBytes {
		return "", fmt.Errorf("secret file is too large: %s", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("secret file permissions are too open: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxSecretFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxSecretFileBytes {
		return "", fmt.Errorf("secret file is too large: %s", path)
	}
	secret := strings.TrimRight(string(data), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("secret file is empty: %s", path)
	}
	return secret, nil
}

func openNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("secret file must not be a symlink: %s", path)
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
