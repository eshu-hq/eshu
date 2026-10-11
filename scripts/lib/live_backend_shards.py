#!/usr/bin/env python3
"""Partition the live backend plan and account for every scheduled run.

The full plan comes from the ledger extractor shared by runner and aggregate.
This module never owns a file allowlist: each report is checked against that
same canonical input, including backend pins and complete test expressions.
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


SHARDS = ("1/4", "2/4", "3/4", "4/4")
COMPOSE_FILES = {
    "nornicdb": "docker-compose.live-backend-nornicdb.yml",
    "neo4j": "docker-compose.live-backend-neo4j.yml",
}
RUN_RE = re.compile(r"^go/[A-Za-z0-9_./-]+_test\.go$")
PACKAGE_RE = re.compile(r"^\./[A-Za-z0-9_./-]+$")
TEST_RE = re.compile(r"^Test[A-Za-z0-9_]+$")
HEAD_RE = re.compile(r"^[0-9a-f]{40}$")
EVENT_KEYS = {"run", "status", "setup_seconds", "test_seconds", "cleanup_seconds"}
REPORT_KEYS = {"schema_version", "head", "shard", "ok", "expected_count",
               "observed_count", "expected_runs", "runs", "errors"}


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Reject duplicate JSON fields instead of silently taking the last value."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON field: {key}")
        result[key] = value
    return result


def _reject_constant(value: str) -> object:
    """Reject NaN and infinity, which are outside JSON's number grammar."""
    raise ValueError(f"invalid JSON constant: {value}")


def _parse_json(source: str) -> object:
    """Reject ambiguous duplicate keys and nonstandard JSON constants."""
    return json.loads(source, object_pairs_hook=_unique_object,
                      parse_constant=_reject_constant)


def _validate_run(line: str) -> str:
    """Validate one complete runner line without losing multi-test fields."""
    if not isinstance(line, str) or not line or "\n" in line or "\r" in line:
        raise ValueError(f"invalid run line: {line!r}")
    fields = line.split("|")
    if len(fields) < 5:
        raise ValueError(f"malformed run line: {line!r}")
    backend, compose, file_path, package, *tests = fields
    if (backend not in COMPOSE_FILES or compose != COMPOSE_FILES[backend]
            or not RUN_RE.fullmatch(file_path) or ".." in Path(file_path).parts
            or not PACKAGE_RE.fullmatch(package) or ".." in Path(package).parts
            or not tests or any(not TEST_RE.fullmatch(test) for test in tests)
            or len(set(tests)) != len(tests)):
        raise ValueError(f"invalid backend, target, or tests in run: {line!r}")
    return backend


def _validate_plan(runs: list[str]) -> None:
    """Reject empty, malformed, or duplicate canonical schedules."""
    if not isinstance(runs, list) or not runs:
        raise ValueError("full live backend plan is empty")
    for line in runs:
        _validate_run(line)
    duplicates = [line for line, count in Counter(runs).items() if count != 1]
    if duplicates:
        raise ValueError(f"duplicate planned run: {duplicates[0]}")


def expand_targets(targets: list[str], backend: str) -> list[str]:
    """Expand ledger targets into complete backend-pinned runner tuples."""
    if backend not in (*COMPOSE_FILES, "both"):
        raise ValueError(f"invalid backend: {backend}")
    runs: list[str] = []
    for target in targets:
        fields = target.split("|")
        if len(fields) < 4 or fields[-1] not in (*COMPOSE_FILES, "both"):
            raise ValueError(f"malformed ledger target: {target!r}")
        file_path, package, *tests = fields[:-1]
        pinned = fields[-1]
        for name, compose in COMPOSE_FILES.items():
            if backend in (name, "both") and pinned in (name, "both"):
                runs.append("|".join((name, compose, file_path, package, *tests)))
    _validate_plan(runs)
    return runs


def partition_runs(runs: list[str], shard: str) -> list[str]:
    """Select a pinned backend shard, preserving canonical relative order.

    Args:
        runs: Full ledger-derived runner lines.
        shard: ``all`` or one of four exact shard identifiers.

    Returns:
        The selected complete runner lines.

    Raises:
        ValueError: The plan or shard is invalid, or a selected shard is empty.
    """
    _validate_plan(runs)
    if shard == "all":
        return list(runs)
    if shard not in SHARDS:
        raise ValueError(f"invalid shard: {shard!r}")
    if shard == "1/4":
        selected = [line for line in runs if line.startswith("nornicdb|")]
    else:
        neo4j = [line for line in runs if line.startswith("neo4j|")]
        width = math.ceil(len(neo4j) / 3)
        index = int(shard[0]) - 2
        selected = neo4j[index * width:(index + 1) * width]
    if not selected:
        raise ValueError(f"shard {shard} has no planned runs")
    return selected


def _read_plan(path: Path) -> list[str]:
    """Read a newline-delimited full plan without accepting blank rows."""
    lines = path.read_text(encoding="utf-8").splitlines()
    _validate_plan(lines)
    return lines


def _validate_head(head: str) -> None:
    if not HEAD_RE.fullmatch(head):
        raise ValueError(f"invalid head SHA: {head!r}")


def _validate_event(value: object) -> dict[str, object]:
    """Accept only the five measured fields and finite nonnegative durations."""
    if not isinstance(value, dict) or set(value) != EVENT_KEYS:
        raise ValueError("event must contain exactly the required fields")
    event = dict(value)
    _validate_run(event["run"])
    status = event["status"]
    if type(status) is not int or status < 0 or status > 255:
        raise ValueError("event status must be an integer exit code from 0 to 255")
    for key in ("setup_seconds", "test_seconds", "cleanup_seconds"):
        number = event[key]
        if type(number) not in (int, float) or not math.isfinite(number) or number < 0:
            raise ValueError(f"event {key} must be a finite nonnegative number")
    return event


def _read_events(path: Path) -> tuple[list[dict[str, object]], list[str]]:
    """Read terminal JSONL events and retain diagnostics for invalid lines."""
    events: list[dict[str, object]] = []
    errors: list[str] = []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as error:
        return events, [f"events unavailable: {error}"]
    for number, line in enumerate(lines, start=1):
        try:
            events.append(_validate_event(_parse_json(line)))
        except (ValueError, TypeError) as error:
            errors.append(f"invalid event line {number}: {error}")
    return events, errors


def _account(expected: list[str], events: list[dict[str, object]]) -> list[str]:
    """List every missing, duplicate, unexpected, and failed terminal event."""
    counts = Counter(event["run"] for event in events)
    expected_set = set(expected)
    errors = [f"missing run: {line}" for line in expected if counts[line] == 0]
    errors.extend(f"duplicate run: {line}" for line, count in counts.items() if count > 1)
    errors.extend(f"unexpected run: {line}" for line in counts if line not in expected_set)
    errors.extend(f"failed run status {event['status']}: {event['run']}"
                  for event in events if event["status"] != 0)
    return errors


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


def _report(head: str, shard: str, plan: Path, events_path: Path,
            output: Path) -> bool:
    """Emit one parseable report even when actual events are incomplete."""
    expected: list[str] = []
    events: list[dict[str, object]] = []
    errors: list[str] = []
    try:
        _validate_head(head)
        expected = partition_runs(_read_plan(plan), shard)
    except (ValueError, OSError) as error:
        errors.append(f"invalid report input: {error}")
    events, event_errors = _read_events(events_path)
    errors.extend(event_errors)
    if expected:
        errors.extend(_account(expected, events))
    report: dict[str, object] = {
        "schema_version": 1, "head": head, "shard": shard,
        "ok": not errors, "expected_count": len(expected),
        "observed_count": len(events), "expected_runs": expected,
        "runs": events, "errors": errors,
    }
    _atomic_json(output, report)
    for error in errors:
        print(f"live backend shard {shard}: {error}", file=sys.stderr)
    print(f"live backend shard {shard}: {len(events)}/{len(expected)} terminal events, "
          f"ok={not errors}")
    return not errors


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
            report = _parse_json(path.read_text(encoding="utf-8"))
            if not isinstance(report, dict) or set(report) != REPORT_KEYS:
                raise ValueError("report schema mismatch")
            shard = report["shard"]
            if shard not in SHARDS or shard in seen_shards:
                raise ValueError(f"invalid or duplicate shard: {shard!r}")
            seen_shards.add(shard)
            expected = partition_runs(plan, shard)
            if (type(report["schema_version"]) is not int
                    or report["schema_version"] != 1
                    or report["head"] != head):
                raise ValueError(f"stale metadata for {shard}")
            if (report["expected_runs"] != expected
                    or type(report["expected_count"]) is not int
                    or report["expected_count"] != len(expected)):
                raise ValueError(f"plan mismatch for {shard}")
            raw_events = report["runs"]
            if not isinstance(raw_events, list):
                raise ValueError(f"invalid events for {shard}")
            events = [_validate_event(value) for value in raw_events]
            if (type(report["observed_count"]) is not int
                    or report["observed_count"] != len(events)
                    or type(report["ok"]) is not bool
                    or not isinstance(report["errors"], list)
                    or any(not isinstance(error, str) for error in report["errors"])):
                raise ValueError(f"invalid accounting for {shard}")
            problems = _account(expected, events)
            if not report["ok"] or report["errors"] or problems:
                raise ValueError(f"failed or incomplete shard {shard}: "
                                 f"{report['errors'] or problems}")
            assigned.extend(expected)
        except (OSError, ValueError, TypeError) as error:
            errors.append(f"{path}: {error}")
    if seen_shards != set(SHARDS):
        errors.append(f"missing shards: {sorted(set(SHARDS) - seen_shards)}")
    if Counter(assigned) != Counter(plan):
        errors.append("shard union differs from canonical full plan")
    for error in errors:
        print(f"live backend aggregate: {error}", file=sys.stderr)
    print(f"live backend aggregate: {len(assigned)}/{len(plan)} planned runs, "
          f"shards={len(seen_shards)}/4, ok={not errors}")
    return not errors


def _record(args: argparse.Namespace) -> None:
    """Append one validated terminal event in a single OS write."""
    event = _validate_event({
        "run": args.run, "status": int(args.status),
        "setup_seconds": float(args.setup_seconds),
        "test_seconds": float(args.test_seconds),
        "cleanup_seconds": float(args.cleanup_seconds),
    })
    path = Path(args.events)
    path.parent.mkdir(parents=True, exist_ok=True)
    data = (json.dumps(event, sort_keys=True, allow_nan=False) + "\n").encode("utf-8")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o644)
    try:
        if os.write(descriptor, data) != len(data):
            raise OSError("short terminal event write")
    finally:
        os.close(descriptor)


def main() -> int:
    """Dispatch planner, event recorder, report writer, or aggregate gate."""
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    expand = commands.add_parser("expand")
    expand.add_argument("--backend", default="both")
    plan = commands.add_parser("plan")
    plan.add_argument("--shard", required=True)
    record = commands.add_parser("record")
    for option in ("run", "status", "setup-seconds", "test-seconds",
                   "cleanup-seconds", "events"):
        record.add_argument(f"--{option}", required=True)
    report = commands.add_parser("report")
    for option in ("head", "shard", "plan", "events", "output"):
        report.add_argument(f"--{option}", required=True)
    verify = commands.add_parser("verify")
    verify.add_argument("--head", required=True)
    verify.add_argument("--plan", required=True)
    verify.add_argument("reports", nargs="*")
    args = parser.parse_args()
    try:
        if args.command == "expand":
            for line in expand_targets(sys.stdin.read().splitlines(), args.backend):
                print(line)
            return 0
        if args.command == "plan":
            for line in partition_runs(sys.stdin.read().splitlines(), args.shard):
                print(line)
            return 0
        if args.command == "record":
            _record(args)
            return 0
        if args.command == "report":
            return 0 if _report(args.head, args.shard, Path(args.plan),
                                Path(args.events), Path(args.output)) else 1
        return 0 if _verify(args.head, Path(args.plan),
                            [Path(path) for path in args.reports]) else 1
    except (ValueError, OSError, TypeError, json.JSONDecodeError) as error:
        print(f"live backend shards: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
