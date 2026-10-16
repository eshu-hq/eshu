"""Contract tests for the reducer contention Go event verifier."""

import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest


LIB = Path(__file__).parent / "lib" / "reducer_contention_events.py"
SPEC = importlib.util.spec_from_file_location("reducer_contention_events", LIB)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class EventProofTest(unittest.TestCase):
    """A selected proof must run once and pass in its intended package."""

    def test_pass_then_missing_skip_duplicate_and_wrong_package_fail(self) -> None:
        package = "github.com/eshu-hq/eshu/go/internal/query"
        proof = "TestStoryLive"
        good = [
            {"Action": "run", "Package": package, "Test": proof},
            {"Action": "pass", "Package": package, "Test": proof},
            {"Action": "pass", "Package": package},
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.json"
            for events, passes in (
                (good, True),
                (good[1:], False),
                ([good[0], {**good[1], "Action": "skip"}, good[2]], False),
                (good + [good[1]], False),
                ([{**event, "Package": package + "x"} for event in good], False),
                (good[:-1], False),
                (good + [{"Action": "skip", "Package": package, "Test": proof + "/child"}], False),
                (good + [{"Action": "fail", "Package": package, "Test": proof + "/child"}], False),
            ):
                path.write_text("".join(json.dumps(event) + "\n" for event in events))
                with self.subTest(events=events):
                    self.assertEqual(
                        MODULE.verify_events(path, package, {proof}, io.StringIO()),
                        passes,
                    )
            path.write_text("".join(json.dumps(event) + "\n" for event in good) + "{")
            self.assertFalse(MODULE.verify_events(path, package, {proof}, io.StringIO()))

    def test_inventory_reads_source_and_selector(self) -> None:
        """A copied declaration cannot conceal a new or unselected test."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "go/internal/query/proof_live_test.go"
            source.parent.mkdir(parents=True)
            source.write_text("package query\nfunc TestStoryLive(t *testing.T) {}\n")
            workflow = root / MODULE.WORKFLOW
            workflow.parent.mkdir(parents=True)
            workflow.write_text(
                "      - name: Run story proof\n"
                "        run: |\n"
                "          set -o pipefail\n"
                "          go test ./internal/query/ -run '^TestStoryLive$' -json | tee events.jsonl\n"
                "          python3 ../scripts/lib/reducer_contention_events.py --events events.jsonl "
                "--package github.com/eshu-hq/eshu/go/internal/query\n"
            )
            inventory = {
                "go/internal/query/proof_live_test.go": {
                    "package": "github.com/eshu-hq/eshu/go/internal/query",
                    "tests": ["TestStoryLive"],
                }
            }
            MODULE.verify_inventory(root, inventory)
            source.write_text(source.read_text() + "func TestNewLive(t *testing.T) {}\n")
            with self.assertRaisesRegex(ValueError, "source Test inventory changed"):
                MODULE.verify_inventory(root, inventory)
            inventory["go/internal/query/proof_live_test.go"]["tests"] = [
                "TestNewLive", "TestStoryLive"
            ]
            with self.assertRaisesRegex(ValueError, "unselected tests"):
                MODULE.verify_inventory(root, inventory)
            inventory["go/internal/query/proof_live_test.go"]["tests"] = [
                "TestStoryLive"
            ]
            source.write_text("package query\nfunc TestStoryLive(t *testing.T) {}\n")
            valid_workflow = workflow.read_text()
            workflow.write_text(
                valid_workflow.replace(
                    "python3 ../scripts/lib/reducer_contention_events.py",
                    "echo event verifier removed",
                )
            )
            with self.assertRaisesRegex(ValueError, "missing event verifier"):
                MODULE.verify_inventory(root, inventory)
            workflow.write_text(valid_workflow.replace("go test ./", "# go test ./"))
            with self.assertRaisesRegex(ValueError, "no workflow selector"):
                MODULE.verify_inventory(root, inventory)

    def test_ci_wiring_selects_harness_changes(self) -> None:
        """A harness-only change must select the owner job and its self-tests."""
        root = Path(__file__).resolve().parent.parent
        registry = (root / "specs/ci-gates.d/gates-05.yaml").read_text()
        owner = registry.split("  - id: reducer-contention\n", 1)[1].split(
            "\n  - id:", 1
        )[0]
        workflow = (root / MODULE.WORKFLOW).read_text()
        required = (
            "scripts/lib/reducer_contention_events.py",
            "scripts/lib/reducer_contention_inventory.json",
            "scripts/test-reducer-contention-events.py",
            "scripts/test-reducer-contention-ledger-policy.py",
            "scripts/lib/verify-live-tests-ledger.py",
            "specs/live-tests.v1.yaml",
            "specs/live-tests.d/**",
            ".github/workflows/reducer-contention-gate.yml",
        )
        for path in required:
            with self.subTest(path=path):
                self.assertIn(path, owner)
        for command in (
            "python3 scripts/test-reducer-contention-events.py",
            "python3 scripts/test-reducer-contention-ledger-policy.py",
            "python3 scripts/lib/reducer_contention_events.py",
            "bash scripts/verify-live-tests-ledger.sh",
        ):
            with self.subTest(command=command):
                self.assertIn(command, workflow)


if __name__ == "__main__":
    unittest.main()
