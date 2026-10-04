package config

import (
	"os"
	"strings"
	"testing"
)

func readPrivilegeContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestPrimaryUserDoesNotReceiveSilentRootCapabilities(t *testing.T) {
	settings := readPrivilegeContractFile(t, "../../../NixOS/system/settings.nix")
	for _, forbidden := range []string{
		`"@wheel"`,
		"config.wahrwelt.user.username",
	} {
		if strings.Contains(settings, forbidden) {
			t.Fatalf("Nix trusted-users retains %q\n%s", forbidden, settings)
		}
	}

	security := readPrivilegeContractFile(t, "../../../NixOS/system/security.nix")
	if strings.Contains(security, "NOPASSWD") || strings.Contains(security, "extraRules") {
		t.Fatalf("sudo policy retains an unrestricted passwordless root path\n%s", security)
	}

	user := readPrivilegeContractFile(t, "../../../NixOS/users/user.nix")
	if strings.Contains(user, `"docker"`) {
		t.Fatalf("primary user remains root-equivalent through the Docker group\n%s", user)
	}
}

func TestFishContainerAliasesUseDockerCLI(t *testing.T) {
	fish := readPrivilegeContractFile(t, "../../../NixOS/home/programs/fish.nix")
	if strings.Contains(fish, `      docker = `) {
		t.Fatalf("Fish masks the Docker command with an alias\n%s", fish)
	}
	for _, required := range []string{
		`dc = "docker compose";`,
		`dps = "docker ps";`,
		`dpsa = "docker ps -a";`,
		`di = "docker images";`,
		`drm = "docker rm";`,
		`drmi = "docker rmi";`,
	} {
		if !strings.Contains(fish, required) {
			t.Errorf("Fish is missing Docker shortcut %q", required)
		}
	}
}

func TestDeveloperContainersDefaultToRootlessDocker(t *testing.T) {
	options := readPrivilegeContractFile(t, "../../../NixOS/modules/mysetup-options.nix")
	for _, required := range []string{
		`type = types.enum [`,
		`"docker"`,
		`"podman"`,
		`default = "docker";`,
	} {
		if !strings.Contains(options, required) {
			t.Errorf("container engine option is missing %q", required)
		}
	}

	virtualization := readPrivilegeContractFile(t, "../../../NixOS/services/virtualization.nix")
	for _, required := range []string{
		"enable = lib.mkDefault false;",
		"enable = useDocker;",
		"setSocketVariable = useDocker;",
		"enable = usePodman;",
		"dockerCompat = false;",
		"dockerSocket.enable = false;",
		`"unix://$XDG_RUNTIME_DIR/docker.sock"`,
		`"unix://$XDG_RUNTIME_DIR/podman/podman.sock"`,
		"users.users.${cfg.user.username}.linger = true;",
	} {
		if !strings.Contains(virtualization, required) {
			t.Errorf("selected rootless engine contract is missing %q", required)
		}
	}
}

func TestPortainerRunsAsAUserServiceOnTheSelectedEngine(t *testing.T) {
	portainer := readPrivilegeContractFile(t, "../../../NixOS/services/portainer.nix")
	for _, required := range []string{
		"systemd.user.services.portainer = {",
		"requires = [ engineService ];",
		"after = [ engineService ];",
		"partOf = [ engineService ];",
		"StateDirectory = \"portainer\";",
		"StateDirectoryMode = \"0700\";",
		"RestartPreventExitStatus = \"42\";",
		"ExecStart = \"${startCommand} ${engineCommand}\";",
		"ExecStop = \"${stopCommand} ${engineCommand}\";",
		"--label=io.wahrwelt.service=portainer",
		"exit 42",
		"$XDG_RUNTIME_DIR/${socketPath}:/var/run/docker.sock",
	} {
		if !strings.Contains(portainer, required) {
			t.Errorf("Portainer engine contract is missing %q", required)
		}
	}
	if strings.Contains(portainer, "virtualisation.oci-containers") {
		t.Fatalf("Portainer still starts through the rootful OCI container service\n%s", portainer)
	}
}
