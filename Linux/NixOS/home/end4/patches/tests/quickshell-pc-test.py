#!/usr/bin/env python3
"""Regression tests for the end4-pC update action patch."""

from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


sys.dont_write_bytecode = True
PATCHER_PATH = Path(__file__).resolve().parent.parent / "quickshell-pc.py"
SPEC = importlib.util.spec_from_file_location("quickshell_pc", PATCHER_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"unable to load {PATCHER_PATH}")
QUICKSHELL_PC = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(QUICKSHELL_PC)


UPDATER_NOTICE = "Wahrwelt manages end4-pC updates through the flake input"

LEGACY_ABOUT = '''\
ContentPage {
    function runSystemUpdate() {
        Quickshell.execDetached([
            "kitty", "--hold",
            "fish", "-i", "-l", "-c",
            "yay -Syu --combinedupgrade=false"
        ])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }

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

    Rectangle {}
}
'''

SHARED_UPDATER = '''\
pragma Singleton

import qs
import QtQuick
import Quickshell

Singleton {
    id: root

    function runSystemUpdate() {
        Quickshell.execDetached([
            "kitty", "--hold",
            "fish", "-i", "-l", "-c",
            "yay -Syu --combinedupgrade=false"
        ])
        Qt.callLater(() => GlobalStates.settingsOpen = false)
    }

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

    readonly property bool updateAvailable: true

    function updateSummary() {
        return "managed"
    }
}
'''


def write_file(root: Path, relative_path: str, text: str) -> Path:
    path = root / relative_path
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    return path


def updates_count_source() -> str:
    return '''\
Process {
        command: [
            "kitty", "--hold",
            "fish", "-i", "-l", "-c",
            "yay -Syu --combinedupgrade=false"
        ]
}
'''


def run_main(root: Path) -> None:
    with (
        patch.object(QUICKSHELL_PC, "patch_managed_quickshell_lifecycle"),
        patch.object(QUICKSHELL_PC, "patch_managed_hypridle_settings"),
        patch.object(
            sys,
            "argv",
            [str(PATCHER_PATH), str(root), UPDATER_NOTICE],
        ),
    ):
        QUICKSHELL_PC.main()


def update_functions(text: str) -> str:
    functions = []
    for name in ("runSystemUpdate", "runUpdateDots"):
        match = re.search(rf"\bfunction\s+{name}\s*\(\s*\)\s*\{{", text)
        if match is None:
            raise AssertionError(f"missing {name} function")

        opening_brace = match.end() - 1
        depth = 0
        quote = None
        escaped = False
        for index in range(opening_brace, len(text)):
            char = text[index]
            if quote is not None:
                if escaped:
                    escaped = False
                elif char == "\\":
                    escaped = True
                elif char == quote:
                    quote = None
                continue
            if char in "'\"`":
                quote = char
            elif char == "{":
                depth += 1
            elif char == "}":
                depth -= 1
                if depth == 0:
                    functions.append(text[match.start() : index + 1])
                    break
        else:
            raise AssertionError(f"unterminated {name} function")
    return "\n".join(functions)


def execute_update_functions(text: str) -> dict[str, object]:
    node = node_executable()
    harness = r'''
const fs = require("node:fs");
const detached = [];
const state = { settingsOpen: true };
const source = fs.readFileSync(0, "utf8");
const actions = new Function(
    "Quickshell", "Qt", "GlobalStates",
    source + "\nreturn { runSystemUpdate, runUpdateDots };"
)({ execDetached: command => detached.push(command) },
   { callLater: callback => callback() },
   state);
actions.runSystemUpdate();
actions.runUpdateDots();
process.stdout.write(JSON.stringify({ detached, settingsOpen: state.settingsOpen }));
'''
    result = subprocess.run(
        [node, "-e", harness],
        input=update_functions(text),
        capture_output=True,
        check=True,
        text=True,
    )
    return json.loads(result.stdout)


def node_executable() -> str:
    node_command = os.environ.get("NODE", "node")
    node = shutil.which(node_command)
    if node is None:
        raise RuntimeError(
            f"Node.js executable {node_command!r} is required to execute patched QML actions; set NODE to its path"
        )
    return node


def execute_about_forwarders(text: str) -> list[str]:
    node = node_executable()
    harness = r'''
const fs = require("node:fs");
const calls = [];
const source = fs.readFileSync(0, "utf8");
const actions = new Function(
    "DotsUpdater",
    source + "\nreturn { runSystemUpdate, runUpdateDots };"
)({
    runSystemUpdate: () => calls.push("runSystemUpdate"),
    runUpdateDots: () => calls.push("runUpdateDots")
});
actions.runSystemUpdate();
actions.runUpdateDots();
process.stdout.write(JSON.stringify(calls));
'''
    result = subprocess.run(
        [node, "-e", harness],
        input=update_functions(text),
        capture_output=True,
        check=True,
        text=True,
    )
    return json.loads(result.stdout)


class QuickShellPcUpdatePatchTest(unittest.TestCase):
    def test_legacy_about_update_actions_are_nix_managed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            about = write_file(
                root, "modules/ii/settings/pages/About.qml", LEGACY_ABOUT
            )
            updates_count = write_file(
                root, "modules/ii/bar/UpdatesCount.qml", updates_count_source()
            )

            run_main(root)

            about_text = about.read_text()
            self.assertEqual(
                execute_update_functions(about_text),
                {
                    "detached": [
                        ["foot", "nixos-update"],
                        ["notify-send", "Wahrwelt", UPDATER_NOTICE],
                    ],
                    "settingsOpen": False,
                },
            )
            self.assertIn('command: ["foot", "nixos-update"]', updates_count.read_text())

    def test_shared_updater_protects_about_and_dashboard_call_sites(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            service = write_file(root, "services/DotsUpdater.qml", SHARED_UPDATER)
            about = write_file(
                root,
                "modules/ii/settings/pages/About.qml",
                '''\
ContentPage {
    function runSystemUpdate( ) {
        DotsUpdater . runSystemUpdate ( );
    }
    function runUpdateDots( ) {
        DotsUpdater . runUpdateDots ( );
    }
    Rectangle {}
}
''',
            )
            dashboard = write_file(
                root,
                "modules/ii/settings/DashboardHomePage.qml",
                '''\
Item {
    downAction: () => DotsUpdater.runSystemUpdate()
    downAction: () => DotsUpdater.runUpdateDots()
}
''',
            )

            write_file(
                root, "modules/ii/bar/UpdatesCount.qml", updates_count_source()
            )
            run_main(root)

            self.assertEqual(
                execute_update_functions(service.read_text()),
                {
                    "detached": [
                        ["foot", "nixos-update"],
                        ["notify-send", "Wahrwelt", UPDATER_NOTICE],
                    ],
                    "settingsOpen": False,
                },
            )
            self.assertEqual(
                execute_about_forwarders(about.read_text()),
                ["runSystemUpdate", "runUpdateDots"],
            )
            self.assertIn("DotsUpdater.runSystemUpdate()", dashboard.read_text())
            self.assertIn("DotsUpdater.runUpdateDots()", dashboard.read_text())
            self.assertIn(
                "readonly property bool updateAvailable: true", service.read_text()
            )
            self.assertIn(
                'function updateSummary() {\n        return "managed"\n    }',
                service.read_text(),
            )

    def test_missing_node_is_a_clear_test_failure(self) -> None:
        with patch.dict(os.environ, {"NODE": "missing-node"}):
            with patch("shutil.which", return_value=None):
                with self.assertRaisesRegex(
                    RuntimeError,
                    "Node.js executable 'missing-node'.*required",
                ):
                    execute_update_functions("")

    def test_unrecognized_shared_updater_is_rejected_without_writing(self) -> None:
        malformed_sources = (
            SHARED_UPDATER.replace(
                '"yay -Syu --combinedupgrade=false"',
                '"unreviewed-updater-command"',
            ),
            SHARED_UPDATER.replace(
                "        Qt.callLater(() => GlobalStates.settingsOpen = false)\n    }\n\n    function runUpdateDots() {",
                '        Quickshell.execDetached(["unexpected-command"])\n'
                "        Qt.callLater(() => GlobalStates.settingsOpen = false)\n"
                "    }\n\n    function runUpdateDots() {",
                1,
            ),
        )
        for malformed in malformed_sources:
            with self.subTest(malformed=malformed != malformed_sources[0]):
                with tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    service = write_file(root, "services/DotsUpdater.qml", malformed)
                    original = service.read_text()
                    about = write_file(
                        root,
                        "modules/ii/settings/pages/About.qml",
                        '''\
ContentPage {
    function runSystemUpdate() { DotsUpdater.runSystemUpdate() }
    function runUpdateDots() { DotsUpdater.runUpdateDots() }
    Rectangle {}
}
''',
                    )
                    about_original = about.read_text()
                    write_file(
                        root, "modules/ii/bar/UpdatesCount.qml", updates_count_source()
                    )

                    with self.assertRaisesRegex(SystemExit, "unrecognized.*DotsUpdater"):
                        run_main(root)

                    self.assertEqual(service.read_text(), original)
                    self.assertEqual(about.read_text(), about_original)


if __name__ == "__main__":
    unittest.main()
