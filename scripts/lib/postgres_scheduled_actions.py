#!/usr/bin/env python3
"""Expand the reviewed scheduled PostgreSQL composite actions into the workflow.

#9488 split the bulk leg: the six root storage package families run from their
own composite in the scheduled-storage job. Each composite must keep its
canonical header and step controls and be called exactly once.
"""

from pathlib import Path


BULK_ACTION = Path(".github/actions/postgres-scheduled-bulk/action.yml")
STORAGE_ACTION = Path(".github/actions/postgres-scheduled-storage/action.yml")
# Each reviewed composite: (path, name, description, calling step).
ACTIONS = {
    "bulk": (BULK_ACTION, "Scheduled PostgreSQL bulk proofs",
             "Run the enrolled bulk proof families and upload their streams.",
             "Run bulk proof families"),
    "storage": (STORAGE_ACTION, "Scheduled PostgreSQL storage proofs",
                "Run the enrolled root storage package proof families and upload "
                "their streams.",
                "Run storage proof families"),
}


def expand_action(workflow: str, action: str, kind: str) -> str:
    """Expose one reviewed composite's commands to the existing proof checks.

    Args:
        workflow: The scheduled workflow text.
        action: The composite action's bytes.
        kind: The ACTIONS key naming the composite.

    Returns:
        The workflow with the composite's steps inlined at its call site.

    Raises:
        ValueError: The header, call site, or step controls differ from
            the reviewed contract.
    """
    path, name, description, step_name = ACTIONS[kind]
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
    body = action[len(prefix):]
    raw_steps = body.split("    - name:")
    if raw_steps[0].strip():
        raise ValueError(f"hosted {kind} action has unsupported step controls")
    for raw_step in raw_steps[1:]:
        step_lines = raw_step.splitlines()
        has_run = any(
            line == "      run: |" or line.startswith("      run: ")
            for line in step_lines
        )
        if has_run and step_lines.count("      shell: bash") != 1:
            raise ValueError(f"hosted {kind} action run step missing shell: bash")
    steps = []
    for line in body.splitlines():
        if line == "      shell: bash":
            continue
        if "shell:" in line or (line and not line.startswith("    ")):
            raise ValueError(f"hosted {kind} action has unsupported step controls")
        steps.append("  " + line.replace("${{ inputs.postgres_container }}",
                                        "${{ job.services.postgres.id }}"))
    return workflow.replace(marker, "\n".join(steps) + "\n", 1)


def expand_actions(workflow: str, root: Path) -> str:
    """Expand every reviewed composite the workflow calls, read from root."""
    for kind, (path, *_rest) in ACTIONS.items():
        workflow = expand_action(
            workflow, (root / path).read_text(encoding="utf-8"), kind)
    return workflow
