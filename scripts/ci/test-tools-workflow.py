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
            "sha256:05ab9636508dd971fc39f532e5b13ef7088209f89be30172e403c98e9fab1738",
        )
        self.assertEqual(
            tools["recipe_sha256"], hashlib.sha256(RECIPE.read_bytes()).hexdigest()
        )
        self.assertEqual(set(tools["platforms"]), {"linux/amd64", "linux/arm64"})
        self.assertEqual(set(tools["inventories"]), set(tools["platforms"]))

        gate = workflow()["jobs"]["gate"]
        self.assertEqual(
            gate["container"],
            "${{ (github.event_name != 'merge_group' || matrix.queue_selected) && '"
            + tools["destination"] + "@" + tools["digest"] + "' || '' }}",
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
        self.assertIn("actions/cache@v4", uses)
        runs = [step.get("run", "") for step in steps]
        self.assertFalse(any("install-apt-packages.sh" in run for run in runs))

    def test_existing_blocking_cell_runs_image_regressions(self) -> None:
        """New image checks must run inside the unchanged Docker Hub gate cell."""
        source = WORKFLOW.read_text(encoding="utf-8")
        self.assertEqual(source.count('          append_gate "${{ steps.filter.outputs.'), 31)
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
