"""Exercise the managed-file preflight against current and stale symlinks."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

GUARD = Path(__file__).resolve().parents[2] / "lib/managed-file-guard.py"


class ManagedFileGuardTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with tempfile.TemporaryDirectory() as temp:
            fixture = Path(temp) / "managed-guard-test"
            fixture.write_text("approved")
            result = subprocess.run(
                ["nix-store", "--add", str(fixture)],
                capture_output=True,
                text=True,
                check=True,
            )
        cls.stored_file = Path(result.stdout.strip())

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.root = self.home / ".config"
        self.managed = self.root / "hypr/scripts/noctalia-msg.sh"
        self.managed.parent.mkdir(parents=True)
        self.source = self.home / "default.sh"
        self.source.write_text("approved")
        self.manifest = self.home / "manifest.json"
        self.manifest.write_text(
            json.dumps(
                {
                    "version": 1,
                    "files": {
                        "hypr/scripts/noctalia-msg.sh": {
                            "source": str(self.source),
                            "baselines": [],
                        }
                    },
                }
            )
        )

    def check_guard(self):
        return subprocess.run(
            [
                sys.executable,
                str(GUARD),
                "--home",
                str(self.home),
                "--root",
                str(self.root),
                "--manifest",
                str(self.manifest),
            ],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_stale_store_symlink_with_unchanged_contents_is_accepted(self):
        self.managed.symlink_to(self.stored_file)
        result = self.check_guard()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_symlink_outside_store_still_requires_managed_target(self):
        replacement = self.home / "replacement.sh"
        replacement.write_text("approved")
        self.managed.symlink_to(replacement)
        self.assertNotEqual(self.check_guard().returncode, 0)

    def test_changed_store_symlink_is_rejected(self):
        self.source.write_text("new default")
        self.managed.symlink_to(self.stored_file)
        self.assertNotEqual(self.check_guard().returncode, 0)


if __name__ == "__main__":
    unittest.main()
