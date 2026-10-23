#!/usr/bin/env python3
"""Exercise the real B-7 shell guard with independent Go event fixtures."""

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sys
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
HELPER = ROOT / "scripts/lib/golden-corpus-container-image-demotion.sh"
PROOFS = (
    ("github.com/eshu-hq/eshu/go/internal/reducer/containerimage",
     "TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive"),
    ("github.com/eshu-hq/eshu/go/internal/storage/postgres",
     "TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive"),
)


def events(package: str, name: str) -> list[dict[str, str]]:
    """Build one successful selected test and its enclosing package terminal."""
    return [
        {"Action": "run", "Package": package, "Test": name},
        {"Action": "pass", "Package": package, "Test": name},
        {"Action": "pass", "Package": package},
    ]


def guard(raw: str, name: str) -> subprocess.CompletedProcess[str]:
    """Invoke the production shell entrypoint, preserving its process status."""
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "events.jsonl"
        path.write_text(raw, encoding="utf-8")
        return subprocess.run(
            ["bash", "-c", '. "$1"; golden_corpus_exact_test_passed "$2" "$3"',
             "guard", str(HELPER), str(path), name],
            capture_output=True, text=True, check=False,
        )


def encode(rows: list[dict[str, str]]) -> str:
    """Encode the newline-delimited stdout format emitted by go test -json."""
    return "".join(json.dumps(row) + "\n" for row in rows)


class EventGuardTest(unittest.TestCase):
    """Matching names alone never prove that the selected package ran."""

    def test_success_and_unrelated_no_test_package(self) -> None:
        for package, name in PROOFS:
            with self.subTest(name=name):
                rows = events(package, name)
                rows.insert(0, {"Action": "skip", "Package": "example.invalid/no-tests"})
                result = guard(encode(rows), name)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_invalid_streams_fail(self) -> None:
        for package, name in PROOFS:
            good = events(package, name)
            seeds = {
                "wrong package": events("example.invalid/wrong", name),
                "missing package terminal": good[:2],
                "duplicate package terminal": good + [good[-1]],
                "failed package": good[:2] + [{"Action": "fail", "Package": package}],
                "skipped package": good[:2] + [{"Action": "skip", "Package": package}],
                "duplicate run": [good[0]] + good,
                "duplicate pass": good[:2] + [good[1]] + good[2:],
                "selected skip": [good[0], {**good[1], "Action": "skip"}, good[2]],
                "selected fail": good + [{**good[1], "Action": "fail"}],
                "wrong-package duplicate": good + events("example.invalid/wrong", name),
                "missing package": [{k: v for k, v in row.items() if k != "Package"}
                                    for row in good],
                "zero tests": [good[2]],
                "pass before run": [good[1], good[0], good[2]],
                "terminal before test": [good[2], good[0], good[1]],
            }
            for label, rows in seeds.items():
                with self.subTest(name=name, seed=label):
                    self.assertNotEqual(guard(encode(rows), name).returncode, 0)
            for raw in ("", "[]\n", "null\n", "not-json\n", encode(good) + "broken\n"):
                with self.subTest(name=name, raw=raw):
                    self.assertNotEqual(guard(raw, name).returncode, 0)

    def test_unknown_test_name_fails(self) -> None:
        self.assertNotEqual(guard(encode(events("example.invalid", "TestOther")),
                                  "TestOther").returncode, 0)


class HelperExitTest(unittest.TestCase):
    """A valid event file never masks an unsuccessful go test process."""

    def test_both_helpers_propagate_go_exit_and_guard_failure(self) -> None:
        functions = ("run_container_image_identity_demotion_proof",
                     "run_container_image_identity_cloud_reopen_ordering_proof")
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            fake_go = temporary / "go"
            fake_go.write_text('#!/bin/sh\ncat "$PROOF_EVENTS"\nexit "$PROOF_EXIT"\n')
            fake_go.chmod(0o755)
            event_path = temporary / "events.jsonl"
            for (package, name), function in zip(PROOFS, functions, strict=True):
                for go_exit, valid in ((0, True), (1, True), (0, False)):
                    with self.subTest(function=function, go_exit=go_exit, valid=valid):
                        event_path.write_text(encode(events(package, name) if valid else []))
                        code = ('set -euo pipefail; repo_root="$1"; log_dir="$2"; '
                                'log() { :; }; die() { exit 1; }; . "$3"; ' + function)
                        result = subprocess.run(
                            ["bash", "-c", code, "helper", str(ROOT), directory, str(HELPER)],
                            env={**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"],
                                 "PROOF_EVENTS": str(event_path), "PROOF_EXIT": str(go_exit),
                                 "ESHU_POSTGRES_DSN": "unused-fixture"},
                            capture_output=True, text=True, check=False, timeout=30,
                        )
                        self.assertEqual(result.returncode == 0, go_exit == 0 and valid,
                                         result.stdout + result.stderr)


def flatten() -> str:
    """Read the real ledger through its production fragment loader."""
    return subprocess.check_output(
        ["python3", str(ROOT / "scripts/lib/live_tests_registry.py"), "flatten",
         str(ROOT / "specs/live-tests.v1.yaml")], text=True,
    )


def repin(text: str) -> str:
    """Keep legacy metadata valid when planting an enrollment regression."""
    scheduled = sorted(re.findall(
        r"^  - file: (\S+)\n    tag: .*\n    class: scheduled$", text, re.M))
    digest = hashlib.sha256("".join(path + "\n" for path in scheduled).encode()).hexdigest()
    text = re.sub(r"(?m)^  count: \d+$", f"  count: {len(scheduled)}", text, count=1)
    return re.sub(r"(?m)^  sha256: [0-9a-f]{64}$", f"  sha256: {digest}", text, count=1)


def ledger_result(text: str) -> subprocess.CompletedProcess[str]:
    """Run the actual ledger validator against a standalone seeded ledger."""
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "ledger.yaml"
        path.write_text(text, encoding="utf-8")
        return subprocess.run(
            ["bash", str(ROOT / "scripts/verify-live-tests-ledger.sh")],
            env={**os.environ, "REPO_ROOT": str(ROOT), "LEDGER_PATH": str(path)},
            capture_output=True, text=True, check=False,
        )


class LedgerGuardTest(unittest.TestCase):
    """Existing B-7 proofs cannot be demoted by merely re-pinning the ledger."""

    def test_current_ledger(self) -> None:
        result = ledger_result(flatten())
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_closed_mapping_and_legacy_demotion(self) -> None:
        text = flatten()
        paths = [
            "go/internal/reducer/containerimage/"
            "container_image_identity_support_writer_demotion_postgres_live_test.go",
            "go/internal/storage/postgres/"
            "container_image_identity_cloud_reopen_ordering_postgres_live_test.go",
        ]
        for path in paths:
            marker = f"  - file: {path}\n"
            start = text.index(marker)
            following = text.find("  - file: ", start + len(marker))
            end = len(text) if following < 0 else following
            row = text[start:end]
            demoted = re.sub(r"    class: \S+", "    class: scheduled", row)
            demoted = demoted.replace("    runner: golden-corpus-identity\n", "")
            candidates = {
                "demoted with valid legacy digest": repin(text[:start] + demoted + text[end:]),
                "missing row": repin(text[:start] + text[end:]),
                "wrong runner": text[:start] + row.replace(
                    "    runner: golden-corpus-identity", "    runner: query-methodology") + text[end:],
                "graph backend claim": text[:start] + row.replace(
                    "    runner: golden-corpus-identity\n",
                    "    runner: golden-corpus-identity\n    backends: neo4j\n") + text[end:],
                "duplicate row": text + row,
            }
            for label, seeded in candidates.items():
                with self.subTest(path=path, seed=label):
                    result = ledger_result(seeded)
                    self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)



class SourceGuardTest(unittest.TestCase):
    """Mutate real runner sources to prove the closed inventory is executable."""

    def test_source_and_invocation_drift(self) -> None:
        sys.path.insert(0, str(ROOT / "scripts/lib"))
        from golden_corpus_identity import PROOFS as mapping, verify_inventory

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            relatives = list(mapping) + [
                "scripts/verify-golden-corpus-gate.sh",
                "scripts/lib/golden-corpus-container-image-demotion.sh",
            ]
            for relative in relatives:
                destination = root / relative
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, destination)
            verify_inventory(root)
            for relative, (_, name, function) in mapping.items():
                path = root / relative
                before = path.read_text()
                for changed in (before.replace(name, name + "Changed"),
                                before + "\nfunc TestExtra(t *testing.T) {}\n",
                                "//go:build integration\n" + before):
                    path.write_text(changed)
                    with self.assertRaises(ValueError):
                        verify_inventory(root)
                path.write_text(before)
                driver = root / "scripts/verify-golden-corpus-gate.sh"
                before = driver.read_text()
                for replacement in ("# " + function, "if false; then " + function + "; fi",
                                    function + " || true", "", function + "\n" + function):
                    driver.write_text(before.replace("\n" + function + "\n",
                                                     "\n" + replacement + "\n"))
                    with self.assertRaises(ValueError):
                        verify_inventory(root)
                driver.write_text(before)
            helper = root / "scripts/lib/golden-corpus-container-image-demotion.sh"
            before = helper.read_text()
            helper.write_text(before.replace(
                '\tgolden_corpus_exact_test_passed "${proof_json}" "${test_name}" ||',
                '\ttrue ||', 1))
            with self.assertRaises(ValueError):
                verify_inventory(root)



class RouteGuardTest(unittest.TestCase):
    """Every new input must still reach both the live and hermetic B-7 jobs."""

    def test_missing_inputs_and_inactive_owners_fail(self) -> None:
        sys.path.insert(0, str(ROOT / "scripts/lib"))
        from golden_corpus_identity import verify_routes

        inputs = ("scripts/lib/golden_corpus_identity.py",
                  "scripts/test-golden-corpus-identity.py",
                  "scripts/lib/verify-live-tests-ledger.py",
                  "scripts/lib/live_tests_registry.py",
                  "scripts/verify-live-tests-ledger.sh",
                  "specs/live-tests.v1.yaml", "specs/live-tests.d/**")

        relatives = [".github/workflows/golden-corpus-gate.yml",
                     "specs/ci-pr-workflows.d/golden-corpus-gate.yml",
                     "specs/ci-gates.d/gates-07.yaml", "specs/ci-gates.d/gates-08.yaml"]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for relative in relatives:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, path)
            verify_routes(root)
            for relative in relatives:
                path = root / relative
                before = path.read_text()
                for route in inputs:
                    line = ("  - " + route + "\n" if "ci-pr-workflows.d" in relative
                            else "      - \"" + route + "\"\n")
                    for occurrence in range(before.count(line)):
                        index = -1
                        for _ in range(occurrence + 1):
                            index = before.index(line, index + 1)
                        if relative.endswith("gates-08.yaml"):
                            owner = before[:index].rsplit("  - id: ", 1)[1].splitlines()[0]
                            if owner not in {"golden-corpus-gate", "golden-corpus-gate-neo4j"}:
                                continue
                        path.write_text(before[:index] + before[index + len(line):])
                        with self.subTest(relative=relative, route=route, index=occurrence):
                            with self.assertRaises(ValueError):
                                verify_routes(root)
                path.write_text(before)
            path = root / relatives[0]
            before = path.read_text()
            seeds = [before.replace("    needs: queue-selection\n", "", 1),
                     before.replace("branches: [ main ]", "branches: [ other ]", 1),
                     before.replace("  merge_group:", "  workflow_dispatch:", 1),
                     before.replace("  corpus-gate:\n", "  corpus-gate:\n    if: false\n"),
                     before.replace("  static-mirror:\n", "  static-mirror:\n    continue-on-error: true\n"),
                     before.replace("        run: bash scripts/verify-golden-corpus-gate.sh",
                                    "        run: bash scripts/verify-golden-corpus-gate.sh || true"),
                     before.replace("        run: bash scripts/test-verify-golden-corpus-gate.sh",
                                    "        run: true")]
            for index, seed in enumerate(seeds):
                path.write_text(seed)
                with self.subTest(seed=index), self.assertRaises(ValueError):
                    verify_routes(root)
            path.write_text(before)


if __name__ == "__main__":
    unittest.main()
