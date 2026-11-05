#!/usr/bin/env python3
"""Seeded RED/GREEN cases for scripts/verify-nornicdb-ci-off.py.

Each case builds a small fixture repository, so the gate is proven against
planted violations and a clean tree, then once against the committed tree.
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GATE = ROOT / "scripts/verify-nornicdb-ci-off.py"

OFF_WORKFLOW = """\
name: Fixture
on:
  push:
jobs:
  replay:
    # Disabled by owner order.
    if: ${{ false }}
    name: Offline replay vs NornicDB
    steps:
      - run: bash scripts/verify-replay-tier.sh
  e2e:
    name: test (${{ matrix.graph_backend }})
    strategy:
      matrix:
        include:
          #   - graph_backend: nornicdb
          - graph_backend: neo4j
            graph_service: neo4j
    steps:
      - run: docker compose up -d neo4j postgres
"""

REGISTRY_OK = """\
version: v1
gates:
  - id: replay
    tier: manual
    blocking: false
    ci:
      workflow: wf.yml
      job: "Offline replay vs NornicDB"
  - id: e2e
    tier: ci-heavy
    blocking: true
    ci:
      workflow: wf.yml
      job: "test"
"""


class NornicOffGateTest(unittest.TestCase):
    """Plant violations in a fixture tree and run the gate over it."""

    def build(self, workflow: str = OFF_WORKFLOW, registry: str = REGISTRY_OK) -> Path:
        """Write a fixture repository and return its root."""
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        (root / ".github/workflows").mkdir(parents=True)
        (root / "specs/ci-gates.d").mkdir(parents=True)
        (root / ".github/workflows/wf.yml").write_text(workflow, encoding="utf-8")
        (root / "specs/ci-gates.d/gates-01.yaml").write_text(registry, encoding="utf-8")
        return root

    def run_gate(self, root: Path) -> subprocess.CompletedProcess[str]:
        """Run the gate against a repository root."""
        return subprocess.run([sys.executable, str(GATE), "--root", str(root)],
                              text=True, capture_output=True, check=False)

    def test_clean_fixture_passes(self) -> None:
        """GREEN: a disabled job with a manual row and a Neo4j-only live job."""
        result = self.run_gate(self.build())
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_reenabled_matrix_cell_fails(self) -> None:
        """RED: a live job that selects the nornicdb backend is refused."""
        workflow = OFF_WORKFLOW.replace("          #   - graph_backend: nornicdb\n",
                                        "          - graph_backend: nornicdb\n")
        self.assertNotEqual(workflow, OFF_WORKFLOW)
        result = self.run_gate(self.build(workflow))
        self.assertEqual(result.returncode, 1)
        self.assertIn("wf.yml/e2e", result.stderr)
        self.assertIn("nornicdb graph backend", result.stderr)

    def test_other_start_markers_fail(self) -> None:
        """RED: env, compose up, backend flag, and docker run all start NornicDB."""
        for line in ("ESHU_GRAPH_BACKEND: nornicdb",
                     "run: docker compose up -d nornicdb postgres",
                     "run: bash scripts/run-live-backend-tests.sh --backend nornicdb",
                     "run: docker run -d ghcr.io/eshu-hq/nornicdb-amd64-cpu:x",
                     "graph_backend: [nornicdb, neo4j]"):
            with self.subTest(line=line):
                workflow = OFF_WORKFLOW + f"      - {line}\n" if line.startswith("run:") else \
                    OFF_WORKFLOW + f"    env:\n      {line}\n"
                result = self.run_gate(self.build(workflow))
                self.assertEqual(result.returncode, 1, line)

    def test_disabled_job_with_blocking_row_fails(self) -> None:
        """RED: an `if: false` job whose row is still blocking is refused."""
        registry = REGISTRY_OK.replace("    tier: manual\n    blocking: false\n",
                                       "    tier: ci-heavy\n    blocking: true\n", 1)
        self.assertNotEqual(registry, REGISTRY_OK)
        result = self.run_gate(self.build(registry=registry))
        self.assertEqual(result.returncode, 1)
        self.assertIn("registry row replay", result.stderr)
        self.assertIn("permanently disabled", result.stderr)

    def test_disabled_job_with_nonblocking_ci_heavy_row_fails(self) -> None:
        """RED: a ci-heavy tier is a blocking tier even with blocking: false."""
        registry = REGISTRY_OK.replace("    tier: manual\n", "    tier: ci-heavy\n", 1)
        result = self.run_gate(self.build(registry=registry))
        self.assertEqual(result.returncode, 1)

    def test_comments_do_not_count(self) -> None:
        """GREEN: markers that appear only in comments are not active."""
        workflow = textwrap.dedent(OFF_WORKFLOW) + "      # up -d nornicdb\n"
        self.assertEqual(self.run_gate(self.build(workflow)).returncode, 0)

    def test_committed_tree_is_clean(self) -> None:
        """GREEN: the committed workflows and registry satisfy the gate."""
        result = self.run_gate(ROOT)
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
