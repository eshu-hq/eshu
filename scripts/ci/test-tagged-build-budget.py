#!/usr/bin/env python3
"""Exercise the nightly tagged-build workflow's cold-cache lifecycle budget."""

import json
import math
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/tagged-builds.yml"
# Job 114394988120: full vet sweep passed before cache saving crossed 30m.
OBSERVED_GATE_SECONDS = 1531
MINIMUM_RESERVE_SECONDS = max(300, math.ceil(OBSERVED_GATE_SECONDS / 4))


def load_workflow() -> dict[str, Any]:
    """Read the actual Actions workflow with its production yq parser."""
    return json.loads(subprocess.check_output(
        ["yq", "-o=json", ".", str(WORKFLOW)], text=True
    ))


class TaggedBuildBudgetTest(unittest.TestCase):
    """The nightly workflow preserves the historical timeline reserve."""

    def test_failed_run_timeline_has_restore_and_cleanup_reserve(self) -> None:
        """The real job deadline covers the observed cold gate and reserve."""
        wf = load_workflow()
        self.assertIn("schedule", wf["on"])
        self.assertIn("workflow_dispatch", wf["on"])
        self.assertNotIn("pull_request", wf["on"])
        self.assertNotIn("push", wf["on"])
        job = wf["jobs"]["tagged-builds"]
        required = OBSERVED_GATE_SECONDS + 120 + 300 + MINIMUM_RESERVE_SECONDS
        self.assertGreaterEqual(job["timeout-minutes"] * 60, required)
        sweep = next(
            step for step in job["steps"]
            if step.get("name") == "Run tagged-builds sweep"
        )
        self.assertEqual(sweep["run"], "bash scripts/verify-tagged-builds.sh --all")
        self.assertEqual(sweep["timeout-minutes"], 35)

    def test_tagged_cache_is_restore_only_and_bounded(self) -> None:
        """Cache transport is optional; proof steps remain blocking."""
        job = load_workflow()["jobs"]["tagged-builds"]
        self.assertFalse(job.get("continue-on-error", False))
        steps = job["steps"]
        restores = [step for step in steps if step.get("id") == "tagged-cache"]
        self.assertEqual(len(restores), 1, "tagged cache needs one bounded restore")
        restore = restores[0]
        self.assertEqual(restore["uses"], "actions/cache/restore@v4")
        self.assertEqual(restore["timeout-minutes"], 2)
        self.assertTrue(restore["continue-on-error"])
        saves = [step for step in steps
                 if step.get("uses", "").startswith("actions/cache/save")]
        self.assertEqual(len(saves), 0, "nightly tagged-builds must not save cache")
        for step in steps:
            if step.get("name") in ("Pre-warm Go modules", "Run tagged-builds sweep"):
                self.assertNotIn("if", step)
                self.assertFalse(step.get("continue-on-error", False))

    def test_registry_declares_nightly_workflow(self) -> None:
        """specs/ci-gates.v1.yaml tracks tagged-builds.yml as a scheduled non-gate workflow."""
        registry = json.loads(subprocess.check_output(
            ["yq", "-o=json", ".", str(ROOT / "specs/ci-gates.v1.yaml")],
            text=True,
        ))
        files = {row["file"] for row in registry["non_gate_workflows"]}
        self.assertIn("tagged-builds.yml", files)

    def test_restore_outcomes_preserve_or_discard_partial_cache(self) -> None:
        """Execute the real diagnostic step on isolated partial-cache fixtures."""
        steps = load_workflow()["jobs"]["tagged-builds"]["steps"]
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


if __name__ == "__main__":
    unittest.main()
