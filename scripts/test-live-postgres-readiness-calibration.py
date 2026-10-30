#!/usr/bin/env python3
"""Seeded contract checks for the frozen calibration schedule."""

from __future__ import annotations

import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts/lib"))
import live_postgres_readiness_calibration as calibration  # noqa: E402
import live_postgres_readiness_calibration_provenance as provenance  # noqa: E402


def frozen_records(plan: list[str]) -> list[tuple[str, list[str]]]:
    """Group a flat ``package|test`` plan into consecutive package records."""
    records: list[tuple[str, list[str]]] = []
    for run in plan:
        package, test = run.split("|", 1)
        if records and records[-1][0] == package:
            records[-1][1].append(test)
        else:
            records.append((package, [test]))
    return records


class CalibrationContractTest(unittest.TestCase):
    """Check the manifest against the inventory it was frozen from.

    The manifest is a snapshot of the readiness inventory at ``source_sha``.
    The live inventory keeps growing as live tests are enrolled, so these
    tests must not compare the snapshot with it. They rebuild the frozen
    inventory from the rows and require it to match the independent
    ``source_inventory_sha256`` pin recorded in the manifest. Refusing a
    drifted live inventory is its own contract, tested below.
    """

    @classmethod
    def setUpClass(cls) -> None:
        cls.document = json.loads((ROOT / calibration.MANIFEST).read_text())
        cls.plan = [run for shard in cls.document["rows"]
                    for row in shard for run in row]

    def test_frozen_inventory_matches_its_independent_pin(self) -> None:
        pinned = self.document["source_inventory_sha256"]
        self.assertEqual(hashlib.sha256("\n".join(self.plan).encode()).hexdigest(), pinned)
        changed = self.plan[:-1] + [self.plan[-1] + "X"]
        self.assertNotEqual(hashlib.sha256("\n".join(changed).encode()).hexdigest(), pinned)

    def test_drifted_inventory_blocks_dispatch(self) -> None:
        enrolled = self.plan + ["./internal/query|TestNewlyEnrolledLive"]
        with self.assertRaisesRegex(ValueError, "ordered coverage"):
            calibration.validate_manifest(self.document, enrolled)
        retired = self.plan[:-1]
        with self.assertRaisesRegex(ValueError, "ordered coverage"):
            calibration.validate_manifest(self.document, retired)
        with mock.patch.object(calibration, "package_records",
                               return_value=frozen_records(enrolled)):
            with self.assertRaisesRegex(ValueError, "ordered coverage"):
                calibration.load(ROOT)
        with mock.patch.object(calibration, "package_records",
                               return_value=frozen_records(self.plan)):
            calibration.load(ROOT)

    def test_fixed_rows_cover_canonical_inventory(self) -> None:
        calibration.validate_manifest(self.document, self.plan)
        self.assertEqual(sum(len(row) for shard in self.document["rows"]
                             for row in shard), 702)
        self.assertEqual([sum(len(row) for row in shard)
                          for shard in self.document["rows"]],
                         [75, 44, 230, 45, 97, 211])

    def test_rejects_missing_and_duplicate_runs(self) -> None:
        missing = copy.deepcopy(self.document)
        missing["rows"][0][1].pop()
        with self.assertRaisesRegex(ValueError, "ordered coverage"):
            calibration.validate_manifest(missing, self.plan)
        duplicate = copy.deepcopy(self.document)
        duplicate["rows"][0][1].append(duplicate["rows"][0][1][0])
        with self.assertRaisesRegex(ValueError, "ordered coverage"):
            calibration.validate_manifest(duplicate, self.plan)

    def test_rejects_repartition_even_with_complete_coverage(self) -> None:
        shifted = copy.deepcopy(self.document)
        moved = shifted["rows"][2][0].pop()
        shifted["rows"][2][1].insert(0, moved)
        with self.assertRaisesRegex(ValueError, "row identity"):
            calibration.validate_manifest(shifted, self.plan)


    def test_reports_reject_partial_duplicate_and_changed_rows(self) -> None:
        rows = calibration.validate_manifest(self.document, self.plan)
        records = mock.patch.object(calibration, "package_records",
                                    return_value=frozen_records(self.plan))
        records.start()
        self.addCleanup(records.stop)
        head = "a" * 40
        reports = []
        for shard, shard_rows in enumerate(rows, 1):
            reports.append({"schema_version": 1, "workflow": calibration.WORKFLOW,
                            "head": head, "run_id": 101, "run_attempt": 1,
                            "shard": shard, "manifest_sha256": calibration.file_digest(ROOT, calibration.MANIFEST),
                            "workflow_sha256": calibration.file_digest(ROOT, calibration.WORKFLOW),
                            "harness_sha256": calibration.file_digest(ROOT, calibration.HARNESS),
                            "row_sha256": [calibration.row_digest(row) for row in shard_rows],
                            "runs": [run for row in shard_rows for run in row], "ok": True})
        calibration.verify_reports(ROOT, head, None, reports)
        with self.assertRaisesRegex(ValueError, "partial"):
            calibration.verify_reports(ROOT, head, None, reports[:-1])
        duplicate = copy.deepcopy(reports)
        duplicate[5] = duplicate[4]
        with self.assertRaisesRegex(ValueError, "duplicate"):
            calibration.verify_reports(ROOT, head, None, duplicate)
        changed = copy.deepcopy(reports)
        changed[0]["runs"][0] = "./wrong|TestWrong"
        with self.assertRaisesRegex(ValueError, "differs"):
            calibration.verify_reports(ROOT, head, None, changed)
        false_success = copy.deepcopy(reports)
        false_success[0]["ok"] = 1
        with self.assertRaisesRegex(ValueError, "differs"):
            calibration.verify_reports(ROOT, head, None, false_success)
        mixed = copy.deepcopy(reports)
        mixed[0]["run_attempt"] = 2
        with self.assertRaisesRegex(ValueError, "mix"):
            calibration.verify_reports(ROOT, head, None, mixed)

    def test_paired_sources_reject_duplicate_runs_and_profile_drift(self) -> None:
        row = {"category": "first_row"}
        measured = [row, {"category": "first_later"}, {"category": "repeat"}]
        first = {"run_id": 101, "head": "a" * 40, "profile": ["ubuntu-24.04", "1.26.10", "pg"],
                 "job_ids": {str(i): 100 + i for i in range(7)}, "rows": measured}
        second = copy.deepcopy(first)
        second["run_id"] = 102
        second["job_ids"] = {str(i): 200 + i for i in range(7)}
        with mock.patch.object(provenance, "read_source", side_effect=[first, second]):
            provenance.build_evidence([ROOT / "101", ROOT / "102"], ROOT)
        with mock.patch.object(provenance, "read_source", side_effect=[first, first]):
            with self.assertRaisesRegex(ValueError, "duplicate a run"):
                provenance.build_evidence([ROOT / "101", ROOT / "101"], ROOT)
        second["profile"] = ["ubuntu-22.04", "1.26.10", "pg"]
        with mock.patch.object(provenance, "read_source", side_effect=[first, second]):
            with self.assertRaisesRegex(ValueError, "profile differs"):
                provenance.build_evidence([ROOT / "101", ROOT / "102"], ROOT)


    def test_log_reader_classifies_first_later_and_repeat_rows(self) -> None:
        prefix = "2026-10-29T00:00:00Z "
        lines = [
            prefix + "Image: ubuntu-24.04",
            prefix + "Version: 20261012.334.1",
            prefix + "Setup go version spec 1.26.10",
            prefix + "ESHU_LIVE_POSTGRES_IMAGE: ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:" + "c" * 64,
            prefix + "calibration-start shard=1/6",
        ]
        for second, package, name in ((2, "a", "TestOne"), (4, "b", "TestTwo"),
                                      (6, "b", "TestThree")):
            stamp = f"2026-10-29T00:00:{second:02d}Z "
            lines.extend((stamp + f"{name}: PASS elapsed=1.0s",
                          stamp + f"live-postgres-readiness: ./{package} 1/1 PASS"))
        lines.append("2026-10-29T00:00:06Z readiness calibration shard 1/6: "
                     "3/3 passing runs, ok=True")
        parsed = provenance.refresh._parse_shard(
            provenance._normalized_log("\n".join(lines), 1), 1, 6)
        self.assertEqual([row[1] for row in parsed[1]],
                         ["first_row", "first_later", "repeat"])
        with self.assertRaisesRegex(ValueError, "start marker"):
            provenance._normalized_log("\n".join(lines + [lines[4]]), 1)


    def test_metadata_rejects_push_and_partial_job_page(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary) / "101"
            directory.mkdir()
            run = {"id": 101, "head_sha": "a" * 40, "head_branch": "main",
                   "event": "push", "status": "completed", "conclusion": "success",
                   "path": calibration.WORKFLOW, "run_attempt": 1,
                   "repository": {"full_name": "eshu-hq/eshu"},
                   "head_repository": {"full_name": "eshu-hq/eshu"}}
            (directory / "run.json").write_text(json.dumps(run))
            (directory / "jobs.json").write_text(json.dumps({"total_count": 0, "jobs": []}))
            with self.assertRaisesRegex(ValueError, "dispatch"):
                provenance._run_metadata(directory, ROOT)
            run["event"] = "workflow_dispatch"
            (directory / "run.json").write_text(json.dumps(run))
            (directory / "jobs.json").write_text(json.dumps({"total_count": 7, "jobs": []}))
            with self.assertRaisesRegex(ValueError, "partial"):
                provenance._run_metadata(directory, ROOT)


    def test_reader_rejects_reports_from_other_run_or_attempt(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary) / "101"
            directory.mkdir()
            reports = [{"run_id": 102, "run_attempt": 1} for _ in range(6)]
            for number, report in enumerate(reports, 1):
                (directory / f"shard{number}.json").write_text(json.dumps(report))
            jobs = [{"id": 100, "run_id": 101, "head_sha": "a" * 40,
                     "run_attempt": 1, "name": "readiness-calibration coverage",
                     "status": "completed", "conclusion": "success"}]
            jobs.extend({"id": 100 + number, "run_id": 101, "head_sha": "a" * 40,
                         "run_attempt": 1, "name": f"readiness-calibration (postgres18) shard ({number}/6)",
                         "status": "completed", "conclusion": "success"}
                        for number in range(1, 7))
            with mock.patch.object(provenance, "_run_metadata",
                                   return_value=(101, "a" * 40, 1, jobs)):
                with mock.patch.object(calibration, "verify_reports"):
                    with self.assertRaisesRegex(ValueError, "report run identity"):
                        provenance.read_source(directory, ROOT)
            for number, report in enumerate(reports, 1):
                report["run_id"] = 101
                report["run_attempt"] = 2
                (directory / f"shard{number}.json").write_text(json.dumps(report))
            with mock.patch.object(provenance, "_run_metadata",
                                   return_value=(101, "a" * 40, 1, jobs)):
                with mock.patch.object(calibration, "verify_reports"):
                    with self.assertRaisesRegex(ValueError, "report run identity"):
                        provenance.read_source(directory, ROOT)


    def test_log_rows_accept_sorted_verifier_output_without_losing_schedule(self) -> None:
        expected = self.document["rows"][0][1:3]
        self.assertNotEqual(expected[0], sorted(expected[0]))
        observed = [run for row in expected for run in sorted(row)]
        members = [frozenset(row) for row in expected]
        provenance.verify_log_rows(observed, members, expected)
        with self.assertRaisesRegex(ValueError, "ordered row identity"):
            provenance.verify_log_rows(observed, list(reversed(members)), expected)
        with self.assertRaisesRegex(ValueError, "ordered row identity"):
            provenance.verify_log_rows(list(reversed(observed)), members, expected)


if __name__ == "__main__":
    unittest.main()
