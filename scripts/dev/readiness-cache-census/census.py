#!/usr/bin/env python3
"""Per-shard Go build cache hit/miss census over fetched readiness runs (#9598).

The #9598 acceptance needs the restore hit rate over at least 10 main push
runs after the fix. This script is the instrument: it reads one fetched run
directory per run (the layout `fetch.sh` in
scripts/dev/readiness-refit-evidence writes: run.json, jobs.json, one
shardN.log per shard job) and reports every shard's restore outcome from its
own log, using the same rule the weights refresher enforces
(live_postgres_readiness_refresh_v2_pool.restore_outcome): "Cache restored
from key:" in the restore step's section is a hit, "Cache not found for
input keys" is a miss. Anything else fails closed with exit 2, like the
refresher's unknown-era refusal; a census is a measurement, so a readable
table always exits 0 and the acceptance decision stays with the reader.

Usage, from the repository root:

    scripts/dev/readiness-refit-evidence/fetch.sh /tmp/census RUN_ID...
    python3 -I scripts/dev/readiness-cache-census/census.py \\
        --lib scripts/lib RUN_ID=/tmp/census/RUN_ID ...
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from pathlib import Path


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lib", required=True,
                        help="scripts/lib of the tree whose pool module "
                        "defines the restore rule")
    parser.add_argument("samples", nargs="+", metavar="RUN_ID=RUN_DIR",
                        help="one fetched run directory per run ID")
    return parser.parse_args(argv)


def split_sample(sample: str) -> tuple[str, Path]:
    run_id, _, directory = sample.partition("=")
    if not run_id or not directory:
        raise ValueError(f"sample must read RUN_ID=RUN_DIR, got {sample!r}")
    return run_id, Path(directory)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    sys.path.insert(0, str(Path(args.lib).resolve()))
    import live_postgres_readiness_refresh_v2_pool as pool
    import live_postgres_readiness_refresh_v2_provenance as provenance

    totals: Counter[str] = Counter()
    shards_hit = 0
    shards_total = 0
    for sample in args.samples:
        try:
            run_id, directory = split_sample(sample)
            payload = json.loads((directory / "jobs.json").read_text(encoding="utf-8"))
            jobs = payload.get("jobs", [])
            if not isinstance(jobs, list):
                raise ValueError("jobs.json holds no job list")
        except (ValueError, OSError, AttributeError) as exc:
            print(f"census: cannot read sample {sample!r}: {exc}", file=sys.stderr)
            return 2
        outcomes: dict[int, str] = {}
        for job in jobs:
            if not isinstance(job, dict):
                print(f"census: run {run_id} holds a job entry that is not "
                      f"an object; refusing to guess", file=sys.stderr)
                return 2
            name = job.get("name")
            if name is None:
                continue
            if not isinstance(name, str):
                print(f"census: run {run_id} holds a job with a non-string "
                      f"name; refusing to guess", file=sys.stderr)
                return 2
            match = provenance.SHARD_RE.fullmatch(name)
            if match is None:
                continue
            number = int(match.group(1))
            if number in outcomes:
                print(f"census: run {run_id} shard {number} is listed "
                      f"twice; refusing to guess", file=sys.stderr)
                return 2
            try:
                log = (directory / f"shard{number}.log").read_text(encoding="utf-8")
            except OSError as exc:
                print(f"census: run {run_id} shard {number}: {exc}", file=sys.stderr)
                return 2
            outcome = pool.restore_outcome(log)
            if outcome is None:
                print(f"census: run {run_id} shard {number} restore log shows "
                      f"neither a hit nor a miss; refusing to guess", file=sys.stderr)
                return 2
            outcomes[number] = outcome
        if not outcomes:
            print(f"census: run {run_id} has no shard jobs in jobs.json", file=sys.stderr)
            return 2
        hits = sum(1 for outcome in outcomes.values() if outcome == "hit")
        total = len(outcomes)
        misses = sorted(n for n, outcome in outcomes.items() if outcome == "miss")
        shards_hit += hits
        shards_total += total
        if hits == total:
            verdict: str = "all-hit"
        elif hits == 0:
            verdict = "all-miss"
        else:
            verdict = f"mixed (miss: {','.join(map(str, misses))})"
        totals[verdict.split(" ")[0]] += 1
        print(f"run {run_id}: {hits}/{total} hit => {verdict}")
    rate = f"{shards_hit}/{shards_total} hit"
    print(f"census: {len(args.samples)} runs, {totals['all-hit']} all-hit, "
          f"{totals['mixed']} mixed, {totals['all-miss']} all-miss; "
          f"shards {rate}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
