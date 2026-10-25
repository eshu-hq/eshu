"""Seed route and event violations for the delta union scheduled proof."""

import importlib.util
import fnmatch
import io
import os
import re
import subprocess
import tempfile
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts/lib"))


class DeltaUnionScheduledTest(unittest.TestCase):
    """The scheduled route must stay active and select both exact proofs."""

    @classmethod
    def setUpClass(cls) -> None:
        """Load the guard as the workflow uses it."""
        path = ROOT / "scripts/lib/delta_union_scheduled.py"
        spec = importlib.util.spec_from_file_location("delta_union_scheduled", path)
        assert spec and spec.loader
        cls.guard = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.guard)

    def test_source_and_workflow_routes(self) -> None:
        """Source, selection, services, and activation stay tied together."""
        self.guard.verify(ROOT)
        workflow = (ROOT / self.guard.WORKFLOW).read_text()
        dependency = "      - 'scripts/ci/go-mod-download-retry.sh'\n"
        self.assertIn(dependency, workflow)
        changes = {
            "missing_module_prewarm_route": workflow.replace(dependency, "", 1),
            "missing_internal_dependency_route": workflow.replace(
                "      - 'go/internal/**'\n", "", 1),
            "missing_root": workflow.replace("  schedule:\n", "  inactive_schedule:\n", 1),
            "disabled_job": workflow.replace("    runs-on: ubuntu-24.04", "    if: false\n    runs-on: ubuntu-24.04", 1),
            "root_shell_override": workflow.replace(
                "\npermissions:\n", "\ndefaults:\n  run:\n    shell: echo {0}\n\npermissions:\n", 1),
            "job_shell_override": workflow.replace(
                "    services:\n", "    defaults:\n      run:\n        shell: echo {0}\n    services:\n", 1),
            "step_shell_override": workflow.replace(
                "        working-directory: go\n", "        shell: echo {0}\n        working-directory: go\n", 1),
            "checkout_ref_override": workflow.replace(
                "          persist-credentials: false\n",
                "          persist-credentials: false\n          ref: main\n", 1),
            "unknown_service_environment": workflow.replace(
                "          NEO4J_AUTH: none\n",
                "          NEO4J_AUTH: none\n          NEO4J_dbms_security_auth__enabled: false\n", 1),
            "disabled_step": workflow.replace("      - name: Run delta union proofs", "      - name: Run delta union proofs\n        if: false", 1),
            "suppressed_failure": workflow.replace("      - name: Run delta union proofs", "      - name: Run delta union proofs\n        continue-on-error: true", 1),
            "early_exit": workflow.replace("          set -euo pipefail\n", "          set -euo pipefail\n          exit 0\n", 1),
            "contract_early_exit": workflow.replace(
                "          python3 scripts/test-delta-union-scheduled.py\n",
                "          exit 0\n          python3 scripts/test-delta-union-scheduled.py\n", 1),
            "metadata_early_exit": workflow.replace(
                "          {\n            printf 'checkout_sha=",
                "          exit 0\n          {\n            printf 'checkout_sha=", 1),
            "missing_backend_version": workflow.replace(
                "          neo_version=\"$(docker exec '${{ job.services.neo4j.id }}' cypher-shell",
                "          neo_version=\"$(printf skipped # docker exec '${{ job.services.neo4j.id }}' cypher-shell", 1),
            "bad_postgres_pin": workflow.replace("ci-postgres-bookworm@sha256:", "ci-postgres-alpine@sha256:", 1),
            "bad_neo4j_pin": workflow.replace("ghcr.io/eshu-hq/ci-neo4j-community@sha256:", "neo4j:latest@sha256:", 1),
            "missing_selector": workflow.replace("TestDeltaUnionBaseMissingRefusesWithoutPartialLive", "TestUnrelatedProof", 1),
            "missing_event_verifier": workflow.replace("--events \"${RUNNER_TEMP}/delta-union.jsonl\"", "--events wrong.jsonl", 1),
        }
        expected_errors = {
            "missing_internal_dependency_route": "scoped PR dependency route changed",
            "root_shell_override": "non-fail-closed workflow key",
            "job_shell_override": "non-fail-closed workflow key",
            "step_shell_override": "non-fail-closed workflow key",
            "early_exit": "non-fail-closed delta union step command body",
            "contract_early_exit": "non-fail-closed delta union step command body",
            "metadata_early_exit": "non-fail-closed delta union step command body",
            "checkout_ref_override": "checkout must use the workflow revision",
            "unknown_service_environment": "reviewed workflow bytes changed",
        }
        for name, changed in changes.items():
            with self.subTest(name=name):
                with self.assertRaisesRegex(ValueError, expected_errors.get(name, ".")):
                    self.guard.verify(ROOT, changed)

    def test_production_dependencies_trigger_pr_proof(self) -> None:
        """Direct Go imports must select the proof on scoped PR changes."""
        workflow = (ROOT / self.guard.WORKFLOW).read_text()
        paths = re.findall(r"(?m)^      - '([^']+)'$", workflow.partition("\njobs:\n")[0])
        registry = (ROOT / "specs/ci-gates.d/gates-10.yaml").read_text()
        gate = registry.split("  - id: delta-union-scheduled\n", 1)[1].split("\n  - id: ", 1)[0]
        triggers = re.findall(r'(?m)^      - "([^"]+)"$', gate)
        package_files = sorted((ROOT / "go/cmd/projector").glob("*.go"))
        self.assertTrue(package_files)
        imports = set()
        for source in package_files:
            imports.update(re.findall(
                r'"github\.com/eshu-hq/eshu/go/(internal/[^"\s]+)"', source.read_text()))
        self.assertIn("internal/storage/postgres", imports)
        self.assertIn("internal/scope", imports)
        self.assertIn("internal/content", imports)
        self.assertIn("internal/repositoryidentity", imports)
        for imported in sorted(imports):
            package_sources = sorted((ROOT / "go" / imported).glob("*.go"))
            with self.subTest(package=imported):
                self.assertTrue(package_sources, imported)
                changed_path = package_sources[0].relative_to(ROOT).as_posix()
                self.assertTrue(any(fnmatch.fnmatchcase(changed_path, pattern) for pattern in paths), changed_path)
                self.assertTrue(any(fnmatch.fnmatchcase(changed_path, pattern) for pattern in triggers), changed_path)
        module = (ROOT / "go/go.mod").read_text()
        for path, replacement in (
            ("go/pkg/log/log.go", "github.com/eshu-hq/eshu/go/pkg/log"),
            ("sdk/go/collector/types.go", "../sdk/go/collector"),
            ("sdk/go/factschema/decode_map_coerce.go", "../sdk/go/factschema"),
        ):
            with self.subTest(path=path):
                self.assertTrue((ROOT / path).is_file(), path)
                if path.startswith("sdk/"):
                    # Post-#8851 the main module pins the standalone SDK
                    # releases and carries no workspace replaces (C4); keep
                    # demanding the replace only while a main-module file
                    # still imports the retired in-tree path.
                    old_module = "github.com/eshu-hq/eshu/" + replacement[3:]
                    try:
                        still_imported = subprocess.check_output(
                            ["git", "-C", str(ROOT), "grep", "-l", "-F",
                             old_module, "--", "go/"],
                            text=True,
                        ).strip()
                    except subprocess.CalledProcessError:
                        still_imported = ""
                    if still_imported:
                        self.assertIn(replacement, module)
                self.assertTrue(any(fnmatch.fnmatchcase(path, pattern) for pattern in paths), path)
                self.assertTrue(any(fnmatch.fnmatchcase(path, pattern) for pattern in triggers), path)

    def test_ledger_demotion_and_false_overlay_enrollment(self) -> None:
        """The ledger rejects drift even if its legacy digest is repinned."""
        source = subprocess.check_output(
            ["python3", str(ROOT / "scripts/lib/live_tests_registry.py"),
             "flatten", str(ROOT / "specs/live-tests.v1.yaml")], text=True)
        target = "go/cmd/projector/delta_union_live_test.go"
        overlay = "go/cmd/projector/superseded_delta_overlay_live_test.go"
        self.assertIn(f"  - file: {target}\n", source)
        self.assertIn(f"  - file: {overlay}\n", source)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.yaml"
            env = {**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(path)}
            script = ROOT / "scripts/verify-live-tests-ledger.sh"
            path.write_text(source)
            clean = subprocess.run([str(script)], env=env, capture_output=True, text=True)
            self.assertEqual(clean.returncode, 0, clean.stderr)
            variants = {
                "stale_digest": re.sub(r"(?m)^  sha256: [0-9a-f]{64}$",
                                       "  sha256: " + "0" * 64, source, count=1),
                "false_overlay_row": source.replace(
                    f"  - file: {overlay}\n    tag: ~\n    class: scheduled\n",
                    f"  - file: {overlay}\n    tag: ~\n    class: verified_scheduled\n"
                    "    runner: delta-union-scheduled\n", 1),
                "demoted_union": source.replace(
                    f"  - file: {target}\n    tag: ~\n    class: verified_scheduled\n",
                    f"  - file: {target}\n    tag: ~\n    class: scheduled\n", 1),
            }
            for name, changed in variants.items():
                with self.subTest(name=name):
                    self.assertNotEqual(changed, source)
                    path.write_text(changed)
                    result = subprocess.run([str(script)], env=env,
                                            capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0, result.stderr)

    def test_event_accounting(self) -> None:
        """The shared verifier rejects missing, skipped, and failed tests."""
        from reducer_contention_events import verify_events
        import json
        import tempfile
        package = self.guard.PACKAGE
        names = self.guard.TESTS
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            for wrong in ("missing", "skip", "fail"):
                events = []
                for name in names:
                    if wrong == "missing" and name == names[-1]:
                        continue
                    terminal = wrong if wrong in {"skip", "fail"} and name == names[-1] else "pass"
                    events.extend(({"Package": package, "Test": name, "Action": action}
                                   for action in ("run", terminal)))
                events.append({"Package": package, "Action": "pass"})
                path.write_text("".join(json.dumps(event) + "\n" for event in events))
                self.assertFalse(verify_events(path, package, set(names), io.StringIO()))


if __name__ == "__main__":
    unittest.main()
