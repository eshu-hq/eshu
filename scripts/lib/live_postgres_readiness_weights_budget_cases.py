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

The planted cases do not use fixed sizes on the live plan (#9371). A fixed plant
there tests the plan's remaining headroom and its package lumps, not the model:
the old 10 x 20 s tail landed in a band where no five-shard cut meets both the
1440 s budget and the 15% spread, and a correct refit or a PR adding eight live
tests moved the live plan into or out of that band. The appended tail is now
sized to the tail shard's own headroom. The count-split overflow runs on a fixed
synthetic plan. The drift case asserts the drift message itself.
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
NO_FIT = "no shard partition fits the job budget"
TAIL_COUNT = 10
SYNTHETIC_PACKAGES = 20
SYNTHETIC_TESTS = 20
SYNTHETIC_SEEDS = 15
SYNTHETIC_SEED_SECONDS = 60.0


def active_model() -> Model:
    """Load the model CI cuts: the committed default, v2 first when present."""
    return wt.load_weights()


def priced(weights: Model, runs: list[str], seconds: float = SLOW_SECONDS) -> Model:
    """Return a copy of either model with every given run priced at ``seconds``."""
    merged = dict(weights.test)
    merged.update({run: wt._tenths(seconds, run) for run in runs})
    return replace(weights, test=merged)


def synthetic_model(test: dict[str, int] | None = None) -> v2.WeightsV2:
    """Return a fixed v2 model with round prices that no refit changes.

    Every price is in tenths: 120 s fixed, 60 s cold, 10 s per test, 40 s
    first use, and 10 s per repeated row.
    """
    return v2.WeightsV2(source_runs=("1", "2"), fixed=1200, cold=600, default_test=100,
                        default_package_first=400, default_package_repeat=100,
                        package_first={}, package_repeat={}, test=dict(test or {}))


def synthetic_plan() -> list[str]:
    """Return a fixed plan of 20 packages with 20 tests each.

    Under ``synthetic_model`` both the count split and the weighted cut score
    every shard at 1140 s, so the plan fits before anything is seeded.
    """
    return [f"./internal/synthetic/pkg{package:02d}|TestSynthetic{package:02d}x{test:02d}Live"
            for package in range(SYNTHETIC_PACKAGES) for test in range(SYNTHETIC_TESTS)]


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

    def tail(self, prefix: str) -> list[str]:
        """Plant ``TAIL_COUNT`` runs in the last package, which v2 keeps contiguous."""
        return slow_runs(TAIL_COUNT, prefix=prefix, package=self.plan[-1].split("|")[0])

    def overflow_seconds(self) -> float:
        """Price one tail run so ``TAIL_COUNT`` of them pass every job's budget.

        The tail's test seconds alone then exceed the shard count times the
        budget, so no partition of any plan can fit, whatever its headroom.
        """
        return (wt.BUDGET_SECONDS * self.count) / TAIL_COUNT + 1

    def test_drift_gate_bites_on_too_many_unweighted_tests(self) -> None:
        # The drift error must name the unpriced tests whether or not the
        # planted plan also fits the budget (#9371): drift_report used to
        # raise the cut's spread error instead.
        unweighted = slow_runs(wt.MAX_DEFAULTED_TESTS + 1, prefix="TestNoWeight",
                               package=self.plan[-1].split("|")[0])
        drift = wt.drift_report([*self.plan, *unweighted], self.weights)
        self.assertEqual(len(drift.defaulted_tests), len(unweighted)
                         + len(wt.drift_report(self.plan, self.weights).defaulted_tests))
        self.assertRegex(" ".join(wt.drift_errors(drift)),
                         rf"planned tests have no weight \(limit {wt.MAX_DEFAULTED_TESTS}\)")
        newpkgs = [f"./internal/newpkg{index}|TestNew{index}Live" for index in range(
            wt.MAX_DEFAULTED_PACKAGES + 1)]
        package_drift = wt.drift_report([*self.plan, *newpkgs], self.weights)
        self.assertRegex(" ".join(wt.drift_errors(package_drift)),
                         rf"planned packages have no cost \(limit {wt.MAX_DEFAULTED_PACKAGES}\)")

    def test_drift_report_survives_a_plan_the_budget_rejects(self) -> None:
        # A PR that adds too many unpriced tests and also breaks the budget
        # must see both errors, not a cut error in place of the drift error.
        unweighted = slow_runs(wt.MAX_DEFAULTED_TESTS + 1, prefix="TestNoWeight",
                               package=self.plan[-1].split("|")[0])
        overflow = self.tail("TestOverflowTail")
        plan = [*self.plan, *unweighted, *overflow]
        weights = priced(self.weights, overflow, self.overflow_seconds())
        self.assertEqual(wt.budget_errors(plan, weights), [NO_FIT])
        drift = wt.drift_report(plan, weights)
        self.assertTrue(set(unweighted) <= set(drift.defaulted_tests))
        self.assertRegex(" ".join(wt.drift_errors(drift)),
                         rf"planned tests have no weight \(limit {wt.MAX_DEFAULTED_TESTS}\)")
        with self.assertRaisesRegex(ValueError, "duplicate v2 planned run"):
            wt.drift_report([*plan, plan[0]], weights)

    def test_seeded_slow_tests_overflow_a_count_split_but_not_the_weighted_one(self) -> None:
        # A fixed synthetic plan, not the live one (#9371): the live plan's
        # count split and its weighted headroom both move with every
        # enrollment and refit, so a live plant proves headroom, not the cut.
        # The live count split and live fit stay asserted by the unseeded
        # live-plan cases.
        plan = synthetic_plan()
        unseeded = synthetic_model()
        naive = self.jobs(plan, unseeded, naive_count_bounds(len(plan), v2.SHARD_COUNT))
        self.assertLessEqual(max(naive), wt.BUDGET_SECONDS, naive)
        seeds = slow_runs(SYNTHETIC_SEEDS, prefix="TestSyntheticSeed",
                          package=plan[0].split("|")[0])
        plan = [*seeds, *plan]
        weights = priced(unseeded, seeds, SYNTHETIC_SEED_SECONDS)
        naive = self.jobs(plan, weights, naive_count_bounds(len(plan), v2.SHARD_COUNT))
        self.assertGreater(max(naive), wt.JOB_CAP_SECONDS, naive)
        self.assertEqual(wt.budget_errors(plan, weights), [])
        jobs = self.jobs(plan, weights, wt.weighted_bounds(plan, weights))
        self.assertLessEqual(max(jobs), wt.BUDGET_SECONDS, jobs)

    def test_seeded_count_split_already_fails_the_gate_on_the_unchanged_plan(self) -> None:
        naive = self.jobs(self.plan, self.weights,
                          naive_count_bounds(len(self.plan), self.count))
        self.assertGreater(max(naive), wt.BUDGET_SECONDS, naive)

    def test_tests_appended_at_the_end_are_absorbed(self) -> None:
        # Size the tail to the tail shard's headroom on the live cut (#9371).
        # Past that headroom the live plan's package lumps decide whether any
        # cut meets the budget and the 15% spread, which is the live-plan
        # fit case's question, not this one's.
        base = self.cut(self.plan, self.weights)
        headroom = round((wt.BUDGET_SECONDS - base[-1]) * wt.UNIT)
        self.assertGreaterEqual(headroom, 0, base)
        appended = self.tail("TestAppendedTail")
        plan = [*self.plan, *appended]
        weights = priced(self.weights, appended, headroom // TAIL_COUNT / wt.UNIT)
        self.assertEqual(wt.budget_errors(plan, weights), [])
        jobs = self.cut(plan, weights)
        self.assertLessEqual(max(jobs), wt.BUDGET_SECONDS, jobs)
        overflow = priced(self.weights, appended, self.overflow_seconds())
        self.assertEqual(wt.budget_errors(plan, overflow), [NO_FIT])


if __name__ == "__main__":
    unittest.main()
