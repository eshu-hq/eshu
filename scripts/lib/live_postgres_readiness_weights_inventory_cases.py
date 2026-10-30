"""Cross-inventory and cache-era pooling contract for the v2 refresher (#9566).

A pool may hold runs of different test inventories when every run is a green
main push with one profile and one cache era. Each test is priced from the
runs that contain it: at the pool rule when it has at least QUANTILE_MIN_POOL
samples, otherwise it is left to `default_test_seconds` and named with its
sample count. Row residuals, cold and fixed come from every run, and the
output inventory is the newest run's. The cache era is read from the shard
jobs' "Restore Go build cache" step; a pool never mixes eras. The test script
`scripts/test-live-postgres-readiness-refresh-v2.py` imports and runs these
cases.
"""

from __future__ import annotations

import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import live_postgres_readiness_refresh_v2 as refresh
from live_postgres_readiness_weights_refresh_fixture import (
    FOUR_SHARD_ROWS, OTHER_SHA, PLAN, SHA, write_sample)

NEW = "./c|TestThree"
NEW_PLAN = [*PLAN, NEW]
NEW_ROWS = [*FOUR_SHARD_ROWS[:3], [("c", "TestTwo", 8), ("c", "TestThree", 2)]]


def _rows(plan: list[str]) -> dict[int, list[frozenset[str]]]:
    """Return the source rows the fixture shards close, for either inventory."""
    rows = {
        1: [frozenset([PLAN[0]])],
        2: [frozenset([PLAN[1]]), frozenset([PLAN[2]]), frozenset([PLAN[3]])],
        3: [frozenset([PLAN[4]]), frozenset([PLAN[5]])],
        4: [frozenset([PLAN[6]])],
    }
    if NEW in plan:
        rows[4] = [frozenset([PLAN[6]]), frozenset([NEW])]
    return rows


class InventoryPoolingTest(unittest.TestCase):
    """Mixed-inventory pools, the newest-run inventory, and the cache era."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.plans = {SHA: PLAN, OTHER_SHA: NEW_PLAN}
        for target, effect in (
                ("source_inventory", lambda _root, sha: self.plans[sha]),
                ("source_rows", lambda _root, sha, _plan, _count: _rows(self.plans[sha])),
                ("source_shard_count", lambda _root, _sha: 4)):
            patcher = mock.patch.object(refresh.provenance, target, side_effect=effect)
            patcher.start()
            self.addCleanup(patcher.stop)

    def sample(self, run_id: int, sha: str) -> Path:
        """Write one run on the old (SHA) or new (OTHER_SHA) inventory."""
        return write_sample(self.root, run_id, sha, NEW_ROWS if sha == OTHER_SHA else None)

    def mixed_pool(self) -> list[Path]:
        """Two old-inventory runs, then three runs that add ./c|TestThree."""
        return [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, OTHER_SHA),
                self.sample(103, OTHER_SHA), self.sample(104, OTHER_SHA)]

    def set_elapsed(self, source: Path, seconds: float) -> None:
        """Give ./a|TestOne another elapsed in shard 1; its 9 s row keeps a residual."""
        log = source / "shard1.log"
        log.write_text(log.read_text().replace("TestOne: PASS elapsed=1.0s",
                                               f"TestOne: PASS elapsed={seconds}s"))

    def set_restore(self, source: Path, conclusion: str | None,
                    shards: tuple[int, ...] = (1, 2, 3, 4), outcome: str | None = "hit",
                    setup_go_hit: bool = False) -> None:
        """Add the cache restore step, and its log section, to the named shard jobs of one run.

        outcome "hit" or "miss" writes the actions/cache/restore line the runner
        prints; None writes neither. setup_go_hit adds setup-go's own cache hit
        line after the restore step, which the era check must not read.
        """
        path = source / "jobs.json"
        payload = json.loads(path.read_text())
        for job in payload["jobs"]:
            if any(job["name"].endswith(f"shard ({shard}/4)") for shard in shards) and conclusion:
                job["steps"].insert(0, {"name": "Restore Go build cache", "status": "completed",
                                        "conclusion": conclusion})
        path.write_text(json.dumps(payload))
        stamp = "2026-10-26T00:00:00Z"
        line = {"hit": f"{stamp} Cache restored from key: Linux-X64-gobuild-abc",
                "miss": f"{stamp} Cache not found for input keys: Linux-X64-gobuildreadiness-abc, "
                        "Linux-X64-gobuild-", None: f"{stamp} Downloading cache"}[outcome]
        section = [f"{stamp} ##[group]Run actions/cache/restore@v4", line,
                   f"{stamp} ##[group]Run actions/setup-go@v6"]
        if setup_go_hit:
            section.append(f"{stamp} Cache restored from key: setup-go-Linux-x64-abc")
        for shard in shards:
            log = source / f"shard{shard}.log"
            text = log.read_text()
            anchor = f"{stamp} Setup go version spec"
            log.write_text(text.replace(anchor, "\n".join(section) + "\n" + anchor, 1))

    def test_mixed_inventory_prices_shared_tests_at_p75_and_newer_only_at_default(self) -> None:
        sources = self.mixed_pool()
        for source, seconds in zip(sources, (1.0, 1.0, 1.0, 5.0, 9.0)):
            self.set_elapsed(source, seconds)
        document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        tests = document["test_seconds"]
        self.assertEqual(tests["./a|TestOne"], 5.0)  # p75 of 1, 1, 1, 5, 9; the mean is 3.4
        self.assertNotIn(NEW, tests)
        self.assertEqual(set(tests), set(PLAN))
        self.assertEqual(document["source_runs"], ["100", "101", "102", "103", "104"])
        self.assertEqual(pool.defaulted, {NEW: 3})
        self.assertEqual(pool.newest, "104")
        self.assertIn("output inventory 8 tests from run 104", pool.inventory_line())
        self.assertIn("1 test priced at default_test_seconds (fewer than 4 samples): "
                      "./c|TestThree (3 samples)", pool.inventory_line())

    def test_newer_test_with_exactly_four_samples_is_priced(self) -> None:
        sources = [*self.mixed_pool(), self.sample(105, OTHER_SHA)]
        document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        self.assertEqual(document["test_seconds"][NEW], 1.0)
        self.assertEqual(set(document["test_seconds"]), set(NEW_PLAN))
        self.assertEqual(pool.defaulted, {})
        self.assertIn("0 tests priced at default_test_seconds (fewer than 4 samples): none",
                      pool.inventory_line())

    def test_tests_missing_from_the_newest_run_are_dropped(self) -> None:
        sources = [self.sample(100, OTHER_SHA), self.sample(101, OTHER_SHA),
                   self.sample(102, OTHER_SHA), self.sample(103, SHA), self.sample(104, SHA)]
        document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        self.assertEqual(set(document["test_seconds"]), set(PLAN))
        self.assertEqual(pool.defaulted, {})
        self.assertEqual(pool.newest, "104")

    def test_mixed_inventory_needs_a_quantile_pool(self) -> None:
        sources = [self.sample(100, SHA), self.sample(101, OTHER_SHA), self.sample(102, OTHER_SHA)]
        with self.assertRaisesRegex(ValueError, r"^source inventories differ in a 3-run pool; "
                                    r"mixing inventories needs at least 4 runs$"):
            refresh.fit_with_provenance(sources, self.root)

    def test_equal_inventory_pool_reports_one_inventory_and_no_defaults(self) -> None:
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        self.assertEqual(set(document["test_seconds"]), set(PLAN))
        self.assertFalse(pool.mixed)
        self.assertEqual(pool.defaulted, {})
        self.assertEqual(pool.era, "cold")

    def test_refuses_mixed_cache_eras_by_name(self) -> None:
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        self.set_restore(sources[1], "success")
        with self.assertRaisesRegex(ValueError, r"^source runs mix cache eras: 100 is cold "
                                    r"and 101 is warm; a pool holds one era$"):
            refresh.fit_with_provenance(sources, self.root)

    def test_warm_pool_is_accepted_and_named(self) -> None:
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        for source in sources:
            self.set_restore(source, "success")
        document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        self.assertEqual(pool.era, "warm")
        self.assertEqual(pool.era_line(), "weights refresh v2: cache era warm "
                         "(\"Restore Go build cache\" in 12 shard jobs: 12 hit, 0 missed; "
                         "0 shard jobs without the step)")
        self.assertEqual(set(document["test_seconds"]), set(PLAN))

    def test_cache_miss_is_cold_and_pools_with_runs_without_the_step(self) -> None:
        """A restore step that succeeds on a miss compiles like a cold run (#9566 ruling)."""
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        self.set_restore(sources[1], "success", outcome="miss")
        document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        self.assertEqual(pool.era, "cold")
        self.assertEqual(pool.era_line(), "weights refresh v2: cache era cold "
                         "(\"Restore Go build cache\" in 4 shard jobs: 0 hit, 4 missed; "
                         "8 shard jobs without the step)")
        self.assertEqual(set(document["test_seconds"]), set(PLAN))

    def test_refuses_a_run_whose_shards_differ_in_era(self) -> None:
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        self.set_restore(sources[0], "success", shards=(1, 2, 3))
        self.set_restore(sources[0], "success", shards=(4,), outcome="miss")
        with self.assertRaisesRegex(ValueError, r"^source 100 cache era differs across shards: "
                                    r"1=warm, 2=warm, 3=warm, 4=cold$"):
            refresh.fit_with_provenance(sources, self.root)

    def test_era_reads_only_the_restore_step_section(self) -> None:
        """setup-go's own cache hit later in the log does not make a missed shard warm."""
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        for source in sources:
            self.set_restore(source, "success", outcome="miss", setup_go_hit=True)
        _document, _images, _cold, pool = refresh.fit_with_provenance(sources, self.root)
        self.assertEqual(pool.era, "cold")

    def test_refuses_failed_restore_and_unknown_outcome(self) -> None:
        sources = [self.sample(100, SHA), self.sample(101, SHA), self.sample(102, SHA)]
        self.set_restore(sources[0], "failure")
        with self.assertRaisesRegex(ValueError, r"^source 100 shard 1 cache restore step "
                                    r"did not succeed \(failure\); its cache era is unknown$"):
            refresh.fit_with_provenance(sources, self.root)
        sources = [self.sample(200, SHA), self.sample(201, SHA), self.sample(202, SHA)]
        self.set_restore(sources[0], "success", outcome=None)
        with self.assertRaisesRegex(ValueError, r"^source 200 shard 1 cache restore log shows "
                                    r"neither a hit nor a miss; its cache era is unknown$"):
            refresh.fit_with_provenance(sources, self.root)

if __name__ == "__main__":
    unittest.main()
