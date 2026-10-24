#!/usr/bin/env python3
"""Partition the live-postgres-readiness plan and account for every proof.

The full plan is the flat ``package|test`` list derived from the
list-packages TSV shared by runner and aggregate (the same inventory
``live_postgres_readiness_records`` owns). Shards start as contiguous quarters
in canonical inventory order; the complete admin package runs on shard 4.
Each shard report is checked against that same canonical input, and the
aggregate requires the four reports' exact union to be the full plan.
"""

from __future__ import annotations

import argparse
from collections import Counter
import json
import math
import os
from pathlib import Path
import re
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).parent))
from live_postgres_readiness_records import package_records  # noqa: E402


SHARDS = ("1/4", "2/4", "3/4", "4/4")
ADMIN_PACKAGE = "./internal/query/admin"
ARTIFACT_RE = re.compile(r"^readiness-shard-\d+-(\d+)-[1-4]$")
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


def partition_runs(runs: list[str], shard: str) -> list[str]:
    """Select a pinned quarter with the complete admin package on shard 4.

    Args:
        runs: Full flat ``package|test`` plan lines.
        shard: ``all`` or one of four exact shard identifiers.

    Returns:
        The selected plan lines.

    Raises:
        ValueError: The plan or shard is invalid, or a selected shard is empty.
    """
    _validate_plan(runs)
    if shard == "all":
        return list(runs)
    if shard not in SHARDS:
        raise ValueError(f"invalid shard: {shard!r}")
    width = math.ceil(len(runs) / len(SHARDS))
    index = SHARDS.index(shard)
    selected = runs[index * width:(index + 1) * width]
    if shard == "4/4":
        assigned = set(selected)
        assigned.update(line for line in runs
                        if line.partition("|")[0] == ADMIN_PACKAGE)
        selected = [line for line in runs if line in assigned]
    else:
        selected = [line for line in selected
                    if line.partition("|")[0] != ADMIN_PACKAGE]
    if not selected:
        raise ValueError(f"shard {shard} has no planned runs")
    return selected


def subset_tsv(runs: list[str], shard: str) -> list[str]:
    """Render the shard's runner TSV rows grouped by package."""
    selected = partition_runs(runs, shard)
    by_package: dict[str, list[str]] = {}
    for line in selected:
        package, test = line.split("|")
        by_package.setdefault(package, []).append(test)
    return [f"{package}\t^({'|'.join(tests)})$\t{' '.join(tests)}"
            for package, tests in by_package.items()]


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


def _report(head: str, shard: str, plan: Path, manifest_path: Path,
            output: Path) -> bool:
    """Emit one parseable report even when the manifest is incomplete."""
    expected: list[str] = []
    entries: list[dict[str, object]] = []
    errors: list[str] = []
    try:
        _validate_head(head)
        expected = partition_runs(_read_plan(plan), shard)
    except ValueError as error:
        errors.append(f"invalid report input: {error}")
    manifest_entries, manifest_errors = _read_manifest(manifest_path)
    entries.extend(manifest_entries)
    errors.extend(manifest_errors)
    runs: list[dict[str, object]] = []
    if expected:
        runs, account_errors = _account(expected, entries)
        errors.extend(account_errors)
    report: dict[str, object] = {
        "schema_version": 1, "head": head, "shard": shard,
        "ok": not errors, "expected_count": len(expected),
        "observed_count": sum(1 for run in runs if run["status"] == 0),
        "expected_runs": expected, "runs": runs, "errors": errors,
    }
    _atomic_json(output, report)
    for error in errors:
        print(f"readiness shard {shard}: {error}", file=sys.stderr)
    print(f"readiness shard {shard}: {report['observed_count']}/{len(expected)} "
          f"passing runs, ok={not errors}")
    return not errors


def _validate_report(value: object, plan: list[str], head: str,
                     seen_shards: set[str]) -> tuple[list[str], str]:
    """Require one fresh matching report and return its assigned runs."""
    if not isinstance(value, dict) or set(value) != REPORT_KEYS:
        raise ValueError("report schema mismatch")
    shard = value["shard"]
    if shard not in SHARDS or shard in seen_shards:
        raise ValueError(f"invalid or duplicate shard: {shard!r}")
    seen_shards.add(shard)
    expected = partition_runs(plan, shard)  # type: ignore[arg-type]
    if (type(value["schema_version"]) is not int
            or value["schema_version"] != 1
            or value["head"] != head):
        raise ValueError(f"stale metadata for {shard}")
    if (value["expected_runs"] != expected
            or type(value["expected_count"]) is not int
            or value["expected_count"] != len(expected)):
        raise ValueError(f"plan mismatch for {shard}")
    raw_runs = value["runs"]
    if not isinstance(raw_runs, list):
        raise ValueError(f"invalid runs for {shard}")
    for entry in raw_runs:
        if (not isinstance(entry, dict) or set(entry) != {"run", "status"}
                or entry["run"] not in expected
                or entry["status"] not in (0, 1)):
            raise ValueError(f"invalid run entry for {shard}: {entry!r}")
    if (type(value["observed_count"]) is not int
            or type(value["ok"]) is not bool
            or not isinstance(value["errors"], list)
            or any(not isinstance(error, str) for error in value["errors"])):
        raise ValueError(f"invalid accounting for {shard}")
    ok_runs = [entry["run"] for entry in raw_runs if entry["status"] == 0]
    if sorted(ok_runs) != sorted(expected):
        raise ValueError(f"failed or incomplete shard {shard}: {value['errors']}")
    if value["observed_count"] != len(ok_runs):
        raise ValueError(f"miscounted shard {shard}: observed {value['observed_count']}")
    if not value["ok"] or value["errors"]:
        raise ValueError(f"failed or incomplete shard {shard}: {value['errors']}")
    return expected, shard  # type: ignore[return-value]


def _verify(head: str, plan_path: Path, report_paths: list[Path]) -> bool:
    """Require four valid reports whose exact union is the canonical plan."""
    _validate_head(head)
    plan = _read_plan(plan_path)
    if len(report_paths) != 4:
        paths = sorted(str(path) for path in report_paths)
        raise ValueError(f"expected exactly four shard reports, got {len(report_paths)}: {paths}")
    seen_shards: set[str] = set()
    assigned: list[str] = []
    errors: list[str] = []
    for path in report_paths:
        try:
            assigned.extend(_validate_report(
                _parse_json(path.read_text(encoding="utf-8")), plan, head,
                seen_shards)[0])
        except (OSError, ValueError, TypeError) as error:
            errors.append(f"{path}: {error}")
    if seen_shards != set(SHARDS):
        errors.append(f"missing shards: {sorted(set(SHARDS) - seen_shards)}")
    if Counter(assigned) != Counter(plan):
        errors.append("shard union differs from canonical full plan")
    for error in errors:
        print(f"readiness aggregate: {error}", file=sys.stderr)
    print(f"readiness aggregate: {len(assigned)}/{len(plan)} planned runs, "
          f"shards={len(seen_shards)}/4, ok={not errors}")
    return not errors


def _candidate_order(path: Path) -> tuple[int, int, str]:
    """Order rerun candidates newest first for one shard's report.

    A failed-job rerun uploads under a newer attempt while earlier
    attempts' reports stay downloadable, so recency comes from the
    artifact directory name before filesystem timestamps. Unparseable
    layouts sort oldest; the path breaks remaining ties deterministically.
    """
    attempt = 0
    match = ARTIFACT_RE.fullmatch(path.parent.name)
    if match:
        attempt = int(match.group(1))
    try:
        stamp = path.stat().st_mtime_ns
    except OSError:
        stamp = 0
    return (attempt, stamp, str(path))


def _select(head: str, plan_path: Path, report_paths: list[Path],
            output_dir: Path) -> bool:
    """Copy the newest valid report per shard for a rerun-proof aggregate.

    Every candidate still passes the same head and plan validation as
    ``verify``; a rejected older attempt is only a diagnostic. A shard
    whose candidates are all stale or failed keeps the aggregate red, and
    a genuinely missing shard is still an error. Only the four winners
    reach ``verify``, which keeps its exact-four rule.
    """
    _validate_head(head)
    plan = _read_plan(plan_path)
    contenders: dict[str, list[tuple[tuple[int, int, str], Path]]] = {}
    attempted: set[str] = set()
    for path in report_paths:
        claimed: object = None
        try:
            value = _parse_json(path.read_text(encoding="utf-8"))
            claimed = value.get("shard") if isinstance(value, dict) else None
            _, shard = _validate_report(value, plan, head, set())
        except (OSError, ValueError, TypeError, AttributeError) as error:
            print(f"readiness select: rejected {path}: {error}", file=sys.stderr)
            if isinstance(claimed, str) and claimed in SHARDS:
                attempted.add(claimed)
            continue
        attempted.add(shard)
        contenders.setdefault(shard, []).append((_candidate_order(path), path))
    errors = []
    if missing := sorted(set(SHARDS) - attempted):
        errors.append(f"missing shards: {missing}")
    errors.extend(f"no valid report for shard {shard}"
                  for shard in sorted(attempted - set(contenders)))
    for error in errors:
        print(f"readiness select: {error}", file=sys.stderr)
    if errors:
        return False
    output_dir.mkdir(parents=True, exist_ok=True)
    for shard in SHARDS:
        _, winner = max(contenders[shard])
        target = output_dir / f"readiness-shard-{shard[0]}-report.json"
        try:
            target.write_bytes(winner.read_bytes())
        except OSError as error:
            print(f"readiness select: {target}: {error}", file=sys.stderr)
            return False
        print(f"readiness select: {shard} <- {winner}")
    print(f"readiness select: {len(SHARDS)}/4 shards selected, ok=True")
    return True


def main() -> int:
    """Dispatch planner, shard reporter, selector, or aggregate gate."""
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
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
    args = parser.parse_args()
    try:
        if args.command == "plan":
            rows = sys.stdin.read().splitlines()
            if any(not row.strip() for row in rows):
                raise ValueError("plan rows must not be blank")
            for line in subset_tsv(expand_tsv(rows), args.shard):
                print(line)
            return 0
        if args.command == "report":
            return 0 if _report(args.head, args.shard, Path(args.plan),
                                Path(args.manifest), Path(args.output)) else 1
        if args.command == "select":
            return 0 if _select(args.head, Path(args.plan),
                                [Path(path) for path in args.reports],
                                Path(args.output_dir)) else 1
        return 0 if _verify(args.head, Path(args.plan),
                            [Path(path) for path in args.reports]) else 1
    except (ValueError, OSError, TypeError, json.JSONDecodeError) as error:
        print(f"readiness shards: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
