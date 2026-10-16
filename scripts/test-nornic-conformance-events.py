"""Acceptance tests for exact NornicDB conformance enrollment."""

import ast
import hashlib
import importlib.util
import os
import io
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


LIB = Path(__file__).parent / "lib" / "nornic_conformance_events.py"
SPEC = importlib.util.spec_from_file_location("nornic_conformance_events", LIB)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class NornicConformanceTest(unittest.TestCase):
    """The hosted owner must execute every enrolled test exactly once."""

    def test_source_and_runner_are_bound(self) -> None:
        root = Path(__file__).resolve().parent.parent
        inventory = MODULE.load_inventory()
        self.assertEqual(len(inventory), 7)
        self.assertEqual(sum(len(tests) for tests in inventory.values()), 9)
        MODULE.verify_source_and_runner(root, inventory)
        runner = (root / MODULE.RUNNER).read_text()
        self.assertIn("python3 ../scripts/lib/nornic_conformance_events.py --source-check", runner)
        self.assertIn("ESHU_SEMANTIC_ENTITY_NORNICDB_LIVE=1", runner)

        with tempfile.TemporaryDirectory() as directory:
            copied = Path(directory)
            source = root / next(iter(inventory))
            target = copied / source.relative_to(root)
            target.parent.mkdir(parents=True)
            target.write_text(source.read_text() + "\nfunc TestUnselected(t *testing.T) {}\n")
            with self.assertRaises(ValueError):
                MODULE.verify_source(copied, inventory)

            script = (root / MODULE.RUNNER).read_text()
            for old, new in (
                ("python3 ../scripts/lib/nornic_conformance_events.py", "# python3 ../scripts/lib/nornic_conformance_events.py"),
                ("set -o pipefail", "set +o pipefail"),
                ("TestBuildLiveRetryContractRowsMirrorProductionPlatformBatch", "TestMissingHelper"),
                ("    set -o pipefail", "    if false; then\n    set -o pipefail"),
                ("set -euo pipefail", "set +e"),
            ):
                with self.subTest(old=old):
                    with self.assertRaises(ValueError):
                        MODULE.verify_runner_text(script.replace(old, new, 1), inventory)

            workflow = (root / MODULE.WORKFLOW).read_text()
            for old, new in (
                ("run: ./scripts/verify_backend_conformance_live.sh", "run: true"),
                ("      - name: Run live backend conformance", "      - name: Disabled conformance"),
                ("        run: ./scripts/verify_backend_conformance_live.sh", "        continue-on-error : true\n        run: ./scripts/verify_backend_conformance_live.sh"),
                ("        run: ./scripts/verify_backend_conformance_live.sh", "        if: ${{ false }}\n        run: ./scripts/verify_backend_conformance_live.sh"),
                ("    steps:", "    continue-on-error : true\n    steps:"),
                ("    steps:", "    'continue-on-error': true\n    steps:"),
                ("          - graph_backend: nornicdb", "          - graph_backend: neo4j"),
                ("            database: nornic", "            database: neo4j"),
                ("          ESHU_GRAPH_BACKEND: ${{ matrix.graph_backend }}", "          ESHU_GRAPH_BACKEND: neo4j"),
                ("          DEFAULT_DATABASE: ${{ matrix.database }}", "          DEFAULT_DATABASE: neo4j"),
                ("    branches: [ main ]\n    # Path-filtered", "    branches: [ develop ]\n    # Path-filtered"),
                ('      - "go/**"\n', ''),
                ('      - "go/**"\n', '      - "go/**"\n      - "!go/internal/storage/cypher/**"\n'),
                ("          - graph_backend: nornicdb\n            compose_file: docker-compose.yaml\n            graph_service: nornicdb\n            database: nornic\n",
                 "        exclude:\n          - graph_backend: nornicdb\n            compose_file: docker-compose.yaml\n            graph_service: nornicdb\n            database: nornic\n"),
                ("jobs:\n", '"jobs": {}\njobs:\n'),
                ("  pull_request:\n", "  pull_request :\n"),
            ):
                with self.subTest(old=old):
                    with self.assertRaises(ValueError):
                        MODULE.verify_workflow_text(workflow.replace(old, new, 1))

    def test_runner_preamble_and_tail_are_bound(self) -> None:
        """Executable code outside the Nornic branch cannot bypass CORE."""
        root = Path(__file__).resolve().parent.parent
        runner = (root / MODULE.RUNNER).read_text(encoding="utf-8")
        inventory = MODULE.load_inventory()
        MODULE.verify_runner_text(runner, inventory)
        cd = 'cd "$REPO_ROOT/go"\n'
        core = "go test ./internal/backendconformance -run '^TestLiveBackendConformance$' -count=1 -v"
        for label, changed in (
            ("early exit", runner.replace(cd, cd + "exit 0\n", 1)),
            ("early exec", runner.replace(cd, cd + "exec true\n", 1)),
            ("early false", runner.replace(cd, cd + "false\n", 1)),
            ("outer wrapper", runner.replace(core, "if false; then\n" + core, 1) + "fi\n"),
            ("omitted core", runner.replace(core, "true", 1)),
            ("tail exit", runner + "exit 0\n"),
        ):
            with self.subTest(label=label):
                self.assertNotEqual(changed, runner)
                with self.assertRaises(ValueError):
                    MODULE.verify_runner_text(changed, inventory)

    def test_active_metadata_routes_and_static_owner(self) -> None:
        """A source edit must select both hosted jobs and their local mirror."""
        root = Path(__file__).resolve().parent.parent
        e2e = (root / MODULE.WORKFLOW).read_text()
        live = (root / ".github/workflows/live-backend-tests.yml").read_text()
        registry = (root / "specs/ci-gates.d/gates-09.yaml").read_text()
        MODULE.verify_routing_text(e2e, live, registry)
        for changed, subject in (
            (e2e.replace('      - "scripts/verify_backend_conformance_live.sh"\n', '', 1), "e2e"),
            (e2e.replace('      - ".github/workflows/live-backend-tests.yml"\n', '', 1), "e2e"),
            (e2e.replace('      - "specs/ci-gates.d/gates-09.yaml"\n', '', 1), "e2e"),
            (live.replace("      - 'scripts/verify_backend_conformance_live.sh'\n", "", 1), "live"),
            (registry.replace('      - "scripts/verify_backend_conformance_live.sh"\n', '', 1), "registry"),
            (registry.replace('      - "scripts/lib/nornic_conformance_events.py"\n', '', 1), "registry"),
            (registry.replace('      - "specs/live-tests.d/**"\n',
                              '      - "specs/live-tests.d/live-tests-04.yaml"\n', 1), "registry"),
            (registry.replace("    triggers:\n", "    triggers :\n", 1), "registry"),
            (registry.replace("    triggers:\n", "    triggers: []\n    triggers:\n", 1), "registry"),
            (live.replace("    branches: [ main ]\n    paths:",
                          "    branches: [ other ]\n    paths:", 1), "live"),
            (live.replace("      - 'specs/live-tests.d/**'\n", "", 1), "live"),
            (live.replace("        run: python3 scripts/test-nornic-conformance-events.py",
                          "        run: true\n        # run: python3 scripts/test-nornic-conformance-events.py", 1), "live"),
            (live.replace("  static-mirror:\n", "  static-mirror :\n", 1), "live"),
        ):
            with self.subTest(subject=subject, changed=changed):
                subjects = {"e2e": e2e, "live": live, "registry": registry}
                self.assertNotEqual(changed, subjects[subject])
                subjects[subject] = changed
                with self.assertRaises(ValueError):
                    MODULE.verify_routing_text(**subjects)

    def test_ledger_command_rejects_removed_static_guard_step(self) -> None:
        """The always-run ledger command must read the active static workflow."""
        root = Path(__file__).resolve().parent.parent
        with tempfile.TemporaryDirectory() as directory:
            hook = Path(directory) / "sitecustomize.py"
            hook.write_text('''from pathlib import Path
import atexit
import os
import sys

original = Path.read_text
subject = Path(os.environ["N7_STATIC_SUBJECT"]).resolve()
reads = 0

def seeded_read(self, *args, **kwargs):
    global reads
    text = original(self, *args, **kwargs)
    if self.resolve() == subject:
        reads += 1
        start = text.index("      - name: Test Nornic conformance enrollment\\n")
        end = text.index("\\n      - name:", start + 1)
        return text[:start] + text[end:]
    return text

Path.read_text = seeded_read
atexit.register(lambda: print(f"N7_MUTATED_SUBJECT_READS={reads}", file=sys.stderr))
''')
            result = subprocess.run(
                ["bash", "scripts/verify-live-tests-ledger.sh"],
                cwd=root, capture_output=True, text=True, check=False,
                env={**os.environ, "PYTHONPATH": directory,
                     "PYTHONDONTWRITEBYTECODE": "1",
                     "N7_STATIC_SUBJECT": str(root / MODULE.STATIC_WORKFLOW)},
            )
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("N7_MUTATED_SUBJECT_READS=1", result.stderr)
        self.assertIn("missing active Nornic static mirror step", result.stderr)

    def test_ledger_command_rejects_early_runner_exit(self) -> None:
        """The production ledger guard must reject a successful early exit."""
        root = Path(__file__).resolve().parent.parent
        with tempfile.TemporaryDirectory() as directory:
            hook = Path(directory) / "sitecustomize.py"
            hook.write_text('''from pathlib import Path
import atexit
import os
import sys

original = Path.read_text
subject = Path(os.environ["N7_RUNNER_SUBJECT"]).resolve()
reads = 0

def seeded_read(self, *args, **kwargs):
    global reads
    text = original(self, *args, **kwargs)
    if self.resolve() == subject:
        reads += 1
        needle = 'cd "$REPO_ROOT/go"\\n'
        if text.count(needle) != 1:
            raise ValueError("expected one runner directory change")
        return text.replace(needle, needle + "exit 0\\n", 1)
    return text

Path.read_text = seeded_read
atexit.register(lambda: print(f"N7_MUTATED_SUBJECT_READS={reads}", file=sys.stderr))
''')
            seeded = subprocess.run(
                ["bash", "scripts/verify-live-tests-ledger.sh"],
                cwd=root, capture_output=True, text=True, check=False,
                env={**os.environ, "PYTHONPATH": directory,
                     "PYTHONDONTWRITEBYTECODE": "1",
                     "N7_RUNNER_SUBJECT": str(root / MODULE.RUNNER)},
            )
        self.assertNotEqual(seeded.returncode, 0, seeded.stdout + seeded.stderr)
        self.assertIn("N7_MUTATED_SUBJECT_READS=1", seeded.stderr)
        self.assertIn("runner", seeded.stderr)
        clean = subprocess.run(
            ["bash", "scripts/verify-live-tests-ledger.sh"],
            cwd=root, capture_output=True, text=True, check=False,
        )
        self.assertEqual(clean.returncode, 0, clean.stdout + clean.stderr)
        self.assertIn("live-tests ledger ok", clean.stdout)

    def test_ledger_command_rejects_inactive_verifier_cli(self) -> None:
        """The ledger must reject a verifier whose CLI can falsely succeed."""
        root = Path(__file__).resolve().parent.parent
        subject = root / "scripts/lib/nornic_conformance_events.py"
        source = subject.read_text(encoding="utf-8")
        entry = 'if __name__ == "__main__":\n    raise SystemExit(main())\n'
        main = ('def main() -> int:\n'
                '    """Check runner enrollment, then optionally a captured Go event stream."""\n')
        self.assertEqual(source.count(entry), 1)
        self.assertEqual(source.count(main), 1)
        mutations = (
            ("constant CLI success", source.replace(entry, entry.replace("main()", "0"), 1)),
            ("early main return", source.replace(main, main + "    return 0\n", 1)),
            ("top-level success", source + "raise SystemExit(0)\n"),
            ("false wrapper", source.replace(entry, entry.replace('__name__ == "__main__"', 'False'), 1)),
        )
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            hook = path / "sitecustomize.py"
            hook.write_text('''from importlib.machinery import SourceFileLoader
from pathlib import Path
import atexit
import os
import sys

original_read = Path.read_text
original_data = SourceFileLoader.get_data
subject = Path(os.environ["N7_CLI_SUBJECT"]).resolve()
replacement = original_read(Path(os.environ["N7_CLI_REPLACEMENT"]), encoding="utf-8")
reads = 0

def seeded_read(self, *args, **kwargs):
    global reads
    text = original_read(self, *args, **kwargs)
    if self.resolve() == subject:
        reads += 1
        return replacement
    return text

def seeded_data(self, filename):
    global reads
    data = original_data(self, filename)
    if Path(filename).resolve() == subject:
        reads += 1
        return replacement.encode("utf-8")
    return data

Path.read_text = seeded_read
SourceFileLoader.get_data = seeded_data
atexit.register(lambda: print(f"N7_MUTATED_SUBJECT_READS={reads}", file=sys.stderr))
''')
            for label, changed in mutations:
                with self.subTest(label=label):
                    self.assertNotEqual(changed, source)
                    replacement = path / "mutated.py"
                    replacement.write_text(changed, encoding="utf-8")
                    result = subprocess.run(
                        ["bash", "scripts/verify-live-tests-ledger.sh"],
                        cwd=root, capture_output=True, text=True, check=False,
                        env={**os.environ, "PYTHONPATH": directory,
                             "PYTHONPYCACHEPREFIX": str(path / "pycache"),
                             "PYTHONDONTWRITEBYTECODE": "1",
                             "N7_CLI_SUBJECT": str(subject),
                             "N7_CLI_REPLACEMENT": str(replacement)},
                    )
                    self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn("N7_MUTATED_SUBJECT_READS=1", result.stderr)
                    self.assertIn("CLI activation", result.stderr)
        clean = subprocess.run(
            ["bash", "scripts/verify-live-tests-ledger.sh"],
            cwd=root, capture_output=True, text=True, check=False,
        )
        self.assertEqual(clean.returncode, 0, clean.stdout + clean.stderr)
        self.assertIn("live-tests ledger ok", clean.stdout)

    def test_cli_semantics_survive_hash_repin(self) -> None:
        """A new digest alone cannot authorize a CLI that skips validation."""
        root = Path(__file__).resolve().parent.parent
        ledger_source = (root / "scripts/lib/verify-live-tests-ledger.py").read_text()
        tree = ast.parse(ledger_source)
        names = {"NORNIC_CLI_SOURCE_SHA256", "NORNIC_CLI_MAIN",
                 "NORNIC_CLI_DISPATCH"}
        definitions = [
            node for node in tree.body
            if isinstance(node, ast.Assign)
            and any(isinstance(target, ast.Name) and target.id in names
                    for target in node.targets)
        ]
        functions = [node for node in tree.body
                     if isinstance(node, ast.FunctionDef)
                     and node.name == "verify_nornic_cli_activation"]
        self.assertEqual(len(definitions), 3)
        self.assertEqual(len(functions), 1)
        contract = ast.fix_missing_locations(
            ast.Module(body=definitions + functions, type_ignores=[]))
        namespace = {"ast": ast, "hashlib": hashlib, "Path": Path}
        exec(compile(contract, "<production CLI activation>", "exec"), namespace)
        source = (root / MODULE.__file__).read_text()
        entry = 'if __name__ == "__main__":\n    raise SystemExit(main())\n'
        main = ('def main() -> int:\n'
                '    """Check runner enrollment, then optionally a captured Go event stream."""\n')
        for changed in (
            source.replace(entry, entry.replace("main()", "0"), 1),
            source.replace(main, main + "    return 0\n", 1),
            source + "raise SystemExit(0)\n",
            source.replace(entry, entry.replace('__name__ == "__main__"', 'False'), 1),
        ):
            with tempfile.TemporaryDirectory() as directory:
                subject = Path(directory) / "mutated.py"
                subject.write_text(changed)
                namespace["NORNIC_CLI_SOURCE_SHA256"] = hashlib.sha256(
                    changed.encode()).hexdigest()
                with self.assertRaises(ValueError):
                    namespace["verify_nornic_cli_activation"](subject)

    def test_inventory_cannot_swap_reviewed_files(self) -> None:
        inventory = MODULE.load_inventory()
        first = next(iter(inventory))
        swapped = dict(inventory)
        swapped[first + ".other"] = swapped.pop(first)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "inventory.json"
            path.write_text(json.dumps(swapped))
            with self.assertRaises(ValueError):
                MODULE.load_inventory(path)

    def test_ledger_rows_close_even_with_recomputed_digest(self) -> None:
        root = Path(__file__).resolve().parent.parent
        inventory = MODULE.load_inventory()
        ledger = MODULE.read_ledger_rows(root)
        MODULE.verify_ledger(inventory, ledger)
        first = next(iter(inventory))
        wrong = dict(ledger)
        wrong[first] = ("scheduled", "", "")
        with self.assertRaises(ValueError):
            MODULE.verify_ledger(inventory, wrong)

    def test_validator_rejects_demoted_row_with_recomputed_digest(self) -> None:
        """A ledger checksum update cannot hide an unenrolled owner row."""
        root = Path(__file__).resolve().parent.parent
        inventory = MODULE.load_inventory()
        path = next(iter(inventory))
        flattened = subprocess.run(
            ["python3", "scripts/lib/live_tests_registry.py", "flatten",
             "specs/live-tests.v1.yaml"], cwd=root, capture_output=True,
            text=True, check=True,
        ).stdout
        marker = f"  - file: {path}\n"
        start = flattened.index(marker)
        end = flattened.index("  - file: ", start + len(marker))
        body = flattened[start:end]
        body = body.replace("    class: dedicated_ci", "    class: scheduled", 1)
        body = body.replace("    runner: backend-conformance-nornicdb\n", "", 1)
        flattened = flattened[:start] + body + flattened[end:]
        scheduled = sorted(re.findall(
            r"(?m)^  - file: (\S+)\n    tag: .*\n    class: scheduled$",
            flattened,
        ))
        digest = hashlib.sha256(
            "".join(f"{name}\n" for name in scheduled).encode("utf-8")
        ).hexdigest()
        flattened = re.sub(r"(?m)^  count: \d+$", f"  count: {len(scheduled)}", flattened, count=1)
        flattened = re.sub(r"(?m)^  sha256: [0-9a-f]{64}$", f"  sha256: {digest}", flattened, count=1)
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory) / "ledger.yaml"
            fixture.write_text(flattened)
            process = subprocess.run(
                ["bash", "scripts/verify-live-tests-ledger.sh"], cwd=root,
                env={**os.environ, "LEDGER_PATH": str(fixture)},
                capture_output=True, text=True, check=False,
            )
        self.assertNotEqual(process.returncode, 0)
        self.assertIn("Nornic conformance runner enrollment failed", process.stderr)

    def test_go_events_reject_missing_skip_duplicate_and_wrong_package(self) -> None:
        inventory = MODULE.load_inventory()
        names = {name for tests in inventory.values() for name in tests}
        package = MODULE.PACKAGE
        good = [item for name in sorted(names) for item in (
            {"Action": "run", "Package": package, "Test": name},
            {"Action": "pass", "Package": package, "Test": name},
        )] + [{"Action": "pass", "Package": package}]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            for events, passes in (
                (good, True),
                (good[1:], False),
                (good[:-2] + [{**good[-2], "Action": "skip"}, good[-1]], False),
                (good + [good[1]], False),
                ([{**item, "Package": package + "x"} for item in good], False),
                (good[:-1], False),
            ):
                path.write_text("".join(json.dumps(item) + "\n" for item in events))
                with self.subTest(events=events):
                    self.assertEqual(MODULE.verify_events(path, names, io.StringIO()), passes)


if __name__ == "__main__":
    unittest.main()
