"""Synthetic contracts for the five-shard row-aware readiness model."""

from __future__ import annotations

import itertools
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import live_postgres_readiness_weights as wt
import live_postgres_readiness_v2 as v2
import live_postgres_readiness_v2_planner as planner


def document() -> dict[str, object]:
    """Return a minimal independently priced v2 model."""
    return {
        "schema_version": 2, "source_runs": ["11", "12"], "shard_count": 5,
        "fixed_job_seconds": 10.0, "cold_cache_seconds": 20.0,
        "default_test_seconds": 1.0,
        "default_package_first_seconds": 30.0,
        "default_package_repeat_seconds": 5.0,
        "package_first_seconds": {"./p": 100.0},
        "package_repeat_seconds": {"./p": 10.0},
        "test_seconds": {},
    }


class V2ModelTest(unittest.TestCase):
    """Pin strict parsing, row costs, and exact cuts before implementation."""

    def test_present_malformed_v2_never_falls_back(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            legacy = root / "weights.v1.json"
            legacy.write_text(wt.WEIGHTS_PATH.read_text(encoding="utf-8"),
                              encoding="utf-8")
            next_model = root / "weights.v2.json"
            self.assertIsInstance(wt.load_weights(legacy), wt.Weights)
            next_model.write_text('{"schema_version":2,"schema_version":2}',
                                  encoding="utf-8")
            with self.assertRaises(ValueError):
                wt.load_weights(legacy)

    def test_explicit_candidate_dispatches_by_schema(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            candidate = Path(directory) / "candidate.json"
            candidate.write_text(json.dumps(document()), encoding="utf-8")
            self.assertIsInstance(wt.load_weights(candidate), v2.WeightsV2)

    def test_schema_rejects_unknown_missing_duplicate_and_bad_defaults(self) -> None:
        for change in (
                {"extra": 1}, {"source_runs": ["11", "11"]},
                {"shard_count": 4}, {"default_package_repeat_seconds": 0},
                {"package_repeat_seconds": {"bad": 1.0}}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                v2.weights_from_json({**document(), **change})
        missing = document()
        del missing["package_repeat_seconds"]
        with self.assertRaises(ValueError):
            v2.weights_from_json(missing)

    def test_first_use_repeat_and_conservative_rows(self) -> None:
        runs = [f"./p|Test{i}Live" for i in range(12)]
        changed = {**document(), "test_seconds": {run: 100.0 for run in runs}}
        model = v2.weights_from_json(changed)
        rows = v2.split_rows(runs, model)
        self.assertEqual([run for row in rows for run in row], runs)
        self.assertGreater(len(rows), 1)
        self.assertTrue(all(v2.row_cost_tenths(row, model) <= 7200 for row in rows))
        expected = (model.fixed + model.cold + 12 * 1000 + model.package_first["./p"]
                    + (len(rows) - 1) * model.package_repeat["./p"])
        self.assertEqual(v2.segment_cost_tenths(runs, model), expected)

    def test_row_and_spread_guards_fail_closed(self) -> None:
        runs = [f"./p{i}|Test{i}Live" for i in range(5)]
        prices = {run: 1.0 for run in runs}
        prices[runs[-1]] = 800.0
        model = v2.weights_from_json({**document(),
                                      "fixed_job_seconds": 0.0,
                                      "cold_cache_seconds": 0.0,
                                      "default_package_first_seconds": 1.0,
                                      "test_seconds": prices})
        self.assertGreater(v2.row_cost_tenths([runs[-1]], model), v2.ROW_BUDGET)
        self.assertTrue(wt.budget_errors(runs, model))
        prices[runs[-1]] = 100.0
        balanced_rows = v2.weights_from_json({**document(),
                                              "fixed_job_seconds": 0.0,
                                              "cold_cache_seconds": 0.0,
                                              "default_package_first_seconds": 1.0,
                                              "test_seconds": prices})
        with self.assertRaisesRegex(ValueError, "spread"):
            wt.weighted_bounds(runs, balanced_rows)

    def test_exact_planner_matches_bruteforce_small_instances(self) -> None:
        for costs in ([0, 0, 0, 0, 0, 0], [9, 2, 8, 3, 7, 4],
                      [2, 0, 10, 1, 0, 8]):
            for cap in (10, 20):
                with self.subTest(costs=costs, cap=cap):
                    interval = lambda a, b: sum(costs[a:b])
                    candidates = []
                    for cuts in itertools.combinations(range(1, len(costs)), 2):
                        bounds = (0, *cuts, len(costs))
                        values = [interval(a, b) for a, b in zip(bounds, bounds[1:])]
                        if max(values) <= cap:
                            candidates.append(((-min(values), max(values),
                                                sum(value * value for value in values), bounds),
                                               bounds))
                    if candidates:
                        self.assertEqual(tuple(planner.exact_bounds(
                            len(costs), 3, cap, interval)), min(candidates)[1])
                    else:
                        with self.assertRaises(ValueError):
                            planner.exact_bounds(len(costs), 3, cap, interval)

    def test_nonmonotone_interval_does_not_prune_a_feasible_cut(self) -> None:
        prices = {(0, 1): 5, (0, 2): 15, (0, 3): 5,
                  (1, 2): 5, (1, 3): 5, (2, 3): 5}
        self.assertEqual(planner.exact_bounds(
            3, 1, 10, lambda a, b: prices[(a, b)]), [0, 3])

    def test_default_planner_count_follows_v2_schema(self) -> None:
        model = v2.weights_from_json(document())
        runs = [f"./p|Test{i}Live" for i in range(5)]
        self.assertEqual(wt.weighted_bounds(runs, model), [0, 1, 2, 3, 4, 5])
        with self.assertRaises(ValueError):
            wt.weighted_bounds(runs, model, 4)

    def test_defaulted_repeat_counts_only_within_a_shard(self) -> None:
        model = v2.weights_from_json({**document(), "package_repeat_seconds": {}})
        runs = [f"./p|Test{i}Live" for i in range(2)]
        # A split package pays first-use in each job; neither has a repeat row.
        self.assertEqual(v2.defaulted_packages(
            runs, model, [[runs[:1]], [runs[1:]]]), ())
        # Two runner rows in one job require an unmeasured repeat price.
        self.assertEqual(v2.defaulted_packages(
            runs, model, [[runs[:1], runs[1:]]]), ("./p",))

    def test_reentered_package_and_duplicate_test_fail_closed(self) -> None:
        model = v2.weights_from_json(document())
        for runs in (["./p|TestA", "./q|TestB", "./p|TestC"],
                     ["./p|TestA", "./p|TestA"]):
            with self.subTest(runs=runs), self.assertRaises(ValueError):
                v2.Scorer(runs, model)

    def test_fingerprint_binds_all_tables(self) -> None:
        original = v2.weights_from_json(document())
        changed = v2.weights_from_json({**document(),
                                        "package_repeat_seconds": {"./p": 11.0}})
        self.assertNotEqual(original.fingerprint(), changed.fingerprint())


class V2ReportTest(unittest.TestCase):
    """Exercise the command boundary and five-report accounting."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.weights_path = self.root / "weights.v2.json"
        self.weights_path.write_text(json.dumps(document()), encoding="utf-8")
        import live_postgres_readiness_shards as shards
        self.plan = self.root / "plan.tsv"
        self.script = Path(__file__).with_name("live_postgres_readiness_shards.py")
        inventory = subprocess.run(
            [sys.executable, str(Path(__file__).with_name(
                "live_postgres_readiness_results.py")), "list-packages"],
            capture_output=True, text=True, check=True,
            cwd=Path(__file__).parents[1])
        self.plan.write_text(inventory.stdout, encoding="utf-8")
        self.runs = shards.expand_tsv(inventory.stdout.splitlines())
        self.bounds = wt.weighted_bounds(self.runs, wt.load_weights(self.weights_path), 5)
        self.head = "a" * 40

    def invoke(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        """Run the public CLI, preserving its exit code and output."""
        return subprocess.run(
            [sys.executable, str(self.script), *arguments,
             "--weights", str(self.weights_path)], capture_output=True, text=True,
            check=False)

    def reports(self) -> list[Path]:
        """Write one terminal PASS manifest and report per selected shard."""
        paths = []
        for index, (low, high) in enumerate(zip(self.bounds, self.bounds[1:]), 1):
            manifest = self.root / f"manifest{index}.tsv"
            manifest.write_text("".join(
                f"{package}|0|{test}\n" for package, test in
                (run.split("|", 1) for run in self.runs[low:high])),
                encoding="utf-8")
            output = self.root / f"report{index}.json"
            result = self.invoke("report", "--head", self.head, "--shard",
                                 f"{index}/5", "--plan", str(self.plan),
                                 "--manifest", str(manifest), "--output", str(output))
            self.assertEqual(result.returncode, 0, result.stderr)
            paths.append(output)
        return paths

    def test_matrix_is_five_and_malformed_model_fails_nonzero(self) -> None:
        result = self.invoke("matrix")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout),
                         {"include": [{"shard": index, "total": 5}
                                      for index in range(1, 6)]})
        self.weights_path.write_text("{}", encoding="utf-8")
        self.assertNotEqual(self.invoke("matrix").returncode, 0)

    def test_report_verify_fifth_and_model_fingerprint(self) -> None:
        reports = self.reports()
        result = self.invoke("verify", "--head", self.head, "--plan",
                             str(self.plan), *(str(path) for path in reports))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotEqual(self.invoke(
            "verify", "--head", self.head, "--plan", str(self.plan),
            *(str(path) for path in reports[:4])).returncode, 0)
        changed = document()
        changed["default_package_repeat_seconds"] = 6.0
        self.weights_path.write_text(json.dumps(changed), encoding="utf-8")
        self.assertNotEqual(self.invoke(
            "verify", "--head", self.head, "--plan", str(self.plan),
            *(str(path) for path in reports)).returncode, 0)

    def test_select_rejects_stale_newest_and_missing_fifth(self) -> None:
        reports = self.reports()
        output_dir = self.root / "selected"
        result = self.invoke("select", "--head", self.head, "--plan",
                             str(self.plan), "--output-dir", str(output_dir),
                             *(str(path) for path in reports))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotEqual(self.invoke(
            "select", "--head", self.head, "--plan", str(self.plan),
            "--output-dir", str(output_dir),
            *(str(path) for path in reports[:4])).returncode, 0)

    def test_wrong_count_stale_head_and_duplicate_run_fail(self) -> None:
        reports = self.reports()
        wrong = self.invoke("report", "--head", self.head, "--shard", "1/4",
                            "--plan", str(self.plan), "--manifest",
                            str(self.root / "manifest1.tsv"), "--output",
                            str(self.root / "wrong.json"))
        self.assertNotEqual(wrong.returncode, 0)
        stale = json.loads(reports[0].read_text(encoding="utf-8"))
        stale["head"] = "b" * 40
        reports[0].write_text(json.dumps(stale), encoding="utf-8")
        self.assertNotEqual(self.invoke(
            "verify", "--head", self.head, "--plan", str(self.plan),
            *(str(path) for path in reports)).returncode, 0)
        stale["head"] = self.head
        stale["runs"].append(stale["runs"][0])
        reports[0].write_text(json.dumps(stale), encoding="utf-8")
        self.assertNotEqual(self.invoke(
            "verify", "--head", self.head, "--plan", str(self.plan),
            *(str(path) for path in reports)).returncode, 0)


class ActiveDefaultTest(unittest.TestCase):
    """Prove the installed model's matrix and reports use one shard count."""

    def invoke(self, *arguments: str, input_text: str | None = None
               ) -> subprocess.CompletedProcess[str]:
        """Run the public CLI with its committed default model."""
        script = Path(__file__).with_name("live_postgres_readiness_shards.py")
        return subprocess.run([sys.executable, str(script), *arguments],
                              input=input_text, capture_output=True, text=True,
                              check=False)

    def test_default_matrix_plan_reports_and_aggregate_agree(self) -> None:
        """The four- or five-shard activation covers the inventory exactly."""
        matrix = self.invoke("matrix")
        self.assertEqual(matrix.returncode, 0, matrix.stderr)
        include = json.loads(matrix.stdout)["include"]
        count = len(include)
        self.assertIn(count, (4, 5))
        self.assertEqual(include, [{"shard": index, "total": count}
                                   for index in range(1, count + 1)])
        inventory = subprocess.run(
            [sys.executable, str(Path(__file__).with_name(
                "live_postgres_readiness_results.py")), "list-packages"],
            capture_output=True, text=True, check=True,
            cwd=Path(__file__).parents[1]).stdout
        import live_postgres_readiness_shards as shards
        full_runs = shards.expand_tsv(inventory.splitlines())
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            plan_path = root / "plan.tsv"
            plan_path.write_text(inventory, encoding="utf-8")
            reports: list[Path] = []
            assigned: list[str] = []
            for cell in include:
                shard = f"{cell['shard']}/{cell['total']}"
                plan = self.invoke("plan", "--shard", shard, input_text=inventory)
                self.assertEqual(plan.returncode, 0, plan.stderr)
                rows = plan.stdout.splitlines()
                assigned.extend(
                    f"{row.split(chr(9))[0]}|{test}"
                    for row in rows for test in row.split(chr(9))[2].split(" "))
                manifest = root / f"manifest-{cell['shard']}.tsv"
                manifest.write_text("".join(
                    f"{row.split(chr(9))[0]}|0|{row.split(chr(9))[2].replace(' ', ',')}\n"
                    for row in rows), encoding="utf-8")
                report = root / f"report-{cell['shard']}.json"
                result = self.invoke("report", "--head", "a" * 40, "--shard", shard,
                                     "--plan", str(plan_path), "--manifest", str(manifest),
                                     "--output", str(report))
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue(json.loads(report.read_text(encoding="utf-8"))["ok"])
                reports.append(report)
            self.assertEqual(assigned, full_runs)
            verify_args = ["verify", "--head", "a" * 40, "--plan", str(plan_path)]
            self.assertEqual(self.invoke(*verify_args, *(str(path) for path in reports)
                                         ).returncode, 0)
            self.assertNotEqual(self.invoke(*verify_args, *(str(path) for path in reports[:-1])
                                            ).returncode, 0)
            wrong = f"1/{4 if count == 5 else 5}"
            self.assertNotEqual(self.invoke("plan", "--shard", wrong,
                                            input_text=inventory).returncode, 0)
