"""Synthetic main-run fixtures for the v2 readiness weights refresher tests.

Each sample is a run directory with run.json, jobs.json and one timestamped
shard log, shaped like the GitHub Actions data the refresher reads. Row
residuals are exact: every test takes 1.0 s and each row closes
1 + residual seconds after the previous one.
"""

from __future__ import annotations

import json
from pathlib import Path


PLAN = [f"./{package}|Test{name}" for package, name in (
    ("a", "One"), ("a", "Two"), ("b", "One"), ("b", "Two"),
    ("b", "Three"), ("c", "One"), ("c", "Two"))]
SHA = "a" * 40
OTHER_SHA = "b" * 40
FOUR_SHARD_ROWS = [
    [("a", "TestOne", 8)],
    [("a", "TestTwo", 8), ("b", "TestOne", 5), ("b", "TestTwo", 2)],
    [("b", "TestThree", 8), ("c", "TestOne", 5)],
    [("c", "TestTwo", 8)],
]


def shard_log(shard: int, rows: list[tuple[str, str, int]], run_id: int,
              total: int = 4) -> str:
    """Build a small timestamped runner proof with explicit row residuals."""
    lines = [
        f"2026-10-26T00:00:00Z Complete job name: live-postgres-readiness (postgres18) shard ({shard}/{total})",
        "2026-10-26T00:00:00Z Image: ubuntu-24.04",
        "2026-10-26T00:00:00Z Version: 20261012.334.1",
        "2026-10-26T00:00:00Z Setup go version spec 1.26.10",
        "2026-10-26T00:00:00Z ESHU_LIVE_POSTGRES_IMAGE: ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:" + "c" * 64,
        f'2026-10-26T00:00:00Z ##[group]Run bash scripts/run-live-postgres-readiness-tests.sh --shard "{shard}/{total}" --report readiness-shard-report.json',
    ]
    second = 0
    for package, test, residual in rows:
        second += 1 + residual
        lines.append(f"2026-10-26T00:00:{second:02d}Z {test}: PASS elapsed=1.0s")
        lines.append(f"2026-10-26T00:00:{second:02d}Z live-postgres-readiness: ./{package} 1/1 PASS")
    lines.append(f"2026-10-26T00:00:{second:02d}Z readiness shard {shard}/{total}: "
                 f"{len(rows)}/{len(rows)} passing runs, ok=True")
    lines.append(f"2026-10-26T00:00:{second:02d}Z ##[endgroup]")
    lines.append(f"2026-10-26T00:00:{second:02d}Z   name: readiness-shard-{run_id}-1-{shard}")
    return "\n".join(lines) + "\n"


def write_sample(root: Path, run_id: int, sha: str,
                 rows_by_shard: list[list[tuple[str, str, int]]] | None = None) -> Path:
    """Write one complete synthetic main workflow sample.

    Args:
        root: Directory that receives the run directory named by run_id.
        run_id: Synthetic GitHub Actions run ID.
        sha: Source commit the run claims.
        rows_by_shard: (package, test, residual seconds) rows per shard; the
            shard count is its length. Defaults to the four-shard PLAN cut.

    Returns:
        The run directory with run.json, jobs.json and one log per shard.
    """
    directory = root / str(run_id)
    directory.mkdir()
    (directory / "run.json").write_text(json.dumps({
        "id": run_id, "head_sha": sha, "head_branch": "main", "event": "push",
        "status": "completed", "conclusion": "success",
        "path": ".github/workflows/live-postgres-readiness.yml", "run_attempt": 1,
        "repository": {"full_name": "eshu-hq/eshu"},
        "head_repository": {"full_name": "eshu-hq/eshu"},
    }))
    jobs = []
    rows_by_shard = FOUR_SHARD_ROWS if rows_by_shard is None else rows_by_shard
    total = len(rows_by_shard)
    for shard, rows in enumerate(rows_by_shard, 1):
        jobs.append({
            "id": 1000 * run_id + shard, "run_id": run_id, "head_sha": sha,
            "run_attempt": 1, "name": f"live-postgres-readiness (postgres18) shard ({shard}/{total})",
            "status": "completed", "conclusion": "success",
            "started_at": "2026-10-26T00:00:00Z", "completed_at": "2026-10-26T00:00:30Z",
            "steps": [{"name": "Run live readiness proofs", "status": "completed",
                       "conclusion": "success", "started_at": "2026-10-26T00:00:05Z",
                       "completed_at": "2026-10-26T00:00:25Z"}],
        })
        (directory / f"shard{shard}.log").write_text(shard_log(shard, rows, run_id, total))
    for name in ("live-postgres-readiness hermetic",
                 "live-postgres-readiness (postgres18)",):
        jobs.append({"id": 1000 * run_id + len(jobs) + 1, "run_id": run_id,
                     "head_sha": sha, "run_attempt": 1, "name": name,
                     "status": "completed", "conclusion": "success"})
    (directory / "jobs.json").write_text(json.dumps({"total_count": len(jobs), "jobs": jobs}))
    return directory
