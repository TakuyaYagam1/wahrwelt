#!/usr/bin/env python3
"""Patch end4-pC for immutable Wahrwelt-managed installation."""

from pathlib import Path
import sys


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if old not in text:
        raise SystemExit(f"{label} not found")
    return text.replace(old, new, 1)


def replace_function(text: str, start: str, end: str, replacement: str) -> str:
    start_index = text.find(start)
    if start_index < 0:
        raise SystemExit(f"start marker not found: {start.strip()}")

    end_index = text.find(end, start_index)
    if end_index < 0:
        raise SystemExit(f"end marker not found: {end.strip()}")

    return text[:start_index] + replacement + text[end_index:]


def patch_about(root: Path, updater_notice: str) -> None:
    path = root / "modules/ii/settings/pages/About.qml"
    text = path.read_text()

    system_update = """    function runSystemUpdate() {
        Quickshell.execDetached(["foot", "nixos-update"])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }

"""
    text = replace_function(
        text,
        "    function runSystemUpdate() {",
        "    function runUpdateDots() {",
        system_update,
    )

    dots_update = f"""    function runUpdateDots() {{
        Quickshell.execDetached([
            "notify-send",
            "Wahrwelt",
            {updater_notice!r}
        ])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }}

"""
    text = replace_function(
        text,
        "    function runUpdateDots() {",
        "    Rectangle {",
        dots_update,
    )

    path.write_text(text)


def patch_updates_count(root: Path) -> None:
    path = root / "modules/ii/bar/UpdatesCount.qml"
    text = path.read_text()
    old = """        command: [
            "kitty", "--hold",
            "fish", "-i", "-l", "-c",
            "yay -Syu --combinedupgrade=false"
        ]"""
    new = '        command: ["foot", "nixos-update"]'
    if old not in text:
        raise SystemExit("UpdatesCount Arch update command not found")
    path.write_text(text.replace(old, new, 1))


def patch_managed_quickshell_lifecycle(root: Path) -> None:
    ipc_old = '["qs", "-p", Quickshell.shellPath(""), "ipc", "call"'
    ipc_new = '["qs", "-c", Quickshell.env("qsConfig"), "ipc", "call"'
    ipc_rewrites = 0
    for path in root.rglob("*.qml"):
        text = path.read_text()
        count = text.count(ipc_old)
        if count:
            path.write_text(text.replace(ipc_old, ipc_new))
            ipc_rewrites += count
    if ipc_rewrites != 7:
        raise SystemExit(
            f"expected 7 path-scoped QuickShell IPC calls, found {ipc_rewrites}"
        )

    replacements = [
        (
            root / "services/FirstRunExperience.qml",
            '        Quickshell.execDetached(["bash", "-c", `qs -p \'${root.welcomeQmlPath}\'`])',
            '        Quickshell.execDetached(["notify-send", root.firstRunNotifSummary, root.firstRunNotifBody, "-a", "Shell"])',
            "first-run welcome lifecycle",
        ),
        (
            root / "services/ConflictKiller.qml",
            '                    Quickshell.execDetached(["qs", "-p", root.killDialogQmlPath])',
            '                    Quickshell.execDetached(["notify-send", "Shell conflict", "Another notification daemon is already running", "-a", "Shell"])',
            "conflict dialog lifecycle",
        ),
    ]
    for path, old, new, label in replacements:
        text = path.read_text()
        path.write_text(replace_once(text, old, new, label))

    for path in root.rglob("*.qml"):
        text = path.read_text()
        if '["qs", "-p"' in text or "qs -p" in text:
            raise SystemExit(f"unmanaged QuickShell lifecycle remains in {path}")


def patch_managed_hypridle_settings(root: Path) -> None:
    service_path = root / "services/HyprlandConfig.qml"
    service_text = service_path.read_text()
    direct_restart = "pkill -x hypridle; sleep 0.3; setsid -f hypridle"
    managed_markers = (
        "idleConfiguratorScriptPath",
        "hypridlePath",
        "function setIdle(",
    )

    if direct_restart not in service_text:
        if any(marker in service_text for marker in managed_markers):
            raise SystemExit("unrecognized end4-pC hypridle settings implementation")
        return

    upstream_block = '''    readonly property string idleConfiguratorScriptPath: Quickshell.shellPath("scripts/hyprland/hypridleconfigurator.py")
    readonly property string hypridlePath: FileUtils.trimFileProtocol(`${Directories.config}/hypr/hypridle.conf`)

    function setIdle(lock: int, screenOff: int, suspend: int) {
        Quickshell.execDetached([
            "python3", root.idleConfiguratorScriptPath,
            "--file", root.hypridlePath,
            "--lock", String(lock),
            "--screen-off", String(screenOff),
            "--suspend", String(suspend)
        ])
        Quickshell.execDetached(["bash", "-c", "pkill -x hypridle; sleep 0.3; setsid -f hypridle >/dev/null 2>&1"])
    }

'''
    managed_block = '''    function setIdle(lock: int, screenOff: int, suspend: int) {
        Quickshell.execDetached([
            "notify-send",
            "Wahrwelt",
            "Idle timers are managed by Wahrwelt through NixOS configuration",
            "-a", "Shell"
        ])
    }

'''
    service_text = replace_once(
        service_text,
        upstream_block,
        managed_block,
        "end4-pC hypridle settings lifecycle",
    )
    if direct_restart in service_text:
        raise SystemExit("unmanaged end4-pC hypridle restart remains")
    service_path.write_text(service_text)

    page_path = root / "modules/ii/settings/pages/HyprlandConfig.qml"
    page_text = page_path.read_text()
    idle_section = '''        ContentSection {
            id: idleSection
            icon: "timer"
            shape: MaterialShape.Shape.Cookie12Sided
            title: Translation.tr("Idle")
'''
    managed_idle_section = '''        ContentSection {
            id: idleSection
            enabled: false
            icon: "timer"
            shape: MaterialShape.Shape.Cookie12Sided
            title: Translation.tr("Idle") + " - managed by Wahrwelt"
'''
    page_path.write_text(
        replace_once(
            page_text,
            idle_section,
            managed_idle_section,
            "end4-pC idle settings section",
        )
    )


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit(
            "usage: quickshell-pc.py <patched-quickshell-root> <updater-notice>"
        )

    root = Path(sys.argv[1])
    patch_about(root, sys.argv[2])
    patch_updates_count(root)
    patch_managed_quickshell_lifecycle(root)
    patch_managed_hypridle_settings(root)


if __name__ == "__main__":
    main()
