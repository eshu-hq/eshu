#!/usr/bin/env python3
"""Focused contract tests for live backend shard coverage and reports."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).parent / "lib" / "live_backend_shards.py"
SPEC = importlib.util.spec_from_file_location("live_backend_shards", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
shards = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = shards
SPEC.loader.exec_module(shards)
HEAD = "a" * 40


def run(backend: str, name: str, tests: str = "TestOne") -> str:
    """Build a synthetic runner plan line."""
    compose = f"docker-compose.live-backend-{backend}.yml"
    return f"{backend}|{compose}|go/pkg/{name}_live_test.go|./pkg|{tests}"


def event(line: str, status: int = 0,
          durations: tuple[float, float, float] = (1.0, 2.0, 3.0)) -> dict[str, object]:
    """Build a terminal event with measured phase durations."""
    setup, test, cleanup = durations
    return {"run": line, "status": status, "setup_seconds": setup,
            "test_seconds": test, "cleanup_seconds": cleanup}


class ShardContractTest(unittest.TestCase):
    """Exercise planner and fail-closed report accounting."""

    def setUp(self) -> None:
        self.plan = [run("nornicdb", "a"), run("neo4j", "b", "TestOne|TestTwo"),
                     run("nornicdb", "c"), run("neo4j", "d"),
                     run("neo4j", "e"), run("neo4j", "f"),
                     run("neo4j", "g")]
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)

    def invoke(self, *args: str, input_text: str | None = None) -> subprocess.CompletedProcess[str]:
        """Run the public command line as the CI shell does."""
        return subprocess.run([sys.executable, str(SCRIPT), *args], input=input_text,
                              text=True, capture_output=True, check=False)

    def write_plan(self) -> Path:
        """Write a canonical full plan."""
        path = self.directory / "plan.txt"
        path.write_text("\n".join(self.plan) + "\n", encoding="utf-8")
        return path

    def write_report(self, shard: str, events: list[dict[str, object]],
                     head: str = HEAD) -> Path:
        """Run report CLI and leave parseable evidence even on failure."""
        event_path = self.directory / f"{shard[0]}-events.jsonl"
        report_path = self.directory / f"{shard[0]}-report.json"
        event_path.write_text("".join(json.dumps(item) + "\n" for item in events),
                              encoding="utf-8")
        result = self.invoke("report", "--head", head, "--shard", shard,
                             "--plan", str(self.write_plan()), "--events",
                             str(event_path), "--output", str(report_path))
        self.assertTrue(report_path.exists(), result.stderr)
        return report_path

    def good_reports(self) -> list[Path]:
        """Create successful evidence for all four shards."""
        return [self.write_report(shard, [event(line) for line in
                shards.partition_runs(self.plan, shard)])
                for shard in ("1/4", "2/4", "3/4", "4/4")]

    def verify(self, reports: list[Path]) -> subprocess.CompletedProcess[str]:
        """Invoke aggregate coverage gate."""
        return self.invoke("verify", "--head", HEAD, "--plan", str(self.write_plan()),
                           *[str(report) for report in reports])

    def test_partition_preserves_pins_order_and_multitest(self) -> None:
        self.assertEqual(shards.partition_runs(self.plan, "all"), self.plan)
        self.assertEqual(shards.partition_runs(self.plan, "1/4"), self.plan[0:3:2])
        self.assertEqual(shards.partition_runs(self.plan, "2/4"), [self.plan[1], self.plan[3]])
        self.assertEqual(shards.partition_runs(self.plan, "3/4"), [self.plan[4], self.plan[5]])
        self.assertEqual(shards.partition_runs(self.plan, "4/4"), [self.plan[6]])

    def test_future_row_stays_covered(self) -> None:
        expanded = [*self.plan, run("neo4j", "future")]
        assigned = [line for shard in ("1/4", "2/4", "3/4", "4/4")
                    for line in shards.partition_runs(expanded, shard)]
        self.assertCountEqual(assigned, expanded)

    def test_future_nonstandard_live_filename_stays_covered(self) -> None:
        future = run("neo4j", "future").replace("future_live_test.go",
                                                       "future_test.go")
        expanded = [*self.plan, future]
        assigned = [line for shard in ("1/4", "2/4", "3/4", "4/4")
                    for line in shards.partition_runs(expanded, shard)]
        self.assertCountEqual(assigned, expanded)

    def test_invalid_plan_and_shard_fail(self) -> None:
        for plan, shard in (([], "all"), (self.plan * 2, "all"),
                            (["bad"], "all"), ([run("neo4j", "x").replace(
                                "backend-neo4j", "backend-nornicdb")], "all"),
                            (self.plan, "5/4")):
            with self.subTest(plan=plan, shard=shard), self.assertRaises(ValueError):
                shards.partition_runs(plan, shard)

    def test_plan_cli_selects_lines(self) -> None:
        result = self.invoke("plan", "--shard", "3/4", input_text="\n".join(self.plan) + "\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines(), [self.plan[4], self.plan[5]])

    def test_report_and_verify_green(self) -> None:
        reports = self.good_reports()
        self.assertEqual(self.verify(reports).returncode, 0)
        result = json.loads(reports[1].read_text(encoding="utf-8"))
        self.assertEqual(result["runs"][0]["run"], self.plan[1])
        self.assertEqual(result["runs"][0]["test_seconds"], 2.0)

    def test_missing_duplicate_unexpected_failed_events_fail_report(self) -> None:
        selected = shards.partition_runs(self.plan, "2/4")
        cases = ([], [event(selected[0]), event(selected[0])],
                 [event(line) for line in selected] + [event(self.plan[0])],
                 [event(selected[0], 1), event(selected[1])])
        for index, events in enumerate(cases):
            with self.subTest(index=index):
                path = self.directory / "events.jsonl"
                output = self.directory / "result.json"
                path.write_text("".join(json.dumps(item) + "\n" for item in events),
                                encoding="utf-8")
                result = self.invoke("report", "--head", HEAD, "--shard", "2/4",
                                     "--plan", str(self.write_plan()), "--events",
                                     str(path), "--output", str(output))
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(json.loads(output.read_text(encoding="utf-8"))["ok"])

    def test_verify_rejects_stale_head_missing_and_duplicate_shards(self) -> None:
        reports = self.good_reports()
        self.assertNotEqual(self.verify(reports[:-1]).returncode, 0)
        absent = self.directory / "cancelled-shard.json"
        self.assertNotEqual(self.verify([*reports[:3], absent]).returncode, 0)
        self.assertNotEqual(self.verify([*reports[:3], reports[0]]).returncode, 0)
        stale = self.write_report("4/4", [event(self.plan[6])], "b" * 40)
        self.assertNotEqual(self.verify([*reports[:3], stale]).returncode, 0)

    def test_expand_backend_pins_and_complete_test_expressions(self) -> None:
        """Pure planning preserves canonical backend order and multi-Test rows."""
        targets = "go/pkg/a_live_test.go|./pkg|TestOne|TestTwo|both\n" + (
            "go/pkg/b_live_test.go|./pkg|TestOne|neo4j\n")
        expected = [run("nornicdb", "a", "TestOne|TestTwo"),
                    run("neo4j", "a", "TestOne|TestTwo"), run("neo4j", "b")]
        for backend in ("both", "nornicdb", "neo4j"):
            result = self.invoke("expand", "--backend", backend, input_text=targets)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.splitlines(),
                             [line for line in expected if backend == "both" or line.startswith(backend + "|")])
        for bad in ("", targets.replace("|both", "|unknown"), targets + targets,
                    targets.replace("TestTwo", "bad")):
            self.assertNotEqual(self.invoke("expand", "--backend", "both", input_text=bad).returncode, 0)

    def test_report_count_error_names_received_paths_in_order(self) -> None:
        """Distinguish missing coverage from unexpected download nesting."""
        paths = [self.directory / "z.json", self.directory / "a.json"]
        result = self.verify(paths)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(str(sorted(str(path) for path in paths)), result.stderr)
        self.assertIn("expected exactly four shard reports, got 2", result.stderr)

    def test_verify_rejects_failed_shard_report(self) -> None:
        reports = self.good_reports()
        failed = self.write_report("4/4", [event(self.plan[6], 1)])
        self.assertNotEqual(self.verify([*reports[:3], failed]).returncode, 0)

    def test_record_rejects_invalid_data_and_appends_terminal_event(self) -> None:
        path = self.directory / "events.jsonl"
        args = ("record", "--run", self.plan[0], "--status", "0",
                "--setup-seconds", "1", "--test-seconds", "2",
                "--cleanup-seconds", "3", "--events", str(path))
        self.assertEqual(self.invoke(*args).returncode, 0)
        self.assertEqual(json.loads(path.read_text(encoding="utf-8"))["run"], self.plan[0])
        bad = list(args)
        bad[bad.index("--test-seconds") + 1] = "-2"
        self.assertNotEqual(self.invoke(*bad).returncode, 0)

    def test_duplicate_json_keys_and_boolean_schema_version_fail(self) -> None:
        selected = shards.partition_runs(self.plan, "4/4")
        events_path = self.directory / "events.jsonl"
        output = self.directory / "report.json"
        encoded = json.dumps(event(selected[0]))
        events_path.write_text(encoded[:-1] + ',"status":0}\n', encoding="utf-8")
        result = self.invoke("report", "--head", HEAD, "--shard", "4/4",
                             "--plan", str(self.write_plan()), "--events",
                             str(events_path), "--output", str(output))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(json.loads(output.read_text(encoding="utf-8"))["ok"])

        reports = self.good_reports()
        changed = json.loads(reports[0].read_text(encoding="utf-8"))
        changed["schema_version"] = True
        reports[0].write_text(json.dumps(changed), encoding="utf-8")
        self.assertNotEqual(self.verify(reports).returncode, 0)

    def test_invalid_event_encoding_still_writes_failed_report(self) -> None:
        events_path = self.directory / "events.jsonl"
        output = self.directory / "report.json"
        events_path.write_bytes(b"\xff\n")
        result = self.invoke("report", "--head", HEAD, "--shard", "4/4",
                             "--plan", str(self.write_plan()), "--events",
                             str(events_path), "--output", str(output))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(json.loads(output.read_text(encoding="utf-8"))["ok"])

    def layout_report(self, shard: str, events: list[dict[str, object]],
                      attempt: int, run_id: int = 39652375048,
                      head: str = HEAD) -> Path:
        """Write a report under a CI-shaped rerun artifact directory."""
        artifact = self.directory / f"live-backend-{run_id}-{attempt}-{shard[0]}"
        artifact.mkdir(parents=True, exist_ok=True)
        event_path = artifact / "events.jsonl"
        report_path = artifact / "live-backend-result.json"
        event_path.write_text("".join(json.dumps(item) + "\n" for item in events),
                              encoding="utf-8")
        result = self.invoke("report", "--head", head, "--shard", shard,
                             "--plan", str(self.write_plan()), "--events",
                             str(event_path), "--output", str(report_path))
        self.assertTrue(report_path.exists(), result.stderr)
        return report_path

    def select(self, reports: list[Path]) -> tuple[subprocess.CompletedProcess[str], list[Path]]:
        """Run newest-valid selection into a fresh directory per call."""
        self.select_calls = getattr(self, "select_calls", 0) + 1
        out = self.directory / f"selected-{self.select_calls}"
        result = self.invoke("select", "--head", HEAD, "--plan", str(self.write_plan()),
                             "--output-dir", str(out),
                             *[str(report) for report in reports])
        return result, sorted(out.glob("*.json")) if out.is_dir() else []

    def mixed_attempt_layout(self) -> list[Path]:
        """Three attempt-1 greens plus a failed then rerun-green shard 2/4."""
        second = shards.partition_runs(self.plan, "2/4")
        layout = [self.layout_report(shard, [event(line) for line in
                    shards.partition_runs(self.plan, shard)], 1)
                  for shard in ("1/4", "3/4", "4/4")]
        layout.append(self.layout_report("2/4", [event(second[0], 1), event(second[1])], 1))
        layout.append(self.layout_report("2/4", [event(line) for line in second], 2))
        return layout

    def test_verify_still_demands_exactly_four_reports(self) -> None:
        """Raw verify keeps the completeness rule: a rerun layout trips it."""
        result = self.verify(self.mixed_attempt_layout())
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("expected exactly four shard reports, got 5", result.stderr)

    def test_select_mixed_attempts_verifies_green(self) -> None:
        """Rerun-green shard 2/4 reunites with attempt-1 greens (#8724)."""
        layout = self.mixed_attempt_layout()
        result, selected = self.select(layout)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(selected), 4)
        winners = [line for line in result.stdout.splitlines()
                   if line.startswith("live backend select: ") and " <- " in line]
        self.assertEqual(len(winners), 4)
        rerun = str(layout[-1])
        self.assertTrue(any(line.endswith(f"2/4 <- {rerun}") for line in winners))
        self.assertEqual(self.verify(selected).returncode, 0)

    def test_select_prefers_newest_valid_report(self) -> None:
        """Two green attempts for one shard select the rerun bytes."""
        second = shards.partition_runs(self.plan, "2/4")
        older = self.layout_report("2/4", [event(line, durations=(1.0, 2.0, 3.0))
                                           for line in second], 1)
        newer = self.layout_report("2/4", [event(line, durations=(4.0, 5.0, 6.0))
                                           for line in second], 2)
        others = [self.layout_report(shard, [event(line) for line in
                    shards.partition_runs(self.plan, shard)], 1)
                  for shard in ("1/4", "3/4", "4/4")]
        result, selected = self.select([*others, older, newer])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(selected), 4)
        bodies = {path.read_bytes() for path in selected}
        self.assertIn(newer.read_bytes(), bodies)
        self.assertNotIn(older.read_bytes(), bodies)

    def test_select_rejects_stale_newest_report(self) -> None:
        """A shard whose rerun report is stale keeps the aggregate red."""
        second = shards.partition_runs(self.plan, "2/4")
        others = [self.layout_report(shard, [event(line) for line in
                    shards.partition_runs(self.plan, shard)], 1)
                  for shard in ("1/4", "3/4", "4/4")]
        stale = self.layout_report("2/4", [event(line) for line in second],
                                   2, head="b" * 40)
        failed = self.layout_report("2/4", [event(second[0], 1), event(second[1])], 1)
        result, _ = self.select([*others, failed, stale])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no valid report for shard 2/4", result.stderr)

    def test_select_rejects_missing_shard(self) -> None:
        """A genuinely absent shard still fails selection."""
        present = [report for report in self.mixed_attempt_layout()
                   if not str(report.parent.name).endswith("-4")]
        result, _ = self.select(present)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing shards: ['4/4']", result.stderr)
        self.assertNotEqual(self.verify(present).returncode, 0)


if __name__ == "__main__":
    unittest.main()
