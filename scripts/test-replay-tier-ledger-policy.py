#!/usr/bin/env python3
"""Seed replay ledger demotion while keeping the legacy digest consistent."""

from __future__ import annotations

import hashlib
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
VALIDATOR = ROOT / "scripts/lib/verify-live-tests-ledger.py"
MANIFEST = ROOT / "specs/live-tests.v1.yaml"
FRAGMENT = "live-tests-03b.yaml"


class ReplayTierLedgerPolicyTest(unittest.TestCase):
    """Exercise the production ledger validator, including a repinned digest."""

    def test_enrolled_fragment_stays_under_file_cap(self) -> None:
        """Keep the enrollment readable within the repository file limit."""
        fragment = ROOT / "specs/live-tests.d" / FRAGMENT
        self.assertLess(len(fragment.read_text().splitlines()), 500)

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "live-tests.d").mkdir()
        shutil.copy2(MANIFEST, self.root / MANIFEST.name)
        for source in (ROOT / "specs/live-tests.d").glob("*.yaml"):
            shutil.copy2(source, self.root / "live-tests.d" / source.name)

    def validate(self) -> subprocess.CompletedProcess[str]:
        """Call the same file entrypoint as the ledger gate."""
        return subprocess.run(
            ["python3", str(VALIDATOR), str(self.root / MANIFEST.name), str(ROOT)],
            capture_output=True, text=True, check=False,
        )

    def test_enrollment_is_valid_and_single_demotion_fails(self) -> None:
        clean = self.validate()
        self.assertEqual(clean.returncode, 0, clean.stderr + clean.stdout)
        fragment = self.root / "live-tests.d" / FRAGMENT
        text = fragment.read_text()
        text = text.replace(
            "    class: dedicated_ci\n"
            "    reason: exact Go JSON pass in blocking replay-tier NornicDB gate\n"
            "    runner: replay-tier\n"
            "    backends: nornicdb\n",
            "    class: scheduled\n"
            "    reason: intentionally demoted fixture\n", 1,
        )
        fragment.write_text(text)
        self.repin()
        changed = self.validate()
        self.assertNotEqual(changed.returncode, 0, changed.stdout)
        self.assertIn("replay-tier", changed.stderr + changed.stdout)

    def test_all_rows_demoted_with_repinned_digest_fails(self) -> None:
        fragment = self.root / "live-tests.d" / FRAGMENT
        text = fragment.read_text()
        text = text.replace(
            "    class: dedicated_ci\n"
            "    reason: exact Go JSON pass in blocking replay-tier NornicDB gate\n"
            "    runner: replay-tier\n"
            "    backends: nornicdb\n",
            "    class: scheduled\n"
            "    reason: intentionally demoted fixture\n",
        )
        fragment.write_text(text)
        self.repin()
        changed = self.validate()
        self.assertNotEqual(changed.returncode, 0, changed.stdout)
        self.assertIn("replay-tier", changed.stderr + changed.stdout)

    def repin(self) -> None:
        """Recompute the legacy policy so only the replay closure can catch drift."""
        all_rows = "\n".join(path.read_text() for path in
                             (self.root / "live-tests.d").glob("*.yaml"))
        paths = sorted(re.findall(
            r"^  - file: (\S+)\n    tag: .*\n    class: scheduled$", all_rows, re.M
        ))
        digest = hashlib.sha256("".join(f"{path}\n" for path in paths).encode()).hexdigest()
        manifest = self.root / MANIFEST.name
        text = manifest.read_text()
        text = re.sub(r"  count: \d+\n  sha256: [0-9a-f]{64}",
                      f"  count: {len(paths)}\n  sha256: {digest}", text, count=1)
        manifest.write_text(text)


if __name__ == "__main__":
    unittest.main()
