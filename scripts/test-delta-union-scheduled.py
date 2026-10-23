"""Seed route and event violations for the delta union scheduled proof."""

import importlib.util
import io
import os
import re
import subprocess
import tempfile
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts/lib"))


class DeltaUnionScheduledTest(unittest.TestCase):
    """The scheduled route must stay active and select both exact proofs."""

    @classmethod
    def setUpClass(cls) -> None:
        """Load the guard as the workflow uses it."""
        path = ROOT / "scripts/lib/delta_union_scheduled.py"
        spec = importlib.util.spec_from_file_location("delta_union_scheduled", path)
        assert spec and spec.loader
        cls.guard = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.guard)

    def test_source_and_workflow_routes(self) -> None:
        """Source, selection, services, and activation stay tied together."""
        self.guard.verify(ROOT)
        workflow = (ROOT / self.guard.WORKFLOW).read_text()
        changes = {
            "missing_root": workflow.replace("  schedule:\n", "  inactive_schedule:\n", 1),
            "disabled_job": workflow.replace("    runs-on: ubuntu-latest", "    if: false\n    runs-on: ubuntu-latest", 1),
            "disabled_step": workflow.replace("      - name: Run delta union proofs", "      - name: Run delta union proofs\n        if: false", 1),
            "suppressed_failure": workflow.replace("      - name: Run delta union proofs", "      - name: Run delta union proofs\n        continue-on-error: true", 1),
            "bad_postgres_pin": workflow.replace("ci-postgres-bookworm@sha256:", "ci-postgres-alpine@sha256:", 1),
            "bad_neo4j_pin": workflow.replace("neo4j:2026-community@sha256:", "neo4j:latest@sha256:", 1),
            "missing_selector": workflow.replace("TestDeltaUnionBaseMissingRefusesWithoutPartialLive", "TestUnrelatedProof", 1),
            "missing_event_verifier": workflow.replace("--events \"${RUNNER_TEMP}/delta-union.jsonl\"", "--events wrong.jsonl", 1),
        }
        for name, changed in changes.items():
            with self.subTest(name=name):
                with self.assertRaises(ValueError):
                    self.guard.verify(ROOT, changed)

    def test_ledger_demotion_and_false_overlay_enrollment(self) -> None:
        """The ledger rejects drift even if its legacy digest is repinned."""
        source = subprocess.check_output(
            ["python3", str(ROOT / "scripts/lib/live_tests_registry.py"),
             "flatten", str(ROOT / "specs/live-tests.v1.yaml")], text=True)
        target = "go/cmd/projector/delta_union_live_test.go"
        overlay = "go/cmd/projector/superseded_delta_overlay_live_test.go"
        self.assertIn(f"  - file: {target}\n", source)
        self.assertIn(f"  - file: {overlay}\n", source)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.yaml"
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(path)}
            script = ROOT / "scripts/verify-live-tests-ledger.sh"
            path.write_text(source)
            clean = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertEqual(clean.returncode, 0, clean.stderr)
            variants = {
                "stale_digest": source.replace("  count: 386", "  count: 387", 1),
                "false_overlay_row": source.replace(
                    f"  - file: {overlay}\n    tag: ~\n    class: scheduled\n",
                    f"  - file: {overlay}\n    tag: ~\n    class: verified_scheduled\n"
                    "    runner: delta-union-scheduled\n", 1),
                "demoted_union": source.replace(
                    f"  - file: {target}\n    tag: ~\n    class: verified_scheduled\n",
                    f"  - file: {target}\n    tag: ~\n    class: scheduled\n", 1),
            }
            for name, changed in variants.items():
                with self.subTest(name=name):
                    self.assertNotEqual(changed, source)
                    path.write_text(changed)
                    result = subprocess.run([str(script)], env=env,
                                            capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0, result.stderr)

    def test_event_accounting(self) -> None:
        """The shared verifier rejects missing, skipped, and failed tests."""
        from reducer_contention_events import verify_events
        import json
        import tempfile
        package = self.guard.PACKAGE
        names = self.guard.TESTS
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            for wrong in ("missing", "skip", "fail"):
                events = []
                for name in names:
                    if wrong == "missing" and name == names[-1]:
                        continue
                    terminal = wrong if wrong in {"skip", "fail"} and name == names[-1] else "pass"
                    events.extend(({"Package": package, "Test": name, "Action": action}
                                   for action in ("run", terminal)))
                events.append({"Package": package, "Action": "pass"})
                path.write_text("".join(json.dumps(event) + "\n" for event in events))
                self.assertFalse(verify_events(path, package, set(names), io.StringIO()))


if __name__ == "__main__":
    unittest.main()
