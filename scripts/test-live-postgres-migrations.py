#!/usr/bin/env python3
"""Check the dedicated migration proof inventory and fail-closed results."""

import json
import pathlib
import re
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts" / "lib"))

from live_postgres_migrations import (  # noqa: E402
    EXPECTED,
    MODULE_PACKAGE,
    verify_events,
    verify_ledger,
)


class MigrationProofInventoryTest(unittest.TestCase):
    """Reject unenrolled, retagged, skipped, or missing migration proofs."""

    def test_committed_ledger_and_inventory(self) -> None:
        self.assertEqual(verify_ledger(ROOT / "specs/live-tests.v1.yaml", ROOT), 0)
        self.assertEqual(sum(map(len, EXPECTED.values())), 7)

    def test_ci_helper_changes_select_migration_proof(self) -> None:
        workflow = (ROOT / ".github/workflows/live-postgres-migrations.yml").read_text(
            encoding="utf-8"
        )
        registry = (ROOT / "specs/ci-gates.d/gates-09.yaml").read_text(
            encoding="utf-8"
        )
        helper_paths = (
            "scripts/ci/go-mod-download-retry.sh",
            "scripts/ci/install-apt-packages.sh",
            "scripts/testdata/live-postgres-migrations-fake-docker.sh",
            "scripts/testdata/live-postgres-migrations-fake-go.sh",
        )
        for event in ("pull_request", "push"):
            match = re.search(
                rf"(?m)^  {event}:\n    branches: [^\n]+\n    paths:\n"
                r"((?:      - '[^']+'\n)+)",
                workflow,
            )
            self.assertIsNotNone(match, f"missing {event} path filter")
            paths = set(re.findall(r"(?m)^      - '([^']+)'$", match.group(1)))
            for helper in helper_paths:
                with self.subTest(selector=event, helper=helper):
                    self.assertIn(helper, paths)

        gate = re.search(
            r"(?ms)^  - id: live-postgres-migrations\n.*?"
            r"^    triggers:\n((?:      - \"[^\"]+\"\n)+)",
            registry,
        )
        self.assertIsNotNone(gate, "missing migration gate triggers")
        paths = set(re.findall(r'(?m)^      - "([^"]+)"$', gate.group(1)))
        for helper in helper_paths:
            with self.subTest(selector="registry", helper=helper):
                self.assertIn(helper, paths)

    def test_ci_heavy_migration_gate_does_not_claim_pre_push_floor(self) -> None:
        """A CI-only live gate must not promise a local pre-push run."""
        registry = (ROOT / "specs/ci-gates.d/gates-09.yaml").read_text(
            encoding="utf-8"
        )
        gate = re.search(
            r"(?ms)^  - id: live-postgres-migrations\n(.*?)(?=^  - id:|\Z)",
            registry,
        )
        self.assertIsNotNone(gate, "missing migration gate")
        self.assertIn("    tier: ci-heavy\n", gate.group(1))
        self.assertNotIn("      pre_push: floor\n", gate.group(1))

    def test_wrong_runner_and_tag_fail(self) -> None:
        source = (ROOT / "specs/live-tests.v1.yaml").read_text(encoding="utf-8")
        fragment = ROOT / "specs/live-tests.d/live-tests-06.yaml"
        body = fragment.read_text(encoding="utf-8")
        with tempfile.TemporaryDirectory() as temporary:
            directory = pathlib.Path(temporary)
            (directory / "live-tests.d").mkdir()
            for child in (ROOT / "specs/live-tests.d").glob("*.yaml"):
                (directory / "live-tests.d" / child.name).write_bytes(child.read_bytes())
            manifest = directory / "live-tests.v1.yaml"
            manifest.write_text(source, encoding="utf-8")
            self.assertEqual(verify_ledger(manifest, ROOT), 0)
            anchor = (
                "  - file: go/internal/storage/postgres/"
                "migration_169_interruption_live_test.go\n"
            )
            before, remainder = body.split(anchor, 1)
            selected, after = remainder.split("  - file: ", 1)
            for old, new in (
                ("runner: live-postgres-migrations", "runner: wrong-runner"),
                ("tag: integration", "tag: ~"),
            ):
                changed = before + anchor + selected.replace(old, new, 1) + "  - file: " + after
                self.assertNotEqual(changed, body)
                (directory / "live-tests.d" / fragment.name).write_text(
                    changed, encoding="utf-8"
                )
                self.assertNotEqual(verify_ledger(manifest, ROOT), 0)
                (directory / "live-tests.d" / fragment.name).write_text(
                    body, encoding="utf-8"
                )

    def test_omitted_inventory_row_fails(self) -> None:
        fragment = ROOT / "specs/live-tests.d/live-tests-06.yaml"
        body = fragment.read_text(encoding="utf-8")
        anchor = "  - file: go/internal/storage/postgres/migration_169_interruption_live_test.go\n"
        before, remainder = body.split(anchor, 1)
        _, after = remainder.split("  - file: ", 1)
        with tempfile.TemporaryDirectory() as temporary:
            directory = pathlib.Path(temporary)
            (directory / "live-tests.d").mkdir()
            for child in (ROOT / "specs/live-tests.d").glob("*.yaml"):
                (directory / "live-tests.d" / child.name).write_bytes(child.read_bytes())
            (directory / "live-tests.d" / fragment.name).write_text(
                before + "  - file: " + after, encoding="utf-8"
            )
            manifest = directory / "live-tests.v1.yaml"
            manifest.write_bytes((ROOT / "specs/live-tests.v1.yaml").read_bytes())
            self.assertNotEqual(verify_ledger(manifest, ROOT), 0)

    def test_missing_and_skipped_pass_events_fail(self) -> None:
        names = [name for tests in EXPECTED.values() for name in tests]
        for omitted in (None, names[0]):
            for terminal in ("pass", "skip"):
                events = []
                for name in names:
                    if name == omitted:
                        continue
                    events.extend(
                        ({"Action": "run", "Test": name},
                         {"Action": terminal if name == names[0] else "pass", "Test": name})
                    )
                events.append({"Action": "pass", "Package": MODULE_PACKAGE})
                with tempfile.TemporaryDirectory() as temporary:
                    path = pathlib.Path(temporary) / "events.jsonl"
                    path.write_text(
                        "".join(json.dumps(event) + "\n" for event in events),
                        encoding="utf-8",
                    )
                    expected = 0 if omitted is None and terminal == "pass" else 1
                    self.assertEqual(verify_events(path), expected)

    def test_skipped_child_fails(self) -> None:
        names = [name for tests in EXPECTED.values() for name in tests]
        events = []
        for name in names:
            events.extend((
                {"Action": "run", "Test": name},
                {"Action": "pass", "Test": name},
            ))
        events.extend((
            {"Action": "skip", "Test": names[0] + "/nested"},
            {"Action": "pass", "Package": MODULE_PACKAGE},
        ))
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / "events.jsonl"
            path.write_text(
                "".join(json.dumps(event) + "\n" for event in events),
                encoding="utf-8",
            )
            self.assertEqual(verify_events(path), 1)


if __name__ == "__main__":
    unittest.main()
