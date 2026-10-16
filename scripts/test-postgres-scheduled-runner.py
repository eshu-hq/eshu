"""Contract tests for the PostgreSQL scheduled live proof runner."""

import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
LIBRARY = ROOT / "scripts/lib/postgres_scheduled_runner.py"
sys.path.insert(0, str(LIBRARY.parent))
SPEC = importlib.util.spec_from_file_location("postgres_scheduled_runner", LIBRARY)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ScheduledRunnerTest(unittest.TestCase):
    """The inventory, workflow, and events must name the same live proofs."""

    def test_committed_inventory_and_workflow(self) -> None:
        inventory = MODULE.load_inventory()
        self.assertEqual(len(inventory), 24)
        self.assertEqual(sum(len(row["tests"]) for row in inventory.values()), 41)
        for group, files, tests in (
            ("crossplane", 5, 9),
            ("infra_inventory", 19, 32),
        ):
            rows = [row for row in inventory.values() if row["group"] == group]
            self.assertEqual(len(rows), files)
            self.assertEqual(sum(len(row["tests"]) for row in rows), tests)
        MODULE.verify_inventory(ROOT, inventory)

    def test_source_drift_and_missing_workflow_command_fail(self) -> None:
        inventory = MODULE.load_inventory()
        target = next(iter(inventory))
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for path in inventory:
                copy = root / path
                copy.parent.mkdir(parents=True, exist_ok=True)
                copy.write_text((ROOT / path).read_text())
            source = ROOT / target
            copy = root / target
            copy.write_text(source.read_text().replace("func Test", "func Gone", 1))
            workflow = root / MODULE.WORKFLOW
            workflow.parent.mkdir(parents=True)
            workflow.write_text((ROOT / MODULE.WORKFLOW).read_text())
            with self.assertRaisesRegex(ValueError, "source Test inventory"):
                MODULE.verify_inventory(root, inventory)
            copy.write_text(source.read_text())
            workflow.write_text(workflow.read_text().replace("--events", "--removed-events", 1))
            with self.assertRaisesRegex(ValueError, "event verifier"):
                MODULE.verify_inventory(root, inventory)

    def test_wrong_backend_cadence_and_selector_fail(self) -> None:
        inventory = MODULE.load_inventory()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for target in inventory:
                copy = root / target
                copy.parent.mkdir(parents=True, exist_ok=True)
                copy.write_text((ROOT / target).read_text())
            workflow = root / MODULE.WORKFLOW
            workflow.parent.mkdir(parents=True)
            original = (ROOT / MODULE.WORKFLOW).read_text()
            for old, new, error in (
                ("ci-postgres-alpine@sha256:", "postgres:latest #", "pinned PostgreSQL"),
                ("35 4 * * *", "35 5 * * *", "cadence"),
                ("-run '^(", "-run '^(TestMissing|", "selector"),
                (
                    "CREATE DATABASE eshu_inventory_scheduled",
                    "CREATE DATABASE eshu_crossplane_scheduled",
                    "fresh database",
                ),
            ):
                workflow.write_text(original.replace(old, new, 1))
                with self.assertRaisesRegex(ValueError, error):
                    MODULE.verify_inventory(root, inventory)

    def test_events_require_each_run_and_pass(self) -> None:
        inventory = MODULE.load_inventory()
        for group in ("crossplane", "infra_inventory"):
            rows = [row for row in inventory.values() if row["group"] == group]
            package = rows[0]["package"]
            expected = {test for row in rows for test in row["tests"]}
            events = [
                {"Action": action, "Package": package, "Test": name}
                for name in sorted(expected)
                for action in ("run", "pass")
            ] + [{"Action": "pass", "Package": package}]
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "events.jsonl"
                path.write_text("\n".join(json.dumps(event) for event in events) + "\n")
                self.assertTrue(MODULE.verify_events(path, package, expected))
                for altered in (
                    events[1:],
                    events[:-1],
                    events + [events[1]],
                    events[:-1]
                    + [
                        {
                            "Action": "skip",
                            "Package": package,
                            "Test": sorted(expected)[0] + "/child",
                        },
                        events[-1],
                    ],
                    [{**event, "Package": package + "-wrong"} for event in events],
                ):
                    path.write_text("\n".join(json.dumps(event) for event in altered) + "\n")
                    self.assertFalse(MODULE.verify_events(path, package, expected))



if __name__ == "__main__":
    unittest.main()
