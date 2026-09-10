#!/usr/bin/env python3
import importlib.util
import tempfile
import unittest
from pathlib import Path

from PIL import Image


MODULE_PATH = Path(__file__).with_name("vendor") / "python" / "video_cache.py"
SPEC = importlib.util.spec_from_file_location("video_cache", MODULE_PATH)
assert SPEC and SPEC.loader
video_cache = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(video_cache)


class MediaClassificationTests(unittest.TestCase):
    def test_video_extensions_are_live(self):
        for suffix in (".mp4", ".webm", ".mkv", ".mov", ".avi", ".gif"):
            with self.subTest(suffix=suffix):
                self.assertEqual(video_cache.classify_path(Path("wallpaper" + suffix)), "live")

    def test_static_webp_remains_static(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "still.webp"
            Image.new("RGB", (2, 2), "red").save(path, "WEBP")
            self.assertEqual(video_cache.classify_path(path), "static")

    def test_animated_webp_is_live(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "animated.webp"
            first = Image.new("RGB", (2, 2), "red")
            second = Image.new("RGB", (2, 2), "blue")
            first.save(path, "WEBP", save_all=True, append_images=[second], duration=100, loop=0)
            self.assertEqual(video_cache.classify_path(path), "live")


if __name__ == "__main__":
    unittest.main()
