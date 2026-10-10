#!/usr/bin/env python3
"""Exercise the live-test manifest boundary with independent small fixtures."""

import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent / "lib"))

from live_tests_registry import load_ledger_text  # noqa: E402


HEADER = """version: live-tests/v1
updated_at: 2026-10-10
issue: 6784
parent_issue: 6788
owners:
  - graph
legacy_scheduled_exemptions:
  baseline_main: 0000000000000000000000000000000000000000
  steward: fixture-owner
  tracking_issue: 7533
  count: 0
  sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
purpose: fixture
design: fixture
"""
ROW = """  - file: go/one_live_test.go
    tag: ~
    class: postgres_ci
    reason: owned fixture
    runner: live-postgres-readiness
"""


class LiveTestsRegistryTest(unittest.TestCase):
    """Check flat compatibility and strict, ordered fragment enrollment."""

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        (self.root / "live-tests.d").mkdir()
        self.manifest = self.root / "live-tests.v1.yaml"

    def write(self, manifest: str, fragment: str = "tests:\n" + ROW) -> None:
        """Write a root manifest and one declared local fragment."""
        self.manifest.write_text(HEADER + manifest, encoding="utf-8")
        (self.root / "live-tests.d" / "one.yaml").write_text(
            fragment, encoding="utf-8"
        )

    def test_flat_compatibility_and_fragment_order(self) -> None:
        second = ROW.replace("one_live_test.go", "two_live_test.go")
        self.write("tests:\n" + ROW + second)
        expected = load_ledger_text(self.manifest)
        self.write(
            "fragments:\n  - live-tests.d/one.yaml\n"
            "  - live-tests.d/two.yaml\n"
        )
        (self.root / "live-tests.d" / "two.yaml").write_text(
            "tests:\n" + second, encoding="utf-8"
        )
        self.assertEqual(load_ledger_text(self.manifest), expected)

    def test_bad_fragment_declarations_fail(self) -> None:
        seeds = {
            "missing declaration": "",
            "empty list": "fragments:\n",
            "duplicate": "fragments:\n  - live-tests.d/one.yaml\n"
            "  - live-tests.d/one.yaml\n",
            "missing file": "fragments:\n  - live-tests.d/missing.yaml\n",
            "absolute": "fragments:\n  - /tmp/escape.yaml\n",
            "traversal": "fragments:\n  - ../escape.yaml\n",
            "mixed": "fragments:\n  - live-tests.d/one.yaml\n"
            + "tests:\n"
            + ROW,
        }
        for name, body in seeds.items():
            with self.subTest(name=name):
                self.write(body)
                with self.assertRaises(ValueError):
                    load_ledger_text(self.manifest)

    def test_malformed_fragments_and_duplicate_rows_fail(self) -> None:
        self.write("fragments:\n  - live-tests.d/one.yaml\n")
        bad_fragments = {
            "unknown row field": "tests:\n" + ROW + "    unknown: true\n",
            "duplicate field": "tests:\n" + ROW + "    class: ci\n",
            "duplicate row": "tests:\n" + ROW + ROW,
            "unknown shard key": "entries:\n" + ROW,
            "malformed shard": "tests:\n  - file go/one_live_test.go\n",
        }
        for name, fragment in bad_fragments.items():
            with self.subTest(name=name):
                self.write("fragments:\n  - live-tests.d/one.yaml\n", fragment)
                with self.assertRaises(ValueError):
                    load_ledger_text(self.manifest)

    def test_root_unknown_duplicate_key_and_symlink_escape_fail(self) -> None:
        self.write("fragments:\n  - live-tests.d/one.yaml\n")
        for name, header in {
            "unknown": HEADER + "unknown: value\n",
            "duplicate": HEADER + "issue: 9999\n",
            "version": HEADER.replace("live-tests/v1", "live-tests/v2"),
        }.items():
            with self.subTest(name=name):
                self.manifest.write_text(
                    header + "fragments:\n  - live-tests.d/one.yaml\n",
                    encoding="utf-8",
                )
                with self.assertRaises(ValueError):
                    load_ledger_text(self.manifest)
        external = tempfile.TemporaryDirectory()
        self.addCleanup(external.cleanup)
        outside = pathlib.Path(external.name) / "outside.yaml"
        outside.write_text("tests:\n" + ROW, encoding="utf-8")
        (self.root / "live-tests.d" / "one.yaml").unlink()
        (self.root / "live-tests.d" / "one.yaml").symlink_to(outside)
        self.manifest.write_text(
            HEADER + "fragments:\n  - live-tests.d/one.yaml\n",
            encoding="utf-8",
        )
        with self.assertRaises(ValueError):
            load_ledger_text(self.manifest)


if __name__ == "__main__":
    unittest.main()
