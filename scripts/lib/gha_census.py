#!/usr/bin/env python3
"""Daily GitHub Actions census for the #8617 CI revamp.

Fetches the last 24 hours of workflow runs in hourly slices (the run-list
endpoint caps one query at 1000 rows, roughly an hour of this repo's
traffic), dedupes by run id, and aggregates what the revamp is measured
against: total runs, cancelled share, runs over 60 minutes, and per-workflow
duration percentiles, merged-head required-green latency, and evidenced
cancelled-run runner occupancy. Output is a Markdown report (job summary) plus the
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

from gha_census_enrichment import (UNKNOWN_WORKFLOW, cancellation_cost,
                                   percentile, required_green)

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
    except (KeyError, ValueError, AttributeError, TypeError):
        return None
    return (updated - created).total_seconds() / 60.0


def aggregate(runs: Sequence[Run]) -> dict[str, Any]:
    """Aggregate deduped runs into the census report shape.

    Args:
        runs: Run objects as returned by `gh run list --json`.

    Returns:
        A JSON-serializable dict with totals, conclusion counts, the
        cancelled share of completed runs, the count of completed runs
        over 60 minutes, and per-workflow created and completed counts.
    """
    # Keep malformed rows in the raw and created counts. Completed-run
    # durations still require a workflow name.
    malformed = sum(1 for r in runs if not r.get("workflowName"))
    named = [r for r in runs if r.get("workflowName")]
    completed = [r for r in named if r.get("status") == "completed"]
    by_conclusion = Counter(r.get("conclusion") or "none" for r in completed)
    workflows: dict[str, dict[str, Any]] = {}
    for run in runs:
        name = run.get("workflowName") or UNKNOWN_WORKFLOW
        stats = workflows.setdefault(name, {"created_runs": 0, "runs": 0,
                                            "cancelled_runs": 0, "failures": 0,
                                            "by_conclusion": {}})
        stats["created_runs"] += 1
        if run.get("conclusion") == "cancelled":
            stats["cancelled_runs"] += 1
    durations: dict[str, list[float]] = {}
    over_60m = 0
    for run in completed:
        stats = workflows[run["workflowName"]]
        stats["runs"] += 1
        conclusion = run.get("conclusion") or "none"
        stats["by_conclusion"][conclusion] = (
            stats["by_conclusion"].get(conclusion, 0) + 1)
        if run.get("conclusion") == "failure":
            stats["failures"] += 1
        minutes = _minutes(run)
        if minutes is not None:
            durations.setdefault(run["workflowName"], []).append(minutes)
            if minutes > 60:
                over_60m += 1
    for name, values in durations.items():
        workflows[name]["median_minutes"] = round(statistics.median(values), 1)
        workflows[name]["p50_minutes"] = percentile(values, 0.5)
        workflows[name]["p95_minutes"] = percentile(values, 0.95)
        workflows[name]["max_minutes"] = round(max(values), 1)
    cancelled = by_conclusion.get("cancelled", 0)
    return {
        "total": len(runs),
        "malformed_rows": malformed,
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
    ]
    if report.get("truncated_slices"):
        lines.append(
            f"WARNING: {report['truncated_slices']} slice(s) hit the query "
            "cap at maximum subdivision; those windows are undercounted.")
    if report.get("malformed_rows"):
        lines.append(
            f"WARNING: {report['malformed_rows']} row(s) had no workflow "
            "name; created/cancelled counts use Unknown workflow, while "
            "completed-run stats exclude them.")
    lines += [
        "",
        "| Workflow | Created | Completed | Cancelled | Failures | P50 min | P95 min | Max min |",
        "|---|---:|---:|---:|---:|---:|---:|---:|",
    ]
    ranked = sorted(
        report["workflows"].items(),
        key=lambda item: item[1].get("median_minutes", 0.0), reverse=True)
    for name, stats in ranked:
        lines.append(
            f"| {name} | {stats['created_runs']} | {stats['runs']} | "
            f"{stats['cancelled_runs']} | {stats['failures']} | "
            f"{stats.get('p50_minutes', '—')} | "
            f"{stats.get('p95_minutes', '—')} | "
            f"{stats.get('max_minutes', '—')} |")
    failures = sorted(
        ((s["failures"], n) for n, s in report["workflows"].items()
         if s["failures"]), reverse=True)
    if failures:
        lines += ["", "Failures by workflow: " + ", ".join(
            f"{name} {count}" for count, name in failures[:8]) + "."]
    required = report["workflows"].get("Required Gates", {})
    lines += ["", "### Required Gates window counts",
              f"Required Gates: {required.get('created_runs', 0)} created, "
              f"{required.get('cancelled_runs', 0)} cancelled, "
              f"{required.get('runs', 0)} completed."]
    cost = report.get("cancellation_cost", {})
    if cost.get("completeness") == "failed":
        lines.append("Cancellation classification unavailable: enrichment failed.")
    elif cost.get("by_workflow") is not None:
        counts = cost["by_workflow"].get("Required Gates", {}).get("categories", {})
        lines.append(f"{counts.get('zero_job', 0)} zero-job, "
                     f"{counts.get('executed', 0)} executed, "
                     f"{counts.get('unknown', 0)} unknown cancellations.")
    green = report.get("required_green")
    if green:
        if green.get("completeness") == "failed":
            lines += ["", "### Required-green latency (merged PR final heads)",
                      "WARNING: enrichment failed; required-green metrics are "
                      f"unavailable: {green['error']}."]
        else:
            lines += _render_required_green(green)
    cancellation = report.get("cancellation_cost")
    if cancellation:
        if cancellation.get("completeness") == "failed":
            lines += ["", "### Cancelled-run runner cost",
                      "WARNING: enrichment failed; runner-cost metrics are "
                      f"unavailable: {cancellation['error']}."]
        else:
            lines += _render_cancellation_cost(cancellation)
    return "\n".join(lines) + "\n"


def _render_required_green(green: dict[str, Any]) -> list[str]:
    """Render successful required-green enrichment."""
    lines = ["", "### Required-green latency (merged PR final heads)",
             "First observed PR workflow enqueue → last required context green by merge; "
             "includes queueing and retries. Push-to-dispatch delay: NOT_CHECKED.",
             f"Outcomes: {green['outcomes']}.",
             "| Publisher kind | Heads | Green | P50 min | P95 min |",
             "|---|---:|---:|---:|---:|"]
    overall = green["overall"]
    overall_p50 = overall["p50_minutes"] if overall["p50_minutes"] is not None else "—"
    overall_p95 = overall["p95_minutes"] if overall["p95_minutes"] is not None else "—"
    lines.append(f"| All | {overall['heads']} | {overall['success']} | "
                 f"{overall_p50} | {overall_p95} |")
    for kind, stats in green["groups"].items():
        p50 = stats["p50_minutes"] if stats["p50_minutes"] is not None else "—"
        p95 = stats["p95_minutes"] if stats["p95_minutes"] is not None else "—"
        lines.append(f"| {kind} | {stats['heads']} | {stats['success']} | "
                     f"{p50} | {p95} |")
    non_green = [f"#{head['number']} {head['outcome']}"
                 for head in green["heads"] if head["outcome"] != "success"]
    if non_green:
        lines.append("Non-green final heads at merge: " + ", ".join(non_green) + ".")
    return lines


def _render_cancellation_cost(cancellation: dict[str, Any]) -> list[str]:
    """Render successful cancellation-cost enrichment."""
    lines = ["", "### Cancelled-run runner cost",
             "Raw cancelled share above is unchanged. Job evidence covers "
             f"{cancellation['runs']} cancelled runs; "
             f"categories: {cancellation['categories']}.",
             "Evidenced runner occupancy: "
             f"{cancellation['evidenced_runner_minutes']} runner-minutes. "
             "Unknown runs are excluded from this sum."]
    if cancellation["job_budget_exhausted"]:
        lines.append("WARNING: job enrichment stopped at "
                     f"{cancellation['job_enrichments']} requests; "
                     f"{cancellation['job_budget_exhausted']} further "
                     "cancelled runs are unknown.")
    if cancellation.get("job_fetch_errors"):
        count = cancellation["job_fetch_errors"]
        lines.append(f"WARNING: {count} cancelled-run job fetch failed"
                     f"{'s' if count != 1 else ''}; affected runs are unknown. "
                     "See job_fetch_error_examples in JSON.")
    if cancellation["categories"].get("unknown"):
        lines.append("WARNING: unknown cancellation observations have incomplete "
                     "suite or job evidence; runner cost is a floor.")
    drift = cancellation["source_drift"]
    if drift["missing_from_rest"] or drift["late_cancelled"]:
        lines.append("Source drift after raw snapshot: "
                     f"{drift['missing_from_rest']} raw cancellations missing "
                     f"from REST; {drift['late_cancelled']} later cancellations "
                     "excluded from raw totals and classification.")
    if cancellation.get("completeness") == "partial":
        lines.append("Completeness: partial; inspect unknown runs and source drift.")
    return lines


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


def _fetch_slice(start: datetime, end: datetime,
                 depth: int = 0) -> tuple[list[Run], int]:
    """Fetch one bounded slice; second value counts still-capped leaves.

    A leaf that is still at the cap at maximum depth is truncated: the
    census undercounts that window, and the caller must be able to say
    so instead of publishing a silently short number.
    """
    rows = _gh_run_list(start, end)
    if len(rows) < _SLICE_LIMIT:
        return rows, 0
    if depth >= 3:
        return rows, 1
    mid = start + (end - start) / 2
    left, left_truncated = _fetch_slice(start, mid, depth + 1)
    right, right_truncated = _fetch_slice(mid, end, depth + 1)
    return left + right, left_truncated + right_truncated


def fetch_window(start: datetime, end: datetime) -> tuple[list[Run], int]:
    """Fetch every run created in [start, end) plus truncated-slice count."""
    collected: list[Run] = []
    truncated = 0
    hours = int((end - start).total_seconds() // 3600)
    for i in range(hours):
        rows, cut = _fetch_slice(
            start + timedelta(hours=i), start + timedelta(hours=i + 1))
        collected += rows
        truncated += cut
    return dedupe(collected), truncated


def main(argv: Sequence[str] | None = None) -> int:
    """Census the last 24 hours; write Markdown to stdout, JSON on request."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hours", type=int, default=24)
    parser.add_argument("--json-out", default="")
    args = parser.parse_args(argv)
    end = datetime.now(timezone.utc).replace(microsecond=0)
    start = end - timedelta(hours=args.hours)
    runs, truncated = fetch_window(start, end)
    report = aggregate(runs)
    report["truncated_slices"] = truncated
    if truncated:
        print(f"gha-census: warning: {truncated} slice(s) still capped at "
              "maximum subdivision; counts for those windows are a floor",
              file=sys.stderr)
    report["window"] = {"start": start.isoformat(), "end": end.isoformat()}
    try:
        report["required_green"] = required_green(start, end)
        report["required_green"]["completeness"] = "complete"
    except Exception as exc:  # noqa: BLE001 -- retain raw census on enrichment failure
        print(f"gha-census: warning: required-green enrichment failed: {exc}",
              file=sys.stderr)
        report["required_green"] = {"completeness": "failed", "error": str(exc)}
    try:
        report["cancellation_cost"] = cancellation_cost(start, end, runs)
        cost = report["cancellation_cost"]
        cost["completeness"] = (
            "partial" if cost["categories"].get("unknown") or
            cost["job_budget_exhausted"] or
            cost.get("job_fetch_errors") or
            cost["source_drift"]["missing_from_rest"] else "complete")
    except Exception as exc:  # noqa: BLE001 -- the enrichments are independent
        print(f"gha-census: warning: cancellation enrichment failed: {exc}",
              file=sys.stderr)
        report["cancellation_cost"] = {"completeness": "failed", "error": str(exc)}
    sys.stdout.write(render_markdown(
        report, report["window"]["start"], report["window"]["end"]))
    if args.json_out:
        with open(args.json_out, "w", encoding="utf-8") as handle:
            json.dump(report, handle, indent=2, sort_keys=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
