#!/usr/bin/env python3
"""Execute and validate the fixed, calibration-only readiness schedule."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

from live_postgres_readiness_records import package_records
from live_postgres_readiness_shards import _parse_json, expand_tsv

MANIFEST = "specs/live-postgres-readiness-calibration.v1.json"
WORKFLOW = ".github/workflows/live-postgres-readiness-calibration.yml"
HARNESS = "scripts/run-live-postgres-readiness-tests.sh"
ROW_SHA256 = "a203de14ef00ae93c0ccb274b65460a9b0e8680e0c0d25f47316ba1d49707a0c"
REVIEWED_SOURCE_SHA256 = "f00e9933ca1c2a913c42c667e080f8542118fa4acc67651bc56b8092ebec5ba9"
SOURCE_SHA = "2e3ba9fc40ec89a552e28ac25038646571df8b4e"
BOUNDS = [0, 75, 119, 349, 394, 491, 702]
RUN_RE = re.compile(r"^\./[A-Za-z0-9_./-]+\|Test[A-Za-z0-9_]+$")
SHA_RE = re.compile(r"^[0-9a-f]{40}$")


def digest(data: bytes) -> str:
    """Return a lowercase SHA-256 digest."""
    return hashlib.sha256(data).hexdigest()


def file_digest(root: Path, relative: str) -> str:
    """Hash one checked-out file used by calibration."""
    return digest((root / relative).read_bytes())


def row_digest(row: list[str]) -> str:
    """Hash an ordered runner row, including its package and test order."""
    return digest(json.dumps(row, separators=(",", ":"), ensure_ascii=False).encode())


def validate_manifest(document: object, plan: list[str]) -> list[list[list[str]]]:
    """Reject changed reviewed rows, source identity, or inventory drift."""
    if not isinstance(document, dict):
        raise ValueError("calibration manifest is malformed")
    expected = {"purpose": "offline_calibration_topology_proposal", "status": "NOT_RUN",
                "source_sha": SOURCE_SHA, "source_acceptance_run": "40784513987",
                "source_acceptance_shards": 5, "calibration_shards": 6,
                "source_inventory_sha256": "458d943f293a1e43cbcd202a7bb725979a2d1a1225031c415bc3a5b383b59629",
                "source_weights_sha256": "2b624a3b8e731317980a8248629f4b075a603f0c9d55f0625ec9aa3e02e9e885",
                "bounds": BOUNDS}
    if set(document) != set(expected) | {"rows"} or any(
            document.get(key) != value for key, value in expected.items()):
        raise ValueError("calibration manifest provenance or bounds changed")
    rows = document.get("rows")
    if not isinstance(rows, list) or len(rows) != 6:
        raise ValueError("calibration row identity is invalid")
    flattened: list[str] = []
    for shard in rows:
        if not isinstance(shard, list) or not shard:
            raise ValueError("calibration row identity is invalid")
        for row in shard:
            if not isinstance(row, list) or not row or any(
                    not isinstance(run, str) or not RUN_RE.fullmatch(run) for run in row):
                raise ValueError("calibration row identity is invalid")
            if len({run.split("|", 1)[0] for run in row}) != 1:
                raise ValueError("calibration row spans packages")
            flattened.extend(row)
    if flattened != plan or len(plan) != 702 or len(set(plan)) != 702:
        raise ValueError("calibration ordered coverage differs from inventory")
    lengths = [sum(map(len, shard)) for shard in rows]
    if lengths != [BOUNDS[i + 1] - BOUNDS[i] for i in range(6)]:
        raise ValueError("calibration shard bounds differ")
    if digest(json.dumps(rows, separators=(",", ":"), ensure_ascii=False).encode()) != ROW_SHA256:
        raise ValueError("calibration row identity differs from reviewed proposal")
    return rows


def load(root: Path, full_tsv: list[str] | None = None) -> tuple[dict[str, object], list[list[list[str]]]]:
    """Read the pinned manifest and validate it against the current inventory."""
    document = _parse_json((root / MANIFEST).read_text(encoding="utf-8"))
    plan = ([f"{package}|{test}" for package, tests in package_records() for test in tests]
            if full_tsv is None else expand_tsv(full_tsv))
    return document, validate_manifest(document, plan)


def tsv_rows(rows: list[list[str]]) -> list[str]:
    """Render exact fixed rows as go-test package, pattern, and test names."""
    result = []
    for row in rows:
        package, _ = row[0].split("|", 1)
        names = [run.split("|", 1)[1] for run in row]
        result.append(f"{package}\t^({'|'.join(names)})$\t{' '.join(names)}")
    return result


def _manifest_rows(path: Path) -> list[list[str]]:
    """Parse successful runner rows in invocation order."""
    result: list[list[str]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        fields = line.split("|")
        if len(fields) != 3 or fields[1] != "0" or not fields[2]:
            raise ValueError("calibration runner row failed or malformed")
        package, _, names = fields
        row = [f"{package}|{name}" for name in names.split(",")]
        if any(not RUN_RE.fullmatch(run) for run in row):
            raise ValueError("calibration runner row has invalid tests")
        result.append(row)
    return result


def make_report(root: Path, shard: int, head: str, full_tsv: list[str],
                runner_manifest: Path, run_id: int, attempt: int) -> dict[str, object]:
    """Bind one successful exact shard to its checkout and workflow identity."""
    if not SHA_RE.fullmatch(head) or run_id <= 0 or attempt <= 0:
        raise ValueError("invalid calibration run identity")
    _, rows = load(root, full_tsv)
    if not 1 <= shard <= 6:
        raise ValueError("invalid calibration shard")
    observed = _manifest_rows(runner_manifest)
    expected = rows[shard - 1]
    if observed != expected:
        raise ValueError("calibration runner row identity or order differs")
    return {"schema_version": 1, "workflow": WORKFLOW, "head": head,
            "run_id": run_id, "run_attempt": attempt, "shard": shard,
            "manifest_sha256": file_digest(root, MANIFEST),
            "workflow_sha256": file_digest(root, WORKFLOW),
            "harness_sha256": file_digest(root, HARNESS),
            "row_sha256": [row_digest(row) for row in expected],
            "runs": [run for row in observed for run in row], "ok": True}


def verify_reports(root: Path, head: str, full_tsv: list[str] | None,
                   reports: list[dict[str, object]]) -> None:
    """Require six exact, successful reports from one dispatch attempt."""
    _, rows = load(root, full_tsv)
    if not SHA_RE.fullmatch(head) or len(reports) != 6:
        raise ValueError("calibration reports are partial")
    if any(not isinstance(report, dict) for report in reports):
        raise ValueError("calibration report is malformed")
    run_ids = {report.get("run_id") for report in reports}
    attempts = {report.get("run_attempt") for report in reports}
    if len(run_ids) != 1 or len(attempts) != 1 or any(
            type(value) is not int or value <= 0 for value in run_ids | attempts):
        raise ValueError("calibration reports mix runs or attempts")
    numbers = [report.get("shard") for report in reports]
    if any(type(number) is not int for number in numbers) or sorted(numbers) != list(range(1, 7)):
        raise ValueError("calibration reports have duplicate or missing shard")
    for report in reports:
        shard = report["shard"]
        expected = rows[shard - 1]
        required = {"schema_version": 1, "workflow": WORKFLOW, "head": head,
                    "manifest_sha256": file_digest(root, MANIFEST),
                    "workflow_sha256": file_digest(root, WORKFLOW),
                    "harness_sha256": file_digest(root, HARNESS), "ok": True,
                    "row_sha256": [row_digest(row) for row in expected],
                    "runs": [run for row in expected for run in row]}
        if (type(report.get("ok")) is not bool or
                type(report.get("schema_version")) is not int or
                set(report) != set(required) | {"run_id", "run_attempt", "shard"} or any(
                report.get(key) != value for key, value in required.items())):
            raise ValueError(f"calibration shard {shard} report differs")


def main() -> int:
    """Render fixed rows or build and verify calibration coverage reports."""
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    plan = commands.add_parser("plan")
    plan.add_argument("--shard", type=int, required=True)
    report = commands.add_parser("report")
    report.add_argument("--shard", type=int, required=True)
    report.add_argument("--head", required=True)
    report.add_argument("--manifest", type=Path, required=True)
    report.add_argument("--output", type=Path, required=True)
    report.add_argument("--run-id", type=int, required=True)
    report.add_argument("--attempt", type=int, required=True)
    verify = commands.add_parser("verify")
    verify.add_argument("--head", required=True)
    verify.add_argument("reports", nargs="+", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    try:
        full_tsv = sys.stdin.read().splitlines()
        if args.command == "plan":
            _, rows = load(root, full_tsv)
            if not 1 <= args.shard <= 6:
                raise ValueError("invalid calibration shard")
            print("\n".join(tsv_rows(rows[args.shard - 1])))
        elif args.command == "report":
            result = make_report(root, args.shard, args.head, full_tsv,
                                 args.manifest, args.run_id, args.attempt)
            args.output.write_text(json.dumps(result, sort_keys=True, indent=2) + "\n")
            print(f"readiness calibration shard {args.shard}/6: "
                  f"{len(result['runs'])}/{len(result['runs'])} passing runs, ok=True")
        else:
            loaded = [_parse_json(path.read_text()) for path in args.reports]
            verify_reports(root, args.head, full_tsv, loaded)
            print("readiness calibration: 702/702 ordered runs in six fixed shards")
        return 0
    except (ValueError, OSError, TypeError, json.JSONDecodeError) as error:
        print(f"readiness calibration: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
