#!/usr/bin/env python3
"""Patch end4-pC for immutable Wahrwelt-managed installation."""

from pathlib import Path
import re
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


def require_markers(text: str, markers: tuple[str, ...], label: str) -> None:
    for marker in markers:
        if marker not in text:
            raise SystemExit(f"unrecognized {label}: missing {marker.strip()}")


def qml_function_span(text: str, name: str, label: str) -> tuple[int, int]:
    matches = list(re.finditer(rf"\bfunction\s+{re.escape(name)}\s*\(", text))
    if len(matches) != 1:
        raise SystemExit(
            f"unrecognized {label}: expected one {name} function, found {len(matches)}"
        )

    start = matches[0].start()
    opening = text.find("{", matches[0].end())
    if opening < 0:
        raise SystemExit(f"unrecognized {label}: {name} function has no body")

    depth = 0
    quote = None
    escaped = False
    line_comment = False
    block_comment = False
    index = opening
    while index < len(text):
        char = text[index]
        next_char = text[index + 1] if index + 1 < len(text) else ""

        if line_comment:
            if char == "\n":
                line_comment = False
        elif block_comment:
            if char == "*" and next_char == "/":
                block_comment = False
                index += 1
        elif quote is not None:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = None
        elif char == "/" and next_char == "/":
            line_comment = True
            index += 1
        elif char == "/" and next_char == "*":
            block_comment = True
            index += 1
        elif char in "'\"`":
            quote = char
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return start, index + 1
        index += 1

    raise SystemExit(f"unrecognized {label}: unterminated {name} function")


def replace_qml_function(
    text: str, name: str, replacement: str, label: str
) -> str:
    start, end = qml_function_span(text, name, label)
    return text[:start] + replacement.rstrip("\n") + text[end:]


def normalized_source(text: str) -> str:
    return " ".join(text.split())


UPSTREAM_SYSTEM_UPDATE = '''\
    function runSystemUpdate() {
        Quickshell.execDetached([
            "kitty", "--hold",
            "fish", "-i", "-l", "-c",
            "yay -Syu --combinedupgrade=false"
        ])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }
'''

UPSTREAM_DOTS_UPDATE = '''\
    function runUpdateDots() {
        const updateScript = `
            set -e
            DIR="$HOME/.config/quickshell"

            rm -rf "$DIR/end4-pC-tmp"
            git clone https://github.com/pctrade/end4-pC.git "$DIR/end4-pC-tmp"

            rm -rf "$DIR/end4-pC-old"
            [ -d "$DIR/end4-pC" ] && mv "$DIR/end4-pC" "$DIR/end4-pC-old"
            mv "$DIR/end4-pC-tmp" "$DIR/end4-pC"

            killall qs 2>/dev/null || true
            sleep 0.5
            setsid qs -c end4-pC >/tmp/qs.log 2>&1 < /dev/null &
            disown

            rm -rf "$DIR/end4-pC-old"
        `

        Quickshell.execDetached(["kitty", "--hold", "bash", "-c", updateScript])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }
'''


def update_function_sources(updater_notice: str) -> tuple[str, str]:
    system_update = """    function runSystemUpdate() {
        Quickshell.execDetached(["foot", "nixos-update"])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }

"""
    dots_update = f"""    function runUpdateDots() {{
        Quickshell.execDetached([
            "notify-send",
            "Wahrwelt",
            {updater_notice!r}
        ])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }}

"""
    return system_update, dots_update


def patch_about(root: Path, updater_notice: str) -> None:
    path = root / "modules/ii/settings/pages/About.qml"
    text = path.read_text()

    require_markers(
        text,
        (
            "    function runSystemUpdate() {",
            '"yay -Syu --combinedupgrade=false"',
            "    function runUpdateDots() {",
            "const updateScript = `",
            "git clone https://github.com/pctrade/end4-pC.git",
            "killall qs 2>/dev/null",
            "setsid qs -c end4-pC",
        ),
        "end4-pC About updater",
    )
    system_update, dots_update = update_function_sources(updater_notice)
    text = replace_function(
        text,
        "    function runSystemUpdate() {",
        "    function runUpdateDots() {",
        system_update,
    )

    text = replace_function(
        text,
        "    function runUpdateDots() {",
        "    Rectangle {",
        dots_update,
    )

    path.write_text(text)


def validate_shared_update_callers(root: Path) -> None:
    about = root / "modules/ii/settings/pages/About.qml"
    dashboard = root / "modules/ii/settings/DashboardHomePage.qml"
    about_text = about.read_text()
    dashboard_text = dashboard.read_text()

    for action in ("runSystemUpdate", "runUpdateDots"):
        wrapper = re.compile(
            rf"\bfunction\s+{action}\s*\(\s*\)\s*\{{\s*"
            rf"DotsUpdater\s*\.\s*{action}\s*\(\s*\)\s*;?\s*\}}"
        )
        if not wrapper.search(about_text):
            raise SystemExit(
                f"unrecognized end4-pC DotsUpdater call sites: About.qml does not forward {action}"
            )
        dashboard_call = re.compile(
            rf"DotsUpdater\s*\.\s*{action}\s*\(\s*\)"
        )
        if not dashboard_call.search(dashboard_text):
            raise SystemExit(
                f"unrecognized end4-pC DotsUpdater call sites: DashboardHomePage.qml lacks {action}"
            )


def patch_dots_updater(root: Path, updater_notice: str) -> None:
    path = root / "services/DotsUpdater.qml"
    text = path.read_text()
    system_start, system_end = qml_function_span(
        text, "runSystemUpdate", "end4-pC DotsUpdater"
    )
    dots_start, dots_end = qml_function_span(
        text, "runUpdateDots", "end4-pC DotsUpdater"
    )
    if system_end > dots_start:
        raise SystemExit("unrecognized end4-pC DotsUpdater: updater functions are reordered")
    if normalized_source(text[system_start:system_end]) != normalized_source(
        UPSTREAM_SYSTEM_UPDATE
    ):
        raise SystemExit(
            "unrecognized end4-pC DotsUpdater: runSystemUpdate implementation changed"
        )
    if normalized_source(text[dots_start:dots_end]) != normalized_source(
        UPSTREAM_DOTS_UPDATE
    ):
        raise SystemExit(
            "unrecognized end4-pC DotsUpdater: runUpdateDots implementation changed"
        )

    validate_shared_update_callers(root)
    system_update, dots_update = update_function_sources(updater_notice)
    text = replace_qml_function(
        text, "runSystemUpdate", system_update, "end4-pC DotsUpdater"
    )
    text = replace_qml_function(
        text, "runUpdateDots", dots_update, "end4-pC DotsUpdater"
    )
    if any(
        marker in text
        for marker in (
            "yay -Syu",
            "git clone https://github.com/pctrade/end4-pC.git",
            "killall qs",
            "setsid qs -c end4-pC",
        )
    ):
        raise SystemExit("unmanaged end4-pC DotsUpdater update action remains")
    path.write_text(text)


def patch_updates(root: Path, updater_notice: str) -> None:
    service_path = root / "services/DotsUpdater.qml"
    if service_path.exists():
        if not service_path.is_file():
            raise SystemExit("unrecognized end4-pC DotsUpdater: not a regular file")
        patch_dots_updater(root, updater_notice)
        return

    patch_about(root, updater_notice)


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
            visible: false
            icon: "timer"
            shape: MaterialShape.Shape.Cookie12Sided
            title: Translation.tr("Idle")
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
    patch_updates(root, sys.argv[2])
    patch_updates_count(root)
    patch_managed_quickshell_lifecycle(root)
    patch_managed_hypridle_settings(root)


if __name__ == "__main__":
    main()
