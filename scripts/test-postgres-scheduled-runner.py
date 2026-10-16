"""Contract tests for the PostgreSQL scheduled live proof runner."""

import importlib.util
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import unittest
from unittest import mock


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

    def test_ack_family_integration_requires_exact_sixth_stream(self) -> None:
        """The combined enrollment keeps ACK in the hosted proof bundle."""
        inventory = MODULE.load_inventory()
        self.assertEqual(len(inventory), 89)
        self.assertEqual(sum(len(row["tests"]) for row in inventory.values()), 167)
        self.assertIn("ack_family", MODULE.PACKAGES)
        self.assertEqual(
            sum(row["group"] == "ack_family" for row in inventory.values()), 22
        )
        self.assertIn("ack_family.jsonl", (ROOT / MODULE.WORKFLOW).read_text())

    def test_committed_inventory_and_workflow(self) -> None:
        inventory = MODULE.load_inventory()
        self.assertEqual(len(inventory), 89)
        self.assertEqual(sum(len(row["tests"]) for row in inventory.values()), 167)
        self.assertNotIn(
            "go/internal/query/code_topic_parallel_guarded_live_test.go", inventory
        )
        for group, files, tests in (
            ("crossplane", 5, 9),
            ("infra_inventory", 19, 32),
            ("disposable_gate", 1, 1),
            ("disposable_query", 9, 9),
            ("disposable_storage", 1, 2),
            ("ack_family", 22, 43),
            ("claim", 13, 36),
            ("liveness", 7, 18),
            ("fanout", 3, 3),
            ("search", 5, 8),
            ("readiness", 4, 6),
        ):
            rows = [row for row in inventory.values() if row["group"] == group]
            self.assertEqual(len(rows), files)
            self.assertEqual(sum(len(row["tests"]) for row in rows), tests)
        MODULE.verify_inventory(ROOT, inventory)

    def test_pull_request_trigger_covers_exact_enrollment(self) -> None:
        inventory = MODULE.load_inventory()
        workflow = (ROOT / MODULE.WORKFLOW).read_text()
        trigger = re.search(
            r"(?ms)^  pull_request:\n    paths:\n((?:      - '[^']+'\n)+)",
            workflow,
        )
        self.assertIsNotNone(trigger)
        paths = re.findall(r"^      - '([^']+)'$", trigger.group(1), re.MULTILINE)
        self.assertTrue(set(inventory).issubset(paths))
        self.assertIn(
            "go/internal/storage/postgres/ack_supersession_assertions_test.go", paths
        )
        self.assertNotIn("go/**", paths)
        self.assertNotIn("  push:", workflow)
        self.assertNotIn("  merge_group:", workflow)
        self.assertIn("github.event_name", workflow)

    def test_trigger_and_active_verification_drift_fail(self) -> None:
        inventory = MODULE.load_inventory()
        original = (ROOT / MODULE.WORKFLOW).read_text()
        first_path = next(iter(sorted(inventory)))
        ack_path = next(
            path for path in sorted(inventory)
            if inventory[path]["group"] == "ack_family"
        )
        changes = (
            ("  pull_request:\n", "  push:\n", "PR events"),
            ("  workflow_dispatch:\n", "  workflow_dispatch:\n  merge_group:\n", "PR events"),
            ("  workflow_dispatch:\n", "  workflow_dispatch:\n  workflow_dispatch:\n", "PR events"),
            ("    paths:\n", "    paths:\n      - 'go/**'\n", "PR paths"),
            (f"      - '{first_path}'\n", "", "PR paths"),
            (f"      - '{ack_path}'\n", "", "PR paths"),
            (
                "postgres-scheduled-live-${{ github.event_name }}-",
                "postgres-scheduled-live-", "PR concurrency",
            ),
            (
                "  group: postgres-scheduled-live-",
                "  # group: postgres-scheduled-live-", "PR concurrency",
            ),
            (
                "          python3 scripts/test-postgres-scheduled-runner.py\n",
                "          # python3 scripts/test-postgres-scheduled-runner.py\n", "harness",
            ),
            (
                "      - name: Check exact enrollment and negative cases\n",
                "      - name: Check exact enrollment and negative cases\n        if: false\n",
                "conditional",
            ),
            (
                "          python3 ../scripts/lib/postgres_scheduled_runner.py --events",
                "          # python3 ../scripts/lib/postgres_scheduled_runner.py --events",
                "event verifier",
            ),
            (
                "      - name: Run Crossplane proofs\n",
                "      - name: Run Crossplane proofs\n        if: false\n", "conditional",
            ),
            ("  postgres-scheduled:\n", "  postgres-scheduled:\n    if: false\n", "conditional"),
            (
                "  postgres-scheduled:\n",
                "  postgres-scheduled:\n    continue-on-error: true\n",
                "continue-on-error",
            ),
            (
                "  postgres-scheduled:\n",
                "  postgres-scheduled:\n    continue-on-error : true\n",
                "non-fail-closed",
            ),
            (
                "  postgres-scheduled:\n",
                "  postgres-scheduled:\n    'continue-on-error': true\n",
                "non-fail-closed",
            ),
            (
                "  postgres-scheduled:\n",
                "  postgres-scheduled:\n    if : false\n",
                "non-fail-closed",
            ),
            (
                "\njobs:\n",
                "\ndefaults :\n  run:\n    shell: bash {0} || true\n\njobs:\n",
                "non-fail-closed",
            ),
            (
                "permissions:\n  contents: read",
                "permissions:\n  contents: write",
                "non-fail-closed",
            ),
            (
                "  contents: read\n",
                "  contents: read\n  contents: write\n",
                "non-fail-closed",
            ),
            (
                "  cancel-in-progress: false\n",
                "  group : postgres-scheduled-live\n"
                "  cancel-in-progress: false\n",
                "non-fail-closed",
            ),
            (
                "      - name: Check exact enrollment and negative cases\n",
                "      - name: Check exact enrollment and negative cases\n"
                "        continue-on-error : true\n",
                "non-fail-closed",
            ),
            (
                "      - name: Check exact enrollment and negative cases\n",
                "      - name: Check exact enrollment and negative cases\n"
                "        'if': ${{ false }}\n",
                "non-fail-closed",
            ),
            (
                "      - name: Check exact enrollment and negative cases\n",
                "      - name: Check exact enrollment and negative cases\n"
                "        shell : bash {0} || true\n",
                "non-fail-closed",
            ),
            (
                "      - name: Check exact enrollment and negative cases\n",
                "      - name: Check exact enrollment and negative cases\n"
                "        <<: *soft_failure\n",
                "non-fail-closed",
            ),
            (
                "            --write-manifest \"${RUNNER_TEMP}/manifest.json\"",
                "            --missing-manifest \"${RUNNER_TEMP}/manifest.json\"",
                "manifest",
            ),
            (
                "          python3 scripts/lib/postgres_scheduled_runner.py \\\n",
                "          # python3 scripts/lib/postgres_scheduled_runner.py \\\n",
                "manifest",
            ),
            (
                "          reviewed_head_sha=\"${PR_HEAD_SHA:-$checkout_sha}\"",
                "          reviewed_head_sha=\"$checkout_sha\"",
                "manifest",
            ),
            (
                "          postgres_identity=\"$(docker exec",
                "          # postgres_identity=\"$(docker exec",
                "manifest",
            ),
            (
                "run: python3 scripts/lib/postgres_scheduled_runner.py "
                "--verify-proof-bundle \"${RUNNER_TEMP}\"",
                "run: echo bundle-verified",
                "bundle",
            ),
            (
                "uses: actions/upload-artifact@v4",
                "uses: actions/checkout@v5",
                "artifact",
            ),
            (
                "      - name: Retain hosted proof bundle\n        if: always()",
                "      - name: Retain hosted proof bundle\n        if: false",
                "artifact",
            ),
            ("            ${{ runner.temp }}/manifest.json\n", "", "artifact"),
            (
                "            ${{ runner.temp }}/manifest.json\n",
                "            ${{ runner.temp }}/manifest.json\n"
                "            ${{ runner.temp }}/*.env\n",
                "artifact",
            ),
            (
                "--verify-proof-bundle \"${RUNNER_TEMP}\"",
                "--verify-proof-bundle \"${RUNNER_TEMP}\" --allow-local-simulation",
                "local simulation",
            ),
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for target in inventory:
                copy = root / target
                copy.parent.mkdir(parents=True, exist_ok=True)
                copy.write_text((ROOT / target).read_text())
            workflow = root / MODULE.WORKFLOW
            workflow.parent.mkdir(parents=True)
            for old, new, message in changes:
                with self.subTest(change=message, old=old):
                    self.assertIn(old, original)
                    workflow.write_text(original.replace(old, new, 1))
                    with self.assertRaisesRegex(ValueError, message):
                        MODULE.verify_inventory(root, inventory)

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
                (
                    "CREATE DATABASE eshu_ack_scheduled",
                    "CREATE DATABASE eshu_crossplane_scheduled",
                    "fresh database",
                ),
                (
                    "--group ack_family",
                    "--group disposable_storage",
                    "event verifier",
                ),
                (
                    "ESHU_POSTGRES_DSN: postgres://eshu:change-me@localhost:15432/"
                    "eshu_ack_scheduled",
                    "ESHU_POSTGRES_DSN: postgres://eshu:change-me@localhost:15432/"
                    "postgres",
                    "secondary DSN",
                ),
            ):
                workflow.write_text(original.replace(old, new, 1))
                with self.assertRaisesRegex(ValueError, error):
                    MODULE.verify_inventory(root, inventory)

    def test_each_disposable_step_has_its_own_admin_env(self) -> None:
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
            removed = (
                "          ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN: "
                "postgres://eshu:change-me@localhost:15432/"
                "postgres?sslmode=disable\n"
            )
            self.assertEqual(original.count(removed), 2)
            workflow.write_text(original.replace(removed, "", 1))
            with self.assertRaisesRegex(ValueError, "missing admin DSN"):
                MODULE.verify_inventory(root, inventory)

    def test_events_require_each_run_and_pass(self) -> None:
        inventory = MODULE.load_inventory()
        for group in MODULE.PACKAGES:
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

    @mock.patch.dict(os.environ, {"GITHUB_ACTIONS": "false"})
    def test_manifest_and_bundle_fail_closed(self) -> None:
        inventory = MODULE.load_inventory()
        manifest = MODULE.build_manifest(
            inventory=inventory,
            event="pull_request",
            run_id="123",
            run_attempt="2",
            run_url="https://github.com/eshu-hq/eshu/actions/runs/123/attempts/2",
            reviewed_head_sha="a" * 40,
            checkout_sha="b" * 40,
            backend_identity="PostgreSQL 18.6 on aarch64-linux",
            local_simulation=True,
        )
        self.assertEqual(manifest["proof_origin"], "local_simulation")
        self.assertEqual(manifest["reviewed_head_sha"], "a" * 40)
        self.assertEqual(manifest["checkout_sha"], "b" * 40)
        self.assertEqual(
            manifest["inventory_sha256"],
            MODULE.inventory_digest(MODULE.INVENTORY),
        )
        self.assertEqual(manifest["postgres_image"], MODULE.IMAGE)
        with self.assertRaisesRegex(ValueError, "backend identity"):
            MODULE.build_manifest(
                inventory, "pull_request", "123", "2", manifest["run_url"],
                "a" * 40, "b" * 40, "PostgreSQL 17.9",
            )
        with tempfile.TemporaryDirectory() as directory:
            bundle = Path(directory)
            for group, package in MODULE.PACKAGES.items():
                tests = sorted(
                    test for row in inventory.values() if row["group"] == group
                    for test in row["tests"]
                )
                events = [
                    {"Action": action, "Package": package, "Test": test}
                    for test in tests for action in ("run", "pass")
                ] + [{"Action": "pass", "Package": package}]
                (bundle / f"{group}.jsonl").write_text(
                    "\n".join(json.dumps(event) for event in events) + "\n"
                )
            with self.assertRaisesRegex(ValueError, "manifest"):
                MODULE.verify_proof_bundle(bundle, inventory)
            (bundle / "manifest.json").write_text(json.dumps(manifest))
            with self.assertRaisesRegex(ValueError, "local simulation"):
                MODULE.verify_proof_bundle(bundle, inventory)
            MODULE.verify_proof_bundle(
                bundle, inventory, allow_local_simulation=True
            )
            ack_stream = bundle / "ack_family.jsonl"
            saved_ack = ack_stream.read_text()
            ack_stream.unlink()
            with self.assertRaisesRegex(ValueError, "missing.*ack_family"):
                MODULE.verify_proof_bundle(
                    bundle, inventory, allow_local_simulation=True
                )
            ack_stream.write_text(saved_ack)
            (bundle / "disposable_query.jsonl").unlink()
            with self.assertRaisesRegex(ValueError, "missing.*disposable_query"):
                MODULE.verify_proof_bundle(
                    bundle, inventory, allow_local_simulation=True
                )
            (bundle / "disposable_query.jsonl").write_text("not-json\n")
            with self.assertRaisesRegex(ValueError, "event.*disposable_query"):
                MODULE.verify_proof_bundle(
                    bundle, inventory, allow_local_simulation=True
                )
            (bundle / "manifest.json").write_text(
                json.dumps({**manifest, "inventory_sha256": "0" * 64})
            )
            with self.assertRaisesRegex(ValueError, "inventory"):
                MODULE.verify_proof_bundle(
                    bundle, inventory, allow_local_simulation=True
                )
            (bundle / "manifest.json").write_text(
                json.dumps({**manifest, "source_sha256": "0" * 64})
            )
            with self.assertRaisesRegex(ValueError, "source checksum"):
                MODULE.verify_proof_bundle(
                    bundle, inventory, allow_local_simulation=True
                )

    def test_hosted_bundle_matches_github_run_metadata(self) -> None:
        inventory = MODULE.load_inventory()
        manifest = MODULE.build_manifest(
            inventory, "pull_request", "123", "2",
            "https://github.com/eshu-hq/eshu/actions/runs/123/attempts/2",
            "a" * 40, "b" * 40, "PostgreSQL 18.6 on aarch64-linux",
        )
        with tempfile.TemporaryDirectory() as directory:
            event_path = Path(directory) / "event.json"
            event_path.write_text(json.dumps({
                "pull_request": {"head": {"sha": "a" * 40}},
            }))
            hosted_env = {
                "GITHUB_ACTIONS": "true",
                "GITHUB_REPOSITORY": "eshu-hq/eshu",
                "GITHUB_EVENT_NAME": "pull_request",
                "GITHUB_RUN_ID": "999",
                "GITHUB_RUN_ATTEMPT": "2",
                "GITHUB_SHA": "b" * 40,
                "GITHUB_EVENT_PATH": str(event_path),
            }
            with mock.patch.dict(os.environ, hosted_env, clear=True):
                with self.assertRaisesRegex(ValueError, "run ID"):
                    MODULE.verify_hosted_environment(manifest)
                os.environ["GITHUB_RUN_ID"] = "123"
                MODULE.verify_hosted_environment(manifest)
                os.environ["GITHUB_SHA"] = "c" * 40
                with self.assertRaisesRegex(ValueError, "checkout SHA"):
                    MODULE.verify_hosted_environment(manifest)
                os.environ["GITHUB_SHA"] = "b" * 40
                event_path.write_text(json.dumps({
                    "pull_request": {"head": {"sha": "d" * 40}},
                }))
                with self.assertRaisesRegex(ValueError, "reviewed head SHA"):
                    MODULE.verify_hosted_environment(manifest)



if __name__ == "__main__":
    unittest.main()
