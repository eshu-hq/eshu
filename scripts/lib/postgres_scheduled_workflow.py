#!/usr/bin/env python3
"""Restrict the scheduled PostgreSQL workflow to reviewed YAML key spelling."""

import re

ROOT_KEYS = frozenset({"name", "on", "permissions", "concurrency", "jobs"})
JOB_KEYS = frozenset({"runs-on", "timeout-minutes", "services", "steps"})
STEP_KEYS = frozenset({"uses", "with", "run", "env", "working-directory", "if"})
STEP_NAMES = (
    "Check out repository",
    "Set up Go",
    "Pre-warm Go modules",
    "Check exact enrollment and negative cases",
    "Record hosted proof manifest",
    "Create fresh Crossplane database",
    "Run Crossplane proofs",
    "Create fresh inventory database",
    "Run infra inventory proofs",
    "Create fresh ACK database",
    "Run ACK and completion proofs",
    "Create fresh claim database",
    "Run claim proofs",
    "Create fresh liveness database",
    "Run liveness proofs",
    "Create fresh fanout database",
    "Run fanout proofs",
    "Create fresh search database",
    "Run search proofs",
    "Create fresh readiness database",
    "Run readiness proofs",

    "Create fresh containerimage database",
    "Run containerimage proofs",
    "Run disposable golden corpus residual proof",
    "Run disposable query proofs",
    "Run disposable storage proofs",
    "Create fresh freshness_links database",
    "Run freshness_links proofs",
    "Verify hosted proof bundle",
    "Retain hosted proof bundle",
)


def canonical_keys(text: str, indent: int, allowed: frozenset[str]) -> dict[str, str]:
    """Read literal keys at one indentation level and reject YAML aliases."""
    entries: dict[str, str] = {}
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        depth = len(line) - len(line.lstrip(" "))
        if depth != indent:
            continue
        match = re.fullmatch(rf" {{{indent}}}([a-z][a-z-]*):(?: .*)?", line)
        if match is None or match.group(1) not in allowed or match.group(1) in entries:
            raise ValueError(f"non-fail-closed conditional workflow key: {line}")
        entries[match.group(1)] = line
    return entries


def verify_canonical_workflow(workflow: str) -> None:
    """Reject alternate activation or failure-suppression keys in owned job."""
    root = canonical_keys(workflow, 0, ROOT_KEYS)
    if (set(root) != ROOT_KEYS or root["on"] != "on:"
            or root["jobs"] != "jobs:" or root["permissions"] != "permissions:"
            or root["concurrency"] != "concurrency:"
            or workflow.count("permissions:\n  contents: read\n") != 1):
        raise ValueError("non-fail-closed workflow root")
    permissions = re.search(r"(?ms)^permissions:\n(.*?)(?=^concurrency:)", workflow)
    concurrency = re.search(r"(?ms)^concurrency:\n(.*?)(?=^jobs:)", workflow)
    if (not permissions or canonical_keys(
            permissions.group(1), 2, frozenset({"contents"})
        ) != {"contents": "  contents: read"}):
        raise ValueError("non-fail-closed workflow permissions")
    concurrency_keys = frozenset({"group", "cancel-in-progress"})
    if (not concurrency or canonical_keys(
            concurrency.group(1), 2, concurrency_keys
        ) != {
            "group": "  group: postgres-scheduled-live-${{ github.event_name }}-${{ github.ref }}",
            "cancel-in-progress": "  cancel-in-progress: false",
        }):
        raise ValueError("non-fail-closed workflow PR concurrency")
    jobs = workflow.split("jobs:\n", 1)[1]
    job_headers = [
        line for line in jobs.splitlines()
        if line.startswith("  ") and not line.startswith("   ")
        and line.strip() and not line.lstrip().startswith("#")
    ]
    if job_headers != ["  postgres-scheduled:"]:
        raise ValueError("non-fail-closed workflow job header")
    job_keys = canonical_keys(jobs, 4, JOB_KEYS)
    if (set(job_keys) != JOB_KEYS or job_keys["services"] != "    services:"
            or job_keys["steps"] != "    steps:"):
        raise ValueError("non-fail-closed workflow job")
    step_list = jobs.split("    steps:\n", 1)[1]
    names = [
        line.removeprefix("      - name: ")
        for line in step_list.splitlines()
        if line.startswith("      ") and not line.startswith("       ")
        and line.strip() and not line.lstrip().startswith("#")
    ]
    if names != list(STEP_NAMES):
        raise ValueError("non-fail-closed workflow step headers")
    for name, step in zip(names, re.split(r"(?m)^      - name: .+\n", step_list)[1:]):
        keys = canonical_keys(step, 8, STEP_KEYS)
        if not keys:
            raise ValueError(f"non-fail-closed workflow step: {name}")
        if "if" in keys and name != "Retain hosted proof bundle":
            raise ValueError(f"non-fail-closed conditional workflow step: {name}")
        if name == "Retain hosted proof bundle" and keys.get("if") != "        if: always()":
            raise ValueError("non-fail-closed artifact workflow step")


def verify_trigger(workflow: str, inventory: dict[str, dict[str, object]], pr_paths: set[str]) -> None:
    """Keep the same daily job enrolled for narrowly selected PR changes."""
    match = re.search(r"(?ms)^on:\n(.*?)(?=^permissions:)", workflow)
    if not match:
        raise ValueError("scheduled workflow event block missing")
    events = re.findall(r"(?m)^  ([a-z_]+):", match.group(1))
    if len(events) != 3 or set(events) != {"schedule", "workflow_dispatch", "pull_request"}:
        raise ValueError("scheduled workflow PR events changed")
    if not re.search(r"(?m)^    - cron: '35 4 \* \* \*'$", match.group(1)):
        raise ValueError("scheduled workflow cadence missing")
    pr = re.search(
        r"(?ms)^  pull_request:\n(.*?)(?=^  [a-z_]+:|\Z)",
        match.group(1),
    )
    if not pr:
        raise ValueError("scheduled workflow PR path filter missing")
    paths = sorted(pr_paths | set(inventory))
    expected = "    branches:\n      - main\n    paths:\n" + "".join(
        f"      - '{path}'\n" for path in paths
    )
    if pr.group(1).rstrip("\n") + "\n" != expected:
        raise ValueError("scheduled workflow PR paths or branches differ from exact enrollment")
    group = "  group: postgres-scheduled-live-${{ github.event_name }}-${{ github.ref }}"
    if (len(re.findall("(?m)^" + re.escape(group) + "$", workflow)) != 1
            or not re.search(r"(?m)^  cancel-in-progress: false$", workflow)):
        raise ValueError("scheduled workflow PR concurrency is not separate")
    jobs = workflow.split("\njobs:\n", 1)
    if len(jobs) != 2 or re.findall(r"(?m)^  ([A-Za-z0-9_-]+):", jobs[1]) != ["postgres-scheduled"]:
        raise ValueError("scheduled workflow needs the same live job")
    if re.search(r"(?m)^    if:", jobs[1]):
        raise ValueError("conditional scheduled live job")
    if re.search(r"(?m)^\s*continue-on-error:", jobs[1]):
        raise ValueError("scheduled live job cannot use continue-on-error")
