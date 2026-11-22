#!/usr/bin/env python3
"""Record and summarize where `make pre-push` / `make pre-pr` / CI spend time.

Subcommands:
  record   Append one JSON line describing a finished local run. Called by
           scripts/lib/gate-timing.sh. It never fails the caller.
  summary  Medians and maxima per step, gate, and package across recorded runs.
  ci       Per-job queue and run time for recent GitHub Actions runs (needs gh).

The local store is ~/.cache/eshu-gate-timing/runs.jsonl (override with
ESHU_GATE_TIMING_DIR; ESHU_GATE_TIMING=0 turns recording off). It lives outside
the repo on purpose: it is a measurement of this host, never committed.
"""

import argparse
from collections import defaultdict
from dataclasses import dataclass
import datetime as dt
import json
import os
from pathlib import Path
import re
import statistics
import subprocess
import sys
from typing import Any, Iterable

SCHEMA = "eshu-gate-timing/v1"
# Load average above which a run is flagged: a timing taken on a busy host
# measures the host as much as the gate (the repo's timing-proof rule uses 9).
NOISY_LOAD1 = 9.0
_TEST_LINE = re.compile(r"^(ok|FAIL)\s+(\S+)\s+([0-9.]+)s\b")
_SHARD = re.compile(r"shard \(\d+/\d+\)")
_RACE_SHARD = re.compile(r"\((\d+)\)$")


def store_path() -> Path:
    """Return the JSONL file runs are appended to."""
    base = os.environ.get("ESHU_GATE_TIMING_DIR")
    root = Path(base) if base else Path.home() / ".cache" / "eshu-gate-timing"
    return root / "runs.jsonl"


def read_tsv(path: str | None) -> list[list[str]]:
    """Read a tab-separated file; a missing or unreadable file is empty."""
    if not path:
        return []
    try:
        text = Path(path).read_text()
    except OSError:
        return []
    return [line.split("\t") for line in text.splitlines() if line.strip()]


def parse_steps(rows: list[list[str]]) -> list[dict[str, Any]]:
    """Turn `name<TAB>seconds<TAB>exit` rows into step records."""
    steps = []
    for row in rows:
        if len(row) != 3:
            continue
        steps.append({"name": row[0], "seconds": int(float(row[1])), "exit": int(row[2])})
    return steps


def parse_gates(path: str | None) -> list[dict[str, Any]]:
    """Read the ci-gates `--report-file` JSON; keep only commands that ran."""
    if not path:
        return []
    try:
        report = json.loads(Path(path).read_text())
    except (OSError, ValueError):
        return []
    gates = []
    for cmd in report.get("commands", []):
        # A skipped or reused command cost nothing in this run, so it is not
        # counted as time spent.
        if cmd.get("outcome") == "skipped" or cmd.get("reused_from"):
            continue
        gates.append({
            "gate_id": cmd.get("gate_id", ""),
            "kind": cmd.get("kind", ""),
            "outcome": cmd.get("outcome", ""),
            "seconds": round(cmd.get("duration_ms", 0) / 1000.0, 1),
        })
    return gates


def parse_packages(path: str | None, mode: str) -> list[dict[str, Any]]:
    """Read per-package seconds from captured `go test` output."""
    if not path:
        return []
    try:
        text = Path(path).read_text(errors="replace")
    except OSError:
        return []
    packages = []
    for line in text.splitlines():
        match = _TEST_LINE.match(line)
        if match:
            packages.append({"mode": mode, "pkg": match.group(2),
                             "seconds": float(match.group(3))})
    return packages


def render_slowest(steps: list[dict[str, Any]], count: int = 5) -> str:
    """Return the slowest steps of one run as a short table."""
    ranked = sorted(steps, key=lambda s: -s["seconds"])[:count]
    lines = ["slowest steps this run:"]
    lines += [f"  {s['seconds']:>6}s  {s['name']}" for s in ranked]
    return "\n".join(lines)


def command_record(args: argparse.Namespace) -> int:
    """Append one run record. Any failure is reported and swallowed."""
    if os.environ.get("ESHU_GATE_TIMING") == "0":
        return 0
    steps = parse_steps(read_tsv(args.steps))
    now = dt.datetime.now(dt.timezone.utc)
    try:
        load_end = os.getloadavg()[0]
    except OSError:
        load_end = -1.0
    run = {
        "schema": SCHEMA,
        "script": args.script,
        "ts": now.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "base": args.base,
        "head": git_value("rev-parse", "--short", "HEAD"),
        "branch": git_value("symbolic-ref", "--short", "-q", "HEAD"),
        "cpus": os.cpu_count(),
        "load1_start": args.load_start,
        "load1_end": round(load_end, 2),
        "wall_s": max(0, int(now.timestamp()) - args.start_epoch),
        "overall": args.overall,
        "meta": {r[0]: r[1] for r in read_tsv(args.meta) if len(r) == 2},
        "steps": steps,
        "gates": parse_gates(args.gate_report),
        "packages": parse_packages(args.test_log, "test")
        + parse_packages(args.race_log, "race"),
    }
    try:
        target = store_path()
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("a") as handle:
            handle.write(json.dumps(run, sort_keys=True) + "\n")
    except OSError as err:
        print(f"gate-timing: timing not recorded ({err})", file=sys.stderr)
        return 0
    print(render_slowest(steps))
    print(f"gate-timing: recorded to {target} (summary: scripts/dev/gate-timing.py summary)")
    return 0


def git_value(*argv: str) -> str:
    """Return one git value, or an empty string when git cannot answer."""
    try:
        out = subprocess.run(["git", *argv], capture_output=True, text=True,
                             check=False, timeout=10)
    except (OSError, subprocess.TimeoutExpired):
        return ""
    return out.stdout.strip()


def _row(name: str, values: list[float], width: int) -> str:
    return (f"{name[:width]:<{width}} {len(values):>3} "
            f"{statistics.median(values):>7.0f} {max(values):>7.0f}")


def render_summary(runs: list[dict[str, Any]], top: int) -> str:
    """Return median / max seconds per step, gate, and package over runs."""
    noisy = sum(1 for r in runs if r.get("load1_end", 0) > NOISY_LOAD1)
    out = [f"{len(runs)} run(s), {noisy} noisy (load1_end > {NOISY_LOAD1:g}); "
           "seconds are wall time per run"]
    steps: dict[str, list[float]] = defaultdict(list)
    gates: dict[str, list[float]] = defaultdict(list)
    pkgs: dict[str, list[float]] = defaultdict(list)
    for run in runs:
        for step in run.get("steps", []):
            steps[step["name"]].append(step["seconds"])
        for gate in run.get("gates", []):
            gates[f"{gate['gate_id']} [{gate['kind']}]"].append(gate["seconds"])
        for pkg in run.get("packages", []):
            pkgs[f"{pkg['mode']} {pkg['pkg'].split('/go/', 1)[-1]}"].append(pkg["seconds"])
    for title, table in (("steps", steps), ("gates", gates), ("packages", pkgs)):
        if not table:
            continue
        out.append("")
        out.append(f"{title:<62} {'n':>3} {'median':>7} {'max':>7}")
        ranked = sorted(table.items(), key=lambda kv: -statistics.median(kv[1]))
        out += [_row(name, vals, 62) for name, vals in ranked[:top]]
    return "\n".join(out)


def command_summary(args: argparse.Namespace) -> int:
    """Print the cross-run summary."""
    try:
        lines = store_path().read_text().splitlines()
    except OSError:
        print("gate-timing: no recorded runs yet", file=sys.stderr)
        return 1
    runs = [json.loads(line) for line in lines if line.strip()]
    if args.script:
        runs = [r for r in runs if r.get("script") == args.script]
    runs = runs[-args.last:]
    if not runs:
        print("gate-timing: no matching runs", file=sys.stderr)
        return 1
    print(render_summary(runs, args.top))
    return 0


@dataclass
class JobGroup:
    """Aggregate of one CI job (shards collapsed) across runs."""

    per_run: float
    median_s: float
    max_s: float
    minutes_per_run: float


def normalize_job(name: str) -> str:
    """Collapse shard numbers so shards of one job aggregate together."""
    name = _SHARD.sub("shard", name)
    return _RACE_SHARD.sub("(N)", name)


def aggregate_jobs(rows: Iterable[dict[str, Any]]) -> dict[str, JobGroup]:
    """Group job rows by normalized name; report counts, spread and cost."""
    rows = list(rows)
    run_count = max(1, len({r["run"] for r in rows}))
    groups: dict[str, list[float]] = defaultdict(list)
    for row in rows:
        groups[normalize_job(row["name"])].append(float(row["seconds"]))
    return {
        name: JobGroup(per_run=len(v) / run_count, median_s=statistics.median(v),
                       max_s=max(v), minutes_per_run=sum(v) / run_count / 60.0)
        for name, v in groups.items()
    }


def gh_json(argv: list[str]) -> Any:
    """Run gh and parse JSON, retrying once on a transient server error."""
    for attempt in (1, 2):
        out = subprocess.run(["gh", *argv], capture_output=True, text=True, check=False)
        if out.returncode == 0:
            return json.loads(out.stdout or "null")
        if attempt == 2:
            raise RuntimeError(out.stderr.strip() or "gh failed")
    return None


def fetch_job_rows(repo: str, run_id: str) -> list[dict[str, Any]]:
    """Fetch one run's jobs as queue-wait and run-seconds rows."""
    data = gh_json(["api", f"repos/{repo}/actions/runs/{run_id}/jobs?per_page=100",
                    "--paginate", "--jq", "[.jobs[] | {name, started_at, created_at, completed_at}]"])
    rows = []
    for job in data or []:
        if not (job.get("started_at") and job.get("completed_at")):
            continue
        start = dt.datetime.fromisoformat(job["started_at"].replace("Z", "+00:00"))
        end = dt.datetime.fromisoformat(job["completed_at"].replace("Z", "+00:00"))
        made = dt.datetime.fromisoformat(job["created_at"].replace("Z", "+00:00"))
        rows.append({"run": run_id, "name": job["name"],
                     "queue": (start - made).total_seconds(),
                     "seconds": (end - start).total_seconds()})
    return rows


def command_ci(args: argparse.Namespace) -> int:
    """Summarize recent CI runs of one workflow."""
    runs = gh_json(["run", "list", "--repo", args.repo, "--workflow", args.workflow,
                    "--event", "pull_request", "--status", "completed",
                    "--limit", str(args.runs), "--json", "databaseId"])
    rows: list[dict[str, Any]] = []
    for run in runs or []:
        try:
            rows += fetch_job_rows(args.repo, str(run["databaseId"]))
        except RuntimeError as err:
            print(f"gate-timing: skipped run {run['databaseId']}: {err}", file=sys.stderr)
    if not rows:
        print("gate-timing: no CI job data", file=sys.stderr)
        return 1
    groups = aggregate_jobs(rows)
    queue = statistics.median(r["queue"] for r in rows)
    print(f"{len({r['run'] for r in rows})} run(s); median queue wait {queue:.0f}s; "
          f"{sum(g.minutes_per_run for g in groups.values()):.0f} job-min/run")
    print(f"{'job':<66} {'n/run':>5} {'med_m':>6} {'max_m':>6} {'min/run':>8}")
    for name, g in sorted(groups.items(), key=lambda kv: -kv[1].minutes_per_run)[:args.top]:
        print(f"{name[:66]:<66} {g.per_run:>5.1f} {g.median_s / 60:>6.1f} "
              f"{g.max_s / 60:>6.1f} {g.minutes_per_run:>8.1f}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    """Build the command-line parser."""
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="command", required=True)
    rec = sub.add_parser("record", help="append one local run record")
    rec.add_argument("--script", required=True)
    rec.add_argument("--base", default="")
    rec.add_argument("--overall", type=int, default=0)
    rec.add_argument("--steps")
    rec.add_argument("--meta")
    rec.add_argument("--gate-report")
    rec.add_argument("--test-log")
    rec.add_argument("--race-log")
    rec.add_argument("--start-epoch", type=int, default=0)
    rec.add_argument("--load-start", type=float, default=-1.0)
    rec.set_defaults(func=command_record)
    summ = sub.add_parser("summary", help="medians across recorded runs")
    summ.add_argument("--script", choices=("pre-push", "pre-pr"))
    summ.add_argument("--last", type=int, default=20)
    summ.add_argument("--top", type=int, default=15)
    summ.set_defaults(func=command_summary)
    ci = sub.add_parser("ci", help="per-job time over recent CI runs (needs gh)")
    ci.add_argument("--repo", default="eshu-hq/eshu")
    ci.add_argument("--workflow", default="Build Test")
    ci.add_argument("--runs", type=int, default=8)
    ci.add_argument("--top", type=int, default=20)
    ci.set_defaults(func=command_ci)
    return parser


def main() -> int:
    """Entry point."""
    args = build_parser().parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
