#!/usr/bin/env python3
"""Exercise the tagged-build workflow's cold-cache lifecycle budget."""

import json
import math
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/static-contract-gates.yml"
# Job 114394988120: full vet sweep passed before cache saving crossed 30m.
OBSERVED_GATE_SECONDS = 1531
MINIMUM_RESERVE_SECONDS = max(300, math.ceil(OBSERVED_GATE_SECONDS / 4))


def load_workflow() -> dict[str, Any]:
    """Read the actual Actions workflow with its production yq parser."""
    return json.loads(subprocess.check_output(
        ["yq", "-o=json", ".", str(WORKFLOW)], text=True
    ))


def matrix_rows(event: str, queue_jobs: list[str]) -> list[dict[str, Any]]:
    """Execute the workflow's matrix builder, without a handwritten mirror."""
    steps = load_workflow()["jobs"]["changes"]["steps"]
    source = next(step["run"] for step in steps if step.get("id") == "gate-matrix")
    source = source.replace("${{ github.event_name }}", event)
    source = re.sub(r"\$\{\{ steps\.filter\.outputs\.\w+ }}", "true", source)
    result = subprocess.check_output(
        ["bash", "-euo", "pipefail", "-c", source], text=True,
        env={**os.environ, "QUEUE_JOBS": json.dumps(queue_jobs),
             "PLAN_GATES": "[]",
             "GITHUB_OUTPUT": "/dev/stdout"},
    )
    line = next(line for line in result.splitlines() if line.startswith("matrix="))
    return json.loads(line.removeprefix("matrix="))["include"]


class TaggedBuildBudgetTest(unittest.TestCase):
    """The lifecycle policy preserves the historical timeline reserve."""

    def test_failed_run_timeline_has_restore_and_cleanup_reserve(self) -> None:
        """The real matrix deadline covers the observed cold gate and reserve."""
        rows = matrix_rows("push", [])
        tagged = next(row for row in rows if row["key"] == "taggedbuilds")
        # Conservative non-vet setup/mirror/teardown allowance; exact retained
        # job phase measurements must remain below this five-minute budget.
        required = OBSERVED_GATE_SECONDS + 120 + 300 + MINIMUM_RESERVE_SECONDS
        self.assertGreaterEqual(tagged["timeout"] * 60, required)
        self.assertEqual(tagged["display"], "Verify tagged-builds gate")
        self.assertEqual(tagged["gate"], "bash scripts/verify-tagged-builds.sh --all")
        self.assertTrue(tagged["run_gate"])

    def test_only_tagged_cache_is_restore_only_and_bounded(self) -> None:
        """Cache transport is optional; all proof steps remain blocking."""
        gate = load_workflow()["jobs"]["gate"]
        self.assertFalse(gate.get("continue-on-error", False))
        steps = gate["steps"]
        restores = [step for step in steps if step.get("id") == "tagged-cache"]
        self.assertEqual(len(restores), 1, "tagged cache needs one bounded restore")
        restore = restores[0]
        self.assertEqual(restore["uses"], "actions/cache/restore@v4")
        self.assertEqual(restore["timeout-minutes"], 2)
        self.assertTrue(restore["continue-on-error"])
        self.assertEqual(restore["if"], "${{ matrix.key == 'taggedbuilds' }}")
        ordinary = [step for step in steps
                    if step.get("uses") == "actions/cache/restore@v4"
                    and step.get("id") != "tagged-cache"]
        self.assertEqual(len(ordinary), 1, "one restore-only ordinary cache")
        self.assertEqual(ordinary[0]["if"], "${{ matrix.key != 'taggedbuilds' }}")
        saves = [step for step in steps
                 if step.get("uses", "").startswith("actions/cache/save")]
        self.assertEqual(len(saves), 1, "exactly one bounded save")
        save = saves[0]
        self.assertIn("matrix.key != 'taggedbuilds'", save["if"])
        self.assertIn("refs/heads/main", save["if"])
        self.assertIn("cache-hit != 'true'", save["if"])
        for step in steps:
            if "Run test mirror" in step.get("name", "") or "Run gate" in step.get("name", ""):
                self.assertFalse(step.get("continue-on-error", False))
        proof = next(step for step in steps if step.get("name", "").startswith("Run gate"))
        self.assertEqual(proof["timeout-minutes"],
                         "${{ matrix.key == 'taggedbuilds' && 35 || matrix.timeout }}")

    def test_registry_and_proof_steps_stay_blocking(self) -> None:
        """Cache failure tolerance cannot leak into a required assertion."""
        registry = json.loads(subprocess.check_output(
            ["yq", "-o=json", ".", str(ROOT / "specs/ci-gates.d/gates-15.yaml")],
            text=True,
        ))
        tagged = next(row for row in registry["gates"] if row["id"] == "tagged-builds")
        self.assertTrue(tagged["blocking"])
        gate = load_workflow()["jobs"]["gate"]
        self.assertFalse(gate["strategy"]["fail-fast"])
        for step in gate["steps"]:
            name = step.get("name", "")
            if name == "Pre-warm Go modules" or name.startswith("Run test mirror"):
                self.assertNotIn("if", step)
                self.assertFalse(step.get("continue-on-error", False))
            if name.startswith("Run gate"):
                self.assertEqual(step["if"], "${{ matrix.run_gate }}")
                self.assertFalse(step.get("continue-on-error", False))

    def test_restore_outcomes_preserve_or_discard_partial_cache(self) -> None:
        """Execute the real diagnostic step on isolated partial-cache fixtures."""
        steps = load_workflow()["jobs"]["gate"]["steps"]
        reports = [step for step in steps
                   if step.get("id") == "tagged-cache-outcome"]
        self.assertEqual(len(reports), 1, "cache fallback needs a visible disposition")
        report = reports[0]
        self.assertIn("!cancelled()", report["if"])
        for outcome, hit in (("success", "true"), ("success", ""),
                             ("failure", ""), ("cancelled", "")):
            with self.subTest(outcome=outcome, hit=hit), tempfile.TemporaryDirectory() as temp:
                cache_dirs = [Path(temp) / "build", Path(temp) / "modules"]
                for directory in cache_dirs:
                    directory.mkdir()
                    (directory / "partial").write_text("fixture", encoding="utf-8")
                    directory.chmod(0o555)
                output = subprocess.check_output(
                    ["bash", "-euo", "pipefail", "-c", report["run"]], text=True,
                    env={**os.environ, "CACHE_OUTCOME": outcome, "CACHE_HIT": hit,
                         "GOCACHE": str(cache_dirs[0]), "GOMODCACHE": str(cache_dirs[1])},
                )
                for directory in cache_dirs:
                    self.assertEqual(directory.exists(), outcome == "success")
                    if directory.exists():
                        directory.chmod(0o755)
                if outcome != "success":
                    self.assertIn("::warning::", output)
                    self.assertIn("full cold proof", output)
                elif hit != "true":
                    self.assertIn("::notice::", output)

    def test_unselected_gate_does_not_schedule(self) -> None:
        """An unselected cell is absent from the matrix, not an idle runner."""
        self.assertEqual(matrix_rows("pull_request", []), [])
        tagged = next(row for row in matrix_rows("push", [])
                      if row["key"] == "taggedbuilds")
        self.assertTrue(tagged["run_gate"])


if __name__ == "__main__":
    unittest.main()
