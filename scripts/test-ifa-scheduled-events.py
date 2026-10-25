"""Contract tests for the Ifá scheduled live proof runner."""

import fnmatch
import hashlib
import importlib.util
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent.parent
MODULE_PATH = ROOT / "scripts/lib/ifa_scheduled_events.py"
SPEC = importlib.util.spec_from_file_location("ifa_scheduled_events", MODULE_PATH)
assert SPEC and SPEC.loader
sys.path.insert(0, str(MODULE_PATH.parent))
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ScheduledProofTest(unittest.TestCase):
    """Enrollment must fail closed when a live proof is removed."""

    def test_real_runner_contract(self) -> None:
        MODULE.verify_inventory(ROOT, MODULE.load_inventory())

    def test_missing_second_endpoint_and_verifier_fail(self) -> None:
        inventory = MODULE.load_inventory()
        script = (ROOT / MODULE.RUNNER).read_text(encoding="utf-8")
        for old, new in (
            ("ESHU_REPO_DEPENDENCY_PROOF_MISMATCH_HTTP_URL", "MISSING_ENDPOINT"),
            ("ESHU_GRAPH_BACKEND=nornicdb", "ESHU_GRAPH_BACKEND=neo4j"),
            ("ifa_scheduled_events.py --events", "missing_verifier --events"),
            ("ifa_scheduled_manifest.py --primary", "missing_manifest.py --primary"),
            ("ifa_scheduled_bundle.py --directory", "missing_bundle.py --directory"),
            ('--output "${events_dir}/manifest.json"', '--output "${events_dir}/manifest.json" --local-proof'),
            ('ifa_scheduled_bundle.py --directory "${events_dir}"', 'ifa_scheduled_bundle.py --directory "${events_dir}" --allow-local-proof'),
            ("-tags ifarepodependencyproof", "-tags wrongproof"),
            ("ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1", "ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=0"),
        ):
            with self.subTest(old=old):
                self.assertIn(old, script)
                with self.assertRaises(ValueError):
                    MODULE.verify_inventory(ROOT, inventory, runner_text=script.replace(old, new))

    def test_missing_cadence_and_source_test_fail(self) -> None:
        inventory = MODULE.load_inventory()
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        self.assertIn("50 4 * * *", workflow)
        with self.assertRaises(ValueError):
            MODULE.verify_inventory(ROOT, inventory, workflow_text=workflow.replace("50 4 * * *", "50 5 * * *"))
        with self.assertRaises(ValueError):
            MODULE.verify_inventory(ROOT, inventory, workflow_text=workflow.replace("  workflow_call:\n", "  merge_group:\n  workflow_call:\n"))
        changed = {path: dict(row) for path, row in inventory.items()}
        path = "go/internal/replay/offlinetier/repo_dependency_ifa_cleanup_live_test.go"
        changed[path]["tests"] = ["TestMissing"]
        with self.assertRaises(ValueError):
            MODULE.verify_inventory(ROOT, changed)

    def test_pr_bootstrap_route_is_exact_and_active(self) -> None:
        """A bounded PR route must run the same live proof job."""
        inventory = MODULE.load_inventory()
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        MODULE.verify_inventory(ROOT, inventory, workflow_text=workflow)
        fragment = (ROOT / MODULE.FRAGMENT).read_text()
        for old, new in (
            ("  synchronize: true", "  synchronize: false"),
            ("  - scripts/lib/ifa_scheduled_events.py", "  - go/**"),
            ("  - go/internal/replay/offlinetier/postgres_test.go\n", ""),
            ("  workflow_call:\n", "  push:\n  workflow_call:\n"),
            ("    runs-on: ubuntu-24.04", "    if: false\n    runs-on: ubuntu-24.04"),
            ("      - name: Run tagged Ifá proofs", "      # - name: Run tagged Ifá proofs"),
            ("          bash scripts/run-ifa-scheduled-proofs.sh", "          bash scripts/run-ifa-scheduled-proofs.sh\n        continue-on-error: true"),
            ("          python3 scripts/lib/ifa_scheduled_events.py", "          # python3 scripts/lib/ifa_scheduled_events.py"),
            ("      - name: Verify Ifá runner contract", "      - name: Verify Ifá runner contract\n        if: false"),
            ("            ${{ runner.temp }}/ifa-scheduled/manifest.json", "            # ${{ runner.temp }}/ifa-scheduled/manifest.json"),
        ):
            with self.subTest(old=old):
                if old in workflow:
                    with self.assertRaises(ValueError):
                        MODULE.verify_inventory(ROOT, inventory,
                                                workflow_text=workflow.replace(old, new, 1))
                else:
                    self.assertIn(old, fragment)
                    with self.assertRaises(ValueError):
                        MODULE.verify_workflow_route(workflow, fragment.replace(old, new, 1))

    def test_pr_route_covers_go_module_inputs(self) -> None:
        """The production validator rejects fragment routes missing Go build inputs."""
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        fragment = (ROOT / MODULE.FRAGMENT).read_text(encoding="utf-8")
        self.assertIn("          go-version-file: go/go.mod", workflow)
        self.assertIn("        run: scripts/ci/go-mod-download-retry.sh", workflow)
        for path in ("go/go.mod", "go/go.sum"):
            with self.subTest(path=path):
                old = f"  - {path}\n"
                self.assertIn(old, fragment)
                with self.assertRaisesRegex(ValueError, "path filter drifted"):
                    MODULE.verify_workflow_route(workflow, fragment.replace(old, "", 1))

    def test_workflow_rejects_alternate_activation_and_failure_keys(self) -> None:
        """The owner job and steps accept only their literal reviewed keys."""
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        MODULE.verify_workflow_route(workflow)
        mutations = []
        for marker, indent in (
            ("  ifa-scheduled:\n", "    "),
            ("      - name: Checkout repository\n", "        "),
            ("      - name: Verify Ifá runner contract\n", "        "),
            ("      - name: Set up Go\n", "        "),
            ("      - name: Pre-warm Go modules\n", "        "),
            ("      - name: Run tagged Ifá proofs\n", "        "),
            ("      - name: Upload Ifá proof events\n", "        "),
        ):
            for property_line in (
                "continue-on-error : true",
                "'continue-on-error': true",
                '"continue-on-error": true',
                "if : ${{ false }}",
                "'if': ${{ false }}",
                "defaults:",
                "shell: bash {0} || true",
                "<<: *soft_failure",
            ):
                mutations.append(
                    workflow.replace(marker, marker + indent + property_line + "\n", 1)
                )
        mutations.extend((
            workflow.replace("jobs:\n", "defaults:\n  run:\n    shell: bash {0} || true\njobs:\n", 1),
            workflow.replace("jobs:\n", "defaults :\n  run:\n    shell: bash {0} || true\njobs:\n", 1),
            workflow.replace("  ifa-scheduled:\n", "  ifa-scheduled:\n    runs-on: ubuntu-24.04\n", 1),
            workflow + "\n  ifa-scheduled:\n    continue-on-error: true\n",
            workflow.replace(
                "      - name: Run tagged Ifá proofs\n",
                "      - name: Run tagged Ifá proofs\n      - name: Run tagged Ifá proofs\n",
                1,
            ),
            workflow.replace(
                "          bash scripts/run-ifa-scheduled-proofs.sh\n",
                "          bash scripts/run-ifa-scheduled-proofs.sh\n        run: echo bypass\n",
                1,
            ),
            workflow.replace(
                "          bash scripts/run-ifa-scheduled-proofs.sh\n",
                "          bash scripts/run-ifa-scheduled-proofs.sh\n          echo bypass\n",
                1,
            ),
        ))
        for index, mutated in enumerate(mutations):
            with self.subTest(index=index):
                with self.assertRaises(ValueError):
                    MODULE.verify_workflow_route(mutated)

    def test_checkout_cannot_override_event_revision(self) -> None:
        """A stale or relocated checkout cannot stand in for the event revision."""
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        marker = "          persist-credentials: false\n"
        self.assertIn(marker, workflow)
        for addition in (
            "          ref: 080cd0152e112851e722ef89c7374b4816d2bd65\n",
            "          'ref': '080cd0152e112851e722ef89c7374b4816d2bd65'\n",
            "          ref : 'refs/pull/1/merge'\n",
            "          path: old-checkout\n",
            "          persist-credentials: false\n",
            "          persist-credentials: 'false'\n",
        ):
            with self.subTest(addition=addition):
                with self.assertRaises(ValueError):
                    MODULE.verify_workflow_route(
                        workflow.replace(marker, marker + addition, 1)
                    )

    def test_preproof_setup_and_helper_route_are_closed(self) -> None:
        """No executable prewarm or alternate Go setup may precede the proof."""
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        for old, new in (
            ("        uses: actions/setup-go@v6\n",
             "        uses: actions/setup-go@v5\n"),
            ("        run: scripts/ci/go-mod-download-retry.sh\n",
             "        run: |\n          scripts/ci/go-mod-download-retry.sh\n"
             "          python3 -c 'print(1)'\n"),
        ):
            with self.subTest(old=old):
                self.assertIn(old, workflow)
                with self.assertRaises(ValueError):
                    MODULE.verify_workflow_route(workflow.replace(old, new, 1))
        self.assertIn("scripts/ci/go-mod-download-retry.sh", MODULE.PR_EXTRA_PATHS)

    def test_runner_executable_bytes_are_pinned(self) -> None:
        """An extra runner command invalidates the exact proof inventory."""
        runner = (ROOT / MODULE.RUNNER).read_text(encoding="utf-8")
        changed = runner.replace("verify_reviewed_source\n", "verify_reviewed_source\ntrue\n", 1)
        self.assertNotEqual(changed, runner)
        original_read_text = Path.read_text
        with mock.patch.object(Path, "read_text", autospec=True) as read_text:
            def read_fixture(path: Path, *args: object, **kwargs: object) -> str:
                """Return only the mutated runner while reading real proof inputs."""
                if path == ROOT / MODULE.RUNNER:
                    return changed
                return original_read_text(path, *args, **kwargs)
            read_text.side_effect = read_fixture
            with self.assertRaises(ValueError):
                MODULE.verify_inventory(ROOT, MODULE.load_inventory())

    def test_workflow_guard_rejects_dirty_proof_source(self) -> None:
        """The actual hosted shell checks staged, unstaged, and new files."""
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^      - name: Run tagged Ifá proofs\n.*?^        run: \|\n"
            r"(.*?)(?=^          bash scripts/run-ifa-scheduled-proofs.sh)",
            workflow,
        )
        self.assertIsNotNone(match)
        script = "\n".join(line[10:] for line in match.group(1).splitlines())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "checkout"
            root.mkdir()
            (root / ".gitignore").write_text("go.work\n", encoding="utf-8")
            for relative in ("go/proof_test.go", "sdk/proof.go", "scripts/ci/go-mod-download-retry.sh",
                             ".github/workflows/ifa-scheduled-proofs.yml"):
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(
                    'func TestProof(t *testing.T) { t.Fatal("reached") }\n'
                    if relative == "go/proof_test.go" else "reviewed\n")
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            subprocess.run(["git", "add", "-A"], cwd=root, check=True)
            subprocess.run(["git", "-c", "user.name=Proof", "-c",
                            "user.email=proof@example.invalid", "commit", "-qm", "fixture"],
                           cwd=root, check=True)
            def run(goenv: str = "off") -> int:
                """Execute the workflow source guard in the fixture checkout."""
                return subprocess.run(["bash", "-c", script], cwd=root,
                                      env={**{key: value for key, value in os.environ.items()
                                              if key not in ("GOFLAGS", "GOWORK")},
                                           "GOENV": goenv},
                                      check=False, capture_output=True).returncode
            self.assertEqual(run(), 0)
            source = root / "go/proof_test.go"
            source.write_text('func TestProof(t *testing.T) { return }\n')
            self.assertNotEqual(run(), 0)
            subprocess.run(["git", "add", "go/proof_test.go"], cwd=root, check=True)
            self.assertNotEqual(run(), 0)
            subprocess.run(["git", "restore", "--staged", "go/proof_test.go"],
                           cwd=root, check=True)
            source.write_text('func TestProof(t *testing.T) { t.Fatal("reached") }\n')
            (root / "go/new_test.go").write_text("new\n")
            self.assertNotEqual(run(), 0)
            (root / "go/new_test.go").unlink()
            sdk = root / "sdk/proof.go"
            sdk.write_text("changed\n")
            self.assertNotEqual(run(), 0, "SDK source drift must fail the real guard")
            sdk.write_text("reviewed\n")
            workspace = root / "go.work"
            workspace.write_text("go 1.26.9\n")
            with self.subTest(configuration="ignored workspace"):
                self.assertNotEqual(run(), 0, "ignored workspace must fail")
            workspace.unlink()
            ancestor_workspace = Path(temporary) / "go.work"
            ancestor_workspace.write_text("go 1.26.9\nuse ./checkout/go\n")
            with self.subTest(configuration="ancestor workspace"):
                self.assertNotEqual(run(), 0, "ancestor workspace must fail")
            ancestor_workspace.unlink()
            goenv_file = Path(temporary) / "go-env"
            goenv_file.write_text("GOFLAGS=-run=^$\n")
            with self.subTest(configuration="persisted GOFLAGS"):
                self.assertNotEqual(run(str(goenv_file)), 0,
                                    "persisted GOFLAGS must fail")
            (root / "scripts/ci/go-mod-download-retry.sh").write_text("changed\n")
            self.assertNotEqual(run(), 0)

    def test_manifest_pin_drift_fails(self) -> None:
        """A receipt cannot silently name another backend image."""
        with mock.patch.object(MODULE, "NORNIC_DIGEST", "sha256:wrong"):
            with self.assertRaises(ValueError):
                MODULE.verify_inventory(ROOT, MODULE.load_inventory())

    def test_static_mirror_selects_harness_changes(self) -> None:
        """PR and merge queue changes must run the hermetic negative guards."""
        workflow = (ROOT / ".github/workflows/live-backend-tests.yml").read_text()
        fragment = (ROOT / "specs/ci-pr-workflows.d/live-backend-tests.yml").read_text()
        registry = (ROOT / "specs/ci-gates.d/gates-09.yaml").read_text()
        mirror = registry.split("  - id: live-backend-static-mirror\n", 1)[1].split(
            "\n  - id:", 1
        )[0]
        for path in (
            "scripts/lib/ifa_scheduled_events.py",
            "scripts/lib/ifa_scheduled_inventory.json",
            "scripts/lib/ifa_scheduled_manifest.py",
            "scripts/test-ifa-scheduled-manifest.py",
            "scripts/lib/ifa_scheduled_bundle.py",
            "scripts/test-ifa-scheduled-bundle.py",
            "scripts/test-ifa-scheduled-events.py",
            "scripts/test-ifa-scheduled-ledger-policy.py",
            "scripts/run-ifa-scheduled-proofs.sh",
            ".github/workflows/ifa-scheduled-proofs.yml",
            "specs/ci-gates.v1.yaml",
            "specs/ci-gates.d/gates-09.yaml",
        ):
            with self.subTest(path=path):
                self.assertIn(path, fragment)
                triggers = re.findall(r'^      - "([^"]+)"$', mirror, re.MULTILINE)
                self.assertTrue(
                    any(fnmatch.fnmatchcase(path, trigger) for trigger in triggers),
                    f"static mirror does not select {path}",
                )
        for command in (
            "python3 scripts/test-ifa-scheduled-events.py",
            "python3 scripts/test-ifa-scheduled-manifest.py",
            "python3 scripts/test-ifa-scheduled-bundle.py",
            "python3 scripts/test-ifa-scheduled-ledger-policy.py",
            "bash scripts/test-verify-live-tests-ledger.sh",
        ):
            with self.subTest(command=command):
                self.assertIn(command, workflow)
                self.assertIn(command, mirror)
        self.assertIn("merge_group:", workflow)
        self.assertIn("workflow_call:", workflow)


if __name__ == "__main__":
    unittest.main()
