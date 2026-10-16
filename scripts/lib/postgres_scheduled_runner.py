#!/usr/bin/env python3
"""Verify source enrollment and Go events for scheduled PostgreSQL proofs."""

import argparse
import json
from pathlib import Path
import re
import sys

from reducer_contention_events import verify_events as verify_go_events


INVENTORY = Path(__file__).with_name("postgres_scheduled_inventory.json")
WORKFLOW = Path(".github/workflows/postgres-scheduled-live.yml")
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
PACKAGES = {
    "crossplane": "github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane",
    "infra_inventory": "github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory",
}
DSNS = {
    "crossplane": "ESHU_CROSSPLANE_REDRIVE_PROOF_DSN",
    "infra_inventory": "ESHU_POSTGRES_DSN",
}
IMAGE = (
    "ghcr.io/eshu-hq/ci-postgres-alpine@sha256:"
    "77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
)


def load_inventory(path: Path = INVENTORY) -> dict[str, dict[str, object]]:
    """Load the reviewed exact file and test inventory."""
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or not value:
        raise ValueError("scheduled inventory must be a nonempty mapping")
    return value


def verify_inventory(root: Path, inventory: dict[str, dict[str, object]]) -> None:
    """Reject source, schedule, backend, selector, or event-check drift."""
    workflow = (root / WORKFLOW).read_text(encoding="utf-8")
    if "workflow_dispatch:" not in workflow or "35 4 * * *" not in workflow:
        raise ValueError("scheduled workflow cadence or manual dispatch missing")
    if IMAGE not in workflow or "postgres:latest" in workflow:
        raise ValueError("pinned PostgreSQL backend missing")
    if workflow.count(IMAGE) != 1:
        raise ValueError("pinned PostgreSQL service must appear once")
    for group, package in PACKAGES.items():
        package_path = "./" + package.split("/go/", 1)[1]
        pattern = re.compile(
            r"(?m)^\s*go test -json -race -p=1 -count=1 -timeout=7m "
            + re.escape(package_path)
            + r" -run '([^']+)' \| tee \"\$\{RUNNER_TEMP\}/"
            + re.escape(group)
            + r"\.jsonl\"$"
        )
        selectors = pattern.findall(workflow)
        if len(selectors) != 1:
            raise ValueError(f"missing or duplicate selector for {group}")
        selected = selectors[0]
        expected = sorted(
            name for row in inventory.values() if row.get("group") == group
            for name in row.get("tests", [])
        )
        if selected != "^(" + "|".join(expected) + ")$":
            raise ValueError(f"selector does not equal exact inventory for {group}")
        if workflow.count(f"{DSNS[group]}: postgres://") != 1:
            raise ValueError(f"missing or duplicate DSN for {group}")
        verifier = (
            f"python3 ../scripts/lib/postgres_scheduled_runner.py --events "
            f'"${{RUNNER_TEMP}}/{group}.jsonl" --group {group}'
        )
        if workflow.count(verifier) != 1:
            raise ValueError(f"missing event verifier for {group}")
    if workflow.count("set -o pipefail") != 2:
        raise ValueError("both scheduled package steps require pipefail")
    for group, database in (("crossplane", "eshu_crossplane_scheduled"),
                            ("infra_inventory", "eshu_inventory_scheduled")):
        create = f"-c 'CREATE DATABASE {database}'"
        dsn = f"{DSNS[group]}: postgres://eshu:change-me@localhost:15432/{database}?sslmode=disable"
        if workflow.count(create) != 1 or workflow.count(dsn) != 1:
            raise ValueError(f"fresh database and DSN mismatch for {group}")

    for relative, row in inventory.items():
        group, package, tests = row.get("group"), row.get("package"), row.get("tests")
        if group not in PACKAGES or package != PACKAGES[group]:
            raise ValueError(f"wrong inventory group or package: {relative}")
        if not isinstance(tests, list) or not tests or tests != sorted(set(tests)):
            raise ValueError(f"invalid inventory tests: {relative}")
        if package != "github.com/eshu-hq/eshu/" + relative.rsplit("/", 1)[0]:
            raise ValueError(f"wrong source package: {relative}")
        source = (root / relative).read_text(encoding="utf-8")
        actual = sorted(TEST_PATTERN.findall(source))
        if actual != tests:
            raise ValueError(f"source Test inventory changed: {relative}")


def verify_events(path: Path, package: str, expected: set[str]) -> bool:
    """Require each enrolled test and the package to run and pass once."""
    return verify_go_events(path, package, expected, sys.stderr)


def main() -> int:
    """Check the committed inventory, then optionally one Go event stream."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--events", type=Path)
    parser.add_argument("--group", choices=tuple(PACKAGES))
    arguments = parser.parse_args()
    try:
        inventory = load_inventory()
        verify_inventory(arguments.repo_root, inventory)
        if arguments.events is None:
            print(f"scheduled PostgreSQL inventory: {len(inventory)} files verified")
            return 0
        if arguments.group is None:
            parser.error("--group is required with --events")
        expected = {
            name for row in inventory.values() if row["group"] == arguments.group
            for name in row["tests"]
        }
        return 0 if verify_events(arguments.events, PACKAGES[arguments.group], expected) else 1
    except (OSError, ValueError, re.error, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
