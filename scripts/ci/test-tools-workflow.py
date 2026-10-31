#!/usr/bin/env python3
"""Check the pinned static-gate tools image and selected-job behavior."""

import hashlib
import json
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/static-contract-gates.yml"
LEDGER = ROOT / "specs/ci-images.v1.json"
RECIPE = ROOT / "scripts/ci/tools.Dockerfile"


def workflow() -> dict[str, object]:
    """Parse the workflow with the same yq CLI available in the tools image."""
    rendered = subprocess.check_output(
        ["yq", "-o=json", ".", str(WORKFLOW)], text=True
    )
    return json.loads(rendered)


class ToolsWorkflowTest(unittest.TestCase):
    """Pin tools and preserve blocking gate selection semantics."""

    def test_tools_ledger_matches_recipe_and_workflow(self) -> None:
        """The consumed index and recipe have one reviewed identity."""
        ledger = json.loads(LEDGER.read_text(encoding="utf-8"))
        tools = ledger["tools"]
        self.assertEqual(tools["destination"], "ghcr.io/eshu-hq/ci-tools")
        self.assertEqual(
            tools["digest"],
            "sha256:2cd4305390057560cd876df8c3350e296b7df0b4b11a3d04e3a7d904d169a776",
        )
        self.assertEqual(
            tools["recipe_sha256"], hashlib.sha256(RECIPE.read_bytes()).hexdigest()
        )
        self.assertEqual(set(tools["platforms"]), {"linux/amd64", "linux/arm64"})
        self.assertEqual(set(tools["inventories"]), set(tools["platforms"]))

        gate = workflow()["jobs"]["gate"]
        self.assertEqual(
            gate["container"],
            tools["destination"] + "@" + tools["digest"],
        )

    def test_selected_jobs_get_tools_and_idle_jobs_do_not(self) -> None:
        """The container expression must select only jobs that run tests."""
        gate = workflow()["jobs"]["gate"]
        self.assertEqual(gate["defaults"]["run"]["shell"], "bash")
        steps = gate["steps"]
        self.assertEqual(gate["env"]["GOTOOLCHAIN"], "local")
        for name in ("GOCACHE", "GOMODCACHE"):
            self.assertTrue(gate["env"][name].startswith("/tmp/"))
        uses = [step.get("uses", "") for step in steps]
        self.assertNotIn("actions/setup-go@v6", uses)
        self.assertTrue(
            "actions/cache@v4" in uses
            or "actions/cache/restore@v4" in uses,
            "gate must wire a cache transport (full or restore-only)",
        )
        runs = [step.get("run", "") for step in steps]
        self.assertFalse(any("install-apt-packages.sh" in run for run in runs))

    def test_existing_blocking_cell_runs_image_regressions(self) -> None:
        """New image checks must run inside the unchanged Docker Hub gate cell."""
        source = WORKFLOW.read_text(encoding="utf-8")
        # 31st cell is workflowscriptcheckout (#8008), not an image check.
        # 32nd cell is the readinessderive drift gate (#7859), not an image check.
        # 34th cell is the collectorcutoverrollback test mirror (#8895), not an image check.
        # 35th cell is the changelogunreleased gate (#9096), not an image check.
        # 36th cell is buildcacheprune (#9632), the GHCR build-cache prune self-test, not an image check.
        self.assertEqual(source.count('          append_gate "${{ steps.filter.outputs.'), 36)
        cell = next(
            line for line in source.splitlines()
            if line.lstrip().startswith('append_gate "${{ steps.filter.outputs.dockerhublogin }}"')
        )
        for script in (
            "scripts/test-ci-image-compose.sh",
            "scripts/ci/test-image-overrides.sh",
            "scripts/ci/test-build-reproducibility-image.sh",
            "scripts/ci/test-mirror-images.sh",
            "scripts/ci/test-tools-workflow.py",
        ):
            self.assertIn(script, cell)
        self.assertIn('"Verify Docker Hub login gate"', cell)
        self.assertIn('"bash scripts/verify-dockerhub-login.sh"', cell)

        changed_paths = workflow()["jobs"]["changes"]["steps"]
        filter_step = next(step for step in changed_paths if step.get("id") == "filter")
        self.assertEqual(
            filter_step["with"]["filters"], ".github/static-contract-filters.yml"
        )
        filters = json.loads(subprocess.check_output(
            ["yq", "-o=json", ".", str(ROOT / ".github/static-contract-filters.yml")],
            text=True,
        ))["dockerhublogin"]
        for path in (
            "scripts/ci/tools.Dockerfile",
            "scripts/ci/test-tools-workflow.py",
            "specs/ci-images.v1.json",
        ):
            self.assertIn(path, filters)


if __name__ == "__main__":
    unittest.main()
