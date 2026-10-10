#!/usr/bin/env python3
"""Verify that every selected readiness proof ran and passed on PostgreSQL."""

import pathlib
import re
import sys

from live_postgres_readiness_events import verify_results
from live_tests_registry import load_ledger_text

from live_postgres_readiness_inventory import (
    EXPECTED, PACKAGES, TOTAL_TESTS, neo4j_tests, package_records,
)

def list_packages() -> int:
    """Print one tab-separated record per package for the runner.

    Fields: Go package path, anchored go test -run pattern, then the expected
    test names separated by spaces. Names are \\w only, so no field needs
    escaping.
    """
    try:
        records = package_records()
    except ValueError as error:
        print(f"list-packages: {error}", file=sys.stderr)
        return 1
    for package, tests in records:
        print(f"{package}\t^({'|'.join(tests)})$\t{' '.join(tests)}")
    return 0


def list_neo4j_tests() -> int:
    """Print the Neo4j-backed test names separated by spaces."""
    print(" ".join(neo4j_tests()))
    return 0


def parse_skips(args: list[str]) -> tuple[set[str], str]:
    """Parse repeated `--skip NAME` flags, failing closed on bad input."""
    skipped: set[str] = set()
    rest = list(args)
    while rest:
        flag = rest.pop(0)
        if flag != "--skip" or not rest:
            return set(), f"expected `--skip NAME`, got {flag!r}"
        skipped.add(rest.pop(0))
    known = {test for tests in EXPECTED.values() for test in tests}
    unknown = sorted(skipped - known)
    if unknown:
        return set(), f"unknown --skip test(s): {', '.join(unknown)}"
    return skipped, ""


def verify_ledger(ledger_path: pathlib.Path, repo_root: pathlib.Path) -> int:
    """Require the untagged rows, runner ownership, and test names."""
    try:
        ledger = load_ledger_text(ledger_path)
    except ValueError as error:
        print(f"verify-ledger: {error}", file=sys.stderr)
        return 1
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
        f"{TOTAL_TESTS} tests selected"
    )
    return 0


def main() -> int:
    """Dispatch the ledger, per-package go-test event, and summary checks."""
    if len(sys.argv) == 4 and sys.argv[1] == "verify-ledger":
        return verify_ledger(pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]))
    if len(sys.argv) >= 4 and sys.argv[1] == "verify-results" and sys.argv[3] in PACKAGES:
        skipped, error = parse_skips(sys.argv[4:])
        if error:
            print(f"verify-results: {error}", file=sys.stderr)
            return 2
        return verify_results(pathlib.Path(sys.argv[2]), sys.argv[3], PACKAGES, skipped)
    if len(sys.argv) == 2 and sys.argv[1] == "list-packages":
        return list_packages()
    if len(sys.argv) == 2 and sys.argv[1] == "list-neo4j-tests":
        return list_neo4j_tests()
    if len(sys.argv) >= 2 and sys.argv[1] == "summary":
        skipped, error = parse_skips(sys.argv[2:])
        if error:
            print(f"summary: {error}", file=sys.stderr)
            return 2
        passed = TOTAL_TESTS - len(skipped)
        if skipped:
            names = " ".join(sorted(skipped))
            print(f"live-postgres-readiness: {passed}/{TOTAL_TESTS} PASS (skipped: {names})")
        else:
            print(f"live-postgres-readiness: {TOTAL_TESTS}/{TOTAL_TESTS} PASS")
        return 0
    print(
        "usage: live_postgres_readiness_results.py "
        "verify-ledger <ledger> <repo-root> | verify-results <events> <package> [--skip NAME]... "
        "| list-packages | list-neo4j-tests | summary [--skip NAME]...",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    sys.exit(main())
