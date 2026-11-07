"""Exercise the OmniRouter update steps without fetching or building packages."""

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/update-omnirouter.yml"
PACKAGE = ROOT / "Linux/NixOS/pkgs/omnirouter.nix"
SOURCE_HASH = "sha256-+ImTZzi10hvj02qTWlO9J0XF6Y95JHUSFUHv/wdCddQ="
SWC_HASH = "sha256-rnD1tTpShE9ihP80zgnY8xEgYHha1hg4pkj0JAOeu2o="
NPM_HASH = "sha256-abcdef0123456789abcdef0123456789abcdef01234="


def step(name):
    lines = WORKFLOW.read_text().splitlines()
    start = lines.index(f"      - name: {name}")
    run = next(i for i in range(start + 1, len(lines)) if lines[i] == "        run: |")
    end = next(
        (
            i
            for i in range(run + 1, len(lines))
            if lines[i].startswith("      - name: ")
        ),
        len(lines),
    )
    return (
        "\n".join(
            line[10:] for line in lines[run + 1 : end] if line.startswith("          ")
        )
        + "\n"
    )


class OmniRouterUpdateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.package = self.directory / "omnirouter.nix"
        self.package.write_bytes(PACKAGE.read_bytes())
        self.swc_binding = self.package.read_text().split("in\nbuildNpmPackage'")[0]
        self.output = self.directory / "output"
        self.env = os.environ | {
            "OMNIROUTER_NIX": str(self.package),
            "GITHUB_OUTPUT": str(self.output),
            "FLAKE": "./Linux/NixOS",
        }

    def run_step(self, name, **values):
        script = step(name)
        for key, value in values.items():
            script = script.replace("${{ " + key + " }}", value)
        return subprocess.run(
            ["bash", "-e", "-c", script],
            check=False,
            env=self.env,
            text=True,
            capture_output=True,
        )

    def patch(self):
        result = self.run_step(
            "Patch version and source hash",
            **{
                "steps.release.outputs.version": "3.8.53",
                "steps.src.outputs.hash": SOURCE_HASH,
            },
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_source_update_preserves_swc_binding(self):
        self.patch()
        text = self.package.read_text()
        self.assertEqual(text.split("in\nbuildNpmPackage'")[0], self.swc_binding)
        self.assertIn(
            f'    hash = "{SOURCE_HASH}";', text.split("src = fetchFromGitHub {")[1]
        )
        self.assertIn('version = "3.8.53";', text)

    def fake_nix(self, log):
        binary = self.directory / "nix"
        binary.write_text('#!/bin/sh\ncat "$NIX_LOG"\nexit 1\n')
        binary.chmod(0o755)
        log_file = self.directory / "nix.log"
        log_file.write_text(log)
        self.env["NIX_LOG"] = str(log_file)
        self.env["PATH"] = str(self.directory) + os.pathsep + os.environ["PATH"]

    def test_other_hash_mismatch_is_not_npm_hash(self):
        self.patch()
        self.fake_nix(
            "error: hash mismatch in fixed-output derivation '/nix/store/abc-core-linux-x64-gnu-1.16.20.tgz.drv':\n"
            f"         specified: {SOURCE_HASH}\n"
            f"            got:    {SWC_HASH}\n"
        )
        result = self.run_step("Compute npmDepsHash")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(f'npmDepsHash = "{SWC_HASH}";', self.package.read_text())

    def test_npm_mismatch_updates_npm_hash(self):
        self.patch()
        self.fake_nix(
            "error: hash mismatch in fixed-output derivation '/nix/store/abc-omnirouter-3.8.53-npm-deps.drv':\n"
            "         specified: sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"
            f"            got:    {NPM_HASH}\n"
        )
        result = self.run_step("Compute npmDepsHash")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f'npmDepsHash = "{NPM_HASH}";', self.package.read_text())
        self.assertEqual(self.output.read_text(), f"hash={NPM_HASH}\n")


if __name__ == "__main__":
    unittest.main()
