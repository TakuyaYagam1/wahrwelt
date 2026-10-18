package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/config"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/defaults"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/fsowner"
	migrationv1tov2 "github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/migrations/v1_to_v2"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/paths"
	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/run"
	"golang.org/x/sys/unix"
)

func prepareStagingHostLocal(ctx context.Context, runner run.CommandRunner, staging, dest string, state config.State, secrets config.Secrets, layout Layout) error {
	if layout == LayoutThin {
		if err := copyExistingThinHostLocal(ctx, runner, staging, dest, state); err != nil {
			return err
		}
	}
	if err := copyHostHardware(staging, dest, layout); err != nil {
		return err
	}
	return preparePasswordHashMarker(ctx, runner, staging, dest, secrets, layout)
}

func preparePasswordHashMarker(ctx context.Context, runner run.CommandRunner, staging, dest string, secrets config.Secrets, _ Layout) error {
	configured := secrets.UserPassword != ""
	if !configured {
		target := passwordHashTarget(dest)
		var err error
		configured, err = externalPasswordHashConfigured(
			ctx,
			runner,
			target,
			os.Lstat,
			privilegedFSHelperPath,
		)
		if err != nil {
			return err
		}
	}
	if !configured {
		for _, source := range existingHashedPasswordPaths(dest) {
			info, err := os.Lstat(source)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("legacy password hash path is not a regular file: %s", source)
			}
			configured = true
			break
		}
	}
	if !configured {
		return nil
	}
	marker := filepath.Join(staging, paths.PasswordHashMarkerName)
	return os.WriteFile(marker, nil, 0o644)
}

func externalPasswordHashConfigured(
	ctx context.Context,
	runner run.CommandRunner,
	target string,
	lstat func(string) (os.FileInfo, error),
	resolveHelper func() (string, error),
) (bool, error) {
	info, err := lstat(target)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("external password hash path is not a regular file: %s", target)
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	if !os.IsPermission(err) || filepath.Clean(target) != paths.DefaultPasswordHashPath {
		return false, err
	}
	helper, helperErr := resolveHelper()
	if helperErr != nil {
		return false, helperErr
	}
	if validateErr := validateExternalPasswordHashWithHelper(ctx, runner, target, helper); validateErr != nil {
		return false, fmt.Errorf("validate inaccessible external password hash: %w", validateErr)
	}
	return true, nil
}

func passwordHashTarget(dest string) string {
	if filepath.Clean(dest) == "/etc/nixos" {
		return paths.DefaultPasswordHashPath
	}
	return filepath.Join(filepath.Dir(filepath.Clean(dest)), "wahrwelt", "hashed-password")
}

func copyExistingThinHostLocal(ctx context.Context, runner run.CommandRunner, staging, dest string, state config.State) error {
	if _, _, err := existingUserDirectorySource(dest); err != nil {
		return err
	}
	thinFlake, err := copyExistingThinFlake(dest, filepath.Join(staging, "flake.nix"), state)
	if err != nil {
		return err
	}

	preservedFiles := []string{}
	if thinFlake {
		preservedFiles = []string{"flake.lock", "configuration.nix", "home.nix"}
	}
	if err := copyExistingThinHostLocalFiles(dest, staging, preservedFiles); err != nil {
		return err
	}

	if err := stageExistingUserDirectory(ctx, runner, dest, staging); err != nil {
		return err
	}
	if err := writeUserDefaultTemplate(staging); err != nil {
		return err
	}

	return copyExistingThinSecrets(ctx, runner, dest, staging)
}

func copyExistingThinHostLocalFiles(dest, staging string, names []string) error {
	for _, name := range names {
		source := filepath.Join(dest, name)
		if _, err := os.Stat(source); err == nil {
			target := filepath.Join(staging, name)
			if err := copyFile(source, target); err != nil {
				return err
			}
			if name == "configuration.nix" {
				if err := migrateConfigurationUserImport(target); err != nil {
					return err
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyExistingThinSecrets(ctx context.Context, runner run.CommandRunner, dest, staging string) error {
	for _, secretsDir := range existingSecretsDirs(dest) {
		if info, err := os.Lstat(secretsDir); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("secrets path must be an ordinary directory: %s", secretsDir)
			}
			source := filepath.Join(secretsDir, "secrets.yaml")
			target := filepath.Join(staging, "secrets", "secrets.yaml")
			copied, err := copyValidatedEncryptedSecretsFile(ctx, runner, source, target)
			if err != nil {
				return fmt.Errorf("stage secrets: %w", err)
			}
			if copied {
				return nil
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

const maxEncryptedSecretsBytes = 4 << 20

var encryptedSOPSValuePattern = regexp.MustCompile(`ENC\[[A-Z0-9_]+,`)

func copyValidatedEncryptedSecretsFile(ctx context.Context, runner run.CommandRunner, source, target string) (bool, error) {
	file, err := openSecretsFileNoFollow(source)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		if os.IsPermission(err) {
			if runner.IsDryRun() {
				return true, runner.Command(ctx, "sudo", "install", "-D", "-m", "600", source, target)
			}
			return true, sudoCopyValidatedEncryptedSecretsFile(ctx, runner, source, target)
		}
		return false, err
	}
	defer closeFile(file)
	data, err := io.ReadAll(io.LimitReader(file, maxEncryptedSecretsBytes+1))
	if err != nil {
		return false, err
	}
	if err := validateEncryptedSOPSPayload(data); err != nil {
		return false, fmt.Errorf("%s is not a validated SOPS payload: %w", source, err)
	}
	if runner.IsDryRun() {
		return true, runner.Command(ctx, "install", "-D", "-m", "600", source, target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return false, err
	}
	targetFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, fmt.Errorf("create staged secrets payload: %w", err)
	}
	writeErr := error(nil)
	if _, err := targetFile.Write(data); err != nil {
		writeErr = err
	} else if err := targetFile.Sync(); err != nil {
		writeErr = err
	}
	if err := targetFile.Close(); writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		return false, fmt.Errorf("write staged secrets payload; failed candidate retained at %s: %w", target, writeErr)
	}
	return true, nil
}

func openSecretsFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file, wrapErr := fileFromUnixDescriptor(fd, path)
	if wrapErr != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open secrets payload: %w", wrapErr)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("secrets payload must be a regular file: %s", path)
	}
	return file, nil
}

func validateEncryptedSOPSPayload(data []byte) error {
	if len(data) == 0 || len(data) > maxEncryptedSecretsBytes {
		return fmt.Errorf("payload size is outside the allowed range")
	}
	hasSOPS := false
	hasEncryptedValue := false
	hasEncryptedMAC := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		isMAC := bytes.HasPrefix(trimmed, []byte("mac:"))
		switch {
		case bytes.Equal(line, []byte("sops:")):
			hasSOPS = true
		case !isMAC && encryptedSOPSValuePattern.Match(trimmed):
			hasEncryptedValue = true
		}
		if isMAC && encryptedSOPSValuePattern.Match(trimmed) {
			hasEncryptedMAC = true
		}
	}
	if !hasSOPS || !hasEncryptedValue || !hasEncryptedMAC {
		return fmt.Errorf("missing SOPS metadata or encrypted values")
	}
	return nil
}

//nolint:gosec // This embedded helper contains only SOPS field names and performs no credential storage.
const privilegedCopyEncryptedSecretsPython = `
import os
import re
import stat
import sys

source_dir, target_dir, owner, group = sys.argv[1:]
owner = int(owner)
group = int(group)
limit = 4 * 1024 * 1024

parent = os.open(source_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
try:
    source = os.open("secrets.yaml", os.O_RDONLY | os.O_NOFOLLOW, dir_fd=parent)
    try:
        info = os.fstat(source)
        if not stat.S_ISREG(info.st_mode):
            raise RuntimeError("secrets payload is not a regular file")
        data = os.read(source, limit + 1)
        if not data or len(data) > limit:
            raise RuntimeError("secrets payload size is outside the allowed range")
        lines = data.splitlines()
        encrypted = re.compile(rb"ENC\[[A-Z0-9_]+,")
        if b"sops:" not in lines or not any(not line.strip().startswith(b"mac:") and encrypted.search(line.strip()) for line in lines):
            raise RuntimeError("secrets payload has no SOPS metadata or encrypted values")
        if not any(line.strip().startswith(b"mac:") and encrypted.search(line.strip()) for line in lines):
            raise RuntimeError("secrets payload has no encrypted SOPS MAC")
        os.makedirs(target_dir, mode=0o700, exist_ok=True)
        target_parent = os.open(target_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            target = os.open("secrets.yaml", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=target_parent)
            try:
                offset = 0
                while offset < len(data):
                    offset += os.write(target, data[offset:])
                os.fsync(target)
                os.fchmod(target, 0o600)
                os.fchown(target, owner, group)
            finally:
                os.close(target)
        finally:
            os.close(target_parent)
    finally:
        os.close(source)
finally:
    os.close(parent)
`

func sudoCopyValidatedEncryptedSecretsFile(ctx context.Context, runner run.CommandRunner, source, target string) error {
	pythonPath, err := privilegedPythonPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return runner.Command(ctx, "sudo", pythonPath, "-c", privilegedCopyEncryptedSecretsPython,
		filepath.Dir(source), filepath.Dir(target), strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid()))
}

func stageExistingUserDirectory(ctx context.Context, runner run.CommandRunner, dest, staging string) error {
	source, exists, err := existingUserDirectorySource(dest)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := copyHostLocalTree(ctx, runner, source, filepath.Join(staging, "user")); err != nil {
		return fmt.Errorf("stage user directory: %w", err)
	}
	return nil
}

func existingUserDirectorySource(dest string) (string, bool, error) {
	userDir := filepath.Join(dest, "user")
	legacyDir := filepath.Join(dest, "private")
	userExists, err := ordinaryDirectory(userDir, "user")
	if err != nil {
		return "", false, err
	}
	legacyExists, err := ordinaryDirectory(legacyDir, "legacy user")
	if err != nil {
		return "", false, err
	}
	if userExists && legacyExists {
		return "", false, fmt.Errorf("both user and legacy private directories exist; migrate manually before applying")
	}
	if userExists {
		return userDir, true, nil
	}
	if legacyExists {
		return legacyDir, true, nil
	}
	return "", false, nil
}

func ordinaryDirectory(path, label string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("unsupported %s path: %s", label, path)
	}
	return true, nil
}

func migrateConfigurationUserImport(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	migrated := rewriteLegacyUserImport(string(data))
	if migrated == string(data) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(migrated), info.Mode().Perm())
}

func rewriteLegacyUserImport(text string) string {
	rewritten, _ := rewriteNixCode(text, 0, false)
	return rewritten
}

var (
	nixURITokenPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:[A-Za-z0-9%/?:@&=+$,_.!~*'-]+`)
	nixSearchPathPattern = regexp.MustCompile(`^<[A-Za-z0-9._+-]+(?:/[A-Za-z0-9._+-]+)*>`)
	nixPathTokenPattern  = regexp.MustCompile(`^[A-Za-z0-9._+-]*(?:/[A-Za-z0-9._+-]+)+/?`)
	nixHomePathPattern   = regexp.MustCompile(`^~/(?:[A-Za-z0-9._+-]+/)*[A-Za-z0-9._+-]*`)
	nixInPathLiteral     = regexp.MustCompile(`^[A-Za-z0-9._+/-]+`)
	nixIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_'-]*`)
)

const maxPasswordModuleBytes = 16 * 1024

func rewriteNixCode(text string, index int, stopAtClosingBrace bool) (string, int) {
	var out strings.Builder
	for index < len(text) {
		if stopAtClosingBrace && text[index] == '}' {
			out.WriteByte('}')
			return out.String(), index + 1
		}
		if text[index] == '#' {
			end := strings.IndexByte(text[index:], '\n')
			if end < 0 {
				out.WriteString(text[index:])
				return out.String(), len(text)
			}
			end += index + 1
			out.WriteString(text[index:end])
			index = end
			continue
		}
		if strings.HasPrefix(text[index:], "/*") {
			end := nixBlockCommentEnd(text, index)
			out.WriteString(text[index:end])
			index = end
			continue
		}
		if text[index] == '"' {
			rewritten, end := rewriteNixQuotedString(text, index)
			out.WriteString(rewritten)
			index = end
			continue
		}
		if strings.HasPrefix(text[index:], "''") {
			rewritten, end := rewriteNixIndentedString(text, index)
			out.WriteString(rewritten)
			index = end
			continue
		}
		if rewritten, end, ok := rewriteNixInterpolatedPath(text, index); ok {
			out.WriteString(rewritten)
			index = end
			continue
		}
		if stopAtClosingBrace && text[index] == '{' {
			out.WriteByte('{')
			rewritten, end := rewriteNixCode(text, index+1, true)
			out.WriteString(rewritten)
			index = end
			continue
		}
		if replacement, end, ok := rewriteNixToken(text, index); ok {
			out.WriteString(replacement)
			index = end
			continue
		}
		out.WriteByte(text[index])
		index++
	}
	return out.String(), index
}

func rewriteNixQuotedString(text string, index int) (string, int) {
	var out strings.Builder
	out.WriteByte('"')
	index++
	for index < len(text) {
		if text[index] == '\\' {
			out.WriteByte(text[index])
			index++
			if index < len(text) {
				out.WriteByte(text[index])
				index++
			}
			continue
		}
		if text[index] == '"' {
			out.WriteByte('"')
			return out.String(), index + 1
		}
		if strings.HasPrefix(text[index:], "${") {
			out.WriteString("${")
			rewritten, end := rewriteNixCode(text, index+2, true)
			out.WriteString(rewritten)
			index = end
			continue
		}
		out.WriteByte(text[index])
		index++
	}
	return out.String(), index
}

func rewriteNixIndentedString(text string, index int) (string, int) {
	var out strings.Builder
	out.WriteString("''")
	index += 2
	for index < len(text) {
		if strings.HasPrefix(text[index:], "''") && index+2 < len(text) {
			switch text[index+2] {
			case '$', '\'':
				out.WriteString(text[index : index+3])
				index += 3
				continue
			case '\\':
				end := index + 3
				if end < len(text) {
					end++
				}
				out.WriteString(text[index:end])
				index = end
				continue
			}
		}
		if strings.HasPrefix(text[index:], "''") {
			out.WriteString("''")
			return out.String(), index + 2
		}
		if strings.HasPrefix(text[index:], "${") {
			out.WriteString("${")
			rewritten, end := rewriteNixCode(text, index+2, true)
			out.WriteString(rewritten)
			index = end
			continue
		}
		out.WriteByte(text[index])
		index++
	}
	return out.String(), index
}

func nixBlockCommentEnd(text string, index int) int {
	depth := 1
	index += 2
	for index < len(text) && depth > 0 {
		switch {
		case strings.HasPrefix(text[index:], "/*"):
			depth++
			index += 2
		case strings.HasPrefix(text[index:], "*/"):
			depth--
			index += 2
		default:
			index++
		}
	}
	return index
}

func rewriteNixToken(text string, index int) (string, int, bool) {
	remaining := text[index:]
	if token := nixURITokenPattern.FindString(remaining); token != "" {
		return token, index + len(token), true
	}
	if token := nixSearchPathPattern.FindString(remaining); token != "" {
		return token, index + len(token), true
	}
	if strings.HasPrefix(remaining, "//") {
		return "//", index + 2, true
	}
	if token := nixPathTokenPattern.FindString(remaining); token != "" {
		end := index + len(token)
		return rewriteLegacyNixPathToken(token), end, true
	}
	if token := nixHomePathPattern.FindString(remaining); token != "" {
		return token, index + len(token), true
	}
	if token := nixIdentifierPattern.FindString(remaining); token != "" {
		return token, index + len(token), true
	}
	return "", index, false
}

func rewriteLegacyNixPathToken(token string) string {
	return migrationv1tov2.RewritePrivateUserPathToken(token)
}

func rewriteNixInterpolatedPath(text string, index int) (string, int, bool) {
	path := nixFilesystemPathToken(text[index:])
	if path == "" {
		return "", index, false
	}
	var out strings.Builder
	out.WriteString(rewriteLegacyNixPathToken(path))
	cursor := index + len(path)
	if literal := nixInPathLiteral.FindString(text[cursor:]); literal != "" {
		out.WriteString(literal)
		cursor += len(literal)
	}
	if !strings.HasPrefix(text[cursor:], "${") {
		return "", index, false
	}
	for cursor < len(text) {
		if strings.HasPrefix(text[cursor:], "${") {
			out.WriteString("${")
			rewritten, end := rewriteNixCode(text, cursor+2, true)
			out.WriteString(rewritten)
			cursor = end
			continue
		}
		literal := nixInPathLiteral.FindString(text[cursor:])
		if literal == "" {
			break
		}
		out.WriteString(literal)
		cursor += len(literal)
	}
	return out.String(), cursor, true
}

func nixFilesystemPathToken(text string) string {
	if token := nixPathTokenPattern.FindString(text); token != "" {
		return token
	}
	return nixHomePathPattern.FindString(text)
}

func copyExistingThinFlake(dest, target string, state config.State) (bool, error) {
	source := filepath.Join(dest, "flake.nix")
	data, err := os.ReadFile(source)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	text := string(data)
	previous, err := readFlakeState(dest)
	if err != nil {
		return false, err
	}
	if isGeneratedMySetupWrapperFlake(text) || len(previous) != 0 {
		plan, err := PlanFlake(text, state, previous)
		if err != nil {
			return false, fmt.Errorf("update Wahrwelt wrapper: %w", err)
		}
		info, err := os.Stat(source)
		if err != nil {
			return false, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(target, []byte(plan.Content), info.Mode().Perm()); err != nil {
			return false, err
		}
		return true, os.WriteFile(filepath.Join(filepath.Dir(target), FlakeStateName), plan.State, 0o644)
	}
	if !isThinWrapperFlake(text) {
		return false, fmt.Errorf("existing flake.nix is not a recognized Wahrwelt wrapper; refusing to replace user configuration")
	}
	// The template's baseline cannot claim fields in a user-owned wrapper.
	if err := os.Remove(filepath.Join(filepath.Dir(target), FlakeStateName)); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, copyFile(source, target)
}

func isThinWrapperFlake(text string) bool {
	return strings.Contains(text, "wahrwelt.lib.mkWahrweltHost") ||
		strings.Contains(text, "mysetup.lib.mkMySetupHost") ||
		containsWahrweltNixOSFlakeURL(text)
}

func isGeneratedMySetupWrapperFlake(text string) bool {
	for _, marker := range []string{
		"# Generated by Wahrwelt installer.",
		"# Generated by MySetup installer.",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if !containsWahrweltNixOSFlakeURL(text) {
		return false
	}
	hostMarkerFound := false
	for _, marker := range []string{
		`nixosConfigurations.${hostname} = wahrwelt.lib.mkWahrweltHost {`,
		`nixosConfigurations.${hostname} = mysetup.lib.mkMySetupHost {`,
	} {
		if strings.Contains(text, marker) {
			hostMarkerFound = true
			break
		}
	}
	if !hostMarkerFound {
		return false
	}
	descriptionFound := strings.Contains(text, `description = "Host-local Wahrwelt NixOS wrapper";`) ||
		strings.Contains(text, `description = "Host-local MySetup NixOS wrapper";`)
	if !descriptionFound {
		return false
	}
	for _, marker := range []string{
		`hostVars = ./host-vars.nix;`,
		`hardware = ./hardware-configuration.nix;`,
		`extraModules = [ ./configuration.nix ];`,
		`if builtins.pathExists ./home.nix then [ ./home.nix ] else [ ];`,
	} {
		if !strings.Contains(text, marker) {
			return false
		}
	}
	return true
}

func containsWahrweltNixOSFlakeURL(text string) bool {
	for _, flakeURL := range config.KnownWahrweltFlakeURLs() {
		if strings.Contains(text, flakeURL) {
			return true
		}
	}
	return false
}

func copyHostLocalTree(ctx context.Context, runner run.CommandRunner, source, target string) error {
	if err := copyTree(ctx, runner, source, target); err != nil {
		if sudoErr := sudoCopyHostLocalTree(ctx, runner, source, target); sudoErr != nil {
			return err
		}
	}
	return nil
}

func sudoCopyHostLocalTree(ctx context.Context, runner run.CommandRunner, source, target string) error {
	uid := strconv.Itoa(os.Getuid())
	gid := strconv.Itoa(os.Getgid())
	if err := runner.Command(ctx, "sudo", "mkdir", "-p", target); err != nil {
		return err
	}
	return runner.Command(
		ctx,
		"sudo",
		"rsync",
		"-a",
		"--delete",
		"--checksum",
		"--chown",
		uid+":"+gid,
		source+"/",
		target+"/",
	)
}

func existingHashedPasswordPaths(dest string) []string {
	return []string{
		filepath.Join(dest, "hashed-password.nix"),
		filepath.Join(dest, "hosts", "NixOS", "hashed-password.nix"),
	}
}

func existingSecretsDirs(dest string) []string {
	return []string{
		filepath.Join(dest, "secrets"),
		filepath.Join(dest, "hosts", "NixOS", "secrets"),
	}
}

func hashPassword(ctx context.Context, password string) (string, error) {
	rounds := fmt.Sprintf("--rounds=%d", defaults.ShaCryptRounds)
	cmd := exec.CommandContext(ctx, "mkpasswd", "-sm", "sha-512", rounds)
	cmd.Stdin = strings.NewReader(password)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mkpasswd failed: %w", err)
	}
	hash := strings.TrimSpace(string(out))
	if hash == "" {
		return "", fmt.Errorf("mkpasswd produced empty hash")
	}
	return hash, nil
}

var (
	generatedPasswordDetailsPattern = regexp.MustCompile(`(?s)^\{ config, \.\.\. \}:\n\n\{\n  users\.users\.\$\{config\.(wahrwelt|mysetup)\.user\.username\}\.initialHashedPassword = "([^"\r\n]+)";\n\}\n$`)
	sha512CryptPattern              = regexp.MustCompile(`^\$6\$(?:rounds=[0-9]+\$)?[./A-Za-z0-9]{1,16}\$[./A-Za-z0-9]{86}$`)
)

func validateRawPasswordHash(data []byte) error {
	if !sha512CryptPattern.Match(bytes.TrimSpace(data)) {
		return fmt.Errorf("unrecognized raw password hash")
	}
	return nil
}

type legacyPasswordHashCandidate struct {
	path      string
	hash      string
	namespace string
	identity  fsowner.Identity
	pinned    *pinnedRegularFile
}

//nolint:gocyclo // Candidate pinning and hash agreement checks stay linear to preserve fail-closed migration semantics.
func inspectLegacyGeneratedPasswordHashes(dest string) ([]legacyPasswordHashCandidate, error) {
	requiredUID := currentEffectiveUID()
	if filepath.Clean(dest) == "/etc/nixos" {
		requiredUID = 0
	}
	candidates := make([]legacyPasswordHashCandidate, 0, len(existingHashedPasswordPaths(dest)))
	for _, source := range existingHashedPasswordPaths(dest) {
		directory, err := fsowner.OpenDirectory(filepath.Dir(source))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOENT) {
				continue
			}
			closeLegacyPasswordHashCandidates(candidates)
			return nil, err
		}
		entry, err := directory.Inspect(filepath.Base(source))
		_ = directory.Close()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOENT) {
				continue
			}
			closeLegacyPasswordHashCandidates(candidates)
			return nil, err
		}
		if entry.Identity.Kind != fsowner.KindRegular || entry.Identity.UID != requiredUID || entry.Identity.Links != 1 {
			closeLegacyPasswordHashCandidates(candidates)
			return nil, fmt.Errorf("legacy password hash ownership collision at %s: unsupported identity", source)
		}
		pinned, err := openPinnedRegularFile(source, "legacy generated password hash")
		if err != nil {
			closeLegacyPasswordHashCandidates(candidates)
			return nil, err
		}
		actual, identityErr := fsownerIdentityFromPinned(pinned)
		if identityErr != nil {
			closeFile(pinned.file)
			closeLegacyPasswordHashCandidates(candidates)
			return nil, identityErr
		}
		if actual != entry.Identity {
			closeFile(pinned.file)
			closeLegacyPasswordHashCandidates(candidates)
			return nil, fmt.Errorf("legacy password hash ownership collision at %s: identity changed while pinning", source)
		}
		data, readErr := readPinnedRegularFile(pinned)
		if readErr != nil {
			closeFile(pinned.file)
			closeLegacyPasswordHashCandidates(candidates)
			return nil, readErr
		}
		matches := generatedPasswordDetailsPattern.FindSubmatch(data)
		if len(matches) != 3 || !sha512CryptPattern.Match(matches[2]) {
			closeFile(pinned.file)
			closeLegacyPasswordHashCandidates(candidates)
			parseErr := fmt.Errorf("unrecognized generated password hash module")
			return nil, fmt.Errorf("legacy password hash ownership collision at %s: %w", source, parseErr)
		}
		namespace, hash := string(matches[1]), string(matches[2])
		if len(candidates) != 0 && candidates[0].hash != hash {
			closeFile(pinned.file)
			closeLegacyPasswordHashCandidates(candidates)
			return nil, fmt.Errorf("legacy password hash ownership collision: generated modules disagree")
		}
		candidates = append(candidates, legacyPasswordHashCandidate{
			path: source, hash: hash, namespace: namespace, identity: entry.Identity, pinned: pinned,
		})
	}
	return candidates, nil
}

func closeLegacyPasswordHashCandidates(candidates []legacyPasswordHashCandidate) {
	for index := range candidates {
		if candidates[index].pinned != nil {
			closeFile(candidates[index].pinned.file)
			candidates[index].pinned = nil
		}
	}
}

func fsownerIdentityFromPinned(pinned *pinnedRegularFile) (fsowner.Identity, error) {
	if pinned == nil || pinned.file == nil {
		return fsowner.Identity{}, fmt.Errorf("missing pinned regular file")
	}
	var stat unix.Stat_t
	descriptor, err := checkedFileDescriptor(pinned.file, "pinned regular file")
	if err != nil {
		return fsowner.Identity{}, err
	}
	if err := unix.Fstat(descriptor, &stat); err != nil {
		return fsowner.Identity{}, err
	}
	kind := fsowner.KindOther
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		kind = fsowner.KindRegular
	case unix.S_IFDIR:
		kind = fsowner.KindDirectory
	case unix.S_IFLNK:
		kind = fsowner.KindSymlink
	}
	return fsowner.Identity{
		Device: stat.Dev,
		Inode:  stat.Ino,
		Mode:   stat.Mode,
		Links:  stat.Nlink,
		UID:    stat.Uid,
		Kind:   kind,
	}, nil
}

//nolint:gocyclo // Publication preflight and descriptor handoff stay linear so every early return fails closed.
func writeStagedUserDefault(ctx context.Context, runner run.CommandRunner, staging, dest string, layout Layout) error {
	if layout != LayoutThin {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source, sourceExists, err := stagedUserDefaultSource(staging)
	if err != nil {
		return err
	}
	if !sourceExists {
		return nil
	}
	target := filepath.Join(dest, "user", "default.nix")
	if runner.IsDryRun() {
		needsPublication, err := userDefaultNeedsPublication(target)
		if err != nil {
			return err
		}
		if !needsPublication {
			return nil
		}
		fmt.Println("write user/default.nix")
		return nil
	}
	sourcePinned, err := openPinnedRegularFile(source, "staged user default")
	if err != nil {
		return err
	}
	defer closeFile(sourcePinned.file)
	sourceData, err := readPinnedRegularFile(sourcePinned)
	if err != nil {
		return fmt.Errorf("read staged user/default.nix: %w", err)
	}
	publicationSource, cleanupPublicationSource, err := createSealedPinnedSource("wahrwelt-user-default", sourceData)
	if err != nil {
		return fmt.Errorf("snapshot staged user/default.nix: %w", err)
	}
	defer cleanupPublicationSource()
	root, err := openPinnedParentDirectory(filepath.Join(dest, "user"), "user default root")
	if err != nil {
		return err
	}
	defer closeFile(root)
	userDir, exists, err := openPinnedDirectoryEntryAt(root, "user", filepath.Join(dest, "user"), "user")
	if err != nil {
		return err
	}
	if !exists {
		return publishStagedUserDefaultWithDirectoryCreate(ctx, runner, publicationSource, root, dest, target)
	}
	defer closeFile(userDir.file)
	if err := verifyPinnedUserDestination(root, userDir, dest); err != nil {
		return err
	}
	targetExists, err := preservableUserDefaultAt(userDir.file, filepath.Base(target), target)
	if err != nil {
		return err
	}
	if targetExists {
		return verifyPinnedUserDestination(root, userDir, dest)
	}
	return publishStagedUserDefault(ctx, runner, publicationSource, root, userDir, target)
}

func stagedUserDefaultSource(staging string) (string, bool, error) {
	source := filepath.Join(staging, "user", "default.nix")
	exists, err := preservableUserDefault(source)
	if err != nil || !exists {
		return source, exists, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return source, false, err
	}
	return source, info.Mode()&os.ModeSymlink == 0, nil
}

func userDefaultNeedsPublication(target string) (bool, error) {
	if err := ordinaryPathParent(target, "user default"); err != nil {
		return false, err
	}
	exists, err := preservableUserDefault(target)
	return !exists, err
}

const privilegedCreateUserDefaultScript = `
set -eu

root_fd=$1
source_fd=$2
root_display=$3
python_bin=$4

root_id=$(stat -Lc '%d:%i' -- "$root_fd")
visible_root_matches() {
	[ ! -L "$root_display" ] && [ -d "$root_display" ] &&
		[ "$(stat -c '%d:%i' -- "$root_display" 2>/dev/null || true)" = "$root_id" ]
}
user_absent() {
	[ ! -e "$root_fd/user" ] && [ ! -L "$root_fd/user" ]
}
if ! visible_root_matches || ! user_absent; then
	printf '%s\n' "user directory appeared or destination root changed before publication: $root_display/user" >&2
	exit 17
fi

` + privilegedPinnedDirectoryCreatorScript + `

umask 077
create_pinned_directory "$root_fd" ".wahrwelt-user-default-recovery-" "$python_bin"
candidate_name=$created_name
candidate_id=$created_id
candidate_fd=/proc/self/fd/8
cd -- "$candidate_fd"
install -m 0644 -- "$source_fd" default.nix
[ ! -L default.nix ] && [ -f default.nix ]
cmp -s -- "$source_fd" default.nix
chmod 0755 -- .

candidate_matches() {
	[ ! -L "$root_fd/$candidate_name" ] && [ -d "$root_fd/$candidate_name" ] &&
		[ "$(stat -c '%d:%i' -- "$root_fd/$candidate_name" 2>/dev/null || true)" = "$candidate_id" ] &&
		[ "$(stat -Lc '%d:%i' -- "$candidate_fd")" = "$candidate_id" ]
}
restore_created_user() {
	user_id=$(stat -c '%d:%i' -- "$root_fd/user" 2>/dev/null || true)
	if [ "$user_id" = "$candidate_id" ] &&
		mv -T --no-copy --update=none-fail -- "$root_fd/user" "$root_fd/$candidate_name"; then
		printf '%s\n' "created user directory retained for recovery at $root_display/$candidate_name" >&2
		exit 1
	fi
	if [ -n "$user_id" ] && [ ! -e "$root_fd/$candidate_name" ] && [ ! -L "$root_fd/$candidate_name" ] &&
		mv -T --no-copy --update=none-fail -- "$root_fd/user" "$root_fd/$candidate_name"; then
		restored_id=$(stat -c '%d:%i' -- "$root_fd/$candidate_name" 2>/dev/null || true)
		if [ "$restored_id" = "$user_id" ]; then
			printf '%s\n' "unexpected user directory restored at $root_display/$candidate_name; expected recovery retained through pinned descriptor $candidate_fd" >&2
			exit 1
		fi
	fi
	printf '%s\n' "created user directory publication has an uncertain recovery under pinned $root_display" >&2
	exit 1
}

if ! visible_root_matches || ! user_absent || ! candidate_matches; then
	printf '%s\n' "user directory publication precondition changed; recovery retained at $root_display/$candidate_name" >&2
	exit 1
fi
if ! mv -T --no-copy --update=none-fail -- "$root_fd/$candidate_name" "$root_fd/user"; then
	printf '%s\n' "user directory collision; recovery retained at $root_display/$candidate_name" >&2
	exit 17
fi
moved_id=$(stat -c '%d:%i' -- "$root_fd/user" 2>/dev/null || true)
if [ "$moved_id" != "$candidate_id" ]; then
	restore_created_user
fi
if ! visible_root_matches; then
	restore_created_user
fi
printf '%s\n' "$candidate_id"
`

//nolint:gocyclo // Post-publication identity and content checks form one fail-closed transaction.
func publishStagedUserDefaultWithDirectoryCreate(ctx context.Context, runner run.CommandRunner, source *pinnedRegularFile, root *os.File, dest, target string) error {
	if source == nil || source.file == nil || root == nil {
		return fmt.Errorf("missing pinned user/default.nix creation handles")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	publishCtx, cancel := cleanupContext(ctx)
	defer cancel()
	pythonPath, err := privilegedPythonPath()
	if err != nil {
		return err
	}
	token, err := runner.Output(
		publishCtx,
		"sudo", "sh", "-c", privilegedCreateUserDefaultScript, "--",
		fileDescriptorPath(root), fileDescriptorPath(source.file), dest, pythonPath,
	)
	runtime.KeepAlive(root)
	runtime.KeepAlive(source.file)
	if err != nil {
		return fmt.Errorf("create user directory with default.nix: %w", err)
	}
	userDir, exists, err := openPinnedDirectoryEntryAt(root, "user", filepath.Join(dest, "user"), "user")
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("created user directory is absent beneath pinned destination: %s", filepath.Join(dest, "user"))
	}
	defer closeFile(userDir.file)
	userFD, err := checkedFileDescriptor(userDir.file, "created user directory")
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(userFD, &stat); err != nil {
		return fmt.Errorf("stat created user directory: %w", err)
	}
	if got := fmt.Sprintf("%d:%d", stat.Dev, stat.Ino); token != got {
		return fmt.Errorf("created user directory changed before pin: %s", filepath.Join(dest, "user"))
	}
	if err := verifyPinnedUserDestination(root, userDir, dest); err != nil {
		return err
	}
	published, exists, err := openPinnedRegularFileAt(userDir.file, filepath.Base(target), target, "user default")
	if err != nil {
		return fmt.Errorf("verify created user/default.nix: %w", err)
	}
	if !exists {
		return fmt.Errorf("created user/default.nix is absent: %s", target)
	}
	defer closeFile(published.file)
	sameContent, err := samePinnedContent(source, published)
	if err != nil {
		return fmt.Errorf("read created user/default.nix: %w", err)
	}
	if !sameContent {
		return fmt.Errorf("created user/default.nix content changed during publication: %s", target)
	}
	return verifyPinnedUserDestination(root, userDir, dest)
}

func preservableUserDefaultAt(parent *os.File, name, display string) (bool, error) {
	parentFD, err := checkedFileDescriptor(parent, "user default parent")
	if err != nil {
		return false, err
	}
	var info unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return false, nil
		}
		return false, &os.PathError{Op: "lstat", Path: display, Err: err}
	}
	switch info.Mode & unix.S_IFMT {
	case unix.S_IFREG, unix.S_IFLNK:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported user/default.nix path: %s", display)
	}
}

func verifyPinnedUserDestination(root *os.File, userDir *pinnedDirectoryEntry, dest string) error {
	display := filepath.Join(dest, "user")
	if err := verifyPinnedDirectoryEntry(root, "user", display, "user", userDir); err != nil {
		return err
	}
	return verifyPinnedDirectoryVisible(root, dest, "user default root")
}

func publishStagedUserDefault(ctx context.Context, runner run.CommandRunner, sourcePinned *pinnedRegularFile, root *os.File, userDir *pinnedDirectoryEntry, target string) error {
	if err := publishPinnedWithCleanupContext(ctx, runner, "user-default", userDir.file, target, sourcePinned, nil); err != nil {
		if resolveErr := resolveUserDefaultPublishErrorAt(userDir.file, filepath.Base(target), target, err); resolveErr != nil {
			return resolveErr
		}
		return verifyPinnedUserDestination(root, userDir, filepath.Dir(filepath.Dir(target)))
	}
	published, exists, err := openPinnedRegularFileAt(userDir.file, filepath.Base(target), target, "user default")
	if err != nil {
		return fmt.Errorf("verify published user/default.nix: %w", err)
	}
	if !exists {
		return fmt.Errorf("verify published user/default.nix: target is absent: %s", target)
	}
	defer closeFile(published.file)
	sameContent, err := samePinnedContent(sourcePinned, published)
	if err != nil {
		return fmt.Errorf("read published user/default.nix: %w", err)
	}
	if !sameContent {
		return fmt.Errorf("published user/default.nix content changed during publication: %s", target)
	}
	return verifyPinnedUserDestination(root, userDir, filepath.Dir(filepath.Dir(target)))
}

func resolveUserDefaultPublishErrorAt(parent *os.File, name, display string, publishErr error) error {
	if !isUserDefaultPublishCollision(publishErr) {
		return fmt.Errorf("publish user/default.nix: %w", publishErr)
	}
	targetExists, err := preservableUserDefaultAt(parent, name, display)
	if err != nil {
		return err
	}
	if targetExists {
		return nil
	}
	return fmt.Errorf("user/default.nix collision reported without a target: %w", publishErr)
}

func isUserDefaultPublishCollision(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 17
}

func openPinnedDirectoryPath(path, label string) (*pinnedDirectoryEntry, error) {
	parent, err := openPinnedParentDirectory(path, label)
	if err != nil {
		return nil, err
	}
	defer closeFile(parent)
	directory, exists, err := openPinnedDirectoryEntryAt(parent, filepath.Base(path), path, label)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &os.PathError{Op: "open", Path: path, Err: unix.ENOENT}
	}
	return directory, nil
}

func verifyPublishedRegularFileAt(parent *os.File, name, display, label string, expectedTarget *pinnedRegularFile, expectedData []byte) error {
	published, exists, err := openPinnedRegularFileAt(parent, name, display, label)
	if err != nil {
		return fmt.Errorf("verify published %s: %w", label, err)
	}
	if !exists {
		return fmt.Errorf("verify published %s: target is absent: %s", label, display)
	}
	defer closeFile(published.file)
	if expectedTarget != nil && sameRegularFile(published.info, expectedTarget.info) {
		return fmt.Errorf("published %s target was not replaced: %s", label, display)
	}
	if filepath.Clean(display) == paths.DefaultPasswordHashPath {
		identity, identityErr := fsownerIdentityFromPinned(published)
		if identityErr != nil {
			return fmt.Errorf("identify published %s: %w", label, identityErr)
		}
		if identity.Kind != fsowner.KindRegular || identity.UID != 0 || identity.Links != 1 || published.info.Mode().Perm() != 0o600 || published.info.Size() != int64(len(expectedData)) {
			return fmt.Errorf("published %s metadata is not root-private: %s", label, display)
		}
		return nil
	}
	actualData, err := readPinnedRegularFile(published)
	if err != nil {
		return fmt.Errorf("read published %s: %w", label, err)
	}
	if !bytes.Equal(actualData, expectedData) {
		return fmt.Errorf("published %s content changed during publication: %s", label, display)
	}
	if published.info.Mode().Perm() != 0o600 {
		return fmt.Errorf("published %s mode = %04o, want 0600: %s", label, published.info.Mode().Perm(), display)
	}
	return nil
}

//nolint:gocyclo // External secret publication and migration checks intentionally stay linear and fail closed.
func writeStagedHashedPassword(ctx context.Context, runner run.CommandRunner, staging, dest string, secrets config.Secrets, _ Layout) error {
	marker := filepath.Join(staging, paths.PasswordHashMarkerName)
	markerInfo, err := os.Lstat(marker)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !markerInfo.Mode().IsRegular() || markerInfo.Size() != 0 {
		return fmt.Errorf("password hash marker is not an empty regular file: %s", marker)
	}
	target := passwordHashTarget(dest)
	if runner.IsDryRun() {
		if filepath.Clean(target) == paths.DefaultPasswordHashPath {
			fmt.Printf("validate external password hash %s via privileged helper\n", target)
			if secrets.UserPassword != "" {
				fmt.Printf("write external password hash %s via sealed descriptor\n", target)
			}
			return nil
		}
		targetInfo, err := regularFileOrAbsent(target, "external password hash")
		if err != nil {
			return fmt.Errorf("external password hash target is not a regular file: %s", target)
		}
		if secrets.UserPassword == "" && targetInfo != nil {
			return nil
		}
		fmt.Printf("write external password hash %s\n", target)
		return nil
	}
	if filepath.Clean(target) == paths.DefaultPasswordHashPath {
		return writePrivilegedExternalPasswordHash(ctx, runner, target, secrets)
	}
	legacyCandidates, err := inspectLegacyGeneratedPasswordHashes(dest)
	if err != nil {
		return err
	}
	defer closeLegacyPasswordHashCandidates(legacyCandidates)
	existingHash, externalExists, err := existingExternalPasswordHash(target)
	if err != nil {
		return err
	}
	if externalExists && filepath.Clean(target) == paths.DefaultPasswordHashPath {
		helper, helperErr := privilegedFSHelperPath()
		if helperErr != nil {
			return helperErr
		}
		if err := validateExternalPasswordHashWithHelper(ctx, runner, target, helper); err != nil {
			return fmt.Errorf("validate external password hash: %w", err)
		}
	}
	if secrets.UserPassword == "" {
		if externalExists {
			if existingHash != "" {
				if err := requireLegacyHashesMatchExternal(legacyCandidates, existingHash); err != nil {
					return err
				}
			}
			if err := sanitizeLegacyGeneratedPasswordModulesLocally(target, legacyCandidates); err != nil {
				return err
			}
			return nil
		}
		if len(legacyCandidates) == 0 {
			return fmt.Errorf("password hash marker exists but no owned password hash source is available")
		}
		if err := publishExternalPasswordHash(ctx, runner, target, legacyCandidates[0].hash); err != nil {
			return err
		}
		if err := sanitizeLegacyGeneratedPasswordModulesLocally(target, legacyCandidates); err != nil {
			return err
		}
		logMigratedPasswordModules(legacyCandidates, target)
		return nil
	}

	replacementHash, err := hashPassword(ctx, secrets.UserPassword)
	if err != nil {
		return err
	}
	if len(legacyCandidates) != 0 {
		if externalExists {
			if existingHash != "" {
				if err := requireLegacyHashesMatchExternal(legacyCandidates, existingHash); err != nil {
					return err
				}
			}
		} else if err := publishExternalPasswordHash(ctx, runner, target, legacyCandidates[0].hash); err != nil {
			return err
		}
		if err := sanitizeLegacyGeneratedPasswordModulesLocally(target, legacyCandidates); err != nil {
			return err
		}
		logMigratedPasswordModules(legacyCandidates, target)
	}
	return publishExternalPasswordHash(ctx, runner, target, replacementHash)
}

var passwordHashStatusPattern = regexp.MustCompile(`^[0-9]+:[1-9][0-9]*:[0-9]+:1:0:regular$`)

func privilegedPasswordHashStatus(ctx context.Context, runner run.CommandRunner, helper, target string) (string, error) {
	status, err := runner.Output(ctx, "sudo", helper, "password-hash-status", "--path", target)
	if err != nil {
		return "", fmt.Errorf("inspect external password hash: %w", err)
	}
	if status != "absent" && !passwordHashStatusPattern.MatchString(status) {
		return "", fmt.Errorf("privileged helper returned an invalid password hash status")
	}
	return status, nil
}

func publishPrivilegedExternalPasswordHash(
	ctx context.Context,
	runner run.CommandRunner,
	helper, target, rawHash, expected string,
) error {
	source, cleanup, err := createSealedPinnedSource("wahrwelt-password-hash", []byte(rawHash+"\n"))
	if err != nil {
		return fmt.Errorf("create sealed external password hash source: %w", err)
	}
	defer cleanup()
	err = runner.Command(
		ctx,
		"sudo",
		helper,
		"publish-password-hash",
		"--path", target,
		"--source", fileDescriptorPath(source.file),
		"--expected", expected,
	)
	runtime.KeepAlive(source.file)
	if err != nil {
		return fmt.Errorf("publish external password hash: %w", err)
	}
	return nil
}

func writePrivilegedExternalPasswordHash(
	ctx context.Context,
	runner run.CommandRunner,
	target string,
	secrets config.Secrets,
) error {
	helper, err := privilegedFSHelperPath()
	if err != nil {
		return err
	}
	migrationStatus, err := runner.Output(ctx, "sudo", helper, "migrate-generated-password-modules")
	if err != nil {
		return fmt.Errorf("migrate generated password modules: %w", err)
	}
	if migrationStatus != "absent" && migrationStatus != "migrated" {
		return fmt.Errorf("privileged helper returned an invalid password module migration status")
	}
	status, err := privilegedPasswordHashStatus(ctx, runner, helper, target)
	if err != nil {
		return err
	}
	replacementHash := ""
	if secrets.UserPassword != "" {
		replacementHash, err = hashPassword(ctx, secrets.UserPassword)
		if err != nil {
			return err
		}
	}
	if replacementHash == "" {
		if status == "absent" {
			return fmt.Errorf("password hash marker exists but no owned password hash source is available")
		}
		return nil
	}
	return publishPrivilegedExternalPasswordHash(ctx, runner, helper, target, replacementHash, status)
}

func existingExternalPasswordHash(target string) (string, bool, error) {
	if filepath.Clean(target) == paths.DefaultPasswordHashPath {
		return existingExternalPasswordHashForOwner(target, true, 0)
	}
	return existingExternalPasswordHashForOwner(target, false, currentEffectiveUID())
}

func existingExternalPasswordHashForOwner(target string, metadataOnly bool, requiredUID uint32) (string, bool, error) {
	existing, exists, err := openPinnedRegularFileAtPath(target, "external password hash")
	if err != nil || !exists {
		return "", exists, err
	}
	defer closeFile(existing.file)
	identity, err := fsownerIdentityFromPinned(existing)
	if err != nil {
		return "", false, err
	}
	if identity.Kind != fsowner.KindRegular || identity.UID != requiredUID || identity.Links != 1 || existing.info.Mode().Perm() != 0o600 {
		return "", false, fmt.Errorf("external password hash metadata is not private at %s", target)
	}
	if metadataOnly {
		if existing.info.Size() < 91 || existing.info.Size() > 256 {
			return "", false, fmt.Errorf("external password hash size is outside the allowed range at %s", target)
		}
		return "", true, nil
	}
	data, err := readPinnedRegularFile(existing)
	if err != nil {
		return "", false, err
	}
	if err := validateRawPasswordHash(data); err != nil {
		return "", false, fmt.Errorf("external password hash ownership collision at %s: %w", target, err)
	}
	return string(bytes.TrimSpace(data)), true, nil
}

func validateExternalPasswordHashWithHelper(ctx context.Context, runner run.CommandRunner, target, helper string) error {
	if filepath.Clean(target) != paths.DefaultPasswordHashPath {
		return nil
	}
	if helper == "" {
		return fmt.Errorf("missing privileged filesystem helper")
	}
	return runner.Command(ctx, "sudo", helper, "validate-password-hash", "--path", paths.DefaultPasswordHashPath)
}

func requireLegacyHashesMatchExternal(candidates []legacyPasswordHashCandidate, externalHash string) error {
	for _, candidate := range candidates {
		if candidate.hash != externalHash {
			return fmt.Errorf("legacy password hash ownership collision: external hash and generated module disagree")
		}
	}
	return nil
}

func logMigratedPasswordModules(candidates []legacyPasswordHashCandidate, target string) {
	for _, candidate := range candidates {
		fmt.Printf("migrated generated password hash from %s to %s\n", candidate.path, target)
	}
}

func publishExternalPasswordHash(ctx context.Context, runner run.CommandRunner, target, rawHash string) error {
	expectedData := []byte(rawHash + "\n")
	publicationSource, cleanupPublicationSource, err := createSealedPinnedSource("wahrwelt-password-hash", expectedData)
	if err != nil {
		return fmt.Errorf("snapshot external password hash: %w", err)
	}
	defer cleanupPublicationSource()
	if err := ensureExternalPasswordHashParent(ctx, runner, filepath.Dir(target)); err != nil {
		return err
	}
	parent, err := openPinnedParentDirectory(target, "external password hash")
	if err != nil {
		return err
	}
	defer closeFile(parent)
	expectedTarget, _, err := openPinnedRegularFileAt(parent, filepath.Base(target), target, "external password hash")
	if err != nil {
		return fmt.Errorf("external password hash target is not a regular file: %w", err)
	}
	if expectedTarget != nil {
		defer closeFile(expectedTarget.file)
	}
	if err := publishPinnedWithCleanupContext(ctx, runner, "password-hash", parent, target, publicationSource, expectedTarget); err != nil {
		return err
	}
	if err := verifyPublishedRegularFileAt(parent, filepath.Base(target), target, "external password hash", expectedTarget, expectedData); err != nil {
		return err
	}
	if err := verifyPinnedDirectoryVisible(parent, filepath.Dir(target), "external password hash"); err != nil {
		return err
	}
	return nil
}

func sanitizeLegacyGeneratedPasswordModulesLocally(target string, candidates []legacyPasswordHashCandidate) error {
	for _, candidate := range candidates {
		if err := sanitizeLegacyGeneratedPasswordModuleLocally(target, candidate); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocyclo // Scrub, stub publication, and post-verification are one crash-aware migration transaction.
func sanitizeLegacyGeneratedPasswordModuleLocally(target string, candidate legacyPasswordHashCandidate) error {
	pinned, err := openPinnedRegularFile(target, "external password hash")
	if err != nil {
		return err
	}
	defer closeFile(pinned.file)
	raw, err := readPinnedRegularFile(pinned)
	if err != nil {
		return err
	}
	if pinned.info.Mode().Perm() != 0o600 || string(bytes.TrimSpace(raw)) != candidate.hash {
		return fmt.Errorf("external password hash does not match generated module")
	}
	if candidate.pinned == nil || candidate.pinned.file == nil {
		return fmt.Errorf("missing pinned legacy generated password module")
	}
	fd, err := unix.Open(fileDescriptorPath(candidate.pinned.file), unix.O_RDWR|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file, wrapErr := fileFromUnixDescriptor(fd, candidate.path)
	if wrapErr != nil {
		_ = unix.Close(fd)
		return wrapErr
	}
	defer closeFile(file)
	actual, err := fsownerIdentityFromPinned(&pinnedRegularFile{file: file})
	if err != nil {
		return err
	}
	if actual != candidate.identity {
		return fmt.Errorf("legacy password hash ownership collision at %s: pinned identity changed", candidate.path)
	}
	module, err := io.ReadAll(io.NewSectionReader(file, 0, maxPasswordModuleBytes+1))
	if err != nil {
		return err
	}
	if len(module) > maxPasswordModuleBytes {
		return fmt.Errorf("legacy password hash module exceeds allowed size")
	}
	matches := generatedPasswordDetailsPattern.FindSubmatch(module)
	if len(matches) != 3 || string(matches[1]) != candidate.namespace || string(matches[2]) != candidate.hash {
		return fmt.Errorf("legacy password hash ownership collision at %s: pinned content changed", candidate.path)
	}
	hashOffset := bytes.Index(module, matches[2])
	if hashOffset < 0 || bytes.Count(module, matches[2]) != 1 {
		return fmt.Errorf("legacy password hash ownership collision at %s: hash span is ambiguous", candidate.path)
	}
	if err := writePinnedPasswordModuleAt(file, bytes.Repeat([]byte{'x'}, len(matches[2])), int64(hashOffset)); err != nil {
		return fmt.Errorf("scrub legacy password hash: %w", err)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	stub := []byte(functionalLegacyPasswordModule(candidate.namespace))
	if err := writePinnedPasswordModuleAt(file, stub, 0); err != nil {
		return err
	}
	if err := file.Truncate(int64(len(stub))); err != nil {
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	visible, visibleErr := os.Lstat(candidate.path)
	if visibleErr != nil || !sameRegularFile(visible, candidate.pinned.info) {
		return fmt.Errorf("ownership collision: legacy password module public name changed after its pinned inode was sanitized: %s", candidate.path)
	}
	post, err := os.ReadFile(fileDescriptorPath(file))
	if err != nil {
		return err
	}
	if !bytes.Equal(post, stub) {
		return fmt.Errorf("functional legacy password module failed exact verification")
	}
	revalidatedTarget, exists, err := openPinnedRegularFileAtPath(target, "external password hash")
	if err != nil {
		return err
	}
	if revalidatedTarget != nil {
		defer closeFile(revalidatedTarget.file)
	}
	if !exists || !sameRegularFile(revalidatedTarget.info, pinned.info) {
		return fmt.Errorf("ownership collision: external password hash changed during module migration")
	}
	return nil
}

func functionalLegacyPasswordModule(namespace string) string {
	return fmt.Sprintf(`{ config, ... }:

{
  users.users.${config.%s.user.username}.hashedPasswordFile = "/etc/wahrwelt/hashed-password";
}
`, namespace)
}

func writePinnedPasswordModuleAt(file *os.File, payload []byte, offset int64) error {
	for len(payload) != 0 {
		written, err := file.WriteAt(payload, offset)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		offset += int64(written)
		payload = payload[written:]
	}
	return nil
}

func ensureExternalPasswordHashParent(ctx context.Context, runner run.CommandRunner, parent string) error {
	info, err := os.Lstat(parent)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("external password hash parent is not an ordinary directory: %s", parent)
		}
		if filepath.Clean(parent) == filepath.Dir(paths.DefaultPasswordHashPath) {
			return validateExternalPasswordHashParentMetadata(parent, 0)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := runner.Command(ctx, "sudo", "install", "-d", "-m", "700", "-o", "root", "-g", "root", parent); err != nil {
		return fmt.Errorf("prepare external password hash directory: %w", err)
	}
	if filepath.Clean(parent) == filepath.Dir(paths.DefaultPasswordHashPath) && !runner.IsDryRun() {
		return validateExternalPasswordHashParentMetadata(parent, 0)
	}
	return nil
}

func validateExternalPasswordHashParentMetadata(parent string, requiredUID uint32) error {
	var stat unix.Stat_t
	if err := unix.Lstat(parent, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != requiredUID || stat.Mode&0o777 != 0o700 {
		return fmt.Errorf("external password hash parent is not an owned private directory: %s", parent)
	}
	return nil
}

func openPinnedRegularFileAtPath(path, label string) (*pinnedRegularFile, bool, error) {
	parent, err := openPinnedParentDirectory(path, label)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer closeFile(parent)
	return openPinnedRegularFileAt(parent, filepath.Base(path), path, label)
}

const privilegedPublishSecretsScript = `
set -eu

parent_fd=$1
source_fd=$2
parent_display=$3
python_bin=$4
target_name=secrets

parent_id=$(stat -Lc '%d:%i' -- "$parent_fd")
source_id=$(stat -Lc '%d:%i' -- "$source_fd")
visible_parent_matches() {
	[ ! -L "$parent_display" ] && [ -d "$parent_display" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_display" 2>/dev/null || true)" = "$parent_id" ]
}
target_absent() {
	[ ! -e "$parent_fd/$target_name" ] && [ ! -L "$parent_fd/$target_name" ]
}
if ! visible_parent_matches || ! target_absent; then
	printf '%s\n' "secrets target appeared or destination root changed before publication: $parent_display/$target_name" >&2
	exit 17
fi

` + privilegedPinnedDirectoryCreatorScript + `

umask 077
create_pinned_directory "$parent_fd" ".wahrwelt-secrets-recovery-" "$python_bin"
candidate_name=$created_name
candidate_id=$created_id
candidate_fd=/proc/self/fd/8
if [ "$(stat -Lc '%d:%i' -- "$source_fd")" != "$source_id" ]; then
	printf '%s\n' "staged secrets directory changed before copy; recovery retained at $parent_display/$candidate_name" >&2
	exit 1
fi
if [ "$(id -u)" = 0 ]; then
	rsync -a --delete --checksum --chown root:root "$source_fd/" "$candidate_fd/"
else
	rsync -a --delete --checksum "$source_fd/" "$candidate_fd/"
fi
# Nix store paths intentionally strip owner write bits. Secrets are published
# from the validated store tree, so restore a private writable shape without
# granting group/other access.
chmod -R u+rwX,go-rwx -- "$candidate_fd"
candidate_matches() {
	[ ! -L "$parent_fd/$candidate_name" ] && [ -d "$parent_fd/$candidate_name" ] &&
		[ "$(stat -c '%d:%i' -- "$parent_fd/$candidate_name" 2>/dev/null || true)" = "$candidate_id" ] &&
		[ "$(stat -Lc '%d:%i' -- "$candidate_fd")" = "$candidate_id" ]
}
if ! candidate_matches || ! visible_parent_matches; then
	printf '%s\n' "secrets candidate or destination root changed; recovery retained at $parent_display/$candidate_name" >&2
	exit 1
fi
if ! target_absent; then
	printf '%s\n' "secrets target appeared during publication; recovery retained at $parent_display/$candidate_name" >&2
	exit 17
fi
if ! mv -T --no-copy --update=none-fail -- "$parent_fd/$candidate_name" "$parent_fd/$target_name"; then
	printf '%s\n' "secrets target collision; recovery retained at $parent_display/$candidate_name" >&2
	exit 17
fi
target_id=$(stat -c '%d:%i' -- "$parent_fd/$target_name" 2>/dev/null || true)
if [ "$target_id" != "$candidate_id" ]; then
	if [ -n "$target_id" ] && [ ! -e "$parent_fd/$candidate_name" ] && [ ! -L "$parent_fd/$candidate_name" ] &&
		mv -T --no-copy --update=none-fail -- "$parent_fd/$target_name" "$parent_fd/$candidate_name"; then
		restored_id=$(stat -c '%d:%i' -- "$parent_fd/$candidate_name" 2>/dev/null || true)
		if [ "$restored_id" = "$target_id" ]; then
			printf '%s\n' "unexpected secrets candidate restored at $parent_display/$candidate_name; expected recovery retained through pinned descriptor $candidate_fd" >&2
			exit 1
		fi
	fi
	printf '%s\n' "secrets target changed after publication; expected tree retained through pinned descriptor $candidate_fd" >&2
	exit 1
fi
if ! visible_parent_matches; then
	if [ ! -e "$parent_fd/$candidate_name" ] && [ ! -L "$parent_fd/$candidate_name" ]; then
		mv -T --no-copy --update=none-fail -- "$parent_fd/$target_name" "$parent_fd/$candidate_name" || true
	fi
	printf '%s\n' "secrets destination root changed after publication; recovery retained through pinned $parent_display" >&2
	exit 1
fi
printf '%s\n' "$candidate_id"
`

//nolint:gocyclo // Directory publication keeps its complete identity chain visible in one transaction.
func writeStagedSecrets(ctx context.Context, runner run.CommandRunner, staging, dest string, layout Layout) error {
	if layout != LayoutThin {
		return nil
	}
	source := filepath.Join(staging, "secrets")
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.IsDir() {
		return fmt.Errorf("staged secrets path is not a directory: %s", source)
	}
	target := filepath.Join(dest, "secrets")
	if runner.IsDryRun() {
		if targetInfo, err := os.Lstat(target); err == nil {
			if targetInfo.Mode()&os.ModeSymlink != 0 || !targetInfo.IsDir() {
				return fmt.Errorf("target secrets path is not a directory: %s", target)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		fmt.Println("write secrets/")
		return nil
	}
	sourcePinned, err := openPinnedDirectoryPath(source, "staged secrets")
	if err != nil {
		return err
	}
	defer closeFile(sourcePinned.file)
	root, err := openPinnedParentDirectory(target, "secrets root")
	if err != nil {
		return err
	}
	defer closeFile(root)
	existing, exists, err := openPinnedDirectoryEntryAt(root, filepath.Base(target), target, "secrets")
	if err != nil {
		if _, lstatErr := os.Lstat(target); lstatErr == nil {
			return fmt.Errorf("target secrets path is not a directory: %s", target)
		}
		return err
	}
	if existing != nil {
		defer closeFile(existing.file)
	}
	if exists {
		if err := verifyPinnedDirectoryEntry(root, filepath.Base(target), target, "secrets", existing); err != nil {
			return err
		}
		return verifyPinnedDirectoryVisible(root, dest, "secrets root")
	}
	publishCtx, cancel := cleanupContext(ctx)
	defer cancel()
	pythonPath, err := privilegedPythonPath()
	if err != nil {
		return err
	}
	token, err := runner.Output(publishCtx, "sudo", "sh", "-c", privilegedPublishSecretsScript, "--",
		fileDescriptorPath(root), fileDescriptorPath(sourcePinned.file), dest, pythonPath)
	runtime.KeepAlive(root)
	runtime.KeepAlive(sourcePinned.file)
	if err != nil {
		return fmt.Errorf("publish secrets directory: %w", err)
	}
	published, exists, err := openPinnedDirectoryEntryAt(root, filepath.Base(target), target, "secrets")
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("published secrets directory is absent: %s", target)
	}
	defer closeFile(published.file)
	publishedFD, err := checkedFileDescriptor(published.file, "published secrets")
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(publishedFD, &stat); err != nil {
		return fmt.Errorf("stat published secrets directory: %w", err)
	}
	if got := fmt.Sprintf("%d:%d", stat.Dev, stat.Ino); token != got {
		return fmt.Errorf("published secrets directory changed before pin: %s", target)
	}
	if err := verifyPinnedDirectoryEntry(root, filepath.Base(target), target, "secrets", published); err != nil {
		return err
	}
	if err := verifyPinnedDirectoryVisible(root, dest, "secrets root"); err != nil {
		return err
	}
	if published.info.Mode().Perm() != 0o700 {
		return fmt.Errorf("published secrets directory mode = %04o, want 0700: %s", published.info.Mode().Perm(), target)
	}
	return nil
}
