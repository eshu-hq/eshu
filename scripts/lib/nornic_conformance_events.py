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
WORKFLOW = Path(".github/workflows/e2e-tests.yml")
PACKAGE = "github.com/eshu-hq/eshu/go/internal/storage/cypher"
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
EXPECTED_FILES = frozenset({
    "go/internal/storage/cypher/cloud_resource_node_writer_heterogeneous_batch_live_test.go",
    "go/internal/storage/cypher/retrying_executor_live_test.go",
    "go/internal/storage/cypher/retrying_executor_relationship_snapshot_live_test.go",
    "go/internal/storage/cypher/semantic_entity_full_repo_retract_live_test.go",
    "go/internal/storage/cypher/semantic_entity_live_test.go",
    "go/internal/storage/cypher/semantic_entity_variable_nornicdb_live_test.go",
    "go/internal/storage/cypher/tfstate_canonical_writer_stale_attrs_live_test.go",
})


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


def verify_runner_text(text: str, inventory: dict[str, list[str]]) -> None:
    """Require the exact Nornic-only call and shell failure propagation."""
    if not text.startswith("#!/usr/bin/env bash\n\nset -euo pipefail\n"):
        raise ValueError("Nornic runner must propagate failures")
    marker = 'if [ "$ESHU_GRAPH_BACKEND" = "nornicdb" ]; then\n'
    if text.count(marker) != 1 or text.count("go test ./internal/storage/cypher") != 1:
        raise ValueError("missing or duplicate Nornic-only runner branch")
    block = text.split(marker, 1)[1].rsplit("\nfi", 1)[0]
    names = sorted(name for tests in inventory.values() for name in tests)
    selector = "|".join(names)
    continuation = chr(92)
    expected = "\n".join((
        "    set -o pipefail",
        "    python3 ../scripts/lib/nornic_conformance_events.py --source-check",
        '    echo "Running nine exact live NornicDB conformance proofs"',
        "    ESHU_NORNICDB_RETRY_CONTRACT_LIVE=1 " + continuation,
        "    ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE=1 " + continuation,
        "    ESHU_SEMANTIC_ENTITY_NORNICDB_LIVE=1 " + continuation,
        '    ESHU_CYPHER_BOLT_DSN="$ESHU_NEO4J_URI" ' + continuation,
        '    ESHU_CYPHER_BOLT_DATABASE="$ESHU_NEO4J_DATABASE" ' + continuation,
        "        go test ./internal/storage/cypher -run '^(" + selector
        + ")$' -json -count=1 -v | tee \"${RUNNER_TEMP:-/tmp}/nornic-conformance.jsonl\"",
        '    python3 ../scripts/lib/nornic_conformance_events.py --events '
        '"${RUNNER_TEMP:-/tmp}/nornic-conformance.jsonl"',
    ))
    if block != expected:
        raise ValueError("Nornic conformance runner activation or selector drift")


def verify_workflow_text(text: str) -> None:
    """Require the active failing conformance step in the hosted owner job."""
    if text.count("  test:\n") != 1 or text.count("      - name: Run live backend conformance\n") != 1:
        raise ValueError("missing hosted conformance owner")
    match = re.search(r"(?ms)^  test:\n(.*?)(?=^  [a-z][\w-]*:\n|\Z)", text)
    if match is None:
        raise ValueError("missing hosted conformance owner job")
    job = match.group(1)
    nornic_matrix = (
        "          - graph_backend: nornicdb\n"
        "            compose_file: docker-compose.yaml\n"
        "            graph_service: nornicdb\n"
        "            database: nornic\n"
    )
    if (
        text.count("jobs:\n") != 1
        or text.count("  pull_request:\n") != 1
        or text.count("  merge_group:\n") != 1
        or job.count(nornic_matrix) != 1
    ):
        raise ValueError("hosted Nornic conformance matrix or event changed")
    headers = [line for line in job.splitlines()
               if line.startswith("    ") and not line.startswith("     ")
               and line.strip() and not line.lstrip().startswith("#")]
    keys = [re.fullmatch(r"    ([a-z][a-z-]*):(?: .*)?", line) for line in headers]
    if any(key is None for key in keys) or [key.group(1) for key in keys if key] != [
        "needs", "name", "runs-on", "timeout-minutes", "strategy", "steps"
    ]:
        raise ValueError("noncanonical hosted conformance job properties")
    if re.search(r"(?m)^    (?:if|continue-on-error)\s*:", job):
        raise ValueError("hosted conformance job can be disabled")
    step = job.split("      - name: Run live backend conformance\n", 1)[1].split("\n      - name:", 1)[0]
    top = [line for line in step.splitlines() if line.startswith("        ") and not line.startswith("         ")]
    keys = [re.fullmatch(r"        ([a-z][a-z-]*):(?: .*)?", line) for line in top]
    if any(match is None for match in keys):
        raise ValueError("noncanonical hosted conformance step key")
    fields = [match.group(1) for match in keys if match]
    if fields != ["if", "env", "run"]:
        raise ValueError("hosted conformance step activation or failure suppression changed")
    if 'run: ./scripts/verify_backend_conformance_live.sh' not in step:
        raise ValueError("hosted conformance runner is not invoked")
    expected_if = "        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'test')) }}"
    if top[0] != expected_if or top[-1] != "        run: ./scripts/verify_backend_conformance_live.sh":
        raise ValueError("hosted conformance step is inactive")
    env_lines = [line for line in step.splitlines() if line.startswith("          ")]
    if env_lines != [
        "          ESHU_GRAPH_BACKEND: ${{ matrix.graph_backend }}",
        "          NEO4J_URI: bolt://localhost:7687",
        "          NEO4J_USERNAME: neo4j",
        "          NEO4J_PASSWORD: change-me",
        "          DEFAULT_DATABASE: ${{ matrix.database }}",
        "          ESHU_NEO4J_DATABASE: ${{ matrix.database }}",
        "          NEO4J_DATABASE: ${{ matrix.database }}",
    ]:
        raise ValueError("hosted conformance backend environment changed")


def verify_source_and_runner(root: Path, inventory: dict[str, list[str]]) -> None:
    """Check source, ledger, shell invocation, and hosted step together."""
    verify_source(root, inventory)
    verify_ledger(inventory, read_ledger_rows(root))
    verify_runner_text((root / RUNNER).read_text(encoding="utf-8"), inventory)
    verify_workflow_text((root / WORKFLOW).read_text(encoding="utf-8"))


def verify_events(path: Path, expected: set[str], output: TextIO) -> bool:
    """Require one run/pass per test, one package pass, and no skipped proof."""
    runs = {name: 0 for name in expected}
    endings: dict[str, list[str]] = {name: [] for name in expected}
    package_endings: list[str] = []
    errors: list[str] = []
    try:
        for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                errors.append(f"malformed event line {number}")
                continue
            if not isinstance(event, dict) or event.get("Package") != PACKAGE:
                errors.append(f"wrong package or event shape at line {number}")
                continue
            action, name = event.get("Action"), event.get("Test")
            if action in {"fail", "skip"}:
                errors.append(f"failed or skipped event at line {number}: {name}")
            if name is None:
                if action in {"pass", "fail", "skip"}:
                    package_endings.append(action)
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
    if package_endings != ["pass"]:
        errors.append(f"package terminal={package_endings}")
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
        return 0 if verify_events(args.events, names, sys.stderr) else 1
    except (OSError, ValueError, re.error) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
