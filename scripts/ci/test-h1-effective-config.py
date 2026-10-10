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
        for key in ("GOMAXPROCS", "GOGC", "ESHU_POSTGRES_READ_MAX_OPEN_CONNS",
                    "ESHU_POSTGRES_READ_MAX_IDLE_CONNS"):
            process_env.pop(key, None)
        process_env.update({
            "ESHU_POSTGRES_DSN": "postgres://user:sentinel-secret@private-host/eshu",
            "ESHU_POSTGRES_READ_DSN": "postgres://user:sentinel-secret@private-host/eshu",
            "ESHU_POSTGRES_READ_MEMBERS": '[{"host":"private-host"}]',
            "ESHU_STATUS_SUMMARY_READ_ENABLED": "false",
            "ESHU_STATUS_SUMMARY_STALE_AFTER": "",
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
                            str(process.pid), str(output), "test-source-sha"], check=True)
            raw = output.read_text(encoding="utf-8")
            self.assertNotIn("sentinel-secret", raw)
            self.assertNotIn("private-host", raw)
            settings = json.loads(raw)["settings"]
            self.assertEqual(settings["ESHU_POSTGRES_MAX_OPEN_CONNS"],
                             {"value": "37", "origin": "process_env"})
            self.assertEqual(settings["ESHU_STATUS_SUMMARY_STALE_AFTER"],
                             {"value": "", "origin": "process_env_empty_unresolved"})
            self.assertEqual(settings["ESHU_POSTGRES_MAX_IDLE_CONNS"],
                             {"value": "10", "origin": "code_default"})
            self.assertEqual(settings["ESHU_POSTGRES_READ_MAX_OPEN_CONNS"],
                             {"value": "unknown", "origin": "runtime_derived_unsampled"})
            self.assertEqual(settings["GOMAXPROCS"],
                             {"value": "unknown", "origin": "runtime_derived_unsampled"})
            self.assertTrue(settings["ESHU_POSTGRES_READ_DSN_PRESENT"]["value"])
            self.assertTrue(settings["ESHU_POSTGRES_READ_DSN_EQUALS_WRITER"]["value"])
            self.assertTrue(settings["ESHU_POSTGRES_READ_MEMBERS_PRESENT"]["value"])
            self.assertTrue(settings["OTLP_EXPORTER_LOCAL"]["value"])
            self.assertEqual(json.loads(raw)["defaults_source"]["source_commit"],
                             "test-source-sha")


if __name__ == "__main__":
    unittest.main()
