#!/usr/bin/env python3
"""Ensure H1 config capture reads the process and excludes secrets."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class EffectiveConfigTest(unittest.TestCase):
    """Check allowlisted process settings and sensitive-value suppression."""

    def test_process_snapshot_redacts_connection_material(self) -> None:
        process_env = dict(os.environ)
        process_env.update({
            "ESHU_POSTGRES_DSN": "postgres://user:sentinel-secret@private-host/eshu",
            "ESHU_POSTGRES_READ_DSN": "postgres://user:sentinel-secret@private-host/eshu",
            "ESHU_POSTGRES_READ_MEMBERS": '[{"host":"private-host"}]',
            "ESHU_STATUS_SUMMARY_READ_ENABLED": "false",
            "ESHU_POSTGRES_MAX_OPEN_CONNS": "37",
            "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:14317",
        })
        process = subprocess.Popen(["sleep", "5"], env=process_env)
        self.addCleanup(process.wait)
        self.addCleanup(process.terminate)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "config.json"
            subprocess.run([sys.executable,
                            str(Path(__file__).with_name("h1-effective-config.py")),
                            str(process.pid), str(output)], check=True)
            raw = output.read_text(encoding="utf-8")
            self.assertNotIn("sentinel-secret", raw)
            self.assertNotIn("private-host", raw)
            settings = json.loads(raw)["settings"]
            self.assertEqual(settings["ESHU_POSTGRES_MAX_OPEN_CONNS"], "37")
            self.assertTrue(settings["ESHU_POSTGRES_READ_DSN_PRESENT"])
            self.assertTrue(settings["ESHU_POSTGRES_READ_DSN_EQUALS_WRITER"])
            self.assertTrue(settings["ESHU_POSTGRES_READ_MEMBERS_PRESENT"])
            self.assertTrue(settings["OTLP_EXPORTER_LOCAL"])


if __name__ == "__main__":
    unittest.main()
