"""Cross-inventory and cache-era pool rules for the v2 weights refresher (#9566).

A refit pool may hold runs of different test inventories when every run is a
green main push with one runner profile and one cache era (arbiter ruling for
#9473 on the warm Go build cache, item 3). Each test is priced from the runs
that contain it: at the pool's rule when it has at least QUANTILE_MIN_POOL
samples, otherwise it is left out of `test_seconds`, so the planner prices it
at `default_test_seconds` and the drift gate counts it. Row residuals, cold and
fixed seconds come from every run. The output inventory is the newest run's.

The cache era is read per shard job (arbiter ruling for #9566 on the cache
era, 2026-10-30). With no "Restore Go build cache" step the shard is cold. The
step must have succeeded, and actions/cache/restore succeeds on a miss too, so
the shard's log decides: "Cache restored from key:" in the restore step's
section is warm (#9462), "Cache not found for input keys" is cold, because a
miss compiles what a cold run compiles. A run whose shards disagree is refused,
and so is a pool that mixes eras: cold and warm residuals measure different
things.
"""

from __future__ import annotations

from collections import Counter
from dataclasses import dataclass, field
import json
from pathlib import Path
from typing import Callable, Mapping

import live_postgres_readiness_refresh_v2_provenance as provenance

RESTORE_STEP = "Restore Go build cache"
RESTORE_GROUP = "##[group]Run actions/cache/restore@"
STEP_GROUP = "##[group]Run "
CACHE_HIT = "Cache restored from key:"
CACHE_MISS = "Cache not found for input keys"


def restore_outcome(log: str) -> str | None:
    """Return "hit", "miss" or None from the restore step's section of a shard log.

    Only the lines between the actions/cache/restore group and the next step
    are read, because setup-go prints its own cache lines later in the job.
    """
    section: list[str] = []
    inside = False
    for line in log.splitlines():
        if RESTORE_GROUP in line:
            inside = True
        elif inside and STEP_GROUP in line:
            break
        elif inside:
            section.append(line)
    hit = any(CACHE_HIT in line for line in section)
    miss = any(CACHE_MISS in line for line in section)
    if hit == miss:
        return None
    return "hit" if hit else "miss"


def cache_era(directory: Path, run_id: str,
              logs: Mapping[int, str]) -> tuple[str, Counter[str]]:
    """Return the era of one source run already validated by read_source.

    Args:
        directory: The run directory holding jobs.json.
        run_id: The run ID, for error messages.
        logs: The run's shard logs keyed by shard number.

    Returns:
        "cold" or "warm", and the count of shard jobs by restore outcome
        ("hit", "miss", "no step").

    Raises:
        ValueError: A shard's restore step did not succeed, its log shows
            neither a hit nor a miss, or the run's shards differ in era.
    """
    payload = json.loads((directory / "jobs.json").read_text(encoding="utf-8"))
    eras: dict[int, str] = {}
    outcomes: Counter[str] = Counter()
    for job in payload["jobs"]:
        name = job.get("name")
        match = provenance.SHARD_RE.fullmatch(name) if isinstance(name, str) else None
        if match is None:
            continue
        steps = job.get("steps") if isinstance(job.get("steps"), list) else []
        restore = [step for step in steps if isinstance(step, dict)
                   and step.get("name") == RESTORE_STEP]
        number = int(match.group(1))
        if not restore:
            eras[number] = "cold"
            outcomes["no step"] += 1
            continue
        state = restore[0]
        if len(restore) != 1 or state.get("status") != "completed" or state.get("conclusion") != "success":
            raise ValueError(f"source {run_id} shard {number} cache restore step did not "
                             f"succeed ({state.get('conclusion')}); its cache era is unknown")
        outcome = restore_outcome(logs[number])
        if outcome is None:
            raise ValueError(f"source {run_id} shard {number} cache restore log shows "
                             "neither a hit nor a miss; its cache era is unknown")
        eras[number] = "warm" if outcome == "hit" else "cold"
        outcomes[outcome] += 1
    if len(set(eras.values())) != 1:
        shards = ", ".join(f"{number}={era}" for number, era in sorted(eras.items()))
        raise ValueError(f"source {run_id} cache era differs across shards: {shards}")
    return next(iter(eras.values())), outcomes


@dataclass
class PoolProvenance:
    """Inventories, cache era and defaulted tests of one refit pool.

    Attributes:
        era: "cold" or "warm", shared by every run.
        plans: Each run's own-SHA inventory, keyed by run ID in fit order.
        defaulted: Tests of the newest inventory left to default_test_seconds,
            with the number of samples each had.
        min_samples: The sample count a test needed in a mixed pool.
    """

    era: str = ""
    plans: dict[str, list[str]] = field(default_factory=dict)
    defaulted: dict[str, int] = field(default_factory=dict)
    min_samples: int = 0
    outcomes: Counter[str] = field(default_factory=Counter)

    def add(self, run_id: str, plan: list[str], era: str,
            outcomes: Counter[str] | None = None) -> None:
        """Record one run and its shard restore outcomes, refusing a second cache era."""
        if self.era and era != self.era:
            first = next(iter(self.plans))
            raise ValueError(f"source runs mix cache eras: {first} is {self.era} and "
                             f"{run_id} is {era}; a pool holds one era")
        self.era = era
        self.plans[run_id] = plan
        self.outcomes.update(outcomes or {})

    @property
    def newest(self) -> str:
        """The run with the highest run ID; its inventory is the output's."""
        return max(self.plans, key=int)

    @property
    def mixed(self) -> bool:
        """Whether the pool's runs have more than one inventory."""
        return len({tuple(plan) for plan in self.plans.values()}) > 1

    def price_tests(self, values: dict[str, list[float]], price: Callable[[list[float], str], float],
                    min_samples: int) -> dict[str, float]:
        """Price the newest inventory's tests from the runs that contain them.

        Args:
            values: Per-test elapsed samples gathered from every run.
            price: Prices one test's samples under the pool's rule.
            min_samples: Samples a test needs in a mixed pool; also the
                smallest mixed pool accepted.

        Raises:
            ValueError: A mixed pool is smaller than min_samples runs, or the
                priced set does not match the newest inventory.
        """
        newest = self.plans[self.newest]
        self.min_samples = min_samples
        if not self.mixed:
            tests = {run: price(samples, run) for run, samples in sorted(values.items())}
            if set(tests) != set(newest):
                raise ValueError("source test prices differ from canonical inventory")
            return tests
        if len(self.plans) < min_samples:
            raise ValueError(f"source inventories differ in a {len(self.plans)}-run pool; "
                             f"mixing inventories needs at least {min_samples} runs")
        tests: dict[str, float] = {}
        for run in sorted(newest):
            samples = values.get(run, [])
            if len(samples) >= min_samples:
                tests[run] = price(samples, run)
            else:
                self.defaulted[run] = len(samples)
        return tests

    def era_line(self) -> str:
        """Render the cache-era stdout line."""
        hit, miss, absent = (self.outcomes[key] for key in ("hit", "miss", "no step"))
        return (f"weights refresh v2: cache era {self.era} (\"{RESTORE_STEP}\" in {hit + miss} "
                f"shard jobs: {hit} hit, {miss} missed; {absent} shard jobs without the step)")

    def inventory_line(self) -> str:
        """Render the mixed-inventory stdout line, naming every defaulted test."""
        groups: dict[int, list[str]] = {}
        for run, plan in self.plans.items():
            groups.setdefault(len(plan), []).append(run)
        inventories = ", ".join(f"{size} tests (runs {', '.join(runs)})"
                                for size, runs in sorted(groups.items()))
        noun = "test" if len(self.defaulted) == 1 else "tests"
        named = ", ".join(f"{run} ({count} sample{'' if count == 1 else 's'})"
                          for run, count in sorted(self.defaulted.items()))
        return (f"weights refresh v2: inventories {inventories}; output inventory "
                f"{len(self.plans[self.newest])} tests from run {self.newest}; "
                f"{len(self.defaulted)} {noun} priced at default_test_seconds "
                f"(fewer than {self.min_samples} samples): {named or 'none'}")
