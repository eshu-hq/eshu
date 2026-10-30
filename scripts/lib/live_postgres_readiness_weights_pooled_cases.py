"""Pooled-quantile pricing contract for the v2 weights refresher (#9519).

A pool of four or more equal-inventory runs prices per-test, first-use and
repeat samples at the linear inclusive p75; a pool of three keeps the mean,
and a pair is refused before pricing, so two fast draws cannot set the prices
(#9470). The end-to-end pool fixture lives in
`scripts/test-live-postgres-readiness-refresh-v2.py`, which imports and runs
these cases too.
"""

from __future__ import annotations

import unittest

import live_postgres_readiness_refresh_v2 as refresh

LABEL = "./a|TestOne"


class PooledPricingTest(unittest.TestCase):
    """Price-rule selection, interpolation pins, and the provenance line."""

    def price(self, values: list[float], runs: int, label: str = LABEL,
              positive: bool = False) -> float:
        """Price values under the rule a pool of the given run count selects."""
        rule = refresh.pricing_rule([str(100 + index) for index in range(runs)])
        return refresh._price_tenth(values, label, rule, positive=positive)

    def test_one_fast_three_normal_prices_at_p75_not_mean(self) -> None:
        self.assertEqual(self.price([10.0, 20.0, 20.0, 20.0], runs=4), 20.0)
        self.assertEqual(self.price([20.0, 10.0, 20.0, 20.0], runs=4), 20.0)

    def test_pricing_rule_refuses_a_pair(self) -> None:
        """The factory itself refuses a pair, not just the fit entry (#9470)."""
        with self.assertRaisesRegex(ValueError, "at least three"):
            refresh.pricing_rule(["100", "101"])

    def test_three_run_pool_keeps_the_mean(self) -> None:
        self.assertFalse(refresh.pricing_rule(["100", "101", "102"]).quantile)
        self.assertTrue(refresh.pricing_rule(["100", "101", "102", "103"]).quantile)
        self.assertEqual(self.price([10.0, 20.0, 20.0], runs=3), 16.7)
        self.assertEqual(self.price([10.0, 20.0, 20.0, 20.0], runs=3), 17.5)

    def test_p75_is_linear_inclusive_on_four_and_six_samples(self) -> None:
        # Inclusive position (n - 1) * 0.75: 2.25 on four samples, 3.75 on six.
        # The exclusive method gives 7.0 and 20.0; p90 gives 6.8 and 24.0.
        self.assertEqual(self.price([8.0, 1.0, 4.0, 2.0], runs=4), 5.0)
        self.assertEqual(self.price([32.0, 1.0, 16.0, 2.0, 8.0, 4.0], runs=6), 14.0)
        self.assertEqual(self.price([1.0, 2.0], runs=5), 1.8)

    def test_quantile_mode_prices_a_single_sample_at_itself(self) -> None:
        self.assertEqual(self.price([7.3], runs=4), 7.3)

    def test_quantile_mode_keeps_validity_checks(self) -> None:
        with self.assertRaisesRegex(ValueError, f"no measured {LABEL}"):
            self.price([], runs=4)
        with self.assertRaisesRegex(ValueError, "measured repeat ./a is not positive"):
            self.price([0.0, 0.0, 0.0, 0.01], runs=4, label="repeat ./a", positive=True)
        with self.assertRaisesRegex(ValueError, "measured first-use ./a is not positive"):
            self.price([-3.0, -2.0, -1.0, -1.0], runs=4, label="first-use ./a")

    def test_pricing_line_names_quantile_pool_and_size(self) -> None:
        self.assertEqual(
            refresh.pricing_rule(["100", "101", "102", "103"]).line(),
            "weights refresh v2: pricing p75 (linear inclusive) of per-test, "
            "first-use and repeat samples over a 4-run pool 100, 101, 102, 103; "
            "cold, fixed and defaults keep their committed rules")
        self.assertEqual(
            refresh.pricing_rule(["100", "101", "102"]).line(),
            "weights refresh v2: pricing mean of per-test, first-use and repeat "
            "samples over a 3-run pool 100, 101, 102 (p75 needs at least 4 runs); "
            "cold, fixed and defaults keep their committed rules")
        self.assertIn("over a 3-run pool 100 (5 shards), 101 (6 shards), 102 (4 shards) "
                      "(p75 needs",
                      refresh.pricing_rule(["100", "101", "102"],
                                           {"101": 6, "100": 5, "102": 4}).line())

    def test_shard_counts_pair_by_run_id_not_position(self) -> None:
        # Review P3-4: a reordered source list must not relabel the counts.
        self.assertIn("over a 3-run pool 102 (4 shards), 101 (6 shards), 100 (5 shards) "
                      "(p75 needs",
                      refresh.pricing_rule(["102", "101", "100"],
                                           {"100": 5, "101": 6, "102": 4}).line())
        for counts in ({"100": 5}, {"100": 5, "101": 6},
                       {"100": 5, "101": 6, "102": 4, "103": 4}):
            with self.subTest(counts=counts), self.assertRaisesRegex(
                    ValueError, "pricing shard counts do not match the source pool"):
                refresh.pricing_rule(["100", "101", "102"], counts)
