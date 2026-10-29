"""Fit row-aware readiness weights from complete, compatible main CI runs."""

from __future__ import annotations

from collections import Counter, defaultdict
from dataclasses import dataclass
import datetime as dt
import math
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


@dataclass(frozen=True)
class ImageProvenance:
    """Runner image versions seen per source run and shard.

    Attributes:
        name: The one runner image name every source shard ran on.
        versions: Image version by run ID, then by shard number.
    """

    name: str
    versions: dict[str, dict[int, str]]

    def line(self) -> str:
        """Render one stdout line for the refit PR body and evidence note."""
        runs = "; ".join(
            f"run {run} " + " ".join(f"shard{number}={version}"
                                     for number, version in sorted(shards.items()))
            for run, shards in self.versions.items())
        distinct = ", ".join(sorted({version for shards in self.versions.values()
                                     for version in shards.values()}))
        return (f"weights refresh v2: runner image {self.name} versions {runs}; "
                f"distinct {distinct}")


def _profile(log: str) -> tuple[tuple[str, ...], str]:
    """Return the gated runner profile and the reported image version.

    The gate is (image name, Go version, Postgres digest). The image version
    is provenance only: hosted runners roll versions weekly over several days
    and cannot be pinned, and the version is never used in pricing (#9411).
    """
    image = IMAGE_VERSION_RE.search(log)
    if image is None:
        raise ValueError("source log lacks runner image version profile")
    profile: list[str] = [image.group(1)]
    for pattern in PROFILE_RE:
        match = pattern.search(log)
        if match is None:
            raise ValueError("source log lacks image, Go, or Postgres profile")
        profile.append(match.group(1))
    return tuple(profile), image.group(2)


def _parse_shard(log: str, number: int, total: int) -> tuple[
        list[str], list[tuple[str, str, float]], tuple[tuple[str, ...], str],
        list[frozenset[str]]]:
    """Return ordered tests, classified row residuals, and the runner profile."""
    profile = _profile(log)
    previous: dt.datetime | None = None
    block: list[tuple[str, float]] = []
    seen_packages: set[str] = set()
    observed: list[str] = []
    rows: list[tuple[str, str, float]] = []
    row_members: list[frozenset[str]] = []
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
            if kind == "repeat" and rows[-1][0] != package:
                raise ValueError("source package rows are noncontiguous")
            rows.append((package, kind, residual))
            row_members.append(frozenset(f"{package}|{test}" for test, _ in block))
            observed.extend(f"{package}|{test}" for test, _ in block)
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
    return observed, rows, profile, row_members


def _mean_tenth(values: list[float], label: str, positive: bool = False) -> float:
    if not values:
        raise ValueError(f"no measured {label}")
    result = round(statistics.mean(values), 1)
    if result < 0 or (positive and result <= 0):
        raise ValueError(f"measured {label} is not positive")
    return result


MIN_COLD_PAIRS = 2
COLD_VARIANCE_FLOOR = 1.0


@dataclass(frozen=True)
class ColdComponent:
    """One eligible paired package's contribution to the cold-cache fit.

    Attributes:
        package: The paired package these residuals belong to.
        diff: Mean first-row residual minus mean first-later residual, seconds.
        first_rows: Count of first-row residuals behind the diff.
        first_laters: Count of first-later residuals behind the diff.
        variance: Floored variance of the diff, seconds squared.
        weight: Normalized inverse-variance weight in the cold mean.
    """

    package: str
    diff: float
    first_rows: int
    first_laters: int
    variance: float
    weight: float

    def line(self) -> str:
        """Render one stdout line for the refit PR body and evidence note."""
        return (f"weights refresh v2: cold {self.package} "
                f"diff={self.diff:.1f}s n_first_row={self.first_rows} "
                f"n_first_later={self.first_laters} "
                f"variance={self.variance:.2f}s2 weight={self.weight:.4f}")


def fit_cold(categories: dict[str, dict[str, list[float]]]) -> tuple[float, list[ColdComponent]]:
    """Fit the cold-cache constant as an inverse-variance-weighted mean.

    A paired package is eligible only with at least two first-row and two
    first-later residuals. Each eligible diff is weighted by the inverse of
    its difference variance (sample variances over counts, floored so no
    package takes unbounded weight from two identical samples), so a giant
    binary's compile noise cannot veto a tight small-package measurement.

    Args:
        categories: Per-package residuals by row kind, as accumulated in
            fit_with_provenance.

    Returns:
        The cold-cache seconds rounded to a tenth, and the per-package
        components sorted by package.

    Raises:
        ValueError: Fewer than two eligible paired packages, a non-positive
            weighted cold, or a cold within two standard errors of zero.
    """
    eligible: list[tuple[str, float, float, int, int]] = []
    for package in sorted(categories):
        kinds = categories[package]
        first_rows, first_laters = kinds["first_row"], kinds["first_later"]
        if len(first_rows) < 2 or len(first_laters) < 2:
            continue
        diff = statistics.mean(first_rows) - statistics.mean(first_laters)
        variance = max(statistics.variance(first_rows) / len(first_rows)
                       + statistics.variance(first_laters) / len(first_laters),
                       COLD_VARIANCE_FLOOR)
        eligible.append((package, diff, variance, len(first_rows), len(first_laters)))
    if len(eligible) < MIN_COLD_PAIRS:
        raise ValueError("measured identifiable cold component needs at least two "
                         f"eligible paired packages, found {len(eligible)}")
    total = sum(1.0 / variance for _, _, variance, _, _ in eligible)
    cold = round(sum(diff / variance for _, diff, variance, _, _ in eligible) / total, 1)
    if cold <= 0:
        raise ValueError("measured identifiable cold component is not positive")
    if cold - 2 * math.sqrt(1.0 / total) <= 0:
        raise ValueError("measured identifiable cold component is not "
                         "significant at two standard errors")
    return cold, [ColdComponent(package, diff, first_rows, first_laters, variance,
                                (1.0 / variance) / total)
                  for package, diff, variance, first_rows, first_laters in eligible]


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
    return fit_with_provenance(sources, root)[0]


def fit_with_provenance(sources: list[Path], root: Path) -> tuple[
        dict[str, object], ImageProvenance, list[ColdComponent]]:
    """Fit a v2 document and report the runner image versions it accepted.

    Args:
        sources: Directories named by run ID, each with run/jobs JSON and shard logs.
        root: Git checkout containing the source commit objects.

    Returns:
        The validated v2 document (as from build_document), the image
        provenance seen across every shard of every source, and the
        per-package cold components behind the fitted cold constant.

    Raises:
        ValueError: A source is incomplete, incompatible, or cannot identify a
            cold/repeat category.
    """
    if len(sources) < 2:
        raise ValueError("v2 fit needs at least two complete main runs")
    source_runs: list[str] = []
    image_versions: dict[str, dict[int, str]] = {}
    expected_plan: list[str] | None = None
    expected_profile: tuple[str, ...] | None = None
    expected_count: int | None = None
    fixed: list[float] = []
    test_values: dict[str, list[float]] = defaultdict(list)
    categories: dict[str, dict[str, list[float]]] = defaultdict(lambda: defaultdict(list))
    for source in sources:
        run_id, sha, plan, logs, overhead = provenance.read_source(source, root)
        expected_rows = provenance.source_rows(root, sha, plan, len(logs))
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
            tests, rows, (profile, version), row_members = _parse_shard(log, number, len(logs))
            if row_members != expected_rows[number]:
                raise ValueError(f"source shard {number} row selected test set differs from source weights")
            if expected_profile is not None and profile != expected_profile:
                raise ValueError("source image name, Go, or Postgres profile differs")
            expected_profile = profile
            image_versions.setdefault(run_id, {})[number] = version
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
    cold, cold_components = fit_cold(categories)
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
    if expected_profile is None:
        raise ValueError("source runs have no shard profile")
    return document, ImageProvenance(expected_profile[0], image_versions), cold_components


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
