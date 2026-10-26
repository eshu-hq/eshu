"""Fit row-aware readiness weights from complete, compatible main CI runs."""

from __future__ import annotations

from collections import Counter, defaultdict
import datetime as dt
from pathlib import Path
import re
import statistics

import live_postgres_readiness_refresh_v2_provenance as provenance
import live_postgres_readiness_v2 as v2
import live_postgres_readiness_weights as weights

START_RE = re.compile(r'^\ufeff?(\S+Z) ##\[group\]Run bash scripts/run-live-postgres-readiness-tests\.sh --shard "(\d+)/(\d+)" --report readiness-shard-report\.json$')
TEST_RE = re.compile(r"^\ufeff?(\S+Z) (Test[A-Za-z0-9_]+): PASS elapsed=([0-9.]+)s$")
PACKAGE_RE = re.compile(r"^\ufeff?(\S+Z) live-postgres-readiness: (\./\S+) (\d+)/(\d+) PASS$")
SUMMARY_RE = re.compile(r"^\ufeff?\S+Z readiness shard (\d+)/(\d+): (\d+)/(\d+) passing runs, ok=True$")
IMAGE_VERSION_RE = re.compile(
    r"^\ufeff?\S+Z Image: (ubuntu-[^\s]+)\n^\ufeff?\S+Z Version: ([0-9]{8}\.[0-9.]+)$",
    re.MULTILINE)
PROFILE_RE = (
    re.compile(r"^\ufeff?\S+Z Setup go version spec ([0-9.]+)$", re.MULTILINE),
    re.compile(r"^\ufeff?\S+Z\s+ESHU_LIVE_POSTGRES_IMAGE: (ghcr\.io/eshu-hq/ci-postgres-bookworm@sha256:[0-9a-f]{64})$", re.MULTILINE),
)


def _time(value: str) -> dt.datetime:
    """Parse runner timestamps without losing the chronological ordering."""
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))


def _profile(log: str) -> tuple[str, ...]:
    """Require an explicit comparable runner, toolchain, and pinned database."""
    image = IMAGE_VERSION_RE.search(log)
    if image is None:
        raise ValueError("source log lacks runner image version profile")
    profile: list[str] = [image.group(1), image.group(2)]
    for pattern in PROFILE_RE:
        match = pattern.search(log)
        if match is None:
            raise ValueError("source log lacks image, Go, or Postgres profile")
        profile.append(match.group(1))
    return tuple(profile)


def _parse_shard(log: str, number: int, total: int) -> tuple[list[str], list[tuple[str, str, float]], tuple[str, ...]]:
    """Return ordered tests and classified row residuals from one proof step."""
    profile = _profile(log)
    previous: dt.datetime | None = None
    block: list[tuple[str, float]] = []
    seen_packages: set[str] = set()
    observed: list[str] = []
    rows: list[tuple[str, str, float]] = []
    summary = False
    for line in log.splitlines():
        if previous is None:
            if match := START_RE.fullmatch(line):
                if (int(match.group(2)), int(match.group(3))) != (number, total):
                    raise ValueError("source proof step has wrong shard boundary")
                previous = _time(match.group(1))
            continue
        if match := TEST_RE.fullmatch(line):
            if summary:
                raise ValueError("source has tests after terminal summary")
            seconds = float(match.group(3))
            if not 0 <= seconds < 100000:
                raise ValueError("source has invalid test elapsed")
            block.append((match.group(2), seconds))
        elif match := PACKAGE_RE.fullmatch(line):
            if summary:
                raise ValueError("source has rows after terminal summary")
            package, passed, expected = match.group(2), int(match.group(3)), int(match.group(4))
            if not weights.PACKAGE_RE.fullmatch(package) or not block or passed != expected or passed != len(block):
                raise ValueError("source has invalid package PASS row")
            now = _time(match.group(1))
            residual = (now - previous).total_seconds() - sum(seconds for _, seconds in block)
            if residual < 0:
                raise ValueError("source has negative row residual")
            kind = "first_row" if not rows else ("repeat" if package in seen_packages else "first_later")
            rows.append((package, kind, residual))
            observed.extend(f"{package}|{test}" for test, _ in block)
            if kind == "repeat" and rows[-1][0] != package:
                raise ValueError("source package rows are noncontiguous")
            seen_packages.add(package)
            previous, block = now, []
        elif match := SUMMARY_RE.fullmatch(line):
            if ((int(match.group(1)), int(match.group(2))) != (number, total)
                    or int(match.group(3)) != len(observed)
                    or int(match.group(4)) != len(observed) or block):
                raise ValueError("source terminal summary does not cover all rows")
            summary = True
        elif ("live-postgres-readiness:" in line and "live-postgres-readiness: suite_elapsed=" not in line) or re.search(r"\bTest[A-Za-z0-9_]+: (?:FAIL|SKIP)\b", line):
            raise ValueError("source has unsupported failed or skipped proof row")
    if previous is None or not summary or block or not rows:
        raise ValueError("source shard proof is partial")
    return observed, rows, profile


def _mean_tenth(values: list[float], label: str, positive: bool = False) -> float:
    if not values:
        raise ValueError(f"no measured {label}")
    result = round(statistics.mean(values), 1)
    if result < 0 or (positive and result <= 0):
        raise ValueError(f"measured {label} is not positive")
    return result


def build_document(sources: list[Path], root: Path) -> dict[str, object]:
    """Fit a v2 document after source-SHA inventory and CI provenance checks.

    Args:
        sources: Directories named by run ID, each with run/jobs JSON and shard logs.
        root: Git checkout containing the source commit objects.

    Returns:
        A validated v2 JSON document with measured prices and positive defaults.

    Raises:
        ValueError: A source is incomplete, incompatible, or cannot identify a
            cold/repeat category.
    """
    if len(sources) < 2:
        raise ValueError("v2 fit needs at least two complete main runs")
    source_runs: list[str] = []
    expected_plan: list[str] | None = None
    expected_profile: tuple[str, ...] | None = None
    expected_count: int | None = None
    fixed: list[float] = []
    test_values: dict[str, list[float]] = defaultdict(list)
    categories: dict[str, dict[str, list[float]]] = defaultdict(lambda: defaultdict(list))
    for source in sources:
        run_id, plan, logs, overhead = provenance.read_source(source, root)
        if run_id in source_runs:
            raise ValueError("v2 source run IDs must be distinct")
        if expected_plan is not None and plan != expected_plan:
            raise ValueError("source inventories differ")
        expected_plan = plan
        if expected_count is not None and len(logs) != expected_count:
            raise ValueError("source shard topology differs")
        expected_count = len(logs)
        observed: list[str] = []
        row_packages: list[str] = []
        for number, log in sorted(logs.items()):
            tests, rows, profile = _parse_shard(log, number, len(logs))
            if expected_profile is not None and profile != expected_profile:
                raise ValueError("source image, Go, or Postgres profile differs")
            expected_profile = profile
            observed.extend(tests)
            for package, kind, residual in rows:
                row_packages.append(package)
                categories[package][kind].append(residual)
            _collect_test_values(log, tests, test_values)
        if Counter(observed) != Counter(plan):
            raise ValueError("source PASS rows differ from source-SHA canonical inventory or coverage")
        package_order = list(dict.fromkeys(run.split("|", 1)[0] for run in plan))
        positions = {package: index for index, package in enumerate(package_order)}
        if row_packages != sorted(row_packages, key=positions.__getitem__):
            raise ValueError("source package rows differ from canonical order")
        source_runs.append(run_id)
        fixed.extend(overhead)
    first: dict[str, float] = {}
    repeat: dict[str, float] = {}
    paired_cold = [statistics.mean(kinds["first_row"]) - statistics.mean(kinds["first_later"])
                   for kinds in categories.values() if kinds["first_row"] and kinds["first_later"]]
    cold = _mean_tenth(paired_cold, "identifiable cold component", positive=True)
    for package, kinds in categories.items():
        later = kinds["first_later"]
        first_rows = kinds["first_row"]
        if later:
            first[package] = _mean_tenth(later, f"first-use {package}")
        elif first_rows:
            first[package] = _mean_tenth([value - cold for value in first_rows],
                                         f"first-use {package}")
        else:
            raise ValueError(f"no measured first use for {package}")
        if kinds["repeat"]:
            repeat[package] = _mean_tenth(kinds["repeat"], f"repeat {package}", positive=True)
    if not repeat:
        raise ValueError("no measured repeat row category")
    tests = {run: _mean_tenth(values, run) for run, values in sorted(test_values.items())}
    if set(tests) != set(expected_plan or []):
        raise ValueError("source test prices differ from canonical inventory")
    document: dict[str, object] = {
        "schema_version": 2, "source_runs": source_runs, "shard_count": 5,
        "fixed_job_seconds": _mean_tenth(fixed, "fixed job overhead"),
        "cold_cache_seconds": cold,
        "default_test_seconds": _mean_tenth(list(tests.values()), "default test", positive=True),
        "default_package_first_seconds": _mean_tenth(list(first.values()), "default first use", positive=True),
        "default_package_repeat_seconds": _mean_tenth(list(repeat.values()), "default repeat", positive=True),
        "package_first_seconds": dict(sorted(first.items())),
        "package_repeat_seconds": dict(sorted(repeat.items())),
        "test_seconds": tests,
    }
    v2.weights_from_json(document)
    return document


def _collect_test_values(log: str, expected: list[str], values: dict[str, list[float]]) -> None:
    """Associate each test line with its closing package in source row order."""
    pending: list[tuple[str, float]] = []
    observed: list[str] = []
    for line in log.splitlines():
        if match := TEST_RE.fullmatch(line):
            pending.append((match.group(2), float(match.group(3))))
        elif match := PACKAGE_RE.fullmatch(line):
            package = match.group(2)
            for test, seconds in pending:
                run = f"{package}|{test}"
                values[run].append(seconds)
                observed.append(run)
            pending = []
    if pending or observed != expected:
        raise ValueError("source test elapsed values differ from PASS rows")
