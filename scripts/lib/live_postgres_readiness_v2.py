"""Strict five-shard weights and row-aware costs for readiness proofs.

The v1 model remains separate because its package price has different meaning.
All costs here are exact integer tenths of a second.
"""

from __future__ import annotations

from bisect import bisect_left
from dataclasses import dataclass
import json
from typing import Mapping

import live_postgres_readiness_row_split as row_split
import live_postgres_readiness_weights as wt

KEYS = {"schema_version", "source_runs", "shard_count", "fixed_job_seconds",
        "cold_cache_seconds", "default_test_seconds", "default_package_first_seconds",
        "default_package_repeat_seconds", "package_first_seconds",
        "package_repeat_seconds", "test_seconds"}
SHARD_COUNT = 5
ROW_BUDGET = row_split.ROW_BUDGET_TENTHS


@dataclass(frozen=True)
class WeightsV2:
    """Measured first-use and repeat-row costs with positive defaults."""

    source_runs: tuple[str, ...]
    fixed: int
    cold: int
    default_test: int
    default_package_first: int
    default_package_repeat: int
    package_first: Mapping[str, int]
    package_repeat: Mapping[str, int]
    test: Mapping[str, int]
    shard_count: int = SHARD_COUNT
    schema_version: int = 2

    def fingerprint(self) -> str:
        """Bind cached cuts and reports to the complete row-aware model."""
        return json.dumps([self.schema_version, self.shard_count, self.source_runs,
                           self.fixed, self.cold, self.default_test,
                           self.default_package_first, self.default_package_repeat,
                           sorted(self.package_first.items()),
                           sorted(self.package_repeat.items()), sorted(self.test.items()),
                           "midpoint-v2-cold-plus-max-first-repeat-every-row"],
                          separators=(",", ":"))


def weights_from_json(value: object) -> WeightsV2:
    """Reject an ambiguous or incomplete v2 weights document."""
    if not isinstance(value, dict) or set(value) != KEYS:
        raise ValueError("v2 weights schema mismatch")
    if type(value["schema_version"]) is not int or value["schema_version"] != 2:
        raise ValueError("unsupported weights schema_version")
    if type(value["shard_count"]) is not int or value["shard_count"] != SHARD_COUNT:
        raise ValueError("v2 shard_count must be 5")
    runs = value["source_runs"]
    if (not isinstance(runs, list) or len(runs) < 2
            or any(not isinstance(run, str) or not run.isascii() or not run.isdigit()
                   for run in runs) or len(set(runs)) != len(runs)):
        raise ValueError("v2 source_runs must contain distinct numeric run ids")
    defaults = {key: wt._tenths(value[key], key) for key in (
        "default_test_seconds", "default_package_first_seconds",
        "default_package_repeat_seconds")}
    if not all(defaults.values()):
        raise ValueError("v2 default prices must be positive")
    return WeightsV2(
        source_runs=tuple(runs),
        fixed=wt._tenths(value["fixed_job_seconds"], "fixed_job_seconds"),
        cold=wt._tenths(value["cold_cache_seconds"], "cold_cache_seconds"),
        default_test=defaults["default_test_seconds"],
        default_package_first=defaults["default_package_first_seconds"],
        default_package_repeat=defaults["default_package_repeat_seconds"],
        package_first=wt._table(value["package_first_seconds"],
                                "package_first_seconds", wt.PACKAGE_RE),
        package_repeat=wt._table(value["package_repeat_seconds"],
                                 "package_repeat_seconds", wt.PACKAGE_RE),
        test=wt._table(value["test_seconds"], "test_seconds", wt.RUN_RE))


def _row_package_price(model: WeightsV2, package: str) -> int:
    """Bound a row by either its first-use or its repeat package price."""
    return max(model.package_first.get(package, model.default_package_first),
               model.package_repeat.get(package, model.default_package_repeat))


def _row_weights(model: WeightsV2) -> wt.Weights:
    """Give the production midpoint splitter conservative admission prices."""
    packages = model.package_first.keys() | model.package_repeat.keys()
    return wt.Weights(
        model.source_runs, model.fixed, model.cold, model.default_test,
        max(model.default_package_first, model.default_package_repeat),
        {package: _row_package_price(model, package) for package in packages},
        model.test)


def row_cost_tenths(row: list[str], model: WeightsV2) -> int:
    """Return the conservative admission price used for every runner row."""
    return row_split.group_cost_tenths(row, _row_weights(model))


def split_rows(runs: list[str], model: WeightsV2) -> list[list[str]]:
    """Split contiguous package groups exactly as the production runner does."""
    groups: list[list[str]] = []
    for run in runs:
        package = run.split("|", 1)[0]
        if not groups or groups[-1][0].split("|", 1)[0] != package:
            groups.append([])
        groups[-1].append(run)
    return row_split.split_rows(groups, _row_weights(model))


class Scorer:
    """Score all contiguous intervals using prefix sums and exact row counts."""

    def __init__(self, runs: list[str], model: WeightsV2) -> None:
        self.runs = runs
        self.model = model
        self.package_names: list[str] = []
        self.package_at: list[int] = []
        self.package_starts = [0]
        self.test_prefix = [0]
        self.first_prefix = [0]
        seen_runs: set[str] = set()
        seen_packages: set[str] = set()
        for index, run in enumerate(runs):
            if run in seen_runs:
                raise ValueError(f"duplicate v2 planned run: {run}")
            seen_runs.add(run)
            package = run.split("|", 1)[0]
            if not self.package_names or package != self.package_names[-1]:
                if package in seen_packages:
                    raise ValueError(f"noncontiguous v2 package: {package}")
                seen_packages.add(package)
                if index:
                    self.package_starts.append(index)
                self.package_names.append(package)
                self.first_prefix.append(
                    self.first_prefix[-1] + model.package_first.get(
                        package, model.default_package_first))
            self.package_at.append(len(self.package_names) - 1)
            self.test_prefix.append(self.test_prefix[-1]
                                    + model.test.get(run, model.default_test))
        self.package_starts.append(len(runs))
        self._row_cache: dict[tuple[int, int], int] = {}
        self._cost_cache: dict[tuple[int, int], int] = {}
        self.repeat_prefix = [0]
        for index, package in enumerate(self.package_names):
            repeats = self.row_count(self.package_starts[index],
                                     self.package_starts[index + 1]) - 1
            self.repeat_prefix.append(self.repeat_prefix[-1] + repeats *
                                      model.package_repeat.get(
                                          package, model.default_package_repeat))

    def row_count(self, low: int, high: int) -> int:
        """Count midpoint rows without copying test slices or rescanning prices."""
        cached = self._row_cache.get((low, high))
        if cached is not None:
            return cached
        if not 0 <= low < high <= len(self.runs):
            raise ValueError("invalid row interval")
        if self.package_at[low] != self.package_at[high - 1]:
            raise ValueError("row interval spans packages")
        package = self.package_names[self.package_at[low]]
        admission = _row_package_price(self.model, package)
        pending = [(low, high)]
        count = 0
        while pending:
            start, end = pending.pop()
            total = self.test_prefix[end] - self.test_prefix[start]
            if end - start == 1 or self.model.cold + admission + total <= ROW_BUDGET:
                count += 1
                continue
            half = (total + 1) // 2
            cut = bisect_left(self.test_prefix, self.test_prefix[start] + half,
                              start + 1, end)
            if cut >= end:
                cut = start + (end - start) // 2
            pending.append((cut, end))
            pending.append((start, cut))
        self._row_cache[(low, high)] = count
        return count

    def cost(self, low: int, high: int) -> int:
        """Predict total job tenths for one nonempty contiguous shard."""
        cached = self._cost_cache.get((low, high))
        if cached is not None:
            return cached
        if not 0 <= low < high <= len(self.runs):
            raise ValueError("invalid shard interval")
        first_pkg, last_pkg = self.package_at[low], self.package_at[high - 1]
        total = (self.model.fixed + self.model.cold
                 + self.test_prefix[high] - self.test_prefix[low]
                 + self.first_prefix[last_pkg + 1] - self.first_prefix[first_pkg])
        if first_pkg == last_pkg:
            package = self.package_names[first_pkg]
            total += (self.row_count(low, high) - 1) * self.model.package_repeat.get(
                package, self.model.default_package_repeat)
        else:
            first_name = self.package_names[first_pkg]
            last_name = self.package_names[last_pkg]
            total += (self.row_count(low, self.package_starts[first_pkg + 1]) - 1) * (
                self.model.package_repeat.get(first_name, self.model.default_package_repeat))
            total += self.repeat_prefix[last_pkg] - self.repeat_prefix[first_pkg + 1]
            total += (self.row_count(self.package_starts[last_pkg], high) - 1) * (
                self.model.package_repeat.get(last_name, self.model.default_package_repeat))
        self._cost_cache[(low, high)] = total
        return total


def segment_cost_tenths(runs: list[str], model: WeightsV2) -> int:
    """Return the exact row-aware job cost of a nonempty shard."""
    return Scorer(runs, model).cost(0, len(runs))


def defaulted_packages(runs: list[str], model: WeightsV2,
                       rows_by_shard: list[list[list[str]]]) -> tuple[str, ...]:
    """Count missing first prices and repeat prices needed within a job."""
    packages = {run.split("|", 1)[0] for run in runs}
    missing = packages - set(model.package_first)
    for rows in rows_by_shard:
        row_counts: dict[str, int] = {}
        for row in rows:
            package = row[0].split("|", 1)[0]
            row_counts[package] = row_counts.get(package, 0) + 1
        missing.update(package for package, count in row_counts.items()
                       if count > 1 and package not in model.package_repeat)
    return tuple(sorted(missing))
