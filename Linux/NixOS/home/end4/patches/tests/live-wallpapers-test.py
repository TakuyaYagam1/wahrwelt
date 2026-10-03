#!/usr/bin/env python3
"""Regression tests for the End4 live wallpaper patcher."""

from __future__ import annotations

import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest


sys.dont_write_bytecode = True
PATCHER_PATH = Path(__file__).resolve().parent.parent / "live-wallpapers.py"
SPEC = importlib.util.spec_from_file_location("live_wallpapers", PATCHER_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"unable to load {PATCHER_PATH}")
LIVE_WALLPAPERS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(LIVE_WALLPAPERS)


class PcSettingsPreviewTest(unittest.TestCase):
    def test_preview_patch_accepts_upstream_indentation_changes(self) -> None:
        patch_preview = getattr(LIVE_WALLPAPERS, "patch_pc_settings_preview", None)
        self.assertIsNotNone(patch_preview, "missing pC settings preview patch helper")

        expression = (
            "source: /\\.(mp4|webm|mkv|avi|mov)$/i.test(Config.options.background.wallpaperPath)\n"
            "    ? Config.options.background.thumbnailPath\n"
            "    : Config.options.background.wallpaperPath"
        )
        replacement = "source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)"

        for indentation in (20, 24):
            with self.subTest(indentation=indentation):
                indent = " " * indentation
                source = indent + expression.replace("\n", f"\n{indent}") + "\n"
                expected = indent + replacement + "\n"
                self.assertEqual(patch_preview(source, "QuickConfig fixture"), expected)


class WallpaperServiceDriftTest(unittest.TestCase):
    def test_unreviewed_service_is_rejected_without_replacing_it(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "services/Wallpapers.qml"
            path.parent.mkdir()
            original = "pragma Singleton\nFolderListModelWithHistory\nproperty string searchQuery\n"
            path.write_text(original)
            with self.assertRaisesRegex(SystemExit, "upstream Wallpapers.qml drifted"):
                LIVE_WALLPAPERS.patch_service(root, "pc")
            self.assertEqual(path.read_text(), original)


if __name__ == "__main__":
    unittest.main()
