#!/usr/bin/env python3
"""Hermetic checks for scripts/lib/gha_census.py aggregation."""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from gha_census import aggregate, dedupe, render_markdown  # noqa: E402


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
        self.assertEqual(build["max_minutes"], 90.0)

    def test_render_names_the_numbers(self):
        text = render_markdown(aggregate(FIXTURE), "2026-10-18T00:00:00Z",
                               "2026-10-19T00:00:00Z")
        self.assertIn("6 runs", text)
        self.assertIn("Build Test", text)
        self.assertIn("40.0%", text)


if __name__ == "__main__":
    unittest.main()
