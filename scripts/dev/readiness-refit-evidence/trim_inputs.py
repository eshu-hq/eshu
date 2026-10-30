#!/usr/bin/env python3
"""Reduce fetched readiness run data to the parts the refit evidence reads.

A fetched run directory (see fetch.sh) holds run.json, jobs.json and one raw
log per shard job. The raw logs run to tens of megabytes. The refresher
parser, its provenance check and the evidence scripts read only a few lines
of each log and a few fields of each JSON file. This script keeps exactly
those, so the refresher and the evidence scripts give the same results on the
trimmed copy as on the raw data. reproduce.sh proves that equality.

Kept log lines, in their original order:

* lines the refresher parser matches (proof step start, test PASS rows,
  package PASS rows, the shard summary);
* every line containing ``live-postgres-readiness:`` and every FAIL or SKIP
  test row, so the parser's refusal checks see the same input;
* the runner profile lines (image and version pair, Go version, Postgres
  image digest);
* the provenance lines (``Complete job name:`` and artifact
  ``name: readiness-shard-`` lines).

Usage, from the repository root:
    python3 -I scripts/dev/readiness-refit-evidence/trim_inputs.py \
        --lib scripts/lib --out OUT_DIR RAW_RUN_DIR...
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

RUN_FIELDS = ("id", "name", "head_branch", "head_sha", "path", "run_attempt", "event",
              "status", "conclusion", "created_at", "run_started_at", "updated_at")
REPO_FIELDS = ("full_name",)
JOB_FIELDS = ("id", "run_id", "head_sha", "run_attempt", "name", "status", "conclusion",
              "started_at", "completed_at")
STEP_FIELDS = ("name", "status", "conclusion", "number", "started_at", "completed_at")
KEEP_SUBSTRINGS = ("live-postgres-readiness:", "Complete job name: ", "name: readiness-shard-")
FAIL_OR_SKIP_RE = re.compile(r"\bTest[A-Za-z0-9_]+: (?:FAIL|SKIP)\b")


def _pick(record: dict[str, Any], fields: tuple[str, ...]) -> dict[str, Any]:
    """Returns the named fields of record that are present."""
    return {field: record[field] for field in fields if field in record}


def trim_run(run: dict[str, Any]) -> dict[str, Any]:
    """Returns run.json reduced to the fields the provenance check reads."""
    trimmed = _pick(run, RUN_FIELDS)
    for field in ("repository", "head_repository"):
        if isinstance(run.get(field), dict):
            trimmed[field] = _pick(run[field], REPO_FIELDS)
    return trimmed


def trim_jobs(jobs: dict[str, Any]) -> dict[str, Any]:
    """Returns jobs.json reduced to job timing, identity and step outcomes."""
    trimmed = []
    for job in jobs.get("jobs", []):
        kept = _pick(job, JOB_FIELDS)
        if isinstance(job.get("steps"), list):
            kept["steps"] = [_pick(step, STEP_FIELDS) for step in job["steps"]]
        trimmed.append(kept)
    return {"total_count": len(trimmed), "jobs": trimmed}


def _span_lines(text: str, starts: list[int], pattern: re.Pattern[str]) -> set[int]:
    """Returns the indexes of lines covered by any match of pattern."""
    import bisect
    covered: set[int] = set()
    for match in pattern.finditer(text):
        first = bisect.bisect_right(starts, match.start()) - 1
        last = bisect.bisect_right(starts, max(match.end() - 1, match.start())) - 1
        covered.update(range(first, last + 1))
    return covered


def trim_log(log: str, parser: Any) -> str:
    """Returns the lines of one shard log that the parsers and checks read."""
    lines = log.splitlines()
    starts, offset = [], 0
    for line in log.splitlines(keepends=True):
        starts.append(offset)
        offset += len(line)
    keep: set[int] = set()
    for pattern in (parser.IMAGE_VERSION_RE, *parser.PROFILE_RE):
        keep |= _span_lines(log, starts, pattern)
    line_patterns = (parser.START_RE, parser.TEST_RE, parser.PACKAGE_RE, parser.SUMMARY_RE)
    for index, line in enumerate(lines):
        if (any(pattern.fullmatch(line) for pattern in line_patterns)
                or any(token in line for token in KEEP_SUBSTRINGS)
                or FAIL_OR_SKIP_RE.search(line)):
            keep.add(index)
    return "".join(lines[index] + "\n" for index in sorted(keep))


def trim_directory(source: Path, target: Path, parser: Any) -> int:
    """Writes the trimmed copy of one run directory and returns its byte size."""
    target.mkdir(parents=True, exist_ok=True)
    size = 0
    # Some older runs were fetched without run.json; only the refresher reads
    # it, and the evidence scripts score those runs from jobs.json alone.
    if (source / "run.json").exists():
        run = json.loads((source / "run.json").read_text(encoding="utf-8"))
        (target / "run.json").write_text(json.dumps(trim_run(run), indent=1, sort_keys=True) + "\n",
                                          encoding="utf-8")
        size += (target / "run.json").stat().st_size
    jobs = json.loads((source / "jobs.json").read_text(encoding="utf-8"))
    (target / "jobs.json").write_text(json.dumps(trim_jobs(jobs), indent=1, sort_keys=True) + "\n",
                                       encoding="utf-8")
    size += (target / "jobs.json").stat().st_size
    for log_path in sorted(source.glob("shard[0-9].log")):
        trimmed = trim_log(log_path.read_text(encoding="utf-8"), parser)
        (target / log_path.name).write_text(trimmed, encoding="utf-8")
        size += len(trimmed.encode("utf-8"))
    return size


def main(argv: list[str] | None = None) -> int:
    """Trims each raw run directory into OUT_DIR/<run id>."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--lib", type=Path, required=True,
                        help="scripts/lib of the tree whose parser defines the kept lines")
    parser.add_argument("--out", type=Path, required=True, help="directory for trimmed runs")
    parser.add_argument("runs", type=Path, nargs="+", help="raw run directories")
    args = parser.parse_args(argv)
    sys.path.insert(0, str(args.lib.resolve()))
    import live_postgres_readiness_refresh_v2 as refresh_v2  # pylint: disable=import-outside-toplevel
    total = 0
    for run_dir in args.runs:
        size = trim_directory(run_dir, args.out / run_dir.name, refresh_v2)
        total += size
        print(f"{run_dir.name}: {size} bytes")
    print(f"total: {total} bytes")
    return 0


if __name__ == "__main__":
    sys.exit(main())
