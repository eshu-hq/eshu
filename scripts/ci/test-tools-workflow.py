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


# Workflows that joined the tools image after static-contract-gates.yml.
# The ledger repin checklist is: these 6 container sites + ledger digest +
# test hardcode + per-platform inventories.
NEW_PIN_WORKFLOWS = (
    "sdk-go.yml",
    "tagged-builds.yml",
    "verify-ci-gate-registry.yml",
)

NEW_PIN_JOBS = {
    "sdk-go.yml#sdk-go-collector",
    "sdk-go.yml#sdk-go-apicontract",
    "sdk-go.yml#sdk-go-factschema",
    "sdk-go.yml#go-apilib",
    "tagged-builds.yml#tagged-builds",
    "verify-ci-gate-registry.yml#test-verify",
    "verify-ci-gate-registry.yml#verify",
}


def new_pin_containers() -> dict[str, str]:
    """Map workflow#job to its container pin for the repinned workflows."""
    out = {}
    for name in NEW_PIN_WORKFLOWS:
        path = ROOT / ".github/workflows" / name
        doc = json.loads(
            subprocess.check_output(["yq", "-o=json", ".", str(path)], text=True)
        )
        for job, spec in doc["jobs"].items():
            if "container" in spec:
                out[f"{name}#{job}"] = spec["container"]
    return out


class ToolsWorkflowTest(unittest.TestCase):
    """Pin tools and preserve blocking gate selection semantics."""

    def test_tools_ledger_matches_recipe_and_workflow(self) -> None:
        """The consumed index and recipe have one reviewed identity."""
        ledger = json.loads(LEDGER.read_text(encoding="utf-8"))
        tools = ledger["tools"]
        self.assertEqual(tools["destination"], "ghcr.io/eshu-hq/ci-tools")
        self.assertEqual(
            tools["digest"],
            "sha256:5bd016995aa47cb5c292b99e2354b235fc5f5883882f14fadf31535579beda50",
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

    def test_new_container_pins_match_ledger(self) -> None:
        """The sdk-go/verify repin sites track the ledger tools pin."""
        ledger = json.loads(LEDGER.read_text(encoding="utf-8"))
        tools = ledger["tools"]
        pin = tools["destination"] + "@" + tools["digest"]
        containers = new_pin_containers()
        self.assertEqual(set(containers), NEW_PIN_JOBS)
        for key, container in containers.items():
            self.assertEqual(container, pin, key)

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

    def test_single_serial_product_contract_job(self) -> None:
        """static-contract-gates.yml runs product contracts serially in one job."""
        wf = workflow()
        self.assertEqual(list(wf["jobs"].keys()), ["gate"])
        gate = wf["jobs"]["gate"]
        self.assertEqual(gate["name"], "Product Contract Gates")
        runs = "\n".join(step.get("run", "") for step in gate["steps"])
        for verifier in (
            "scripts/verify-openapi.sh",
            "scripts/verify-edge-source-tool-coverage.sh",
            "scripts/verify-evidence-continuity.sh",
            "scripts/generate-contracttest.sh",
            "scripts/generate-fact-kind-registry.sh",
            "scripts/verify-parser-relationship-kit.sh",
            "scripts/verify-telemetry-coverage.sh",
            "scripts/generate-operator-dashboard.sh",
            "scripts/verify-env-registry-doc.sh",
            "scripts/verify-cassette-author.sh",
        ):
            self.assertIn(verifier, runs)


if __name__ == "__main__":
    unittest.main()
