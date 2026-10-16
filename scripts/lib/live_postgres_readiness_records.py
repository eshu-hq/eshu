#!/usr/bin/env python3
"""Derived views and checks over the live PostgreSQL readiness inventory.

PACKAGES lives in live_postgres_readiness_inventory.py (data only), plus the
siblings live_postgres_readiness_inventory_own_gate.py and
live_postgres_readiness_inventory_config_fence.py merged below. This
module derives EXPECTED and TOTAL_TESTS from it and owns the fail-closed
checks the runner relies on; it is the one import site for callers.
"""

import re

from live_postgres_readiness_inventory import PACKAGES
from live_postgres_readiness_inventory_config_fence import CONFIG_FENCE_PACKAGES
from live_postgres_readiness_inventory_own_gate import OWN_GATE_PACKAGES

# Merge the sibling tables into their existing packages only, so an emptied or
# partial PACKAGES never gains a package from here.
for _sibling in (OWN_GATE_PACKAGES, CONFIG_FENCE_PACKAGES):
    for _package, _files in _sibling.items():
        if _package in PACKAGES:
            PACKAGES[_package] = {**PACKAGES[_package], **_files}

__all__ = ["EXPECTED", "PACKAGES", "TOTAL_TESTS", "neo4j_tests", "package_records"]

EXPECTED = {
    path: tests
    for package_expected in PACKAGES.values()
    for path, tests in package_expected.items()
}
TOTAL_TESTS = sum(len(tests) for tests in EXPECTED.values())


def package_records() -> list[tuple[str, list[str]]]:
    """Return (package, expected tests) per package, failing closed on bad data.

    PACKAGES is the single source of truth for the runner's package list and
    its -run patterns, so an empty or malformed table must stop the run rather
    than let it pass with fewer packages.
    """
    if not isinstance(PACKAGES, dict) or not PACKAGES:
        raise ValueError("PACKAGES is empty")
    records = []
    for package, files in PACKAGES.items():
        if not isinstance(package, str) or not package.startswith("./"):
            raise ValueError(f"malformed package path: {package!r}")
        if not isinstance(files, dict) or not files:
            raise ValueError(f"{package}: no expected files")
        tests: list[str] = []
        for path, names in files.items():
            if not isinstance(names, (tuple, list)) or not names:
                raise ValueError(f"{package}: {path}: no expected tests")
            for name in names:
                if not isinstance(name, str) or not re.fullmatch(r"Test\w+", name):
                    raise ValueError(f"{package}: {path}: malformed test {name!r}")
                if name in tests:
                    raise ValueError(f"{package}: duplicate test {name}")
                tests.append(name)
        records.append((package, tests))
    return records


def neo4j_tests() -> list[str]:
    """Return the enrolled tests that need a Neo4j proof backend.

    These are the tests carried by `*_graph_live_test.go` files: they open
    a Bolt driver and self-skip without one. The runner excuses exactly
    these (and only these) when no Neo4j env is configured.
    """
    tests: set[str] = set()
    for files in PACKAGES.values():
        for path, names in files.items():
            if path.endswith("_graph_live_test.go"):
                tests.update(names)
    return sorted(tests)
