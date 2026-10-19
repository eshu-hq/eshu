#!/usr/bin/env python3
"""Daily GitHub Actions census for the #8617 CI revamp.

Fetches the last 24 hours of workflow runs in hourly slices (the run-list
endpoint caps one query at 1000 rows, roughly an hour of this repo's
traffic), dedupes by run id, and aggregates what the revamp is measured
against: total runs, cancelled share, runs over 60 minutes, and per-workflow
duration percentiles. Output is a Markdown report (job summary) plus the
same numbers as JSON (build artifact), so a census is comparable to the
baseline recorded on #8617 without re-deriving anything by hand.
"""

import argparse
from collections import Counter
import json
import statistics
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from typing import Any, Iterable, Sequence

Run = dict[str, Any]

_FIELDS = "databaseId,workflowName,status,conclusion,event,createdAt,updatedAt"
_SLICE_LIMIT = 1000


def dedupe(runs: Iterable[Run]) -> list[Run]:
    """Keep the first occurrence of each run id, preserving order."""
    seen: set[int] = set()
    kept: list[Run] = []
    for run in runs:
        run_id = run["databaseId"]
        if run_id in seen:
            continue
        seen.add(run_id)
        kept.append(run)
    return kept


def _minutes(run: Run) -> float | None:
    """Wall-clock minutes between createdAt and updatedAt, if parseable."""
    try:
        created = datetime.fromisoformat(run["createdAt"].replace("Z", "+00:00"))
        updated = datetime.fromisoformat(run["updatedAt"].replace("Z", "+00:00"))
    except (KeyError, ValueError):
        return None
    return (updated - created).total_seconds() / 60.0


def aggregate(runs: Sequence[Run]) -> dict[str, Any]:
    """Aggregate deduped runs into the census report shape.

    Args:
        runs: Run objects as returned by `gh run list --json`.

    Returns:
        A JSON-serializable dict with totals, conclusion counts, the
        cancelled share of completed runs, the count of completed runs
        over 60 minutes, and per-workflow stats.
    """
    completed = [r for r in runs if r.get("status") == "completed"]
    by_conclusion = Counter(r.get("conclusion") or "none" for r in completed)
    workflows: dict[str, dict[str, Any]] = {}
    durations: dict[str, list[float]] = {}
    over_60m = 0
    for run in completed:
        stats = workflows.setdefault(
            run["workflowName"], {"runs": 0, "failures": 0})
        stats["runs"] += 1
        if run.get("conclusion") == "failure":
            stats["failures"] += 1
        minutes = _minutes(run)
        if minutes is not None:
            durations.setdefault(run["workflowName"], []).append(minutes)
            if minutes > 60:
                over_60m += 1
    for name, values in durations.items():
        workflows[name]["median_minutes"] = round(statistics.median(values), 1)
        workflows[name]["max_minutes"] = round(max(values), 1)
    cancelled = by_conclusion.get("cancelled", 0)
    return {
        "total": len(runs),
        "completed": len(completed),
        "by_conclusion": dict(by_conclusion),
        "cancelled_share": (cancelled / len(completed)) if completed else 0.0,
        "over_60m": over_60m,
        "workflows": workflows,
    }


def render_markdown(report: dict[str, Any], start: str, end: str) -> str:
    """Render the report as the Markdown a job summary shows."""
    lines = [
        f"## CI census {start} .. {end}",
        "",
        f"{report['total']} runs, {report['completed']} completed, "
        f"{report['cancelled_share']:.1%} of completed runs cancelled, "
        f"{report['over_60m']} completed runs over 60 minutes.",
        "",
        "| Workflow | Runs | Failures | Median min | Max min |",
        "|---|---:|---:|---:|---:|",
    ]
    ranked = sorted(
        report["workflows"].items(),
        key=lambda item: item[1].get("median_minutes", 0.0), reverse=True)
    for name, stats in ranked[:15]:
        lines.append(
            f"| {name} | {stats['runs']} | {stats['failures']} | "
            f"{stats.get('median_minutes', 0)} | {stats.get('max_minutes', 0)} |")
    failures = sorted(
        ((s["failures"], n) for n, s in report["workflows"].items()
         if s["failures"]), reverse=True)
    if failures:
        lines += ["", "Failures by workflow: " + ", ".join(
            f"{name} {count}" for count, name in failures[:8]) + "."]
    return "\n".join(lines) + "\n"


def _gh_run_list(start: datetime, end: datetime) -> list[Run]:
    """One capped run-list query bounded to [start, end) server-side.

    The bound must be in the query: results are newest-first, so an
    unbounded ">= start" query against a day of traffic returns only the
    newest page and a client-side end filter then keeps almost nothing
    for older slices.
    """
    fmt = "%Y-%m-%dT%H:%M:%SZ"
    created = f"{start.strftime(fmt)}..{end.strftime(fmt)}"
    out = subprocess.run(
        ["gh", "run", "list", "--limit", str(_SLICE_LIMIT),
         "--created", created, "--json", _FIELDS],
        capture_output=True, text=True, check=True, timeout=300)
    return json.loads(out.stdout)


def _fetch_slice(start: datetime, end: datetime, depth: int = 0) -> list[Run]:
    """Fetch one bounded slice, subdividing while it comes back capped."""
    rows = _gh_run_list(start, end)
    if len(rows) < _SLICE_LIMIT or depth >= 3:
        return rows
    mid = start + (end - start) / 2
    return _fetch_slice(start, mid, depth + 1) + _fetch_slice(mid, end, depth + 1)


def fetch_window(start: datetime, end: datetime) -> list[Run]:
    """Fetch every run created in [start, end) in hourly bounded slices."""
    collected: list[Run] = []
    hours = int((end - start).total_seconds() // 3600)
    for i in range(hours):
        collected += _fetch_slice(
            start + timedelta(hours=i), start + timedelta(hours=i + 1))
    return dedupe(collected)


def main(argv: Sequence[str] | None = None) -> int:
    """Census the last 24 hours; write Markdown to stdout, JSON on request."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hours", type=int, default=24)
    parser.add_argument("--json-out", default="")
    args = parser.parse_args(argv)
    end = datetime.now(timezone.utc).replace(microsecond=0)
    start = end - timedelta(hours=args.hours)
    report = aggregate(fetch_window(start, end))
    report["window"] = {"start": start.isoformat(), "end": end.isoformat()}
    sys.stdout.write(render_markdown(
        report, report["window"]["start"], report["window"]["end"]))
    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as handle:
            json.dump(report, handle, indent=2, sort_keys=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
