#!/usr/bin/env python3
"""Seeded RED/GREEN cases for the workflow-file and shell forms the guard must read.

Round-4 review planted shapes the guard passed silently: a ``.yaml`` workflow, a
workflow indented four spaces, a job ``services:`` or ``container:`` image, and
shell forms in the scripts a job runs (``cd x && docker ...``, ``sudo``,
``if ...; then``, ``timeout N``, ``bash -c '...'``). Each RED case plants one
shape and expects exit 1 with a message that names the job and the file. Each
GREEN twin is the same construct for Neo4j and passes. The last class pins what
the guard still does not read, so closing a limit means changing that test and
the docs in the same edit. The cases for earlier rounds live in
test-verify-nornicdb-ci-off-shapes.py.
"""

from __future__ import annotations

import importlib.util
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
_SPEC = importlib.util.spec_from_file_location(
    "nornicdb_ci_off_shapes_base", ROOT / "scripts/test-verify-nornicdb-ci-off-shapes.py")
_SHELL_SPEC = importlib.util.spec_from_file_location(
    "nornicdb_ci_off_shell", ROOT / "scripts/lib/nornicdb_ci_off_shell.py")
_BASE = importlib.util.module_from_spec(_SPEC)  # type: ignore[arg-type]
_SPEC.loader.exec_module(_BASE)  # type: ignore[union-attr]
ShapeTest = _BASE.ShapeTest
workflow = _BASE.workflow
run_step = _BASE.run_step

COMPOSE_UP = "docker compose -f x.yml up -d"


def script_job(command: str) -> tuple[str, dict[str, str]]:
    """Return a workflow that runs scripts/s.sh and that script's body."""
    return (workflow(run_step("bash scripts/s.sh")),
            {"scripts/s.sh": f"#!/usr/bin/env bash\n{command}\n"})


class WorkflowFileTest(ShapeTest):
    """A workflow the guard must open: extension, indentation, inline jobs."""

    def test_yaml_extension_is_read(self) -> None:
        """RED: GitHub runs ``x.yaml`` as well as ``x.yml``."""
        root = self.build(workflow(run_step("docker run -d nornicdb")), name="x.yaml")
        self.assert_red(root, "x.yaml/live", "x.yaml:")

    def test_yaml_extension_neo4j_passes(self) -> None:
        """GREEN: the same ``.yaml`` job for Neo4j."""
        self.assert_green(self.build(workflow(run_step("docker run -d neo4j")), name="x.yaml"))

    def test_job_keys_indented_four_spaces_are_read(self) -> None:
        """RED: the job keys sit four spaces in, its own keys eight."""
        text = ("name: F\non:\n  push:\njobs:\n    live:\n        name: Live\n"
                "        steps:\n          - run: docker run -d nornicdb\n")
        self.assert_red(self.build(text), "wf.yml/live")

    def test_whole_file_indented_is_read(self) -> None:
        """RED: every top-level key is indented two spaces."""
        text = ("  name: F\n  jobs:\n    live:\n      steps:\n"
                "        - run: docker run -d nornicdb\n")
        self.assert_red(self.build(text), "wf.yml/live")

    def test_four_space_neo4j_passes(self) -> None:
        """GREEN: the four-space workflow for Neo4j."""
        text = ("name: F\njobs:\n    live:\n        steps:\n"
                "          - run: docker run -d neo4j\n")
        self.assert_green(self.build(text))

    def test_four_space_disabled_job_is_honoured(self) -> None:
        """GREEN: a job-level ``if: false`` is found at the file's own indentation."""
        text = ("name: F\njobs:\n    off:\n        if: ${{ false }}\n        steps:\n"
                "          - run: docker run -d nornicdb\n")
        self.assert_green(self.build(text))

    def test_quoted_job_id_is_read(self) -> None:
        """RED: ``"live":`` is a job id."""
        text = 'name: F\njobs:\n  "live":\n    steps:\n      - run: docker run -d nornicdb\n'
        self.assert_red(self.build(text), "wf.yml/live")

    def test_inline_job_with_a_backend_is_read(self) -> None:
        """RED: a flow-style reusable-workflow call that passes ``graph_backend: nornicdb``."""
        text = ("name: F\njobs:\n  call: {uses: ./.github/workflows/x.yml, "
                "with: {graph_backend: nornicdb}}\n")
        self.assert_red(self.build(text), "wf.yml/call")

    def test_inline_reusable_call_for_neo4j_passes(self) -> None:
        """GREEN: the same inline caller for Neo4j."""
        text = ("name: F\njobs:\n  call: {uses: ./.github/workflows/x.yml, "
                "with: {graph_backend: neo4j}}\n")
        self.assert_green(self.build(text))

    def test_inline_job_without_uses_is_refused(self) -> None:
        """RED: an inline job with steps cannot be read, so the guard says so."""
        text = "name: F\njobs:\n  live: {runs-on: x, steps: [{run: docker run -d neo4j}]}\n"
        self.assert_red(self.build(text), "wf.yml/live", "written inline")

    def test_workflow_without_a_block_jobs_mapping_is_refused(self) -> None:
        """RED: ``jobs: {..}`` hides every job from the line reader."""
        text = "name: F\njobs: {live: {steps: [{run: echo hi}]}}\n"
        self.assert_red(self.build(text), "wf.yml", "no block-style `jobs:`")


class JobImageTest(ShapeTest):
    """GitHub starts a job's ``services:`` and ``container:`` images with no run line."""

    def job(self, body: str) -> str:
        """Return a workflow whose live job has the block ``body`` before its steps."""
        return workflow("") .replace("    steps:\n", body + "    steps:\n") + \
            "      - run: echo hi\n"

    def test_service_image_fails(self) -> None:
        """RED: ``services.graph.image`` is a NornicDB image."""
        body = "    services:\n      graph:\n        image: ghcr.io/example/nornicdb:latest\n"
        self.assert_red(self.build(self.job(body)), "wf.yml/live", "job service or container")

    def test_service_image_neo4j_passes(self) -> None:
        """GREEN: the Neo4j service image."""
        body = "    services:\n      graph:\n        image: neo4j:5\n"
        self.assert_green(self.build(self.job(body)))

    def test_container_image_key_fails(self) -> None:
        """RED: ``container.image`` is a NornicDB image."""
        body = "    container:\n      image: ghcr.io/example/nornicdb:latest\n"
        self.assert_red(self.build(self.job(body)), "wf.yml/live", "job service or container")

    def test_container_string_fails(self) -> None:
        """RED: ``container: <image>`` is a NornicDB image."""
        body = "    container: ghcr.io/example/nornicdb:latest\n"
        self.assert_red(self.build(self.job(body)), "wf.yml/live")

    def test_container_neo4j_passes(self) -> None:
        """GREEN: both container spellings for Neo4j."""
        self.assert_green(self.build(self.job("    container: neo4j:5\n")))
        self.assert_green(self.build(self.job("    container:\n      image: neo4j:5\n")))

    def test_image_outside_a_service_block_is_not_a_start(self) -> None:
        """GREEN: an ``image:`` key in a later step input is not a job image."""
        text = workflow("      - uses: x/y@v1\n        with:\n          image: nornicdb-docs\n")
        self.assert_green(self.build(text))


class SelectionTest(ShapeTest):
    """Ways to select the backend that the exact-text markers missed."""

    def test_profile_up_fails(self) -> None:
        """RED: ``--profile nornicdb up`` puts the profile before ``up``."""
        root = self.build(workflow(run_step("docker compose --profile nornicdb up -d")))
        self.assert_red(root, "wf.yml/live", "compose profile")

    def test_profile_neo4j_and_down_pass(self) -> None:
        """GREEN: a Neo4j profile, and a ``down`` that starts nothing."""
        self.assert_green(self.build(workflow(run_step("docker compose --profile neo4j up -d"))))
        self.assert_green(self.build(workflow(run_step("docker compose --profile nornicdb down"))))

    def test_compose_profiles_env_fails(self) -> None:
        """RED: ``COMPOSE_PROFILES=nornicdb`` in a run line and in an env key."""
        self.assert_red(self.build(workflow(run_step("COMPOSE_PROFILES=nornicdb docker compose up"))),
                        "wf.yml/live", "compose profile")
        env = "    env:\n      COMPOSE_PROFILES: nornicdb\n"
        text = workflow(run_step("echo hi")).replace("    steps:\n", env + "    steps:\n")
        self.assert_red(self.build(text), "wf.yml/live")

    def test_compose_profiles_neo4j_passes(self) -> None:
        """GREEN: the same variable for Neo4j."""
        self.assert_green(self.build(workflow(run_step("COMPOSE_PROFILES=neo4j docker compose up"))))

    def test_backend_expression_fails(self) -> None:
        """RED: an expression that can pick nornicdb, on an env key and a matrix key."""
        env = ("    env:\n      ESHU_GRAPH_BACKEND: ${{ github.event_name == 'push' "
               "&& 'nornicdb' || 'neo4j' }}\n")
        text = workflow(run_step("echo hi")).replace("    steps:\n", env + "    steps:\n")
        self.assert_red(self.build(text), "wf.yml/live")
        matrix = ("    strategy:\n      matrix:\n        backend: "
                  "${{ fromJSON('[\"nornicdb\"]') }}\n")
        text = workflow(run_step("echo hi")).replace("    steps:\n", matrix + "    steps:\n")
        self.assert_red(self.build(text), "wf.yml/live", "through an expression")

    def test_backend_expression_for_neo4j_passes(self) -> None:
        """GREEN: an expression that only names neo4j."""
        env = "    env:\n      ESHU_GRAPH_BACKEND: ${{ github.event_name == 'push' && 'neo4j' }}\n"
        text = workflow(run_step("echo hi")).replace("    steps:\n", env + "    steps:\n")
        self.assert_green(self.build(text))

    def test_names_are_matched_ignoring_case(self) -> None:
        """RED: ``NornicDB`` in a workflow line, a backend value, and a script."""
        self.assert_red(self.build(workflow(run_step("docker run -d ghcr.io/x/NornicDB:1"))),
                        "wf.yml/live")
        self.assert_red(self.build(workflow(run_step("ESHU_GRAPH_BACKEND=NornicDB go test"))),
                        "wf.yml/live")
        self.assert_red(self.build(*script_job("docker run -d ghcr.io/x/NornicDB:1")),
                        "scripts/s.sh")


class ShellFormTest(ShapeTest):
    """Ordinary shell forms in a script the job runs; each has a Neo4j twin."""

    FORMS = {
        "cd_and": "cd /tmp && {up}",
        "sudo": "sudo {up}",
        "sudo_with_user": "sudo -E -u root {up}",
        "if_then": "if true; then {up}; fi",
        "while_do": "while true; do {up}; done",
        "timeout": "timeout 60 {up}",
        "timeout_flags": "timeout -k 5 --foreground 60s {up}",
        "env_wrapper": "env FOO=bar {up}",
        "nohup": "nohup {up}",
        "piped": "echo y | {up}",
        "subshell": "( {up} )",
        "command_substitution": "out=$({up})",
        "bash_c": "bash -c '{up}'",
        "sh_lc": 'sh -lc "{up}"',
        "eval": "eval '{up}'",
        "nested": "sudo bash -c 'cd /tmp && {up}'",
        "or_list": "false || {up}",
        "negated": "! {up}",
    }

    def test_every_form_fails_for_nornicdb(self) -> None:
        """RED: the start behind each wrapper is found and names the script."""
        for name, form in self.FORMS.items():
            with self.subTest(form=name):
                root = self.build(*script_job(form.format(up=f"{COMPOSE_UP} nornicdb")))
                self.assert_red(root, "wf.yml/live", "scripts/s.sh")

    def test_every_form_passes_for_neo4j(self) -> None:
        """GREEN: the same wrapper around a Neo4j start."""
        for name, form in self.FORMS.items():
            with self.subTest(form=name):
                self.assert_green(self.build(*script_job(form.format(up=f"{COMPOSE_UP} neo4j"))))

    def test_docker_run_behind_wrappers_fails(self) -> None:
        """RED: ``docker run`` behind ``sudo`` and ``if/then``."""
        self.assert_red(self.build(*script_job("sudo docker run -d nornicdb")), "scripts/s.sh")
        self.assert_red(self.build(*script_job("if true; then docker run -d nornicdb; fi")),
                        "scripts/s.sh")

    def test_a_quoted_command_is_not_a_start(self) -> None:
        """GREEN: a separator inside quotes does not start a command."""
        for body in (
            f'echo "x && {COMPOSE_UP} nornicdb"',
            f"printf '%s\\n' 'a; docker run -d nornicdb'",
            f'grep -q "{COMPOSE_UP} nornicdb" log.txt',
            'assert_contains "if true; then docker run nornicdb; fi"',
        ):
            with self.subTest(body=body):
                self.assert_green(self.build(*script_job(body)))

    def test_script_run_behind_wrappers_is_followed(self) -> None:
        """RED: ``sudo``, ``cd &&``, ``timeout`` and ``bash -c`` still reach the next script."""
        inner = {"scripts/inner.sh": f"{COMPOSE_UP} nornicdb\n"}
        for name, form in (("sudo", "sudo bash scripts/inner.sh"),
                           ("cd_and", "cd /tmp && bash scripts/inner.sh"),
                           ("timeout", "timeout 60 ./scripts/inner.sh"),
                           ("bash_c", "bash -c 'scripts/inner.sh'"),
                           ("if_then", "if true; then bash scripts/inner.sh; fi")):
            with self.subTest(form=name):
                root = self.build(workflow(run_step(form)), inner)
                self.assert_red(root, "wf.yml/live", "scripts/inner.sh")

    def test_script_run_behind_wrappers_neo4j_passes(self) -> None:
        """GREEN: the same wrapped chain over a Neo4j script."""
        inner = {"scripts/inner.sh": f"{COMPOSE_UP} neo4j\n"}
        for form in ("sudo bash scripts/inner.sh", "cd /tmp && bash scripts/inner.sh",
                     "timeout 60 ./scripts/inner.sh", "bash -c 'scripts/inner.sh'"):
            with self.subTest(form=form):
                self.assert_green(self.build(workflow(run_step(form)), inner))

    def test_profile_in_a_script_fails(self) -> None:
        """RED: a script that brings up the NornicDB profile, and COMPOSE_PROFILES."""
        self.assert_red(self.build(*script_job("docker compose --profile nornicdb up -d")),
                        "scripts/s.sh", "compose profile")
        self.assert_red(self.build(*script_job("export COMPOSE_PROFILES=nornicdb")),
                        "scripts/s.sh", "compose profile")
        self.assert_green(self.build(*script_job("docker compose --profile neo4j up -d")))

    def test_ambiguous_timeout_options_finish_quickly(self) -> None:
        """REGRESSION (CodeQL): ambiguous ``timeout`` flags must not backtrack.

        ``-k`` matches both option spellings, so ``timeout -k -! ...`` with no
        duration explores every partition before failing. The wrapper must
        return the line unchanged, and fast.
        """
        shell = importlib.util.module_from_spec(_SHELL_SPEC)  # type: ignore[arg-type]
        _SHELL_SPEC.loader.exec_module(shell)  # type: ignore[union-attr]
        line = ("timeout " + "-k -! " * 24).rstrip()
        started = time.monotonic()
        self.assertEqual(shell.commands(line), [line])
        self.assertLess(time.monotonic() - started, 2.0)


class StatedLimitTest(ShapeTest):
    """What the guard does not read. Closing one means changing this test and the docs."""

    def test_limit_default_compose_file_is_not_opened(self) -> None:
        """LIMIT: a compose file's own default service is not resolved."""
        self.assert_green(self.build(workflow(run_step(
            "docker compose -f eshu-deploy/compose/docker-compose.yaml up -d"))))

    def test_limit_computed_names_are_not_resolved(self) -> None:
        """LIMIT: ``$(...)`` and a ``${X:-nornicdb}`` default assigned earlier."""
        self.assert_green(self.build(workflow(run_step(
            'svc=$(echo nornicdb)\n' + COMPOSE_UP + ' "$svc"'))))
        self.assert_green(self.build(workflow(run_step(
            'svc="${SVC:-nornicdb}"\n' + COMPOSE_UP + ' "$svc"'))))

    def test_limit_expression_on_a_non_backend_key_is_not_read(self) -> None:
        """LIMIT: only backend, graph_backend, graph_service and the env key are read."""
        matrix = "    strategy:\n      matrix:\n        variant: ${{ fromJSON('[\"nornicdb\"]') }}\n"
        text = workflow(run_step("echo hi")).replace("    steps:\n", matrix + "    steps:\n")
        self.assert_green(self.build(text))

    def test_limit_command_substitution_inside_double_quotes_is_not_split(self) -> None:
        """LIMIT: a ``$(..)`` inside a double-quoted string is part of that string."""
        self.assert_green(self.build(*script_job('echo "$(docker run -d nornicdb)"')))

    def test_limit_yaml_anchor_alias_is_not_resolved(self) -> None:
        """LIMIT: a value reached through a YAML alias is not followed to its anchor."""
        text = ("name: F\non:\n  push:\nx-backend: &b nornicdb\njobs:\n  live:\n"
                "    env:\n      ESHU_GRAPH_BACKEND: *b\n    steps:\n      - run: echo hi\n")
        self.assert_green(self.build(text))

    def test_limit_script_without_an_extension_is_not_followed(self) -> None:
        """LIMIT: only ``*.sh`` and ``*.py`` files a job runs are opened."""
        root = self.build(workflow(run_step("bash scripts/run-live")),
                          {"scripts/run-live": "docker run -d nornicdb\n"})
        self.assert_green(root)

    def test_limit_makefile_target_is_not_opened(self) -> None:
        """LIMIT: ``make <target>`` does not open the Makefile."""
        root = self.build(workflow(run_step("make live")),
                          {"Makefile": "live:\n\tdocker run -d nornicdb\n"})
        self.assert_green(root)

    def test_limit_quoting_nested_past_two_levels_is_not_parsed(self) -> None:
        """LIMIT: a ``bash -c`` string inside two other quoted strings is not read."""
        inner = f"{COMPOSE_UP} nornicdb"
        nested = f"bash -c 'bash -c \"bash -c \\\"{inner}\\\"\"'"
        self.assert_green(self.build(*script_job(nested)))

    def test_limit_go_helper_that_execs_docker_is_not_read(self) -> None:
        """LIMIT: Go source is not scanned; only scripts and workflow text are."""
        root = self.build(workflow(run_step("go test ./...")), {
            "go/x/x_test.go": 'package x\nfunc f() { exec.Command("docker", "run", "-d", "nornicdb") }\n'})
        self.assert_green(root)

    def test_limit_xargs_argument_is_not_resolved(self) -> None:
        """LIMIT: a name piped to ``xargs docker run`` is not joined to the command."""
        self.assert_green(self.build(workflow(run_step("echo nornicdb | xargs docker run -d"))))
        self.assert_green(self.build(*script_job("echo nornicdb | xargs docker run -d")))

    def test_dead_step_in_a_live_job_is_still_flagged(self) -> None:
        """Only a job-level ``if: false`` switches a job off; a dead step errs safe."""
        step = "      - if: false\n        run: docker run -d nornicdb\n"
        self.assert_red(self.build(workflow(step)), "wf.yml/live")


if __name__ == "__main__":
    unittest.main()
