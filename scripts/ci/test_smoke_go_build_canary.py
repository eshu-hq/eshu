#!/usr/bin/env python3
"""Exercise the bounded failure canary through the selected Go build step."""

from __future__ import annotations

import json
import os
import pathlib
import re
import subprocess
import tempfile
import unittest
from datetime import datetime, timedelta, timezone

ROOT = pathlib.Path(__file__).resolve().parents[2]
HELPER = ROOT / "scripts" / "ci" / "smoke_go_build_canary.py"
ACTION = ROOT / ".github" / "actions" / "ci-go-core" / "action.yml"
HEAD = "a" * 40


class SmokeGoBuildCanaryTest(unittest.TestCase):
    """Prove exact targeting and preservation of the real build result."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.event = self.root / "event.json"
        self.event.write_text(
            json.dumps({
                "repository": {"full_name": "eshu-hq/eshu"},
                "number": 42,
                "pull_request": {"number": 42, "head": {"sha": HEAD}},
            }),
            encoding="utf-8",
        )
        self.config = {
            "repository": "eshu-hq/eshu",
            "pr": 42,
            "head_sha": HEAD,
            "attempt": 3,
            "mode": "smoke",
            "gate": "go-build",
            "expires_at": (datetime.now(timezone.utc) + timedelta(minutes=10)).strftime(
                "%Y-%m-%dT%H:%M:%SZ"
            ),
        }
        self.env = os.environ.copy()
        self.env.update({
            "GITHUB_EVENT_NAME": "pull_request",
            "GITHUB_EVENT_PATH": str(self.event),
            "GITHUB_RUN_ATTEMPT": "3",
            "CI_PLAN_MODE": "smoke",
            "CI_PLAN_GATES": '["go-build"]',
            "CI_SMOKE_GO_BUILD_FAIL_CANARY": json.dumps(self.config),
        })

    def run_helper(self, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
        """Run the production helper with a native-event fixture."""
        return subprocess.run(
            ["python3", str(HELPER)],
            env=env or self.env,
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_exact_target_fails_and_other_runs_are_inert(self) -> None:
        """Only the live matching smoke build receives the fixed failure."""
        matched = self.run_helper()
        self.assertEqual(matched.returncode, 1, matched.stderr)
        self.assertIn("deliberate smoke go-build failure", matched.stdout)

        for key, value in (
            ("CI_SMOKE_GO_BUILD_FAIL_CANARY", ""),
            ("CI_PLAN_MODE", "full"),
            ("CI_PLAN_MODE", "reuse"),
            ("CI_PLAN_GATES", '["go-vet"]'),
            ("GITHUB_EVENT_NAME", "push"),
            ("GITHUB_RUN_ATTEMPT", "4"),
        ):
            with self.subTest(key=key, value=value):
                env = self.env | {key: value}
                self.assertEqual(self.run_helper(env).returncode, 0)

    def test_mismatch_expiry_and_malformed_input_are_inert(self) -> None:
        """A stale or malformed repo variable cannot affect another run."""
        invalid = [
            self.config | {"repository": "other/repo"},
            self.config | {"pr": 43},
            self.config | {"head_sha": "b" * 40},
            self.config | {"head_sha": "short"},
            self.config | {"attempt": 4},
            self.config | {"mode": "full"},
            self.config | {"gate": "go-vet"},
            self.config | {"expires_at": "2000-01-01T00:00:00Z"},
            self.config | {"expires_at": "not-utc"},
            self.config | {"command": "touch " + str(self.root / "unexpected")},
        ]
        for config in invalid:
            with self.subTest(config=config):
                env = self.env | {"CI_SMOKE_GO_BUILD_FAIL_CANARY": json.dumps(config)}
                self.assertEqual(self.run_helper(env).returncode, 0)
        self.assertFalse((self.root / "unexpected").exists())
        for value in ("{", "[]", "null"):
            with self.subTest(value=value):
                env = self.env | {"CI_SMOKE_GO_BUILD_FAIL_CANARY": value}
                self.assertEqual(self.run_helper(env).returncode, 0)
        self.event.write_text("{", encoding="utf-8")
        self.assertEqual(self.run_helper().returncode, 0)

    def test_unexpected_event_read_error_fails_the_step(self) -> None:
        """An unexpected helper error cannot be converted into a green build."""
        env = self.env | {"GITHUB_EVENT_PATH": str(self.root / "missing-event.json")}
        self.assertNotEqual(self.run_helper(env).returncode, 0)

    def test_action_runs_real_build_before_the_canary(self) -> None:
        """A failing build retains its exit code; a green build reaches the canary."""
        source = ACTION.read_text(encoding="utf-8")
        self.assertEqual(source.count("    - name: Build all Go binaries\n"), 1)
        match = re.search(
            r"^    - name: Build all Go binaries\n(.*?)(?=^    - name:|\Z)",
            source,
            re.MULTILINE | re.DOTALL,
        )
        self.assertIsNotNone(match)
        block = match.group(1)
        self.assertIn("contains(fromJSON(inputs.gates), 'go-build')", block)
        self.assertNotIn("continue-on-error: true", block)
        self.assertIn(
            "CI_SMOKE_GO_BUILD_FAIL_CANARY: ${{ vars.CI_SMOKE_GO_BUILD_FAIL_CANARY }}",
            block,
        )
        self.assertIn("CI_PLAN_MODE: ${{ inputs.mode }}", block)
        self.assertIn("CI_PLAN_GATES: ${{ inputs.gates }}", block)
        self.assertIn("      run: |\n", block)
        script_lines = block.split("      run: |\n", 1)[1].splitlines()
        self.assertTrue(all(line.startswith("        ") for line in script_lines))
        script = "\n".join(line[8:] for line in script_lines)

        executable = self.root / "go"
        executable.write_text(
            '#!/bin/sh\nprintf "%s\\n" "$*" > "$CI_GO_MOCK_MARKER"\nexit "$CI_GO_MOCK_EXIT"\n',
            encoding="utf-8",
        )
        executable.chmod(0o755)
        marker = self.root / "go-invocation"
        for build_exit, expected in ((19, 19), (0, 1)):
            with self.subTest(build_exit=build_exit):
                env = self.env | {
                    "PATH": str(self.root) + os.pathsep + self.env["PATH"],
                    "CI_GO_MOCK_MARKER": str(marker),
                    "CI_GO_MOCK_EXIT": str(build_exit),
                }
                result = subprocess.run(
                    ["bash", "-e", "-o", "pipefail", "-c", script],
                    env=env,
                    cwd=ROOT / "go",
                    capture_output=True,
                    text=True,
                    check=False,
                )
                self.assertEqual(result.returncode, expected, result.stderr)
                self.assertEqual(marker.read_text(encoding="utf-8").strip(), "build ./...")


if __name__ == "__main__":
    unittest.main()
