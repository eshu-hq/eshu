#!/usr/bin/env python3
"""Contract tests for provenance-checked v2 readiness weight refreshes."""

from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import statistics
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent / "lib"))
import live_postgres_readiness_refresh_v2 as refresh  # noqa: E402
import live_postgres_readiness_weights_refresh as cli_refresh  # noqa: E402
from live_postgres_readiness_weights_history_cases import SourceHistoryTest  # noqa: E402,F401
from live_postgres_readiness_weights_pooled_cases import PooledPricingTest  # noqa: E402,F401
from live_postgres_readiness_weights_refresh_fixture import (  # noqa: E402
    OTHER_SHA, PLAN, SHA, shard_log as _log, write_sample as _sample)


class RefreshV2Cases(unittest.TestCase):
    """A v2 fit requires eligible source runs and identifiable row categories."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.sources = [_sample(self.root, 100, SHA), _sample(self.root, 101, OTHER_SHA)]
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
        self.real_source_rows = refresh.provenance.source_rows
        rows = mock.patch.object(refresh.provenance, "source_rows",
                                 side_effect=lambda _root, _sha, _plan, _count: self.expected_rows)
        rows.start()
        self.addCleanup(rows.stop)

    def fit(self) -> dict[str, object]:
        """Fit the two complete synthetic workflows."""
        return refresh.build_document(self.sources, self.root)

    def test_v2_render_is_sorted_and_two_entries_per_line(self) -> None:
        document = {
            "test_seconds": {"./z|TestC": 3.0, "./b|TestB": 2.0,
                             "./a|TestA": 1.0},
            "source_runs": ["100", "101"],
            "package_repeat_seconds": {},
            "schema_version": 2,
        }
        expected = (
            '{\n'
            '  "package_repeat_seconds": {\n'
            '  },\n'
            '  "schema_version": 2,\n'
            '  "source_runs": ["100", "101"],\n'
            '  "test_seconds": {\n'
            '    "./a|TestA": 1.0, "./b|TestB": 2.0,\n'
            '    "./z|TestC": 3.0\n'
            '  }\n'
            '}\n'
        )
        rendered = cli_refresh.render(document)
        self.assertEqual(rendered, expected)
        self.assertEqual(json.loads(rendered), document)
        self.assertEqual(cli_refresh.render(json.loads(rendered)), rendered)

    def test_v1_render_keeps_legacy_bytes(self) -> None:
        document = {
            "source_runs": ["100"],
            "test_seconds": {"./b|TestB": 2.0, "./a|TestA": 1.0},
            "schema_version": 1,
        }
        self.assertEqual(cli_refresh.render(document), (
            '{\n'
            '  "source_runs": ["100"],\n'
            '  "schema_version": 1,\n'
            '  "test_seconds": {\n'
            '    "./a|TestA": 1.0,\n'
            '    "./b|TestB": 2.0\n'
            '  }\n'
            '}\n'
        ))

    def test_cli_writes_compact_v2_layout(self) -> None:
        output = self.root / "weights.v2.json"
        argv = ["refresh", "--schema-version", "2", "--out", str(output)]
        argv.extend(f"--sample={source.name}={source}" for source in self.sources)
        with mock.patch.object(cli_refresh.sys, "argv", argv):
            self.assertEqual(cli_refresh.main(), 0)
        rendered = output.read_text()
        self.assertEqual(json.loads(rendered), self.fit())
        self.assertTrue(any(line.count("|Test") == 2 for line in rendered.splitlines()))
        self.assertEqual(cli_refresh.render(json.loads(rendered)), rendered)

    def test_source_rows_support_six_shard_v2(self) -> None:
        plan = [f"./a|Test{number}" for number in range(1, 7)]
        document = {
            "schema_version": 2, "source_runs": ["100", "101"], "shard_count": 6,
            "fixed_job_seconds": 1.0, "cold_cache_seconds": 1.0,
            "default_test_seconds": 1.0, "default_package_first_seconds": 1.0,
            "default_package_repeat_seconds": 1.0,
            "package_first_seconds": {"./a": 1.0},
            "package_repeat_seconds": {"./a": 1.0},
            "test_seconds": {run: 1.0 for run in plan},
        }
        with mock.patch.object(refresh.provenance, "_source_text",
                               return_value=json.dumps(document)):
            rows = self.real_source_rows(self.root, SHA, plan, 6)
        self.assertEqual(list(rows), [1, 2, 3, 4, 5, 6])
        self.assertEqual([next(iter(rows[index])) for index in rows],
                         [frozenset([run]) for run in plan])

    def test_measured_first_repeat_and_cold_fit(self) -> None:
        result = self.fit()
        self.assertEqual(result["source_runs"], ["100", "101"])
        self.assertEqual(result["schema_version"], 2)
        self.assertEqual(result["shard_count"], 6)
        self.assertEqual(result["cold_cache_seconds"], 3.0)
        self.assertEqual(result["package_first_seconds"],
                         {"./a": 5.0, "./b": 5.0, "./c": 5.0})
        self.assertEqual(result["package_repeat_seconds"], {"./b": 2.0})
        self.assertGreater(result["default_package_repeat_seconds"], 0)

    def test_cold_fit_downweights_noisy_negative_pair(self) -> None:
        """A huge-variance negative pair must not veto a tight positive one."""
        tight = {"first_row": [60.8, 63.1], "first_later": [1.6, 1.6]}
        noisy = {"first_row": [666.6, 488.0, 434.7, 482.3, 469.2, 422.5],
                 "first_later": [556.0, 573.5]}
        diffs = [statistics.mean(kinds["first_row"]) - statistics.mean(kinds["first_later"])
                 for kinds in (tight, noisy)]
        self.assertLessEqual(statistics.mean(diffs), 0)
        cold, components = refresh.fit_cold({"./tight": dict(tight),
                                             "./noisy": dict(noisy)})
        self.assertAlmostEqual(cold, 60.3, delta=0.5)
        by_package = {component.package: component for component in components}
        self.assertEqual(sorted(by_package), ["./noisy", "./tight"])
        self.assertLess(by_package["./noisy"].weight, 0.01)
        self.assertAlmostEqual(sum(component.weight for component in components), 1.0)
        line = by_package["./tight"].line()
        for fragment in ("./tight", "diff=", "variance=", "weight="):
            self.assertIn(fragment, line)

    def test_cold_fit_rejects_all_tight_negative_pairs(self) -> None:
        """Tight negative pairs still fail closed instead of pricing cold."""
        categories = {"./a": {"first_row": [5.0, 5.2], "first_later": [10.1, 10.3]},
                      "./b": {"first_row": [7.0, 7.1], "first_later": [12.0, 12.2]}}
        with self.assertRaisesRegex(ValueError, "cold"):
            refresh.fit_cold(categories)

    def test_cold_fit_rejects_single_eligible_pair(self) -> None:
        """One eligible pair cannot identify cold, however tight it is."""
        categories = {"./only": {"first_row": [60.8, 63.1], "first_later": [1.6, 1.6]},
                      "./thin": {"first_row": [9.0], "first_later": [2.0, 2.1]}}
        with self.assertRaisesRegex(ValueError, "cold"):
            refresh.fit_cold(categories)

    def test_cold_fit_rejects_positive_but_insignificant_cold(self) -> None:
        """A positive cold within two standard errors still fails closed."""
        categories = {"./a": {"first_row": [0.0, 20.0], "first_later": [9.5, 9.9]},
                      "./b": {"first_row": [0.0, 30.0], "first_later": [14.3, 14.7]}}
        with self.assertRaisesRegex(ValueError, "significant"):
            refresh.fit_cold(categories)

    def test_cold_fit_matches_unweighted_mean_at_equal_variance(self) -> None:
        """Equal-variance pairs reproduce the old arithmetic mean."""
        categories = {"./a": {"first_row": [10.0, 12.0], "first_later": [4.0, 6.0]},
                      "./b": {"first_row": [20.0, 22.0], "first_later": [8.0, 10.0]}}
        expected = statistics.mean(
            statistics.mean(kinds["first_row"]) - statistics.mean(kinds["first_later"])
            for kinds in categories.values())
        cold, _ = refresh.fit_cold(categories)
        self.assertAlmostEqual(cold, expected, places=1)

    def test_pool_of_four_prices_fast_draw_at_p75(self) -> None:
        """One fast run among four prices at p75; two or three runs keep the mean."""
        self.sources += [_sample(self.root, 102, SHA), _sample(self.root, 103, OTHER_SHA)]
        fast = self.sources[0]
        (fast / "shard2.log").write_text(_log(2, [
            ("a", "TestTwo", 8), ("b", "TestOne", 1), ("b", "TestTwo", 0)], 100))
        path = fast / "shard4.log"
        path.write_text(path.read_text().replace("TestTwo: PASS elapsed=1.0s",
                                                 "TestTwo: PASS elapsed=0.2s"))
        priced = {4: (1.0, 5.0, 2.0), 3: (0.7, 3.7, 1.3), 2: (0.6, 3.0, 1.0)}
        for runs, (test, first, repeat) in priced.items():
            result = refresh.build_document(self.sources[:runs], self.root)
            self.assertEqual((result["test_seconds"]["./c|TestTwo"],
                              result["package_first_seconds"]["./b"],
                              result["package_repeat_seconds"]["./b"]),
                             (test, first, repeat), f"{runs}-run pool")
        output = self.root / "weights.v2.json"
        argv = ["refresh", "--schema-version", "2", "--out", str(output)]
        argv.extend(f"--sample={source.name}={source}" for source in self.sources)
        stdout = io.StringIO()
        with mock.patch.object(cli_refresh.sys, "argv", argv), \
                contextlib.redirect_stdout(stdout):
            self.assertEqual(cli_refresh.main(), 0)
        self.assertIn(refresh.pricing_rule(["100", "101", "102", "103"],
                                           dict.fromkeys(["100", "101", "102", "103"], 4)
                                           ).line() + "\n",
                      stdout.getvalue())
        self.assertIn("p75 (linear inclusive)", stdout.getvalue())

    def test_rejects_feature_or_partial_run(self) -> None:
        path = self.sources[0] / "run.json"
        run = json.loads(path.read_text())
        for field, value in (("head_branch", "feature"), ("conclusion", "failure")):
            changed = dict(run, **{field: value})
            path.write_text(json.dumps(changed))
            with self.assertRaisesRegex(ValueError, "main|success"):
                self.fit()
        path.write_text(json.dumps(run))

    def test_rejects_missing_or_duplicate_inventory_tests(self) -> None:
        path = self.sources[0] / "shard4.log"
        original = path.read_text()
        path.write_text(original.replace("TestTwo: PASS", "TestOne: PASS"))
        with self.assertRaisesRegex(ValueError, "inventory|duplicate|coverage|row.*source"):
            self.fit()

    def test_rejects_unidentified_repeat(self) -> None:
        for source in self.sources:
            path = source / "shard2.log"
            path.write_text(path.read_text().replace("TestTwo: PASS", "TestTwo: SKIP"))
        with self.assertRaises(ValueError):
            self.fit()

    def test_rejects_swapped_same_package_row_membership(self) -> None:
        for source in self.sources:
            (source / "shard2.log").write_text(_log(2, [
                ("a", "TestTwo", 8), ("b", "TestTwo", 5),
                ("b", "TestOne", 2)], int(source.name)))
        with self.assertRaisesRegex(ValueError, "row.*source"):
            self.fit()

    def test_rejects_source_shard_boundary_drift(self) -> None:
        self.expected_rows[1] = [frozenset([PLAN[1]])]
        self.expected_rows[2][0] = frozenset([PLAN[0]])
        with self.assertRaisesRegex(ValueError, "row.*source"):
            self.fit()

    def test_rejects_no_identifiable_cold(self) -> None:
        for source in self.sources:
            (source / "shard2.log").write_text(_log(2, [("a", "TestTwo", 8)], int(source.name)))
            (source / "shard3.log").write_text(_log(3, [
                ("b", "TestOne", 8), ("b", "TestTwo", 2),
                ("b", "TestThree", 8)], int(source.name)))
            (source / "shard4.log").write_text(_log(4, [
                ("c", "TestOne", 5), ("c", "TestTwo", 8)], int(source.name)))
        self.expected_rows[2] = [frozenset([PLAN[1]])]
        self.expected_rows[3] = [frozenset([PLAN[2]]), frozenset([PLAN[3]]),
                                 frozenset([PLAN[4]])]
        self.expected_rows[4] = [frozenset([PLAN[5]]), frozenset([PLAN[6]])]
        with self.assertRaisesRegex(ValueError, "cold"):
            self.fit()

    def test_rejects_no_measured_repeat(self) -> None:
        for source in self.sources:
            path = source / "shard2.log"
            log = _log(2, [("a", "TestTwo", 8), ("b", "TestOne", 5)], int(source.name))
            log = log.replace(
                "2026-10-26T00:00:15Z live-postgres-readiness: ./b 1/1 PASS",
                "2026-10-26T00:00:15Z TestTwo: PASS elapsed=1.0s\n"
                "2026-10-26T00:00:15Z live-postgres-readiness: ./b 2/2 PASS")
            log = log.replace("2/2 passing runs", "3/3 passing runs")
            path.write_text(log)
        self.expected_rows[2] = [frozenset([PLAN[1]]), frozenset([PLAN[2], PLAN[3]])]
        with self.assertRaisesRegex(ValueError, "repeat"):
            self.fit()

    def test_accepts_mixed_runner_image_version_and_reports_it(self) -> None:
        # Hosted images roll weekly over several days, so one run mixes image
        # versions. The version is reported, never gated, and never priced.
        uniform = self.fit()
        # Runner logs put a provisioner Version before the actual Image Version.
        for source in self.sources:
            for number in range(1, 5):
                path = source / f"shard{number}.log"
                path.write_text("2026-10-26T00:00:00Z Version: 20261021.618\n"
                                + path.read_text())
        path = self.sources[0] / "shard1.log"
        path.write_text(path.read_text().replace("Version: 20261012.334.1",
                                                "Version: 20261025.353.1"))
        self.assertEqual(self.fit(), uniform)
        output = self.root / "weights.v2.json"
        argv = ["refresh", "--schema-version", "2", "--out", str(output)]
        argv.extend(f"--sample={source.name}={source}" for source in self.sources)
        stdout = io.StringIO()
        with mock.patch.object(cli_refresh.sys, "argv", argv), \
                contextlib.redirect_stdout(stdout):
            self.assertEqual(cli_refresh.main(), 0)
        self.assertEqual(json.loads(output.read_text()), uniform)
        self.assertIn(
            "weights refresh v2: runner image ubuntu-24.04 versions "
            "run 100 shard1=20261025.353.1 shard2=20261012.334.1 "
            "shard3=20261012.334.1 shard4=20261012.334.1; "
            "run 101 shard1=20261012.334.1 shard2=20261012.334.1 "
            "shard3=20261012.334.1 shard4=20261012.334.1; "
            "distinct 20261012.334.1, 20261025.353.1\n",
            stdout.getvalue())

    def test_ignores_runner_cpu_step_output(self) -> None:
        # Each shard job prints `lscpu | head -20 || true; nproc` first
        # (#9532). That output lands before the Go setup and the proof step,
        # and it must change neither the profile nor any price. Its stamp is
        # 30 s earlier than every other line, so a parser that took its timing
        # baseline from any line but the proof step's group line would move a
        # residual and fail here.
        uniform = self.fit()
        cpu_step = "".join(f"2026-10-25T23:59:30Z {line}\n" for line in (
            "##[group]Run lscpu | head -20 || true; nproc", "Architecture:  x86_64",
            "CPU(s):  4", "Vendor ID:  AuthenticAMD",
            "Model name:  AMD EPYC 7763 64-Core Processor", "Model:  1",
            "Thread(s) per core:  2", "4", "##[endgroup]"))
        for source in self.sources:
            for number in range(1, 5):
                path = source / f"shard{number}.log"
                text = path.read_text()
                marker = "2026-10-26T00:00:00Z Setup go version spec"
                self.assertEqual(text.count(marker), 1)
                path.write_text(text.replace(marker, cpu_step + marker))
        self.assertEqual(self.fit(), uniform)

    def test_rejects_changed_runner_image_name(self) -> None:
        path = self.sources[0] / "shard1.log"
        path.write_text(path.read_text().replace("Image: ubuntu-24.04",
                                                "Image: ubuntu-22.04"))
        with self.assertRaisesRegex(ValueError, "profile"):
            self.fit()

    def test_rejects_changed_postgres_digest(self) -> None:
        path = self.sources[1] / "shard3.log"
        path.write_text(path.read_text().replace("sha256:" + "c" * 64,
                                                "sha256:" + "d" * 64))
        with self.assertRaisesRegex(ValueError, "profile"):
            self.fit()

    def test_rejects_incompatible_profile_and_missing_job(self) -> None:
        log_path = self.sources[0] / "shard1.log"
        original = log_path.read_text()
        log_path.write_text(original.replace("1.26.10", "1.26.11"))
        with self.assertRaisesRegex(ValueError, "profile"):
            self.fit()
        log_path.write_text(original)
        jobs_path = self.sources[0] / "jobs.json"
        payload = json.loads(jobs_path.read_text())
        payload["jobs"] = payload["jobs"][:-1]
        payload["total_count"] -= 1
        jobs_path.write_text(json.dumps(payload))
        with self.assertRaisesRegex(ValueError, "required jobs"):
            self.fit()

    def test_rejects_other_repository(self) -> None:
        path = self.sources[0] / "run.json"
        run = json.loads(path.read_text())
        run["repository"]["full_name"] = "someone/fork"
        path.write_text(json.dumps(run))
        with self.assertRaisesRegex(ValueError, "repository"):
            self.fit()

    def test_rejects_log_from_other_run(self) -> None:
        target = self.sources[0] / "shard1.log"
        target.write_text((self.sources[1] / "shard1.log").read_text())
        with self.assertRaisesRegex(ValueError, "log"):
            self.fit()

    def test_rejects_duplicate_job_ids(self) -> None:
        path = self.sources[0] / "jobs.json"
        payload = json.loads(path.read_text())
        payload["jobs"][1]["id"] = payload["jobs"][0]["id"]
        path.write_text(json.dumps(payload))
        with self.assertRaisesRegex(ValueError, "job ID"):
            self.fit()

    def test_rejects_reordered_source_package_rows(self) -> None:
        for source in self.sources:
            (source / "shard1.log").write_text(_log(1, [("c", "TestTwo", 8)], int(source.name)))
            (source / "shard4.log").write_text(_log(4, [("a", "TestOne", 8)], int(source.name)))
        self.expected_rows[1] = [frozenset([PLAN[6]])]
        self.expected_rows[4] = [frozenset([PLAN[0]])]
        with self.assertRaisesRegex(ValueError, "order"):
            self.fit()

    def test_rejects_source_inventory_mismatch(self) -> None:
        with mock.patch.object(refresh.provenance, "source_inventory", return_value=PLAN[:-1]):
            with self.assertRaisesRegex(ValueError, "inventory|coverage"):
                self.fit()


if __name__ == "__main__":
    unittest.main()
