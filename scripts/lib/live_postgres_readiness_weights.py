#!/usr/bin/env python3
"""Time-weighted contiguous cuts for the live-postgres-readiness shards (#8930).

The flat ``package|test`` plan is cut into contiguous shards by predicted
seconds, not by test count. The prediction is a small model fitted on measured
CI runs and committed as ``specs/live-postgres-readiness-weights.v1.json``:

* per-test elapsed seconds (a documented default for tests with no weight),
* a per-package compile and link cost paid each time a package appears in a
  shard (the giant storage package pays several minutes per appearance),
* a cold-build-cache constant paid by the first package of every shard,
* the fixed per-job overhead (containers, checkout, toolchain setup).

Planner, shard report, and aggregate verification all call
``weighted_bounds`` with the same selected weights file, so every proof
lands in exactly one shard and a weights change cannot silently move a proof
out of the union.
A present v2 file switches to a separate row-aware five-shard model; the
v1 path and its four-shard behavior remain unchanged when v2 is absent.
All arithmetic uses integer tenths of a second. Two gates keep the file
honest: ``budget_errors`` fails when a shard's predicted job seconds pass
80% of the job cap, and ``drift_errors``
fails when too many plan entries have no measured weight.
"""

from __future__ import annotations

from dataclasses import dataclass, replace
import json
import math
from pathlib import Path
import re
from typing import Mapping, TYPE_CHECKING

if TYPE_CHECKING:
    from live_postgres_readiness_v2 import WeightsV2

WEIGHTS_PATH = (Path(__file__).resolve().parents[2] / "specs"
                 / "live-postgres-readiness-weights.v1.json")
JOB_CAP_SECONDS = 1800  # timeout-minutes: 30 on the shard job
BUDGET_PERCENT = 80
BUDGET_SECONDS = JOB_CAP_SECONDS * BUDGET_PERCENT // 100
MAX_DEFAULTED_TESTS = 40
MAX_DEFAULTED_PACKAGES = 2
SHARD_COUNT = 4
UNIT = 10  # weights are exact tenths of a second
SCHEMA_VERSION = 1
MAX_SECONDS = 100000
RUN_RE = re.compile(r"^\./[A-Za-z0-9_./-]+\|Test[A-Za-z0-9_]+$")
PACKAGE_RE = re.compile(r"^\./[A-Za-z0-9_./-]+$")
KEYS = {"schema_version", "source_runs", "fixed_job_seconds", "cold_cache_seconds",
        "default_test_seconds", "default_package_seconds", "package_seconds",
        "test_seconds"}
_BOUNDS_CACHE: dict[tuple[str, tuple[str, ...], int], list[int]] = {}


@dataclass(frozen=True)
class Weights:
    """The fitted cost model, in integer tenths of a second."""

    source_runs: tuple[str, ...]
    fixed: int
    cold: int
    default_test: int
    default_package: int
    package: Mapping[str, int]
    test: Mapping[str, int]

    def with_test_seconds(self, seconds: Mapping[str, float]) -> "Weights":
        """Return a copy with these runs priced at the given seconds."""
        merged = dict(self.test)
        merged.update({run: _tenths(value, run) for run, value in seconds.items()})
        return replace(self, test=merged)

    def fingerprint(self) -> str:
        """Identify the weights content for the cut cache."""
        return json.dumps([self.fixed, self.cold, self.default_test, self.default_package,
                           sorted(self.package.items()), sorted(self.test.items())])


@dataclass(frozen=True)
class Drift:
    """Plan entries the weights do not cover, and weights the plan dropped."""

    defaulted_tests: tuple[str, ...]
    defaulted_packages: tuple[str, ...]
    stale_tests: tuple[str, ...]
    schema_version: int = 1


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """Reject duplicate JSON fields instead of keeping the last value."""
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON field: {key}")
        result[key] = value
    return result


def _reject_constant(value: str) -> object:
    """Reject NaN and infinity, which are outside JSON's number grammar."""
    raise ValueError(f"invalid JSON constant: {value}")


def _tenths(value: object, name: str) -> int:
    """Convert a seconds value to exact tenths or fail closed."""
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"weight {name!r} is not a number")
    if not math.isfinite(value) or value < 0 or value > MAX_SECONDS:
        raise ValueError(f"weight {name!r} is out of range: {value!r}")
    scaled = value * UNIT
    if abs(scaled - round(scaled)) > 1e-6:
        raise ValueError(f"weight {name!r} is not a whole number of tenths: {value!r}")
    return round(scaled)


def _table(value: object, name: str, key_re: re.Pattern[str]) -> dict[str, int]:
    """Validate a name-to-seconds table."""
    if not isinstance(value, dict):
        raise ValueError(f"{name} must be an object")
    for key in value:
        if not isinstance(key, str) or not key_re.fullmatch(key):
            raise ValueError(f"{name} has an invalid key: {key!r}")
    return {key: _tenths(seconds, f"{name}[{key}]") for key, seconds in value.items()}


def weights_from_json(value: object) -> Weights:
    """Validate a parsed weights document and convert it to ``Weights``."""
    if not isinstance(value, dict) or set(value) != KEYS:
        raise ValueError("weights schema mismatch")
    if type(value["schema_version"]) is not int or value["schema_version"] != SCHEMA_VERSION:
        raise ValueError("unsupported weights schema_version")
    runs = value["source_runs"]
    if (not isinstance(runs, list) or not runs
            or any(not isinstance(run, str) or not re.fullmatch(r"\d+", run) for run in runs)):
        raise ValueError("source_runs must list numeric run ids")
    return Weights(
        source_runs=tuple(runs),
        fixed=_tenths(value["fixed_job_seconds"], "fixed_job_seconds"),
        cold=_tenths(value["cold_cache_seconds"], "cold_cache_seconds"),
        default_test=_tenths(value["default_test_seconds"], "default_test_seconds"),
        default_package=_tenths(value["default_package_seconds"], "default_package_seconds"),
        package=_table(value["package_seconds"], "package_seconds", PACKAGE_RE),
        test=_table(value["test_seconds"], "test_seconds", RUN_RE))


def load_weights(path: Path | None = None) -> Weights | WeightsV2:
    """Prefer a present v2 sibling, failing closed on malformed v2 content."""
    target = path or WEIGHTS_PATH
    if target.name.endswith(".v1.json"):
        candidate = target.with_name(target.name[:-8] + ".v2.json")
        if candidate.exists():
            target = candidate
    try:
        text = target.read_text(encoding="utf-8")
        parsed = json.loads(text, object_pairs_hook=_unique_object,
                            parse_constant=_reject_constant)
    except OSError as error:
        raise ValueError(f"weights unavailable: {error}")
    except json.JSONDecodeError as error:
        raise ValueError(f"weights are not valid JSON: {error}")
    version = parsed.get("schema_version") if isinstance(parsed, dict) else None
    if target.name.endswith(".v2.json") and version != 2:
        raise ValueError("present v2 weights have unsupported schema_version")
    if version == 2:
        import live_postgres_readiness_v2 as v2
        return v2.weights_from_json(parsed)
    return weights_from_json(parsed)


def _costs(runs: list[str], weights: Weights) -> tuple[list[int], list[int], list[bool]]:
    """Return prefix test seconds, prefix package-start seconds, and start flags."""
    tests = [0]
    starts = [0]
    is_start: list[bool] = []
    previous = ""
    for line in runs:
        package = line.split("|")[0]
        begins = package != previous
        is_start.append(begins)
        tests.append(tests[-1] + weights.test.get(line, weights.default_test))
        paid = weights.package.get(package, weights.default_package) if begins else 0
        starts.append(starts[-1] + paid)
        previous = package
    return tests, starts, is_start


def _segment_cost(runs: list[str], weights: Weights,
                  prefix: tuple[list[int], list[int], list[bool]], low: int, high: int) -> int:
    """Predicted tenths of a second to run ``runs[low:high]`` as one shard step."""
    tests, starts, is_start = prefix
    cost = tests[high] - tests[low] + starts[high] - starts[low] + weights.cold
    if not is_start[low]:
        package = runs[low].split("|")[0]
        cost += weights.package.get(package, weights.default_package)
    return cost


def weighted_bounds(runs: list[str], weights: Weights | WeightsV2,
                    shard_count: int | None = None) -> list[int]:
    """Cut the plan into ``shard_count`` contiguous shards by predicted seconds.

    V1 minimizes the largest shard, then squared costs at that limit.
    V2 maximizes the smallest guarded job, minimizes the largest, then
    minimizes squared costs. Returns ``shard_count + 1`` boundaries.

    Raises:
        ValueError: There are fewer runs than shards.
    """
    import live_postgres_readiness_v2 as v2
    if isinstance(weights, v2.WeightsV2):
        from live_postgres_readiness_v2_planner import exact_bounds
        if shard_count is None:
            shard_count = weights.shard_count
        if shard_count != weights.shard_count:
            raise ValueError("v2 planner requires five shards")
        key = (weights.fingerprint(), tuple(runs), shard_count)
        cached = _BOUNDS_CACHE.get(key)
        if cached is not None:
            return list(cached)
        scorer = v2.Scorer(runs, weights)
        bounds = exact_bounds(len(runs), shard_count, BUDGET_SECONDS * UNIT,
                              scorer.cost)
        jobs = [scorer.cost(low, high) for low, high in zip(bounds, bounds[1:])]
        mean_sum = sum(jobs)
        if mean_sum == 0 or max(abs(job * shard_count - mean_sum)
                                for job in jobs) * 100 > 15 * mean_sum:
            raise ValueError("v2 predicted job spread exceeds 15% of mean")
        for low, high in zip(bounds, bounds[1:]):
            rows = v2.split_rows(runs[low:high], weights)
            if [run for row in rows for run in row] != runs[low:high]:
                raise ValueError("v2 row union differs from shard plan")
            if any(v2.row_cost_tenths(row, weights) > v2.ROW_BUDGET
                   for row in rows):
                raise ValueError("v2 runner row exceeds 720 s budget")
        if len(_BOUNDS_CACHE) > 64:
            _BOUNDS_CACHE.clear()
        _BOUNDS_CACHE[key] = list(bounds)
        return bounds
    if shard_count is None:
        shard_count = SHARD_COUNT
    count = len(runs)
    if count < shard_count:
        raise ValueError(f"plan has {count} runs, fewer than {shard_count} shards")
    key = (weights.fingerprint(), tuple(runs), shard_count)
    cached = _BOUNDS_CACHE.get(key)
    if cached is not None:
        return list(cached)
    prefix = _costs(runs, weights)

    def cost(low: int, high: int) -> int:
        return _segment_cost(runs, weights, prefix, low, high)

    infinity = 1 << 60
    best = [[infinity] * (count + 1) for _ in range(shard_count + 1)]
    best[0][0] = 0
    for shard in range(1, shard_count + 1):
        for high in range(shard, count - (shard_count - shard) + 1):
            for low in range(high - 1, shard - 2, -1):
                segment = cost(low, high)
                if segment >= best[shard][high]:
                    break  # a longer segment only costs more
                candidate = max(best[shard - 1][low], segment)
                if candidate < best[shard][high]:
                    best[shard][high] = candidate
    limit = best[shard_count][count]
    spread = [[infinity] * (count + 1) for _ in range(shard_count + 1)]
    choice = [[0] * (count + 1) for _ in range(shard_count + 1)]
    spread[0][0] = 0
    for shard in range(1, shard_count + 1):
        for high in range(shard, count - (shard_count - shard) + 1):
            for low in range(high - 1, shard - 2, -1):
                segment = cost(low, high)
                if segment > limit:
                    break
                if spread[shard - 1][low] >= infinity:
                    continue
                candidate = spread[shard - 1][low] + segment * segment
                if candidate < spread[shard][high]:
                    spread[shard][high] = candidate
                    choice[shard][high] = low
    bounds = [count]
    for shard in range(shard_count, 0, -1):
        bounds.append(choice[shard][bounds[-1]])
    bounds.reverse()
    if bounds[0] != 0 or any(low >= high for low, high in zip(bounds, bounds[1:])):
        raise ValueError("weighted cut produced an empty shard")
    if len(_BOUNDS_CACHE) > 64:
        _BOUNDS_CACHE.clear()
    _BOUNDS_CACHE[key] = list(bounds)
    return bounds


def shard_job_seconds(runs: list[str], weights: Weights | WeightsV2,
                      bounds: list[int]) -> list[float]:
    """Predicted wall seconds of each shard job, fixed overhead included."""
    import live_postgres_readiness_v2 as v2
    if isinstance(weights, v2.WeightsV2):
        scorer = v2.Scorer(runs, weights)
        return [scorer.cost(low, high) / UNIT for low, high in zip(bounds, bounds[1:])]
    prefix = _costs(runs, weights)
    return [(_segment_cost(runs, weights, prefix, low, high) + weights.fixed) / UNIT
            for low, high in zip(bounds, bounds[1:])]


def budget_errors(runs: list[str], weights: Weights | WeightsV2) -> list[str]:
    """List shards whose predicted job seconds pass the budget share of the cap."""
    import live_postgres_readiness_v2 as v2
    if isinstance(weights, v2.WeightsV2):
        try:
            weighted_bounds(runs, weights, weights.shard_count)
        except ValueError as error:
            return [str(error)]
        return []
    jobs = shard_job_seconds(runs, weights, weighted_bounds(runs, weights))
    return [f"shard {index}/{SHARD_COUNT} predicted {seconds:.0f} s exceeds "
            f"{BUDGET_PERCENT}% of the {JOB_CAP_SECONDS} s cap ({BUDGET_SECONDS} s); "
            "refresh specs/live-postgres-readiness-weights.v1.json or rebalance"
            for index, seconds in enumerate(jobs, start=1) if seconds > BUDGET_SECONDS]


def drift_report(runs: list[str], weights: Weights | WeightsV2) -> Drift:
    """Compare the plan with the measured weights."""
    import live_postgres_readiness_v2 as v2
    if isinstance(weights, v2.WeightsV2):
        bounds = weighted_bounds(runs, weights, weights.shard_count)
        rows_by_shard = [v2.split_rows(runs[low:high], weights)
                         for low, high in zip(bounds, bounds[1:])]
        return Drift(
            defaulted_tests=tuple(line for line in runs if line not in weights.test),
            defaulted_packages=v2.defaulted_packages(runs, weights, rows_by_shard),
            stale_tests=tuple(sorted(set(weights.test) - set(runs))),
            schema_version=2)
    planned = set(runs)
    packages = {line.split("|")[0] for line in runs}
    return Drift(
        defaulted_tests=tuple(line for line in runs if line not in weights.test),
        defaulted_packages=tuple(sorted(packages - set(weights.package))),
        stale_tests=tuple(sorted(set(weights.test) - planned)))


def drift_errors(drift: Drift) -> list[str]:
    """Fail when too many plan entries fall into the documented defaults."""
    errors = []
    version = "v2" if drift.schema_version == 2 else "v1"
    if len(drift.defaulted_tests) > MAX_DEFAULTED_TESTS:
        errors.append(f"{len(drift.defaulted_tests)} planned tests have no weight "
                      f"(limit {MAX_DEFAULTED_TESTS}): new tests run on "
                      f"default_test_seconds, so feature PRs must not edit "
                      f"specs/live-postgres-readiness-weights.{version}.json; a standalone "
                      f"refit PR refreshes it "
                      f"(docs/public/reference/local-testing/"
                      f"live-postgres-readiness-shards.md)")
    if len(drift.defaulted_packages) > MAX_DEFAULTED_PACKAGES:
        errors.append(f"{len(drift.defaulted_packages)} planned packages have no cost "
                      f"(limit {MAX_DEFAULTED_PACKAGES}): {list(drift.defaulted_packages)}")
    return errors
