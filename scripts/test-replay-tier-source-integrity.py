#!/usr/bin/env python3
"""Reject replay proof source and CLI bypasses before a live run."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from replay_tier_enrollment import (  # noqa: E402
    verify_reviewed_source, verify_source_guard_activation,
)

ROOT = Path(__file__).resolve().parent.parent


def committed_fixture(root: Path) -> None:
    """Create a small reviewed Go checkout for source-guard mutations."""
    (root / "go").mkdir()
    (root / "go/go.mod").write_text("module example.com/replayguard\n\ngo 1.26\n")
    (root / ".gitignore").write_text("go.work\ngoenv\n")
    (root / "tracked.txt").write_text("reviewed\n")
    for command in (
        ("git", "init", "-q"),
        ("git", "config", "user.name", "Replay Fixture"),
        ("git", "config", "user.email", "replay@example.invalid"),
        ("git", "add", "."),
        ("git", "commit", "-qm", "fixture"),
    ):
        subprocess.run(command, cwd=root, check=True, capture_output=True)


class ReplayTierSourceIntegrityTest(unittest.TestCase):
    """Check source guards and CLI activation against seeded bypasses."""

    def test_source_guard_function_and_cli_activation_are_closed(self) -> None:
        source = (ROOT / "scripts/lib/replay_tier_enrollment.py").read_text()
        verify_source_guard_activation(source)
        for changed in (
            source.replace("\n        verify_reviewed_source(args.root)\n",
                           "\n        # verify_reviewed_source(args.root)\n", 1),
            source.replace("\n        verify_reviewed_source(args.root)\n",
                           "\n        pass\n", 1),
            source.replace("\n            verify_reviewed_source(args.root)\n",
                           "\n            pass\n", 1),
            source.replace('    for name in ("GOFLAGS", "GOWORK"):',
                           '    return\n    for name in ("GOFLAGS", "GOWORK"):', 1),
            source.replace("raise SystemExit(main())", "raise SystemExit(0)", 1),
            source.replace("    try:\n        verify_reviewed_source(args.root)",
                           "    return 0\n    try:\n        verify_reviewed_source(args.root)", 1),
        ):
            with self.assertRaises(ValueError):
                verify_source_guard_activation(changed)

    def test_reviewed_source_rejects_dirty_checkout_and_go_overrides(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            committed_fixture(fixture)
            with mock.patch.dict(os.environ, {"GOFLAGS": "", "GOWORK": ""}):
                verify_reviewed_source(fixture)
                check = subprocess.run(
                    ("python3", str(ROOT / "scripts/lib/replay_tier_enrollment.py"),
                     "--root", str(fixture), "--reviewed-source"),
                    capture_output=True, text=True, check=False,
                )
                self.assertEqual(check.returncode, 0, check.stderr)
                tracked = fixture / "tracked.txt"
                tracked.write_text("dirty\n")
                with self.assertRaisesRegex(ValueError, "source"):
                    verify_reviewed_source(fixture)
                subprocess.run(("git", "add", "tracked.txt"), cwd=fixture,
                               check=True, capture_output=True)
                tracked.write_text("reviewed\n")
                with self.assertRaisesRegex(ValueError, "source"):
                    verify_reviewed_source(fixture)
                subprocess.run(("git", "reset", "-q", "HEAD", "--", "tracked.txt"),
                               cwd=fixture, check=True, capture_output=True)
                tracked.write_text("reviewed\n")
                (fixture / "untracked.txt").write_text("untracked\n")
                with self.assertRaisesRegex(ValueError, "source"):
                    verify_reviewed_source(fixture)
                (fixture / "untracked.txt").unlink()
                with mock.patch.dict(os.environ, {"GOFLAGS": "-run=OnlyOne"}):
                    with self.assertRaisesRegex(ValueError, "GOFLAGS"):
                        verify_reviewed_source(fixture)
                with mock.patch.dict(os.environ, {"GOWORK": "off"}):
                    with self.assertRaisesRegex(ValueError, "GOWORK"):
                        verify_reviewed_source(fixture)
                goenv = fixture / "goenv"
                goenv.write_text("GOFLAGS=-run=OnlyOne\n")
                with mock.patch.dict(os.environ, {"GOENV": str(goenv)}):
                    with self.assertRaisesRegex(ValueError, "GOFLAGS"):
                        verify_reviewed_source(fixture)
                goenv.unlink()
                (fixture / "go.work").write_text("go 1.26\nuse ./go\n")
                with self.assertRaisesRegex(ValueError, "GOWORK"):
                    verify_reviewed_source(fixture)
                (fixture / "go.work").unlink()
                (fixture / "go/go.work").write_text("go 1.26\nuse .\n")
                with self.assertRaisesRegex(ValueError, "GOWORK"):
                    verify_reviewed_source(fixture)

    def test_ledger_checks_subject_before_import_with_repinned_cli(self) -> None:
        """A matching source digest cannot authorize a vacuous CLI."""
        cli_source = (ROOT / "scripts/lib/replay_tier_enrollment.py").read_text()
        ledger_source = (ROOT / "scripts/lib/verify-live-tests-ledger.py").read_text()
        cli_digest = hashlib.sha256(cli_source.encode()).hexdigest()
        self.assertEqual(ledger_source.count(cli_digest), 1)
        cases = (
            ("raise SystemExit(main())", "raise SystemExit(0)"),
            ('if __name__ == "__main__":',
             'raise SystemExit(0)\n\nif __name__ == "__main__":'),
            ('if __name__ == "__main__":',
             'if False and __name__ == "__main__":'),
            ("    try:\n        verify_reviewed_source(args.root)",
             "    return 0\n    try:\n        verify_reviewed_source(args.root)"),
            ("    try:\n        verify_reviewed_source(args.root)",
             "    sys.exit(0)\n    try:\n        verify_reviewed_source(args.root)"),
        )
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            lib = fixture / "scripts/lib"
            lib.mkdir(parents=True)
            (fixture / "scripts/verify-replay-tier.sh").write_bytes(
                (ROOT / "scripts/verify-replay-tier.sh").read_bytes())
            cli = lib / "replay_tier_enrollment.py"
            ledger = lib / "verify-live-tests-ledger.py"
            env = {**os.environ, "PYTHONPATH": str(ROOT / "scripts/lib")}
            for before, after in cases:
                with self.subTest(after=after.splitlines()[0]):
                    self.assertIn(before, cli_source)
                    changed = cli_source.replace(before, after, 1)
                    cli.write_text(changed)
                    ledger.write_text(ledger_source.replace(
                        cli_digest, hashlib.sha256(changed.encode()).hexdigest()))
                    result = subprocess.run(
                        (sys.executable, str(ledger),
                         str(ROOT / "specs/live-tests.v1.yaml"), str(fixture)),
                        env=env, capture_output=True, text=True, check=False)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn("source integrity", result.stderr)

if __name__ == "__main__":
    unittest.main()
