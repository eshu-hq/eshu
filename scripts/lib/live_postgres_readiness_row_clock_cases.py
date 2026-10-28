"""Row admission prices what the ``go test -timeout`` clock times (#9348).

One runner row is one ``go test -json -count=1 -timeout=15m`` invocation.
``go help testflag`` says the timeout panics "if a test binary runs longer
than duration d", so compile and link happen before the clock starts. The
row budget therefore charges a row its test prices plus the package repeat
residual (link, start, TestMain, teardown) and never the cold-cache or
first-use build cost. The job score still charges both build costs once, so
these cases also pin that the job score uses the row count of the new
admission.
"""

from __future__ import annotations

from dataclasses import replace
from pathlib import Path
import re
import unittest

import live_postgres_readiness_balance_cases as balance
import live_postgres_readiness_row_split as row_split
import live_postgres_readiness_v2 as v2
import live_postgres_readiness_weights as wt

RUNNER = Path(__file__).parents[1] / "run-live-postgres-readiness-tests.sh"
GUARD = "./internal/storage/postgres|TestProjectorClaimMarkedGuardContention"


def model(tests: dict[str, float], packages: list[str]) -> v2.WeightsV2:
    """Return a v2 model with a 500 s first use and a 10 s repeat residual."""
    return v2.weights_from_json({
        "schema_version": 2, "source_runs": ["11", "12"], "shard_count": 5,
        "fixed_job_seconds": 0.0, "cold_cache_seconds": 10.0,
        "default_test_seconds": 1.0, "default_package_first_seconds": 500.0,
        "default_package_repeat_seconds": 10.0,
        "package_first_seconds": {package: 500.0 for package in packages},
        "package_repeat_seconds": {package: 10.0 for package in packages},
        "test_seconds": tests,
    })


def lone_tests(seconds: float) -> tuple[list[str], v2.WeightsV2]:
    """Five one-test packages, one per shard, each test priced at ``seconds``."""
    runs = [f"./p{index}|TestLone{index}Live" for index in range(5)]
    packages = [run.split("|", 1)[0] for run in runs]
    return runs, model({run: seconds for run in runs}, packages)


class RowClockAdmissionTest(unittest.TestCase):
    """Pin the clock-timed row price, the 675 s budget, and the job score."""

    def test_row_budget_is_75_percent_of_the_go_test_timeout(self) -> None:
        text = RUNNER.read_text(encoding="utf-8")
        clocks = re.findall(r"-timeout=(\d+)m", text)
        self.assertEqual(clocks, ["15"], "runner go test clock moved")
        self.assertEqual(row_split.ROW_BUDGET_TENTHS, 675 * wt.UNIT)
        self.assertEqual(row_split.ROW_BUDGET_TENTHS * 100, 75 * 15 * 60 * wt.UNIT)
        self.assertEqual(v2.ROW_BUDGET, row_split.ROW_BUDGET_TENTHS)

    def test_row_price_is_tests_plus_repeat_without_build_cost(self) -> None:
        # Old admission: 10 cold + max(500 first, 10 repeat) + 300 = 810 s,
        # over the budget although the clock only times about 310 s.
        runs, weights = lone_tests(300.0)
        self.assertEqual(v2.row_cost_tenths([runs[0]], weights), 3100)
        self.assertEqual(wt.budget_errors(runs, weights), [])
        self.assertEqual(wt.weighted_bounds(runs, weights), [0, 1, 2, 3, 4, 5])

    def test_lone_test_over_the_row_budget_still_trips(self) -> None:
        runs, weights = lone_tests(665.0)
        self.assertEqual(v2.row_cost_tenths([runs[0]], weights), 6750)
        self.assertEqual(wt.budget_errors(runs, weights), [])
        runs, weights = lone_tests(670.0)
        self.assertEqual(v2.row_cost_tenths([runs[0]], weights), 6800)
        self.assertEqual(wt.budget_errors(runs, weights),
                         ["v2 runner row exceeds 675 s budget"])

    def test_job_score_charges_build_once_with_the_new_row_count(self) -> None:
        # Four 150 s tests fit one row at 610 s; the old admission charged
        # 660 s per test and cut four rows, adding three repeat prices. The
        # 165 s and 167 s cases sit on the 675 s boundary (cold 10, repeat 10):
        # 660 + 10 = 670 fits one row, 668 + 10 = 678 does not, so the job
        # score row count cannot drift from the runner's split_rows.
        cases = ((4, 150.0, 1), (6, 150.0, 2), (4, 165.0, 1), (4, 167.0, 2))
        for count, seconds, rows_expected in cases:
            with self.subTest(count=count, seconds=seconds):
                runs = [f"./p|Test{index}Live" for index in range(count)]
                weights = model({run: seconds for run in runs}, ["./p"])
                rows = v2.split_rows(runs, weights)
                self.assertEqual(len(rows), rows_expected)
                self.assertEqual([run for row in rows for run in row], runs)
                self.assertTrue(all(v2.row_cost_tenths(row, weights) <= v2.ROW_BUDGET
                                    for row in rows))
                self.assertEqual(v2.Scorer(runs, weights).row_count(0, count),
                                 rows_expected)
                hand = (0 + 100 + round(count * seconds * 10) + 5000
                        + (rows_expected - 1) * 100)
                self.assertEqual(v2.segment_cost_tenths(runs, weights), hand)

    def test_planted_800_s_lone_test_trips_the_live_gate(self) -> None:
        weights = wt.load_weights()
        self.assertIsInstance(weights, v2.WeightsV2)
        plan = balance.live_plan()
        self.assertIn(GUARD, plan)
        planted = replace(weights, test={**weights.test, GUARD: 8000})
        self.assertGreater(v2.row_cost_tenths([GUARD], planted), v2.ROW_BUDGET)
        self.assertTrue(wt.budget_errors(plan, planted))

    def test_fingerprint_names_the_clock_model(self) -> None:
        _, weights = lone_tests(1.0)
        fingerprint = weights.fingerprint()
        self.assertIn('"midpoint-v2-clock-tests-plus-repeat-675"', fingerprint)
        self.assertNotIn("cold-plus-max-first-repeat-every-row", fingerprint)


if __name__ == "__main__":
    unittest.main()
