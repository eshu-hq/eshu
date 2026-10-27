#!/usr/bin/env python3
"""Bind NornicDB conformance enrollment to source, Actions, and Go events."""

import argparse
import json
from pathlib import Path
import re
import sys
from typing import TextIO


ROOT = Path(__file__).resolve().parents[2]
INVENTORY = Path(__file__).with_name("nornic_conformance_inventory.json")
RUNNER = Path("scripts/verify_backend_conformance_live.sh")
RUNNER_PREFIX = """#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GRAPH_BACKEND="${ESHU_GRAPH_BACKEND:-nornicdb}"
case "$GRAPH_BACKEND" in
    nornicdb)
        DEFAULT_GRAPH_DATABASE="nornic"
        ;;
    neo4j)
        DEFAULT_GRAPH_DATABASE="neo4j"
        ;;
    *)
        echo "Unsupported ESHU_GRAPH_BACKEND for backend conformance: $GRAPH_BACKEND" >&2
        exit 1
        ;;
esac

export ESHU_GRAPH_BACKEND="$GRAPH_BACKEND"
export ESHU_BACKEND_CONFORMANCE_LIVE=1
export ESHU_NEO4J_URI="${ESHU_NEO4J_URI:-${NEO4J_URI:-bolt://localhost:7687}}"
export ESHU_NEO4J_USERNAME="${ESHU_NEO4J_USERNAME:-${NEO4J_USERNAME:-neo4j}}"
export ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-${NEO4J_PASSWORD:-change-me}}"
export ESHU_NEO4J_DATABASE="${ESHU_NEO4J_DATABASE:-${NEO4J_DATABASE:-${DEFAULT_DATABASE:-$DEFAULT_GRAPH_DATABASE}}}"
export NEO4J_URI="$ESHU_NEO4J_URI"
export NEO4J_USERNAME="$ESHU_NEO4J_USERNAME"
export NEO4J_PASSWORD="$ESHU_NEO4J_PASSWORD"
export NEO4J_DATABASE="$ESHU_NEO4J_DATABASE"
export DEFAULT_DATABASE="$ESHU_NEO4J_DATABASE"

echo "Running live backend conformance for $ESHU_GRAPH_BACKEND on $ESHU_NEO4J_URI database $ESHU_NEO4J_DATABASE"

cd "$REPO_ROOT/go"
go test ./internal/backendconformance -run '^TestLiveBackendConformance$' -count=1 -v

"""
WORKFLOW = Path(".github/workflows/e2e-tests.yml")
STATIC_WORKFLOW = Path(".github/workflows/live-backend-tests.yml")
REGISTRY = Path("specs/ci-gates.d/gates-09.yaml")
PACKAGES = frozenset({
    "github.com/eshu-hq/eshu/go/internal/storage/cypher",
    "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/resource",
})
RUNNER_PACKAGES = frozenset({
    "./internal/storage/cypher",
    "./internal/storage/cypher/cloud/resource",
})
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
EXPECTED_FILES = frozenset({
    "go/internal/storage/cypher/cloud/resource/node_writer_heterogeneous_batch_live_test.go",
    "go/internal/storage/cypher/retrying_executor_live_test.go",
    "go/internal/storage/cypher/retrying_executor_relationship_snapshot_live_test.go",
    "go/internal/storage/cypher/semantic_entity_full_repo_retract_live_test.go",
    "go/internal/storage/cypher/semantic_entity_live_test.go",
    "go/internal/storage/cypher/semantic_entity_variable_nornicdb_live_test.go",
    "go/internal/storage/cypher/tfstate_canonical_writer_stale_attrs_live_test.go",
})
ROUTED_PATHS = frozenset({
    "go/**",
    "scripts/verify_backend_conformance_live.sh",
    "scripts/lib/nornic_conformance_events.py",
    "scripts/lib/nornic_conformance_inventory.json",
    "scripts/test-nornic-conformance-events.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "scripts/lib/nornic_cli_activation_guard.py",
    "scripts/test-nornic-ledger-guard-boundary.py",
    "specs/live-tests.v1.yaml",
    "specs/live-tests.d/**",
    ".github/workflows/e2e-tests.yml",
    ".github/workflows/live-backend-tests.yml",
    "specs/ci-gates.d/gates-09.yaml",
})


def _active_lines(source: str) -> list[tuple[int, str]]:
    """Keep the supported active YAML subset without comment decoys."""
    active = []
    for line in source.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = line[:len(line) - len(line.lstrip())]
        if set(indent) - {" "}:
            raise ValueError("unsupported YAML indentation")
        active.append((len(indent), line.strip()))
    return active


def _block(lines: list[tuple[int, str]], key: str,
           depth: int) -> list[tuple[int, str]]:
    """Select one literal mapping, rejecting quoted, spaced, and duplicate keys."""
    matches = [index for index, (indent, content) in enumerate(lines)
               if indent == depth
               and content.split(":", 1)[0].strip().strip("\"'") == key]
    if len(matches) != 1 or lines[matches[0]][1] != key + ":":
        raise ValueError(f"expected one active {key} mapping")
    start = matches[0] + 1
    end = next((index for index in range(start, len(lines))
                if lines[index][0] <= depth), len(lines))
    return lines[start:end]


def _paths(lines: list[tuple[int, str]], depth: int) -> set[str]:
    """Read one literal sequence without accepting nested or duplicate items."""
    paths = set()
    for indent, content in lines:
        match = re.fullmatch(r'''- ["']([^"']+)["']''', content)
        if (indent != depth or match is None or match.group(1) in paths
                or match.group(1).startswith("!")):
            raise ValueError("unsupported active path sequence")
        paths.add(match.group(1))
    return paths


def _workflow_paths(source: str, static: bool, fragment: str) -> set[str]:
    """Read callable events and frozen synchronize paths together."""
    root = [content.split(":", 1)[0] for depth, content in _active_lines(source) if depth == 0]
    expected_root = ["name", "on", "concurrency", "permissions", "env", "jobs"] if static else ["name", "on", "permissions", "concurrency", "env", "jobs"]
    if root != expected_root:
        raise ValueError("unsupported workflow root controls")
    events = _block(_active_lines(source), "on", 0)
    expected = ["workflow_call:", "merge_group:", "push:"]
    if static:
        expected.append("workflow_dispatch:")
    if [content for depth, content in events if depth == 2] != expected:
        raise ValueError("unsupported hosted workflow events")
    if _block(events, "merge_group", 2) != [(4, "types: [checks_requested]")]:
        raise ValueError("merge-group event changed")
    if _block(events, "push", 2) != [(4, "branches: [ main ]")]:
        raise ValueError("main push event changed")
    inputs = _block(_block(events, "workflow_call", 2), "inputs", 4)
    if [content for depth, content in inputs if depth == 6] != ["plan_jobs:", "plan_gates:", "plan_mode:"]:
        raise ValueError("callable planner inputs changed")
    rows = [content for depth, content in _active_lines(fragment) if depth <= 2]
    filename = "live-backend-tests.yml" if static else "e2e-tests.yml"
    name = "Live Backend Tests" if static else "End-to-end Tests"
    prefix = ["schema_version: 1", "workflows:", f"- file: {filename}", f"name: {name}", "synchronize: true", "branches:", "- main", "paths:"]
    suffix = ["jobs:", "- live-backend", "- live-backend-shards", "- queue-selection", "- static-mirror", "job_names:"] if static else ["jobs:", "- queue-selection", "- test", "job_names:"]
    if rows[:8] != prefix or "jobs:" not in rows:
        raise ValueError("unsupported hosted workflow fragment")
    end = rows.index("jobs:")
    if rows[end:] != suffix:
        raise ValueError("unsupported hosted workflow fragment jobs")
    paths = rows[8:end]
    positives = set()
    narrowed = set()
    for row in paths:
        match = re.fullmatch(r"- ([A-Za-z0-9_./*?-]+)", row)
        carveout = re.fullmatch(r"- '!([A-Za-z0-9_./*?-]+)'", row)
        if carveout is not None:
            entry, target = carveout.group(1), narrowed
        elif match is not None:
            entry, target = match.group(1), positives
        else:
            raise ValueError("unsupported active path sequence")
        if entry in positives or entry in narrowed:
            raise ValueError("unsupported active path sequence")
        target.add(entry)
    if not positives:
        raise ValueError("unsupported active path sequence")
    if narrowed - {"**/*.md"}:
        raise ValueError("Nornic conformance narrows routes "
                         "beyond the reviewed docs carve-out")
    return positives


def _fragment_narrowings(fragment: str) -> set[str]:
    """Return the reviewed scheduling narrowings excluded from enrollment.

    Only the `!**/*.md` narrowing is reviewed: prose never feeds the
    conformance proof. A future narrowing needs its own review, and a
    conformance route that ever ends in `.md` must fail loudly instead of
    being silently shadowed by the carve-out.
    """
    rows = [content for depth, content in _active_lines(fragment) if depth <= 2]
    if "jobs:" not in rows:
        raise ValueError("unsupported hosted workflow fragment jobs")
    paths = rows[8:rows.index("jobs:")]
    narrowed = set()
    for row in paths:
        carveout = re.fullmatch(r"- '!([A-Za-z0-9_./*?-]+)'", row)
        if carveout is not None:
            if carveout.group(1) in narrowed:
                raise ValueError("unsupported active path sequence")
            narrowed.add(carveout.group(1))
    return narrowed


def _registry_paths(source: str) -> set[str]:
    """Read the static mirror's effective trigger list and blocking controls."""
    lines = _active_lines(source)
    if [content for depth, content in lines if depth == 0] != [
        "version: v1", "gates:"
    ]:
        raise ValueError("unsupported registry root")
    gates = _block(lines, "gates", 0)
    starts = [index for index, (depth, content) in enumerate(gates)
              if depth == 2 and content.split(":", 1)[0].strip() == "- id"
              and content.partition(":")[2].strip().strip("\"'")
              == "live-backend-static-mirror"]
    if len(starts) != 1 or gates[starts[0]][1] != "- id: live-backend-static-mirror":
        raise ValueError("missing static mirror registry gate")
    end = next((index for index in range(starts[0] + 1, len(gates))
                if gates[index][0] <= 2), len(gates))
    gate = gates[starts[0]:end]
    headers = [content for depth, content in gate if depth == 4]
    if [content.split(":", 1)[0] for content in headers] != [
        "name", "category", "layer", "purpose", "tier", "blocking",
        "triggers", "local", "ci", "requirements", "ci_only_reason"
    ] or "blocking: true" not in headers or "tier: pre-pr" not in headers:
        raise ValueError("static mirror registry controls changed")
    local = _block(gate, "local", 4)
    if len(local) != 2 or local[0] != (6, 'command: "bash scripts/verify-live-tests-ledger.sh"') \
            or local[1][0] != 6 or not local[1][1].startswith('test_command: "') \
            or "python3 scripts/test-nornic-conformance-events.py" not in local[1][1]:
        raise ValueError("static mirror no longer tests conformance guard")
    if _block(gate, "ci", 4) != [
        (6, "workflow: live-backend-tests.yml"),
        (6, 'job: "static-mirror (ledger and runner probes)"'),
    ]:
        raise ValueError("static mirror hosted owner changed")
    return _paths(_block(gate, "triggers", 4), 6)


def _static_step(source: str) -> None:
    """Require the guard test in the active static mirror job."""
    jobs = _block(_active_lines(source), "jobs", 0)
    job = _block(jobs, "static-mirror", 2)
    headers = [content.split(":", 1)[0] for depth, content in job if depth == 4]
    if headers != ["if", "needs", "name", "runs-on", "timeout-minutes", "steps"]:
        raise ValueError("static mirror job controls changed")
    if (4, "if: ${{ github.event_name != 'pull_request' || contains(fromJSON(inputs.plan_jobs || '[]'), 'static-mirror') }}") not in job:
        raise ValueError("static mirror planner selection changed")
    steps = _block(job, "steps", 4)
    starts = [index for index, (depth, content) in enumerate(steps)
              if depth == 6 and content == "- name: Test Nornic conformance enrollment"]
    if len(starts) != 1:
        raise ValueError("missing active Nornic static mirror step")
    end = next((index for index in range(starts[0] + 1, len(steps))
                if steps[index][0] <= 6), len(steps))
    if steps[starts[0]:end] != [
        (6, "- name: Test Nornic conformance enrollment"),
        (8, "if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'static-mirror (ledger and runner probes)')) }}"),
        (8, "run: python3 scripts/test-nornic-conformance-events.py"),
    ]:
        raise ValueError("Nornic static mirror step is inactive or suppressed")


def verify_routing_text(e2e: str, live: str, registry: str,
                        e2e_fragment: str | None = None,
                        live_fragment: str | None = None) -> None:
    """Require edits to select the hosted proof and its blocking static guard."""
    if e2e_fragment is None:
        e2e_fragment = (ROOT / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text()
    if live_fragment is None:
        live_fragment = (ROOT / "specs/ci-pr-workflows.d/live-backend-tests.yml").read_text()
    e2e_paths = _workflow_paths(e2e, static=False, fragment=e2e_fragment)
    live_paths = _workflow_paths(live, static=True, fragment=live_fragment)
    if any(route.endswith(".md") for route in ROUTED_PATHS):
        raise ValueError("Nornic conformance docs carve-out shadows a route")
    gate_paths = _registry_paths(registry)
    _static_step(live)
    for label, paths in (("e2e", e2e_paths), ("static workflow", live_paths),
                         ("static registry", gate_paths)):
        missing = ROUTED_PATHS - paths
        if missing:
            raise ValueError(f"{label} lacks Nornic conformance route: {sorted(missing)}")


def load_inventory(path: Path = INVENTORY) -> dict[str, list[str]]:
    """Load the exact reviewed file-to-test mapping."""
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or set(value) != EXPECTED_FILES:
        raise ValueError("Nornic conformance inventory must name the reviewed files")
    if any(
        not isinstance(path, str)
        or not path.startswith("go/internal/storage/cypher/")
        or not isinstance(tests, list)
        or not tests
        or not all(isinstance(name, str) and name.startswith("Test") for name in tests)
        or len(tests) != len(set(tests))
        for path, tests in value.items()
    ):
        raise ValueError("invalid Nornic conformance inventory")
    names = [name for tests in value.values() for name in tests]
    if len(names) != 9 or len(set(names)) != 9:
        raise ValueError("Nornic conformance inventory must contain nine distinct tests")
    return value


def verify_source(root: Path, inventory: dict[str, list[str]]) -> None:
    """Catch added, removed, or renamed Test declarations in enrolled files."""
    for relative, expected in inventory.items():
        actual = TEST_PATTERN.findall((root / relative).read_text(encoding="utf-8"))
        if sorted(actual) != sorted(expected) or len(actual) != len(set(actual)):
            raise ValueError(f"Nornic source Test inventory drift: {relative}: {actual}")


def read_ledger_rows(root: Path) -> dict[str, tuple[str, str, str]]:
    """Read canonical class, runner, and backend fields from ledger fragments."""
    rows: dict[str, tuple[str, str, str]] = {}
    for fragment in (root / "specs/live-tests.d").glob("live-tests-*.yaml"):
        text = fragment.read_text(encoding="utf-8")
        matches = list(re.finditer(r"(?m)^  - file: (\S+)\n", text))
        for index, match in enumerate(matches):
            body = text[match.end():matches[index + 1].start() if index + 1 < len(matches) else len(text)]
            fields = dict(re.findall(r"(?m)^    (class|runner|backends): (\S+)$", body))
            rows[match.group(1)] = (
                fields.get("class", ""), fields.get("runner", ""),
                fields.get("backends", ""),
            )
    return rows


def verify_ledger(
    inventory: dict[str, list[str]], rows: dict[str, tuple[str, str, str]]
) -> None:
    """Require closed runner membership even if the exemption digest is changed."""
    owned = {path for path, row in rows.items() if row[1] == "backend-conformance-nornicdb"}
    if owned != set(inventory):
        raise ValueError(f"Nornic conformance runner/ledger mismatch: {sorted(owned ^ set(inventory))}")
    for path in inventory:
        if rows.get(path) != ("dedicated_ci", "backend-conformance-nornicdb", "nornicdb"):
            raise ValueError(f"Nornic conformance row has wrong class or backend: {path}")



def verify_source_and_runner(root: Path, inventory: dict[str, list[str]]) -> None:
    """Check source, ledger, shell invocation, and hosted step together."""
    from nornic_conformance_runner import (
        verify_runner_text,
        verify_workflow_text,
    )
    verify_source(root, inventory)
    verify_ledger(inventory, read_ledger_rows(root))
    verify_runner_text((root / RUNNER).read_text(encoding="utf-8"), inventory)
    verify_workflow_text((root / WORKFLOW).read_text(encoding="utf-8"),
                         (root / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text())
    verify_routing_text(
        (root / WORKFLOW).read_text(encoding="utf-8"),
        (root / STATIC_WORKFLOW).read_text(encoding="utf-8"),
        (root / REGISTRY).read_text(encoding="utf-8"),
        (root / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text(),
        (root / "specs/ci-pr-workflows.d/live-backend-tests.yml").read_text(),
    )


def verify_events(
    path: Path,
    expected: set[str],
    packages: frozenset[str],
    output: TextIO,
) -> bool:
    """Require one run/pass per test, one package pass per package, no skip."""
    runs = {name: 0 for name in expected}
    endings: dict[str, list[str]] = {name: [] for name in expected}
    package_endings: dict[str, list[str]] = {}
    errors: list[str] = []
    try:
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                errors.append(f"malformed event line {number}")
                continue
            if (
                not isinstance(event, dict)
                or event.get("Package") not in packages
            ):
                errors.append(f"wrong package or event shape at line {number}")
                continue
            action, name = event.get("Action"), event.get("Test")
            if action in {"fail", "skip"}:
                errors.append(f"failed or skipped event at line {number}: {name}")
            if name is None:
                if action in {"pass", "fail", "skip"}:
                    package_endings.setdefault(event["Package"], []).append(action)
            elif name in expected:
                if action == "run":
                    runs[name] += 1
                elif action in {"pass", "fail", "skip"}:
                    endings[name].append(action)
            elif isinstance(name, str) and name.split("/", 1)[0] not in expected:
                errors.append(f"unexpected selected Test at line {number}: {name}")
    except OSError as error:
        errors.append(f"missing event stream: {error}")
    for name in sorted(expected):
        if runs[name] != 1 or endings[name] != ["pass"]:
            errors.append(f"{name}: run={runs[name]} terminal={endings[name]}")
    for package in sorted(packages):
        if package_endings.get(package) != ["pass"]:
            errors.append(
                f"package terminal={package_endings.get(package)} for {package}"
            )
    for error in errors:
        print(error, file=output)
    if not errors:
        print(f"Nornic conformance: {len(expected)} tests PASS", file=output)
    return not errors


def main() -> int:
    """Check runner enrollment, then optionally a captured Go event stream."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-check", action="store_true")
    parser.add_argument("--events", type=Path)
    args = parser.parse_args()
    try:
        inventory = load_inventory()
        verify_source_and_runner(ROOT, inventory)
        if args.events is None:
            return 0
        names = {name for tests in inventory.values() for name in tests}
        return (
            0
            if verify_events(args.events, names, PACKAGES, sys.stderr)
            else 1
        )
    except (OSError, ValueError, re.error) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
