#!/usr/bin/env python3
"""Guard the child-only DSN exception for the changed-since live proof."""

import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent / "lib"))

from live_postgres_readiness_gen_scan import check_env_honesty, parse_marker  # noqa: E402
from live_postgres_readiness_gen_render import render_env_sh, workflow_value  # noqa: E402


CHILD = "ESHU_CHANGED_SINCE_LINK_CHILD_DSN"
PARENT = "ESHU_POSTGRES_TEST_DSN"
PROCESS = "go/internal/reducer/freshness/links/process_live_test.go"


class ChildEnvTest(unittest.TestCase):
    """Exercise the scanner with planted Go literals and marker declarations."""

    def setUp(self) -> None:
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = pathlib.Path(self.scratch.name)

    def check_file(self, path: str, *, required_child: bool = False,
                   optional_child: bool = False,
                   literal: str = CHILD) -> None:
        """Scan a seeded live file at the requested repository path."""
        required = f"{PARENT} {CHILD}" if required_child else PARENT
        optional = (f"//eshu:readiness:env-optional {CHILD}\n"
                    if optional_child else "")
        source = (
            "package links_test\n"
            "//eshu:readiness live-postgres-readiness\n"
            "//eshu:readiness:reason seeded child env proof\n"
            f"//eshu:readiness:env {required}\n"
            f"{optional}"
            "func TestSeededLive(t *testing.T) {}\n"
            f'const parent = "{PARENT}"\n'
            f'const child = "{literal}"\n'
        )
        file_path = self.root / path
        file_path.parent.mkdir(parents=True, exist_ok=True)
        file_path.write_text(source, encoding="utf-8")
        marker = parse_marker(path, source)
        self.assertIsNotNone(marker)
        check_env_honesty(self.root, {path: marker})

    def test_child_literal_allowed_only_in_process_file(self) -> None:
        """The actual subprocess proof may read its child-only DSN."""
        self.check_file(PROCESS)

    def test_parent_required_child_dsn_is_rejected(self) -> None:
        """A required marker cannot export the subprocess DSN."""
        with self.assertRaisesRegex(ValueError, CHILD):
            self.check_file(PROCESS, required_child=True)

    def test_parent_optional_child_dsn_is_rejected(self) -> None:
        """An optional marker cannot export the subprocess DSN."""
        with self.assertRaisesRegex(ValueError, CHILD):
            self.check_file(PROCESS, optional_child=True)

    def test_other_file_required_child_dsn_is_rejected(self) -> None:
        """No enrolled file may request the child DSN from its parent."""
        with self.assertRaisesRegex(ValueError, CHILD):
            self.check_file(PROCESS.replace("process_live", "other_live"),
                            required_child=True)

    def test_other_file_optional_child_dsn_is_rejected(self) -> None:
        """No enrolled file may request the child DSN as optional."""
        with self.assertRaisesRegex(ValueError, CHILD):
            self.check_file(PROCESS.replace("process_live", "other_live"),
                            optional_child=True)

    def test_unrelated_dsn_in_process_file_is_rejected(self) -> None:
        """An unrelated undeclared DSN still fails in the process file."""
        with self.assertRaisesRegex(ValueError, "ESHU_UNRELATED_TEST_DSN"):
            self.check_file(PROCESS, literal="ESHU_UNRELATED_TEST_DSN")

    def test_unrelated_file_child_literal_is_rejected(self) -> None:
        """The exception does not apply to another live test file."""
        with self.assertRaisesRegex(ValueError, CHILD):
            self.check_file(PROCESS.replace("process_live", "other_live"))

    def test_iac_opt_in_is_bound_to_source_and_required_runner_env(self) -> None:
        """A test-only opt-in must reach both the workflow and local runner."""
        var = "ESHU_IAC_INVENTORY_LIVE"
        path = "go/internal/query/iac/inventory_postgres_live_test.go"
        source = (
            "package iac\n"
            "//eshu:readiness live-postgres-readiness\n"
            "//eshu:readiness:reason seeded IaC opt-in proof\n"
            f"//eshu:readiness:env ESHU_POSTGRES_DSN {var}\n"
            "func TestSeededLive(t *testing.T) {}\n"
            f'const optIn = "{var}"\n'
            'const dsn = "ESHU_POSTGRES_DSN"\n'
        )
        file_path = self.root / path
        file_path.parent.mkdir(parents=True, exist_ok=True)
        file_path.write_text(source, encoding="utf-8")
        marker = parse_marker(path, source)
        self.assertIsNotNone(marker)
        check_env_honesty(self.root, {path: marker})
        self.assertIn("READINESS_OPT_IN_VARS=(\n  ESHU_IAC_INVENTORY_LIVE\n)",
                      render_env_sh(marker["env"], []))
        self.assertEqual(workflow_value(var), "'1'")


if __name__ == "__main__":
    unittest.main()
