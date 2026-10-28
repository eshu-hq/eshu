#!/usr/bin/env python3
"""Partition the live-postgres-readiness plan and account for every proof.

The full plan is the flat ``package|test`` list derived from the
list-packages TSV shared by runner and aggregate (the same inventory
``live_postgres_readiness_records`` owns). Shards are contiguous slices in
canonical inventory order, so newly enrolled tests stay covered without a
rebalance edit. The cut points come from predicted seconds
(``live_postgres_readiness_weights``), not test counts (#8930), and every
caller derives them from the same committed weights file. Each shard report is
checked against that same canonical input, and the aggregate requires the active
model's reports to cover the full plan in canonical order.
"""

from __future__ import annotations

import argparse
from collections import Counter
import json
import os
from pathlib import Path
import re
import sys
import tempfile
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from live_postgres_readiness_v2 import WeightsV2

sys.path.insert(0, str(Path(__file__).parent))
from live_postgres_readiness_records import package_records  # noqa: E402
import live_postgres_readiness_row_split as row_split  # noqa: E402
import live_postgres_readiness_weights as wt  # noqa: E402


SHARDS = ("1/4", "2/4", "3/4", "4/4")
TEST_RE = re.compile(r"^Test[A-Za-z0-9_]+$")
RUN_RE = re.compile(r"^\./[A-Za-z0-9_./-]+\|Test[A-Za-z0-9_]+$")
HEAD_RE = re.compile(r"^[0-9a-f]{40}$")
REPORT_KEYS = {"schema_version", "head", "shard", "ok", "expected_count",
               "observed_count", "expected_runs", "runs", "errors"}


def known_tests() -> dict[str, list[str]]:
    """Return the canonical package-to-tests table, failing closed on bad data."""
    return {package: list(tests) for package, tests in package_records()}


def _validate_run(line: str) -> str:
    """Validate one flat ``package|test`` plan line."""
    if not isinstance(line, str) or not line or "\n" in line or "\r" in line:
        raise ValueError(f"invalid run line: {line!r}")
    if not RUN_RE.fullmatch(line) or ".." in Path(line.split("|")[0]).parts:
        raise ValueError(f"invalid package or test in run: {line!r}")
    return line


def _validate_plan(runs: list[str]) -> None:
    """Reject empty, malformed, or duplicate canonical schedules."""
    if not isinstance(runs, list) or not runs:
        raise ValueError("full readiness plan is empty")
    for line in runs:
        _validate_run(line)
    duplicates = [line for line, count in Counter(runs).items() if count != 1]
    if duplicates:
        raise ValueError(f"duplicate planned run: {duplicates[0]}")


def expand_tsv(rows: list[str]) -> list[str]:
    """Expand list-packages TSV rows into flat ``package|test`` plan lines.

    Each row must name exactly the inventory's tests for its package in
    canonical order, so a stale or hand-edited TSV cannot silently drop or
    add proofs.
    """
    known = known_tests()
    runs: list[str] = []
    seen_packages: set[str] = set()
    for row in rows:
        fields = row.split("\t")
        if len(fields) != 3:
            raise ValueError(f"malformed packages row: {row!r}")
        package, pattern, tests_field = fields
        if package in seen_packages:
            raise ValueError(f"duplicate package row: {package!r}")
        seen_packages.add(package)
        expected = known.get(package)
        if expected is None:
            raise ValueError(f"unknown package row: {package!r}")
        tests = tests_field.split(" ")
        if tests != expected:
            raise ValueError(f"package tests differ from inventory: {package!r}")
        if pattern != f"^({'|'.join(expected)})$":
            raise ValueError(f"package pattern differs from inventory: {package!r}")
        runs.extend(f"{package}|{test}" for test in tests)
    _validate_plan(runs)
    if set(known) != seen_packages:
        missing = sorted(set(known) - seen_packages)
        raise ValueError(f"packages TSV misses inventory packages: {missing}")
    return runs


def partition_runs(runs: list[str], shard: str,
                   weights: wt.Weights | WeightsV2 | None = None) -> list[str]:
    """Select a time-weighted contiguous shard, preserving canonical order.

    Args:
        runs: Full flat ``package|test`` plan lines.
        shard: ``all`` or one identifier from the loaded model's count.
        weights: Cost model; the committed default model when omitted.

    Returns:
        The selected plan lines.

    Raises:
        ValueError: The plan, shard, or weights are invalid, or a shard is empty.
    """
    _validate_plan(runs)
    if shard == "all":
        return list(runs)
    resolved = weights or wt.load_weights()
    import live_postgres_readiness_v2 as v2
    count = resolved.shard_count if isinstance(resolved, v2.WeightsV2) else len(SHARDS)
    allowed = tuple(f"{number}/{count}" for number in range(1, count + 1))
    if shard not in allowed:
        raise ValueError(f"invalid shard: {shard!r}")
    bounds = wt.weighted_bounds(runs, resolved, count)
    index = allowed.index(shard)
    selected = runs[bounds[index]:bounds[index + 1]]
    if not selected:
        raise ValueError(f"shard {shard} has no planned runs")
    return selected


def subset_tsv(runs: list[str], shard: str,
               weights: wt.Weights | WeightsV2 | None = None) -> list[str]:
    """Render the shard's runner TSV rows grouped by package.

    Package rows predicted past the row budget split into sequential
    chunks (see :mod:`live_postgres_readiness_row_split`); every chunk is
    one ``go test`` invocation with its own clock.
    """
    resolved = weights or wt.load_weights()
    selected = partition_runs(runs, shard, resolved)
    by_package: dict[str, list[str]] = {}
    for line in selected:
        package, test = line.split("|")
        by_package.setdefault(package, []).append(line)
    rows = []
    import live_postgres_readiness_v2 as v2
    if isinstance(resolved, v2.WeightsV2):
        split = v2.split_rows(selected, resolved)
    else:
        split = row_split.split_rows(list(by_package.values()), resolved)
    for lines in split:
        package = lines[0].split("|")[0]
        tests = [line.split("|", 1)[1] for line in lines]
        rows.append(f"{package}\t^({'|'.join(tests)})$\t{' '.join(tests)}")
    return rows


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Reject duplicate JSON fields instead of silently taking the last value."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON field: {key}")
        result[key] = value
    return result


def _parse_json(source: str) -> object:
    """Reject ambiguous duplicate keys and nonstandard JSON constants."""
    return json.loads(source, object_pairs_hook=_unique_object,
                      parse_constant=_reject_constant)


def _reject_constant(value: str) -> object:
    """Reject NaN and infinity, which are outside JSON's number grammar."""
    raise ValueError(f"invalid JSON constant: {value}")


def _read_plan(path: Path) -> list[str]:
    """Expand a canonical full list-packages TSV without accepting drift."""
    try:
        rows = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise ValueError(f"plan unavailable: {error}")
    if any(not row.strip() for row in rows):
        raise ValueError("plan rows must not be blank")
    return expand_tsv(rows)


def _validate_head(head: str) -> None:
    if not HEAD_RE.fullmatch(head):
        raise ValueError(f"invalid head SHA: {head!r}")


def _read_manifest(path: Path) -> tuple[list[dict[str, object]], list[str]]:
    """Read runner manifest lines ``package|status|csv-tests`` verbatim."""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        return [], [f"manifest unavailable: {error}"]
    entries: list[dict[str, object]] = []
    errors: list[str] = []
    for number, line in enumerate(lines, start=1):
        fields = line.split("|")
        if len(fields) != 3 or not fields[0] or not fields[2]:
            errors.append(f"invalid manifest line {number}: {line!r}")
            continue
        package, status, tests_field = fields
        if status not in ("0", "1"):
            errors.append(f"invalid manifest status {number}: {line!r}")
            continue
        tests = tests_field.split(",")
        if any(not TEST_RE.fullmatch(test) for test in tests):
            errors.append(f"invalid manifest test {number}: {line!r}")
            continue
        entries.append({"package": package, "status": int(status), "tests": tests})
    return entries, errors


def _account(expected: list[str],
             entries: list[dict[str, object]]) -> tuple[list[dict[str, object]], list[str]]:
    """List every missing, duplicate, unexpected, and failed planned run."""
    seen: Counter[str] = Counter()
    failed: set[str] = set()
    for entry in entries:
        package = entry["package"]
        for test in entry["tests"]:  # type: ignore[union-attr]
            line = f"{package}|{test}"
            seen[line] += 1
            if entry["status"] != 0:
                failed.add(line)
    expected_set = set(expected)
    errors = [f"missing run: {line}" for line in expected if seen[line] == 0]
    errors.extend(f"duplicate run: {line}" for line, count in seen.items() if count > 1)
    errors.extend(f"unexpected run: {line}" for line in seen if line not in expected_set)
    errors.extend(f"failed run: {line}" for line in sorted(failed))
    runs = [{"run": line, "status": 1 if line in failed else 0}
            for line in sorted(seen)]
    return runs, errors


def _atomic_json(path: Path, value: dict[str, object]) -> None:
    """Replace a report only after its complete JSON has reached disk."""
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(value, handle, sort_keys=True, indent=2, allow_nan=False)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main() -> int:
    """Dispatch matrix, planner, shard reporter, selector, or aggregate gate."""
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    matrix = commands.add_parser("matrix")
    plan = commands.add_parser("plan")
    plan.add_argument("--shard", required=True)
    report = commands.add_parser("report")
    for option in ("head", "shard", "plan", "manifest", "output"):
        report.add_argument(f"--{option}", required=True)
    select = commands.add_parser("select")
    select.add_argument("--head", required=True)
    select.add_argument("--plan", required=True)
    select.add_argument("--output-dir", required=True)
    select.add_argument("reports", nargs="*")
    verify = commands.add_parser("verify")
    verify.add_argument("--head", required=True)
    verify.add_argument("--plan", required=True)
    verify.add_argument("reports", nargs="*")
    for command in (matrix, plan, report, select, verify):
        command.add_argument("--weights", help="weights file (default: committed)")
    args = parser.parse_args()
    import live_postgres_readiness_reports as reports
    try:
        weights_path = Path(args.weights) if args.weights else None
        if args.command == "report":
            return 0 if reports._report(args.head, args.shard, Path(args.plan),
                                Path(args.manifest), Path(args.output),
                                weights_path) else 1
        weights = wt.load_weights(weights_path)
        if args.command == "matrix":
            count = len(reports.shard_ids(weights))
            print(json.dumps({"include": [{"shard": index, "total": count}
                                          for index in range(1, count + 1)]},
                             separators=(",", ":")))
            return 0
        if args.command == "plan":
            rows = sys.stdin.read().splitlines()
            if any(not row.strip() for row in rows):
                raise ValueError("plan rows must not be blank")
            for line in subset_tsv(expand_tsv(rows), args.shard, weights):
                print(line)
            return 0
        if args.command == "select":
            return 0 if reports._select(args.head, Path(args.plan),
                                [Path(path) for path in args.reports],
                                Path(args.output_dir), weights) else 1
        return 0 if reports._verify(args.head, Path(args.plan),
                            [Path(path) for path in args.reports], weights) else 1
    except (ValueError, OSError, TypeError, json.JSONDecodeError) as error:
        print(f"readiness shards: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
