#!/usr/bin/env python3
"""Reject an executable Nornic guard before importing it into the ledger."""

import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
LIB = ROOT / "scripts/lib"
GUARD = LIB / "nornic_cli_activation_guard.py"
LEDGER = LIB / "verify-live-tests-ledger.py"


class NornicGuardBoundaryTest(unittest.TestCase):
    """The ledger must inspect the helper before Python executes its imports."""

    def test_repinned_helper_cannot_exit_before_ledger_validation(self) -> None:
        source = GUARD.read_text(encoding="utf-8")
        digest = hashlib.sha256(source.encode()).hexdigest()
        checker = LEDGER.read_text(encoding="utf-8")
        self.assertEqual(checker.count(digest), 1)
        for label, changed in (
            ("exit", source + "\nraise SystemExit(0)\n"),
            ("default", source + "\ndef bypass(value=sys.exit(0)):\n    return value\n"),
            ("import", source.replace("import ast\n", "import exit_at_import\nimport ast\n", 1)),
        ):
            with self.subTest(label=label), tempfile.TemporaryDirectory() as directory:
                fixture = Path(directory)
                (fixture / "exit_at_import.py").write_text(
                    "raise SystemExit(0)\n", encoding="utf-8")
                (fixture / GUARD.name).write_text(changed, encoding="utf-8")
                (fixture / LEDGER.name).write_text(
                    checker.replace(digest, hashlib.sha256(changed.encode()).hexdigest()),
                    encoding="utf-8")
                result = subprocess.run(
                    (sys.executable, str(fixture / LEDGER.name),
                     str(ROOT / "specs/live-tests.v1.yaml"), str(ROOT)),
                    env={**os.environ, "PYTHONPATH": str(LIB),
                         "PYTHONDONTWRITEBYTECODE": "1"},
                    capture_output=True, text=True, check=False)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("Nornic guard source integrity", result.stderr)


if __name__ == "__main__":
    unittest.main()
