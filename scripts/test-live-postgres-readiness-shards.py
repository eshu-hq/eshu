#!/usr/bin/env python3
"""Focused contract tests for live-postgres-readiness shard coverage.

Partitioning itself is exercised on synthetic flat plans; every TSV-level
check runs against the live `list-packages` output, so enrollment changes
cannot stale these tests.
"""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


LIB = Path(__file__).parent / "lib"
SCRIPT = LIB / "live_postgres_readiness_shards.py"
SPEC = importlib.util.spec_from_file_location("live_postgres_readiness_shards", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
shards = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = shards
SPEC.loader.exec_module(shards)
HEAD = "a" * 40


def run(package: str, test: str) -> str:
    """Build one canonical flat plan line."""
    return f"{package}|{test}"


class ShardContractTest(unittest.TestCase):
    """Exercise planner and fail-closed shard accounting."""

    @classmethod
    def setUpClass(cls) -> None:
        proc = subprocess.run([sys.executable, str(LIB / "live_postgres_readiness_results.py"),
                               "list-packages"], text=True, capture_output=True, check=False,
                              cwd=Path(__file__).parent)
        assert proc.returncode == 0, proc.stderr
        cls.full_tsv_text = proc.stdout
        cls.full_rows = proc.stdout.splitlines()
        cls.full_tests: dict[str, list[str]] = {}
        for row in cls.full_rows:
            package, _, names = row.split("\t")
            cls.full_tests[package] = names.split(" ")
        cls.flat = [f"{package}|{test}" for package, tests in cls.full_tests.items()
                    for test in tests]

    def setUp(self) -> None:
        self.plan = [run("./internal/query", f"TestShard{letter}Live")
                     for letter in "ABCDEFGH"]
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)

    def invoke(self, *args: str, input_text: str | None = None) -> subprocess.CompletedProcess[str]:
        """Run the public command line as the CI shell does."""
        return subprocess.run([sys.executable, str(SCRIPT), *args], input=input_text,
                              text=True, capture_output=True, check=False)

    def write_full_tsv(self) -> Path:
        """Write the live full list-packages TSV."""
        path = self.directory / "full.tsv"
        path.write_text(self.full_tsv_text, encoding="utf-8")
        return path

    def write_manifest(self, shard: str, statuses: dict[str, int] | None = None) -> Path:
        """Write a runner manifest for the shard's live assigned tests."""
        selected = shards.partition_runs(self.flat, shard)
        by_package: dict[str, list[str]] = {}
        for line in selected:
            package, test = line.split("|")
            by_package.setdefault(package, []).append(test)
        statuses = statuses or {}
        path = self.directory / f"manifest-{shard[0]}.tsv"
        path.write_text("".join(
            f"{package}|{statuses.get(package, 0)}|{','.join(tests)}\n"
            for package, tests in by_package.items()), encoding="utf-8")
        return path

    def write_report(self, shard: str, statuses: dict[str, int] | None = None,
                     head: str = HEAD) -> Path:
        """Run the report CLI and leave parseable evidence even on failure."""
        report_path = self.directory / f"{shard[0]}-report.json"
        result = self.invoke("report", "--head", head, "--shard", shard,
                             "--plan", str(self.write_full_tsv()),
                             "--manifest", str(self.write_manifest(shard, statuses)),
                             "--output", str(report_path))
        self.assertTrue(report_path.exists(), result.stderr)
        return report_path

    def good_reports(self) -> list[Path]:
        """Create successful evidence for all four shards."""
        return [self.write_report(shard)
                for shard in ("1/4", "2/4", "3/4", "4/4")]

    def verify(self, reports: list[Path]) -> subprocess.CompletedProcess[str]:
        """Invoke the aggregate coverage gate."""
        return self.invoke("verify", "--head", HEAD, "--plan", str(self.write_full_tsv()),
                           *[str(report) for report in reports])

    def subset_tests(self, shard: str) -> list[str]:
        """Read back the shard TSV subset as flat package|test lines."""
        result = self.invoke("plan", "--shard", shard, input_text=self.full_tsv_text)
        self.assertEqual(result.returncode, 0, result.stderr)
        flat: list[str] = []
        for row in result.stdout.splitlines():
            package, pattern, names = row.split("\t")
            tests = names.split(" ")
            self.assertEqual(pattern, f"^({'|'.join(tests)})$")
            full = self.full_tests[package]
            positions = [full.index(test) for test in tests]
            self.assertEqual(positions, sorted(positions))
            flat.extend(f"{package}|{test}" for test in tests)
        return flat

    def test_partition_is_contiguous_quarters(self) -> None:
        self.assertEqual(shards.partition_runs(self.plan, "all"), self.plan)
        self.assertEqual(shards.partition_runs(self.plan, "1/4"), self.plan[0:2])
        self.assertEqual(shards.partition_runs(self.plan, "2/4"), self.plan[2:4])
        self.assertEqual(shards.partition_runs(self.plan, "3/4"), self.plan[4:6])
        self.assertEqual(shards.partition_runs(self.plan, "4/4"), self.plan[6:8])

    def test_partition_union_covers_plan_for_uneven_counts(self) -> None:
        for extra in range(4, 9):
            expanded = [*self.plan, *(run("./internal/query", f"TestFuture{i}Live")
                                       for i in range(extra))]
            assigned = [line for shard in ("1/4", "2/4", "3/4", "4/4")
                        for line in shards.partition_runs(expanded, shard)]
            self.assertCountEqual(assigned, expanded)

    def test_degenerate_plan_fails_closed_instead_of_running_three_shards(self) -> None:
        tiny = self.plan[:3]
        with self.assertRaises(ValueError):
            shards.partition_runs(tiny, "4/4")

    def test_invalid_plan_and_shard_fail(self) -> None:
        for plan, shard in (([], "all"), (self.plan * 2, "all"),
                            (["bad"], "all"), (["./x|bad-name"], "all"),
                            (self.plan, "5/4")):
            with self.subTest(plan=plan, shard=shard), self.assertRaises(ValueError):
                shards.partition_runs(plan, shard)

    def test_plan_cli_partitions_live_tsv_without_loss(self) -> None:
        assigned = [line for shard in ("1/4", "2/4", "3/4", "4/4")
                    for line in self.subset_tests(shard)]
        self.assertCountEqual(assigned, self.flat)

    def test_plan_cli_rejects_drift_and_bad_shard(self) -> None:
        drifted = self.full_tsv_text.replace(self.flat[0].split("|")[1], "TestDriftLive", 1)
        self.assertNotEqual(
            self.invoke("plan", "--shard", "1/4", input_text=drifted).returncode, 0)
        self.assertNotEqual(
            self.invoke("plan", "--shard", "0/4", input_text=self.full_tsv_text).returncode, 0)
        self.assertNotEqual(
            self.invoke("plan", "--shard", "1/4", input_text="bad\trow\n").returncode, 0)

    def test_report_and_verify_green(self) -> None:
        reports = self.good_reports()
        self.assertEqual(self.verify(reports).returncode, 0, self.verify(reports).stderr)
        result = json.loads(reports[1].read_text(encoding="utf-8"))
        expected_second = shards.partition_runs(self.flat, "2/4")
        self.assertEqual(result["expected_runs"], expected_second)
        self.assertEqual(result["observed_count"], len(expected_second))

    def test_missing_failed_and_unexpected_tests_fail_report(self) -> None:
        selected = shards.partition_runs(self.flat, "2/4")
        package, first = selected[0].split("|")
        rest = ",".join(line.split("|")[1] for line in selected[1:] if line.split("|")[0] == package)
        others = "".join(f"{line.split('|')[0]}|0|{line.split('|')[1]}\n" for line in selected
                         if line.split("|")[0] != package)
        cases = [
            others,
            others + f"{package}|0|{rest}\n",
            others + f"{package}|1|{first}{',' + rest if rest else ''}\n",
            others + f"{package}|0|{first}{',' + rest if rest else ''},TestNopeLive\n",
        ]
        for index, body in enumerate(cases):
            with self.subTest(index=index):
                manifest_path = self.directory / "events.tsv"
                manifest_path.write_text(body, encoding="utf-8")
                output = self.directory / "result.json"
                result = self.invoke("report", "--head", HEAD, "--shard", "2/4",
                                     "--plan", str(self.write_full_tsv()),
                                     "--manifest", str(manifest_path),
                                     "--output", str(output))
                self.assertNotEqual(result.returncode, 0, body)
                self.assertFalse(json.loads(output.read_text(encoding="utf-8"))["ok"])

    def test_verify_rejects_stale_head_missing_and_duplicate_shards(self) -> None:
        reports = self.good_reports()
        self.assertNotEqual(self.verify(reports[:-1]).returncode, 0)
        absent = self.directory / "cancelled-shard.json"
        self.assertNotEqual(self.verify([*reports[:3], absent]).returncode, 0)
        self.assertNotEqual(self.verify([*reports[:3], reports[0]]).returncode, 0)
        stale = self.write_report("4/4", head="b" * 40)
        self.assertNotEqual(self.verify([*reports[:3], stale]).returncode, 0)

    def test_verify_rejects_failed_shard_report(self) -> None:
        reports = self.good_reports()
        package = shards.partition_runs(self.flat, "4/4")[0].split("|")[0]
        failed = self.write_report("4/4", {package: 1})
        self.assertNotEqual(self.verify([*reports[:3], failed]).returncode, 0)

    def test_duplicate_json_keys_fail_verify(self) -> None:
        first_three = self.good_reports()[:3]
        tampered = self.directory / "tampered-4-report.json"
        tampered.write_text(self.write_report("4/4").read_text(encoding="utf-8"),
                            encoding="utf-8")
        raw = tampered.read_text(encoding="utf-8")
        tampered.write_text(raw[:-2] + ',"ok":true}\n', encoding="utf-8")
        retry = self.invoke("verify", "--head", HEAD, "--plan", str(self.write_full_tsv()),
                            *[str(p) for p in first_three], str(tampered))
        self.assertNotEqual(retry.returncode, 0)


if __name__ == "__main__":
    unittest.main()
