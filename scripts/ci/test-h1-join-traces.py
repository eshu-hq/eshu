#!/usr/bin/env python3
"""Exercise H1 trace joining with independent socket and timing fixtures."""

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


script = Path(__file__).with_name("h1-join-traces.py")
spec = importlib.util.spec_from_file_location("h1_join_traces", script)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class JoinTracesTest(unittest.TestCase):
    """Check bijection, port reuse and ambiguous root rejection."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.base_ns = 1_790_000_000_000_000_000
        self.clients = []
        self.spans = []
        self.spans.append(self.span("/readyz", 34000, self.base_ns - 2_000_000_000,
                                    self.base_ns - 1_990_000_000, "f" * 32))
        for index in range(20):
            start_ns = self.base_ns + index * 100_000_000
            port = 35000 + index % 4  # Five sequential requests reuse each port.
            self.clients.append({
                "route": "GET /api/v0/status/ingesters/{ingester}",
                "request_index": index,
                "start_utc": self.stamp(start_ns),
                "end_utc": self.stamp(start_ns + 40_000_000),
                "sample_ms": 40.0,
                "status": 200,
                "local_ip": "127.0.0.1",
                "local_port": port,
                "events": [{"name": "got_conn", "at_ms": 1.0},
                           {"name": "first_response_byte", "at_ms": 30.0}],
            })
            self.spans.append(self.span(module.STATUS_PATH, port, start_ns + 5_000_000,
                                        start_ns + 35_000_000, f"{index:032x}"))
        (self.directory / "get_start.txt").write_text(self.stamp(self.base_ns - 1_000_000))
        (self.directory / "get_end.txt").write_text(self.stamp(self.base_ns + 2_000_000_000))
        (self.directory / "readyz-canary.txt").write_text(
            "start_utc=" + self.stamp(self.base_ns - 2_001_000_000) + "\n"
            "end_utc=" + self.stamp(self.base_ns - 1_989_000_000) + "\n"
            "local_endpoint=127.0.0.1 34000\npath=/readyz\nstatus=200\n")

    @staticmethod
    def stamp(value: int) -> str:
        """Return one stable UTC stamp with nanosecond precision."""
        import datetime
        second, nanos = divmod(value, 1_000_000_000)
        return datetime.datetime.fromtimestamp(second, datetime.UTC).strftime(
            "%Y-%m-%dT%H:%M:%S") + f".{nanos:09d}Z"

    @staticmethod
    def span(path: str, port: int, start: int, end: int, trace_id: str) -> dict[str, object]:
        """Build the OTLP server root shape the collector exports."""
        attrs = {"http.request.method": "GET", "url.path": path,
                 "network.peer.address": "127.0.0.1", "network.peer.port": str(port),
                 "http.response.status_code": "200"}
        return {"name": "eshu-api", "kind": 2, "traceId": trace_id,
                "startTimeUnixNano": str(start), "endTimeUnixNano": str(end),
                "attributes": [{"key": key, "value": {"stringValue": value}}
                               for key, value in attrs.items()]}

    def write(self) -> None:
        """Write fixture files in the collector and client report formats."""
        (self.directory / "httptrace.json").write_text(json.dumps({"records": self.clients}))
        (self.directory / "traces.jsonl").write_text(json.dumps({
            "resourceSpans": [{"scopeSpans": [{"spans": self.spans}]}]}) + "\n")

    def test_reused_ports_join_twenty_unique_roots_and_canary(self) -> None:
        self.write()
        result = module.join(self.directory)
        self.assertEqual(result["status"], "joined")
        self.assertEqual(len(result["client_server_joins"]), 20)
        self.assertEqual(result["readyz_trace_id"], "f" * 32)

    def test_ambiguous_same_port_server_starts_fail(self) -> None:
        self.spans[2] = self.span(module.STATUS_PATH, 35000,
                                  self.base_ns + 6_000_000,
                                  self.base_ns + 35_000_000, "e" * 32)
        self.write()
        with self.assertRaisesRegex(ValueError, "client 0 has 2"):
            module.join(self.directory)

    def test_missing_canary_fails(self) -> None:
        self.spans.pop(0)
        self.write()
        with self.assertRaisesRegex(ValueError, "readiness canary has 0"):
            module.join(self.directory)


if __name__ == "__main__":
    unittest.main()
