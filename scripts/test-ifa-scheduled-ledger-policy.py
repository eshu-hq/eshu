"""Seed Ifá ledger demotion and backend errors against the real validator."""

import hashlib
import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
TARGET = "go/internal/replay/offlinetier/repo_dependency_ifa_cleanup_live_test.go"
INVENTORY = ROOT / "scripts/lib/ifa_scheduled_inventory.json"


def flattened_ledger() -> str:
    """Return source ledger rows with current metadata."""
    return subprocess.check_output(
        ["python3", str(ROOT / "scripts/lib/live_tests_registry.py"), "flatten",
         str(ROOT / "specs/live-tests.v1.yaml")], text=True
    )


class IfaLedgerPolicyTest(unittest.TestCase):
    """A named Ifá runner must own every inventory row."""

    def test_backend_runner_and_demoted_row(self) -> None:
        text = flattened_ledger()
        marker = f"  - file: {TARGET}\n"
        start = text.index(marker)
        end = text.find("  - file: ", start + len(marker))
        if end < 0:
            end = len(text)
        row = text[start:end]
        self.assertIn("    class: verified_scheduled\n", row)
        self.assertIn("    runner: ifa-scheduled\n", row)
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "ledger.yaml"
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(ledger)}
            script = ROOT / "scripts/verify-live-tests-ledger.sh"
            ledger.write_text(text)
            clean = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertEqual(clean.returncode, 0, clean.stderr)
            for mutated in (
                row.replace("runner: ifa-scheduled", "runner: wrong-runner"),
                row.replace("backends: nornicdb", "backends: neo4j"),
                row.replace("tag: ifarepodependencyproof", "tag: wrong-tag"),
            ):
                ledger.write_text(text[:start] + mutated + text[end:])
                result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0, mutated)
            demoted = text[:start] + row.replace(
                "    class: verified_scheduled\n", "    class: scheduled\n"
            ).replace("    runner: ifa-scheduled\n", "") + text[end:]
            paths = re.findall(
                r"^  - file: (\S+)\n    tag: .*\n    class: scheduled$",
                demoted, re.MULTILINE,
            )
            digest = hashlib.sha256(
                "".join(f"{path}\n" for path in sorted(paths)).encode()
            ).hexdigest()
            demoted = re.sub(
                r"(?m)^  count: [0-9]+\n  sha256: [0-9a-f]{64}$",
                f"  count: {len(paths)}\n  sha256: {digest}",
                demoted,
            )
            ledger.write_text(demoted)
            result = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Ifá scheduled rows differ", result.stderr)

    def test_exact_inventory_survives_legacy_digest_repin(self) -> None:
        """Removing every enrolled Ifá row cannot become a legacy exemption."""
        source = flattened_ledger()
        inventory = json.loads(INVENTORY.read_text())
        self.assertEqual(len(inventory), 7)
        rows = re.findall(
            r"(?ms)^  - file: (\S+)\n.*?(?=^  - file: |\Z)", source
        )
        self.assertTrue(set(inventory).issubset(rows))
        demoted = source
        for path in inventory:
            marker = f"  - file: {path}\n"
            start = demoted.index(marker)
            end = demoted.find("  - file: ", start + len(marker))
            if end < 0:
                end = len(demoted)
            row = demoted[start:end]
            row = row.replace("    class: verified_scheduled\n", "    class: scheduled\n")
            row = row.replace("    runner: ifa-scheduled\n", "")
            demoted = demoted[:start] + row + demoted[end:]
        paths = re.findall(r"^  - file: (\S+)\n    tag: .*\n    class: scheduled$", demoted, re.M)
        digest = hashlib.sha256("".join(f"{path}\n" for path in sorted(paths)).encode()).hexdigest()
        demoted = re.sub(
            r"(?m)^  count: [0-9]+\n  sha256: [0-9a-f]{64}$",
            f"  count: {len(paths)}\n  sha256: {digest}",
            demoted,
        )
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "ledger.yaml"
            ledger.write_text(demoted)
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(ledger)}
            result = subprocess.run(
                [str(ROOT / "scripts/verify-live-tests-ledger.sh")],
                env=env, capture_output=True, text=True, check=False,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Ifá scheduled rows differ", result.stderr)

    def test_missing_extra_and_unknown_postgres_runner(self) -> None:
        """Only the exact seven files may use this verified class."""
        source = flattened_ledger()
        inventory = json.loads(INVENTORY.read_text())
        target = sorted(inventory)[0]
        marker = f"  - file: {target}\n"
        start = source.index(marker)
        end = source.find("  - file: ", start + len(marker))
        self.assertGreater(end, start)
        row = source[start:end]
        foreign = "go/cmd/reducer/neo4j_write_timeout_live_test.go"
        foreign_marker = f"  - file: {foreign}\n"
        self.assertTrue(
            foreign_marker in source, f"missing foreign fixture row: {foreign}"
        )
        foreign_start = source.index(foreign_marker)
        foreign_end = source.find("  - file: ", foreign_start + len(foreign_marker))
        self.assertGreater(foreign_end, foreign_start)
        foreign_row = source[foreign_start:foreign_end]
        postgres_path = "go/internal/query/content_reader_entity_search_plan_live_test.go"
        postgres_marker = f"  - file: {postgres_path}\n"
        self.assertTrue(
            postgres_marker in source, f"missing PostgreSQL fixture row: {postgres_path}"
        )
        postgres_start = source.index(postgres_marker)
        postgres_end = source.find("  - file: ", postgres_start + len(postgres_marker))
        self.assertGreater(postgres_end, postgres_start)
        postgres_row = source[postgres_start:postgres_end]
        self.assertIn("    tag: ~\n", postgres_row)
        self.assertNotIn("    backends:", postgres_row)
        variants = {
            "missing": source[:start] + source[end:],
            "unknown_postgres_runner": source[:start] + row.replace(
                "runner: ifa-scheduled", "runner: postgres-scheduled"
            ) + source[end:],
            "unknown_postgres_row": source[:postgres_start] + postgres_row.replace(
                "    class: scheduled\n", "    class: verified_scheduled\n"
            ).rstrip("\n") + "\n    runner: postgres-scheduled\n" + source[postgres_end:],
            "extra": source[:foreign_start] + foreign_row.replace(
                "    class: scheduled\n", "    class: verified_scheduled\n"
            ).replace(
                "    backends: neo4j\n", "    runner: ifa-scheduled\n    backends: nornicdb\n"
            ) + source[foreign_end:],
        }
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "ledger.yaml"
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(ledger)}
            for name, mutated in variants.items():
                with self.subTest(name=name):
                    ledger.write_text(mutated)
                    result = subprocess.run(
                        [str(ROOT / "scripts/verify-live-tests-ledger.sh")],
                        env=env, capture_output=True, text=True, check=False,
                    )
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("Ifá scheduled", result.stderr)


if __name__ == "__main__":
    unittest.main()
