#!/usr/bin/env python3
"""Run production fan-in shell against good and hostile shard evidence."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/live-backend-tests.yml"


def step_script(name: str) -> str:
    """Extract the production literal run block for the named step."""
    tail = WORKFLOW.read_text().split(f"- name: {name}\n", 1)[1]
    match = re.search(r"        run: \|\n((?:          [^\n]*\n)+)", tail)
    if match is None:
        raise ValueError(f"missing run block: {name}")
    return textwrap.dedent(match.group(1))


class FanInTest(unittest.TestCase):
    """Check aggregate outcomes and real ledger accounting."""

    def test_local_registry_runs_the_same_runner_probes(self) -> None:
        """Registry-selected local proof includes every hosted runner probe."""
        registry = (ROOT / "specs/ci-gates.d/gates-09.yaml").read_text()
        gate = registry.split("  - id: live-backend-static-mirror", 1)[1].split("\n  - id:", 1)[0]
        command = re.search(r'      test_command: "([^"\n]+)"', gate)
        self.assertIsNotNone(command)
        expected = ["scripts/test-verify-live-tests-ledger.sh",
                    "scripts/test-run-live-backend-tests.sh",
                    "scripts/test-live-backend-shards.py",
                    "scripts/test-live-backend-lifecycle.py",
                    "scripts/test-live-backend-workflow.py"]
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            calls = root / "calls"
            for binary in ("bash", "python3"):
                shim = root / binary
                shim.write_text('#!/bin/sh\nprintf "%s\\n" "$1" >>"$PROBE_CALLS"\n')
                shim.chmod(0o755)
            result = subprocess.run(["/bin/bash", "-ec", command.group(1)],
                                    cwd=ROOT, check=False,
                                    env=dict(os.environ, PATH=str(root), PROBE_CALLS=str(calls)))
            self.assertEqual(result.returncode, 0)
            self.assertEqual(calls.read_text().splitlines(), expected)

    def test_every_shard_outcome_blocks_except_success(self) -> None:
        script = step_script("Require selection and every shard to succeed")
        for selection, shard in (("success", "success"), ("failure", "success"),
                                 ("success", "failure"), ("success", "cancelled"),
                                 ("success", "skipped"), ("success", "")):
            with self.subTest(selection=selection, shard=shard):
                result = subprocess.run(["bash", "-e", "-o", "pipefail", "-c", script],
                                        env=dict(os.environ, SELECTION_RESULT=selection,
                                                 SHARD_RESULT=shard), check=False)
                self.assertEqual(result.returncode == 0, selection == shard == "success")

    def test_real_coverage_gate_rejects_missing_duplicate_and_stale_reports(self) -> None:
        script = step_script("Verify complete live-backend coverage")
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            for directory in ("scripts", "specs", "go"):
                (root / directory).symlink_to(ROOT / directory)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), "-c", "user.name=Fixture", "-c",
                            "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "fixture"], check=True)
            head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
            plan = subprocess.check_output(["bash", str(ROOT / "scripts/run-live-backend-tests.sh")],
                                           env=dict(os.environ, ESHU_LIVE_RUNNER_SELFTEST="plan"), text=True)
            plan_path = root / "full.plan"
            plan_path.write_text(plan)
            helper = ROOT / "scripts/lib/live_backend_shards.py"
            for shard in range(1, 5):
                selected = subprocess.check_output(["python3", str(helper), "plan", "--shard", f"{shard}/4"],
                                                   input=plan, text=True).splitlines()
                events = root / f"{shard}.jsonl"
                events.write_text("".join(json.dumps({"run": line, "status": 0, "setup_seconds": 0,
                                                       "test_seconds": 0, "cleanup_seconds": 0}) + "\n" for line in selected))
                report = root / "live-backend-reports" / str(shard) / "live-backend-result.json"
                subprocess.run(["python3", str(helper), "report", "--head", head, "--shard", f"{shard}/4",
                                "--plan", str(plan_path), "--events", str(events), "--output", str(report)],
                               check=True, capture_output=True)
            report = root / "live-backend-reports/4/live-backend-result.json"
            original = report.read_text()
            def invoke() -> subprocess.CompletedProcess[str]:
                return subprocess.run(["bash", "-e", "-o", "pipefail", "-c", script], cwd=root,
                                      capture_output=True, text=True, check=False)
            self.assertEqual(invoke().returncode, 0)
            report.unlink()
            self.assertNotEqual(invoke().returncode, 0)
            report.write_text(original)
            for field, value in (("head", "a" * 40), ("shard", "3/4"), ("runs", []), ("ok", False)):
                with self.subTest(field=field):
                    bad = json.loads(original)
                    bad[field] = value
                    report.write_text(json.dumps(bad))
                    self.assertNotEqual(invoke().returncode, 0)
            report.write_text(original)
            self.assertEqual(invoke().returncode, 0)


if __name__ == "__main__":
    unittest.main()
