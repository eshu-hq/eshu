#!/usr/bin/env python3
"""Verify credential values are absent from archived H1 API logs."""

import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location(
    "h1_redact_log", Path(__file__).with_name("h1-redact-log.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class RedactLogTest(unittest.TestCase):
    """Check runtime keys, URLs and overlapping values."""

    def test_credential_values_are_replaced(self) -> None:
        environ = {
            "ESHU_POSTGRES_DSN": "postgres://user:sentinel-secret@private-host/eshu",
            "ESHU_POSTGRES_PASSWORD": "sentinel-secret",
            "ESHU_API_KEY": "sentinel-key",
        }
        raw = "connect postgres://user:sentinel-secret@private-host/eshu with sentinel-key"
        result = module.redact(raw, environ)
        self.assertNotIn("sentinel-secret", result)
        self.assertNotIn("sentinel-key", result)
        self.assertNotIn("private-host", result)
        self.assertIn("[REDACTED]", result)


if __name__ == "__main__":
    unittest.main()
