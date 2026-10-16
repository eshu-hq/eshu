#!/usr/bin/env python3
"""Check source enrollment and Go JSON events for contention ledger proofs."""

import argparse
import json
from pathlib import Path
import re
import sys
from typing import TextIO


INVENTORY = Path(__file__).with_name("reducer_contention_inventory.json")
WORKFLOW = Path(".github/workflows/reducer-contention-gate.yml")
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
OWNER_JOB = re.compile(
    r"(?ms)^  contention-gate:\n(?P<body>.*?)(?=^  [A-Za-z][\w-]*:\n|\Z)"
)
STEP_NAME = re.compile(r"(?m)^      - name: ([^\n]+)\n")
ACTIVE_CONDITION = (
    "        if: ${{ github.event_name != 'merge_group' || "
    "(contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), "
    "'reducer contention gate')) }}"
)
PROOF_STEPS = {
    "github.com/eshu-hq/eshu/go/internal/storage/postgres": "Run reducer contention gate",
    "github.com/eshu-hq/eshu/go/internal/query": "Run story target-support semantics proof",
    "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/wait":
        "Run readiness wait ledger proofs",
}
CONTRACT_STEP = "Verify contention runner contract"
GO_CAPTURE = re.compile(
    r"^go test (\./internal/(?:storage/postgres/readiness/wait|storage/postgres|query)/) "
    r"-run '([^']+)' -json -race -count=1 -timeout [0-9]+s(?: -v)? "
    r'\| tee "(\$\{RUNNER_TEMP\}/contention-[\w-]+\.jsonl)"$'
)
CONTRACT_COMMANDS = (
    "python3 scripts/lib/reducer_contention_events.py",
    "python3 scripts/test-reducer-contention-events.py",
    "python3 scripts/test-reducer-contention-ledger-policy.py",
    "bash scripts/verify-live-tests-ledger.sh",
)


def load_inventory(path: Path = INVENTORY) -> dict[str, dict[str, object]]:
    """Read the reviewed file and test enrollment."""
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or not value:
        raise ValueError("contention inventory must be a nonempty mapping")
    return value


def workflow_steps(workflow: str) -> dict[str, str]:
    """Read the owner job and reject enclosing skip or failure suppression."""
    jobs = list(OWNER_JOB.finditer(workflow))
    if len(re.findall(r"(?m)^jobs:$", workflow)) != 1 or len(jobs) != 1:
        raise ValueError("inactive contention owner job: missing job")
    body = jobs[0].group("body")
    if (
        re.search(r"(?m)^    (?:if|continue-on-error|defaults):", body)
        or re.search(r"(?m)^defaults:", workflow)
        or len(re.findall(r"(?m)^    needs: queue-selection$", body)) != 1
    ):
        raise ValueError("inactive or non-fail-closed contention owner job")
    matches = list(STEP_NAME.finditer(body))
    steps: dict[str, str] = {}
    for index, match in enumerate(matches):
        name = match.group(1)
        if name in steps:
            raise ValueError(f"duplicate contention step: {name}")
        end = matches[index + 1].start() if index + 1 < len(matches) else len(body)
        steps[name] = body[match.start():end]
    return steps


def active_commands(steps: dict[str, str], name: str) -> list[str]:
    """Require an active, failing Actions step with literal shell commands."""
    step = steps.get(name, "")
    conditions = re.findall(r"(?m)^        if: .*?$", step)
    if conditions != [ACTIVE_CONDITION]:
        raise ValueError(f"inactive contention step: {name}")
    if re.search(r"(?m)^        (?:continue-on-error|shell):", step):
        raise ValueError(f"non-fail-closed contention step: {name}")
    run = re.search(r"(?m)^        run: \|\n", step)
    if run is None:
        raise ValueError(f"non-fail-closed contention step body: {name}")
    lines = step[run.end():].rstrip("\n").splitlines()
    if not lines or any(not line.startswith("          ") for line in lines):
        raise ValueError(f"non-fail-closed contention step body: {name}")
    return [line[10:] for line in lines]


def verify_inventory(root: Path, inventory: dict[str, dict[str, object]]) -> None:
    """Reject source or workflow drift from the reviewed enrollment."""
    workflow = (root / WORKFLOW).read_text(encoding="utf-8")
    steps = workflow_steps(workflow)
    if tuple(active_commands(steps, CONTRACT_STEP)) != CONTRACT_COMMANDS:
        raise ValueError("non-fail-closed contention runner contract")
    selectors: dict[str, re.Pattern[str]] = {}
    for package, name in PROOF_STEPS.items():
        commands = active_commands(steps, name)
        if len(commands) != 3 or commands[0] != "set -o pipefail":
            raise ValueError(f"non-fail-closed Go event capture for {package}")
        capture = GO_CAPTURE.fullmatch(commands[1])
        if capture is None:
            raise ValueError(f"non-fail-closed Go event capture for {package}")
        relative_package, expression, events = capture.groups()
        selected_package = "github.com/eshu-hq/eshu/go/" + relative_package[2:].rstrip("/")
        if selected_package != package:
            raise ValueError(f"no workflow selector for {package}")
        verifier = (
            "python3 ../scripts/lib/reducer_contention_events.py "
            f'--events "{events}" --package {package}'
        )
        if commands[2] != verifier:
            raise ValueError(f"missing event verifier for {package}")
        selectors[package] = re.compile(expression)
    for relative, row in inventory.items():
        package = row["package"]
        tests = row["tests"]
        if (
            not isinstance(package, str)
            or not isinstance(tests, list)
            or not tests
            or not all(isinstance(name, str) and name.startswith("Test") for name in tests)
            or len(set(tests)) != len(tests)
        ):
            raise ValueError(f"invalid inventory row: {relative}")
        expected_package = "github.com/eshu-hq/eshu/" + relative.rsplit("/", 1)[0]
        if package != expected_package:
            raise ValueError(f"wrong inventory package for {relative}: {package}")
        source = (root / relative).read_text(encoding="utf-8")
        actual = sorted(TEST_PATTERN.findall(source))
        if actual != tests or len(actual) != len(set(actual)):
            raise ValueError(f"source Test inventory changed: {relative}: {actual} != {tests}")
        if package not in selectors:
            raise ValueError(f"no workflow selector for {package}")
        missing = [name for name in tests if not selectors[package].search(name)]
        if missing:
            raise ValueError(f"unselected tests in {relative}: {missing}")


def verify_events(
    path: Path, package: str, expected: set[str], output: TextIO
) -> bool:
    """Require one run and pass for every enrolled test and the real package."""
    runs = {name: 0 for name in expected}
    terminals: dict[str, list[str]] = {name: [] for name in expected}
    package_terminals: list[str] = []
    errors: list[str] = []
    try:
        with path.open(encoding="utf-8") as stream:
            for number, line in enumerate(stream, 1):
                try:
                    event = json.loads(line)
                except json.JSONDecodeError as error:
                    errors.append(f"malformed JSON at line {number}: {error}")
                    break
                if not isinstance(event, dict):
                    errors.append(f"non-object JSON at line {number}")
                    break
                action = event.get("Action")
                name = event.get("Test")
                if event.get("Package") != package:
                    errors.append(f"wrong package at line {number}: {event.get('Package')}")
                    continue
                if action == "fail":
                    errors.append(f"failed event at line {number}: {name or package}")
                if name is None:
                    if action in {"pass", "fail", "skip"}:
                        package_terminals.append(action)
                    continue
                if not isinstance(name, str):
                    errors.append(f"invalid test name at line {number}")
                    continue
                parent = name.split("/", 1)[0]
                if parent not in expected:
                    continue
                if action == "skip":
                    errors.append(f"skipped enrolled test or subtest: {name}")
                if name != parent:
                    continue
                if action == "run":
                    runs[parent] += 1
                elif action in {"pass", "fail", "skip"}:
                    terminals[parent].append(action)
    except OSError as error:
        errors.append(f"cannot read event stream: {error}")
    for name in sorted(expected):
        if runs[name] != 1 or terminals[name] != ["pass"]:
            errors.append(
                f"{name}: run={runs[name]}, terminal={terminals[name]}; expected one pass"
            )
    if package_terminals != ["pass"]:
        errors.append(f"package terminal={package_terminals}; expected one pass")
    for error in errors:
        print(error, file=output)
    if not errors:
        print(f"contention events: {package}: {len(expected)} proofs PASS", file=output)
    return not errors


def main() -> int:
    """Verify the source inventory or a package's captured Go test events."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--events", type=Path)
    parser.add_argument("--package")
    args = parser.parse_args()
    try:
        inventory = load_inventory()
        verify_inventory(args.repo_root, inventory)
        if args.events is None:
            print(f"contention inventory: {len(inventory)} files verified")
            return 0
        if not args.package:
            parser.error("--package is required with --events")
        expected = {
            name
            for row in inventory.values()
            if row["package"] == args.package
            for name in row["tests"]
        }
        if not expected:
            raise ValueError(f"no enrolled tests for {args.package}")
        return 0 if verify_events(args.events, args.package, expected, sys.stderr) else 1
    except (OSError, ValueError, re.error) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
