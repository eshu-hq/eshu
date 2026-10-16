"""Exercise the dedicated contention ledger policy with a seeded promotion."""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
TARGET = "go/internal/query/service_story_target_support_live_test.go"


def promoted_ledger() -> str:
    """Move one real scheduled row while keeping the exemption digest honest."""
    text = subprocess.check_output(
        [
            "python3",
            str(ROOT / "scripts/lib/live_tests_registry.py"),
            "flatten",
            str(ROOT / "specs/live-tests.v1.yaml"),
        ],
        text=True,
    )
    old = (
        f"  - file: {TARGET}\n"
        "    tag: ~\n"
        "    class: scheduled\n"
        "    reason: untagged live proof gated by env Skip; unmeasured cost — promote to blocking with timing evidence\n"
    )
    new = old.replace("class: scheduled", "class: dedicated_ci").replace(
        "timing evidence\n", "timing evidence\n    runner: reducer-contention\n"
    )
    if text.count(old) != 1:
        raise ValueError("fixture target changed")
    text = text.replace(old, new)
    paths = sorted(
        path
        for path, cls in re.findall(
            r"^  - file: (\S+)\n    tag: .*\n    class: (\S+)", text, re.M
        )
        if cls == "scheduled"
    )
    digest = hashlib.sha256(
        "".join(f"{path}\n" for path in paths).encode("utf-8")
    ).hexdigest()
    text = re.sub(r"^  count: \d+$", f"  count: {len(paths)}", text, count=1, flags=re.M)
    text = re.sub(
        r"^  sha256: [0-9a-f]{64}$", f"  sha256: {digest}", text, count=1, flags=re.M
    )
    return text


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
            path.write_text(
                text.replace(
                    "    runner: reducer-contention\n",
                    "    runner: reducer-contention\n    backends: neo4j\n",
                    1,
                )
            )
            result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("PostgreSQL-only", result.stderr)


if __name__ == "__main__":
    unittest.main()
