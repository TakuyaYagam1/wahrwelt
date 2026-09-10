#!/usr/bin/env python3
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from PIL import Image


HELPER = Path(__file__).with_name("vendor") / "update-caelestia-live-thumbs"


class ThumbnailTests(unittest.TestCase):
    def test_one_directory_and_cached_first_frames(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp) / "home"
            wallpapers = home / "Pictures" / "Wallpapers"
            wallpapers.mkdir(parents=True)
            Image.new("RGB", (4, 4), "red").save(wallpapers / "still.webp", "WEBP")
            first = Image.new("RGB", (4, 4), "red")
            second = Image.new("RGB", (4, 4), "blue")
            first.save(wallpapers / "moving.webp", "WEBP", save_all=True, append_images=[second], duration=100, loop=0)

            environment = {**os.environ, "HOME": str(home)}
            subprocess.run([str(HELPER), str(wallpapers)], check=True, env=environment)

            properties_path = home / ".cache" / "caelestia" / "wallpaper_properties.json"
            properties = json.loads(properties_path.read_text())
            self.assertEqual(properties[str(wallpapers / "still.webp")]["kind"], "static")
            self.assertEqual(properties[str(wallpapers / "moving.webp")]["kind"], "live")
            for entry in properties.values():
                self.assertTrue(Path(entry["thumbnail"]).is_file())


if __name__ == "__main__":
    unittest.main()
