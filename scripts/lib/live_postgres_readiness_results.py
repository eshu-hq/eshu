#!/usr/bin/env python3
"""Verify that every selected readiness proof ran and passed on PostgreSQL."""

import json
import pathlib
import re
import sys


EXPECTED = {
    "go/internal/query/supply/chain/impact/readiness_package_manifest_repo_scope_live_test.go": (
        "TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive",
        "TestSupplyChainImpactReadinessRepoArmScopeLive",
    ),
    "go/internal/query/supply/chain/impact/readiness_container_identity_live_test.go": (
        "TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive",
    ),
    "go/internal/query/supply/chain/impact/readiness_package_consumption_scope_live_test.go": (
        "TestSupplyChainImpactReadinessPackageConsumptionScopeLive",
    ),
    "go/internal/query/supply/chain/impact/readiness_scan_tier_explain_live_test.go": (
        "TestSupplyChainImpactReadinessScanTierQueryPlanLive",
        "TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive",
    ),
    "go/internal/query/content_reader_dead_code_incoming_bound_live_test.go": (
        "TestDeadCodeIncomingEntityIDsActiveRunBoundLive",
    ),
}
EXPECTED_TESTS = {test for tests in EXPECTED.values() for test in tests}
# One go test package terminal event per Go package directory of EXPECTED.
EXPECTED_PACKAGE_COUNT = len({path.rsplit("/", 1)[0] for path in EXPECTED})


def verify_ledger(ledger_path: pathlib.Path, repo_root: pathlib.Path) -> int:
    """Require the untagged rows, runner ownership, and test names."""
    ledger = ledger_path.read_text(encoding="utf-8")
    rows = re.findall(
        r"^  - file: (\S+)\n((?:    [^\n]*\n)*)",
        ledger,
        flags=re.MULTILINE,
    )
    selected = {}
    for path, body in rows:
        fields = dict(re.findall(r"^    (\w+): (.*)$", body, flags=re.MULTILINE))
        if fields.get("class") == "postgres_ci":
            selected[path] = fields
    if set(selected) != set(EXPECTED):
        print(
            "postgres_ci ledger files differ: "
            f"expected={sorted(EXPECTED)} actual={sorted(selected)}",
            file=sys.stderr,
        )
        return 1
    for path, expected_tests in EXPECTED.items():
        if selected[path].get("tag") != "~":
            print(f"postgres_ci row has unexpected tag: {path}", file=sys.stderr)
            return 1
        if selected[path].get("runner") != "live-postgres-readiness":
            print(f"postgres_ci row has unexpected runner: {path}", file=sys.stderr)
            return 1
        source = (repo_root / path).read_text(encoding="utf-8")
        actual_tests = re.findall(r"^func (Test\w+)\(", source, flags=re.MULTILINE)
        if set(actual_tests) != set(expected_tests):
            print(
                f"postgres_ci tests differ in {path}: "
                f"expected={sorted(expected_tests)} actual={sorted(actual_tests)}",
                file=sys.stderr,
            )
            return 1
    print(
        f"postgres_ci ledger selection: {len(EXPECTED)} files, "
        f"{len(EXPECTED_TESTS)} tests selected"
    )
    return 0


def verify_results(events_path: pathlib.Path) -> int:
    """Require one run and one passing terminal event for every expected test."""
    runs = {name: 0 for name in EXPECTED_TESTS}
    terminals: dict[str, list[tuple[str, float]]] = {
        name: [] for name in EXPECTED_TESTS
    }
    package_terminal = []
    output: dict[str, list[str]] = {name: [] for name in EXPECTED_TESTS}
    with events_path.open(encoding="utf-8") as events:
        for line_number, line in enumerate(events, start=1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError as error:
                print(f"invalid go test JSON line {line_number}: {error}", file=sys.stderr)
                return 1
            action = event.get("Action")
            name = event.get("Test")
            if name in EXPECTED_TESTS:
                if action == "run":
                    runs[name] += 1
                elif action in {"pass", "fail", "skip"}:
                    terminals[name].append((action, event.get("Elapsed", 0)))
                elif action == "output" and len(output[name]) < 12:
                    output[name].append(event.get("Output", "").rstrip()[:300])
            elif not name and action in {"pass", "fail", "skip"}:
                package_terminal.append(action)

    failed = False
    for name in sorted(EXPECTED_TESTS):
        terminal = terminals[name]
        if runs[name] != 1 or len(terminal) != 1:
            print(
                f"{name}: missing or duplicate event "
                f"(run={runs[name]}, terminal={len(terminal)})"
            )
            failed = True
            continue
        action, elapsed = terminal[0]
        print(f"{name}: {action.upper()} elapsed={elapsed:.3f}s")
        if action != "pass":
            failed = True
            for text in output[name][-6:]:
                print(f"  {text}")
    if package_terminal != ["pass"] * EXPECTED_PACKAGE_COUNT:
        print(
            f"package terminal: expected {EXPECTED_PACKAGE_COUNT} PASS, "
            f"actual={package_terminal}"
        )
        failed = True
    if failed:
        return 1
    print(f"live-postgres-readiness: {len(EXPECTED_TESTS)}/{len(EXPECTED_TESTS)} PASS")
    return 0


def main() -> int:
    """Dispatch the ledger and go-test event checks."""
    if len(sys.argv) == 4 and sys.argv[1] == "verify-ledger":
        return verify_ledger(pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]))
    if len(sys.argv) == 3 and sys.argv[1] == "verify-results":
        return verify_results(pathlib.Path(sys.argv[2]))
    print(
        "usage: live_postgres_readiness_results.py "
        "verify-ledger <ledger> <repo-root> | verify-results <events>",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    sys.exit(main())
