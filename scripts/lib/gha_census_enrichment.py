#!/usr/bin/env python3
"""Bounded GitHub API enrichment for the daily CI census.

Required-green latency covers PRs merged in the window, from first observed
PR workflow enqueue on their final head to three green contexts by merge.
Cancellation accounting covers cancelled runs created in that same window.
"""

from collections import Counter
from datetime import datetime, timedelta
import json
import math
import subprocess
from typing import Any
from urllib.parse import quote

Json = dict[str, Any]
UNKNOWN_WORKFLOW = "Unknown workflow"
REPOSITORY = "repos/eshu-hq/eshu"
REQUIRED_CHECKS = ("go-core-complete", "go-race-complete")
REQUIRED_STATUS = "required-gates-complete"
PAGE_SIZE = 100
MAX_PAGES = 10
MAX_JOB_ENRICHMENTS = 500
MAX_JOB_PAGES = 2


def timestamp(value: str | None) -> datetime | None:
    """Parse a GitHub UTC timestamp, retaining unknown values as unknown."""
    if not value:
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def percentile(values: list[float], fraction: float) -> float | None:
    """Return the nearest-rank percentile on an observed population."""
    if not values:
        return None
    ordered = sorted(values)
    return round(ordered[max(0, math.ceil(len(ordered) * fraction) - 1)], 2)


def measure_head(pr: Json, runs: list[Json], checks: list[Json],
                 statuses: list[Json]) -> Json:
    """Measure one merged final head without treating merge as green."""
    merged = timestamp(pr.get("merged_at"))
    if merged is None:
        raise ValueError(f"PR {pr.get('number')} has no merge timestamp")
    enqueue_times = [time for run in runs
                     if (time := timestamp(run.get("created_at"))) and time <= merged]
    enqueue = min(enqueue_times, default=None)
    selected: dict[str, Json] = {}
    ambiguous_check = False
    for check in checks:
        if check.get("name") not in REQUIRED_CHECKS:
            continue
        start_time = timestamp(check.get("started_at") or
                               check.get("completed_at"))
        if start_time is None:
            ambiguous_check = True
            continue
        if start_time <= merged:
            previous = selected.get(check["name"])
            previous_time = timestamp(previous.get("started_at") or
                                      previous.get("completed_at")) if previous else None
            if previous_time is None or start_time > previous_time:
                selected[check["name"]] = check
    publisher = [status for status in statuses
                 if status.get("context") == REQUIRED_STATUS and
                 (time := timestamp(status.get("created_at"))) and time <= merged]
    latest_status = max(publisher, key=lambda row: timestamp(row["created_at"])) \
        if publisher else None
    states = [selected[name].get("conclusion")
              if (completed := timestamp(selected[name].get("completed_at")))
              and completed <= merged else "pending"
              for name in REQUIRED_CHECKS if name in selected]
    if latest_status:
        states.append(latest_status.get("state"))
    if ambiguous_check:
        outcome = "unknown"
    elif any(state in ("failure", "error", "timed_out", "action_required")
             for state in states):
        outcome = "failure"
    elif any(state in ("pending", None) for state in states):
        outcome = "pending"
    elif len(selected) < len(REQUIRED_CHECKS) or latest_status is None:
        outcome = "missing"
    elif all(state == "success" for state in states) and \
            latest_status.get("state") == "success":
        outcome = "success"
    else:
        outcome = "unknown"
    description = latest_status.get("description") if latest_status else ""
    kind = ("reuse" if description and "Tree-identical reuse" in description
            else "smoke" if description and "Smoke subset" in description
            else "ordinary" if description ==
            "All path-selected blocking gates passed" else "unknown")
    green_times = ([timestamp(selected[name].get("completed_at"))
                    for name in REQUIRED_CHECKS] +
                   [timestamp(latest_status.get("created_at"))]) \
        if latest_status and len(selected) == len(REQUIRED_CHECKS) else []
    last_green = max(green_times) if green_times and all(green_times) else None
    if outcome == "success" and (enqueue is None or last_green is None):
        outcome = "missing"
    minutes = round((last_green - enqueue).total_seconds() / 60, 2) \
        if outcome == "success" else None
    return {"number": pr["number"], "outcome": outcome, "kind": kind,
            "minutes": minutes, "head_sha": pr["head"]["sha"] if "head" in pr else None}


def classify_cancellation(job_count: int | None,
                          jobs: list[Json] | None) -> tuple[str, float | None]:
    """Classify a cancelled run and sum only evidenced runner occupancy."""
    if job_count is None or jobs is None or len(jobs) != job_count:
        return "unknown", None
    if job_count == 0:
        return "zero_job", 0.0
    allocated: list[tuple[Json, int, datetime, datetime]] = []
    for job in jobs:
        if not isinstance(job, dict):
            return "unknown", None
        runner_id = job.get("runner_id")
        if type(runner_id) is not int or runner_id <= 0:
            continue
        start = timestamp(job.get("started_at"))
        end = timestamp(job.get("completed_at"))
        if start is None or end is None or end < start:
            return "unknown", None
        allocated.append((job, runner_id, start, end))
    if not allocated:
        return "unknown", None

    # GitHub may copy an earlier successful job into a later attempt with a
    # new job ID. Only the full native identity can prove one physical interval.
    seen: dict[tuple[int, datetime, datetime],
               dict[int | None, dict[str, dict[str, Any]]]] = {}
    seconds = 0.0
    for job, runner_id, start, end in allocated:
        name = job.get("name")
        exact_name = name if isinstance(name, str) and name else ""
        run_id = job.get("run_id")
        native_run_id = run_id if type(run_id) is int and run_id > 0 else None
        job_id = job.get("id")
        attempt = job.get("run_attempt")
        copy_proof = (end > start and bool(exact_name) and
                      native_run_id is not None and
                      type(job_id) is int and job_id > 0 and
                      type(attempt) is int and attempt > 0 and
                      job.get("status") == "completed" and
                      job.get("conclusion") == "success")
        interval = (runner_id, start, end)
        runs = seen.setdefault(interval, {})
        if (native_run_id is None and runs) or None in runs:
            return "unknown", None
        names = runs.setdefault(native_run_id, {})
        if (not exact_name and names) or (exact_name and "" in names):
            return "unknown", None
        previous = names.get(exact_name)
        if previous is not None:
            if not copy_proof or not previous["copy_proof"] or \
                    job_id in previous["ids"] or attempt in previous["attempts"]:
                return "unknown", None
            previous["ids"].add(job_id)
            previous["attempts"].add(attempt)
            continue
        names[exact_name] = {
            "copy_proof": copy_proof,
            "ids": {job_id} if copy_proof else set(),
            "attempts": {attempt} if copy_proof else set(),
        }
        seconds += (end - start).total_seconds()
    return "executed", round(seconds / 60, 2)


def _api(path: str) -> Json | list[Json]:
    """Issue one authenticated GitHub REST call; propagate failures."""
    result = subprocess.run(["gh", "api", path], capture_output=True,
                            text=True, check=True, timeout=90)
    return json.loads(result.stdout)


def _pages(path: str, array_key: str | None = None,
           page_limit: int | None = None) -> list[Json]:
    """Fetch every page up to the explicit API limit, failing if incomplete."""
    rows: list[Json] = []
    limit = page_limit if page_limit is not None else MAX_PAGES
    for page in range(1, limit + 1):
        separator = "&" if "?" in path else "?"
        payload = _api(f"{path}{separator}per_page={PAGE_SIZE}&page={page}")
        batch = payload[array_key] if array_key else payload
        rows.extend(batch)
        total = payload.get("total_count") if array_key else None
        if total is not None:
            if len(rows) >= total:
                return rows
            if len(batch) < PAGE_SIZE:
                raise RuntimeError(f"GitHub API incomplete page for {path}")
        elif len(batch) < PAGE_SIZE:
            return rows
    raise RuntimeError(f"GitHub API page bound reached for {path}")


def _merged_prs(start: datetime, end: datetime) -> list[Json]:
    """Fetch PRs merged in the window from bounded newest-updated pages."""
    path = f"{REPOSITORY}/pulls?state=closed&sort=updated&direction=desc"
    rows: list[Json] = []
    for page in range(1, MAX_PAGES + 1):
        payload = _api(f"{path}&per_page={PAGE_SIZE}&page={page}")
        rows.extend(pr for pr in payload
                    if (merged := timestamp(pr.get("merged_at"))) and
                    start <= merged < end)
        if len(payload) < PAGE_SIZE or all(
                (updated := timestamp(pr.get("updated_at"))) and updated < start
                for pr in payload):
            return rows
    raise RuntimeError("Merged PR pagination reached its 1000-row bound")


def required_green(start: datetime, end: datetime) -> Json:
    """Measure final-head required-green for all PRs merged in the window."""
    heads = []
    for pr in _merged_prs(start, end):
        sha = pr["head"]["sha"]
        runs = _pages(f"{REPOSITORY}/actions/runs?head_sha={sha}&event=pull_request",
                      "workflow_runs")
        checks = _pages(f"{REPOSITORY}/commits/{sha}/check-runs?filter=all", "check_runs")
        statuses = _pages(f"{REPOSITORY}/commits/{sha}/statuses")
        heads.append(measure_head(pr, runs, checks, statuses))
    groups = {}
    for kind in ("ordinary", "reuse", "smoke", "unknown"):
        subset = [head for head in heads if head["kind"] == kind]
        values = [head["minutes"] for head in subset if head["minutes"] is not None]
        groups[kind] = {"heads": len(subset), "success": len(values),
                        "p50_minutes": percentile(values, 0.5),
                        "p95_minutes": percentile(values, 0.95)}
    all_success = [head["minutes"] for head in heads
                   if head["minutes"] is not None]
    overall = {"heads": len(heads), "success": len(all_success),
               "p50_minutes": percentile(all_success, 0.5),
               "p95_minutes": percentile(all_success, 0.95)}
    return {"cohort": "PRs merged in window; final head, green by merge",
            "enqueue_boundary": "first observed pull_request workflow created_at",
            "end_boundary": "last of three successful required contexts by merge",
            "push_to_dispatch_delay": "NOT_CHECKED",
            "heads": heads, "outcomes": dict(Counter(h["outcome"] for h in heads)),
            "overall": overall, "groups": groups}


def _cancelled_runs(start: datetime, end: datetime) -> list[Json]:
    """List cancelled runs in hourly slices so GitHub's cap cannot hide rows."""
    rows = []
    cursor = start
    while cursor < end:
        next_hour = min(cursor + timedelta(hours=1), end)
        created = quote(f"{cursor:%Y-%m-%dT%H:%M:%SZ}.."
                        f"{next_hour:%Y-%m-%dT%H:%M:%SZ}")
        rows.extend(_pages(f"{REPOSITORY}/actions/runs?status=cancelled"
                           f"&created={created}", "workflow_runs"))
        cursor = next_hour
    return list({run["id"]: run for run in rows}.values())


def _suite_counts(node_ids: list[str]) -> dict[str, int | None]:
    """Batch check-suite job counts through GraphQL, 100 nodes per request."""
    counts: dict[str, int | None] = {}
    query = ("query($ids:[ID!]!){nodes(ids:$ids){id ... on CheckSuite"
             "{checkRuns(first:1){totalCount}}}}")
    for offset in range(0, len(node_ids), 100):
        batch = node_ids[offset:offset + 100]
        command = ["gh", "api", "graphql", "-f", f"query={query}"]
        for node_id in batch:
            command.extend(["-F", f"ids[]={node_id}"])
        result = subprocess.run(command, capture_output=True, text=True,
                                check=True, timeout=90)
        payload = json.loads(result.stdout)
        if payload.get("errors"):
            raise RuntimeError(f"GraphQL check-suite query failed: {payload['errors']}")
        for node in payload["data"]["nodes"]:
            if node is not None:
                counts[node["id"]] = node.get("checkRuns", {}).get("totalCount")
    return counts


def cancellation_cost(start: datetime, end: datetime,
                      census_runs: list[Json]) -> Json:
    """Account for cancelled runs without changing the raw census totals."""
    raw_workflows = {run["databaseId"]: run.get("workflowName") or UNKNOWN_WORKFLOW
                     for run in census_runs if run.get("conclusion") == "cancelled"}
    expected = raw_workflows.keys()
    fetched = {run["id"]: run for run in _cancelled_runs(start, end)}
    missing = expected - fetched.keys()
    extra = fetched.keys() - expected
    matched = {run_id: fetched[run_id] for run_id in expected & fetched.keys()}
    suites = _suite_counts(list({run["check_suite_node_id"] for run in matched.values()
                                 if run.get("check_suite_node_id")}))
    categories: Counter[str] = Counter()
    by_workflow: dict[str, Counter[str]] = {
        name: Counter() for name in raw_workflows.values()}
    if missing:
        categories["unknown"] = len(missing)
        for run_id in missing:
            by_workflow[raw_workflows[run_id]]["unknown"] += 1
    runner_minutes = 0.0
    examples: dict[str, list[int]] = {key: [] for key in
                                      ("zero_job", "executed", "unknown")}
    examples["unknown"].extend(sorted(missing)[:3])
    job_enrichments = 0
    job_fetch_errors = 0
    job_fetch_error_examples: list[Json] = []
    budget_exhausted = 0
    for run in matched.values():
        count = suites.get(run.get("check_suite_node_id"))
        needs_jobs = bool(count) or run.get("run_attempt", 1) > 1
        if needs_jobs and job_enrichments >= MAX_JOB_ENRICHMENTS:
            jobs = None
            budget_exhausted += 1
        elif needs_jobs:
            job_enrichments += 1
            try:
                jobs = _pages(
                    f"{REPOSITORY}/actions/runs/{run['id']}/jobs?filter=all",
                    "jobs", page_limit=MAX_JOB_PAGES)
            except (RuntimeError, subprocess.CalledProcessError,
                    subprocess.TimeoutExpired, json.JSONDecodeError,
                    KeyError, TypeError) as exc:
                jobs = None
                job_fetch_errors += 1
                if len(job_fetch_error_examples) < 3:
                    job_fetch_error_examples.append({
                        "run_id": run["id"],
                        "error": f"{type(exc).__name__}: {exc}"[:200]})
        else:
            jobs = [] if count == 0 else None
        # A retry suite can describe only its current attempt, while REST
        # filter=all includes earlier attempts. Its complete count may grow,
        # but it must never be smaller than the independently observed suite.
        retry = run.get("run_attempt", 1) > 1
        if retry and jobs is not None and (count is None or len(jobs) >= count):
            authoritative_count = len(jobs)
        else:
            authoritative_count = count
        category, minutes = classify_cancellation(authoritative_count, jobs)
        categories[category] += 1
        by_workflow[raw_workflows[run["id"]]][category] += 1
        if minutes is not None:
            runner_minutes += minutes
        if len(examples[category]) < 3:
            examples[category].append(run["id"])
    return {"population": "cancelled workflow runs created in window",
            "runs": len(expected), "categories": dict(categories),
            "by_workflow": {
                name: {"runs": sum(counts.values()), "categories": dict(counts)}
                for name, counts in by_workflow.items()},
            "source_drift": {"missing_from_rest": len(missing),
                             "late_cancelled": len(extra)},
            "evidenced_runner_minutes": round(runner_minutes, 2),
            "job_enrichments": job_enrichments,
            "job_fetch_errors": job_fetch_errors,
            "job_fetch_error_examples": job_fetch_error_examples,
            "job_budget_exhausted": budget_exhausted,
            "examples": examples}
