#!/usr/bin/env python3
"""Exact inventory and event checks for the isolated PostgreSQL migration lane."""

import json
import pathlib
import re
import sys

from live_tests_registry import load_ledger_text

PACKAGE = "./internal/storage/postgres"
MODULE_PACKAGE = "github.com/eshu-hq/eshu/go/internal/storage/postgres"
EXPECTED = {
    "go/internal/storage/postgres/migration_169_interruption_live_test.go": (
        "TestMigration169InterruptedBackfillLive",
    ),
    "go/internal/storage/postgres/migration_169_phased_live_test.go": (
        "TestMigration169PhasedTrackedLive",
    ),
    "go/internal/storage/postgres/schema_7033_interrupt_rollback_live_test.go": (
        "TestBootstrapInterruptedDefinitionRollsBackLive",
        "TestRollbackFixtureDSNGuard",
    ),
    "go/internal/storage/postgres/schema_fixture_connection_guard_security_live_test.go": (
        "TestOwnedFixtureConnectorRejectsUnsafeInputBeforeDial",
        "TestOwnedFixtureConnectorRejectsInheritedPGEnvironment",
        "TestOwnedFixtureConnectorRejectsWrongServerAndReconnect",
    ),
}
MIGRATION_PATHS = frozenset(EXPECTED)


def verify_ledger(manifest: pathlib.Path, repo_root: pathlib.Path) -> int:
    """Require the four exact integration-tagged, dedicated-runner rows."""
    try:
        ledger = load_ledger_text(manifest)
    except ValueError as error:
        print(f"migration ledger: {error}", file=sys.stderr)
        return 1
    rows = re.findall(r"^  - file: (\S+)\n((?:    [^\n]*\n)*)", ledger, re.M)
    selected = {}
    for path, body in rows:
        fields = dict(re.findall(r"^    (\w+): (.*)$", body, re.M))
        if fields.get("runner") == "live-postgres-migrations" or path in MIGRATION_PATHS:
            selected[path] = fields
    if set(selected) != MIGRATION_PATHS:
        print("migration ledger paths differ from the exact inventory", file=sys.stderr)
        return 1
    for path, names in EXPECTED.items():
        fields = selected[path]
        if (fields.get("class"), fields.get("tag"), fields.get("runner")) != (
            "postgres_ci", "integration", "live-postgres-migrations"
        ):
            print(f"migration ledger class/tag/runner mismatch: {path}", file=sys.stderr)
            return 1
        try:
            source = (repo_root / path).read_text(encoding="utf-8")
        except OSError as error:
            print(f"migration inventory source missing: {path}: {error}", file=sys.stderr)
            return 1
        actual = re.findall(r"^func (Test\w+)\(", source, re.M)
        if set(actual) != set(names) or len(actual) != len(names):
            print(f"migration test inventory mismatch: {path}", file=sys.stderr)
            return 1
    print("migration ledger: 4 files, 7 tests selected")
    return 0


def verify_events(events_path: pathlib.Path) -> int:
    """Reject missing, failed, skipped, or duplicate top-level and child tests."""
    expected = {name for names in EXPECTED.values() for name in names}
    runs = {name: 0 for name in expected}
    terminals: dict[str, list[str]] = {name: [] for name in expected}
    packages: list[str] = []
    invalid_children: list[str] = []
    number = 0
    try:
        with events_path.open(encoding="utf-8") as stream:
            for number, line in enumerate(stream, start=1):
                event = json.loads(line)
                action = event.get("Action")
                test = event.get("Test")
                if not test and action in {"pass", "fail", "skip"}:
                    packages.append(f"{event.get('Package')}:{action}")
                elif test in expected:
                    if action == "run":
                        runs[test] += 1
                    elif action in {"pass", "fail", "skip"}:
                        terminals[test].append(action)
                elif test and action in {"fail", "skip"} and test.split("/", 1)[0] in expected:
                    invalid_children.append(f"{test}: {action}")
    except (OSError, json.JSONDecodeError) as error:
        print(f"migration events invalid at line {number}: {error}", file=sys.stderr)
        return 1
    bad = [
        f"{name}: run={runs[name]} terminals={terminals[name]}"
        for name in sorted(expected)
        if runs[name] != 1 or terminals[name] != ["pass"]
    ]
    if packages != [f"{MODULE_PACKAGE}:pass"]:
        bad.append(f"package terminals={packages}")
    bad.extend(invalid_children)
    if bad:
        print("migration proofs failed: " + "; ".join(bad), file=sys.stderr)
        return 1
    print("migration proofs: 7/7 PASS; no skip/fail events")
    return 0


def main() -> int:
    """Expose the inventory, ledger, and event checks to the shell runner."""
    if len(sys.argv) == 4 and sys.argv[1] == "verify-ledger":
        return verify_ledger(pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]))
    if len(sys.argv) == 3 and sys.argv[1] == "verify-events":
        return verify_events(pathlib.Path(sys.argv[2]))
    if len(sys.argv) == 2 and sys.argv[1] == "pattern":
        names = [name for tests in EXPECTED.values() for name in tests]
        print("^(" + "|".join(names) + ")$")
        return 0
    print(
        "usage: live_postgres_migrations.py verify-ledger MANIFEST ROOT "
        "| verify-events JSONL | pattern",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    sys.exit(main())
