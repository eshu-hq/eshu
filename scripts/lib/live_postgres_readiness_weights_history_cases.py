"""Five- and six-shard source history for the v2 weights refresher (#9519, #9473).

A v2 source is legal when its own-SHA weights file says 5 or 6 shards and
passes every other schema check; its shard jobs must report that count. A pool
may mix five- and six-shard runs. The refresher still writes a six-shard file,
and the committed-file loader still accepts only six. Run from
`scripts/test-live-postgres-readiness-refresh-v2.py`.
"""

from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import live_postgres_readiness_refresh_v2 as refresh
import live_postgres_readiness_v2 as v2
import live_postgres_readiness_weights_refresh as cli_refresh
from live_postgres_readiness_weights_refresh_fixture import write_sample

PLAN = [f"./{package}|Test{name}" for package, name in (
    ("a", "One"), ("a", "Two"), ("b", "One"), ("b", "Two"), ("b", "Three"),
    ("c", "One"), ("c", "Two"), ("d", "One"))]
FIVE_SHA, SIX_SHA = "5" * 40, "6" * 40
# Five-shard runs carry the only repeat row (./b|TestTwo after ./b|TestOne).
FIVE = [[("a", "TestOne", 8)], [("a", "TestTwo", 8), ("b", "TestOne", 5), ("b", "TestTwo", 2)],
        [("b", "TestThree", 8), ("c", "TestOne", 5)], [("c", "TestTwo", 8)], [("d", "TestOne", 8)]]
SIX = [[("a", "TestOne", 8)], [("a", "TestTwo", 8), ("b", "TestOne", 5)], [("b", "TestTwo", 8)],
       [("b", "TestThree", 8), ("c", "TestOne", 5)], [("c", "TestTwo", 8)], [("d", "TestOne", 8)]]
WEIGHTS_V2 = "specs/live-postgres-readiness-weights.v2.json"


def _members(rows: list[list[tuple[str, str, int]]]) -> dict[int, list[frozenset[str]]]:
    """Return the source-planned row membership for a synthetic cut."""
    return {number: [frozenset([f"./{package}|{test}"]) for package, test, _ in shard]
            for number, shard in enumerate(rows, 1)}


def own_file(count: object, plan: list[str] = PLAN) -> dict[str, object]:
    """Return a valid own-SHA v2 weights document with the given shard_count."""
    return {"schema_version": 2, "source_runs": ["1", "2"], "shard_count": count,
            "fixed_job_seconds": 1.0, "cold_cache_seconds": 1.0,
            "default_test_seconds": 1.0, "default_package_first_seconds": 1.0,
            "default_package_repeat_seconds": 1.0,
            "package_first_seconds": {"./a": 1.0}, "package_repeat_seconds": {"./a": 1.0},
            "test_seconds": {run: 1.0 for run in plan}}


class SourceHistoryTest(unittest.TestCase):
    """Own-SHA shard counts, mixed pools, and the unchanged committed loader."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.counts = {FIVE_SHA: 5, SIX_SHA: 6}
        self.plan = PLAN
        self.cuts = {5: FIVE, 6: SIX}
        self.planned: list[int] = []
        self.real_source_rows = refresh.provenance.source_rows
        patches = [
            mock.patch.object(refresh.provenance, "_source_text", side_effect=self.text),
            mock.patch.object(refresh.provenance.subprocess, "run",
                              return_value=subprocess.CompletedProcess([], 0)),
            mock.patch.object(refresh.provenance, "source_inventory",
                              side_effect=lambda _root, _sha: self.plan),
            mock.patch.object(refresh.provenance, "source_rows", side_effect=self.rows),
        ]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def text(self, _root: Path, sha: str, path: str) -> str:
        """Serve the own-SHA workflow and v2 weights file of a synthetic source."""
        if path == WEIGHTS_V2:
            return json.dumps(own_file(self.counts[sha], self.plan))
        if path == refresh.provenance.WORKFLOW:
            return ""
        raise ValueError(f"source {sha} lacks {path}")

    def rows(self, _root: Path, _sha: str, _plan: list[str],
             count: int) -> dict[int, list[frozenset[str]]]:
        """Record the count each source is planned at and return its cut."""
        self.planned.append(count)
        return _members(self.cuts[count])

    def pool(self, *shapes: int) -> list[Path]:
        """Write one source per shape (5 or 6 shards), run IDs from 100."""
        return [write_sample(self.root, 100 + index, FIVE_SHA if shape == 5 else SIX_SHA,
                             self.cuts[shape])
                for index, shape in enumerate(shapes)]

    def test_five_shard_source_is_accepted_and_output_is_six_shard(self) -> None:
        document = refresh.build_document(self.pool(5, 5), self.root)
        self.assertEqual(document["shard_count"], v2.SHARD_COUNT)
        self.assertEqual(self.planned, [5, 5])
        # Purity: the widening changes no price; a two-run pool is the plain mean.
        self.assertEqual((document["cold_cache_seconds"], document["fixed_job_seconds"],
                          document["package_first_seconds"], document["package_repeat_seconds"]),
                         (3.0, 10.0, {"./a": 5.0, "./b": 5.0, "./c": 5.0, "./d": 5.0},
                          {"./b": 2.0}))
        self.assertEqual(document["test_seconds"], {run: 1.0 for run in sorted(PLAN)})

    def test_mixed_pool_takes_repeat_rows_from_five_shard_runs(self) -> None:
        sources = self.pool(5, 6, 5, 6)
        document = refresh.build_document(sources, self.root)
        self.assertEqual(self.planned, [5, 6, 5, 6])
        self.assertEqual(document["shard_count"], 6)
        self.assertEqual(document["package_repeat_seconds"], {"./b": 2.0})
        output = self.root / "weights.v2.json"
        argv = ["refresh", "--schema-version", "2", "--out", str(output)]
        argv.extend(f"--sample={source.name}={source}" for source in sources)
        stdout = io.StringIO()
        with mock.patch.object(cli_refresh.sys, "argv", argv), \
                contextlib.redirect_stdout(stdout):
            self.assertEqual(cli_refresh.main(), 0)
        self.assertIn("over a 4-run pool 100 (5 shards), 101 (6 shards), "
                      "102 (5 shards), 103 (6 shards);", stdout.getvalue())

    def test_six_shard_pool_without_repeat_rows_still_refuses(self) -> None:
        with self.assertRaisesRegex(ValueError, "no measured repeat row category"):
            refresh.build_document(self.pool(6, 6, 6, 6), self.root)

    def test_own_file_count_outside_five_or_six_is_rejected(self) -> None:
        for count in (7, 3, 4, "5", True):
            self.counts[SIX_SHA] = count
            with self.subTest(count=count), self.assertRaisesRegex(
                    ValueError, "v2 source shard_count must be 5 or 6"):
                refresh.build_document(self.pool(6, 6), self.root)
            for path in self.root.iterdir():
                for item in path.iterdir():
                    item.unlink()
                path.rmdir()

    def test_log_count_differing_from_own_file_is_rejected_and_named(self) -> None:
        self.counts[SIX_SHA] = 5
        with self.assertRaisesRegex(ValueError, "reports 6 shards but its own-SHA "
                                    "weights file says 5"):
            refresh.build_document(self.pool(6, 5), self.root)

    def test_source_rows_plan_five_shards_from_a_five_shard_own_file(self) -> None:
        rows = self.real_source_rows(self.root, FIVE_SHA, PLAN, 5)
        self.assertEqual(list(rows), [1, 2, 3, 4, 5])
        self.assertEqual(sorted(run for shard in rows.values() for row in shard for run in row),
                         sorted(PLAN))

    def test_committed_loader_still_requires_six(self) -> None:
        self.assertEqual(v2.weights_from_json(own_file(6)).shard_count, 6)
        for count in (5, 7):
            with self.subTest(count=count), self.assertRaisesRegex(
                    ValueError, "v2 shard_count must be 6"):
                v2.weights_from_json(own_file(count))
        self.assertEqual(v2.weights_from_json(own_file(5), history=True).shard_count, 5)
        with self.assertRaisesRegex(ValueError, "v2 weights schema mismatch"):
            v2.weights_from_json({**own_file(5), "extra": 1}, history=True)

    def test_pool_of_four_keeps_cold_fixed_and_defaults_on_their_rules(self) -> None:
        """p75 reaches only the per-test, first-use and repeat tables (#9519 review P2-1).

        Every table here has a p75 that differs from its mean, so a leak of the
        quantile into cold, fixed or a default changes an asserted value.
        """
        self.plan = PLAN + ["./d|TestTwo", "./e|TestOne"]
        self.cuts[5] = [[("a", "TestOne", 8)],
                        [("a", "TestTwo", 8), ("b", "TestOne", 5), ("b", "TestTwo", 2)],
                        [("b", "TestThree", 8), ("c", "TestOne", 5)],
                        [("c", "TestTwo", 8), ("d", "TestOne", 6), ("d", "TestTwo", 7)],
                        [("e", "TestOne", 16)]]
        sources = self.pool(5, 5, 5, 5)
        for source in sources:  # ./e|TestOne takes 9 s, so test prices are uneven.
            path = source / "shard5.log"
            path.write_text(path.read_text().replace("elapsed=1.0s", "elapsed=9.0s"))
        jobs = sources[0] / "jobs.json"  # One of 20 shard jobs has 20 s overhead, not 10 s.
        jobs.write_text(jobs.read_text().replace("2026-10-26T00:00:25Z",
                                                 "2026-10-26T00:00:15Z", 1))
        document = refresh.build_document(sources, self.root)
        self.assertEqual(document["package_first_seconds"],
                         {"./a": 5.0, "./b": 5.0, "./c": 5.0, "./d": 6.0, "./e": 5.0})
        self.assertEqual(document["package_repeat_seconds"], {"./b": 2.0, "./d": 7.0})
        # Each value follows its committed rule; at p75, fixed and the three
        # defaults would be 10.0, 1.0, 5.0 and 5.8.
        self.assertEqual((document["cold_cache_seconds"], document["fixed_job_seconds"],
                          document["default_test_seconds"],
                          document["default_package_first_seconds"],
                          document["default_package_repeat_seconds"]),
                         (3.0, 10.5, 1.8, 5.2, 4.5))

    def test_source_rows_refuses_an_own_file_count_other_than_requested(self) -> None:
        with self.assertRaisesRegex(ValueError, "weights file says 5 shards, not 6"):
            self.real_source_rows(self.root, FIVE_SHA, PLAN, 6)
