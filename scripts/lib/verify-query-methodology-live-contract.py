#!/usr/bin/env python3
"""Bind dedicated methodology live proofs to source, runner, and CI events."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path


GRAPH_FILES = (
    "methodology_graph_artifact_live_test.go",
    "methodology_graph_cases_live_test.go",
    "methodology_graph_cycle_cap_live_test.go",
    "methodology_graph_fixture_live_test.go",
    "methodology_graph_live_test.go",
    "methodology_graph_measure_live_test.go",
    "methodology_graph_oracle_live_test.go",
    "methodology_graph_profile_live_test.go",
)
GRAPH_ROOT = "TestImportDependencyMethodologyLive"
MCP_ROOT = "TestImportDependencyMethodologyMCPTerminalCapLive"
PG_ROOT = "TestQueryMethodologyPostgresLive"
EXPECTED = {
    "go/internal/mcp/methodology_live_test.go": (
        "queryplan_profile_live", "mcp", MCP_ROOT, "neo4j"
    ),
    **{
        f"go/internal/query/{name}": (
            "queryplan_profile_live",
            "query",
            GRAPH_ROOT if name == "methodology_graph_live_test.go" else "",
            "neo4j",
        )
        for name in GRAPH_FILES
    },
    "go/internal/query/methodology_postgres_live_test.go": (
        "integration", "query", PG_ROOT, ""
    ),
}


def require(condition: bool, message: str) -> None:
    """Reject a broken proof binding with a named cause."""
    if not condition:
        raise ValueError(message)


def check_events(path: Path, root_name: str) -> None:
    """Require one real Go JSON run and PASS for a selected root."""
    require(
        root_name in (GRAPH_ROOT, MCP_ROOT, PG_ROOT),
        f"unknown methodology root: {root_name}",
    )
    runs = 0
    passes = 0
    for number, raw in enumerate(path.read_text().splitlines(), 1):
        try:
            event = json.loads(raw)
        except json.JSONDecodeError as error:
            raise ValueError(
                f"invalid Go JSON event at line {number}: {error}"
            ) from error
        require(isinstance(event, dict), f"non-object Go event at line {number}")
        test = event.get("Test", "")
        require(isinstance(test, str), f"invalid Go test name at line {number}")
        action = event.get("Action")
        if test == root_name and action == "run":
            runs += 1
        if test == root_name and action == "pass":
            passes += 1
        if (test == root_name or test.startswith(root_name + "/")) and action in (
            "skip", "fail"
        ):
            raise ValueError(f"methodology root skipped or failed: {test}")
    require(
        runs == 1 and passes == 1,
        f"methodology root needs one run and PASS: {root_name}; "
        f"runs={runs} passes={passes}",
    )


def check_static(root: Path) -> None:
    """Verify every dedicated file and the executable blocking route."""
    ledger = (root / "specs/live-tests.v1.yaml").read_text()
    rows = re.findall(
        r"^  - file: (\S+)\n"
        r"    tag: (.*)\n"
        r"    class: dedicated_ci\n"
        r"    reason: ([^\n]+)\n"
        r"    runner: (\S+)"
        r"(?:\n    backends: (\S+))?",
        ledger,
        re.M,
    )
    paths = [row[0] for row in rows]
    require(
        len(paths) == len(set(paths)) and set(paths) == set(EXPECTED),
        "dedicated file set differs from methodology contract",
    )
    for path, tag, reason, runner, backends in rows:
        wanted_tag, wanted_package, wanted_root, wanted_backend = EXPECTED[path]
        require(
            tag == wanted_tag
            and runner == "query-methodology"
            and backends == wanted_backend,
            f"dedicated ledger metadata differs: {path}",
        )
        require(bool(reason.strip()), f"dedicated reason empty: {path}")
        source = (root / path).read_text()
        build = re.search(r"^//go:build (.+)$", source, re.M)
        package = re.search(r"^package (\w+)$", source, re.M)
        tests = re.findall(r"^func\s+(Test\w+)\s*\(", source, re.M)
        require(
            build is not None and build.group(1) == wanted_tag,
            f"actual build tag differs: {path}",
        )
        require(
            package is not None and package.group(1) == wanted_package,
            f"actual Go package differs: {path}",
        )
        require(
            tests == ([wanted_root] if wanted_root else []),
            f"actual root Test set differs: {path}: {tests}",
        )

    runner = (root / "scripts/verify-query-methodology.sh").read_text()
    runner_lines = runner.splitlines()
    static_call = (
        'python3 "$repo_root/scripts/lib/'
        'verify-query-methodology-live-contract.py" --root "$repo_root"'
    )
    require(
        static_call in runner_lines,
        "static contract validator missing from methodology runner",
    )
    identity_order = (
        'export ESHU_QUERY_METHODOLOGY_IDENTITY="$artifact_dir/identity.json"',
        "export ESHU_QUERY_METHODOLOGY_REQUIRED=1",
        'rm -f "$ESHU_QUERY_METHODOLOGY_IDENTITY"',
        'python3 "$repo_root/scripts/lib/query-methodology-comparison-identity.py" --root "$repo_root" --output "$ESHU_QUERY_METHODOLOGY_IDENTITY"',
        '[[ -s "$ESHU_QUERY_METHODOLOGY_IDENTITY" ]] || { printf \'missing frozen methodology comparison identity\\n\' >&2; exit 1; }',
        static_call,
        "go_test_run_guard 1 '^TestMethodologyProductionSourceTreesMatchBase$' -- -tags queryplan_profile_live ./internal/query -count=1 -v",
        "go test ./internal/queryplan -count=1",
    )
    positions = []
    for line in identity_order:
        require(runner_lines.count(line) == 1, f"methodology runner lost frozen identity or parity step: {line}")
        positions.append(runner_lines.index(line))
    require(positions == sorted(positions), "methodology identity must freeze before any producer or parity check")
    go_call = (
        ' if ! go test -json -tags "$tag" -run "^${root_name}$" '
        '"$package" -count=1 -timeout="$timeout" > "$events"; then'
    )
    event_call = (
        ' python3 "$repo_root/scripts/lib/'
        'verify-query-methodology-live-contract.py" --events "$events" '
        '--root-name "$root_name"'
    )
    require(go_call in runner_lines, "methodology runner lost exact Go JSON invocation")
    require(event_call in runner_lines, "methodology runner lost PASS event verification")
    for name, tag, package, timeout in (
        (PG_ROOT, "integration", "./internal/query", "5m"),
        (GRAPH_ROOT, "queryplan_profile_live", "./internal/query", "10m"),
        (MCP_ROOT, "queryplan_profile_live", "./internal/mcp", "5m"),
    ):
        call = f"methodology_live_test_run {name} {tag} {package} {timeout}"
        require(
            runner_lines.count(call) == 1,
            f"methodology runner missing exact invocation: {name}",
        )
    calls = re.findall(r"^methodology_live_test_run (\S+)", runner, re.M)
    require(
        calls == [PG_ROOT, GRAPH_ROOT, MCP_ROOT],
        f"methodology runner selected roots differ: {calls}",
    )
    for setup in (
        'docker run --rm -d --name "$postgres" -p 127.0.0.1::5432',
        'docker run --rm -d --name "$graph" -p 127.0.0.1::7687',
        ' docker rm -f "$postgres" "$graph"',
        "trap cleanup EXIT INT TERM",
        "export ESHU_POSTGRES_TEST_DSN=",
        "export ESHU_QUERY_METHODOLOGY_LIVE=1 ESHU_QUERYPLAN_PROFILE_ISOLATED=1",
        "export ESHU_NEO4J_URI=",
        "go_test_run_guard 1 '^TestPilotArtifactFiles$'",
    ):
        require(
            any(line.startswith(setup) for line in runner_lines),
            f"methodology runner lost isolation or artifact setup: {setup}",
        )

    workflow = (root / ".github/workflows/test.yml").read_text()
    verify_job = re.search(
        r"^  verify-contracts:\n(.*?)(?=^  [\w-]+:|\Z)",
        workflow,
        re.M | re.S,
    )
    require(verify_job is not None, "verify-contracts workflow job missing")
    job_lines = verify_job.group(1).splitlines()
    require(
        "          scripts/verify-query-plan-regression.sh" in job_lines
        and "          python3 scripts/test-verify-query-methodology-live-contract.py" in job_lines
        and "          python3 scripts/test-query-methodology-comparison-identity.py"
        in job_lines
        and "          fetch-depth: 0" in job_lines
        and "          path: .proof-artifacts/query-methodology/*.json" in job_lines
        and "        uses: actions/setup-python@v6" in job_lines
        and "        run: python -m pip install pre-commit==4.6.2" in job_lines,
        "blocking verify-contracts workflow lost proof, self-test, or hook tool",
    )
    registry = (root / "specs/ci-gates.v1.yaml").read_text()
    for gate_id in ("query-methodology-static", "query-plan-regression"):
        gate = re.search(
            rf"^  - id: {gate_id}\n(.*?)(?=^  - id:|\Z)", registry, re.M | re.S
        )
        require(gate is not None, f"{gate_id} gate missing")
        body = gate.group(1).splitlines()
        anchors = [
            "    blocking: true",
            "      workflow: test.yml",
            '      job: "verify-contracts"',
            '      - "go/internal/mcp/methodology_live_test.go"',
            '      - "scripts/lib/verify-query-methodology-live-contract.py"',
            '      - "scripts/test-verify-query-methodology-live-contract.py"',
            '      - "scripts/lib/query-methodology-comparison-identity.py"',
            '      - "scripts/test-query-methodology-comparison-identity.py"',
        ]
        if gate_id == "query-plan-regression":
            anchors.append('      command: "bash scripts/verify-query-plan-regression.sh"')
        for anchor in anchors:
            require(anchor in body, f"blocking {gate_id} route or trigger missing: {anchor}")
    regression = (root / "scripts/verify-query-plan-regression.sh").read_text()
    require(
        '"${repo_root}/scripts/verify-query-methodology.sh" --live'
        in regression.splitlines(),
        "query-plan-regression lost dedicated live runner call",
    )


def main() -> int:
    """Validate static ownership or one captured Go JSON root result."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path)
    parser.add_argument("--events", type=Path)
    parser.add_argument("--root-name")
    args = parser.parse_args()
    try:
        if args.events is not None:
            require(args.root_name is not None, "--events requires --root-name")
            check_events(args.events, args.root_name)
        else:
            require(args.root is not None, "static validation requires --root")
            check_static(args.root)
    except (OSError, ValueError) as error:
        print(f"query-methodology live contract: {error}", file=sys.stderr)
        return 1
    print("query-methodology live contract: pass")
    return 0


if __name__ == "__main__":
    sys.exit(main())
