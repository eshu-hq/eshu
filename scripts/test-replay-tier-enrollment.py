#!/usr/bin/env python3
"""Exercise replay enrollment guards on controlled source and event mutations."""

from __future__ import annotations

import json
import shutil
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from replay_tier_enrollment import (  # noqa: E402
    verify_events, verify_inventory, verify_routes, verify_runner,
)


ROOT = Path(__file__).resolve().parent.parent
INVENTORY = ROOT / "scripts/lib/replay_tier_inventory.json"


class ReplayTierEnrollmentTest(unittest.TestCase):
    """Check the exact source set and successful top-level Go events."""

    def test_committed_inventory_and_source_are_closed(self) -> None:
        inventory = json.loads(INVENTORY.read_text(encoding="utf-8"))
        self.assertEqual(len(inventory["files"]), 23)
        self.assertEqual(sum(len(item["tests"]) for item in inventory["files"].values()), 27)
        verify_inventory(ROOT, inventory)

    def test_dropped_selector_is_rejected(self) -> None:
        inventory = json.loads(INVENTORY.read_text(encoding="utf-8"))
        inventory["selector"] = inventory["selector"].replace(
            "TestGovScopeMaterializationEntitySourcesHaveFiles", "MissingGovScope"
        )
        with self.assertRaisesRegex(ValueError, "selector"):
            verify_inventory(ROOT, inventory)

    def test_source_declaration_drift_is_rejected(self) -> None:
        inventory = json.loads(INVENTORY.read_text(encoding="utf-8"))
        first = next(iter(inventory["files"]))
        inventory["files"][first]["tests"] = ["TestImaginary"]
        with self.assertRaises(ValueError):
            verify_inventory(ROOT, inventory)

    def test_runner_verifier_must_be_active(self) -> None:
        inventory = json.loads(INVENTORY.read_text(encoding="utf-8"))
        runner = (ROOT / "scripts/verify-replay-tier.sh").read_text()
        verify_runner(runner, inventory["selector"])
        for changed in (
            runner.replace(
                "python3 scripts/lib/replay_tier_enrollment.py",
                "# python3 scripts/lib/replay_tier_enrollment.py", 1,
            ),
            runner.replace('--go-exit "${tier_status}"', '--go-exit 0', 1),
            runner.replace('-count=1 -v -json', '-count=1 -v', 1),
        ):
            with self.assertRaises(ValueError):
                verify_runner(changed, inventory["selector"])

    def test_metadata_routes_and_seeded_removal(self) -> None:
        verify_routes(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            for name in (
                ".github/workflows/verify-replay-tier.yml",
                "specs/ci-gates.d/gates-15.yaml",
                "scripts/dev/pre-pr.sh",
            ):
                target = fixture / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            workflow = fixture / ".github/workflows/verify-replay-tier.yml"
            workflow.write_text(workflow.read_text().replace(
                "      - 'specs/live-tests.d/**'\n", "", 1
            ))
            with self.assertRaisesRegex(ValueError, "workflow"):
                verify_routes(fixture)
            shutil.copy2(ROOT / ".github/workflows/verify-replay-tier.yml", workflow)
            registry = fixture / "specs/ci-gates.d/gates-15.yaml"
            registry.write_text(registry.read_text().replace(
                '      - "specs/live-tests.d/**"\n', "", 1
            ))
            with self.assertRaisesRegex(ValueError, "registry"):
                verify_routes(fixture)
            shutil.copy2(ROOT / "specs/ci-gates.d/gates-15.yaml", registry)
            prepr = fixture / "scripts/dev/pre-pr.sh"
            prepr.write_text(prepr.read_text().replace(
                "|specs/live-tests(\\.v1\\.yaml|\\.d/)", "", 1
            ))
            with self.assertRaisesRegex(ValueError, "pre-pr"):
                verify_routes(fixture)

    def test_old_guard_output_cannot_replace_json_test_events(self) -> None:
        package = "github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
        name = "TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch"
        events = [
            {"Action": "start", "Package": package},
            {"Action": "output", "Package": package,
             "Output": f"--- PASS: {name} (0.01s)\n"},
            {"Action": "pass", "Package": package},
        ]
        with tempfile.TemporaryDirectory() as temporary:
            stream = Path(temporary) / "output-only.jsonl"
            stream.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            with self.assertRaisesRegex(ValueError, "missing selected test"):
                verify_events(stream, {package: {name}}, 0)

    def test_exact_ordered_events_and_direct_exit(self) -> None:
        package = "github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
        expected = {package: {"TestOne"}}
        events = [
            {"Action": "start", "Package": package},
            {"Action": "run", "Package": package, "Test": "TestOne"},
            {"Action": "output", "Package": package, "Test": "TestOne", "Output": "ok"},
            {"Action": "pass", "Package": package, "Test": "TestOne"},
            {"Action": "pass", "Package": package},
        ]
        with tempfile.TemporaryDirectory() as temporary:
            stream = Path(temporary) / "events.jsonl"
            stream.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            verify_events(stream, expected, 0)
            with self.assertRaisesRegex(ValueError, "exit"):
                verify_events(stream, expected, 1)
            for changed in (
                events[:-1],
                [*events, events[3]],
                [*events[:3], {**events[3], "Action": "skip"}, *events[4:]],
                [*events[:3],
                 {**events[3], "Test": "TestOne/child", "Action": "skip"},
                 *events[3:]],
                [*events[:1], {**events[1], "Package": "wrong/package"}, *events[2:]],
            ):
                stream.write_text("\n".join(json.dumps(event) for event in changed) + "\n")
                with self.assertRaises(ValueError):
                    verify_events(stream, expected, 0)
            stream.write_text("{malformed\n")
            with self.assertRaises(ValueError):
                verify_events(stream, expected, 0)


if __name__ == "__main__":
    unittest.main()
