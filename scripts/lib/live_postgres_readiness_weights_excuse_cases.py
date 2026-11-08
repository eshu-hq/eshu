"""Workflow-excused skips for the v2 readiness weights refresher (#10388, #10400).

An excused skip parses only when every shard agrees on the excuse and every
name is an enrolled test; a 0/0 package row parses only when the excuse fully
covers that package in its shard. Run from
`scripts/test-live-postgres-readiness-refresh-v2.py`.
"""

from __future__ import annotations

import statistics
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import live_postgres_readiness_refresh_v2 as refresh
from live_postgres_readiness_weights_refresh_fixture import (
    OTHER_SHA, PLAN, SHA, shard_log as _log, write_sample as _sample)


class ExcuseCases(unittest.TestCase):
    """Excused skips parse fail-closed and price from the runs that ran them."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.sources = [_sample(self.root, 100, SHA), _sample(self.root, 101, OTHER_SHA),
                        _sample(self.root, 102, SHA)]
        inventory = mock.patch.object(refresh.provenance, "source_inventory",
                                      side_effect=lambda _root, _sha: PLAN)
        inventory.start()
        self.addCleanup(inventory.stop)
        topology = mock.patch.object(refresh.provenance, "source_shard_count", return_value=4)
        topology.start()
        self.addCleanup(topology.stop)
        self.expected_rows = {
            1: [frozenset([PLAN[0]])],
            2: [frozenset([PLAN[1]]), frozenset([PLAN[2]]), frozenset([PLAN[3]])],
            3: [frozenset([PLAN[4]]), frozenset([PLAN[5]])],
            4: [frozenset([PLAN[6]])],
        }
        rows = mock.patch.object(refresh.provenance, "source_rows",
                                 side_effect=lambda _root, _sha, _plan, _count: self.expected_rows)
        rows.start()
        self.addCleanup(rows.stop)

    def fit(self) -> dict[str, object]:
        """Fit the three complete synthetic workflows."""
        return refresh.build_document(self.sources, self.root)

    def _excuse_test_three(self, sources, name="TestThree"):
        """Drop TestThree's shard3 row, keep its summary count, name the excuse."""
        for source in sources:
            for log in source.glob("shard*.log"):
                log.write_text(log.read_text().replace("--report readiness-shard-report.json", "--report readiness-shard-report.json\n2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP: " + name))
            shard = source / "shard3.log"
            shard.write_text(shard.read_text().replace("2026-10-26T00:00:09Z TestThree: PASS elapsed=1.0s\n", "").replace(
                "./b 1/1 PASS", "./b 0/0 PASS"))

    def test_accepts_excused_skip_and_prices_it_from_history(self) -> None:
        """An excused skip parses; its price comes from the runs that ran it."""
        self._excuse_test_three(self.sources[:1])
        self.assertEqual(self.fit()["test_seconds"]["./b|TestThree"], 1.0)

    def test_uniform_pool_defaults_unsampled_excused_test(self) -> None:
        """A newest-inventory test with no samples defaults instead of failing."""
        pool = refresh.pool_rules.PoolProvenance()
        pool.add("100", ["./a|TestOne", "./a|TestTwo"], "cold")
        pool.price_tests({"./a|TestOne": [1.0, 1.0, 1.0]}, lambda samples, run: round(statistics.mean(samples), 1), 4, ["./a|TestTwo"])
        self.assertEqual(pool.defaulted, {"./a|TestTwo": 0})

    def test_rejects_unexcused_missing_and_fail_rows(self) -> None:
        """A missing row without excuse, or a FAIL row, still fails the log."""
        path = self.sources[0] / "shard3.log"
        path.write_text(path.read_text().replace("TestThree: PASS", "TestThree: FAIL"))
        with self.assertRaises(ValueError):
            self.fit()
        path.write_text(path.read_text().replace("2026-10-26T00:00:09Z TestThree: FAIL elapsed=1.0s\n", "").replace("2026-10-26T00:00:09Z live-postgres-readiness: ./b 1/1 PASS\n", ""))
        with self.assertRaisesRegex(ValueError, "does not cover all rows"):
            self.fit()

    def test_rejects_unknown_excuse_name(self) -> None:
        """An excuse naming a test outside the plan fails closed."""
        self._excuse_test_three(self.sources[:1], name="TestNope")
        with self.assertRaisesRegex(ValueError, "unknown test"):
            self.fit()

    def test_rejects_printed_row_for_excused_proof(self) -> None:
        """An excuse contradicting a printed PASS row fails closed."""
        log = _log(3, [("b", "TestThree", 8), ("c", "TestOne", 5)], 100)
        with self.assertRaisesRegex(ValueError, "excused"):
            refresh._parse_shard(log, 3, 4, frozenset(["TestThree"]))

    def test_rejects_unscoped_zero_row_with_excuse_present(self) -> None:
        """A 0/0 row for a package outside the excuse fails closed at fit time."""
        self._excuse_test_three(self.sources[:1])
        path = self.sources[0] / "shard3.log"
        summary = "2026-10-26T00:00:15Z readiness shard 3/4:"
        injected = ("2026-10-26T00:00:09Z live-postgres-readiness: ./zzz 0/0 PASS\n"
                    + summary)
        path.write_text(path.read_text().replace(summary, injected))
        with self.assertRaisesRegex(ValueError, "invalid package PASS row"):
            self.fit()

    def test_parse_shard_rejects_unscoped_zero_row(self) -> None:
        """A 0/0 row for a package outside the shard excuse fails closed."""
        log = _log(3, [("b", "TestThree", 8), ("c", "TestOne", 5)], 100)
        log = log.replace("2026-10-26T00:00:09Z TestThree: PASS elapsed=1.0s\n", "").replace(
            "./b 1/1 PASS", "./b 0/0 PASS")
        summary = "2026-10-26T00:00:15Z readiness shard 3/4:"
        log = log.replace(summary, "2026-10-26T00:00:09Z live-postgres-readiness: ./zzz 0/0 PASS\n"
                          + summary)
        with self.assertRaisesRegex(ValueError, "invalid package PASS row"):
            refresh._parse_shard(log, 3, 4, frozenset({"TestThree"}))

    def test_parse_shard_scopes_zero_row_to_covered_package(self) -> None:
        """A 0/0 row parses only when its package is fully covered by the excuse."""
        log = _log(3, [("b", "TestThree", 8), ("c", "TestOne", 5)], 100)
        log = log.replace("2026-10-26T00:00:09Z TestThree: PASS elapsed=1.0s\n", "").replace(
            "./b 1/1 PASS", "./b 0/0 PASS")
        observed, _, _, _ = refresh._parse_shard(
            log, 3, 4, frozenset({"TestThree"}), frozenset({"./b"}))
        self.assertEqual(observed, ["./c|TestOne"])

    def test_parse_shard_rejects_zero_row_without_excuse(self) -> None:
        """A 0/0 row with no excuse is refused exactly as before."""
        log = _log(3, [("b", "TestThree", 8), ("c", "TestOne", 5)], 100)
        summary = "2026-10-26T00:00:15Z readiness shard 3/4:"
        log = log.replace(summary, "2026-10-26T00:00:09Z live-postgres-readiness: ./zzz 0/0 PASS\n"
                          + summary)
        with self.assertRaisesRegex(ValueError, "invalid package PASS row"):
            refresh._parse_shard(log, 3, 4)


if __name__ == "__main__":
    unittest.main()
