#!/usr/bin/env python3
"""Exercise the diagnostic artifact validator with seeded evidence failures."""

import importlib.util
import pathlib
import json
import subprocess
import tempfile
import unittest

MODULE_PATH = pathlib.Path(__file__).parent / "lib" / "status_diagnostic_artifacts.py"
SPEC = importlib.util.spec_from_file_location("status_diagnostic_artifacts", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def fixture() -> tuple[dict[str, object], dict[str, object]]:
    """Build a complete synthetic 20-request correlation pair."""
    requests: list[dict[str, object]] = []
    samples: list[dict[str, object]] = []
    spans: list[dict[str, object]] = []
    names = ["eshu.status_diagnostic_request", "eshu-api",
             "eshu.status_diagnostic_dispatch", "postgres.reader_access",
             "postgres.status_snapshot", "postgres.reader_access"]
    for i in range(20):
        identifier = str(i + 1)
        start = 1000000000 + i * 10000
        requests.append({"id": identifier, "status_code": 200,
                         "trace_id": f"{i + 1:032x}", "start_unix_nano": start,
                         "end_unix_nano": start + 9000, "post_auth_reached": True})
        samples.append({"server_id": identifier, "phase": "concurrent",
                        "index": i, "status_code": 200,
                        "start_unix_nano": start - 10,
                        "measured_end_unix_nano": start + 9010,
                        "elapsed_ms": 0.00902})
        for j, name in enumerate(names):
            record: dict[str, object] = {
                "request_id": identifier, "name": name,
                "span_id": f"{i * 10 + j + 1:016x}",
                "parent_span_id": "0000000000000000" if j == 0 else f"{i * 10 + 1:016x}",
                "start_unix_nano": start + 1, "end_unix_nano": start + (9001 if j == 0 else 8999)}
            if j == 3:
                record.update(role="writer", stage="writer_checkpoint")
            elif j == 5:
                record.update(role="reader", stage="reader_borrow")
            spans.append(record)
    return ({"schema_version": 1, "complete": True, "overflow": False,
             "requests": requests, "spans": spans, "errors": []},
            {"schema_version": 1, "complete": True, "samples": samples,
             "errors": []})


class ArtifactValidationTest(unittest.TestCase):
    """Require complete and unambiguous client/server correlation."""

    def test_supplementary_plan_contract(self) -> None:
        """Reject private fields and plans recorded during request timing."""
        node = {"Node Type": "Result", "Actual Rows": 1, "Actual Loops": 1,
                "Actual Startup Time": 0, "Actual Total Time": 1}
        report = {"schema_version": 1, "complete": True,
                  "scope": "supplementary execution; no request attribution",
                  "source_sha256": "a" * 64, "as_of_unix_nano": 2,
                  "start_unix_nano": 3, "end_unix_nano": 4,
                  "plan": {"Plan": node, "Planning Time": 0, "Execution Time": 1},
                  "errors": []}
        statements = {"snapshots": [{"end_unix_nano": 1}, {"end_unix_nano": 2}]}
        MODULE.validate_plan(report, statements)
        node["Filter"] = "private"
        with self.assertRaises(ValueError):
            MODULE.validate_plan(report, statements)
        del node["Filter"]
        report["start_unix_nano"] = 1
        with self.assertRaises(ValueError):
            MODULE.validate_plan(report, statements)

    def test_resource_artifact_contract(self) -> None:
        """Reject absent, private, stale, or decreasing resource evidence."""
        _, client = fixture()
        roles = ("api", "postgres_postmaster")
        metadata = {"schema_version": 1, "kind": "metadata", "cadence_seconds": 1,
                    "clock_ticks_per_second": 100, "logical_cpus": 4,
                    "process_start_ticks": {role: 10 for role in roles},
                    "scope": "process ticks exclude children; cgroup totals may include other processes",
                    "cgroup_identity": {role: "a" * 64 for role in roles}}
        samples = []
        for index in range(3):
            processes = {role: {"unix_nano": (index + 1) * 1000000000 - 100,
                               "monotonic_nano": (index + 1) * 1000000000,
                               "read_elapsed_nano": 1, "process_cpu_ticks": index,
                               "cgroup_cpu": {"usage_usec": index, "user_usec": index,
                                              "system_usec": 0}, "cgroup_memory_bytes": 1}
                         for role in roles}
            samples.append({"kind": "sample", "gap_nano": 0 if index == 0 else 1000000000,
                            "processes": processes})
        records = [metadata, *samples, {"kind": "completion", "complete": True,
                                       "samples": 3, "late_gap": False}]
        MODULE.validate_resources(records, client)
        for failure in ("missing", "private", "decreasing", "coverage", "long_final_read"):
            damaged = json.loads(json.dumps(records))
            if failure == "missing":
                damaged.pop()
            elif failure == "private":
                damaged[1]["processes"]["api"]["command"] = "private"
            elif failure == "long_final_read":
                damaged[3]["processes"]["api"]["read_elapsed_nano"] = 1100000000
            elif failure == "decreasing":
                damaged[3]["processes"]["api"]["process_cpu_ticks"] = 0
            else:
                damaged[1]["processes"]["api"]["unix_nano"] = 1000000001
            with self.assertRaises(ValueError):
                MODULE.validate_resources(damaged, client)

    def test_complete_pair(self) -> None:
        """Accept all twenty indexed requests without extra network work."""
        server, client = fixture()
        MODULE.validate_reports(server, client, 20)

    def test_status_read_labels_are_retained(self) -> None:
        """Keep every stored-summary row read label; reject an unknown one."""
        for label in ("active_work_summary_model", "terraform_state_model", "coordinator_model"):
            with self.subTest(label=label):
                server, client = fixture()
                spans = server["spans"]
                assert isinstance(spans, list)
                spans[4]["query_summary"] = label
                MODULE.validate_reports(server, client, 20)
        server, client = fixture()
        spans = server["spans"]
        assert isinstance(spans, list)
        spans[4]["query_summary"] = "coordinator_model_typo"
        with self.assertRaises(ValueError):
            MODULE.validate_reports(server, client, 20)

    def test_missing_spans_with_complete_flag(self) -> None:
        """Reject a producer completion flag with removed span evidence."""
        server, client = fixture()
        server["spans"] = []
        with self.assertRaises(ValueError):
            MODULE.validate_reports(server, client, 20)

    def test_seeded_span_and_time_corruption(self) -> None:
        """Reject unknown payloads, detached spans, and stale mixed-run timing."""
        for failure in ("unknown_field", "detached", "stage", "stale", "time", "stage_names"):
            with self.subTest(failure=failure):
                server, client = fixture()
                spans, samples = server["spans"], client["samples"]
                assert isinstance(spans, list) and isinstance(samples, list)
                if failure == "unknown_field":
                    spans[0]["private_payload"] = "do-not-retain"
                elif failure == "detached":
                    spans[1]["parent_span_id"] = "0000000000009999"
                elif failure == "stage":
                    spans[3]["stage"] = "private-value"
                elif failure == "stage_names":
                    spans[3]["name"] = "postgres.status_snapshot"
                    spans[5]["name"] = "eshu.status_diagnostic_dispatch"
                elif failure == "time":
                    spans[0]["end_unix_nano"] = 1
                else:
                    samples[0]["start_unix_nano"] = 2000000000
                    samples[0]["measured_end_unix_nano"] = 2000009020
                with self.assertRaises(ValueError):
                    MODULE.validate_reports(server, client, 20)

    def test_statement_counter_corruption(self) -> None:
        """Reject reset counters and snapshots inside the timed window."""
        _, client = fixture()
        samples = client["samples"]
        assert isinstance(samples, list)
        first = min(sample["start_unix_nano"] for sample in samples)
        last = max(sample["measured_end_unix_nano"] for sample in samples)
        before = {"dbid": 1, "userid": 2, "queryid": 3, "toplevel": True,
                  "calls": 1, "rows": 2, "blocks": 3, "exec_ms": 4, "stats_since_unix_micro": 100}
        after = dict(before, calls=21, rows=42, blocks=63, exec_ms=24)
        delta = dict(before, calls=20, rows=40, blocks=60, exec_ms=20)
        report = {"schema_version": 1, "complete": True,
                  "scope": "database aggregate; no per-request attribution",
                  "errors": [], "deltas": [delta], "snapshots": [
                      {"phase": "before_concurrent", "start_unix_nano": first - 2,
                       "end_unix_nano": first - 1, "records": [before]},
                      {"phase": "after_concurrent", "start_unix_nano": last + 1,
                       "end_unix_nano": last + 2, "records": [after]}]}
        MODULE.validate_statements(report, client)
        after["stats_since_unix_micro"] = 101
        with self.assertRaises(ValueError):
            MODULE.validate_statements(report, client)
        after["stats_since_unix_micro"] = 100
        delta["stats_since_unix_micro"] = 101
        with self.assertRaises(ValueError):
            MODULE.validate_statements(report, client)
        delta["stats_since_unix_micro"] = 100
        after["calls"] = 0
        with self.assertRaises(ValueError):
            MODULE.validate_statements(report, client)
        after["calls"] = 21
        report["snapshots"][0]["end_unix_nano"] = first + 1
        with self.assertRaises(ValueError):
            MODULE.validate_statements(report, client)
        report["snapshots"][0]["end_unix_nano"] = first - 1
        after["calls"] = 2**53 + 21
        delta["calls"] = 2**53 + 21
        with self.assertRaises(ValueError):
            MODULE.validate_statements(report, client)

    def test_cli_seeded_missing_record(self) -> None:
        """Prove the executable rejects a missing record then accepts repair."""
        server, client = fixture()
        with tempfile.TemporaryDirectory() as directory:
            server_path = pathlib.Path(directory) / "server.json"
            client_path = pathlib.Path(directory) / "client.json"
            server_path.write_text(json.dumps(server), encoding="utf-8")
            samples = client["samples"]
            assert isinstance(samples, list)
            removed = samples.pop()
            client_path.write_text(json.dumps(client), encoding="utf-8")
            command = ["python3", str(MODULE_PATH), str(server_path),
                       str(client_path), "--expected", "20"]
            red = subprocess.run(command, capture_output=True, check=False)
            self.assertEqual(red.returncode, 1)
            samples.append(removed)
            client_path.write_text(json.dumps(client), encoding="utf-8")
            green = subprocess.run(command, capture_output=True, check=False)
            self.assertEqual(green.returncode, 0)

    def test_seeded_failures(self) -> None:
        """Reject each planted missing, duplicate, or inconsistent record."""
        for failure in ("missing", "duplicate_id", "duplicate_index", "status",
                        "overflow", "incomplete", "error"):
            with self.subTest(failure=failure):
                server, client = fixture()
                samples = client["samples"]
                assert isinstance(samples, list)
                if failure == "missing":
                    samples.pop()
                elif failure == "duplicate_id":
                    samples[1]["server_id"] = "1"
                elif failure == "duplicate_index":
                    samples[1]["index"] = 0
                elif failure == "status":
                    samples[1]["status_code"] = 500
                elif failure == "overflow":
                    server["overflow"] = True
                elif failure == "incomplete":
                    server["complete"] = False
                else:
                    server["errors"] = ["missing_spans"]
                with self.assertRaises(ValueError):
                    MODULE.validate_reports(server, client, 20)


if __name__ == "__main__":
    unittest.main()
