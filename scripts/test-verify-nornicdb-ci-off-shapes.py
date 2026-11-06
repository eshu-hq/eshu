#!/usr/bin/env python3
"""Seeded RED/GREEN cases for the shapes scripts/verify-nornicdb-ci-off.py must also catch.

Round-3 review of the guard planted shapes that passed it silently: a workflow
``run:`` command split by a ``\\`` continuation, a backend assignment inside a
``run:`` line, a composite action that starts NornicDB, a script that exports
the backend, a three-deep script chain, and a variable service name. Each RED
case plants one shape in a fixture tree and expects exit 1 with a message that
names the job and the file and line. Each GREEN case is the same construct for
Neo4j and must pass. The basic cases live in test-verify-nornicdb-ci-off.py.
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GATE = ROOT / "scripts/verify-nornicdb-ci-off.py"

REGISTRY = """\
version: v1
gates:
  - id: noop
    tier: manual
    blocking: false
    ci:
      workflow: other.yml
      job: "Other"
"""

WORKFLOW_HEAD = """\
name: Fixture
on:
  push:
jobs:
  live:
    name: Live job
    steps:
"""


def workflow(*steps: str, if_line: str = "") -> str:
    """Return a one-job workflow whose steps are the given YAML step bodies."""
    head = WORKFLOW_HEAD
    if if_line:
        head = head.replace("    name: Live job\n", f"    name: Live job\n    {if_line}\n")
    return head + "".join(steps)


def run_step(command: str) -> str:
    """Return a step whose ``run:`` block holds the command lines."""
    body = "".join(f"          {line}\n" for line in command.splitlines())
    return f"      - run: |\n{body}"


class ShapeTest(unittest.TestCase):
    """Plant one construct in a fixture tree and run the gate over it."""

    def build(self, wf: str, files: dict[str, str] | None = None) -> Path:
        """Write a fixture repository; ``files`` maps repo paths to contents."""
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        (root / ".github/workflows").mkdir(parents=True)
        (root / "specs/ci-gates.d").mkdir(parents=True)
        (root / ".github/workflows/wf.yml").write_text(wf, encoding="utf-8")
        (root / "specs/ci-gates.d/gates-01.yaml").write_text(REGISTRY, encoding="utf-8")
        for rel, text in (files or {}).items():
            path = root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding="utf-8")
        return root

    def gate(self, root: Path) -> subprocess.CompletedProcess[str]:
        """Run the gate against a repository root."""
        return subprocess.run([sys.executable, str(GATE), "--root", str(root)],
                              text=True, capture_output=True, check=False)

    def assert_red(self, root: Path, *needles: str) -> None:
        """The gate exits 1 and every needle appears in its output."""
        result = self.gate(root)
        self.assertEqual(result.returncode, 1, result.stderr)
        for needle in needles:
            self.assertIn(needle, result.stderr)

    def assert_green(self, root: Path) -> None:
        """The gate exits 0."""
        result = self.gate(root)
        self.assertEqual(result.returncode, 0, result.stderr)


class ContinuationTest(ShapeTest):
    """Shape 1: a workflow command split across lines by a backslash."""

    def test_compose_up_split_before_the_service_fails(self) -> None:
        """RED: ``up -d \\`` then ``nornicdb`` on the next line."""
        root = self.build(workflow(run_step("docker compose -f x.yml up -d \\\n  nornicdb")))
        self.assert_red(root, "wf.yml/live", "wf.yml:", "nornicdb compose service")

    def test_compose_up_split_before_neo4j_passes(self) -> None:
        """GREEN: the same split command for Neo4j."""
        root = self.build(workflow(run_step("docker compose -f x.yml up -d \\\n  neo4j")))
        self.assert_green(root)

    def test_docker_run_split_fails(self) -> None:
        """RED: ``docker run`` and the NornicDB image on separate physical lines."""
        root = self.build(workflow(run_step("docker run -d \\\n  ghcr.io/x/nornicdb:1")))
        self.assert_red(root, "wf.yml/live", "NornicDB image")

    def test_continuation_hit_is_not_joined_across_a_separate_statement(self) -> None:
        """GREEN: a line with no backslash ends the statement."""
        root = self.build(workflow(run_step("docker compose up -d neo4j\necho nornicdb docs")))
        self.assert_green(root)


class InlineEnvTest(ShapeTest):
    """Shape 2: the backend assigned inside a run command, not in an env: key."""

    def test_inline_env_prefix_fails(self) -> None:
        """RED: ``ESHU_GRAPH_BACKEND=nornicdb go test`` in a run line."""
        root = self.build(workflow(run_step("ESHU_GRAPH_BACKEND=nornicdb go test ./...")))
        self.assert_red(root, "wf.yml/live", "wf.yml:", "ESHU_GRAPH_BACKEND")

    def test_export_in_the_same_block_fails(self) -> None:
        """RED: ``export`` then a command in one run block."""
        root = self.build(workflow(run_step(
            "export ESHU_GRAPH_BACKEND=nornicdb\ngo test ./...")))
        self.assert_red(root, "wf.yml/live", "ESHU_GRAPH_BACKEND")

    def test_quoted_value_fails(self) -> None:
        """RED: the quoted assignment form."""
        root = self.build(workflow(run_step('ESHU_GRAPH_BACKEND="nornicdb" go test ./...')))
        self.assert_red(root, "wf.yml/live")

    def test_inline_env_neo4j_passes(self) -> None:
        """GREEN: the same constructs for Neo4j."""
        root = self.build(workflow(
            run_step("ESHU_GRAPH_BACKEND=neo4j go test ./..."),
            run_step("export ESHU_GRAPH_BACKEND=neo4j\ngo test ./...")))
        self.assert_green(root)

    def test_disabled_job_with_inline_env_passes(self) -> None:
        """GREEN: a job that can never run is not a live leg."""
        root = self.build(workflow(run_step("ESHU_GRAPH_BACKEND=nornicdb go test ./..."),
                                   if_line="if: ${{ false }}"))
        self.assert_green(root)


COMPOSITE_NORNIC = """\
name: Start backend
runs:
  using: composite
  steps:
    - shell: bash
      run: docker compose -f x.yml up -d nornicdb
"""

COMPOSITE_NEO4J = COMPOSITE_NORNIC.replace("nornicdb", "neo4j")
USES_ACTION = "      - uses: ./.github/actions/start\n"


class CompositeActionTest(ShapeTest):
    """Shape 3: a local composite action called from a live job."""

    def test_action_that_starts_nornicdb_fails(self) -> None:
        """RED: the start is in the action, the job only says ``uses``."""
        root = self.build(workflow(USES_ACTION),
                          {".github/actions/start/action.yml": COMPOSITE_NORNIC})
        self.assert_red(root, "wf.yml/live", ".github/actions/start/action.yml:",
                        "nornicdb compose service")

    def test_action_that_starts_neo4j_passes(self) -> None:
        """GREEN: the same action for Neo4j."""
        root = self.build(workflow(USES_ACTION),
                          {".github/actions/start/action.yml": COMPOSITE_NEO4J})
        self.assert_green(root)

    def test_action_called_only_from_a_disabled_job_passes(self) -> None:
        """GREEN: the action is not reached from a live job."""
        root = self.build(workflow(USES_ACTION, if_line="if: ${{ false }}"),
                          {".github/actions/start/action.yml": COMPOSITE_NORNIC})
        self.assert_green(root)

    def test_action_that_sets_the_backend_env_fails(self) -> None:
        """RED: the action's env key selects NornicDB."""
        action = COMPOSITE_NEO4J.replace(
            "      run: docker compose -f x.yml up -d neo4j\n",
            "      env:\n        ESHU_GRAPH_BACKEND: nornicdb\n      run: go test ./...\n")
        root = self.build(workflow(USES_ACTION), {".github/actions/start/action.yml": action})
        self.assert_red(root, "wf.yml/live", "ESHU_GRAPH_BACKEND")

    def test_nested_action_is_followed(self) -> None:
        """RED: job -> action -> action; the inner one starts NornicDB."""
        outer = COMPOSITE_NEO4J.replace(
            "    - shell: bash\n      run: docker compose -f x.yml up -d neo4j\n",
            "    - uses: ./.github/actions/inner\n")
        root = self.build(workflow(USES_ACTION), {
            ".github/actions/start/action.yml": outer,
            ".github/actions/inner/action.yml": COMPOSITE_NORNIC})
        self.assert_red(root, "wf.yml/live", ".github/actions/inner/action.yml:")

    def test_action_cycle_terminates(self) -> None:
        """GREEN: two actions that use each other do not loop."""
        a = COMPOSITE_NEO4J.replace(
            "    - shell: bash\n      run: docker compose -f x.yml up -d neo4j\n",
            "    - uses: ./.github/actions/b\n")
        b = a.replace("actions/b", "actions/start")
        root = self.build(workflow(USES_ACTION), {
            ".github/actions/start/action.yml": a, ".github/actions/b/action.yml": b})
        self.assert_green(root)

    def test_action_that_runs_a_nornicdb_script_fails(self) -> None:
        """RED: the action runs a repo script that starts NornicDB."""
        action = COMPOSITE_NEO4J.replace("docker compose -f x.yml up -d neo4j",
                                         "bash scripts/up.sh")
        root = self.build(workflow(USES_ACTION), {
            ".github/actions/start/action.yml": action,
            "scripts/up.sh": "#!/usr/bin/env bash\ndocker compose up -d nornicdb\n"})
        self.assert_red(root, "wf.yml/live", "scripts/up.sh")

    def test_action_that_runs_its_own_script_fails(self) -> None:
        """RED: the script sits beside the action and is named by ``github.action_path``."""
        action = COMPOSITE_NEO4J.replace(
            "docker compose -f x.yml up -d neo4j",
            'bash "${{ github.action_path }}/up.sh"')
        root = self.build(workflow(USES_ACTION), {
            ".github/actions/start/action.yml": action,
            ".github/actions/start/up.sh": "#!/usr/bin/env bash\ndocker compose up -d nornicdb\n"})
        self.assert_red(root, "wf.yml/live", ".github/actions/start/up.sh")


BACKEND_SCRIPT = "#!/usr/bin/env bash\nset -euo pipefail\nexport ESHU_GRAPH_BACKEND=nornicdb\n"


class ScriptExportTest(ShapeTest):
    """Shape 4: a script a live job runs exports the backend env."""

    def job(self) -> str:
        """A live job that runs scripts/drive.sh."""
        return workflow(run_step("bash scripts/drive.sh"))

    def test_exported_backend_fails(self) -> None:
        """RED: ``export ESHU_GRAPH_BACKEND=nornicdb`` in the script."""
        root = self.build(self.job(), {"scripts/drive.sh": BACKEND_SCRIPT})
        self.assert_red(root, "wf.yml/live", "scripts/drive.sh", "ESHU_GRAPH_BACKEND")

    def test_exported_neo4j_passes(self) -> None:
        """GREEN: the same export for Neo4j."""
        root = self.build(self.job(), {
            "scripts/drive.sh": BACKEND_SCRIPT.replace("nornicdb", "neo4j")})
        self.assert_green(root)

    def test_plain_and_inline_assignments_fail(self) -> None:
        """RED: a bare assignment and an inline prefix before a command."""
        for line in ("ESHU_GRAPH_BACKEND=nornicdb", 'ESHU_GRAPH_BACKEND="nornicdb" go test ./...'):
            with self.subTest(line=line):
                root = self.build(self.job(), {"scripts/drive.sh": f"#!/usr/bin/env bash\n{line}\n"})
                self.assert_red(root, "scripts/drive.sh")

    def test_quoted_mention_does_not_count(self) -> None:
        """GREEN: a statement that only quotes the assignment is not one."""
        script = '#!/usr/bin/env bash\necho "ESHU_GRAPH_BACKEND=nornicdb is off"\n'
        root = self.build(self.job(), {"scripts/drive.sh": script})
        self.assert_green(root)

    def test_export_in_a_script_run_by_a_script_fails(self) -> None:
        """RED: the export sits one script deeper."""
        root = self.build(self.job(), {
            "scripts/drive.sh": "#!/usr/bin/env bash\nbash scripts/lib/inner.sh\n",
            "scripts/lib/inner.sh": BACKEND_SCRIPT})
        self.assert_red(root, "wf.yml/live", "scripts/lib/inner.sh")


class DeepChainTest(ShapeTest):
    """Shape 5: a script chain deeper than two."""

    def chain(self, depth: int, leaf: str) -> dict[str, str]:
        """Scripts s1 -> s2 -> ... -> s<depth>, the last one holding the leaf command."""
        files = {}
        for index in range(1, depth):
            files[f"scripts/s{index}.sh"] = f"#!/usr/bin/env bash\nbash scripts/s{index + 1}.sh\n"
        files[f"scripts/s{depth}.sh"] = f"#!/usr/bin/env bash\n{leaf}\n"
        return files

    def test_three_deep_chain_fails(self) -> None:
        """RED: workflow -> s1 -> s2 -> s3, and s3 brings up NornicDB."""
        root = self.build(workflow(run_step("bash scripts/s1.sh")),
                          self.chain(3, "docker compose up -d nornicdb"))
        self.assert_red(root, "wf.yml/live", "scripts/s3.sh")

    def test_three_deep_chain_for_neo4j_passes(self) -> None:
        """GREEN: the same chain for Neo4j."""
        root = self.build(workflow(run_step("bash scripts/s1.sh")),
                          self.chain(3, "docker compose up -d neo4j"))
        self.assert_green(root)

    def test_script_cycle_terminates(self) -> None:
        """GREEN: two scripts that run each other do not loop."""
        root = self.build(workflow(run_step("bash scripts/s1.sh")), {
            "scripts/s1.sh": "#!/usr/bin/env bash\nbash scripts/s2.sh\n",
            "scripts/s2.sh": "#!/usr/bin/env bash\nbash scripts/s1.sh\n"})
        self.assert_green(root)


class VariableServiceTest(ShapeTest):
    """Shape 6: the compose service named by a shell variable set to a literal."""

    def test_script_variable_service_fails(self) -> None:
        """RED: ``SVC=nornicdb`` then ``up -d "${SVC}"`` in a script."""
        script = '#!/usr/bin/env bash\nBACKEND=nornicdb\ndocker compose up -d "${BACKEND}"\n'
        root = self.build(workflow(run_step("bash scripts/drive.sh")), {"scripts/drive.sh": script})
        self.assert_red(root, "wf.yml/live", "scripts/drive.sh", "nornicdb compose service")

    def test_script_variable_service_for_neo4j_passes(self) -> None:
        """GREEN: the same shape for Neo4j."""
        script = '#!/usr/bin/env bash\nBACKEND=neo4j\ndocker compose up -d "$BACKEND"\n'
        root = self.build(workflow(run_step("bash scripts/drive.sh")), {"scripts/drive.sh": script})
        self.assert_green(root)

    def test_workflow_variable_service_fails(self) -> None:
        """RED: the same shape inside one workflow run block."""
        root = self.build(workflow(run_step('svc=nornicdb\ndocker compose up -d "$svc"')))
        self.assert_red(root, "wf.yml/live", "nornicdb compose service")

    def test_variable_set_in_another_step_is_not_carried(self) -> None:
        """GREEN: each workflow step is its own shell, so the literal does not carry over."""
        root = self.build(workflow(run_step("svc=nornicdb\necho ok"),
                                   run_step('docker compose up -d "$svc"')))
        self.assert_green(root)

    def test_variable_that_is_not_a_literal_is_a_stated_limit(self) -> None:
        """GREEN (stated limit): a value computed at run time is not resolved."""
        script = '#!/usr/bin/env bash\nBACKEND="$(pick)"\ndocker compose up -d "${BACKEND}"\n'
        root = self.build(workflow(run_step("bash scripts/drive.sh")), {"scripts/drive.sh": script})
        self.assert_green(root)


if __name__ == "__main__":
    unittest.main()
