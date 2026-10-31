#!/usr/bin/env python3
"""Check generated readiness fragment rows and seeded boundary failures."""

import argparse
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent / "lib"))

from live_postgres_readiness_gen_scan import collect  # noqa: E402
from live_postgres_readiness_gen_render import (  # noqa: E402
    FRAGMENT_BOUNDARIES,
    fragment_paths,
    render_fragments,
)
from live_tests_registry import load_ledger_text  # noqa: E402


def verify_fragment_rows(root: pathlib.Path) -> None:
    """Compare every emitted proof field with its enrolled source and ledger."""
    enrolled, bindings = collect(root)
    expected = {path: row["reason"] for path, row in
                {**enrolled, **bindings}.items()}
    actual = {}
    for rel in fragment_paths():
        content = (root / rel).read_text(encoding="utf-8")
        for path, reason in re.findall(
                r'  - file: (\S+)\n    tag: ~\n    class: postgres_ci\n'
                r'    reason: "([^"]+)"\n    runner: live-postgres-readiness\n',
                content):
            if path in actual:
                raise ValueError(f"duplicate generated proof: {path}")
            actual[path] = reason
    if actual != expected:
        raise ValueError(
            f"generated proof mismatch: actual={len(actual)} "
            f"expected={len(expected)}"
        )
    flattened = load_ledger_text(root / "specs/live-tests.v1.yaml")
    for path in actual:
        if flattened.count(f"  - file: {path}\n") != 1:
            raise ValueError(f"flattened ledger missing or duplicates {path}")
    print(f"generated proof rows: {len(actual)} exact paths and reasons")


def verify_fragment_failures(root: pathlib.Path) -> None:
    """Seed a deleted boundary and an over-budget range; both must fail."""
    enrolled, bindings = collect(root)
    missing = enrolled.copy()
    missing.pop(FRAGMENT_BOUNDARIES[0])
    try:
        render_fragments(missing, bindings)
    except ValueError as error:
        if "boundary missing" not in str(error):
            raise
    else:
        raise ValueError("deleted boundary passed")
    oversized = enrolled.copy()
    # 100 five-line seeds (502 lines) overflow any single fragment even empty,
    # so the cap seed stays RED as enrollment rebalances across fragments.
    for index in range(100):
        oversized[f"go/000-seeded-{index}_live_test.go"] = {
            "reason": "seeded cap proof"
        }
    try:
        render_fragments(oversized, bindings)
    except ValueError as error:
        if "add a lexical boundary" not in str(error):
            raise
    else:
        raise ValueError("oversized fragment passed")
    print("boundary and oversized fragment seeds rejected")


def main() -> None:
    """Run the requested fragment contract check on a repository root."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("rows", "failures"))
    parser.add_argument("root", type=pathlib.Path)
    args = parser.parse_args()
    root = args.root.resolve()
    if args.mode == "rows":
        verify_fragment_rows(root)
    else:
        verify_fragment_failures(root)


if __name__ == "__main__":
    main()
