#!/usr/bin/env python3
"""Seeded RED/GREEN cases for scripts/verify-nornicdb-ci-off.py.

Each case builds a small fixture repository, so the gate is proven against
planted violations and a clean tree, then once against the committed tree.
"""

from __future__ import annotations

import os
import shutil
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

    def build_with_script(self, workflow: str, script: str) -> Path:
        """Write a fixture with one script the workflow invokes."""
        root = self.build(workflow)
        (root / "scripts").mkdir()
        (root / "scripts/verify-replay-tier.sh").write_text(script, encoding="utf-8")
        return root

    def test_live_job_running_a_script_that_docker_runs_nornicdb_fails(self) -> None:
        """RED: the start is in the invoked script, across `\\` continuation lines."""
        root = self.build_with_script(REPLAY_WORKFLOW_LIVE, REPLAY_SCRIPT)
        result = self.run_gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("scripts/verify-replay-tier.sh", result.stderr)

    def test_disabled_job_running_that_script_passes(self) -> None:
        """GREEN: the same script behind `if: ${{ false }}` is not a live leg."""
        workflow = REPLAY_WORKFLOW_LIVE.replace(
            "if: ${{ github.event_name != 'pull_request' }}", "if: ${{ false }}")
        root = self.build_with_script(workflow, REPLAY_SCRIPT)
        registry = REGISTRY_OK.replace('job: "Offline replay vs NornicDB"',
                                       'job: "Offline replay tier"')
        (root / "specs/ci-gates.d/gates-01.yaml").write_text(registry, encoding="utf-8")
        self.assertEqual(self.run_gate(root).returncode, 0)

    def test_live_job_running_a_neo4j_script_passes(self) -> None:
        """GREEN: an invoked script that docker-runs only Neo4j is fine."""
        script = REPLAY_SCRIPT.replace("NORNICDB_NO_AUTH", "NEO4J_AUTH")
        root = self.build_with_script(REPLAY_WORKFLOW_LIVE, script)
        self.assertEqual(self.run_gate(root).returncode, 0)

    def test_commented_docker_run_in_script_does_not_count(self) -> None:
        """GREEN: a commented-out docker run is not a start."""
        script = "#!/usr/bin/env bash\n# docker run -d -e NORNICDB_NO_AUTH=true x\n"
        root = self.build_with_script(REPLAY_WORKFLOW_LIVE, script)
        self.assertEqual(self.run_gate(root).returncode, 0)

    def test_missing_script_is_not_an_error(self) -> None:
        """GREEN: a run line naming a script that does not exist is skipped."""
        root = self.build(REPLAY_WORKFLOW_LIVE)
        self.assertEqual(self.run_gate(root).returncode, 0)

    def test_script_that_brings_up_the_nornicdb_compose_service_fails(self) -> None:
        """RED: the workflow-line `up ... nornicdb` marker also applies to an invoked script."""
        script = "#!/usr/bin/env bash\ndocker compose up -d \\\n  nornicdb postgres\n"
        root = self.build_with_script(REPLAY_WORKFLOW_LIVE, script)
        result = self.run_gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("scripts/verify-replay-tier.sh", result.stderr)

    def test_script_that_calls_a_second_script_is_followed(self) -> None:
        """RED: the start is one script deeper than the one the workflow runs."""
        root = self.build_with_script(
            REPLAY_WORKFLOW_LIVE, "#!/usr/bin/env bash\nbash scripts/lib/inner.sh\n")
        (root / "scripts/lib").mkdir()
        (root / "scripts/lib/inner.sh").write_text(REPLAY_SCRIPT, encoding="utf-8")
        result = self.run_gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("scripts/lib/inner.sh", result.stderr)

    def test_nested_script_that_starts_only_neo4j_passes(self) -> None:
        """GREEN: following a script one level deeper adds no false positive."""
        root = self.build_with_script(
            REPLAY_WORKFLOW_LIVE, "#!/usr/bin/env bash\nbash scripts/lib/inner.sh\n")
        (root / "scripts/lib").mkdir()
        (root / "scripts/lib/inner.sh").write_text(
            REPLAY_SCRIPT.replace("NORNICDB_NO_AUTH", "NEO4J_AUTH"), encoding="utf-8")
        self.assertEqual(self.run_gate(root).returncode, 0)

    def test_matrix_value_nornicdb_under_any_key_fails(self) -> None:
        """RED: the matrix key is not special; a nornicdb value in any matrix is a start."""
        for line in ("backend: [neo4j, nornicdb]", "- backend: nornicdb",
                     "- store: \"nornicdb\""):
            with self.subTest(line=line):
                workflow = OFF_WORKFLOW + f"    strategy2:\n      matrix:\n        {line}\n"
                result = self.run_gate(self.build(workflow))
                self.assertEqual(result.returncode, 1, line)
                self.assertIn("nornicdb", result.stderr)

    def test_matrix_without_nornicdb_passes(self) -> None:
        """GREEN: a matrix on another key with Neo4j only is fine."""
        workflow = OFF_WORKFLOW + "    strategy2:\n      matrix:\n        backend: [neo4j]\n"
        self.assertEqual(self.run_gate(self.build(workflow)).returncode, 0)

    def test_python_script_that_docker_runs_nornicdb_fails(self) -> None:
        """RED: a `.py` script a live job runs is read for a docker run too."""
        workflow = REPLAY_WORKFLOW_LIVE.replace(
            "scripts/verify-replay-tier.sh", "scripts/replay_tier.py")
        root = self.build(workflow)
        (root / "scripts").mkdir()
        (root / "scripts/replay_tier.py").write_text(REPLAY_PY, encoding="utf-8")
        result = self.run_gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("scripts/replay_tier.py", result.stderr)

    def test_python_script_that_docker_runs_neo4j_passes(self) -> None:
        """GREEN: the same Python script against a Neo4j image is not a start."""
        workflow = REPLAY_WORKFLOW_LIVE.replace(
            "scripts/verify-replay-tier.sh", "scripts/replay_tier.py")
        root = self.build(workflow)
        (root / "scripts").mkdir()
        (root / "scripts/replay_tier.py").write_text(
            REPLAY_PY.replace("nornicdb", "neo4j").replace("NORNICDB", "NEO4J"), encoding="utf-8")
        self.assertEqual(self.run_gate(root).returncode, 0)

    def test_committed_tree_is_clean(self) -> None:
        """GREEN: the committed workflows and registry satisfy the gate."""
        result = self.run_gate(ROOT)
        self.assertEqual(result.returncode, 0, result.stderr)

REPLAY_SCRIPT = """\
#!/usr/bin/env bash
set -euo pipefail
log() { printf '%s\\n' "$*"; }
log "starting lean container"
docker run -d --name "replay-$$" \\
\t-p "7474:7474" \\
\t-e NORNICDB_NO_AUTH=true \\
\t"${REPLAY_IMAGE}" >/dev/null
"""

REPLAY_PY = """\
import subprocess

subprocess.run(
    [
        "docker", "run", "-d",
        "-e", "NORNICDB_NO_AUTH=true",
        "ghcr.io/eshu-hq/nornicdb-amd64-cpu:x",
    ],
    check=True,
)
"""

REPLAY_WORKFLOW_LIVE = """\
name: Fixture
on:
  push:
jobs:
  replay:
    if: ${{ github.event_name != 'pull_request' }}
    name: Offline replay tier
    steps:
      - run: bash scripts/verify-replay-tier.sh
"""


class RealTreeMutantTest(unittest.TestCase):
    """Mutate copies of the committed workflows; specs are copied; scripts and go are linked read-only."""

    def mutant(self, workflow: str, old: str, new: str) -> Path:
        """Return a tree whose `workflow` has `old` replaced by `new` exactly once."""
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        shutil.copytree(ROOT / ".github/workflows", root / ".github/workflows")
        shutil.copytree(ROOT / "specs", root / "specs")
        for linked in ("scripts", "go"):
            os.symlink(ROOT / linked, root / linked)
        target = root / ".github/workflows" / workflow
        text = target.read_text(encoding="utf-8")
        self.assertEqual(text.count(old), 1, f"{workflow}: {old!r} must appear once")
        target.write_text(text.replace(old, new), encoding="utf-8")
        return root

    def run_gate(self, root: Path) -> subprocess.CompletedProcess[str]:
        """Run the gate against a repository root."""
        return subprocess.run([sys.executable, str(GATE), "--root", str(root)],
                              text=True, capture_output=True, check=False)

    def test_restored_replay_tier_if_fails(self) -> None:
        """RED (review F1 mutant A): the replay job is live again and docker-runs NornicDB."""
        root = self.mutant("verify-replay-tier.yml", "    if: ${{ false }}\n",
                           "    if: ${{ github.event_name != 'pull_request' }}\n")
        result = self.run_gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("verify-replay-tier.yml/replay-tier", result.stderr)
        self.assertIn("scripts/verify-replay-tier.sh", result.stderr)

    def test_replay_tier_with_the_if_removed_fails(self) -> None:
        """RED: deleting the `if` leaves an unconditional job, which can run."""
        root = self.mutant("verify-replay-tier.yml", "    if: ${{ false }}\n", "")
        self.assertEqual(self.run_gate(root).returncode, 1)

    def test_shards_restored_to_include_nornicdb_fail(self) -> None:
        """RED (review F1 mutant B): matrix shard 1/4 is the whole NornicDB leg."""
        root = self.mutant("live-backend-tests.yml", "shard: [2, 3, 4]",
                           "shard: [1, 2, 3, 4]")
        result = self.run_gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("live-backend-tests.yml/live-backend-shards", result.stderr)
        self.assertIn("shard 1/4", result.stderr)

    def test_only_the_nornicdb_shard_fails(self) -> None:
        """RED: the NornicDB shard alone is enough; the Neo4j shards are fine."""
        root = self.mutant("live-backend-tests.yml", "shard: [2, 3, 4]", "shard: [1]")
        self.assertEqual(self.run_gate(root).returncode, 1)

    def test_e2e_cell_restored_fails(self) -> None:
        """RED: the e2e NornicDB matrix cell comes back."""
        root = self.mutant("e2e-tests.yml", "#   - graph_backend: nornicdb",
                           "- graph_backend: nornicdb")
        self.assertEqual(self.run_gate(root).returncode, 1)

    def test_differential_matrix_restored_fails(self) -> None:
        """RED: the differential capture matrix runs NornicDB again."""
        root = self.mutant("golden-corpus-gate.yml", "graph_backend: [neo4j]\n",
                           "graph_backend: [neo4j, nornicdb]\n")
        self.assertEqual(self.run_gate(root).returncode, 1)

    def test_committed_tree_copy_is_clean(self) -> None:
        """GREEN: an unmutated copy of the committed workflows passes."""
        root = self.mutant("verify-replay-tier.yml", "name: Replay Tier (offline NornicDB)",
                           "name: Replay Tier (offline NornicDB)")
        self.assertEqual(self.run_gate(root).returncode, 0, self.run_gate(root).stderr)


if __name__ == "__main__":
    unittest.main()
