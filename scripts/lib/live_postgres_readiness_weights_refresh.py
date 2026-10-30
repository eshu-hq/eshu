#!/usr/bin/env python3
"""Refresh the readiness shard weights from green CI shard logs (#8930).

Input is the raw log of each shard job (``gh api repos/OWNER/REPO/actions/jobs/ID/logs``)
from one or more full green runs. A run is one *sample*. The refresher reads
three line shapes the runner already prints:

* ``##[group]Run bash scripts/run-live-postgres-readiness-tests.sh --shard ...``
  starts the proof step,
* ``<Test>: PASS elapsed=<s>s`` carries one test's seconds,
* ``live-postgres-readiness: <package> <n>/<n> PASS`` closes a package.

In the legacy v1 model, a package's wall time is the gap between closing
lines (the step start for the first package). Its residual after reported test
seconds is averaged as one package price. V2 separates first use from repeats.

Usage:
    live_postgres_readiness_weights_refresh.py \\
        --out specs/live-postgres-readiness-weights.v1.json --fixed-job-seconds 82 \\
        --sample 40049588589=DIR_A --sample 40047410741=DIR_B

Each v1 DIR holds shard ``*.log`` files, and v1 keeps only tests in the
current inventory. V2 requires each numeric-ID directory to hold ``run.json``,
``jobs.json``, and every ``shardN.log``. It validates every PASS against the
source SHA's committed inventory:

    live_postgres_readiness_weights_refresh.py --schema-version 2 \
        --out /tmp/weights.v2.json \
        --sample 40363406628=/data/40363406628 \
        --sample 40363532093=/data/40363532093

V2 gates on one runner image name, Go version, and Postgres digest, and prints
one provenance line naming the runner image version seen on each shard plus
one cold-provenance line per paired package behind the fitted cold constant.
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
    r"^\ufeff?(\S+Z) ##\[group\]Run bash scripts/run-live-postgres-readiness-tests\.sh --shard")
TEST_RE = re.compile(r"^\ufeff?(\S+Z) (Test[A-Za-z0-9_]+): PASS elapsed=([0-9.]+)s")
PACKAGE_RE = re.compile(
    r"^\ufeff?(\S+Z) live-postgres-readiness: (\./\S+) (\d+)/(\d+) PASS")


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


def _render_v2(document: dict[str, object]) -> str:
    """Keep the measured v2 tables readable within the file line budget."""
    lines = ["{"]
    keys = sorted(document)
    for key_index, key in enumerate(keys):
        value = document[key]
        suffix = "," if key_index < len(keys) - 1 else ""
        if isinstance(value, dict):
            lines.append(f"  {json.dumps(key)}: {{")
            entries = [f"{json.dumps(name)}: {json.dumps(price)}"
                       for name, price in sorted(value.items())]
            for offset in range(0, len(entries), 2):
                pair = ", ".join(entries[offset:offset + 2])
                lines.append("    " + pair
                             + ("," if offset + 2 < len(entries) else ""))
            lines.append("  }" + suffix)
        else:
            lines.append(f"  {json.dumps(key)}: {json.dumps(value)}" + suffix)
    lines.append("}")
    return "\n".join(lines) + "\n"


def render(document: dict[str, object]) -> str:
    """Render legacy v1 bytes or compact, canonical v2 table entries."""
    if document.get("schema_version") == 2:
        return _render_v2(document)
    lines = ["{"]
    scalars = [key for key in document if not isinstance(document[key], dict)]
    for key in scalars:
        lines.append(f"  {json.dumps(key)}: {json.dumps(document[key])},")
    tables = [key for key in document if isinstance(document[key], dict)]
    for index, key in enumerate(tables):
        table = document[key]
        lines.append(f"  {json.dumps(key)}: {{")
        entries = [f"{json.dumps(name)}: {json.dumps(value)}"
                   for name, value in sorted(table.items())]  # type: ignore[union-attr,attr-defined]
        for position, entry in enumerate(entries):
            lines.append("    " + entry
                         + ("," if position < len(entries) - 1 else ""))
        lines.append("  }" + ("," if index < len(tables) - 1 else ""))
    lines.append("}")
    return "\n".join(lines) + "\n"


def main() -> int:
    """Read the sample directories and write the weights file."""
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", required=True)
    parser.add_argument("--schema-version", type=int, choices=(1, 2), default=1)
    parser.add_argument("--fixed-job-seconds", type=float)
    parser.add_argument("--sample", action="append", required=True, metavar="RUNID=DIR")
    args = parser.parse_args()
    if args.schema_version == 2:
        import live_postgres_readiness_refresh_v2 as v2_refresh  # noqa: E402
        if args.fixed_job_seconds is not None:
            parser.error("--fixed-job-seconds is inferred from source jobs for v2")
        try:
            sources = []
            for item in args.sample:
                run, separator, directory = item.partition("=")
                if (not separator or not run.isascii() or not run.isdigit()
                        or Path(directory).name != run):
                    raise ValueError(f"v2 --sample needs RUNID=DIR named by run ID: {item!r}")
                sources.append(Path(directory))
            document, images, cold_components, pool = v2_refresh.fit_with_provenance(
                sources, Path(__file__).resolve().parents[2])
            output = render(document)
            Path(args.out).write_text(output, encoding="utf-8")
        except (ValueError, OSError) as error:
            print(f"weights refresh v2: {error}", file=sys.stderr)
            return 1
        print(f"weights refresh v2: {len(document['test_seconds'])} priced tests "
              f"from runs {document['source_runs']}")
        print(v2_refresh.pricing_rule(document["source_runs"],  # type: ignore[arg-type]
                                      images.shard_counts).line())
        print(images.line())
        print(pool.era_line())
        if pool.mixed:
            print(pool.inventory_line())
        for component in cold_components:
            print(component.line())
        return 0
    if args.fixed_job_seconds is None:
        parser.error("--fixed-job-seconds is required for v1")
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
    tests: dict[str, float] = document["test_seconds"]  # type: ignore[assignment]
    print(f"weights refresh: {len(tests)} tests from runs {runs}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
