#!/usr/bin/env python3
"""Read complete hosted dispatch calibration runs without acceptance claims."""

from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import sys

import live_postgres_readiness_calibration as calibration
import live_postgres_readiness_refresh_v2 as refresh
import live_postgres_readiness_refresh_v2_provenance as source

JOB_RE = re.compile(r"readiness-calibration \(postgres18\) shard \(([1-6])/6\)")
START_RE = re.compile(r"^(\S+Z) calibration-start shard=([1-6])/6$")
SUMMARY_RE = re.compile(r"readiness calibration shard ([1-6])/6: ")


def _json(path: Path) -> object:
    """Load JSON while rejecting duplicate object keys and invalid constants."""
    import live_postgres_readiness_shards as shards
    return shards._parse_json(path.read_text(encoding="utf-8"))


def _normalized_log(log: str, number: int) -> str:
    """Adapt only the explicit calibration timing markers for the strict parser."""
    starts = [START_RE.fullmatch(line) for line in log.splitlines()]
    matched = [match for match in starts if match]
    if len(matched) != 1 or int(matched[0].group(2)) != number:
        raise ValueError("calibration log lacks unique matching start marker")
    result = []
    for line in log.splitlines():
        if match := START_RE.fullmatch(line):
            result.append(f'{match.group(1)} ##[group]Run bash scripts/run-live-postgres-readiness-tests.sh --shard "{number}/6" --report readiness-shard-report.json')
        else:
            result.append(SUMMARY_RE.sub(f"readiness shard {number}/6: ", line))
    return "\n".join(result) + "\n"


def _run_metadata(directory: Path, root: Path) -> tuple[int, str, int, list[dict[str, object]]]:
    """Bind a successful dispatch to its source tree and complete jobs list."""
    run = _json(directory / "run.json")
    jobs = source._jobs(_json(directory / "jobs.json"))
    if not isinstance(run, dict):
        raise ValueError("calibration run metadata is malformed")
    run_id, sha, attempt = run.get("id"), run.get("head_sha"), run.get("run_attempt")
    for field in ("repository", "head_repository"):
        repo = run.get(field)
        if not isinstance(repo, dict) or repo.get("full_name") != "eshu-hq/eshu":
            raise ValueError("calibration repository identity differs")
    if (type(run_id) is not int or run_id <= 0 or str(run_id) != directory.name
            or type(attempt) is not int or attempt <= 0
            or not isinstance(sha, str) or not calibration.SHA_RE.fullmatch(sha)
            or run.get("head_branch") != "main" or run.get("event") != "workflow_dispatch"
            or run.get("path") != calibration.WORKFLOW
            or run.get("status") != "completed" or run.get("conclusion") != "success"):
        raise ValueError("source must be a complete successful calibration dispatch")
    # The reader is pinned to the reviewed manifest and harness. A later source
    # edit needs a fresh reader/review; it cannot silently inherit this claim.
    for path in (calibration.MANIFEST, calibration.WORKFLOW, calibration.HARNESS):
        committed = source._source_text(root, sha, path).encode()
        if hashlib.sha256(committed).hexdigest() != calibration.file_digest(root, path):
            raise ValueError(f"calibration source changed: {path}")
    if source.source_inventory(root, sha) != [
            f"{package}|{test}" for package, tests in calibration.package_records()
            for test in tests]:
        raise ValueError("calibration source inventory differs")
    for job in jobs:
        source._successful(job, run_id, sha, attempt)
    return run_id, sha, attempt, jobs


def verify_log_rows(observed: list[str], memberships: list[frozenset[str]],
                    expected: list[list[str]]) -> None:
    """Check verifier-sorted log rows against exact scheduled row boundaries."""
    if (memberships != [frozenset(row) for row in expected]
            or observed != [run for row in expected for run in sorted(row)]):
        raise ValueError("calibration log ordered row identity differs")


def read_source(directory: Path, root: Path) -> dict[str, object]:
    """Validate one complete six-shard run and return measured row evidence."""
    run_id, sha, attempt, jobs = _run_metadata(directory, root)
    required = {"readiness-calibration coverage"}
    seen: dict[int, dict[str, object]] = {}
    for job in jobs:
        name = job.get("name")
        if name in required:
            if 0 in seen:
                raise ValueError("duplicate calibration coverage job")
            source._successful(job, run_id, sha, attempt)
            seen[0] = job
        elif isinstance(name, str) and (match := JOB_RE.fullmatch(name)):
            number = int(match.group(1))
            if number in seen:
                raise ValueError("duplicate calibration shard job")
            source._successful(job, run_id, sha, attempt)
            seen[number] = job
    if set(seen) != set(range(7)):
        raise ValueError("calibration jobs are partial")
    raw_reports = [_json(directory / f"shard{number}.json") for number in range(1, 7)]
    if any(not isinstance(report, dict) for report in raw_reports):
        raise ValueError("calibration report is malformed")
    reports: list[dict[str, object]] = raw_reports  # type: ignore[assignment]
    calibration.verify_reports(root, sha, None, reports)
    if any(report["run_id"] != run_id or report["run_attempt"] != attempt
           for report in reports):
        raise ValueError("calibration report run identity differs from run.json")
    rows = calibration.load(root)[1]
    profile: tuple[str, ...] | None = None
    measured: list[dict[str, object]] = []
    runner_versions: dict[str, str] = {}
    for number in range(1, 7):
        job = seen[number]
        steps = job.get("steps")
        proof = [step for step in steps if isinstance(step, dict)
                 and step.get("name") == "Run fixed calibration proofs"] if isinstance(steps, list) else []
        if len(proof) != 1 or proof[0].get("status") != "completed" or proof[0].get("conclusion") != "success":
            raise ValueError("calibration proof step is missing or failed")
        log = (directory / f"shard{number}.log").read_text(encoding="utf-8")
        if (f"Complete job name: {job['name']}" not in log
                or f"name: calibration-shard-{run_id}-{attempt}-{number}" not in log):
            raise ValueError("calibration job log identity differs")
        parsed, timings, (found_profile, version), memberships = refresh._parse_shard(
            _normalized_log(log, number), number, 6)
        expected = rows[number - 1]
        verify_log_rows(parsed, memberships, expected)
        if profile is not None and found_profile != profile:
            raise ValueError("calibration runner/backend profile differs between shards")
        profile = found_profile
        runner_versions[str(number)] = version
        for index, (package, category, seconds) in enumerate(timings):
            measured.append({"shard": number, "row": index + 1, "package": package,
                             "category": category, "residual_seconds": seconds,
                             "row_sha256": calibration.row_digest(expected[index])})
    if profile is None:
        raise ValueError("calibration has no runner profile")
    return {"run_id": run_id, "attempt": attempt, "head": sha,
            "job_ids": {str(number): seen[number]["id"] for number in range(7)},
            "profile": list(profile), "runner_versions": runner_versions, "rows": measured}


def build_evidence(directories: list[Path], root: Path) -> dict[str, object]:
    """Require two genuinely distinct compatible dispatch runs with all categories."""
    if len(directories) != 2:
        raise ValueError("calibration needs exactly two hosted dispatch runs")
    runs = [read_source(directory, root) for directory in directories]
    if len({run["run_id"] for run in runs}) != 2:
        raise ValueError("calibration sources duplicate a run ID")
    if len({run["head"] for run in runs}) != 1 or len({tuple(run["profile"]) for run in runs}) != 1:
        raise ValueError("calibration source SHA or runner/backend profile differs")
    if len({job_id for run in runs for job_id in run["job_ids"].values()}) != 14:
        raise ValueError("calibration job IDs duplicate across runs")
    categories = Counter(row["category"] for run in runs for row in run["rows"])
    if not all(categories[kind] for kind in ("first_row", "first_later", "repeat")):
        raise ValueError("calibration lacks first/repeat measurement categories")
    workflow = (root / calibration.WORKFLOW).read_text(encoding="utf-8")
    postgres = re.search(r"ghcr\.io/eshu-hq/ci-postgres-bookworm@sha256:[0-9a-f]{64}", workflow)
    neo4j = re.search(r"ghcr\.io/eshu-hq/ci-neo4j-community@sha256:[0-9a-f]{64}", workflow)
    if postgres is None or neo4j is None:
        raise ValueError("calibration backend profile is missing")
    return {"schema_version": 1, "status": "CALIBRATION_ONLY_NOT_ACCEPTANCE",
            "backend_profile": {"graph": "neo4j", "postgres_image": postgres.group(),
                                "neo4j_image": neo4j.group()},
            "workflow": calibration.WORKFLOW,
            "manifest_sha256": calibration.file_digest(root, calibration.MANIFEST),
            "workflow_sha256": calibration.file_digest(root, calibration.WORKFLOW),
            "harness_sha256": calibration.file_digest(root, calibration.HARNESS),
            "reviewed_source_sha256": calibration.REVIEWED_SOURCE_SHA256,
            "row_identity_sha256": calibration.ROW_SHA256,
            "coverage": {"shards": 6, "runs_per_dispatch": 702},
            "categories": dict(categories), "runs": runs}


def main() -> int:
    """Write calibration-only provenance from two captured hosted runs."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("sources", nargs=2, type=Path)
    args = parser.parse_args()
    try:
        evidence = build_evidence(args.sources, args.root)
        args.output.write_text(json.dumps(evidence, sort_keys=True, indent=2) + "\n")
        return 0
    except (ValueError, OSError, KeyError, TypeError, json.JSONDecodeError) as error:
        print(f"readiness calibration provenance: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
