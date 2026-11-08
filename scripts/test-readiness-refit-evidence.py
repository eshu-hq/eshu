#!/usr/bin/env python3
"""Contract tests for the readiness refit evidence driver (#10388).

Run from the repository root: ``python3 scripts/test-readiness-refit-evidence.py``.
"""

from __future__ import annotations

import json
import sys
import tempfile
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from typing import Any

sys.path.insert(0, str(Path(__file__).parent / "dev" / "readiness-refit-evidence"))
sys.path.insert(0, str(Path(__file__).parent / "lib"))
import refit_evidence as driver  # noqa: E402
import live_postgres_readiness_refresh_v2_provenance as provenance  # noqa: E402


def _write_run(root: Path, jobs: list[dict[str, Any]],
               run: dict[str, Any] | None = None) -> Path:
    """Writes a minimal run directory with jobs.json and optional run.json."""
    run_dir = root / "42420000001"
    run_dir.mkdir()
    (run_dir / "jobs.json").write_text(json.dumps({"jobs": jobs}))
    if run is not None:
        (run_dir / "run.json").write_text(json.dumps(run))
    (run_dir / "shard1.log").write_text("log")
    return run_dir


def _job(sha: str) -> dict[str, Any]:
    """Returns a minimal jobs.json entry pinned to one head SHA."""
    return {"head_sha": sha, "name": "live-postgres-readiness (postgres18) shard (1/1)"}


def _old_lib() -> SimpleNamespace:
    """Returns a refresher stub predating excused-skip support (#10388)."""
    return SimpleNamespace(
        provenance=SimpleNamespace(
            source_inventory=lambda root, sha: ["p|t1"],
            source_rows=lambda root, sha, plan, count: {1: [frozenset({"p|t1"})]}),
        _parse_shard=lambda log, number, total: ([], [], ((), ""), []))


class RunHeadShaCases(unittest.TestCase):
    """The driver resolves a run's head SHA without requiring run.json."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_prefers_run_json(self) -> None:
        """run.json wins when both metadata files name a SHA."""
        run_dir = _write_run(self.root, [_job("b" * 40)], {"head_sha": "a" * 40})
        self.assertEqual(driver.run_head_sha(run_dir), "a" * 40)

    def test_falls_back_to_unanimous_jobs(self) -> None:
        """Unanimous jobs.json head SHAs identify runs archived without run.json."""
        run_dir = _write_run(self.root, [_job("c" * 40), _job("c" * 40)])
        self.assertEqual(driver.run_head_sha(run_dir), "c" * 40)

    def test_rejects_disagreeing_jobs(self) -> None:
        """Jobs disagreeing on head SHA fail closed instead of guessing."""
        run_dir = _write_run(self.root, [_job("c" * 40), _job("d" * 40)])
        with self.assertRaisesRegex(ValueError, "disagree on head_sha"):
            driver.run_head_sha(run_dir)

    def test_rejects_empty_jobs(self) -> None:
        """A jobs listing with no jobs cannot identify the run."""
        run_dir = _write_run(self.root, [])
        with self.assertRaisesRegex(ValueError, "head_sha"):
            driver.run_head_sha(run_dir)

    def test_rejects_run_json_without_head_sha(self) -> None:
        """A run.json without head_sha fails closed naming the run, not KeyError."""
        run_dir = _write_run(self.root, [_job("c" * 40)], {"conclusion": "success"})
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)

    def test_rejects_run_json_list_payload(self) -> None:
        """A non-dict run.json fails closed naming the run, not TypeError."""
        run_dir = self.root / "42420000001"
        run_dir.mkdir()
        (run_dir / "jobs.json").write_text(json.dumps({"jobs": [_job("c" * 40)]}))
        (run_dir / "run.json").write_text("[1, 2]")
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)

    def test_rejects_empty_head_sha(self) -> None:
        """An empty head_sha is malformed, not a usable SHA."""
        run_dir = _write_run(self.root, [_job("c" * 40)], {"head_sha": ""})
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)

    def test_rejects_run_json_malformed_text(self) -> None:
        """Unparseable run.json fails closed naming the run."""
        run_dir = _write_run(self.root, [_job("c" * 40)], {"head_sha": "a" * 40})
        (run_dir / "run.json").write_text("{nope")
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)

    def test_rejects_jobs_list_payload(self) -> None:
        """A non-dict jobs.json fails closed naming the run, not TypeError."""
        run_dir = self.root / "42420000001"
        run_dir.mkdir()
        (run_dir / "jobs.json").write_text(json.dumps([_job("c" * 40)]))
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)

    def test_rejects_non_dict_job_entry(self) -> None:
        """A non-dict jobs.json entry fails closed naming the run."""
        run_dir = self.root / "42420000001"
        run_dir.mkdir()
        (run_dir / "jobs.json").write_text(
            json.dumps({"jobs": [_job("c" * 40), "not-a-job"]}))
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)

    def test_rejects_missing_jobs_json(self) -> None:
        """A run with neither metadata file fails closed naming the run."""
        run_dir = self.root / "42420000001"
        run_dir.mkdir()
        with self.assertRaisesRegex(ValueError, run_dir.name):
            driver.run_head_sha(run_dir)


class PoolcheckContractCases(unittest.TestCase):
    """poolcheck refuses runs without run.json instead of crashing."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_rejects_run_without_run_json(self) -> None:
        """A run dir without run.json fails closed with a named reason."""
        from argparse import Namespace
        run_dir = self.root / "42420000001"
        run_dir.mkdir()
        with self.assertRaisesRegex(ValueError, "requires run.json"):
            driver.cmd_poolcheck(Namespace(runs=[run_dir]))


class OldLibCompatCases(unittest.TestCase):
    """The driver evaluates refresher commits that predate excuse support."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.real_r2 = driver.r2
        self.addCleanup(setattr, driver, "r2", self.real_r2)

    def test_shard_excuses_empty_without_excuse_api(self) -> None:
        """An old evaluated lib yields empty excuses and no git reads."""
        driver.r2 = _old_lib()
        run_dir = _write_run(self.root, [_job("c" * 40)], {"head_sha": "c" * 40})
        self.assertEqual(driver.shard_excuses(run_dir, 1), {1: frozenset()})

    def test_parse_shard_uses_old_signature(self) -> None:
        """An old evaluated parser is called without the excuses argument."""
        seen: list[tuple] = []
        lib = _old_lib()

        def fake(log: str, number: int, total: int) -> tuple:
            seen.append((log, number, total))
            return ([], [], ((), ""), [])

        lib._parse_shard = fake
        driver.r2 = lib
        driver.parse_shard("log", 1, 1, frozenset())
        self.assertEqual(seen, [("log", 1, 1)])

    def test_parse_shard_rejects_excuses_for_old_lib(self) -> None:
        """Excuses under an old evaluated lib fail closed, never silently dropped."""
        driver.r2 = _old_lib()
        with self.assertRaisesRegex(ValueError, "predates excused-skip"):
            driver.parse_shard("log", 1, 1, frozenset({"t1"}))

    def test_shard_excuses_wires_new_lib_attribution(self) -> None:
        """A new evaluated lib attributes excuses through the run's own-SHA plan."""
        calls: dict[str, Any] = {}

        def inventory(root: Path, sha: str) -> list[str]:
            calls["sha"] = sha
            return ["p|t1", "p|t2"]

        lib = SimpleNamespace(
            provenance=SimpleNamespace(
                source_inventory=inventory,
                source_rows=lambda root, sha, plan, count: {1: [frozenset({"p|t1"})]},
                run_excuse=lambda logs, run_id, plan: frozenset({"t1"}),
                shard_excused_runs=lambda rows, excuse: frozenset({"p|t1"})),
            _parse_shard=lambda log, number, total, excused=frozenset(), excused_packages=frozenset(): ([], [], ((), ""), []))
        driver.r2 = lib
        run_dir = _write_run(self.root, [_job("c" * 40)], {"head_sha": "c" * 40})
        self.assertEqual(driver.shard_excuses(run_dir, 1), {1: frozenset({"t1"})})
        self.assertEqual(calls["sha"], "c" * 40)


class ExcuseRegexCases(unittest.TestCase):
    """The excuse line matcher accepts the emitter forms and nothing else."""

    def test_accepts_both_emitter_forms(self) -> None:
        """The runner and env-var excuse spellings both parse."""
        log = ("2026-10-26T00:00:00Z live-postgres-readiness: extra skip excuse: TestOne TestTwo\n"
               "2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP: TestThree\n")
        self.assertEqual(provenance.excused_names(log),
                         frozenset({"TestOne", "TestTwo", "TestThree"}))

    def test_rejects_malformed_excuse_lines(self) -> None:
        """Double spaces, leading spaces, and non-Test tokens excuse nothing."""
        for line in ("2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP: TestOne  TestTwo\n",
                     "2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP:  TestOne\n",
                     "2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP: TestOne nope\n",
                     "2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP: \n"):
            self.assertEqual(provenance.excused_names(line), frozenset(),
                             f"line excuses unexpectedly: {line!r}")

    def test_packed_names_complete_fast(self) -> None:
        """Packed Test-prefixed names cannot stall the matcher (ReDoS guard)."""
        evil = ("2026-10-26T00:00:00Z   LIVE_READINESS_EXTRA_SKIP: "
                + "TestA" * 128 + "!\n")
        start = time.monotonic()
        names = provenance.excused_names(evil)
        elapsed = time.monotonic() - start
        self.assertEqual(names, frozenset())
        self.assertLess(elapsed, 5.0)


if __name__ == "__main__":
    unittest.main()
