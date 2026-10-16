#!/usr/bin/env python3
"""Exercise replay enrollment guards on controlled source and event mutations."""

from __future__ import annotations

import json
import os
import re
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from replay_tier_enrollment import (  # noqa: E402
    verify_events, verify_inventory, verify_reviewed_source, verify_routes,
    verify_runner, verify_source_guard_activation,
)


ROOT = Path(__file__).resolve().parent.parent
INVENTORY = ROOT / "scripts/lib/replay_tier_inventory.json"


def committed_fixture(root: Path) -> None:
    """Create a small reviewed Go checkout for source-guard mutations."""
    (root / "go").mkdir()
    (root / "go/go.mod").write_text("module example.com/replayguard\n\ngo 1.26\n")
    (root / ".gitignore").write_text("go.work\ngoenv\n")
    (root / "tracked.txt").write_text("reviewed\n")
    for command in (
        ("git", "init", "-q"),
        ("git", "config", "user.name", "Replay Fixture"),
        ("git", "config", "user.email", "replay@example.invalid"),
        ("git", "add", "."),
        ("git", "commit", "-qm", "fixture"),
    ):
        subprocess.run(command, cwd=root, check=True, capture_output=True)


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

        guard_line = (
            'python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" '
            '--reviewed-source'
        )
        self.assertEqual(runner.count(guard_line), 5)
        for changed in (
            runner.replace(guard_line, "# " + guard_line, 1),
            runner.replace(guard_line, guard_line + " || true", 1),
            runner.replace(guard_line, "true", 1),
        ):
            with self.assertRaises(ValueError):
                verify_runner(changed, inventory["selector"])

    def test_source_guard_function_and_cli_activation_are_closed(self) -> None:
        source = (ROOT / "scripts/lib/replay_tier_enrollment.py").read_text()
        verify_source_guard_activation(source)
        for changed in (
            source.replace("\n        verify_reviewed_source(args.root)\n",
                           "\n        # verify_reviewed_source(args.root)\n", 1),
            source.replace("\n        verify_reviewed_source(args.root)\n",
                           "\n        pass\n", 1),
            source.replace("\n            verify_reviewed_source(args.root)\n",
                           "\n            pass\n", 1),
            source.replace('    for name in ("GOFLAGS", "GOWORK"):',
                           '    return\n    for name in ("GOFLAGS", "GOWORK"):', 1),
        ):
            with self.assertRaises(ValueError):
                verify_source_guard_activation(changed)

    def test_reviewed_source_rejects_dirty_checkout_and_go_overrides(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            committed_fixture(fixture)
            with mock.patch.dict(os.environ, {"GOFLAGS": "", "GOWORK": ""}):
                verify_reviewed_source(fixture)
                check = subprocess.run(
                    ("python3", str(ROOT / "scripts/lib/replay_tier_enrollment.py"),
                     "--root", str(fixture), "--reviewed-source"),
                    capture_output=True, text=True, check=False,
                )
                self.assertEqual(check.returncode, 0, check.stderr)
                tracked = fixture / "tracked.txt"
                tracked.write_text("dirty\n")
                with self.assertRaisesRegex(ValueError, "source"):
                    verify_reviewed_source(fixture)
                subprocess.run(("git", "add", "tracked.txt"), cwd=fixture,
                               check=True, capture_output=True)
                tracked.write_text("reviewed\n")
                with self.assertRaisesRegex(ValueError, "source"):
                    verify_reviewed_source(fixture)
                subprocess.run(("git", "reset", "-q", "HEAD", "--", "tracked.txt"),
                               cwd=fixture, check=True, capture_output=True)
                tracked.write_text("reviewed\n")
                (fixture / "untracked.txt").write_text("untracked\n")
                with self.assertRaisesRegex(ValueError, "source"):
                    verify_reviewed_source(fixture)
                (fixture / "untracked.txt").unlink()
                with mock.patch.dict(os.environ, {"GOFLAGS": "-run=OnlyOne"}):
                    with self.assertRaisesRegex(ValueError, "GOFLAGS"):
                        verify_reviewed_source(fixture)
                with mock.patch.dict(os.environ, {"GOWORK": "off"}):
                    with self.assertRaisesRegex(ValueError, "GOWORK"):
                        verify_reviewed_source(fixture)
                goenv = fixture / "goenv"
                goenv.write_text("GOFLAGS=-run=OnlyOne\n")
                with mock.patch.dict(os.environ, {"GOENV": str(goenv)}):
                    with self.assertRaisesRegex(ValueError, "GOFLAGS"):
                        verify_reviewed_source(fixture)
                goenv.unlink()
                (fixture / "go.work").write_text("go 1.26\nuse ./go\n")
                with self.assertRaisesRegex(ValueError, "GOWORK"):
                    verify_reviewed_source(fixture)
                (fixture / "go.work").unlink()
                (fixture / "go/go.work").write_text("go 1.26\nuse .\n")
                with self.assertRaisesRegex(ValueError, "GOWORK"):
                    verify_reviewed_source(fixture)

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
            original = prepr.read_text()
            prefix, marker, suffix = original.partition("run_or_defer replay-tier ")
            self.assertTrue(marker)
            route = "|specs/live-tests(\\.v1\\.yaml|\\.d/)"
            self.assertIn(route, prefix)  # Keep the earlier golden-corpus route.
            self.assertIn(route, suffix.split("\n", 2)[1])
            prepr.write_text(prefix + marker + suffix.replace(route, "", 1))
            self.assertIn(route, prepr.read_text().partition(marker)[0])
            with self.assertRaisesRegex(ValueError, "pre-pr"):
                verify_routes(fixture)

    def test_metadata_routes_reject_inactive_and_overridden_paths(self) -> None:
        """Only effective workflow and replay-gate path lists may route changes."""
        workflow_source = (ROOT / ".github/workflows/verify-replay-tier.yml").read_text()
        registry_source = (ROOT / "specs/ci-gates.d/gates-15.yaml").read_text()
        workflow_path = "      - 'specs/live-tests.d/**'\n"
        registry_path = '      - "specs/live-tests.d/**"\n'
        self.assertIn(workflow_path, workflow_source)
        self.assertIn(registry_path, registry_source)
        cases = {
            "workflow commented": ("workflow", workflow_source.replace(
                workflow_path, "      # - 'specs/live-tests.d/**'\n", 1)),
            "workflow wrong list": ("workflow", workflow_source.replace(
                workflow_path, "", 1).replace(
                "\nconcurrency:\n",
                "    paths-ignore:\n      - 'specs/live-tests.d/**'\n\nconcurrency:\n", 1)),
            "workflow duplicate paths override": ("workflow", workflow_source.replace(
                "\nconcurrency:\n",
                "    paths:\n      - 'go/internal/replay/**'\n\nconcurrency:\n", 1)),
            "workflow duplicate on override": ("workflow", workflow_source.replace(
                "\nconcurrency:\n",
                "\non:\n  pull_request:\n    paths:\n"
                "      - 'go/internal/replay/**'\n\nconcurrency:\n", 1)),
            "workflow duplicate pull_request override": (
                "workflow", workflow_source.replace(
                    "\nconcurrency:\n",
                    "  pull_request:\n    paths:\n"
                    "      - 'go/internal/replay/**'\n\nconcurrency:\n", 1)),
            "workflow quoted duplicate on override": (
                "workflow", workflow_source.replace(
                    "\nconcurrency:\n",
                    '\n"on":\n  pull_request:\n    paths:\n'
                    "      - 'go/internal/replay/**'\n\nconcurrency:\n", 1)),
            "registry commented": ("registry", registry_source.replace(
                registry_path, '      # - "specs/live-tests.d/**"\n', 1)),
            "registry wrong list": ("registry", registry_source.replace(
                registry_path, "", 1).replace(
                "    requirements:\n      - go\n      - docker\n    ci_only_reason:",
                '    requirements:\n      - go\n      - docker\n'
                '      - "specs/live-tests.d/**"\n    ci_only_reason:', 1)),
            "registry duplicate triggers override": ("registry", registry_source.replace(
                '      - ".github/workflows/verify-replay-tier.yml"\n    local:',
                '      - ".github/workflows/verify-replay-tier.yml"\n'
                '    triggers:\n      - "go/internal/replay/**"\n    local:', 1)),
            "registry duplicate replay gate": ("registry", registry_source.replace(
                "  - id: go-test-race\n",
                '  - id: replay-tier\n    triggers:\n'
                '      - "go/internal/replay/**"\n\n  - id: go-test-race\n', 1)),
            "registry duplicate gates override": ("registry", registry_source +
                                                  '\ngates:\n  - id: replay-tier\n'
                                                  '    triggers:\n'
                                                  '      - "go/internal/replay/**"\n'),
            "registry quoted duplicate gates override": (
                "registry", registry_source +
                '\n"gates":\n  - id: replay-tier\n'
                '    triggers:\n      - "go/internal/replay/**"\n'),
            "registry duplicate replay gate with comment": (
                "registry", registry_source.replace(
                    "  - id: go-test-race\n",
                    '  - id: replay-tier # duplicate\n    triggers:\n'
                    '      - "go/internal/replay/**"\n\n'
                    '  - id: go-test-race\n', 1)),
        }
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
            registry = fixture / "specs/ci-gates.d/gates-15.yaml"
            verify_routes(fixture)
            for case, (target, changed) in cases.items():
                with self.subTest(case=case):
                    self.assertNotEqual(changed, workflow_source if target == "workflow"
                                        else registry_source)
                    workflow.write_text(workflow_source)
                    registry.write_text(registry_source)
                    (workflow if target == "workflow" else registry).write_text(changed)
                    with self.assertRaisesRegex(ValueError, target):
                        verify_routes(fixture)

    def test_prepr_comment_decoy_cannot_hide_inactive_selector(self) -> None:
        """Read the active replay command, not a commented old selector."""
        source = (ROOT / "scripts/dev/pre-pr.sh").read_text()
        marker = "\t" + "run_or_defer replay-tier " + chr(92) + "\n"
        prefix, separator, suffix = source.partition(marker)
        self.assertTrue(separator)
        selector, newline, remainder = suffix.partition("\n")
        self.assertTrue(newline)
        target = r"|specs/live-tests(\.v1\.yaml|\.d/)"
        self.assertIn(target, selector)
        decoy = "\t# " + separator.lstrip() + "\t# " + selector.lstrip() + "\n"
        changed = prefix + decoy + separator + selector.replace(target, "", 1)
        changed += newline + remainder
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            for name in (
                ".github/workflows/verify-replay-tier.yml",
                "specs/ci-gates.d/gates-15.yaml",
                "scripts/dev/pre-pr.sh",
            ):
                path = fixture / name
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, path)
            (fixture / "scripts/dev/pre-pr.sh").write_text(changed)
            with self.assertRaisesRegex(ValueError, "pre-pr"):
                verify_routes(fixture)

    def test_workflow_proof_steps_must_stay_live(self) -> None:
        """Reject disabled or replaced CI proof even when PR paths stay intact."""
        workflow_source = (ROOT / ".github/workflows/verify-replay-tier.yml").read_text()

        def disabled_step(name: str) -> str:
            pattern = rf"(?m)(^      - name: {re.escape(name)}\n)        if: [^\n]+"
            changed, count = re.subn(pattern, r"\1        if: false", workflow_source,
                                     count=1)
            self.assertEqual(count, 1, name)
            return changed

        backend = "Run offline replay tier against real NornicDB"
        contract = "Verify replay tier contract"
        cases = {
            "job disabled": workflow_source.replace(
                "  replay-tier:\n", "  replay-tier:\n    if: false\n", 1),
            "queue selection disabled": workflow_source.replace(
                "  queue-selection:\n", "  queue-selection:\n    if: false\n", 1),
            "backend step disabled": disabled_step(backend),
            "contract step disabled": disabled_step(contract),
            "backend command no-op": workflow_source.replace(
                "run: bash scripts/verify-replay-tier.sh", "run: true", 1),
            "contract command no-op": workflow_source.replace(
                "run: bash scripts/test-verify-replay-tier.sh", "run: true", 1),
            "backend error ignored": workflow_source.replace(
                f"      - name: {backend}\n",
                f"      - name: {backend}\n        continue-on-error: true\n", 1),
            "backend quoted error ignored": workflow_source.replace(
                f"      - name: {backend}\n",
                f'      - name: {backend}\n        "continue-on-error": true\n', 1),
            "job quoted if disabled": workflow_source.replace(
                "  replay-tier:\n", '  replay-tier:\n    "if": false\n', 1),
            "quoted jobs override": workflow_source + '\n"jobs": {}\n',
            "quoted replay job override": workflow_source +
            '\n  "replay-tier": {if: false}\n',
            "checkout replaced": workflow_source.replace(
                "uses: actions/checkout@v5", "run: true", 1),
            "Go setup replaced": workflow_source.replace(
                "uses: actions/setup-go@v6", "run: true", 1),
            "queue condition changed": workflow_source.replace(
                "github.event_name != 'merge_group'", "github.event_name == 'push'", 1),
            "duplicate PR trigger overrides paths": workflow_source.replace(
                "\nconcurrency:\n",
                "\n  pull_request:\n    branches: [ main ]\n\nconcurrency:\n", 1),
            "duplicate jobs disables gate": workflow_source + "\njobs: {}\n",
            "queue target overridden": workflow_source.replace(
                "      workflow: verify-replay-tier.yml\n",
                "      workflow: verify-replay-tier.yml\n"
                "      workflow: other.yml\n", 1),
            "Go module overridden": workflow_source.replace(
                "          go-version-file: go/go.mod\n",
                "          go-version-file: go/go.mod\n"
                "          go-version-file: other/go.mod\n", 1),
        }
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
            verify_routes(fixture)
            for case, changed in cases.items():
                with self.subTest(case=case):
                    self.assertNotEqual(changed, workflow_source)
                    workflow.write_text(changed)
                    with self.assertRaisesRegex(ValueError, "workflow"):
                        verify_routes(fixture)

    def test_inventory_closes_workflow_liveness_for_ledger_gate(self) -> None:
        """The independent ledger inventory rejects a disabled replay job."""
        inventory = json.loads(INVENTORY.read_text(encoding="utf-8"))
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            for name in (*inventory["files"],
                         "scripts/verify-replay-tier.sh",
                         "scripts/lib/replay_tier_enrollment.py",
                         ".github/workflows/verify-replay-tier.yml",
                         "specs/ci-gates.d/gates-15.yaml",
                         "scripts/dev/pre-pr.sh"):
                target = fixture / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            verify_inventory(fixture, inventory)
            workflow = fixture / ".github/workflows/verify-replay-tier.yml"
            workflow.write_text(workflow.read_text().replace(
                "  replay-tier:\n", "  replay-tier:\n    if: false\n", 1
            ))
            with self.assertRaisesRegex(ValueError, "workflow"):
                verify_inventory(fixture, inventory)

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
