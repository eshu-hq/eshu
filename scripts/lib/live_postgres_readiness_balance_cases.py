"""V1 cut mechanics and refresher tests for the readiness shard planner (#8930).

``scripts/test-live-postgres-readiness-shards.py`` imports ``BalanceContractTest``
so one command runs the fail-closed accounting tests and these time-balance
tests together. The cases here pin the v1 four-shard planner, which stays the
fallback when the v2 file is absent:

* a fixed synthetic plan bounds the cut's balance and tail growth,
* the planner CLI, the cut cache, and malformed files fail closed,
* a changed weights file can never drop or double a proof silently.

The budget, drift, seeded-overflow, and appended-tail cases run on the model
CI cuts, so they live in ``live_postgres_readiness_weights_budget_cases``
(#9100): gating the live plan on the retired four-shard cut failed enrollments
CI's six-shard cut absorbs.
"""

from __future__ import annotations

import itertools
import json
import math
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

LIB = Path(__file__).parent
sys.path.insert(0, str(LIB))
import live_postgres_readiness_shards as shards  # noqa: E402
import live_postgres_readiness_weights as wt  # noqa: E402
import live_postgres_readiness_weights_refresh as refresh  # noqa: E402

REPO = LIB.parents[1]
SHARD_IDS = ("1/4", "2/4", "3/4", "4/4")
SLOW_SECONDS = 25.0
HEAD = "c" * 40


def live_plan() -> list[str]:
    """Return the flat canonical plan derived from the live inventory."""
    return [f"{package}|{test}" for package, tests in shards.known_tests().items()
            for test in tests]


def naive_count_bounds(count: int, shard_count: int = 4) -> list[int]:
    """Reproduce the retired count-quarter cut so the seeded cases can bite."""
    width = math.ceil(count / shard_count)
    return [min(index * width, count) for index in range(shard_count)] + [count]


def slow_runs(count: int, prefix: str = "TestSeededSlow",
              package: str = "./internal/storage/postgres") -> list[str]:
    """Build ``count`` synthetic plan lines in one package."""
    return [f"{package}|{prefix}{index}Live" for index in range(count)]


def with_slow(weights: wt.Weights, runs: list[str],
              seconds: float = SLOW_SECONDS) -> wt.Weights:
    """Return weights that price every given run at ``seconds``."""
    return weights.with_test_seconds({run: seconds for run in runs})


class BalanceContractTest(unittest.TestCase):
    """Prove the weighted cut fits the budget and cannot lose a proof."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.plan = live_plan()
        cls.weights = wt.weights_from_json(json.loads(wt.WEIGHTS_PATH.read_text(
            encoding="utf-8")))

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.legacy_weights_path = self.directory / "legacy.json"
        self.legacy_weights_path.write_bytes(wt.WEIGHTS_PATH.read_bytes())

    def job_seconds(self, runs: list[str], weights: wt.Weights,
                    bounds: list[int]) -> list[float]:
        """Predicted job seconds per shard for explicit cut points."""
        return wt.shard_job_seconds(runs, weights, bounds)

    @staticmethod
    def fixture() -> tuple[list[str], wt.Weights]:
        """Build a deterministic uneven plan whose near-even cut needs mid-package splits."""
        sizes = (310, 90, 250, 140, 60, 170, 180)
        plan = [f"./internal/pkg{index}|Test{index}x{test}Live"
                for index, size in enumerate(sizes) for test in range(size)]
        document = {
            "schema_version": 1, "source_runs": ["1"], "fixed_job_seconds": 80.0,
            "cold_cache_seconds": 100.0, "default_test_seconds": 2.0,
            "default_package_seconds": 50.0,
            "package_seconds": {f"./internal/pkg{index}": 50.0 for index in range(len(sizes))},
            "test_seconds": {run: 2.0 for run in plan}}
        return plan, wt.weights_from_json(document)

    @staticmethod
    def spread(jobs: list[float]) -> float:
        """Largest absolute deviation of a shard from the mean, as a fraction."""
        mean = sum(jobs) / len(jobs)
        return max(abs(job / mean - 1) for job in jobs)

    def test_fixture_cut_stays_within_one_percent_of_the_mean(self) -> None:
        plan, weights = self.fixture()
        jobs = self.job_seconds(plan, weights, wt.weighted_bounds(plan, weights))
        self.assertLessEqual(self.spread(jobs), 0.01, jobs)

    def test_balance_guard_rejects_a_fourteen_percent_misplaced_boundary(self) -> None:
        plan, weights = self.fixture()
        bounds = wt.weighted_bounds(plan, weights)
        planted = [bounds[0], bounds[1] - 40, *bounds[2:]]
        jobs = self.job_seconds(plan, weights, planted)
        self.assertGreater(self.spread(jobs), 0.14, jobs)
        self.assertLessEqual(self.spread(jobs), 0.15, jobs)  # the live bound would accept it
        self.assertGreater(self.spread(jobs), 0.01, jobs)  # the fixture guard rejects it

    def test_partition_runs_and_plan_cli_follow_the_weighted_cut(self) -> None:
        bounds = wt.weighted_bounds(self.plan, self.weights)
        for index, shard in enumerate(SHARD_IDS):
            self.assertEqual(shards.partition_runs(self.plan, shard, self.weights),
                             self.plan[bounds[index]:bounds[index + 1]])
        sizes = [len(shards.partition_runs(self.plan, shard, self.weights))
                 for shard in SHARD_IDS]
        self.assertEqual([0, *itertools.accumulate(sizes)], bounds)
        result = self.invoke("plan", "--shard", "1/4", input_text=self.full_tsv())
        self.assertEqual(result.returncode, 0, result.stderr)
        planned = sum(len(row.split("\t")[2].split(" ")) for row in result.stdout.splitlines())
        self.assertEqual(planned, sizes[0])

    def test_gate_bites_when_unmeasured_tests_blow_the_budget(self) -> None:
        """The v1 fallback names the cap; the active-model twin is in the budget cases."""
        extra = slow_runs(300)
        heavy = with_slow(self.weights, extra)
        errors = wt.budget_errors([*self.plan, *extra], heavy)
        self.assertTrue(errors)
        self.assertRegex(errors[0], r"exceeds 80% of the 1800 s cap")

    def test_drift_message_states_the_no_feature_edit_rule(self) -> None:
        """The drift gate tells feature PRs to leave the weights file alone (#9148)."""
        drift = wt.Drift(
            defaulted_tests=tuple(
                f"./internal/newpkg|TestNoWeight{index}Live"
                for index in range(wt.MAX_DEFAULTED_TESTS + 1)),
            defaulted_packages=(), stale_tests=())
        errors = wt.drift_errors(drift)
        self.assertTrue(errors)
        self.assertIn("must not edit", errors[0])
        self.assertIn("default_test_seconds", errors[0])

    def test_single_unweighted_test_runs_on_default_without_tripping_gate(self) -> None:
        """One hand-added key is review's call, not the gate's: the cap stays 40 (#9148)."""
        drift = wt.Drift(defaulted_tests=("./internal/newpkg|TestHandAddedLive",),
                         defaulted_packages=(), stale_tests=())
        self.assertEqual(wt.drift_errors(drift), [])

    def test_bounds_cache_is_keyed_on_the_shard_count(self) -> None:
        four = wt.weighted_bounds(self.plan, self.weights, 4)
        two = wt.weighted_bounds(self.plan, self.weights, 2)
        self.assertEqual((len(four), len(two)), (5, 3))
        self.assertEqual((two[0], two[-1]), (0, len(self.plan)))
        self.assertEqual(wt.weighted_bounds(self.plan, self.weights, 4), four)

    def test_fixture_tail_growth_is_bounded(self) -> None:
        # Exact growth depends on indivisible tests and package-start costs.
        # Keep this bound on a fixed plan; enrollments change the live cuts.
        plan, weights = self.fixture()
        base = self.job_seconds(plan, weights, wt.weighted_bounds(plan, weights))
        appended = slow_runs(10, prefix="TestAppendedTail",
                             package=plan[-1].split("|")[0])
        plan = [*plan, *appended]
        weights = with_slow(weights, appended, 20.0)
        jobs = self.job_seconds(plan, weights, wt.weighted_bounds(plan, weights))
        self.assertLessEqual(max(jobs) - max(base), 20.0 * 10 / 2, (base, jobs))

    def test_bounds_are_deterministic_contiguous_and_nonempty(self) -> None:
        first = wt.weighted_bounds(self.plan, self.weights)
        self.assertEqual(first, wt.weighted_bounds(list(self.plan), self.weights))
        self.assertEqual(first[0], 0)
        self.assertEqual(first[-1], len(self.plan))
        self.assertTrue(all(low < high for low, high in zip(first, first[1:])), first)

    def test_union_is_exact_under_perturbed_weights(self) -> None:
        for step in (3, 7, 11):
            changed = self.weights.with_test_seconds(
                {run: 5.0 + index % step * 40.0
                 for index, run in enumerate(self.plan) if index % step == 0})
            with self.subTest(step=step):
                assigned = [line for shard in SHARD_IDS
                            for line in shards.partition_runs(self.plan, shard, changed)]
                self.assertEqual(assigned, self.plan)

    def test_too_few_runs_for_four_shards_fails_closed(self) -> None:
        with self.assertRaises(ValueError):
            wt.weighted_bounds(self.plan[:3], self.weights)

    def write_weights(self, name: str, value: object) -> Path:
        """Write a candidate weights file."""
        path = self.directory / name
        path.write_text(value if isinstance(value, str) else json.dumps(value),
                        encoding="utf-8")
        return path

    def committed_json(self) -> dict[str, object]:
        """Read the committed weights as a mutable JSON object."""
        return json.loads(wt.WEIGHTS_PATH.read_text(encoding="utf-8"))

    def test_malformed_weights_fail_closed(self) -> None:
        good = self.committed_json()
        cases: dict[str, object] = {"missing": self.directory / "absent.json",
                                    "empty": "", "truncated": '{"schema_version": 1'}
        duplicated = wt.WEIGHTS_PATH.read_text(encoding="utf-8")
        cases["duplicate-key"] = duplicated.replace(
            '"schema_version": 1,', '"schema_version": 1, "schema_version": 1,', 1)
        for key, bad in (("schema_version", 2), ("fixed_job_seconds", -1),
                         ("cold_cache_seconds", float("nan")),
                         ("default_test_seconds", True), ("test_seconds", [])):
            cases[f"bad-{key}"] = {**good, key: bad}
        test_seconds = dict(good["test_seconds"])  # type: ignore[arg-type]
        test_seconds["not a plan line"] = 1.0
        cases["bad-test-key"] = {**good, "test_seconds": test_seconds}
        cases["fractional-tenth"] = {**good, "default_test_seconds": 1.234}
        for name, value in cases.items():
            with self.subTest(case=name), self.assertRaises(ValueError):
                path = value if isinstance(value, Path) else self.write_weights(
                    f"{name}.json", value)
                wt.load_weights(path)

    def invoke(self, *args: str, input_text: str | None = None
               ) -> subprocess.CompletedProcess[str]:
        """Run the planner CLI against the legacy model under test."""
        arguments = list(args)
        if "--weights" not in arguments:
            arguments[1:1] = ["--weights", str(self.legacy_weights_path)]
        return subprocess.run([sys.executable, str(LIB / "live_postgres_readiness_shards.py"),
                               *arguments], input=input_text, text=True, capture_output=True,
                              check=False)

    def full_tsv(self) -> str:
        """Render the live full list-packages TSV."""
        proc = subprocess.run([sys.executable, str(LIB / "live_postgres_readiness_results.py"),
                               "list-packages"], text=True, capture_output=True, check=True)
        return proc.stdout

    def test_reports_cut_under_other_weights_fail_the_aggregate(self) -> None:
        """A weights change that moves a cut cannot pass against a stale report."""
        alternate = self.committed_json()
        tests = dict(alternate["test_seconds"])  # type: ignore[arg-type]
        for line in self.plan[: len(self.plan) // 8]:
            tests[line] = 0.1
        alternate["test_seconds"] = tests
        other = self.write_weights("alternate.json", alternate)
        self.assertNotEqual(
            wt.weighted_bounds(self.plan, wt.load_weights(other)),
            wt.weighted_bounds(self.plan, self.weights))
        plan_path = self.directory / "full.tsv"
        plan_path.write_text(self.full_tsv(), encoding="utf-8")
        reports: list[str] = []
        for shard in SHARD_IDS:
            expected = shards.partition_runs(self.plan, shard, wt.load_weights(other))
            by_package: dict[str, list[str]] = {}
            for line in expected:
                package, test = line.split("|")
                by_package.setdefault(package, []).append(test)
            manifest = self.directory / f"manifest-{shard[0]}.tsv"
            manifest.write_text("".join(
                f"{package}|0|{','.join(tests)}\n" for package, tests in by_package.items()),
                encoding="utf-8")
            report = self.directory / f"report-{shard[0]}.json"
            result = self.invoke("report", "--head", HEAD, "--shard", shard,
                                 "--plan", str(plan_path), "--manifest", str(manifest),
                                 "--output", str(report), "--weights", str(other))
            self.assertEqual(result.returncode, 0, result.stderr)
            reports.append(str(report))
        matching = self.invoke("verify", "--head", HEAD, "--plan", str(plan_path),
                               "--weights", str(other), *reports)
        self.assertEqual(matching.returncode, 0, matching.stderr)
        stale = self.invoke("verify", "--head", HEAD, "--plan", str(plan_path), *reports)
        self.assertNotEqual(stale.returncode, 0)
        self.assertRegex(stale.stderr, r"plan mismatch for|shard union differs")

    def test_plan_cli_rejects_missing_weights_file(self) -> None:
        result = self.invoke("plan", "--shard", "1/4", "--weights",
                             str(self.directory / "absent.json"), input_text=self.full_tsv())
        self.assertNotEqual(result.returncode, 0)

    def test_job_cap_matches_the_workflow_timeout(self) -> None:
        workflow = (REPO / ".github/workflows/live-postgres-readiness.yml").read_text(
            encoding="utf-8")
        job = workflow.split("\n  live-postgres-readiness-shards:\n", 1)[1]
        job = job.split("\n  live-postgres-readiness:\n", 1)[0]
        minutes = re.findall(r"(?m)^    timeout-minutes: (\d+)$", job)
        self.assertEqual(minutes, [str(wt.JOB_CAP_SECONDS // 60)])


class RefreshTest(unittest.TestCase):
    """Cover the weights refresher on synthetic shard logs."""

    RUN_STEP = "bash scripts/run-live-postgres-readiness-tests.sh"

    SHARD_ONE = "\n".join([
        f"2026-01-01T00:00:00.0000000Z ##[group]Run {RUN_STEP} --shard \"1/4\"",
        "2026-01-01T00:01:00.0000000Z TestAlphaLive: PASS elapsed=10.0s",
        "2026-01-01T00:01:05.0000000Z TestBetaLive: PASS elapsed=20.0s",
        "2026-01-01T00:02:00.0000000Z live-postgres-readiness: ./internal/p1 2/2 PASS",
        "2026-01-01T00:02:10.0000000Z TestGammaLive: PASS elapsed=5.0s",
        "2026-01-01T00:02:30.0000000Z live-postgres-readiness: ./internal/p2 1/1 PASS",
    ])
    SHARD_TWO = "\n".join([
        f"2026-01-01T00:00:00.0000000Z ##[group]Run {RUN_STEP} --shard \"2/4\"",
        "2026-01-01T00:01:00.0000000Z TestDeltaLive: PASS elapsed=7.0s",
        "2026-01-01T00:02:05.0000000Z live-postgres-readiness: ./internal/p2 1/1 PASS",
    ])
    PLAN = ["./internal/p1|TestAlphaLive", "./internal/p1|TestBetaLive",
            "./internal/p2|TestGammaLive", "./internal/p2|TestDeltaLive",
            "./internal/p2|TestNewLive"]

    def build(self) -> dict[str, object]:
        sample = refresh.parse_sample([self.SHARD_ONE, self.SHARD_TWO])
        return refresh.build_weights([sample], self.PLAN, ["1"], 82.0)

    def test_weights_render_one_entry_per_line_sorted_without_data_loss(self) -> None:
        """One sorted entry per line, so a one-key change stays a local hunk (#9148).

        No line cap applies in specs/: a realistic plan renders past 500
        lines, and that is the point — concurrent refits merge cleanly.
        """
        document = self.build()
        document["test_seconds"] = {
            f"./internal/p1|TestProof{index}Live": 0.1 for index in range(600)
        }
        rendered = refresh.render(document)
        self.assertEqual(json.loads(rendered), document)
        entry_lines = [line for line in rendered.splitlines()
                       if line.startswith('    "')]
        total_entries = (len(document["package_seconds"])  # type: ignore[arg-type]
                         + len(document["test_seconds"]))  # type: ignore[arg-type]
        self.assertEqual(len(entry_lines), total_entries)
        for line in entry_lines:
            self.assertEqual(line.count('": '), 1, line)
        ordered: dict[str, object] = json.loads(
            rendered, object_pairs_hook=lambda pairs: dict(pairs))
        for key in ("package_seconds", "test_seconds"):
            table = ordered[key]
            assert isinstance(table, dict)
            names = list(table)
            self.assertEqual(names, sorted(names))
        self.assertEqual(refresh.render(json.loads(rendered)), rendered)

    def test_per_test_seconds_package_cost_and_cold_constant(self) -> None:
        built = self.build()
        self.assertEqual(built["test_seconds"], {
            "./internal/p1|TestAlphaLive": 10.0, "./internal/p1|TestBetaLive": 20.0,
            "./internal/p2|TestGammaLive": 5.0, "./internal/p2|TestDeltaLive": 7.0})
        # p2 as the non-first package of shard 1: 30 s wall - 5 s tests = 25 s.
        # p2 as the first package of shard 2: 125 s wall - 7 s tests = 118 s,
        # so the cold-cache constant is 118 - 25 = 93 s.
        self.assertEqual(built["package_seconds"]["./internal/p2"], 25.0)  # type: ignore[index]
        self.assertEqual(built["package_seconds"]["./internal/p1"], 0.0)  # type: ignore[index]
        self.assertEqual(built["cold_cache_seconds"], 93.0)
        self.assertEqual(built["fixed_job_seconds"], 82.0)
        self.assertEqual(built["source_runs"], ["1"])

    def test_defaults_are_means_and_unmeasured_tests_stay_out(self) -> None:
        built = self.build()
        seconds = built["test_seconds"]
        self.assertNotIn("./internal/p2|TestNewLive", seconds)  # type: ignore[operator]
        self.assertEqual(built["default_test_seconds"], 10.5)  # mean of 10, 20, 5, 7
        loaded = wt.weights_from_json(built)
        self.assertEqual(loaded.test["./internal/p2|TestGammaLive"], 50)

    def test_bom_prefixed_log_lines_still_parse(self) -> None:
        bom = "\ufeff"
        prefixed = "\n".join(bom + line if line.startswith("2026") else line
                             for line in self.SHARD_ONE.split("\n"))
        sample = refresh.parse_sample([prefixed, self.SHARD_TWO])
        plain = refresh.parse_sample([self.SHARD_ONE, self.SHARD_TWO])
        self.assertEqual(sample, plain)

    def test_log_that_misses_a_planned_group_fails_closed(self) -> None:
        with self.assertRaises(ValueError):
            refresh.parse_sample(["TestAlphaLive: PASS elapsed=1.0s\n"])
        broken = self.SHARD_ONE.replace("2/2 PASS", "3/3 PASS")
        with self.assertRaises(ValueError):
            refresh.parse_sample([broken])


if __name__ == "__main__":
    unittest.main()
