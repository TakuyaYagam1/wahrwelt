"""Evaluate real Home Manager manifests/DAG without building or activating it."""

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


class ManagedConfigIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.home = Path(cls.temp.name)
        fixture = Path(__file__).with_name("managed-config.nix")
        result = subprocess.run(
            [
                "nix",
                "eval",
                "--impure",
                "--json",
                "--file",
                str(fixture),
                "--apply",
                (
                    'f: f { home = builtins.getEnv "DOTFILE_TEST_HOME"; '
                    'nixosDir = builtins.getEnv "DOTFILE_TEST_NIXOS"; }'
                ),
            ],
            env=os.environ
            | {
                "DOTFILE_TEST_HOME": str(cls.home),
                "DOTFILE_TEST_NIXOS": str(fixture.resolve().parents[3]),
            },
            text=True,
            capture_output=True,
            timeout=120,
            check=False,
        )
        if result.returncode:
            raise AssertionError(result.stderr)
        cls.config = json.loads(result.stdout)
        result = subprocess.run(
            [
                "nix",
                "derivation",
                "show",
                *cls.config["manifests"],
            ],
            text=True,
            capture_output=True,
            timeout=30,
            check=False,
        )
        if result.returncode:
            raise AssertionError(result.stderr)
        cls.manifests = {}
        document = json.loads(result.stdout)
        # Nix >= 2.33 wraps the derivations map in a versioned document.
        for path, drv in document.get("derivations", document).items():
            attrs = drv.get("structuredAttrs", drv["env"])
            if "mutable-dotfiles" in path:
                cls.manifests["mutable"] = json.loads(attrs["text"])
            elif "managed-dotfiles" in path:
                cls.manifests["guard"] = json.loads(attrs["text"])

    def test_guard_uses_config_relative_paths_and_immutable_sources(self):
        files = self.manifests["guard"]["files"]
        self.assertIn("fish/config.fish", files)
        self.assertIn("nvim/init.lua", files)
        for name, entry in files.items():
            self.assertFalse(name.startswith("config with spaces/"), name)
            self.assertTrue(entry["source"].startswith("/nix/store/"), entry)
            for baseline in entry["baselines"]:
                self.assertTrue(baseline.startswith("/nix/store/"), baseline)

    def test_mutable_defaults_and_user_templates_have_distinct_policies(self):
        files = self.manifests["mutable"]["files"]
        self.assertEqual(files["nvim/lua/config/options.lua"]["policy"], "merge")
        for name in (
            "fish/user/config.fish",
            "nvim/lua/user/options.lua",
            "nvim/lazy-lock.json",
        ):
            self.assertEqual(files[name]["policy"], "seed")
        self.assertNotIn("nvim/init.lua", files)
        for entry in files.values():
            self.assertTrue(entry["source"].startswith("/nix/store/"), entry)

    def test_preflight_precedes_all_home_manager_writes(self):
        order = self.config["order"]
        self.assertLess(
            order.index("wahrweltDotfilePreflight"), order.index("checkLinkTargets")
        )
        self.assertLess(
            order.index("wahrweltDotfilePreflight"), order.index("writeBoundary")
        )
        self.assertGreater(
            order.index("wahrweltDotfileApply"), order.index("linkGeneration")
        )
        self.assertIn("$DRY_RUN_CMD", self.config["apply"])
        self.assertIn("--previous-root", self.config["preflight"])
        self.assertTrue(all(self.config["assertions"]))


if __name__ == "__main__":
    unittest.main()
