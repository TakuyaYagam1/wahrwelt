"""Verify native, profile-scoped End4 settings launch contracts."""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
from pathlib import Path

OFFICIAL_SETTINGS_LAUNCH = 'Quickshell.execDetached(["wahrwelt-end4-settings"]);'
OFFICIAL_SETTINGS_BUTTONS = (
    "ii/modules/waffle/bar/StartButton.qml",
    "ii/modules/ii/sidebarRight/SidebarRightContent.qml",
    "ii/modules/waffle/actionCenter/mainPage/MainPageFooter.qml",
)


def fail(message: str) -> None:
    raise SystemExit(f"FAIL: {message}")


def read_required(path: Path) -> str:
    try:
        return path.read_text()
    except OSError as error:
        fail(f"cannot read End4 settings contract file {path}: {error}")


def verify_official(artifact: Path) -> None:
    for relative in OFFICIAL_SETTINGS_BUTTONS:
        path = artifact / relative
        text = read_required(path)
        if text.count(OFFICIAL_SETTINGS_LAUNCH) != 1:
            fail(
                "End4 Official settings button must launch the exact ii/settings.qml "
                f"single instance through the managed launcher: {path}"
            )
        if "systemsettings" in text:
            fail(f"End4 Official settings button launches KDE settings: {path}")


def verify_pc(artifact: Path) -> None:
    settings = read_required(artifact / "modules/ii/settings/Settings.qml")
    family = read_required(artifact / "panelFamilies/IllogicalImpulseFamily.qml")
    for expected in (
        'target: "settings"',
        "function open(): void   { GlobalStates.settingsOpen = true; }",
    ):
        if expected not in settings:
            fail(f"End4 pC native settings IPC contract is missing {expected!r}")
    if "PanelLoader { component: Settings {} }" not in family:
        fail("End4 pC main shell does not load its native settings panel")


def fake_qs_end4(path: Path) -> None:
    path.write_text(
        "#!/bin/sh\n"
        ': "${WAHRWELT_TEST_CALLS:?}"\n'
        "printf '%s\\0' \"${WAHRWELT_END4_PROFILE:-}\" "
        '"${WAHRWELT_QS_CONFIG:-}" "${qsConfig:-}" -- "$@" '
        '>"$WAHRWELT_TEST_CALLS"\n'
    )
    path.chmod(0o700)


def launcher_call(
    launcher: Path, config_home: Path, active_profile: str, qs_config: str
) -> tuple[subprocess.CompletedProcess[str], list[str]]:
    with tempfile.TemporaryDirectory(prefix="end4-native-settings-") as temporary:
        root = Path(temporary)
        bin_dir = root / "bin"
        bin_dir.mkdir()
        fake_qs_end4(bin_dir / "qs-end4")
        state_home = root / "state"
        state_dir = state_home / "wahrwelt"
        state_dir.mkdir(parents=True)
        active_shell = state_dir / "active-shell"
        active_shell.write_text(f"{active_profile}\n")
        active_shell.chmod(0o600)
        calls = root / "calls"
        environment = {
            **os.environ,
            "HOME": str(root / "home"),
            "XDG_CONFIG_HOME": str(config_home),
            "XDG_STATE_HOME": str(state_home),
            "qsConfig": qs_config,
            "WAHRWELT_TEST_CALLS": str(calls),
            "PATH": f"{bin_dir}:{os.environ.get('PATH', '')}",
        }
        result = subprocess.run(
            [str(launcher)],
            check=False,
            capture_output=True,
            env=environment,
            text=True,
        )
        arguments = []
        if calls.exists():
            arguments = calls.read_bytes().rstrip(b"\0").decode().split("\0")
        return result, arguments


def verify_hypr(artifact: Path) -> None:
    variables = read_required(artifact / "hyprland/variables.lua")
    launcher = artifact / "wahrwelt/settings"
    if not launcher.is_file() or not os.access(launcher, os.X_OK):
        fail("End4 Hypr artifact is missing its executable native settings launcher")
    expected_binding = 'settingsApp = "wahrwelt-end4-settings"'
    if expected_binding not in variables:
        fail("End4 Hypr settings binding does not use the managed native launcher")
    if "systemsettings" in variables:
        fail("End4 Hypr settings binding retains a KDE settings fallback")

    config_home = Path("/tmp/wahrwelt-settings-contract/config")
    official = str(config_home / "quickshell/ii")
    result, arguments = launcher_call(launcher, config_home, "end4", official)
    if result.returncode != 0:
        fail(f"End4 Official native settings launcher failed: {result.stderr.strip()}")
    expected = [
        "end4",
        official,
        official,
        "--",
        "-n",
        "-d",
        "-p",
        f"{official}/settings.qml",
    ]
    if arguments != expected:
        fail(
            "End4 Official native settings launcher emitted "
            f"{arguments!r}, want {expected!r}"
        )

    pc = str(config_home / "quickshell/end4-pC")
    result, arguments = launcher_call(launcher, config_home, "end4-pc", pc)
    if result.returncode != 0:
        fail(f"End4 pC native settings launcher failed: {result.stderr.strip()}")
    expected = [
        "end4-pc",
        pc,
        pc,
        "--",
        "-c",
        "end4-pC",
        "ipc",
        "call",
        "settings",
        "open",
    ]
    if arguments != expected:
        fail(
            f"End4 pC native settings launcher emitted {arguments!r}, want {expected!r}"
        )

    result, arguments = launcher_call(
        launcher,
        config_home,
        "attacker",
        str(config_home / "quickshell/attacker"),
    )
    if result.returncode == 0 or arguments:
        fail("End4 native settings launcher accepted an unmanaged active shell state")


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {sys.argv[0]} END4_ARTIFACT")
    artifact = Path(sys.argv[1])
    if (artifact / "ii/settings.qml").is_file():
        verify_official(artifact)
    elif (artifact / "modules/ii/settings/Settings.qml").is_file():
        verify_pc(artifact)
    elif (artifact / "hyprland/variables.lua").is_file():
        verify_hypr(artifact)
    else:
        fail(f"cannot identify End4 settings artifact layout: {artifact}")


if __name__ == "__main__":
    main()
