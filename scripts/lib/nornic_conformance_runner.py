"""Verify the Nornic conformance runner invocation and hosted workflow text."""

from __future__ import annotations

import re
from pathlib import Path

from nornic_conformance_events import (
    ROOT,
    RUNNER_PACKAGES,
    RUNNER_PREFIX,
    _active_lines,
    _block,
    _workflow_paths,
)


def verify_runner_text(text: str, inventory: dict[str, list[str]]) -> None:
    """Require the exact Nornic-only call and shell failure propagation."""
    marker = 'if [ "$ESHU_GRAPH_BACKEND" = "nornicdb" ]; then\n'
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
        "        go test " + " ".join(sorted(RUNNER_PACKAGES)) + " -run '^(" + selector
        + ")$' -json -count=1 -v | tee \"${RUNNER_TEMP:-/tmp}/nornic-conformance.jsonl\"",
        '    python3 ../scripts/lib/nornic_conformance_events.py --events '
        '"${RUNNER_TEMP:-/tmp}/nornic-conformance.jsonl"',
    ))
    if text != RUNNER_PREFIX + marker + expected + "\nfi\n":
        raise ValueError("Nornic runner preamble, CORE, activation, or tail drift")


def verify_workflow_text(text: str, fragment: str | None = None) -> None:
    """Require the active failing conformance step in the hosted owner job."""
    if fragment is None:
        fragment = (ROOT / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text()
    if "go/**" not in _workflow_paths(text, static=False, fragment=fragment):
        raise ValueError("hosted workflow no longer selects Go source changes")
    jobs = _block(_active_lines(text), "jobs", 0)
    test_job = _block(jobs, "test", 2)
    strategy = _block(test_job, "strategy", 4)
    if strategy != [
        (6, "fail-fast: false"),
        (6, "matrix:"),
        (8, "include:"),
        (10, "- graph_backend: nornicdb"),
        (12, "compose_file: docker-compose.yaml"),
        (12, "graph_service: nornicdb"),
        (12, "database: nornic"),
        (10, "- graph_backend: neo4j"),
        (12, "compose_file: docker-compose.neo4j.yml"),
        (12, "graph_service: neo4j"),
        (12, "database: neo4j"),
    ]:
        raise ValueError("hosted backend matrix include changed")
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
        text.count("\njobs:\n") != 1
        or text.count("  workflow_call:\n") != 1
        or text.count("  merge_group:\n") != 1
        or job.count(nornic_matrix) != 1
    ):
        raise ValueError("hosted Nornic conformance matrix or event changed")
    headers = [line for line in job.splitlines()
               if line.startswith("    ") and not line.startswith("     ")
               and line.strip() and not line.lstrip().startswith("#")]
    keys = [re.fullmatch(r"    ([a-z][a-z-]*):(?: .*)?", line) for line in headers]
    if any(key is None for key in keys) or [key.group(1) for key in keys if key] != [
        "if", "needs", "name", "runs-on", "timeout-minutes", "strategy", "steps"
    ]:
        raise ValueError("noncanonical hosted conformance job properties")
    condition = "    if: ${{ github.event_name != 'pull_request' || contains(fromJSON(inputs.plan_jobs || '[]'), 'test') }}"
    if headers.count(condition) != 1 or re.search(r"(?m)^    continue-on-error\s*:", job):
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
