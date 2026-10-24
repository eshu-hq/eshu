#!/usr/bin/env python3
"""Behavior checks for required-green and cancellation census enrichment."""

import sys
import unittest
from unittest import mock
from pathlib import Path
from datetime import datetime, timedelta, timezone

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

import gha_census_enrichment  # noqa: E402
from gha_census_enrichment import (  # noqa: E402
    classify_cancellation, measure_head, percentile,
)


class TestRequiredGreen(unittest.TestCase):
    def test_merge_is_censored_until_all_three_contexts_succeed(self):
        pr = {"number": 1, "merged_at": "2026-10-23T12:10:00Z"}
        runs = [{"created_at": "2026-10-23T12:00:00Z"}]
        checks = [
            {"name": "go-core-complete", "conclusion": "success",
             "created_at": "2026-10-23T12:01:00Z",
             "completed_at": "2026-10-23T12:04:00Z"},
            {"name": "go-race-complete", "conclusion": "success",
             "created_at": "2026-10-23T12:02:00Z",
             "completed_at": "2026-10-23T12:08:00Z"},
        ]
        statuses = [{"context": "required-gates-complete", "state": "pending",
                     "created_at": "2026-10-23T12:09:00Z"}]
        result = measure_head(pr, runs, checks, statuses)
        self.assertEqual(result["outcome"], "pending")
        self.assertIsNone(result["minutes"])

        statuses.append({"context": "required-gates-complete", "state": "success",
                         "description": "Tree-identical reuse: verdict copied",
                         "created_at": "2026-10-23T12:10:01Z"})
        self.assertEqual(measure_head(pr, runs, checks, statuses)["outcome"],
                         "pending")
        statuses[-1]["created_at"] = "2026-10-23T12:09:30Z"
        result = measure_head(pr, runs, checks, statuses)
        self.assertEqual(result["outcome"], "success")
        self.assertEqual(result["kind"], "reuse")
        self.assertEqual(result["minutes"], 9.5)

    def test_unknown_success_description_is_not_reported_as_ordinary(self) -> None:
        pr = {"number": 1, "merged_at": "2026-10-23T12:10:00Z"}
        checks = [{"name": name, "conclusion": "success",
                   "started_at": "2026-10-23T12:01:00Z",
                   "completed_at": "2026-10-23T12:04:00Z"}
                  for name in ("go-core-complete", "go-race-complete")]
        statuses = [{"context": "required-gates-complete", "state": "success",
                     "description": "Unrecognized manual verdict",
                     "created_at": "2026-10-23T12:05:00Z"}]
        result = measure_head(pr, [{"created_at": "2026-10-23T12:00:00Z"}],
                              checks, statuses)
        self.assertEqual(result["outcome"], "success")
        self.assertEqual(result["kind"], "unknown")

    def test_retry_still_running_at_merge_censors_older_green(self):
        pr = {"number": 1, "merged_at": "2026-10-23T12:10:00Z"}
        runs = [{"created_at": "2026-10-23T12:00:00Z"}]
        checks = [
            {"name": "go-core-complete", "conclusion": "success",
             "started_at": "2026-10-23T12:01:00Z",
             "completed_at": "2026-10-23T12:04:00Z"},
            {"name": "go-core-complete", "conclusion": "success",
             "started_at": "2026-10-23T12:09:00Z",
             "completed_at": "2026-10-23T12:11:00Z"},
            {"name": "go-race-complete", "conclusion": "success",
             "started_at": "2026-10-23T12:01:00Z",
             "completed_at": "2026-10-23T12:05:00Z"},
        ]
        statuses = [{"context": "required-gates-complete", "state": "success",
                     "created_at": "2026-10-23T12:07:00Z"}]
        result = measure_head(pr, runs, checks, statuses)
        self.assertNotEqual(result["outcome"], "success")
        self.assertIsNone(result["minutes"])

    def test_latest_failed_retry_and_missing_enqueue_are_visible(self):
        pr = {"number": 1, "merged_at": "2026-10-23T12:10:00Z"}
        checks = [
            {"name": "go-core-complete", "conclusion": "success",
             "created_at": "2026-10-23T12:01:00Z",
             "completed_at": "2026-10-23T12:04:00Z"},
            {"name": "go-core-complete", "conclusion": "failure",
             "created_at": "2026-10-23T12:05:00Z",
             "completed_at": "2026-10-23T12:07:00Z"},
        ]
        result = measure_head(pr, [], checks, [])
        self.assertEqual(result["outcome"], "failure")
        self.assertIsNone(result["minutes"])


class TestPagination(unittest.TestCase):
    def test_second_page_is_included_and_bound_is_reported(self):
        pages = [
            {"total_count": 3, "workflow_runs": [{"id": 1}, {"id": 2}]},
            {"total_count": 3, "workflow_runs": [{"id": 3}]},
        ]
        with mock.patch.object(gha_census_enrichment, "PAGE_SIZE", 2), \
                mock.patch.object(gha_census_enrichment, "_api",
                                  side_effect=pages):
            rows = gha_census_enrichment._pages("runs", "workflow_runs")
        self.assertEqual([row["id"] for row in rows], [1, 2, 3])
        with mock.patch.object(gha_census_enrichment, "PAGE_SIZE", 2), \
                mock.patch.object(gha_census_enrichment, "MAX_PAGES", 1), \
                mock.patch.object(gha_census_enrichment, "_api",
                                  return_value=pages[0]):
            with self.assertRaisesRegex(RuntimeError, "page bound"):
                gha_census_enrichment._pages("runs", "workflow_runs")


class TestIncompletePages(unittest.TestCase):
    def test_short_rest_page_cannot_override_reported_total(self) -> None:
        with mock.patch.object(gha_census_enrichment, "_api",
                               return_value={"total_count": 2, "jobs": [{"id": 1}]}):
            with self.assertRaisesRegex(RuntimeError, "incomplete"):
                gha_census_enrichment._pages("jobs", "jobs")


class TestApiHistory(unittest.TestCase):
    def test_required_green_requests_all_attempts_for_historical_merge(self) -> None:
        end = datetime(2026, 10, 23, 12, 30, tzinfo=timezone.utc)
        pr = {"number": 1, "head": {"sha": "head"},
              "merged_at": "2026-10-23T12:10:00Z"}
        checks = [{"name": name, "conclusion": "success",
                   "started_at": "2026-10-23T12:01:00Z",
                   "completed_at": "2026-10-23T12:04:00Z"}
                  for name in ("go-core-complete", "go-race-complete")]
        later_retry = {"name": "go-core-complete", "conclusion": "failure",
                       "started_at": "2026-10-23T12:20:00Z",
                       "completed_at": "2026-10-23T12:21:00Z"}

        def api(path: str):
            if "/actions/runs?" in path:
                return {"total_count": 1, "workflow_runs": [
                    {"created_at": "2026-10-23T12:00:00Z"}]}
            if "/check-runs" in path:
                rows = checks + [later_retry] if "filter=all" in path else [later_retry]
                return {"total_count": len(rows), "check_runs": rows}
            return [{"context": "required-gates-complete", "state": "success",
                     "description": "All path-selected blocking gates passed",
                     "created_at": "2026-10-23T12:05:00Z"}]

        with mock.patch.object(gha_census_enrichment, "_merged_prs", return_value=[pr]), \
                mock.patch.object(gha_census_enrichment, "_api", side_effect=api):
            result = gha_census_enrichment.required_green(end-timedelta(hours=1), end)
        self.assertEqual(result["outcomes"], {"success": 1})
        self.assertEqual(result["heads"][0]["minutes"], 5.0)


class TestOverallRequiredGreen(unittest.TestCase):
    def test_overall_success_percentiles_exclude_censored_heads(self):
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        prs = [{"head": {"sha": str(number)}} for number in (1, 2, 3)]
        heads = [
            {"kind": "ordinary", "outcome": "success", "minutes": 10.0},
            {"kind": "reuse", "outcome": "success", "minutes": 30.0},
            {"kind": "unknown", "outcome": "pending", "minutes": None},
        ]
        with mock.patch.object(gha_census_enrichment, "_merged_prs",
                               return_value=prs), \
                mock.patch.object(gha_census_enrichment, "_pages",
                                  return_value=[]), \
                mock.patch.object(gha_census_enrichment, "measure_head",
                                  side_effect=heads):
            result = gha_census_enrichment.required_green(
                end - timedelta(hours=1), end)
        self.assertEqual(result["overall"],
                         {"heads": 3, "success": 2, "p50_minutes": 10.0,
                          "p95_minutes": 30.0})


class TestPercentiles(unittest.TestCase):
    def test_nearest_rank_uses_only_observed_successes(self):
        self.assertIsNone(percentile([], 0.95))
        self.assertEqual(percentile([2.0, 4.0, 6.0], 0.5), 4.0)
        self.assertEqual(percentile([2.0, 4.0, 6.0], 0.95), 6.0)


class TestCancellation(unittest.TestCase):
    def test_native_copied_successful_attempt_counts_one_interval(self):
        # Native 40058877874 jobs: copied attempt-1 success in attempt 2.
        first = {"id": 120767825444, "run_id": 40058877874,
                 "run_attempt": 1, "status": "completed",
                 "conclusion": "success", "name": "queue-selection / select",
                 "runner_id": 1000613418,
                 "started_at": "2026-10-24T03:34:48Z",
                 "completed_at": "2026-10-24T03:34:53Z"}
        second = dict(first, id=120775668245, run_attempt=2)
        jobs = [first, second]
        self.assertEqual(classify_cancellation(2, jobs), ("executed", 0.08))
        self.assertEqual(classify_cancellation(3, jobs), ("unknown", None))

        end = datetime(2026, 10, 24, 4, tzinfo=timezone.utc)
        with mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                               return_value=[{"id": 40058877874, "run_attempt": 2,
                                              "check_suite_node_id": "suite"}]), \
                mock.patch.object(gha_census_enrichment, "_suite_counts",
                                  return_value={"suite": 1}), \
                mock.patch.object(gha_census_enrichment, "_pages",
                                  return_value=jobs) as pages:
            result = gha_census_enrichment.cancellation_cost(
                end - timedelta(hours=1), end,
                [{"databaseId": 40058877874, "workflowName": "Required Gates",
                  "conclusion": "cancelled"}])
        self.assertEqual(pages.call_count, 1)
        self.assertEqual(result["runs"], 1)
        self.assertEqual(result["categories"], {"executed": 1})
        self.assertEqual(result["evidenced_runner_minutes"], 0.08)

    def test_copied_job_needs_distinct_successful_attempts(self):
        first = {"id": 1, "run_id": 10, "run_attempt": 1,
                 "status": "completed", "conclusion": "success",
                 "name": "select", "runner_id": 3,
                 "started_at": "2026-10-24T03:34:48Z",
                 "completed_at": "2026-10-24T03:34:53Z"}
        copy = dict(first, id=2, run_attempt=2)
        for changes in ({"runner_id": 4}, {"name": "other"},
                        {"completed_at": "2026-10-24T03:34:54Z"},
                        {"run_id": 11}):
            with self.subTest(changes=changes):
                other = dict(copy, **changes)
                expected = 0.18 if "completed_at" in changes else 0.17
                self.assertEqual(classify_cancellation(2, [first, other]),
                                 ("executed", expected))
        for changes in ({"run_attempt": 1}, {"status": "in_progress"},
                        {"conclusion": "failure"},
                        {"id": None}, {"run_id": None}, {"name": None},
                        {"completed_at": None}):
            with self.subTest(ambiguous=changes):
                self.assertEqual(classify_cancellation(
                    2, [first, dict(copy, **changes)]), ("unknown", None))
        zero = dict(first, completed_at=first["started_at"])
        zero_copy = dict(copy, completed_at=first["started_at"])
        self.assertEqual(classify_cancellation(2, [zero, zero_copy]),
                         ("unknown", None))

    def test_same_native_job_id_with_different_interval_is_unknown(self):
        first = {"id": 120767825444, "run_id": 40058877874,
                 "run_attempt": 1, "status": "completed",
                 "conclusion": "success", "name": "queue-selection / select",
                 "runner_id": 1000613418,
                 "started_at": "2026-10-24T03:34:48Z",
                 "completed_at": "2026-10-24T03:34:53Z"}
        conflicting = dict(first, run_attempt=2,
                           started_at="2026-10-24T03:34:54Z",
                           completed_at="2026-10-24T03:34:59Z")
        self.assertEqual(classify_cancellation(2, [first, conflicting]),
                         ("unknown", None))

    def test_workflow_categories_include_missing_budget_and_failed_jobs(self):
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        names = {1: "Required Gates", 2: "Required Gates",
                 3: "Required Gates", 4: "Build Test", 5: None}
        raw = [{"databaseId": rid, "workflowName": name,
                "conclusion": "cancelled"} for rid, name in names.items()]
        fetched = [{"id": rid, "check_suite_node_id": f"suite-{rid}"}
                   for rid in (1, 2, 3, 5, 6)]
        job = {"runner_id": 1, "started_at": "2026-10-23T12:00:00Z",
               "completed_at": "2026-10-23T12:01:00Z"}

        def jobs(path, array_key, page_limit):
            if "/3/jobs" in path:
                raise RuntimeError("incomplete jobs page")
            return [job]

        with mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                               return_value=fetched) as cancelled_api, \
                mock.patch.object(gha_census_enrichment, "_suite_counts",
                                  return_value={"suite-1": 0, "suite-2": 1,
                                                "suite-3": 1, "suite-5": 1}) as suites_api, \
                mock.patch.object(gha_census_enrichment,
                                  "MAX_JOB_ENRICHMENTS", 2), \
                mock.patch.object(gha_census_enrichment, "_pages",
                                  side_effect=jobs) as pages:
            result = gha_census_enrichment.cancellation_cost(
                end - timedelta(hours=1), end, raw)
        self.assertEqual(cancelled_api.call_count, 1)
        self.assertEqual(suites_api.call_count, 1)
        self.assertEqual(pages.call_count, 2)
        self.assertEqual(result["categories"],
                         {"zero_job": 1, "executed": 1, "unknown": 3})
        self.assertEqual(result["by_workflow"]["Required Gates"],
                         {"runs": 3, "categories": {
                             "zero_job": 1, "executed": 1, "unknown": 1}})
        self.assertEqual(result["by_workflow"]["Build Test"],
                         {"runs": 1, "categories": {"unknown": 1}})
        self.assertEqual(result["by_workflow"]["Unknown workflow"],
                         {"runs": 1, "categories": {"unknown": 1}})
        self.assertEqual(result["source_drift"],
                         {"missing_from_rest": 1, "late_cancelled": 1})
        self.assertEqual(sum(group["runs"] for group in
                             result["by_workflow"].values()), result["runs"])
        for category, count in result["categories"].items():
            self.assertEqual(sum(group["categories"].get(category, 0)
                                 for group in result["by_workflow"].values()),
                             count)

    def test_failed_jobs_page_keeps_other_cancelled_run(self):
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        runs = [{"databaseId": rid, "conclusion": "cancelled"}
                for rid in (1, 2)]
        fetched = [{"id": rid, "check_suite_node_id": f"suite-{rid}"}
                   for rid in (1, 2)]
        job = {"runner_id": 5, "started_at": "2026-10-23T12:00:00Z",
               "completed_at": "2026-10-23T12:01:00Z"}

        def jobs(path, array_key, page_limit):
            if "/1/jobs" in path:
                raise RuntimeError("GitHub API incomplete page for run 1")
            return [job]

        with mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                               return_value=fetched), \
                mock.patch.object(gha_census_enrichment, "_suite_counts",
                                  return_value={"suite-1": 1, "suite-2": 1}), \
                mock.patch.object(gha_census_enrichment, "_pages",
                                  side_effect=jobs) as pages:
            result = gha_census_enrichment.cancellation_cost(
                end - timedelta(hours=1), end, runs)
        self.assertEqual(pages.call_count, 2)
        self.assertEqual(result["runs"], 2)
        self.assertEqual(result["categories"], {"unknown": 1, "executed": 1})
        self.assertEqual(result["evidenced_runner_minutes"], 1.0)
        self.assertEqual(result["job_enrichments"], 2)
        self.assertEqual(result["job_fetch_errors"], 1)
        self.assertEqual(result["job_fetch_error_examples"][0]["run_id"], 1)

    def test_later_cancellation_does_not_change_raw_snapshot(self):
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        runs = [{"databaseId": 1, "conclusion": "cancelled"},
                {"databaseId": 2, "conclusion": None}]
        later = [{"id": 1, "check_suite_node_id": "suite-1"},
                 {"id": 2, "check_suite_node_id": "suite-2"}]
        with mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                               return_value=later), \
                mock.patch.object(gha_census_enrichment, "_suite_counts",
                                  return_value={"suite-1": 0}):
            result = gha_census_enrichment.cancellation_cost(
                end - timedelta(hours=1), end, runs)
        self.assertEqual(result["runs"], 1)
        self.assertEqual(result["categories"], {"zero_job": 1})
        self.assertEqual(result["source_drift"]["late_cancelled"], 1)

    def test_job_enrichment_budget_marks_remaining_runs_unknown(self):
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        runs = [{"databaseId": rid, "conclusion": "cancelled"}
                for rid in (1, 2)]
        fetched = [{"id": rid, "check_suite_node_id": f"suite-{rid}"}
                   for rid in (1, 2)]
        job = {"runner_id": 1, "started_at": "2026-10-23T12:00:00Z",
               "completed_at": "2026-10-23T12:01:00Z"}
        with mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                               return_value=fetched), \
                mock.patch.object(gha_census_enrichment, "_suite_counts",
                                  return_value={"suite-1": 1, "suite-2": 1}), \
                mock.patch.object(gha_census_enrichment,
                                  "MAX_JOB_ENRICHMENTS", 1), \
                mock.patch.object(gha_census_enrichment, "_pages",
                                  return_value=[job]) as pages:
            result = gha_census_enrichment.cancellation_cost(
                end - timedelta(hours=1), end, runs)
        self.assertEqual(pages.call_count, 1)
        self.assertEqual(result["categories"], {"executed": 1, "unknown": 1})
        self.assertEqual(result["job_budget_exhausted"], 1)

    def test_rerun_with_empty_latest_suite_fetches_all_attempt_jobs(self):
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        runs = [{"databaseId": 1, "conclusion": "cancelled"}]
        fetched = [{"id": 1, "run_attempt": 2,
                    "check_suite_node_id": "suite-1"}]
        job = {"runner_id": 1, "started_at": "2026-10-23T12:00:00Z",
               "completed_at": "2026-10-23T12:01:00Z"}
        with mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                               return_value=fetched), \
                mock.patch.object(gha_census_enrichment, "_suite_counts",
                                  return_value={"suite-1": 0}), \
                mock.patch.object(gha_census_enrichment, "_pages",
                                  return_value=[job]) as pages:
            result = gha_census_enrichment.cancellation_cost(
                end - timedelta(hours=1), end, runs)
        self.assertEqual(pages.call_count, 1)
        self.assertEqual(result["categories"], {"executed": 1})
        self.assertEqual(result["evidenced_runner_minutes"], 1.0)

    def test_production_accounting_rejects_fewer_rest_jobs_than_suite(self) -> None:
        end = datetime(2026, 10, 23, tzinfo=timezone.utc)
        job = {"runner_id": 1, "started_at": "2026-10-23T12:00:00Z",
               "completed_at": "2026-10-23T12:01:00Z"}
        for attempt in (1, 2):
            with self.subTest(attempt=attempt), \
                    mock.patch.object(gha_census_enrichment, "_cancelled_runs",
                                      return_value=[{"id": 1, "run_attempt": attempt,
                                                     "check_suite_node_id": "suite"}]), \
                    mock.patch.object(gha_census_enrichment, "_suite_counts",
                                      return_value={"suite": 2}), \
                    mock.patch.object(gha_census_enrichment, "_api",
                                      return_value={"total_count": 1, "jobs": [job]}):
                result = gha_census_enrichment.cancellation_cost(
                    end-timedelta(hours=1), end,
                    [{"databaseId": 1, "conclusion": "cancelled"}])
            self.assertEqual(result["categories"], {"unknown": 1})
            self.assertEqual(result["evidenced_runner_minutes"], 0.0)

    def test_unallocated_runner_zero_is_unknown(self):
        job = {"runner_id": 0, "runner_name": "",
               "started_at": "2026-10-23T12:00:00Z",
               "completed_at": "2026-10-23T12:00:04Z"}
        self.assertEqual(classify_cancellation(1, [job]), ("unknown", None))

    def test_zero_jobs_and_runner_time_are_distinct(self):
        self.assertEqual(classify_cancellation(0, []), ("zero_job", 0.0))
        jobs = [
            {"runner_id": None, "started_at": None, "completed_at": None},
            {"runner_id": 1, "started_at": "2026-10-23T12:00:00Z",
             "completed_at": "2026-10-23T12:21:33Z"},
        ]
        self.assertEqual(classify_cancellation(2, jobs),
                         ("executed", 21.55))
        self.assertEqual(classify_cancellation(2, jobs[:1]), ("unknown", None))
        self.assertEqual(classify_cancellation(2, jobs + [
            {"runner_id": 2, "started_at": None, "completed_at": None}]),
                         ("unknown", None))


if __name__ == "__main__":
    unittest.main()
