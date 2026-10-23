#!/usr/bin/env python3
"""Verify that every selected readiness proof ran and passed on PostgreSQL."""

import hashlib
import pathlib
import re
import sys
import unicodedata

from live_postgres_readiness_events import verify_results
from live_postgres_readiness_inventory_helpers import HELPER_PROOFS
from live_postgres_readiness_gen_scan import REQUIRED_HELPER_CONSUMERS
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


HELPER_FIELDS = {"consumer_file", "consumer_test", "helper_sha256", "consumer_sha256"}
SHARED_FIELDS = {"helper_sha256", "consumers"}
CONSUMER_FIELDS = {"consumer_file", "consumer_test", "consumer_sha256"}
HASH_PATTERN = re.compile(r"[0-9a-f]{64}\Z")
TEST_DECLARATION = re.compile(r"^func (Test\w*)\(", flags=re.MULTILINE)
PACKAGE_DECLARATION = re.compile(r"^package ([A-Za-z_][A-Za-z_0-9]*)$", flags=re.MULTILINE)
BUILD_TAG = re.compile(r"^//(?:go:build| \+build) ", flags=re.MULTILINE)


def verify_helper(repo_root: pathlib.Path) -> None:
    """Bind every reviewed helper edge to an enrolled, pinned consumer."""
    if not isinstance(HELPER_PROOFS, dict) or set(HELPER_PROOFS) != set(REQUIRED_HELPER_CONSUMERS):
        raise ValueError("helper proof map differs from the reviewed helpers")
    if set(HELPER_PROOFS) & set(EXPECTED):
        raise ValueError("helper source overlaps executable readiness inventory")
    for helper_path, proof in sorted(HELPER_PROOFS.items()):
        if not isinstance(proof, dict):
            raise ValueError("helper proof fields differ from the closed contract")
        if set(proof) == HELPER_FIELDS:
            consumers = [{field: proof[field] for field in CONSUMER_FIELDS}]
        elif set(proof) == SHARED_FIELDS and isinstance(proof["consumers"], list):
            consumers = proof["consumers"]
            if not consumers or any(not isinstance(item, dict) or set(item) != CONSUMER_FIELDS
                                    for item in consumers):
                raise ValueError("helper shared consumer fields differ from the closed contract")
        else:
            raise ValueError("helper proof fields differ from the closed contract")
        files = [item["consumer_file"] for item in consumers]
        if len(set(files)) != len(files) or set(files) != set(REQUIRED_HELPER_CONSUMERS[helper_path]):
            raise ValueError("helper consumer edges differ from the reviewed owner")
        helper_package = verify_source(repo_root, helper_path, proof["helper_sha256"], True)
        for item in consumers:
            consumer, test = item["consumer_file"], item["consumer_test"]
            if consumer not in EXPECTED or EXPECTED[consumer].count(test) != 1:
                raise ValueError("helper consumer test is not enrolled exactly once")
            package = "./" + pathlib.PurePosixPath(consumer).parent.as_posix().removeprefix("go/")
            owners = [owner for owner, files in PACKAGES.items()
                      if list(files.get(consumer, ())).count(test) == 1]
            if owners != [package]:
                raise ValueError("helper consumer package does not uniquely own the test")
            if pathlib.PurePosixPath(consumer).parent != pathlib.PurePosixPath(helper_path).parent:
                raise ValueError(f"helper and consumer packages differ: {consumer}")
            actual_package = verify_source(repo_root, consumer, item["consumer_sha256"], False)
            if actual_package != helper_package:
                raise ValueError(f"helper and consumer Go packages differ: {consumer}")


def verify_source(repo_root: pathlib.Path, file_path: str, pin: str,
                  helper: bool) -> str:
    """Verify one whole-file pin and return the unique Go package name."""
    pin_field = "helper_sha256" if helper else "consumer_sha256"
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
    if helper:
        names = TEST_DECLARATION.findall(text)
        if any(len(name) == 4 or unicodedata.category(name[4]) != "Ll"
               for name in names):
            raise ValueError("helper proof source declares a Test root")
    if hashlib.sha256(source).hexdigest() != pin:
        raise ValueError(f"helper proof source changed: {file_path}")
    return packages[0]


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
    if set(selected) != set(EXPECTED) | set(REQUIRED_HELPER_CONSUMERS):
        print(
            "postgres_ci ledger files differ: "
            f"expected={sorted(set(EXPECTED) | set(REQUIRED_HELPER_CONSUMERS))} actual={sorted(selected)}",
            file=sys.stderr,
        )
        return 1
    for helper_path in REQUIRED_HELPER_CONSUMERS:
        helper_row = selected[helper_path]
        if "backends" in helper_row:
            print(f"postgres_ci helper row has unexpected backends: {helper_path}",
                  file=sys.stderr)
            return 1
        for field, want in (("class", "postgres_ci"), ("tag", "~"),
                            ("runner", "live-postgres-readiness")):
            if helper_row.get(field) != want:
                print(f"postgres_ci helper row has unexpected {field}: {helper_path}",
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
