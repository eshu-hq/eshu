#!/usr/bin/env python3
"""Contract tests for the readiness refit evidence driver (#10388).

Run from the repository root: ``python3 scripts/test-readiness-refit-evidence.py``.
"""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from typing import Any

sys.path.insert(0, str(Path(__file__).parent / "dev" / "readiness-refit-evidence"))
sys.path.insert(0, str(Path(__file__).parent / "lib"))
import refit_evidence as driver  # noqa: E402


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
            _parse_shard=lambda log, number, total, excused=frozenset(): ([], [], ((), ""), []))
        driver.r2 = lib
        run_dir = _write_run(self.root, [_job("c" * 40)], {"head_sha": "c" * 40})
        self.assertEqual(driver.shard_excuses(run_dir, 1), {1: frozenset({"t1"})})
        self.assertEqual(calls["sha"], "c" * 40)


if __name__ == "__main__":
    unittest.main()
