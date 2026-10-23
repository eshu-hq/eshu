#!/usr/bin/env python3
"""Hermetic checks for scripts/lib/gha_census.py aggregation."""

from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timedelta, timezone
import io
import json
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

import gha_census  # noqa: E402
from gha_census import aggregate, dedupe, fetch_window, render_markdown  # noqa: E402


def run(rid, workflow, conclusion, created, updated, status="completed"):
    return {
        "databaseId": rid,
        "workflowName": workflow,
        "status": status,
        "conclusion": conclusion,
        "createdAt": created,
        "updatedAt": updated,
    }


FIXTURE = [
    run(1, "Build Test", "success", "2026-10-19T00:00:00Z", "2026-10-19T00:30:00Z"),
    run(2, "Build Test", "failure", "2026-10-19T01:00:00Z", "2026-10-19T02:30:00Z"),
    run(3, "Build Test", "cancelled", "2026-10-19T02:00:00Z", "2026-10-19T02:10:00Z"),
    run(4, "Required Gates", "cancelled", "2026-10-19T02:00:00Z", "2026-10-19T02:01:00Z"),
    run(5, "Required Gates", "success", "2026-10-19T03:00:00Z", "2026-10-19T03:05:00Z"),
    run(6, "Required Gates", None, "2026-10-19T04:00:00Z", "2026-10-19T04:00:30Z",
        status="in_progress"),
]


class TestGhaCensus(unittest.TestCase):
    def test_dedupe_keeps_first_per_id(self):
        dupes = FIXTURE + [dict(FIXTURE[0])]
        self.assertEqual(len(dedupe(dupes)), len(FIXTURE))

    def test_totals_and_shares(self):
        report = aggregate(FIXTURE)
        self.assertEqual(report["total"], 6)
        self.assertEqual(report["completed"], 5)
        self.assertEqual(report["by_conclusion"]["cancelled"], 2)
        self.assertAlmostEqual(report["cancelled_share"], 2 / 5)
        self.assertEqual(report["over_60m"], 1)

    def test_workflow_durations(self):
        report = aggregate(FIXTURE)
        build = report["workflows"]["Build Test"]
        self.assertEqual(build["runs"], 3)
        self.assertEqual(build["failures"], 1)
        # durations 30, 90, 10 minutes -> median 30, max 90
        self.assertEqual(build["median_minutes"], 30.0)
        self.assertEqual(build["p50_minutes"], 30.0)
        self.assertEqual(build["p95_minutes"], 90.0)
        self.assertEqual(build["max_minutes"], 90.0)

    def test_render_names_the_numbers(self):
        text = render_markdown(aggregate(FIXTURE), "2026-10-18T00:00:00Z",
                               "2026-10-19T00:00:00Z")
        self.assertIn("6 runs", text)
        self.assertIn("Build Test", text)
        self.assertIn("40.0%", text)
        self.assertIn("P95 min", text)

    def test_required_green_header_describes_publisher_kind(self):
        report = aggregate(FIXTURE)
        report["required_green"] = {
            "outcomes": {"success": 1},
            "overall": {"heads": 1, "success": 1,
                        "p50_minutes": 2, "p95_minutes": 2},
            "groups": {"ordinary": {"heads": 1, "success": 1,
                                     "p50_minutes": 2, "p95_minutes": 2}},
            "heads": [],
        }
        rendered = render_markdown(report, "start", "end")
        self.assertIn("| Publisher kind | Heads | Green |", rendered)


class TestEnrichmentFailure(unittest.TestCase):
    """An enrichment failure preserves raw totals and the other section."""

    def test_required_green_failure_keeps_cancellation_and_raw_totals(self):
        self._check_failure("required_green", "cancellation_cost")

    def test_cancellation_failure_keeps_required_green_and_raw_totals(self):
        self._check_failure("cancellation_cost", "required_green")

    def test_unknown_cancellation_is_marked_partial(self):
        cost = {"runs": 1, "categories": {"unknown": 1},
                "evidenced_runner_minutes": 0, "job_budget_exhausted": 0,
                "source_drift": {"missing_from_rest": 0, "late_cancelled": 0}}
        with TemporaryDirectory() as folder:
            target = Path(folder) / "report.json"
            with mock.patch.object(gha_census, "fetch_window",
                                   return_value=(FIXTURE, 0)), \
                    mock.patch.object(gha_census, "required_green",
                                      return_value={"outcomes": {}, "overall": {
                                          "heads": 0, "success": 0,
                                          "p50_minutes": None,
                                          "p95_minutes": None},
                                                    "groups": {}, "heads": []}), \
                    mock.patch.object(gha_census, "cancellation_cost",
                                      return_value=cost), \
                    redirect_stdout(io.StringIO()):
                self.assertEqual(gha_census.main(["--hours", "1", "--json-out",
                                                  str(target)]), 0)
            report = json.loads(target.read_text(encoding="utf-8"))
        self.assertEqual(report["cancellation_cost"]["completeness"], "partial")

    def _check_failure(self, failing: str, successful: str) -> None:
        good = ({"outcomes": {}, "overall": {"heads": 0, "success": 0,
                  "p50_minutes": None, "p95_minutes": None},
                 "groups": {}, "heads": []}
                if successful == "required_green" else
                {"runs": 0, "categories": {}, "evidenced_runner_minutes": 0,
                 "job_budget_exhausted": 0, "source_drift": {
                     "missing_from_rest": 0, "late_cancelled": 0}})
        out = io.StringIO()
        err = io.StringIO()
        with TemporaryDirectory() as folder:
            target = Path(folder) / "report.json"
            with mock.patch.object(gha_census, "fetch_window",
                                   return_value=(FIXTURE, 0)), \
                    mock.patch.object(gha_census, failing,
                                      side_effect=RuntimeError("API page incomplete")), \
                    mock.patch.object(gha_census, successful,
                                      return_value=good), \
                    redirect_stdout(out), redirect_stderr(err):
                self.assertEqual(gha_census.main(["--hours", "1", "--json-out",
                                                  str(target)]), 0)
            report = json.loads(target.read_text(encoding="utf-8"))
        self.assertEqual(report["total"], 6)
        self.assertEqual(report["by_conclusion"]["cancelled"], 2)
        self.assertEqual({key: report[successful][key] for key in good}, good)
        self.assertIn(report[successful]["completeness"], ("complete", "partial"))
        self.assertEqual(report[failing]["completeness"], "failed")
        self.assertIn("API page incomplete", report[failing]["error"])
        self.assertIn("API page incomplete", out.getvalue())
        self.assertIn("6 runs", out.getvalue())
        self.assertIn("metrics are unavailable", out.getvalue())
        other_heading = ("### Required-green latency" if successful ==
                         "required_green" else "### Cancelled-run runner cost")
        self.assertIn(other_heading, out.getvalue())
        self.assertIn("API page incomplete", err.getvalue())


class TestMalformedRows(unittest.TestCase):
    def test_missing_name_and_null_timestamp_do_not_abort(self):
        rows = FIXTURE + [
            {"databaseId": 99, "status": "completed",
             "conclusion": "success", "createdAt": "2026-10-19T05:00:00Z",
             "updatedAt": "2026-10-19T05:01:00Z"},
            {"databaseId": 100, "workflowName": "W", "status": "completed",
             "conclusion": "success", "createdAt": None,
             "updatedAt": "2026-10-19T05:01:00Z"},
        ]
        report = aggregate(rows)
        self.assertEqual(report["total"], 8)
        self.assertEqual(report["malformed_rows"], 1)


class TestFetchWindow(unittest.TestCase):
    """Slice assembly against a capped, newest-first fake endpoint.

    The fake mimics the real cap behavior that broke the first version:
    each call returns at most N rows, newest first, from within the
    requested bounds. A fetch that undercounts old slices fails here.
    """

    def test_capped_slices_still_collect_every_hour(self):
        base = datetime(2026, 10, 19, tzinfo=timezone.utc)
        # 3 runs per hour for 3 hours; fake cap of 2 forces subdivision.
        all_runs = []
        rid = 0
        for hour in range(3):
            for minute in (5, 25, 45):
                rid += 1
                created = base + timedelta(hours=hour, minutes=minute)
                all_runs.append({
                    "databaseId": rid, "workflowName": "W",
                    "createdAt": created.strftime("%Y-%m-%dT%H:%M:%SZ"),
                })

        def fake(start, end):
            in_range = [r for r in all_runs
                        if start.strftime("%Y-%m-%dT%H:%M:%SZ")
                        <= r["createdAt"] < end.strftime("%Y-%m-%dT%H:%M:%SZ")]
            return sorted(in_range, key=lambda r: r["createdAt"],
                          reverse=True)[:2]

        with mock.patch.object(gha_census, "_SLICE_LIMIT", 2), \
                mock.patch.object(gha_census, "_gh_run_list", fake):
            got, truncated = fetch_window(base, base + timedelta(hours=3))
        self.assertEqual({r["databaseId"] for r in got}, set(range(1, 10)))
        self.assertEqual(truncated, 0)

    def test_still_capped_at_max_depth_reports_truncated(self):
        base = datetime(2026, 10, 19, tzinfo=timezone.utc)

        def always_capped(start, end):
            return [
                {"databaseId": 1, "workflowName": "W",
                 "createdAt": base.strftime("%Y-%m-%dT%H:%M:%SZ")},
                {"databaseId": 2, "workflowName": "W",
                 "createdAt": base.strftime("%Y-%m-%dT%H:%M:%SZ")},
            ]

        with mock.patch.object(gha_census, "_SLICE_LIMIT", 2), \
                mock.patch.object(gha_census, "_gh_run_list",
                                  always_capped):
            got, truncated = fetch_window(base, base + timedelta(hours=1))
        # Every leaf stays at the cap through max depth, so the count
        # must say so instead of passing silently.
        self.assertGreater(truncated, 0)
        report = aggregate(got)
        report["truncated_slices"] = truncated
        text = render_markdown(report, "2026-10-19T00:00:00Z",
                               "2026-10-19T01:00:00Z")
        self.assertIn("WARNING", text)


if __name__ == "__main__":
    unittest.main()
