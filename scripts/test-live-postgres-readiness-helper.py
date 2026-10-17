#!/usr/bin/env python3
"""Check that readiness owns a helper through its proven consumer."""

from __future__ import annotations

import contextlib
import hashlib
import io
import pathlib
import re
import sys
import tempfile
import unittest
from unittest import mock

LIB = pathlib.Path(__file__).parent / "lib"
ROOT = LIB.parent.parent
sys.path.insert(0, str(LIB))

import live_postgres_readiness_results as readiness  # noqa: E402
from live_tests_registry import load_ledger_text  # noqa: E402

HELPER = "go/internal/query/supply/chain/impact/suppression_plan_proof_live_test.go"
CONSUMER = "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go"


class HelperEnrollmentTest(unittest.TestCase):
    """Exercise the production ledger guard with disposable source copies."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.ledger = self.root / "ledger.yaml"
        self.ledger.write_text(load_ledger_text(ROOT / "specs/live-tests.v1.yaml"),
                               encoding="utf-8")
        for path in [*readiness.EXPECTED, HELPER]:
            target = self.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.symlink_to(ROOT / path)

    def verify_output(self) -> tuple[int, str]:
        """Run the production guard and return its failure diagnostic."""
        error = io.StringIO()
        with contextlib.redirect_stderr(error), contextlib.redirect_stdout(io.StringIO()):
            status = readiness.verify_ledger(self.ledger, self.root)
        return status, error.getvalue()

    def verify(self) -> int:
        """Return the production guard status before any Go invocation."""
        return self.verify_output()[0]

    def change_ledger(self, old: str, new: str) -> None:
        """Replace exactly one reviewed ledger fragment."""
        body = self.ledger.read_text(encoding="utf-8")
        self.assertIn(old, body)
        self.ledger.write_text(body.replace(old, new, 1), encoding="utf-8")

    def edit_source(self, path: str, old: str, new: str) -> None:
        """Mutate a local copy without changing the reviewed Go source."""
        target = self.root / path
        body = target.read_text(encoding="utf-8")
        self.assertIn(old, body)
        target.unlink()
        target.write_text(body.replace(old, new, 1), encoding="utf-8")

    def test_reviewed_helper_row_is_owned_without_a_new_root(self) -> None:
        self.assertEqual(self.verify(), 0)
        self.assertNotIn(HELPER, readiness.EXPECTED)
        self.assertIn("TestSupplyChainSuppressionPathsPerformanceLive",
                      readiness.EXPECTED[CONSUMER])

    def test_missing_or_reclassified_helper_row_fails(self) -> None:
        row = f"  - file: {HELPER}\n"
        self.change_ledger(row, "  - file: go/ignored/helper_live_test.go\n")
        self.assertNotEqual(self.verify(), 0)

    def test_helper_test_root_and_source_drift_fail(self) -> None:
        self.edit_source(HELPER, "package impact", "package impact\nfunc TestHiddenLive() {}")
        status, message = self.verify_output()
        self.assertNotEqual(status, 0)
        self.assertIn("helper proof source declares a Test root", message)

    def test_bare_go_test_root_fails_even_with_renewed_source_pin(self) -> None:
        """Go accepts a bare Test name, so the zero-root check must see it."""
        self.edit_source(HELPER, "package impact",
                         "package impact\nfunc Test(t *testing.T) {}")
        source = (self.root / HELPER).read_bytes()
        original = readiness.HELPER_PROOFS[HELPER]
        # This one temporary repin isolates the semantic zero-root check.
        repinned = {HELPER: {**original, "helper_sha256": hashlib.sha256(source).hexdigest()}}
        with mock.patch.object(readiness, "HELPER_PROOFS", repinned):
            status, message = self.verify_output()
        self.assertNotEqual(status, 0)
        self.assertIn("helper proof source declares a Test root", message)

    def test_lowercase_test_prefix_is_not_a_go_test_root(self) -> None:
        """Go treats a lowercase suffix after Test as a helper name."""
        self.edit_source(HELPER, "package impact",
                         "package impact\nfunc Testinternal(t *testing.T) {}")
        source = (self.root / HELPER).read_bytes()
        original = readiness.HELPER_PROOFS[HELPER]
        repinned = {HELPER: {**original, "helper_sha256": hashlib.sha256(source).hexdigest()}}
        with mock.patch.object(readiness, "HELPER_PROOFS", repinned):
            self.assertEqual(self.verify(), 0)

    def test_unicode_ordinal_feminine_suffix_is_a_go_test_root(self) -> None:
        """Go does not classify U+00AA as a lowercase letter."""
        self.edit_source(HELPER, "package impact",
                         "package impact\nfunc Testª(t *testing.T) {}")
        source = (self.root / HELPER).read_bytes()
        original = readiness.HELPER_PROOFS[HELPER]
        repinned = {HELPER: {**original, "helper_sha256": hashlib.sha256(source).hexdigest()}}
        with mock.patch.object(readiness, "HELPER_PROOFS", repinned):
            status, message = self.verify_output()
        self.assertNotEqual(status, 0)
        self.assertIn("helper proof source declares a Test root", message)

    def test_helper_row_rejects_any_backend_field(self) -> None:
        """PostgreSQL helper ownership has no graph-backend metadata."""
        original = self.ledger.read_text(encoding="utf-8")
        row = f"  - file: {HELPER}\n"
        start = original.index(row)
        end = original.find("  - file: ", start + len(row))
        self.assertGreater(end, start)
        section = original[start:end]
        runner = "    runner: live-postgres-readiness\n"
        self.assertIn(runner, section)
        for backend in ("neo4j", "~"):
            with self.subTest(backend=backend):
                changed = section.replace(runner, runner + f"    backends: {backend}\n", 1)
                self.ledger.write_text(original[:start] + changed + original[end:],
                                       encoding="utf-8")
                status, message = self.verify_output()
                self.assertNotEqual(status, 0)
                self.assertIn("helper row has unexpected backends", message)

    def test_consumer_call_removal_fails(self) -> None:
        self.edit_source(CONSUMER, "suppressionListPlanArgs(listFilter, readAt)",
                         "[]any{}")
        status, message = self.verify_output()
        self.assertNotEqual(status, 0)
        self.assertIn(f"source changed: {CONSUMER}", message)

    def test_helper_cannot_also_be_an_executable_inventory_file(self) -> None:
        with mock.patch.dict(readiness.EXPECTED, {HELPER: ()}):
            self.assertNotEqual(self.verify(), 0)

    def test_closed_map_rejects_missing_extra_and_malformed_entries(self) -> None:
        original = readiness.HELPER_PROOFS[HELPER]
        for changed in ({}, {**readiness.HELPER_PROOFS, "go/other_live_test.go": original},
                        {HELPER: {}}, {HELPER: {**original, "unused": "value"}},
                        {HELPER: {**original, "helper_sha256": "invalid"}}):
            with self.subTest(changed=changed), mock.patch.object(
                    readiness, "HELPER_PROOFS", changed):
                self.assertNotEqual(self.verify(), 0)

    def test_consumer_mapping_must_name_the_enrolled_owner(self) -> None:
        original = readiness.HELPER_PROOFS[HELPER]
        cases = (
            {"consumer_file": HELPER},
            {"consumer_file": "go/internal/query/supply/chain/impact/missing_live_test.go"},
            {"consumer_test": "TestUnknownLive"},
        )
        for change in cases:
            with self.subTest(change=change), mock.patch.object(
                    readiness, "HELPER_PROOFS", {HELPER: {**original, **change}}):
                self.assertNotEqual(self.verify(), 0)

    def test_helper_and_consumer_row_class_runner_and_tag_fail_closed(self) -> None:
        for path in (HELPER, CONSUMER):
            for field, value in (("class", "scheduled"), ("runner", "other"),
                                 ("tag", "live_nornicdb_answer_truth")):
                with self.subTest(path=path, field=field):
                    original = self.ledger.read_text(encoding="utf-8")
                    row = f"  - file: {path}\n"
                    start = original.index(row)
                    end = original.find("  - file: ", start + len(row))
                    if end < 0:
                        end = len(original)
                    section = original[start:end]
                    changed = re.sub(rf"^    {field}: .*?$", f"    {field}: {value}",
                                     section, count=1, flags=re.MULTILINE)
                    self.assertNotEqual(section, changed)
                    self.ledger.write_text(original[:start] + changed + original[end:],
                                           encoding="utf-8")
                    self.assertNotEqual(self.verify(), 0)
                    self.ledger.write_text(original, encoding="utf-8")

    def test_package_tag_and_consumer_pin_drift_fail(self) -> None:
        for path, old, new, diagnostic in (
                (HELPER, "package impact", "package other", f"source changed: {HELPER}"),
                (HELPER, "package impact", "//go:build integration\npackage impact",
                 f"build tag: {HELPER}"),
                (CONSUMER, "package impact", "package other", f"packages differ: {CONSUMER}"),
                (CONSUMER, "package impact", "//go:build integration\npackage impact",
                 f"build tag: {CONSUMER}")):
            with self.subTest(path=path, new=new):
                for source_path in (HELPER, CONSUMER):
                    target = self.root / source_path
                    target.unlink(missing_ok=True)
                    target.symlink_to(ROOT / source_path)
                self.edit_source(path, old, new)
                status, message = self.verify_output()
                self.assertNotEqual(status, 0)
                self.assertIn(diagnostic, message)

    def test_missing_source_fails(self) -> None:
        (self.root / HELPER).unlink()
        status, message = self.verify_output()
        self.assertNotEqual(status, 0)
        self.assertIn(f"source unavailable: {HELPER}", message)


if __name__ == "__main__":
    unittest.main()
