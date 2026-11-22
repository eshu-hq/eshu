#!/usr/bin/env python3
"""Tests for scripts/dev/gate-timing.py and scripts/lib/gate-timing.sh.

Covers the local run record (pre-push / pre-pr), its fail-open behavior, the
cross-run summary, and the CI job aggregation. Nothing here needs the network:
the CI path is tested through its pure aggregation function.
"""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
TOOL = ROOT / "scripts/dev/gate-timing.py"
LIB = ROOT / "scripts/lib/gate-timing.sh"


def load_tool():
    """Import gate-timing.py (the filename has a hyphen) as a module."""
    spec = importlib.util.spec_from_file_location("gate_timing", TOOL)
    module = importlib.util.module_from_spec(spec)
    # The tool uses `from __future__ import annotations`, which makes the
    # JobGroup dataclass annotations strings. dataclasses looks the module up in
    # sys.modules to resolve them (Python 3.9 to 3.14) and fails on None, so the
    # module has to be registered before it runs.
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


GATE_REPORT = {
    "schema_version": "v1",
    "commands": [
        {"gate_id": "go-lint", "kind": "gate", "outcome": "pass", "duration_ms": 41000},
        {"gate_id": "go-lint", "kind": "test_command", "outcome": "skipped",
         "skip_reason": "no self-test trigger"},
        {"gate_id": "docs-build", "kind": "gate", "outcome": "fail", "duration_ms": 9000},
        {"gate_id": "go-vet", "kind": "gate", "outcome": "pass",
         "reused_from": "go-lint", "source_duration_ms": 30000},
    ],
}

PLAIN_LOG = (
    "ok  \tgithub.com/eshu-hq/eshu/go/internal/query\t12.500s\n"
    "ok  \tgithub.com/eshu-hq/eshu/go/internal/mcp\t(cached)\n"
    "?   \tgithub.com/eshu-hq/eshu/go/internal/doc\t[no test files]\n"
    "FAIL\tgithub.com/eshu-hq/eshu/go/internal/status\t3.250s\n"
)
RACE_LOG = "ok  \tgithub.com/eshu-hq/eshu/go/internal/query\t54.000s\n"


class RecordTest(unittest.TestCase):
    """`gate-timing.py record` writes exactly one faithful JSONL line."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        (self.dir / "steps.tsv").write_text(
            "go test (changed packages)\t95\t0\n"
            "selected local gates\t610\t1\n"
        )
        (self.dir / "meta.tsv").write_text("changed_go_packages\t3\n")
        (self.dir / "gates.json").write_text(json.dumps(GATE_REPORT))
        (self.dir / "plain.log").write_text(PLAIN_LOG)
        (self.dir / "race.log").write_text(RACE_LOG)
        self.out = self.dir / "store"

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def record(self, **env: str) -> subprocess.CompletedProcess:
        full = {**os.environ, "ESHU_GATE_TIMING_DIR": str(self.out), **env}
        return subprocess.run(
            ["python3", "-I", str(TOOL), "record", "--script", "pre-push",
             "--base", "origin/main", "--overall", "1",
             "--steps", str(self.dir / "steps.tsv"),
             "--meta", str(self.dir / "meta.tsv"),
             "--gate-report", str(self.dir / "gates.json"),
             "--test-log", str(self.dir / "plain.log"),
             "--race-log", str(self.dir / "race.log"),
             "--start-epoch", "1000", "--load-start", "2.5"],
            capture_output=True, text=True, env=full, check=False,
        )

    def test_writes_one_line_with_steps_gates_and_packages(self) -> None:
        result = self.record()
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = (self.out / "runs.jsonl").read_text().splitlines()
        self.assertEqual(len(lines), 1)
        run = json.loads(lines[0])
        self.assertEqual(run["schema"], "eshu-gate-timing/v1")
        self.assertEqual(run["script"], "pre-push")
        self.assertEqual(run["overall"], 1)
        self.assertEqual(run["load1_start"], 2.5)
        self.assertEqual(run["meta"], {"changed_go_packages": "3"})
        self.assertEqual(
            run["steps"],
            [{"name": "go test (changed packages)", "seconds": 95, "exit": 0},
             {"name": "selected local gates", "seconds": 610, "exit": 1}],
        )

    def test_gates_keep_only_commands_that_actually_ran(self) -> None:
        self.record()
        run = json.loads((self.out / "runs.jsonl").read_text())
        ids = [(g["gate_id"], g["outcome"]) for g in run["gates"]]
        # The skipped and the reused rows cost no time this run, so they are
        # not counted as time spent.
        self.assertEqual(ids, [("go-lint", "pass"), ("docs-build", "fail")])
        self.assertEqual(run["gates"][0]["seconds"], 41.0)

    def test_packages_parse_ok_fail_and_ignore_cached_and_no_tests(self) -> None:
        self.record()
        run = json.loads((self.out / "runs.jsonl").read_text())
        got = sorted((p["mode"], p["pkg"].rsplit("/", 1)[-1], p["seconds"])
                     for p in run["packages"])
        self.assertEqual(
            got,
            [("race", "query", 54.0), ("test", "query", 12.5),
             ("test", "status", 3.25)],
        )

    def test_prints_slowest_steps_table(self) -> None:
        result = self.record()
        self.assertIn("slowest steps", result.stdout)
        self.assertLess(result.stdout.index("selected local gates"),
                        result.stdout.index("go test (changed packages)"))

    def test_unwritable_store_fails_open(self) -> None:
        blocker = self.dir / "blocker"
        blocker.write_text("a file, not a directory")
        result = self.record(ESHU_GATE_TIMING_DIR=str(blocker / "sub"))
        self.assertEqual(result.returncode, 0)
        self.assertIn("timing not recorded", result.stderr)

    def test_missing_inputs_still_record(self) -> None:
        for name in ("gates.json", "plain.log", "race.log", "meta.tsv"):
            (self.dir / name).unlink()
        result = self.record()
        self.assertEqual(result.returncode, 0, result.stderr)
        run = json.loads((self.out / "runs.jsonl").read_text())
        self.assertEqual((run["gates"], run["packages"], run["meta"]), ([], [], {}))

    def test_disabled_writes_nothing(self) -> None:
        result = self.record(ESHU_GATE_TIMING="0")
        self.assertEqual(result.returncode, 0)
        self.assertFalse((self.out / "runs.jsonl").exists())

    def test_a_report_with_null_commands_still_records(self) -> None:
        # ci-gates marshals a nil command list as `null` when no gate was
        # selected (go/cmd/ci-gates/run_report.go); iterating it crashed the
        # recorder and dropped the whole run.
        (self.dir / "gates.json").write_text('{"schema_version": "v1", "commands": null}')
        result = self.record()
        self.assertEqual(result.returncode, 0, result.stderr)
        run = json.loads((self.out / "runs.jsonl").read_text())
        self.assertEqual(run["gates"], [])
        self.assertEqual(len(run["steps"]), 2)

    def test_a_command_without_a_duration_counts_zero_seconds(self) -> None:
        (self.dir / "gates.json").write_text(json.dumps({"commands": [
            {"gate_id": "g", "kind": "gate", "outcome": "pass", "duration_ms": None}]}))
        result = self.record()
        self.assertEqual(result.returncode, 0, result.stderr)
        run = json.loads((self.out / "runs.jsonl").read_text())
        self.assertEqual(run["gates"][0]["seconds"], 0.0)

    def test_success_message_shows_a_home_relative_path(self) -> None:
        tool = load_tool()
        self.assertEqual(tool.display_path(Path.home() / ".cache" / "x" / "runs.jsonl"),
                         "~/.cache/x/runs.jsonl")
        self.assertEqual(tool.display_path(Path("/elsewhere/runs.jsonl")),
                         "/elsewhere/runs.jsonl")


class SummaryTest(unittest.TestCase):
    """`summary` reports medians across runs and flags noisy hosts."""

    def test_median_max_and_noisy_runs(self) -> None:
        tool = load_tool()

        def run(secs: int, load: float) -> dict:
            return {"script": "pre-push", "load1_end": load, "overall": 0,
                    "steps": [{"name": "lint", "seconds": secs, "exit": 0}],
                    "gates": [], "packages": []}

        text = tool.render_summary([run(10, 1.0), run(30, 2.0), run(20, 12.0)], top=5)
        self.assertIn("3 run(s)", text)
        self.assertIn("1 noisy", text)
        row = next(line for line in text.splitlines() if line.startswith("lint"))
        self.assertEqual(row.split()[1:4], ["3", "20", "30"])

    def test_one_truncated_store_line_does_not_hide_the_rest(self) -> None:
        tool = load_tool()
        good = json.dumps({"script": "pre-push", "steps": []})
        runs, bad = tool.parse_store_lines([good, '{"script": "pre-pu', "", good, "[1]"])
        self.assertEqual((len(runs), bad), (2, 2))


class CiAggregateTest(unittest.TestCase):
    """CI job rows collapse shards and report job-minutes per run."""

    def test_groups_shards_and_reports_job_minutes(self) -> None:
        tool = load_tool()
        rows = []
        for run_id in ("1", "2"):
            for shard in range(1, 5):
                rows.append({"run": run_id, "name": f"go-race ({shard})",
                             "queue": 2, "seconds": 600})
            rows.append({"run": run_id, "name": "tagged-builds", "queue": 3,
                         "seconds": 2000})
        groups = tool.aggregate_jobs(rows)
        race = groups["go-race (N)"]
        self.assertEqual((race.per_run, race.median_s, race.max_s), (4.0, 600, 600))
        self.assertAlmostEqual(race.minutes_per_run, 40.0)
        self.assertEqual(groups["tagged-builds"].median_s, 2000)

    def test_real_shard_spellings_collapse(self) -> None:
        # Job names copied from a real Build Test run: the shard marker sits
        # before the parenthesis in one family and inside it in another.
        tool = load_tool()
        self.assertEqual(
            tool.normalize_job("call-ifa-determinism-gate / fault-injection (shard 6/15)"),
            tool.normalize_job("call-ifa-determinism-gate / fault-injection (shard 1/15)"))
        self.assertEqual(tool.normalize_job("live-postgres-readiness shard (5/8)"),
                         tool.normalize_job("live-postgres-readiness shard (1/8)"))
        self.assertEqual(tool.normalize_job("go-race (3)"), "go-race (N)")

    def test_jobs_from_two_pages_are_all_read(self) -> None:
        # `gh api --paginate --jq` runs the filter once per page, so a run with
        # more than 100 jobs prints several results. One document per job line
        # is the shape that survives that.
        tool = load_tool()
        job = ('{"name": "%s", "created_at": "2026-01-01T00:00:00Z", '
               '"started_at": "2026-01-01T00:00:02Z", "completed_at": "2026-01-01T00:10:02Z"}')
        page_one = "\n".join(job % f"job-{n}" for n in range(100))
        page_two = "\n".join(job % f"job-{n}" for n in range(100, 130))
        calls = []

        def fake_gh_text(argv):
            calls.append(argv)
            return page_one + "\n" + page_two + "\n"

        tool.gh_text = fake_gh_text
        rows = tool.fetch_job_rows("o/r", "42")
        self.assertEqual(len(rows), 130)
        self.assertEqual((rows[0]["queue"], rows[0]["seconds"]), (2.0, 600.0))
        # The jq filter must emit one object per job, not one array per page.
        jq = calls[0][calls[0].index("--jq") + 1]
        self.assertTrue(jq.startswith(".jobs[]"), jq)
        self.assertFalse(jq.lstrip().startswith("["), jq)
        # GitHub answers per_page=100 with a 502 for the heaviest Build Test
        # runs (3 of the 8 most recent did, on every retry) and returns the same
        # jobs at per_page=50, so the request must ask for 50.
        self.assertIn("per_page=50", calls[0][1])
        self.assertNotIn("per_page=100", calls[0][1])


class ShellLibTest(unittest.TestCase):
    """The sourced shell lib records steps and never changes the exit code."""

    def drive(self, store: Path, body: str) -> subprocess.CompletedProcess:
        script = (
            f'set -uo pipefail; pre_pr_state_dir="$(mktemp -d)"; '
            f'source "{LIB}"; eshu_timing_init pre-push origin/main; {body}'
        )
        env = {**os.environ, "ESHU_GATE_TIMING_DIR": str(store)}
        return subprocess.run(["bash", "-c", script], capture_output=True,
                              text=True, env=env, check=False)

    def test_steps_and_failure_are_recorded_and_caller_exit_is_unchanged(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = Path(tmp) / "store"
            result = self.drive(
                store,
                'eshu_timing_note "good step" 4 0; eshu_timing_note "bad step" 9 1; '
                'eshu_timing_meta changed_go_packages 2; '
                'eshu_timing_finish 1; echo "rc=$?"',
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("rc=0", result.stdout)  # finish itself never fails
            run = json.loads((store / "runs.jsonl").read_text())
            self.assertEqual([s["exit"] for s in run["steps"]], [0, 1])
            self.assertEqual(run["overall"], 1)
            self.assertEqual(run["meta"], {"changed_go_packages": "2"})

    def test_a_broken_recorder_cannot_break_the_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            blocker = Path(tmp) / "blocker"
            blocker.write_text("x")
            result = self.drive(blocker / "sub",
                                'eshu_timing_note s 1 0; eshu_timing_finish 0; echo "rc=$?"')
            self.assertIn("rc=0", result.stdout)


if __name__ == "__main__":
    unittest.main()
