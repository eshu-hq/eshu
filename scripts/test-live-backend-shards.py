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


def event(line: str, status: int = 0) -> dict[str, object]:
    """Build a terminal event with measured phase durations."""
    return {"run": line, "status": status, "setup_seconds": 1.0,
            "test_seconds": 2.0, "cleanup_seconds": 3.0}


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


if __name__ == "__main__":
    unittest.main()
