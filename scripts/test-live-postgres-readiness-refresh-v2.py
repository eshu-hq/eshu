#!/usr/bin/env python3
"""Contract tests for provenance-checked v2 readiness weight refreshes."""

from __future__ import annotations

import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent / "lib"))
import live_postgres_readiness_refresh_v2 as refresh  # noqa: E402
import live_postgres_readiness_weights_refresh as cli_refresh  # noqa: E402


PLAN = [f"./{package}|Test{name}" for package, name in (
    ("a", "One"), ("a", "Two"), ("b", "One"), ("b", "Two"),
    ("b", "Three"), ("b", "Four"))]
SHA = "a" * 40
OTHER_SHA = "b" * 40


def _log(shard: int, rows: list[tuple[str, str, int]], run_id: int) -> str:
    """Build a small timestamped runner proof with explicit row residuals."""
    lines = [
        f"2026-10-26T00:00:00Z Complete job name: live-postgres-readiness (postgres18) shard ({shard}/4)",
        "2026-10-26T00:00:00Z Image: ubuntu-24.04",
        "2026-10-26T00:00:00Z Version: 20261012.334.1",
        "2026-10-26T00:00:00Z Setup go version spec 1.26.10",
        "2026-10-26T00:00:00Z ESHU_LIVE_POSTGRES_IMAGE: ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:" + "c" * 64,
        f'2026-10-26T00:00:00Z ##[group]Run bash scripts/run-live-postgres-readiness-tests.sh --shard "{shard}/4" --report readiness-shard-report.json',
    ]
    second = 0
    for package, test, residual in rows:
        second += 1 + residual
        lines.append(f"2026-10-26T00:00:{second:02d}Z {test}: PASS elapsed=1.0s")
        lines.append(f"2026-10-26T00:00:{second:02d}Z live-postgres-readiness: ./{package} 1/1 PASS")
    lines.append(f"2026-10-26T00:00:{second:02d}Z readiness shard {shard}/4: "
                 f"{len(rows)}/{len(rows)} passing runs, ok=True")
    lines.append(f"2026-10-26T00:00:{second:02d}Z ##[endgroup]")
    lines.append(f"2026-10-26T00:00:{second:02d}Z   name: readiness-shard-{run_id}-1-{shard}")
    return "\n".join(lines) + "\n"


def _sample(root: Path, run_id: int, sha: str) -> Path:
    """Write one complete synthetic main workflow sample."""
    directory = root / str(run_id)
    directory.mkdir()
    (directory / "run.json").write_text(json.dumps({
        "id": run_id, "head_sha": sha, "head_branch": "main", "event": "push",
        "status": "completed", "conclusion": "success",
        "path": ".github/workflows/live-postgres-readiness.yml", "run_attempt": 1,
        "repository": {"full_name": "eshu-hq/eshu"},
        "head_repository": {"full_name": "eshu-hq/eshu"},
    }))
    jobs = []
    rows_by_shard = [
        [("a", "TestOne", 8)],
        [("a", "TestTwo", 8), ("b", "TestOne", 5), ("b", "TestTwo", 2)],
        [("b", "TestThree", 8)],
        [("b", "TestFour", 8)],
    ]
    for shard, rows in enumerate(rows_by_shard, 1):
        jobs.append({
            "id": 1000 * run_id + shard, "run_id": run_id, "head_sha": sha,
            "run_attempt": 1, "name": f"live-postgres-readiness (postgres18) shard ({shard}/4)",
            "status": "completed", "conclusion": "success",
            "started_at": "2026-10-26T00:00:00Z", "completed_at": "2026-10-26T00:00:30Z",
            "steps": [{"name": "Run live readiness proofs", "status": "completed",
                       "conclusion": "success", "started_at": "2026-10-26T00:00:05Z",
                       "completed_at": "2026-10-26T00:00:25Z"}],
        })
        (directory / f"shard{shard}.log").write_text(_log(shard, rows, run_id))
    for name in ("live-postgres-readiness hermetic",
                 "live-postgres-readiness (postgres18)",):
        jobs.append({"id": 1000 * run_id + len(jobs) + 1, "run_id": run_id,
                     "head_sha": sha, "run_attempt": 1, "name": name,
                     "status": "completed", "conclusion": "success"})
    (directory / "jobs.json").write_text(json.dumps({"total_count": len(jobs), "jobs": jobs}))
    return directory


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
            3: [frozenset([PLAN[4]])],
            4: [frozenset([PLAN[5]])],
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

    def test_source_rows_support_five_shard_v2(self) -> None:
        plan = [f"./a|Test{number}" for number in range(1, 6)]
        document = {
            "schema_version": 2, "source_runs": ["100", "101"], "shard_count": 5,
            "fixed_job_seconds": 1.0, "cold_cache_seconds": 1.0,
            "default_test_seconds": 1.0, "default_package_first_seconds": 1.0,
            "default_package_repeat_seconds": 1.0,
            "package_first_seconds": {"./a": 1.0},
            "package_repeat_seconds": {"./a": 1.0},
            "test_seconds": {run: 1.0 for run in plan},
        }
        with mock.patch.object(refresh.provenance, "_source_text",
                               return_value=json.dumps(document)):
            rows = self.real_source_rows(self.root, SHA, plan, 5)
        self.assertEqual(list(rows), [1, 2, 3, 4, 5])
        self.assertEqual([next(iter(rows[index])) for index in rows],
                         [frozenset([run]) for run in plan])

    def test_measured_first_repeat_and_cold_fit(self) -> None:
        result = self.fit()
        self.assertEqual(result["source_runs"], ["100", "101"])
        self.assertEqual(result["schema_version"], 2)
        self.assertEqual(result["shard_count"], 5)
        self.assertEqual(result["cold_cache_seconds"], 3.0)
        self.assertEqual(result["package_first_seconds"], {"./a": 5.0, "./b": 5.0})
        self.assertEqual(result["package_repeat_seconds"], {"./b": 2.0})
        self.assertGreater(result["default_package_repeat_seconds"], 0)

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
        path.write_text(original.replace("TestFour: PASS", "TestThree: PASS"))
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
        self.expected_rows[2] = [frozenset([PLAN[1]])]
        self.expected_rows[3] = [frozenset([PLAN[2]]), frozenset([PLAN[3]]),
                                 frozenset([PLAN[4]])]
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
            (source / "shard1.log").write_text(_log(1, [("b", "TestFour", 8)], int(source.name)))
            (source / "shard4.log").write_text(_log(4, [("a", "TestOne", 8)], int(source.name)))
        self.expected_rows[1] = [frozenset([PLAN[5]])]
        self.expected_rows[4] = [frozenset([PLAN[0]])]
        with self.assertRaisesRegex(ValueError, "order"):
            self.fit()

    def test_rejects_source_inventory_mismatch(self) -> None:
        with mock.patch.object(refresh.provenance, "source_inventory", return_value=PLAN[:-1]):
            with self.assertRaisesRegex(ValueError, "inventory|coverage"):
                self.fit()


if __name__ == "__main__":
    unittest.main()
