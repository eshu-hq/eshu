#!/usr/bin/env python3
"""Check that the central PR caller owns every migrated synchronize leaf."""

from __future__ import annotations

import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / ".github" / "workflows"
FRAGMENTS = ROOT / "specs" / "ci-pr-workflows.d"
PLAN_FILTER = ROOT / "scripts" / "ci" / "pr-plan-v2.jq"
POSTGRES_GROUP = "postgres-scheduled-live-${{ github.event_name }}-${{ github.ref }}"
PR_CANCEL = "${{ github.event_name == 'pull_request' }}"
FULL_MODE_IF = "${{ github.event_name == 'pull_request' && needs.pr-plan.outputs.mode == 'full' }}"
FULL_MODE_RUN = 'test "${{ needs.pr-plan.outputs.mode }}" = full'
PLAN_FULL_MODE_IF = "${{ steps.plan.outputs.mode == 'full' }}"
PLAN_FULL_MODE_RUN = 'test "${{ steps.plan.outputs.mode }}" = full'


def concurrency_fields(source: str) -> dict[str, str]:
    """Parse the workflow's top-level concurrency mapping from its source."""
    match = re.search(r"^concurrency:\n((?:  [^\n]+\n)+)", source, re.MULTILINE)
    if match is None:
        raise ValueError("workflow concurrency block missing")
    fields = {}
    for line in match.group(1).splitlines():
        key, separator, value = line.strip().partition(":")
        if not separator or key in fields:
            raise ValueError("workflow concurrency mapping is malformed")
        fields[key] = value.strip()
    return fields


def check_leaf(name: str, source: str) -> None:
    """Reject a direct PR listener or an unguarded callable payload job."""
    if re.search(r"^  pull_request:\s*$", source, re.MULTILINE):
        raise ValueError(f"{name}: direct PR trigger remains")
    if "  workflow_call:\n" not in source:
        raise ValueError(f"{name}: workflow_call missing")
    for input_name in ("plan_jobs", "plan_gates", "plan_mode"):
        if f"      {input_name}:\n" not in source:
            raise ValueError(f"{name}: {input_name} input missing")
    if "contains(fromJSON(inputs.plan_jobs" not in source:
        raise ValueError(f"{name}: selected-job guard missing")
    if name == "postgres-scheduled-live.yml":
        fields = concurrency_fields(source)
        if fields != {"group": POSTGRES_GROUP, "cancel-in-progress": PR_CANCEL}:
            raise ValueError(f"{name}: PR cancellation or event/ref group changed")
    else:
        group = f"ci-leaf-{name.removesuffix('.yml')}-"
        if group not in source:
            raise ValueError(f"{name}: distinct concurrency group missing")


def check_full_mode_sentinel(source: str) -> None:
    """Require a successful full-only step in the top-level Go core job."""
    section = re.search(
        r"^  go-core:\n(.*?)(?=^  [a-z0-9-]+:\n|\Z)",
        source,
        re.MULTILINE | re.DOTALL,
    )
    if section is None or section.group(1).count("- name: Record full plan mode") != 1:
        raise ValueError("go-core full plan sentinel missing or duplicated")
    step = re.search(
        r"^      - name: Record full plan mode\n"
        r"        if: (.+)\n"
        r"        run: (.+)$",
        section.group(1),
        re.MULTILINE,
    )
    if step is None or step.groups() != (FULL_MODE_IF, FULL_MODE_RUN):
        raise ValueError("go-core full plan sentinel admits smoke or hides failure")


def check_pr_plan_full_mode_sentinel(source: str) -> None:
    """Require the planner's own FULL step, including docs-only PRs."""
    section = re.search(
        r"^  pr-plan:\n(.*?)(?=^  [a-z0-9-]+:\n|\Z)",
        source,
        re.MULTILINE | re.DOTALL,
    )
    if section is None or section.group(1).count("- name: Record full plan mode") != 1:
        raise ValueError("pr-plan full plan sentinel missing or duplicated")
    step = re.search(
        r"^      - name: Record full plan mode\n"
        r"        if: (.+)\n"
        r"        run: (.+)$",
        section.group(1),
        re.MULTILINE,
    )
    if step is None or step.groups() != (PLAN_FULL_MODE_IF, PLAN_FULL_MODE_RUN):
        raise ValueError("pr-plan full plan sentinel admits smoke or hides failure")


def check_pr_plan_transfer_mode_sentinels(source: str) -> None:
    """Require exact planner steps for reuse and smoke publication."""
    section = re.search(
        r"^  pr-plan:\n(.*?)(?=^  [a-z0-9-]+:\n|\Z)",
        source,
        re.MULTILINE | re.DOTALL,
    )
    if section is None:
        raise ValueError("pr-plan job missing")
    for mode in ("reuse", "smoke"):
        name = f"Record {mode} plan mode"
        if section.group(1).count(f"- name: {name}") != 1:
            raise ValueError(f"pr-plan {mode} mode sentinel missing or duplicated")
        step = re.search(
            rf"^      - name: {name}\n"
            r"        if: (.+)\n"
            r"        run: (.+)$",
            section.group(1),
            re.MULTILINE,
        )
        expected = (
            "${{ steps.plan.outputs.mode == '" + mode + "' }}",
            'test "${{ steps.plan.outputs.mode }}" = ' + mode,
        )
        if step is None or step.groups() != expected:
            raise ValueError(f"pr-plan {mode} sentinel admits another mode or hides failure")


def check_pr_plan_schema(caller: str) -> None:
    """Exercise the production jq guard with valid and seeded invalid plans."""
    for token in (
        '--argjson run_id "${GITHUB_RUN_ID}"',
        '--argjson run_attempt "${GITHUB_RUN_ATTEMPT}"',
        '-f scripts/ci/pr-plan-v2.jq',
    ):
        if token not in caller:
            raise ValueError(f"pr-plan schema-2 guard has no native caller binding: {token}")
    head = "a" * 40
    plan = {
        "schema_version": 2,
        "mode": "full",
        "head_sha": head,
        "target_run_id": 101,
        "target_run_attempt": 3,
        "merge_sha": "b" * 40,
        "merge_tree": "c" * 40,
        "workflows": ["test.yml"],
        "jobs": {"test.yml": ["go-core"]},
        "gates": ["go-build"],
        "reason": "full selection",
    }

    def result(candidate: dict[str, object]) -> int:
        """Return the production filter's exit status for one plan."""
        command = [
            "jq", "-e", "--arg", "sha", head,
            "--argjson", "run_id", "101",
            "--argjson", "run_attempt", "3",
            "-f", str(PLAN_FILTER),
        ]
        return subprocess.run(
            command, input=json.dumps(candidate), text=True,
            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, check=False,
        ).returncode

    if result(plan) != 0:
        raise ValueError("valid schema-2 full PR plan was rejected")
    transfer = plan | {
        "mode": "reuse", "source_sha": "d" * 40,
        "source_full_sha": "e" * 40, "tree": "f" * 40,
        "workflows": [], "jobs": {}, "gates": [],
    }
    if result(transfer) != 0:
        raise ValueError("valid schema-2 transferred PR plan was rejected")
    smoke = transfer | {
        "mode": "smoke", "workflows": ["test.yml"],
        "jobs": {"test.yml": ["go-core"]}, "gates": ["go-build"],
    }
    if result(smoke) != 0:
        raise ValueError("valid schema-2 smoke PR plan was rejected")
    for wrong in (
        plan | {"schema_version": 1},
        plan | {"target_run_id": 102},
        plan | {"target_run_attempt": 4},
        plan | {"merge_tree": "short"},
        transfer | {"source_full_sha": ""},
        smoke | {"source_full_sha": ""},
        plan | {"source_full_sha": "e" * 40},
    ):
        if result(wrong) != 1:
            raise ValueError("invalid schema-2 PR plan was not rejected")


def main() -> None:
    """Verify the caller-to-leaf inventory and a seeded duplicate trigger."""
    fragments = sorted(FRAGMENTS.glob("*.yml"))
    if len(fragments) != 28:
        raise ValueError(f"expected 28 migrated leaves, got {len(fragments)}")
    central = (WORKFLOWS / "test.yml").read_text(encoding="utf-8")
    if "  pr-plan:\n" not in central or "ci-gates-bin\" pr-plan" not in central:
        raise ValueError("Build Test has no trusted PR planner")
    check_full_mode_sentinel(central)
    check_pr_plan_full_mode_sentinel(central)
    check_pr_plan_transfer_mode_sentinels(central)
    if concurrency_fields(central).get("group") == POSTGRES_GROUP:
        raise ValueError("Postgres leaf concurrency group collides with Build Test")
    for lane in ("go-core", "go-race"):
        section = re.search(
            rf"^  {lane}-complete:\n(.*?)(?=^  [a-z0-9-]+:\n|\Z)",
            central,
            re.MULTILINE | re.DOTALL,
        )
        if section is None or (
            "CODE_CHANGED: ${{ needs.changes.outputs.code }}" not in section.group(1)
        ):
            raise ValueError(f"{lane}-complete does not receive the code-change result")
    for fragment in fragments:
        name = fragment.name
        source = (WORKFLOWS / name).read_text(encoding="utf-8")
        check_leaf(name, source)
        caller = f"call-{fragment.stem}:"
        if len(re.findall(r"^  " + re.escape(caller), central, re.MULTILINE)) != 1:
            raise ValueError(f"{name}: expected one central call job")
        if f"uses: ./.github/workflows/{name}" not in central:
            raise ValueError(f"{name}: caller does not call its leaf")
    if len(re.findall(r"^  call-[a-z0-9-]+:", central, re.MULTILINE)) != len(fragments):
        raise ValueError("central caller has an unregistered call job")
    first = fragments[0].name
    source = (WORKFLOWS / first).read_text(encoding="utf-8")
    try:
        check_leaf(first, source.replace("  workflow_call:\n", "  pull_request:\n  workflow_call:\n", 1))
    except ValueError:
        pass
    else:
        raise ValueError("seeded duplicate PR trigger was accepted")
    full_if_line = f"        if: {FULL_MODE_IF}\n"
    if central.count(full_if_line) != 1:
        raise ValueError("full plan sentinel condition is not unique")
    for wrong in (
        "${{ github.event_name == 'pull_request' && needs.pr-plan.outputs.mode == 'smoke' }}",
        "${{ github.event_name == 'pull_request' }}",
    ):
        broken = central.replace(full_if_line, f"        if: {wrong}\n", 1)
        try:
            check_full_mode_sentinel(broken)
        except ValueError:
            pass
        else:
            raise ValueError("go-core sentinel accepted a non-full PR mode")
    for wrong in (
        "${{ steps.plan.outputs.mode == 'smoke' }}",
        "${{ github.event_name == 'pull_request' }}",
    ):
        broken = central.replace(f"        if: {PLAN_FULL_MODE_IF}\n", f"        if: {wrong}\n", 1)
        if broken == central:
            raise ValueError("pr-plan full plan sentinel condition is missing")
        try:
            check_pr_plan_full_mode_sentinel(broken)
        except ValueError:
            pass
        else:
            raise ValueError("pr-plan sentinel accepted a non-full mode")
    for mode in ("reuse", "smoke"):
        condition = "${{ steps.plan.outputs.mode == '" + mode + "' }}"
        for broken in (
            central.replace(condition, "${{ steps.plan.outputs.mode == 'full' }}", 1),
            central.replace(
                f'run: test "${{{{ steps.plan.outputs.mode }}}}" = {mode}',
                "run: true", 1,
            ),
        ):
            if broken == central:
                raise ValueError(f"pr-plan {mode} sentinel seed did not change the workflow")
            try:
                check_pr_plan_transfer_mode_sentinels(broken)
            except ValueError:
                pass
            else:
                raise ValueError(f"pr-plan {mode} sentinel accepted a seeded violation")
    postgres = (WORKFLOWS / "postgres-scheduled-live.yml").read_text(encoding="utf-8")
    for invalid in ("false", "true"):
        broken = postgres.replace(f"cancel-in-progress: {PR_CANCEL}", f"cancel-in-progress: {invalid}", 1)
        if broken == postgres:
            raise ValueError("Postgres cancellation seed did not alter the workflow")
        try:
            check_leaf("postgres-scheduled-live.yml", broken)
        except ValueError:
            pass
        else:
            raise ValueError(f"Postgres cancellation accepted unconditional {invalid}")
    subprocess.run(
        [sys.executable, str(ROOT / "scripts" / "ci" / "test_smoke_go_build_canary.py")],
        check=True,
    )
    check_pr_plan_schema(central)
    print("PR scheduling wiring PASS")


if __name__ == "__main__":
    main()
