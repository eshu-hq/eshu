#!/usr/bin/env python3
"""Verify that every selected readiness proof ran and passed on PostgreSQL."""

import hashlib
import pathlib
import re
import sys

from live_postgres_readiness_events import verify_results
from live_postgres_readiness_inventory_helpers import HELPER_PROOFS
from live_tests_registry import load_ledger_text

from live_postgres_readiness_records import (
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


def parse_expect(args: list[str]) -> tuple[list[str] | None, list[str], str]:
    """Parse an optional leading `--expect CSV` subset, failing closed on bad input."""
    if len(args) >= 2 and args[0] == "--expect":
        names = args[1].split(",")
        if any(not re.fullmatch(r"Test\w+", name) for name in names):
            return None, [], f"malformed --expect test name in {args[1]!r}"
        known = {test for tests in EXPECTED.values() for test in tests}
        unknown = sorted(set(names) - known)
        if unknown:
            return None, [], f"unknown --expect test(s): {', '.join(unknown)}"
        if len(set(names)) != len(names):
            return None, [], "duplicate --expect test(s)"
        return names, args[2:], ""
    return None, args, ""


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


HELPER_PATH = "go/internal/query/supply/chain/impact/suppression_plan_proof_live_test.go"
CONSUMER_PATH = "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go"
CONSUMER_TEST = "TestSupplyChainSuppressionPathsPerformanceLive"
HELPER_FIELDS = {"consumer_file", "consumer_test", "helper_sha256", "consumer_sha256"}
HASH_PATTERN = re.compile(r"[0-9a-f]{64}\Z")
TEST_DECLARATION = re.compile(r"^func (Test\w+)\(", flags=re.MULTILINE)
PACKAGE_DECLARATION = re.compile(r"^package ([A-Za-z_][A-Za-z_0-9]*)$", flags=re.MULTILINE)
BUILD_TAG = re.compile(r"^//(?:go:build| \+build) ", flags=re.MULTILINE)


def verify_helper(repo_root: pathlib.Path) -> None:
    """Bind the one helper-only file to its already enrolled consumer."""
    if not isinstance(HELPER_PROOFS, dict) or set(HELPER_PROOFS) != {HELPER_PATH}:
        raise ValueError("helper proof map must contain exactly the reviewed helper")
    if set(HELPER_PROOFS) & set(EXPECTED):
        raise ValueError("helper source overlaps executable readiness inventory")
    proof = HELPER_PROOFS[HELPER_PATH]
    if not isinstance(proof, dict) or set(proof) != HELPER_FIELDS:
        raise ValueError("helper proof fields differ from the closed contract")
    consumer = proof["consumer_file"]
    test = proof["consumer_test"]
    if consumer != CONSUMER_PATH or test != CONSUMER_TEST:
        raise ValueError("helper consumer differs from the reviewed owner")
    if consumer not in EXPECTED or EXPECTED[consumer].count(test) != 1:
        raise ValueError("helper consumer test is not enrolled exactly once")
    package = "./" + pathlib.PurePosixPath(consumer).parent.as_posix().removeprefix("go/")
    owners = [owner for owner, files in PACKAGES.items()
              if list(files.get(consumer, ())).count(test) == 1]
    if owners != [package]:
        raise ValueError("helper consumer package does not uniquely own the test")
    for file_path, pin_field in ((HELPER_PATH, "helper_sha256"),
                                 (consumer, "consumer_sha256")):
        pin = proof[pin_field]
        if not isinstance(pin, str) or not HASH_PATTERN.fullmatch(pin):
            raise ValueError(f"invalid helper proof pin: {pin_field}")
        path = repo_root / file_path
        try:
            source = path.read_bytes()
            text = source.decode("utf-8")
        except (OSError, UnicodeError) as error:
            raise ValueError(f"helper proof source unavailable: {file_path}: {error}") from error
        if BUILD_TAG.search(text):
            raise ValueError(f"helper proof source has a build tag: {file_path}")
        packages = PACKAGE_DECLARATION.findall(text)
        if len(packages) != 1:
            raise ValueError(f"helper proof source has no unique package: {file_path}")
        if file_path == HELPER_PATH:
            helper_package = packages[0]
            if TEST_DECLARATION.search(text):
                raise ValueError("helper proof source declares a Test root")
        elif packages[0] != helper_package:
            raise ValueError(f"helper and consumer Go packages differ: {file_path}")
        if hashlib.sha256(source).hexdigest() != pin:
            raise ValueError(f"helper proof source changed: {file_path}")


def verify_ledger(ledger_path: pathlib.Path, repo_root: pathlib.Path) -> int:
    """Require executable rows and their pinned helper source dependency."""
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
        if fields.get("runner") == "live-postgres-readiness" or path in EXPECTED:
            selected[path] = fields
    if set(selected) != set(EXPECTED) | {HELPER_PATH}:
        print(
            "postgres_ci ledger files differ: "
            f"expected={sorted(set(EXPECTED) | {HELPER_PATH})} actual={sorted(selected)}",
            file=sys.stderr,
        )
        return 1
    helper_row = selected[HELPER_PATH]
    for field, want in (("class", "postgres_ci"), ("tag", "~"),
                        ("runner", "live-postgres-readiness")):
        if helper_row.get(field) != want:
            print(f"postgres_ci helper row has unexpected {field}: {HELPER_PATH}",
                  file=sys.stderr)
            return 1
    try:
        verify_helper(repo_root)
    except ValueError as error:
        print(f"verify-ledger: {error}", file=sys.stderr)
        return 1
    for path, expected_tests in EXPECTED.items():
        if selected[path].get("class") != "postgres_ci":
            print(f"postgres_ci row has unexpected class: {path}", file=sys.stderr)
            return 1
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
        f"postgres_ci ledger selection: {len(EXPECTED)} executable files + "
        f"{len(HELPER_PROOFS)} helper file, {TOTAL_TESTS} tests selected"
    )
    return 0


def main() -> int:
    """Dispatch the ledger, per-package go-test event, and summary checks."""
    if len(sys.argv) == 4 and sys.argv[1] == "verify-ledger":
        return verify_ledger(pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]))
    if len(sys.argv) >= 4 and sys.argv[1] == "verify-results" and sys.argv[3] in PACKAGES:
        expected, rest, error = parse_expect(sys.argv[4:])
        if error:
            print(f"verify-results: {error}", file=sys.stderr)
            return 2
        skipped, error = parse_skips(rest)
        if error:
            print(f"verify-results: {error}", file=sys.stderr)
            return 2
        if expected is not None:
            package_tests = {test for tests in PACKAGES[sys.argv[3]].values()
                             for test in tests}
            outside = sorted(set(expected) - package_tests)
            if outside:
                print(f"verify-results: --expect names tests outside {sys.argv[3]}: "
                      f"{', '.join(outside)}", file=sys.stderr)
                return 2
        return verify_results(pathlib.Path(sys.argv[2]), sys.argv[3], PACKAGES,
                              skipped, expected)
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
        "verify-ledger <ledger> <repo-root> | verify-results <events> <package> [--expect CSV] [--skip NAME]... "
        "| list-packages | list-neo4j-tests | summary [--skip NAME]...",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    sys.exit(main())
