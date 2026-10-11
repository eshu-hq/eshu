#!/usr/bin/env python3
"""Require numeric-only parsing of Linux process and cgroup CPU counters."""

import importlib.util
import pathlib
import unittest
from unittest import mock

PATH = pathlib.Path(__file__).parent / "lib" / "status_diagnostic_resources.py"
SPEC = importlib.util.spec_from_file_location("status_diagnostic_resources", PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ResourceParsingTest(unittest.TestCase):
    """Preserve CPU counters while excluding private process names and paths."""

    def test_process_name_with_parentheses(self) -> None:
        """Read utime/stime from fixed fields after the command delimiter."""
        fields = ["S"] + ["0"] * 20
        fields[11:13] = ["123", "456"]
        self.assertEqual(MODULE.process_ticks("42 (private ) name) " + " ".join(fields)), 579)

    def test_process_identity(self) -> None:
        """Pin field22 starttime to distinguish reused process identifiers."""
        fields = ["S"] + ["0"] * 20
        fields[19] = "789"
        self.assertEqual(MODULE.process_start_ticks("42 (private) " + " ".join(fields)), 789)

    def test_replaced_process_and_migration(self) -> None:
        """Reject a new process instance or changed cgroup membership."""
        fields = ["S"] + ["0"] * 20
        fields[19] = "789"
        stat = "42 (private) " + " ".join(fields)
        with mock.patch.object(pathlib.Path, "read_text", return_value=stat):
            with self.assertRaises(ValueError):
                MODULE.sample(42, pathlib.Path("/sys/fs/cgroup/original"), 790)
            with mock.patch.object(MODULE, "_cgroup", return_value=pathlib.Path("/sys/fs/cgroup/new")):
                with self.assertRaises(ValueError):
                    MODULE.sample(42, pathlib.Path("/sys/fs/cgroup/original"), 789)

    def test_stalled_sample_read(self) -> None:
        """Reject a final proc/cgroup read that exceeds one sampling interval."""
        fields = ["S"] + ["0"] * 20
        fields[19] = "789"
        stat = "42 (private) " + " ".join(fields)
        reads = [stat, "usage_usec 1\nuser_usec 1\nsystem_usec 0\n", "1"]
        with mock.patch.object(pathlib.Path, "read_text", side_effect=reads):
            with mock.patch.object(MODULE, "_cgroup", return_value=pathlib.Path("/sys/fs/cgroup/test")):
                with mock.patch.object(MODULE.time, "monotonic_ns", side_effect=[1, 1100000001]):
                    with self.assertRaises(ValueError):
                        MODULE.sample(42, pathlib.Path("/sys/fs/cgroup/test"), 789)

    def test_cgroup_cpu(self) -> None:
        """Retain only the closed CPU counter keys."""
        result = MODULE.cgroup_cpu("usage_usec 100\nuser_usec 60\nsystem_usec 40\nprivate 999\n")
        self.assertEqual(result, {"usage_usec": 100, "user_usec": 60, "system_usec": 40})

    def test_missing_and_negative(self) -> None:
        """Reject malformed or missing counter evidence."""
        for value in ("", "usage_usec -1", "usage_usec private"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                MODULE.cgroup_cpu(value)


if __name__ == "__main__":
    unittest.main()
