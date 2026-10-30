#!/usr/bin/env python3
"""Re-derive the evidence tables of the pooled readiness refit (#9519).

Each subcommand reproduces one table of
docs/internal/evidence/9519-readiness-pooled-refit.md from run directories
(run.json, jobs.json, shardN.log; see fetch.sh and trim_inputs.py). They reuse
the refresher's parser and the planner's v2.Scorer, imported from the
scripts/lib of the checkout this script runs in, so run it from the root of a
checkout of the commit whose code and weights you want to evaluate.

Subcommands:
    poolcheck  every run green, equal test inventory, one runner profile
    score      job ratio (observed job wall / predicted job seconds) per
               shard, summarized: the holdout and out-of-sample tables
    shards     per-shard job ratio and tests ratio for each run
    tail       row-level measured / predicted test time (#9348 method)
    loo        leave-one-run-out fits and the full-pool fit on extra runs
    plan       main's live plan priced under a weights file
    fixed      mean shard overhead, the v1 --fixed-job-seconds value
    walls      wall time of each cancelled readiness shard job in the runs

Usage, from the root of the evaluated checkout:
    python3 -I -B scripts/dev/readiness-refit-evidence/refit_evidence.py \
        SUBCOMMAND [ARGS...]
"""

from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import io
import json
import re
import statistics
import sys
from collections import Counter
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

sys.path.insert(0, "scripts/lib")
# pylint: disable=wrong-import-position,import-error
import live_postgres_readiness_refresh_v2 as r2  # noqa: E402
import live_postgres_readiness_v2 as v2  # noqa: E402
import live_postgres_readiness_weights as wt  # noqa: E402

# A plan of ordered test keys plus the shard bounds into it.
Plan = tuple[list[str], list[int]]

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


def run_plan(run_dir: Path, count: int) -> Plan:
    """Returns the tests the run executed, in order, and its shard bounds."""
    plan, bounds = [], [0]
    for number in range(1, count + 1):
        tests, _, _, _ = r2._parse_shard(  # pylint: disable=protected-access
            (run_dir / f"shard{number}.log").read_text(), number, count)
        plan.extend(tests)
        bounds.append(len(plan))
    return plan, bounds


def load_weights(path: Path) -> Any:
    """Returns the v2 weights object stored in a weights JSON file."""
    return v2.weights_from_json(json.loads(path.read_text()))


def percentile(values: Sequence[float], quantile: float) -> float:
    """Returns a percentile by linear interpolation, inclusive method."""
    return statistics.quantiles(values, n=100, method="inclusive")[round(quantile * 100) - 1]


def cmd_poolcheck(args: argparse.Namespace) -> int:
    """Checks a declared pool: green main pushes, equal inventory, one profile."""
    reference: Counter[str] | None = None
    profiles: set[tuple[str, ...]] = set()
    ok = True
    for run_dir in args.runs:
        run = json.loads((run_dir / "run.json").read_text())
        jobs = list(shard_jobs(run_dir).values())
        count = len(jobs)
        green = (run["conclusion"] == "success" and run["event"] == "push"
                 and run["head_branch"] == "main"
                 and all(job["conclusion"] == "success" for job in jobs))
        tests: list[str] = []
        versions: set[str] = set()
        run_profiles: set[tuple[str, ...]] = set()
        for number in range(1, count + 1):
            found, _, (profile, version), _ = r2._parse_shard(  # pylint: disable=protected-access
                (run_dir / f"shard{number}.log").read_text(), number, count)
            tests.extend(found)
            versions.add(version)
            run_profiles.add(profile)
        inventory = Counter(tests)
        same = reference is None or inventory == reference
        reference = reference or inventory
        profiles |= run_profiles
        ok &= green and same and len(run_profiles) == 1
        print(f"{run_dir.name} {run['head_sha'][:10]} attempt={run['run_attempt']} shards={count} "
              f"tests={len(tests)} green={green} inventory_equal={same} "
              f"profiles={len(run_profiles)} images={sorted(versions)}")
    print(f"one profile across pool: {len(profiles) == 1} {sorted(profiles)}")
    ok &= len(profiles) == 1
    return 0 if ok else 1


def cmd_score(args: argparse.Namespace) -> int:
    """Prints job-ratio summaries of each weights file over the scored runs."""
    fit_ids = set(args.fit_ids.split(","))
    runs = [run_dir for run_dir in args.runs if run_dir.name not in fit_ids]
    cache = {}
    for run_dir in runs:
        jobs = shard_jobs(run_dir)
        plan, bounds = run_plan(run_dir, len(jobs))
        cache[run_dir.name] = (plan, bounds, [job_wall(jobs[str(n)]) for n in range(1, len(jobs) + 1)])
    print(f"pair {sorted(fit_ids)}; out-of-sample runs {len(runs)}")
    print("weights | s1 n | s1 min | s1 p50 | s1 max | s1 >1.15 | s1 obs>pred | all n | all p50 | "
          "all p95 | all max | all >1.15 | all obs>pred | mean pred/obs")
    for path in args.weights:
        weights = load_weights(path)
        shard1, ratios, slack = [], [], []
        for plan, bounds, walls in cache.values():
            scorer = v2.Scorer(plan, weights)
            for number in range(1, len(walls) + 1):
                predicted = scorer.cost(bounds[number - 1], bounds[number]) / wt.UNIT
                ratio = walls[number - 1] / predicted
                ratios.append(ratio)
                slack.append(predicted / walls[number - 1])
                if number == 1:
                    shard1.append(ratio)
        print(f"{path.stem} | {len(shard1)} | {min(shard1):.3f} | {statistics.median(shard1):.3f} | "
              f"{max(shard1):.3f} | {sum(r > 1.15 for r in shard1)} | {sum(r > 1 for r in shard1)} | "
              f"{len(ratios)} | {statistics.median(ratios):.3f} | {percentile(ratios, 0.95):.3f} | "
              f"{max(ratios):.3f} | {sum(r > 1.15 for r in ratios)} | {sum(r > 1 for r in ratios)} | "
              f"{statistics.mean(slack):.3f}")
    return 0


def cmd_shards(args: argparse.Namespace) -> int:
    """Prints per-shard job and tests ratios for each run."""
    weights = load_weights(args.weights)
    print("run | tests | shard | rows | job wall s | predicted job s | job ratio | "
          "measured tests s | predicted tests s | tests ratio")
    for run_dir in args.runs:
        jobs = shard_jobs(run_dir)
        count = len(jobs)
        plan, bounds, measured, rows_per = [], [0], [], []
        for number in range(1, count + 1):
            log = (run_dir / f"shard{number}.log").read_text()
            tests, rows, _, _ = r2._parse_shard(log, number, count)  # pylint: disable=protected-access
            plan.extend(tests)
            bounds.append(len(plan))
            rows_per.append(len(rows))
            measured.append(measured_seconds(log))
        scorer = v2.Scorer(plan, weights)
        for number in range(1, count + 1):
            low, high = bounds[number - 1], bounds[number]
            predicted_job = scorer.cost(low, high) / wt.UNIT
            observed = job_wall(jobs[str(number)])
            measured_tests = sum(measured[number - 1][key] for key in plan[low:high])
            priced_tests = sum(weights.test.get(key, weights.default_test) for key in plan[low:high]) / wt.UNIT
            print(f"{run_dir.name} | {len(plan)} | {number} | {rows_per[number - 1]} | {observed:.0f} | "
                  f"{predicted_job:.1f} | {observed / predicted_job:.3f} | {measured_tests:.1f} | "
                  f"{priced_tests:.1f} | {measured_tests / priced_tests:.3f}")
    return 0


def cmd_tail(args: argparse.Namespace) -> int:
    """Prints the row-level measured / predicted tail for rows priced at 150 s or more."""
    weights = load_weights(args.weights)
    ratios, worst, residuals = [], [], []
    for run_dir in args.runs:
        count = len(list(run_dir.glob("shard[0-9].log")))
        for number in range(1, count + 1):
            log = (run_dir / f"shard{number}.log").read_text(encoding="utf-8")
            _, rows, _, members = r2._parse_shard(log, number, count)  # pylint: disable=protected-access
            residuals.extend(residual for _, kind, residual in rows if kind == "repeat")
            measured = measured_seconds(log)
            for row in members:
                predicted = sum(weights.test.get(key, weights.default_test) for key in row) / 10
                observed = sum(measured[key] for key in row)
                if predicted >= 150:
                    ratios.append(observed / predicted)
                    worst.append((observed / predicted, run_dir.name, number, len(row), predicted, observed))
    ratios.sort()
    quantiles = statistics.quantiles(ratios, n=100, method="inclusive")
    print(f"n={len(ratios)} p50={statistics.median(ratios):.2f} p95={quantiles[94]:.2f} "
          f"p99={quantiles[98]:.2f} max={ratios[-1]:.2f}")
    if residuals:
        print(f"repeat residual n={len(residuals)} median={statistics.median(residuals):.1f} "
              f"max={max(residuals):.1f}")
    for item in sorted(worst, reverse=True)[:3]:
        print("worst ratio=%.3f run=%s shard=%d tests=%d predicted=%.1f measured=%.1f" % item)
    return 0


def _fit(out: Path, runs: Sequence[Path], name: str) -> tuple[Any, str]:
    """Fits v2 weights on runs with the repository refresher; returns them and its stdout."""
    import live_postgres_readiness_weights_refresh as refresh  # pylint: disable=import-outside-toplevel
    path = out / f"{name}.json"
    argv = ["--schema-version", "2", "--out", str(path)]
    for run_dir in runs:
        argv += ["--sample", f"{run_dir.name}={run_dir}"]
    buffer, saved = io.StringIO(), sys.argv
    sys.argv = ["refresh", *argv]
    try:
        with contextlib.redirect_stdout(buffer):
            code = refresh.main()
    finally:
        sys.argv = saved
    if code != 0 or not path.exists():
        raise SystemExit(f"fit {name} failed: {buffer.getvalue()[-400:]}")
    return load_weights(path), buffer.getvalue()


def _score_run(run_dir: Path, model: Any, shard: int | None = None,
               forced_wall: float | None = None) -> list[tuple[Any, ...]]:
    """Returns (run, shard, count, predicted, wall, job ratio, tests ratio) rows."""
    jobs = shard_jobs(run_dir)
    count = len(jobs)
    plan, bounds = run_plan(run_dir, count)
    measured = [sum(float(m.group(3)) for line in (run_dir / f"shard{n}.log").read_text().splitlines()
                    if (m := r2.TEST_RE.fullmatch(line))) for n in range(1, count + 1)]
    scorer = v2.Scorer(plan, model)
    rows = []
    for number in range(1, count + 1):
        if shard and number != shard:
            continue
        low, high = bounds[number - 1], bounds[number]
        predicted = scorer.cost(low, high) / wt.UNIT
        priced = sum(model.test.get(key, model.default_test) for key in plan[low:high]) / wt.UNIT
        wall = forced_wall if forced_wall is not None else job_wall(jobs[str(number)])
        tests_ratio = measured[number - 1] / priced if forced_wall is None else None
        rows.append((run_dir.name, number, count, predicted, wall, wall / predicted, tests_ratio))
    return rows


def cmd_loo(args: argparse.Namespace) -> int:
    """Prints leave-one-run-out scores and full-pool scores on extra runs."""
    args.out.mkdir(parents=True, exist_ok=True)
    if args.mean:
        r2.QUANTILE_MIN_POOL = 10 ** 6
    rows: list[tuple[Any, ...]] = []
    for held in args.pool:
        model, _ = _fit(args.out, [run for run in args.pool if run != held], f"loo-{held.name}")
        rows += [("loo", *row) for row in _score_run(held, model)]
    full, stdout = _fit(args.out, args.pool, "full")
    print(stdout.strip().splitlines()[-1] if stdout.strip() else "")
    for run_dir in args.extra:
        rows += [("ext", *row) for row in _score_run(run_dir, full)]
    for spec in args.censored:
        run_dir, shard, wall = spec.rsplit(":", 2)
        rows += [("cens", *row) for row in _score_run(Path(run_dir), full, int(shard), float(wall))]
    print("set | run | shard | predicted s | wall s | job ratio | tests ratio")
    for row in rows:
        tests = "-" if row[7] is None else f"{row[7]:.3f}"
        print(f"{row[0]} | {row[1]} | {row[2]}/{row[3]} | {row[4]:.1f} | {row[5]:.0f} | {row[6]:.3f} | {tests}")
    groups = (("LOO pool", ("loo",)), ("full-pool fit on extra runs", ("ext", "cens")),
              ("all", ("loo", "ext", "cens")))
    for label, sets in groups:
        shard1 = [row for row in rows if row[0] in sets and row[2] == 1]
        others = [row for row in rows if row[0] in sets and row[2] != 1]
        if not shard1:
            continue
        def over(group: list[tuple[Any, ...]], bar: float) -> str:
            """Returns "count/total" for rows whose job ratio exceeds bar."""
            return f"{sum(row[6] > bar for row in group)}/{len(group)}"
        def median_tests(group: list[tuple[Any, ...]]) -> float:
            """Returns the upper median tests ratio, or NaN if none is known."""
            values = sorted(row[7] for row in group if row[7] is not None)
            return values[len(values) // 2] if values else float("nan")
        print(f"{label}: job ratio >1.30 shard1 {over(shard1, 1.30)} vs shards2+ {over(others, 1.30)}; "
              f">1.15 shard1 {over(shard1, 1.15)} vs {over(others, 1.15)}; "
              f"max shard1 {max(row[6] for row in shard1):.3f} others {max(row[6] for row in others):.3f}; "
              f"median tests ratio shard1 {median_tests(shard1):.3f} others {median_tests(others):.3f}")
    return 0


def cmd_plan(args: argparse.Namespace) -> int:
    """Prints main's live plan priced under a weights file, as the shards page does."""
    # pylint: disable=import-outside-toplevel
    from live_postgres_readiness_balance_cases import live_plan, slow_runs
    weights, plan = wt.load_weights(args.weights), live_plan()
    drift = wt.drift_report(plan, weights)
    print(f"plan {len(plan)}; defaulted tests {len(drift.defaulted_tests)}; "
          f"defaulted packages {len(drift.defaulted_packages)}; stale {len(drift.stale_tests)}; "
          f"drift errors {wt.drift_errors(drift)}; budget errors {wt.budget_errors(plan, weights)}")
    bounds = wt.weighted_bounds(plan, weights)
    scorer = v2.Scorer(plan, weights)
    jobs = [scorer.cost(low, high) / wt.UNIT for low, high in zip(bounds, bounds[1:])]
    mean = sum(jobs) / len(jobs)
    print("jobs(s) " + " / ".join(f"{job:.1f}" for job in jobs)
          + f"; max deviation {max(abs(job - mean) for job in jobs) / mean * 100:.2f}%")
    row_count, largest = 0, (0.0, "")
    for number, (low, high) in enumerate(zip(bounds, bounds[1:]), 1):
        rows = v2.split_rows(plan[low:high], weights)
        row_count += len(rows)
        per_package: dict[str, int] = {}
        for row in rows:
            package = row[0].split("|")[0]
            per_package[package] = per_package.get(package, 0) + 1
            cost = v2.row_cost_tenths(row, weights) / wt.UNIT
            if cost > largest[0]:
                largest = (cost, f"s{number} {package} {len(row)} tests")
        multi = {package: count for package, count in per_package.items() if count > 1}
        print(f"  s{number}: tests={high - low} rows={len(rows)} multi={ multi }")
    print(f"rows {row_count}; max row {largest[0]:.1f} s ({largest[1]})")
    package = plan[-1].split("|")[0]
    failing = [k for k in range(121)
               if wt.budget_errors([*plan, *slow_runs(k, "TestGrow", package)], weights)]
    contiguous = failing == list(range(failing[0], failing[-1] + 1)) if failing else True
    print("window: " + (f"fails k={failing[0]}..{failing[-1]}; contiguous={contiguous}"
                        if failing else "no failing k in 0..120"))
    return 0


def cmd_fixed(args: argparse.Namespace) -> int:
    """Prints the mean of all shard overheads, the v1 --fixed-job-seconds value."""
    # pylint: disable=import-outside-toplevel
    import live_postgres_readiness_refresh_v2_provenance as provenance
    root = Path(".").resolve()
    overheads: list[float] = []
    for run_dir in args.runs:
        overheads.extend(provenance.read_source(run_dir, root)[4])
    print(f"{round(statistics.mean(overheads), 1)}")
    return 0


def cmd_walls(args: argparse.Namespace) -> int:
    """Prints the wall time of each cancelled readiness shard job in the runs.

    Cancelled cap jobs carry no usable shard log, so their only archived
    input is the jobs API listing (see fetch.sh): job identity and timing.
    """
    print("run | attempt | shard | job | wall s")
    for run_dir in args.runs:
        for number, job in sorted(shard_jobs(run_dir).items(), key=lambda item: int(item[0])):
            if job["conclusion"] == "cancelled":
                print(f"{run_dir.name} | {job.get('run_attempt', '?')} | {number} | "
                      f"{job['id']} | {job_wall(job):.0f}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    """Returns the command-line parser for all subcommands."""
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    paths = {"type": Path, "nargs": "+"}
    p = sub.add_parser("poolcheck", help=cmd_poolcheck.__doc__)
    p.add_argument("runs", **paths)
    p.set_defaults(func=cmd_poolcheck)
    p = sub.add_parser("score", help=cmd_score.__doc__)
    p.add_argument("fit_ids", help="comma-separated run IDs of the fit, excluded from scoring")
    p.add_argument("--weights", type=Path, nargs="+", required=True)
    p.add_argument("--runs", type=Path, nargs="+", required=True)
    p.set_defaults(func=cmd_score)
    for name, func in (("shards", cmd_shards), ("tail", cmd_tail)):
        p = sub.add_parser(name, help=func.__doc__)
        p.add_argument("weights", type=Path)
        p.add_argument("runs", **paths)
        p.set_defaults(func=func)
    p = sub.add_parser("loo", help=cmd_loo.__doc__)
    p.add_argument("out", type=Path, help="directory for the fitted weights files")
    p.add_argument("--mean", action="store_true", help="price at the mean instead of p75")
    p.add_argument("--pool", type=Path, nargs="+", required=True)
    p.add_argument("--extra", type=Path, nargs="*", default=[])
    p.add_argument("--censored", nargs="*", default=[], help="RUN_DIR:SHARD:WALL_SECONDS")
    p.set_defaults(func=cmd_loo)
    p = sub.add_parser("plan", help=cmd_plan.__doc__)
    p.add_argument("weights", type=Path)
    p.set_defaults(func=cmd_plan)
    p = sub.add_parser("fixed", help=cmd_fixed.__doc__)
    p.add_argument("runs", **paths)
    p.set_defaults(func=cmd_fixed)
    p = sub.add_parser("walls", help=cmd_walls.__doc__)
    p.add_argument("runs", **paths)
    p.set_defaults(func=cmd_walls)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    """Runs the selected subcommand."""
    args = build_parser().parse_args(argv)
    func: Callable[[argparse.Namespace], int] = args.func
    return func(args)


if __name__ == "__main__":
    sys.exit(main())
