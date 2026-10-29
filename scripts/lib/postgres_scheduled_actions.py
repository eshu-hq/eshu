#!/usr/bin/env python3
"""Expand the reviewed scheduled PostgreSQL composite actions into the workflow.

#9488 split the bulk leg: the six root storage package families run from their
own composite in the scheduled-storage job. Both composites are pinned by
sha256 and must each be called exactly once.
"""

import hashlib
from pathlib import Path


BULK_ACTION = Path(".github/actions/postgres-scheduled-bulk/action.yml")
STORAGE_ACTION = Path(".github/actions/postgres-scheduled-storage/action.yml")
# Each reviewed composite: (path, name, description, calling step). The
# runner owns the pinned digests and passes them in at call time.
ACTIONS = {
    "bulk": (BULK_ACTION, "Scheduled PostgreSQL bulk proofs",
             "Run the enrolled bulk proof families and upload their streams.",
             "Run bulk proof families"),
    "storage": (STORAGE_ACTION, "Scheduled PostgreSQL storage proofs",
                "Run the enrolled root storage package proof families and upload "
                "their streams.",
                "Run storage proof families"),
}


def expand_action(workflow: str, action: str, kind: str, digest: str) -> str:
    """Expose one reviewed composite's commands to the existing proof checks.

    Args:
        workflow: The scheduled workflow text.
        action: The composite action's bytes.
        kind: The ACTIONS key naming the composite.
        digest: The reviewed sha256 of the composite's bytes.

    Returns:
        The workflow with the composite's steps inlined at its call site.

    Raises:
        ValueError: The bytes, header, call site, or step controls differ from
            the reviewed contract.
    """
    path, name, description, step_name = ACTIONS[kind]
    if hashlib.sha256(action.encode()).hexdigest() != digest:
        raise ValueError(f"hosted {kind} action executable bytes differ from reviewed contract")
    prefix = (
        f"name: {name}\n"
        f"description: {description}\n"
        "inputs:\n  postgres_container:\n"
        "    description: PostgreSQL service container ID.\n"
        "    required: true\n"
        "runs:\n  using: composite\n  steps:\n"
    )
    if not action.startswith(prefix):
        raise ValueError(f"hosted {kind} action has unsupported controls")
    marker = (f"      - name: {step_name}\n"
              f"        uses: ./{path.parent.as_posix()}\n"
              "        with:\n"
              "          postgres_container: ${{ job.services.postgres.id }}\n")
    if workflow.count(marker) != 1:
        raise ValueError(f"hosted {kind} action is not called exactly once")
    steps = []
    for line in action[len(prefix):].splitlines():
        if line == "      shell: bash":
            continue
        if "shell:" in line or (line and not line.startswith("    ")):
            raise ValueError(f"hosted {kind} action has unsupported step controls")
        steps.append("  " + line.replace("${{ inputs.postgres_container }}",
                                        "${{ job.services.postgres.id }}"))
    return workflow.replace(marker, "\n".join(steps) + "\n", 1)


def expand_actions(workflow: str, root: Path, digests: dict[str, str]) -> str:
    """Expand every reviewed composite the workflow calls, read from root."""
    for kind, (path, *_rest) in ACTIONS.items():
        workflow = expand_action(
            workflow, (root / path).read_text(encoding="utf-8"), kind, digests[kind])
    return workflow
