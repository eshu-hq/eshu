#!/usr/bin/env python3
"""Check the trusted PR plan filter before workflow migration activates it."""

import json
import subprocess
from pathlib import Path


FILTER = Path(__file__).with_name("pr-plan-v2.jq")
HEAD = "a" * 40
RUN_ID = 101
RUN_ATTEMPT = 3


def accepts(plan: object) -> bool:
    """Run the checked-in policy on a plan with pinned event identity."""
    result = subprocess.run(
        [
            "jq", "-e", "--arg", "sha", HEAD,
            "--argjson", "run_id", str(RUN_ID),
            "--argjson", "run_attempt", str(RUN_ATTEMPT),
            "-f", str(FILTER),
        ],
        input=json.dumps(plan),
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode not in (0, 1):
        raise AssertionError(f"filter could not run: {result.stderr.strip()}")
    return result.returncode == 0


def main() -> None:
    """Require all modes to bind trusted run and plan identities."""
    full: dict[str, object] = {
        "schema_version": 2,
        "mode": "full",
        "head_sha": HEAD,
        "target_run_id": RUN_ID,
        "target_run_attempt": RUN_ATTEMPT,
        "merge_sha": "b" * 40,
        "merge_tree": "c" * 40,
        "workflows": ["test.yml"],
        "jobs": {"test.yml": ["go-core"]},
        "gates": ["go-build"],
        "reason": "full selection",
    }
    reuse = full | {
        "mode": "reuse",
        "source_sha": "d" * 40,
        "source_full_sha": "e" * 40,
        "tree": "f" * 40,
        "workflows": [],
        "jobs": {},
        "gates": [],
    }
    smoke = reuse | {
        "mode": "smoke",
        "workflows": ["test.yml"],
        "jobs": {"test.yml": ["go-core"]},
        "gates": ["go-build"],
    }
    for label, candidate in (("full", full), ("gateless full", full | {"gates": []}),
                             ("reuse", reuse), ("smoke", smoke)):
        if not accepts(candidate):
            raise AssertionError(f"valid {label} plan was rejected")

    malformed = (
        full | {"schema_version": 1},
        full | {"head_sha": "9" * 40},
        full | {"target_run_id": RUN_ID + 1},
        full | {"target_run_attempt": RUN_ATTEMPT + 1},
        full | {"merge_tree": "short"},
        full | {"source_full_sha": "e" * 40},
        full | {"jobs": {}},
        full | {"mode": "unknown"},
        full | {"jobs": {"other.yml": ["go-core"]}},
        full | {"workflows": []},
        [],
        reuse | {"source_full_sha": ""},
        reuse | {"jobs": {"test.yml": ["go-core"]}},
        smoke | {"source_full_sha": ""},
    )
    for number, candidate in enumerate(malformed, start=1):
        if accepts(candidate):
            raise AssertionError(f"malformed plan {number} was accepted")
    print("PR plan filter: full, reuse, smoke, and identity rejection passed")


if __name__ == "__main__":
    main()
