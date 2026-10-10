#!/usr/bin/env python3
"""Prove the H1 readiness canary uses one real localhost request."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest


class Handler(BaseHTTPRequestHandler):
    """Record the actual accepted client socket endpoint."""

    requests: list[tuple[str, int]] = []

    def do_GET(self) -> None:
        """Answer the readiness request and retain its peer endpoint."""
        if self.path != "/readyz":
            self.send_error(404)
            return
        self.requests.append(self.client_address)
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ready")

    def log_message(self, fmt: str, *args: object) -> None:
        """Keep the localhost test output bounded."""


class ReadyzProbeTest(unittest.TestCase):
    """Check the curl socket tuple and single request count."""

    def test_one_request_reports_actual_peer_port(self) -> None:
        Handler.requests = []
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        self.addCleanup(thread.join)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        with tempfile.TemporaryDirectory() as directory:
            report = Path(directory) / "canary.txt"
            subprocess.run(["bash", str(Path(__file__).with_name("h1-readyz-probe.sh")),
                            f"http://127.0.0.1:{server.server_port}/readyz",
                            str(report)], check=True)
            fields = dict(line.split("=", 1) for line in report.read_text().splitlines())
            self.assertEqual(len(Handler.requests), 1)
            self.assertEqual(fields["local_endpoint"],
                             f"{Handler.requests[0][0]} {Handler.requests[0][1]}")
            self.assertEqual(fields["path"], "/readyz")
            self.assertLessEqual(fields["start_utc"], fields["end_utc"])


if __name__ == "__main__":
    unittest.main()
