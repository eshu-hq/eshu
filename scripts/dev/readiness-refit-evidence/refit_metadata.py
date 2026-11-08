"""Run and shard metadata plus measurement helpers for the refit evidence driver.

Pure data access over run directories (run.json, jobs.json, shard logs) with
no driver state, so the evidence subcommands share one fail-closed reader.
Every refusal raises ValueError naming the run; operators diagnose from the
message, not a traceback.
"""

from __future__ import annotations

import datetime as dt
import json
import re
import statistics
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

sys.path.insert(0, "scripts/lib")
# pylint: disable=wrong-import-position,import-error
import live_postgres_readiness_refresh_v2 as r2  # noqa: E402

# The readiness shard job name, as matched by fetch.sh. Key shard jobs by the
# shard number this parses, not by a character offset: two-digit shard counts
# such as "shard (10/12)" break fixed-offset keying.
SHARD_JOB_RE = re.compile(r"live-postgres-readiness \(postgres18\) shard \((\d+)/\d+\)")


def job_wall(job: dict[str, Any]) -> float:
    """Returns a job's wall time in seconds from its start and completion."""
    def parse(value: str) -> dt.datetime:
        """Returns the time of a GitHub API timestamp ending in Z."""
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    return (parse(job["completed_at"]) - parse(job["started_at"])).total_seconds()


def shard_jobs(run_dir: Path) -> dict[str, dict[str, Any]]:
    """Returns the run's readiness shard jobs keyed by shard number."""
    jobs = json.loads((run_dir / "jobs.json").read_text())["jobs"]
    keyed: dict[str, dict[str, Any]] = {}
    for job in jobs:
        if match := SHARD_JOB_RE.fullmatch(job["name"]):
            keyed[match.group(1)] = job
    return keyed


def measured_seconds(log: str) -> dict[str, float]:
    """Returns measured test seconds keyed by "package|test" for one shard log."""
    seen: dict[str, float] = {}
    pending: list[tuple[str, float]] = []
    for line in log.splitlines():
        if match := r2.TEST_RE.fullmatch(line):
            pending.append((match.group(2), float(match.group(3))))
        elif match := r2.PACKAGE_RE.fullmatch(line):
            for test, seconds in pending:
                seen[f"{match.group(2)}|{test}"] = seconds
            pending = []
    return seen


def run_head_sha(run_dir: Path) -> str:
    """Returns the run's head SHA, preferring run.json over jobs.json.

    The archived inputs predate run.json for some runs (#10388), so a run
    without one falls back to the jobs listing. Every job of a run shares
    its head SHA; anything else fails closed instead of guessing.
    """
    run_file = run_dir / "run.json"
    if run_file.exists():
        try:
            payload = json.loads(run_file.read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise ValueError(f"{run_dir.name}: run.json is unavailable or malformed") from error
        sha = payload.get("head_sha") if isinstance(payload, dict) else None
        if not isinstance(sha, str) or not sha:
            raise ValueError(f"{run_dir.name}: run.json lacks head_sha")
        return sha
    try:
        payload = json.loads((run_dir / "jobs.json").read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"{run_dir.name}: jobs.json is unavailable or malformed") from error
    jobs = payload.get("jobs") if isinstance(payload, dict) else None
    if not isinstance(jobs, list):
        raise ValueError(f"{run_dir.name}: jobs.json is malformed")
    shas = set()
    for job in jobs:
        sha = job.get("head_sha") if isinstance(job, dict) else None
        if not isinstance(sha, str) or not sha:
            raise ValueError(f"{run_dir.name}: a jobs.json job lacks head_sha")
        shas.add(sha)
    if len(shas) != 1:
        raise ValueError(f"{run_dir.name}: no run.json and jobs disagree on head_sha: {sorted(shas)}")
    return shas.pop()


def percentile(values: Sequence[float], quantile: float) -> float:
    """Returns a percentile by linear interpolation, inclusive method."""
    return statistics.quantiles(values, n=100, method="inclusive")[round(quantile * 100) - 1]
