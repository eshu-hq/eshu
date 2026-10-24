#!/usr/bin/env python3
"""Verify the exact delta union source, hosted route, and Go event accounting."""

import argparse
import hashlib
from pathlib import Path
import re
import sys

from reducer_contention_events import canonical_keys, verify_events

WORKFLOW = Path(".github/workflows/delta-union-scheduled.yml")
WORKFLOW_SHA256 = "8331fed7228bf58a09e6635864c47ab3f1812447609f1ff7933bea7e5a40b56d"
SOURCE = Path("go/cmd/projector/delta_union_live_test.go")
PACKAGE = "github.com/eshu-hq/eshu/go/cmd/projector"
TESTS = (
    "TestDeltaUnionProjectsFullTreeFromEmptyLive",
    "TestDeltaUnionBaseMissingRefusesWithoutPartialLive",
)
SELECTOR = "^(" + "|".join(TESTS) + ")$"
PR_PATHS = (
    ".github/workflows/delta-union-scheduled.yml",
    "go/cmd/projector/**", "go/internal/projector/**",
    "go/internal/storage/cypher/**", "go/internal/projector/runtime/**",
    "go/internal/facts/**", "go/internal/collector/**",
    "go/go.mod", "go/go.sum", "scripts/ci/go-mod-download-retry.sh",
    "scripts/lib/delta_union_scheduled.py",
    "scripts/lib/live_tests_legacy_policy.py", "scripts/test-delta-union-scheduled.py",
    "scripts/lib/reducer_contention_events.py",
    "scripts/lib/verify-live-tests-ledger.py", "scripts/verify-live-tests-ledger.sh",
    "specs/live-tests.v1.yaml", "specs/live-tests.d/**",
    "specs/ci-gates.v1.yaml", "specs/ci-gates.d/**",
)
PG_IMAGE = "ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c"
NEO4J_IMAGE = "ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f"


def verify(root: Path, workflow: str | None = None) -> None:
    """Reject any missing proof, activation route, service pin, or verifier."""
    source = (root / SOURCE).read_text(encoding="utf-8")
    actual = tuple(re.findall(r"(?m)^func (Test\w+)\(", source))
    if set(actual) != set(TESTS) or len(actual) != len(TESTS):
        raise ValueError(f"delta union source inventory changed: {actual}")
    if workflow is None:
        workflow = (root / WORKFLOW).read_text(encoding="utf-8")
    prefix, separator, jobs = workflow.partition("\njobs:\n")
    if not separator or jobs.count("\n  delta-union:\n") or not jobs.startswith("  delta-union:\n"):
        raise ValueError("delta union owner job missing")
    if re.search(r"(?m)^  [a-z][\w-]*:", jobs[2:]):
        raise ValueError("extra or inactive owner job")
    root_keys = canonical_keys(prefix + "\njobs:\n", 0,
                               frozenset({"name", "on", "concurrency", "permissions", "jobs"}))
    if set(root_keys) != {"name", "on", "concurrency", "permissions", "jobs"}:
        raise ValueError("noncanonical delta union workflow root")
    job_keys = canonical_keys(jobs, 4,
                              frozenset({"name", "runs-on", "timeout-minutes", "services", "steps"}))
    if set(job_keys) != {"name", "runs-on", "timeout-minutes", "services", "steps"}:
        raise ValueError("noncanonical delta union owner job")
    if re.search(r"(?m)^\s+(?:continue-on-error|if):", jobs.split("    steps:\n", 1)[0]):
        raise ValueError("disabled owner job")
    if re.search(r"(?m)^        (?:continue-on-error|if):", jobs):
        # The artifact step is the only conditional step.
        conditions = re.findall(r"(?m)^        if: (.+)$", jobs)
        if conditions != ["${{ always() }}"] or "continue-on-error:" in jobs:
            raise ValueError("disabled or failure-suppressed proof step")
    expected_routes = ("on:\n  schedule:\n", "  workflow_dispatch:\n",
                       "  pull_request:\n    branches: [ main ]\n    paths:\n")
    if any(route not in prefix for route in expected_routes):
        raise ValueError("missing scheduled, manual, or scoped PR route")
    paths = re.findall(r"(?m)^      - '([^']+)'$", prefix)
    if tuple(paths) != PR_PATHS:
        raise ValueError("scoped PR dependency route changed")
    required = (
        "    runs-on: ubuntu-latest\n", "    timeout-minutes: 20\n",
        f"        image: {PG_IMAGE}\n", f"        image: {NEO4J_IMAGE}\n",
        "          POSTGRES_DB: eshu_delta_union\n",
        "          POSTGRES_USER: eshu\n", "          POSTGRES_PASSWORD: change-me\n",
        "          NEO4J_AUTH: none\n", "          - 15432:5432\n",
        "          - 17687:7687\n", "      - name: Checkout repository\n",
        "        uses: actions/checkout@v5\n", "          persist-credentials: false\n",
        "      - name: Set up Go\n", "        uses: actions/setup-go@v6\n",
        "          go-version-file: go/go.mod\n", "      - name: Run delta union proofs\n",
        "        working-directory: go\n",
        "          ESHU_7389_PROOF_POSTGRES_DSN: postgres://eshu:change-me@localhost:15432/eshu_delta_union?sslmode=disable\n",
        "          ESHU_NEO4J_URI: bolt://localhost:17687\n",
        "          ESHU_NEO4J_USERNAME: neo4j\n",
        "          ESHU_NEO4J_PASSWORD: change-me\n",
        "          ESHU_NEO4J_DATABASE: neo4j\n",
        "          set -euo pipefail\n",
        f"          go test ./cmd/projector -run '{SELECTOR}' -json -race -count=1 -p 4 -timeout 12m | tee \"${{RUNNER_TEMP}}/delta-union.jsonl\"\n",
        '          python3 ../scripts/lib/delta_union_scheduled.py --events "${RUNNER_TEMP}/delta-union.jsonl"\n',
        "      - name: Record backend and checkout metadata\n",
        "      - name: Upload delta union events\n",
        "            ${{ runner.temp }}/delta-union.jsonl\n",
        "            ${{ runner.temp }}/delta-union-meta.txt\n",
        "        uses: actions/upload-artifact@v4\n",
        "          if-no-files-found: error\n",
    )
    if any(item not in jobs for item in required):
        raise ValueError("missing pinned service, selector, event verifier, or artifact route")
    step_names = re.findall(r"(?m)^      - name: (.+)$", jobs)
    if step_names != ["Checkout repository", "Verify delta union contract", "Set up Go",
                      "Pre-warm Go modules", "Record backend and checkout metadata",
                      "Run delta union proofs", "Upload delta union events"]:
        raise ValueError("delta union proof steps changed")
    headers = list(re.finditer(r"(?m)^      - name: (.+)\n", jobs))
    sections = {
        match.group(1): jobs[match.end():headers[index + 1].start()]
        if index + 1 < len(headers) else jobs[match.end():]
        for index, match in enumerate(headers)
    }
    step_keys = {
        "Checkout repository": {"uses", "with"},
        "Verify delta union contract": {"run"},
        "Set up Go": {"uses", "with"},
        "Pre-warm Go modules": {"run"},
        "Record backend and checkout metadata": {"run"},
        "Run delta union proofs": {"working-directory", "env", "run"},
        "Upload delta union events": {"if", "uses", "with"},
    }
    for name, allowed in step_keys.items():
        keys = canonical_keys(sections[name], 8, frozenset(allowed))
        if set(keys) != allowed:
            raise ValueError(f"noncanonical delta union step properties: {name}")
    checkout = sections["Checkout repository"]
    if (checkout.count("        with:\n") != 1
            or checkout.split("        with:\n", 1)[1]
            != "          persist-credentials: false\n"):
        raise ValueError("checkout must use the workflow revision without overrides")
    # These blocks are the active proof route. Require their complete command
    # body so an early successful exit cannot leave expected tokens inert.
    expected_runs = {
        "Verify delta union contract": (
            "          python3 scripts/test-delta-union-scheduled.py\n"
            "          python3 scripts/lib/delta_union_scheduled.py\n"
            "          bash scripts/verify-live-tests-ledger.sh\n"
        ),
        "Record backend and checkout metadata": (
            '          set -euo pipefail\n'
            '          checkout_sha="$(git rev-parse HEAD)"\n'
            '          go_version="$(go version)"\n'
            '          pg_image="$(docker inspect --format \'{{.Image}}\' \'${{ job.services.postgres.id }}\')"\n'
            '          pg_version="$(docker exec \'${{ job.services.postgres.id }}\' psql -U eshu -d eshu_delta_union -tAc \'SHOW server_version\')"\n'
            '          neo_image="$(docker inspect --format \'{{.Image}}\' \'${{ job.services.neo4j.id }}\')"\n'
            '          neo_version="$(docker exec \'${{ job.services.neo4j.id }}\' cypher-shell -a bolt://localhost:7687 \'CALL dbms.components() YIELD name, versions RETURN name, versions\')"\n'
            '          test -n "$checkout_sha" && test -n "$go_version" && test -n "$pg_image" && test -n "$pg_version" && test -n "$neo_image" && test -n "$neo_version"\n'
            '          {\n'
            '            printf \'checkout_sha=%s\\n\' "$checkout_sha"\n'
            '            printf \'go_version=%s\\n\' "$go_version"\n'
            '            printf \'postgres_image_id=%s\\n\' "$pg_image"\n'
            '            printf \'postgres_version=%s\\n\' "$pg_version"\n'
            '            printf \'neo4j_image_id=%s\\n\' "$neo_image"\n'
            '            printf \'neo4j_version=%s\\n\' "$neo_version"\n'
            '          } > "${RUNNER_TEMP}/delta-union-meta.txt"\n'
        ),
        "Run delta union proofs": (
            "          set -euo pipefail\n"
            "          started=${SECONDS}\n"
            f"          go test ./cmd/projector -run '{SELECTOR}' -json -race -count=1 -p 4 -timeout 12m | tee \"${{RUNNER_TEMP}}/delta-union.jsonl\"\n"
            '          python3 ../scripts/lib/delta_union_scheduled.py --events "${RUNNER_TEMP}/delta-union.jsonl"\n'
            '          printf \'proof_wall_seconds=%s\\n\' "$((SECONDS-started))" >> "${RUNNER_TEMP}/delta-union-meta.txt"\n'
        ),
    }
    for name, expected in expected_runs.items():
        section = sections[name]
        if section.count("        run: |\n") != 1 or section.split("        run: |\n", 1)[1] != expected:
            raise ValueError(f"non-fail-closed delta union step command body: {name}")
    # Reviewed workflow bytes close YAML surfaces outside the narrow semantic
    # checks above, including service options and nested action inputs.
    if hashlib.sha256(workflow.encode("utf-8")).hexdigest() != WORKFLOW_SHA256:
        raise ValueError("delta union reviewed workflow bytes changed")


def main() -> int:
    """Check route or verify one captured Go JSON event stream."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--events", type=Path)
    args = parser.parse_args()
    try:
        verify(args.repo_root)
        if args.events is None:
            print(f"delta union scheduled: {len(TESTS)} proofs enrolled")
            return 0
        return 0 if verify_events(args.events, PACKAGE, set(TESTS), sys.stderr) else 1
    except (OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
