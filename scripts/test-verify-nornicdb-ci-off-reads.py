#!/usr/bin/env python3
"""Seeded RED/GREEN cases for round-6 review of scripts/verify-nornicdb-ci-off.py.

Round-5 review found a top-level env: the guard never read, a step name:
read as a bring-up, an undecodable file that ended in a traceback, and run
spellings the markers missed. Each RED case plants one shape in a fixture tree
and expects exit 1 with a message that names the file; each GREEN twin is the
same construct for Neo4j (or a disabled-only workflow) and passes. What the
guard still does not read is pinned in test-verify-nornicdb-ci-off-forms.py.
"""

from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
_SPEC = importlib.util.spec_from_file_location(
    "nornicdb_ci_off_forms_base", ROOT / "scripts/test-verify-nornicdb-ci-off-forms.py")
_FORMS = importlib.util.module_from_spec(_SPEC)  # type: ignore[arg-type]
_SPEC.loader.exec_module(_FORMS)  # type: ignore[union-attr]
ShapeTest = _FORMS.ShapeTest
workflow = _FORMS.workflow
run_step = _FORMS.run_step
script_job = _FORMS.script_job
COMPOSE_UP = _FORMS.COMPOSE_UP


def top_level(block: str, job: str = "      - run: echo hi\n", head: str = "") -> str:
    """Return a one-job workflow with ``block`` as top-level YAML above ``jobs:``."""
    return f"name: F\non:\n  push:\n{head}{block}jobs:\n  live:\n    steps:\n{job}"


class WorkflowLevelTest(ShapeTest):
    """F1: a top-level ``env:`` or input default applies to every live job."""

    ENV = "env:\n  ESHU_GRAPH_BACKEND: {value}\n"

    def test_workflow_env_backend_fails(self) -> None:
        """RED: the backend is set above ``jobs:``; the message names the file and line."""
        root = self.build(top_level(self.ENV.format(value="nornicdb")))
        self.assert_red(root, "wf.yml", "workflow level", "ESHU_GRAPH_BACKEND: nornicdb")

    def test_workflow_env_neo4j_passes(self) -> None:
        """GREEN: the same top-level env for Neo4j."""
        self.assert_green(self.build(top_level(self.ENV.format(value="neo4j"))))

    def test_workflow_env_with_every_job_off_passes(self) -> None:
        """GREEN: no live job reads the top-level env, so nothing starts."""
        text = ("name: F\non:\n  push:\nenv:\n  ESHU_GRAPH_BACKEND: nornicdb\njobs:\n"
                "  off:\n    if: ${{ false }}\n    steps:\n      - run: echo hi\n")
        self.assert_green(self.build(text))

    def test_workflow_env_with_one_live_job_among_off_jobs_fails(self) -> None:
        """RED: one live job is enough."""
        text = ("name: F\non:\n  push:\nenv:\n  ESHU_GRAPH_BACKEND: nornicdb\njobs:\n"
                "  off:\n    if: ${{ false }}\n    steps:\n      - run: echo hi\n"
                "  live:\n    steps:\n      - run: echo hi\n")
        self.assert_red(self.build(text), "wf.yml", "workflow level")

    def test_workflow_compose_profile_env_fails(self) -> None:
        """RED: any marker the job scan uses also applies to the top-level region."""
        root = self.build(top_level("env:\n  COMPOSE_PROFILES: nornicdb\n"))
        self.assert_red(root, "wf.yml", "compose profile")

    def test_env_after_the_jobs_block_fails(self) -> None:
        """RED: a top-level ``env:`` written below ``jobs:`` is still workflow level."""
        text = top_level("") + "env:\n  ESHU_GRAPH_BACKEND: nornicdb\n"
        self.assert_red(self.build(text), "wf.yml", "workflow level")

    INPUT = ("  workflow_call:\n    inputs:\n      graph_backend:\n        type: string\n"
             "        default: {value}\n")

    def test_workflow_call_input_default_read_by_a_live_job_fails(self) -> None:
        """RED: the input defaults to nornicdb and a live job reads it."""
        job = "      - run: echo ${{ inputs.graph_backend }}\n"
        root = self.build(top_level("", job, self.INPUT.format(value="nornicdb")))
        self.assert_red(root, "wf.yml", "input graph_backend", "default: nornicdb")

    def test_workflow_call_input_default_neo4j_passes(self) -> None:
        """GREEN: the same input defaulting to neo4j."""
        job = "      - run: echo ${{ inputs.graph_backend }}\n"
        self.assert_green(self.build(top_level("", job, self.INPUT.format(value="neo4j"))))

    def test_workflow_call_input_default_nobody_reads_passes(self) -> None:
        """GREEN: no live job reads the input, so its default starts nothing."""
        self.assert_green(self.build(top_level("", head=self.INPUT.format(value="nornicdb"))))


class StepNameTest(ShapeTest):
    """F3: a step ``name:`` is a label, not a command."""

    def test_name_with_up_and_nornicdb_is_not_a_start(self) -> None:
        """GREEN: a teardown step named like a bring-up."""
        step = "      - name: Clean up nornicdb volumes\n        run: docker volume rm x\n"
        self.assert_green(self.build(workflow(step)))

    def test_name_with_docker_run_nornicdb_is_not_a_start(self) -> None:
        """GREEN: a name that reads like ``docker run ... nornicdb``."""
        step = "      - name: Smoke docker run nornicdb image check\n        run: echo hi\n"
        self.assert_green(self.build(workflow(step)))

    def test_real_bring_up_under_such_a_name_still_fails(self) -> None:
        """RED: the run command beside the label is still read."""
        step = f"      - name: Clean up nornicdb volumes\n        run: {COMPOSE_UP} nornicdb\n"
        self.assert_red(self.build(workflow(step)), "wf.yml/live", "compose service")

    def test_name_line_matrix_value_is_still_a_start(self) -> None:
        """RED: ``- name: nornicdb`` is a matrix entry, which the name rule leaves alone."""
        text = workflow("      - run: echo hi\n").replace(
            "    steps:\n", "    strategy:\n      matrix:\n        include:\n"
            "          - name: nornicdb\n    steps:\n")
        self.assert_red(self.build(text), "wf.yml/live", "matrix value")


class UnreadableFileTest(ShapeTest):
    """F4: a file the guard cannot decode fails closed with a message, not a traceback."""

    def assert_cannot_read(self, root: Path, needle: str) -> None:
        """The gate exits 1 with ``cannot read`` and the file name, and no traceback."""
        self.assert_red(root, "cannot read", needle)
        self.assertNotIn("Traceback", self.gate(root).stderr)

    def test_unreadable_workflow_fails_with_a_message(self) -> None:
        """RED: invalid UTF-8 in a workflow."""
        root = self.build(workflow(run_step("echo hi")))
        (root / ".github/workflows/wf.yml").write_bytes(b"name: \xff\xfe\n")
        self.assert_cannot_read(root, "wf.yml")

    def test_unreadable_script_fails_with_a_message(self) -> None:
        """RED: invalid UTF-8 in a script the job runs."""
        root = self.build(*script_job("echo hi"))
        (root / "scripts/s.sh").write_bytes(b"echo \xff\xfe\n")
        self.assert_cannot_read(root, "s.sh")

    def test_unreadable_registry_fragment_fails_with_a_message(self) -> None:
        """RED: invalid UTF-8 in a registry fragment."""
        root = self.build(workflow(run_step("echo hi")))
        (root / "specs/ci-gates.d/gates-01.yaml").write_bytes(b"version: \xff\n")
        self.assert_cannot_read(root, "gates-01.yaml")

    def test_readable_files_pass(self) -> None:
        """GREEN: the same trees with readable files."""
        self.assert_green(self.build(*script_job("echo hi")))


class RunnerSpellingTest(ShapeTest):
    """F2: container runtimes and compose spellings the first markers missed."""

    def test_container_runtime_spellings_fail(self) -> None:
        """RED: ``docker container run`` and ``podman run``, in a workflow and a script."""
        for spelling in ("docker container run", "podman run"):
            with self.subTest(spelling=spelling):
                self.assert_red(self.build(workflow(run_step(f"{spelling} -d nornicdb"))),
                                "wf.yml/live", "NornicDB image")
                self.assert_red(self.build(*script_job(f"{spelling} -d nornicdb")),
                                "scripts/s.sh")
                self.assert_green(self.build(workflow(run_step(f"{spelling} -d neo4j"))))
                self.assert_green(self.build(*script_job(f"{spelling} -d neo4j")))

    def test_docker_url_step_fails(self) -> None:
        """RED: ``uses: docker://...nornicdb...`` runs the image as a step."""
        step = "      - uses: docker://ghcr.io/example/nornicdb:latest\n"
        self.assert_red(self.build(workflow(step)), "wf.yml/live", "NornicDB image")

    def test_docker_url_step_neo4j_passes(self) -> None:
        """GREEN: the same step for Neo4j."""
        self.assert_green(self.build(workflow("      - uses: docker://neo4j:5\n")))

    def test_compose_file_named_for_nornicdb_fails(self) -> None:
        """RED: the ``-f`` file name carries the word, and ``up`` comes after it."""
        up = "docker compose -f compose.nornicdb.yml up -d"
        self.assert_red(self.build(workflow(run_step(up))), "wf.yml/live", "compose")
        self.assert_red(self.build(*script_job(up)), "scripts/s.sh", "compose")

    def test_compose_file_named_for_nornicdb_down_passes(self) -> None:
        """GREEN: ``down -v`` on that file starts nothing; a Neo4j file passes too."""
        down = "docker compose -f compose.nornicdb.yml down -v"
        self.assert_green(self.build(workflow(run_step(down))))
        self.assert_green(self.build(*script_job(down)))
        self.assert_green(self.build(workflow(run_step(
            "docker compose -f compose.neo4j.yml up -d"))))


if __name__ == "__main__":
    unittest.main()
