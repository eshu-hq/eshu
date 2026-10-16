"""Exercise the dedicated contention ledger policy with a seeded promotion."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
TARGET = "go/internal/query/service_story_target_support_live_test.go"


def promoted_ledger() -> str:
    """Read the real promoted ledger for the seeded backend mutation."""
    text = subprocess.check_output(
        [
            "python3",
            str(ROOT / "scripts/lib/live_tests_registry.py"),
            "flatten",
            str(ROOT / "specs/live-tests.v1.yaml"),
        ],
        text=True,
    )
    enrolled = (
        f"  - file: {TARGET}\n"
        "    tag: ~\n"
        "    class: dedicated_ci\n"
    )
    if text.count(enrolled) != 1 or text.count("    runner: reducer-contention\n") < 1:
        raise ValueError("fixture target changed")
    return text


def demoted_ledger(text: str, paths: set[str]) -> str:
    """Demote enrolled rows and re-pin the valid legacy exemption inventory."""
    for path in paths:
        marker = f"  - file: {path}\n"
        start = text.index(marker)
        next_row = text.find("  - file: ", start + len(marker))
        end = len(text) if next_row < 0 else next_row
        row = text[start:end]
        if row.count("    class: dedicated_ci\n") != 1:
            raise ValueError(f"enrolled class changed: {path}")
        if row.count("    runner: reducer-contention\n") != 1:
            raise ValueError(f"enrolled runner changed: {path}")
        row = row.replace("    class: dedicated_ci\n", "    class: scheduled\n")
        row = row.replace("    runner: reducer-contention\n", "")
        text = text[:start] + row + text[end:]
    scheduled = sorted(
        re.findall(r"^  - file: (\S+)\n    tag: .*\n    class: scheduled$", text, re.M)
    )
    digest = hashlib.sha256("".join(f"{path}\n" for path in scheduled).encode()).hexdigest()
    text = re.sub(r"(?m)^  count: \d+$", f"  count: {len(scheduled)}", text, count=1)
    return re.sub(r"(?m)^  sha256: [0-9a-f]{64}$", f"  sha256: {digest}", text, count=1)


class LedgerPolicyTest(unittest.TestCase):
    """Only source-enrolled PostgreSQL contention rows qualify."""

    def test_promoted_row_and_backend_claim(self) -> None:
        text = promoted_ledger()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.yaml"
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(path)}
            script = ROOT / "scripts/verify-live-tests-ledger.sh"
            path.write_text(text)
            result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            marker = f"  - file: {TARGET}\n"
            start = text.index(marker)
            next_row = text.find("  - file: ", start + len(marker))
            end = len(text) if next_row < 0 else next_row
            row = text[start:end]
            self.assertEqual(row.count("    runner: reducer-contention\n"), 1)
            row = row.replace(
                "    runner: reducer-contention\n",
                "    runner: reducer-contention\n    backends: neo4j\n",
            )
            path.write_text(text[:start] + row + text[end:])
            result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("PostgreSQL-only", result.stderr)

    def test_demoted_inventory_rows_fail_with_valid_legacy_metadata(self) -> None:
        """One or all enrolled rows cannot become grandfathered exemptions."""
        text = promoted_ledger()
        inventory = json.loads(
            (ROOT / "scripts/lib/reducer_contention_inventory.json").read_text()
        )
        for paths in ({TARGET}, set(inventory)):
            with self.subTest(demoted=len(paths)), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "ledger.yaml"
                path.write_text(demoted_ledger(text, paths))
                env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(path)}
                result = subprocess.run(
                    [str(ROOT / "scripts/verify-live-tests-ledger.sh")],
                    env=env,
                    capture_output=True,
                    text=True,
                )
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertIn("contention inventory/ledger mismatch", result.stderr)


if __name__ == "__main__":
    unittest.main()
