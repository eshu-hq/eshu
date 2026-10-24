#!/usr/bin/env python3
"""Refresh the readiness shard weights from green CI shard logs (#8930).

Input is the raw log of each shard job (``gh api repos/OWNER/REPO/actions/jobs/ID/logs``)
from one or more full green runs. A run is one *sample*. The refresher reads
three line shapes the runner already prints:

* ``##[group]Run bash scripts/run-live-postgres-readiness-tests.sh --shard ...``
  starts the proof step,
* ``<Test>: PASS elapsed=<s>s`` carries one test's seconds,
* ``live-postgres-readiness: <package> <n>/<n> PASS`` closes a package.

A package's wall time is the gap between its closing line and the previous
closing line (the step start for the first package). Wall time minus the sum of
its tests' seconds is the package overhead (compile and link). The overhead of
a package that is not first in its shard becomes the package cost; the extra
overhead of a first package over that cost is the cold-cache constant.

Usage:
    live_postgres_readiness_weights_refresh.py --out specs/live-postgres-readiness-weights.v1.json \\
        --fixed-job-seconds 82 --sample 40049588589=DIR_A --sample 40047410741=DIR_B

Each DIR holds that run's shard logs as ``*.log``. Only tests in the current
inventory are kept; inventory tests absent from every log get the default.
"""

from __future__ import annotations

import argparse
from dataclasses import dataclass, field
import datetime as dt
import json
from pathlib import Path
import re
import statistics
import sys

sys.path.insert(0, str(Path(__file__).parent))
import live_postgres_readiness_weights as wt  # noqa: E402

START_RE = re.compile(
    r"^﻿?(\S+Z) ##\[group\]Run bash scripts/run-live-postgres-readiness-tests\.sh --shard")
TEST_RE = re.compile(r"^﻿?(\S+Z) (Test[A-Za-z0-9_]+): PASS elapsed=([0-9.]+)s")
PACKAGE_RE = re.compile(
    r"^﻿?(\S+Z) live-postgres-readiness: (\./\S+) (\d+)/(\d+) PASS")


@dataclass
class Sample:
    """Measurements from one full run's shard logs."""

    tests: dict[str, float] = field(default_factory=dict)
    first: dict[str, list[float]] = field(default_factory=dict)
    later: dict[str, list[float]] = field(default_factory=dict)


def _stamp(value: str) -> dt.datetime:
    """Parse a GitHub log timestamp with 7-digit fractional seconds."""
    head, _, fraction = value.rstrip("Z").partition(".")
    return dt.datetime.fromisoformat(f"{head}.{fraction[:6].ljust(6, '0')}+00:00")


def parse_sample(logs: list[str]) -> Sample:
    """Fold one run's shard logs into per-test seconds and package overheads."""
    sample = Sample()
    for log in logs:
        previous: dt.datetime | None = None
        block: list[tuple[str, float]] = []
        first = True
        for line in log.splitlines():
            if previous is None:
                if match := START_RE.match(line):
                    previous = _stamp(match.group(1))
                continue
            if match := TEST_RE.match(line):
                block.append((match.group(2), float(match.group(3))))
            elif match := PACKAGE_RE.match(line):
                package = match.group(2)
                if len(block) != int(match.group(3)) or match.group(3) != match.group(4):
                    raise ValueError(f"{package}: {len(block)} test lines for "
                                     f"{match.group(3)}/{match.group(4)} PASS")
                now = _stamp(match.group(1))
                overhead = (now - previous).total_seconds() - sum(s for _, s in block)
                (sample.first if first else sample.later).setdefault(package, []).append(overhead)
                sample.tests.update({f"{package}|{name}": seconds for name, seconds in block})
                previous, block, first = now, [], False
        if previous is None:
            raise ValueError("log has no readiness proof step start")
        if block:
            raise ValueError("log ends with test lines but no package PASS line")
    return sample


def build_weights(samples: list[Sample], plan: list[str], source_runs: list[str],
                  fixed_job_seconds: float) -> dict[str, object]:
    """Fit the cost model on the samples, restricted to the current plan."""
    if not samples:
        raise ValueError("at least one sample is required")
    planned = set(plan)
    values: dict[str, list[float]] = {}
    for sample in samples:
        for run, seconds in sample.tests.items():
            if run in planned:
                values.setdefault(run, []).append(seconds)
    tests = {run: round(statistics.mean(seconds), 1) for run, seconds in sorted(values.items())}
    later: dict[str, list[float]] = {}
    first: dict[str, list[float]] = {}
    for sample in samples:
        for package, overheads in sample.later.items():
            later.setdefault(package, []).extend(overheads)
        for package, overheads in sample.first.items():
            first.setdefault(package, []).extend(overheads)
    packages = {package: round(max(statistics.mean(o), 0.0), 1) for package, o in later.items()}
    for package in first:
        packages.setdefault(package, 0.0)
    pairs = [statistics.mean(o) - packages[package] for package, o in first.items()
             if package in later]
    cold = statistics.mean(pairs) if pairs else statistics.mean(
        [value for o in first.values() for value in o])
    in_plan = {line.split("|")[0] for line in plan}
    costs = [cost for package, cost in packages.items() if package in in_plan]
    return {
        "schema_version": wt.SCHEMA_VERSION,
        "source_runs": source_runs,
        "fixed_job_seconds": round(fixed_job_seconds, 1),
        "cold_cache_seconds": round(max(cold, 0.0), 1),
        "default_test_seconds": round(statistics.mean(tests.values()), 1),
        "default_package_seconds": round(statistics.mean(costs), 1) if costs else 0.0,
        "package_seconds": dict(sorted(
            (package, cost) for package, cost in packages.items() if package in in_plan)),
        "test_seconds": tests,
    }


def render(document: dict[str, object]) -> str:
    """Render the weights with one entry per line so refresh diffs stay small."""
    lines = ["{"]
    scalars = [key for key in document if not isinstance(document[key], dict)]
    for key in scalars:
        lines.append(f"  {json.dumps(key)}: {json.dumps(document[key])},")
    tables = [key for key in document if isinstance(document[key], dict)]
    for index, key in enumerate(tables):
        table = document[key]
        lines.append(f"  {json.dumps(key)}: {{")
        entries = [f"    {json.dumps(name)}: {json.dumps(value)}"
                   for name, value in table.items()]  # type: ignore[union-attr]
        lines.append(",\n".join(entries))
        lines.append("  }" + ("," if index < len(tables) - 1 else ""))
    lines.append("}")
    return "\n".join(lines) + "\n"


def main() -> int:
    """Read the sample directories and write the weights file."""
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", required=True)
    parser.add_argument("--fixed-job-seconds", type=float, required=True)
    parser.add_argument("--sample", action="append", required=True, metavar="RUNID=DIR")
    args = parser.parse_args()
    from live_postgres_readiness_records import package_records  # noqa: E402
    plan = [f"{package}|{test}" for package, tests in package_records() for test in tests]
    runs: list[str] = []
    samples: list[Sample] = []
    try:
        for item in args.sample:
            run, _, directory = item.partition("=")
            logs = sorted(Path(directory).glob("*.log"))
            if not run.isdigit() or not logs:
                raise ValueError(f"--sample needs RUNID=DIR with *.log files: {item!r}")
            runs.append(run)
            samples.append(parse_sample([path.read_text(encoding="utf-8") for path in logs]))
        document = build_weights(samples, plan, runs, args.fixed_job_seconds)
        wt.weights_from_json(json.loads(render(document)))
    except (ValueError, OSError) as error:
        print(f"weights refresh: {error}", file=sys.stderr)
        return 1
    Path(args.out).write_text(render(document), encoding="utf-8")
    print(f"weights refresh: {len(document['test_seconds'])} tests from runs {runs}")  # type: ignore[arg-type]
    return 0


if __name__ == "__main__":
    sys.exit(main())
