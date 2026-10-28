"""Budget and drift contract tests on the model CI actually cuts (#9100).

``scripts/test-live-postgres-readiness-shards.py`` imports
``ActiveBudgetContractTest`` next to the v1 mechanics in
``live_postgres_readiness_balance_cases``. Every case here loads the model
the way the workflow's ``shards.py matrix`` step does: the committed default
path, which prefers a present v2 file (five shards) and falls back to v1 (four
shards) only when v2 is absent. The shard count comes from
``live_postgres_readiness_reports.shard_ids``, the same derivation the matrix,
report, select, and verify commands use. A contract run on a retired model
would gate capacity CI no longer has, so the first case binds the model and
count to the matrix output and to the committed files on disk.
"""

from __future__ import annotations

from dataclasses import replace
import json
from pathlib import Path
import subprocess
import sys
import unittest

LIB = Path(__file__).parent
sys.path.insert(0, str(LIB))
from live_postgres_readiness_balance_cases import (  # noqa: E402
    SLOW_SECONDS, live_plan, naive_count_bounds, slow_runs)
import live_postgres_readiness_reports as reports  # noqa: E402
import live_postgres_readiness_shards as shards  # noqa: E402
import live_postgres_readiness_v2 as v2  # noqa: E402
import live_postgres_readiness_weights as wt  # noqa: E402

REPO = LIB.parents[1]
Model = wt.Weights | v2.WeightsV2
V2_PATH = wt.WEIGHTS_PATH.with_name("live-postgres-readiness-weights.v2.json")
MATRIX_STEP = "python3 scripts/lib/live_postgres_readiness_shards.py matrix)"


def active_model() -> Model:
    """Load the model CI cuts: the committed default, v2 first when present."""
    return wt.load_weights()


def priced(weights: Model, runs: list[str], seconds: float = SLOW_SECONDS) -> Model:
    """Return a copy of either model with every given run priced at ``seconds``."""
    merged = dict(weights.test)
    merged.update({run: wt._tenths(seconds, run) for run in runs})
    return replace(weights, test=merged)


def expected_shard_count() -> int:
    """Read the active count from the committed files, independent of the loader."""
    if V2_PATH.exists():
        count = json.loads(V2_PATH.read_text(encoding="utf-8"))["shard_count"]
        assert isinstance(count, int), count
        return count
    return wt.SHARD_COUNT


class ActiveBudgetContractTest(unittest.TestCase):
    """Prove the active model's cut fits the budget and keeps drift bounded."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.plan = live_plan()
        cls.weights = active_model()
        cls.shard_ids = reports.shard_ids(cls.weights)
        cls.count = len(cls.shard_ids)

    def jobs(self, runs: list[str], weights: Model, bounds: list[int]) -> list[float]:
        """Predicted job seconds per shard for explicit cut points."""
        return wt.shard_job_seconds(runs, weights, bounds)

    def cut(self, runs: list[str], weights: Model) -> list[float]:
        """Predicted job seconds per shard under the active model's own cut."""
        return self.jobs(runs, weights, wt.weighted_bounds(runs, weights, self.count))

    def test_contract_model_is_the_model_ci_cuts(self) -> None:
        workflow = (REPO / ".github/workflows/live-postgres-readiness.yml").read_text(
            encoding="utf-8")
        self.assertEqual(workflow.count(MATRIX_STEP), 1, "CI matrix step moved or gained flags")
        matrix = subprocess.run([sys.executable, str(LIB / "live_postgres_readiness_shards.py"),
                                 "matrix"], text=True, capture_output=True, check=False)
        self.assertEqual(matrix.returncode, 0, matrix.stderr)
        include = json.loads(matrix.stdout)["include"]
        self.assertEqual([f"{cell['shard']}/{cell['total']}" for cell in include],
                         list(self.shard_ids))
        self.assertEqual(self.count, expected_shard_count())
        self.assertIsInstance(self.weights, v2.WeightsV2 if V2_PATH.exists() else wt.Weights)

    def test_active_weights_fit_the_budget_on_the_live_plan(self) -> None:
        # The live plan changes with every enrollment, so this holds the 15%
        # acceptance bound of #8930 and the budget, not exact figures.
        self.assertEqual(wt.budget_errors(self.plan, self.weights), [])
        jobs = self.cut(self.plan, self.weights)
        self.assertEqual(len(jobs), self.count)
        self.assertLessEqual(max(jobs), wt.BUDGET_SECONDS, jobs)
        mean = sum(jobs) / len(jobs)
        self.assertLessEqual(max(jobs) / mean - 1, 0.15, jobs)
        self.assertGreaterEqual(min(jobs) / mean - 1, -0.15, jobs)

    def test_gate_bites_when_unmeasured_tests_blow_the_budget(self) -> None:
        extra = slow_runs(300, package=self.plan[-1].split("|")[0])
        errors = wt.budget_errors([*self.plan, *extra], priced(self.weights, extra))
        self.assertTrue(errors)
        self.assertRegex(errors[0], r"exceeds 80% of the 1800 s cap|fits the job budget")

    def test_default_partition_follows_the_active_cut(self) -> None:
        bounds = wt.weighted_bounds(self.plan, self.weights, self.count)
        assigned: list[str] = []
        for index, shard in enumerate(self.shard_ids):
            selected = shards.partition_runs(self.plan, shard)
            self.assertEqual(selected, self.plan[bounds[index]:bounds[index + 1]])
            assigned.extend(selected)
        self.assertEqual(assigned, self.plan)

    def test_active_weights_have_no_drift_on_the_live_plan(self) -> None:
        drift = wt.drift_report(self.plan, self.weights)
        self.assertEqual(wt.drift_errors(drift), [])
        self.assertLessEqual(len(drift.defaulted_tests), wt.MAX_DEFAULTED_TESTS)

    def test_drift_gate_bites_on_too_many_unweighted_tests(self) -> None:
        # Plant in the last package: v2 rejects a package that re-enters the plan.
        unweighted = slow_runs(wt.MAX_DEFAULTED_TESTS + 1, prefix="TestNoWeight",
                               package=self.plan[-1].split("|")[0])
        drift = wt.drift_report([*self.plan, *unweighted], self.weights)
        self.assertEqual(len(drift.defaulted_tests), len(unweighted)
                         + len(wt.drift_report(self.plan, self.weights).defaulted_tests))
        self.assertTrue(wt.drift_errors(drift))
        newpkgs = [f"./internal/newpkg{index}|TestNew{index}Live" for index in range(
            wt.MAX_DEFAULTED_PACKAGES + 1)]
        package_drift = wt.drift_report([*self.plan, *newpkgs], self.weights)
        self.assertTrue(wt.drift_errors(package_drift))

    def test_seeded_slow_tests_overflow_a_count_split_but_not_the_weighted_one(self) -> None:
        position = 70  # inside shard 1's range under the retired count split
        # 12x25 s planted in shard 1's package push a naive count split past
        # the 1800 s cap; the weighted cut must still absorb them under the
        # 1440 s budget. On the #9100 base the four-shard v1 cut scored this
        # case at 1439.9 s (13 seeds: 1441.6 s), while the five-shard v2 cut
        # CI runs scored 1356.0 s (13 seeds: 1359.9 s).
        inserted = slow_runs(12, package=self.plan[position].split("|")[0])
        plan = [*self.plan[:position], *inserted, *self.plan[position:]]
        weights = priced(self.weights, inserted)
        naive = self.jobs(plan, weights, naive_count_bounds(len(plan), self.count))
        self.assertGreater(max(naive), wt.JOB_CAP_SECONDS, naive)
        jobs = self.cut(plan, weights)
        self.assertLessEqual(max(jobs), wt.BUDGET_SECONDS, jobs)
        self.assertEqual(wt.budget_errors(plan, weights), [])

    def test_seeded_count_split_already_fails_the_gate_on_the_unchanged_plan(self) -> None:
        naive = self.jobs(self.plan, self.weights,
                          naive_count_bounds(len(self.plan), self.count))
        self.assertGreater(max(naive), wt.BUDGET_SECONDS, naive)

    def test_tests_appended_at_the_end_are_absorbed(self) -> None:
        appended = slow_runs(10, prefix="TestAppendedTail",
                             package=self.plan[-1].split("|")[0])
        plan = [*self.plan, *appended]
        jobs = self.cut(plan, priced(self.weights, appended, 20.0))
        self.assertLessEqual(max(jobs), wt.BUDGET_SECONDS, jobs)


if __name__ == "__main__":
    unittest.main()
