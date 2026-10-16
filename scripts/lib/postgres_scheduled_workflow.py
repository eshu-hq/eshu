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
    "Run disposable golden corpus residual proof",
    "Run disposable query proofs",
    "Run disposable storage proofs",
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
