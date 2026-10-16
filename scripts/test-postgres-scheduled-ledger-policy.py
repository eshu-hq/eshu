"""Exercise the verified scheduled PostgreSQL ledger policy with planted errors."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
TARGET = "go/internal/storage/postgres/crossplane/ledger_live_test.go"


def flattened_ledger() -> str:
    """Return the exact committed ledger rows with metadata."""
    return subprocess.check_output(
        ["python3", str(ROOT / "scripts/lib/live_tests_registry.py"), "flatten",
         str(ROOT / "specs/live-tests.v1.yaml")], text=True
    )


class LedgerPolicyTest(unittest.TestCase):
    """A verified scheduled row needs exact enrollment and PostgreSQL only."""

    def test_committed_policy_and_seeded_wrong_runner_backend(self) -> None:
        text = flattened_ledger()
        marker = f"  - file: {TARGET}\n"
        self.assertEqual(text.count(marker), 1)
        start = text.index(marker)
        end = text.find("  - file: ", start + len(marker))
        if end < 0:
            end = len(text)
        row = text[start:end]
        self.assertIn("    class: verified_scheduled\n", row)
        self.assertIn("    runner: postgres-scheduled\n", row)
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "ledger.yaml"
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(ledger)}
            script = ROOT / "scripts/verify-live-tests-ledger.sh"
            ledger.write_text(text)
            result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            for changed, error in (
                (
                    row.replace("runner: postgres-scheduled", "runner: missing-runner"),
                    "verified scheduled",
                ),
                (
                    row.replace(
                        "runner: postgres-scheduled\n",
                        "runner: postgres-scheduled\n    backends: neo4j\n",
                    ),
                    "PostgreSQL-only",
                ),
            ):
                ledger.write_text(text[:start] + changed + text[end:])
                result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)


if __name__ == "__main__":
    unittest.main()
