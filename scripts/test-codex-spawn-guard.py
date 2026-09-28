#!/usr/bin/env python3
"""Test the Codex spawn guard through its configured PreToolUse command."""

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
GUARD = ROOT / "scripts/guard-codex-spawn.py"
CANONICAL_TOOL = "collaborationspawn_agent"


class CodexSpawnGuardTests(unittest.TestCase):
    """Exercise the hook wiring and the payload observed on Codex 0.158."""

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.workspace = Path(self.temporary.name)
        subprocess.run(["git", "init", "-q", str(self.workspace)], check=True)
        (self.workspace / "scripts").mkdir()
        (self.workspace / ".agents").mkdir()
        shutil.copyfile(GUARD, self.workspace / "scripts/guard-codex-spawn.py")
        shutil.copyfile(ROOT / ".agents/roles.json", self.workspace / ".agents/roles.json")
        hooks = json.loads((ROOT / ".codex/hooks.json").read_text())["hooks"]
        matching = [row for row in hooks["PreToolUse"]
                    if row.get("matcher") == "^collaborationspawn_agent$"]
        self.assertEqual(1, len(matching))
        self.command = matching[0]["hooks"][0]["command"]

    def invoke(self, tool_input: object, *, tool_name: str = CANONICAL_TOOL,
               cwd: str | None = None) -> dict[str, object] | None:
        payload = {
            "hook_event_name": "PreToolUse", "tool_name": tool_name,
            "tool_input": tool_input, "cwd": cwd or str(self.workspace),
            "model": "gpt-6-luna", "session_id": "probe-session",
        }
        result = subprocess.run(
            ["/bin/sh", "-c", self.command], input=json.dumps(payload),
            capture_output=True, text=True, cwd=self.workspace,
            env={**os.environ, "CODEX_PROJECT_DIR": str(self.workspace)}, check=True,
        )
        return json.loads(result.stdout)["hookSpecificOutput"] if result.stdout else None

    def test_known_inherited_role_passes_without_rewrite(self) -> None:
        self.assertIsNone(self.invoke({"agent_type": "debug-eshu-deep",
                                       "message": "Diagnose", "fork_turns": "all"}))

    def test_generic_unknown_and_missing_roles_are_denied(self) -> None:
        for role in ("default", "worker", "mystery", None):
            with self.subTest(role=role):
                decision = self.invoke({"agent_type": role, "message": "Work"})
                self.assertEqual("deny", decision["permissionDecision"])
                self.assertIn("scan-eshu", decision["permissionDecisionReason"])
                self.assertIn("develop-eshu", decision["permissionDecisionReason"])

    def test_explicit_model_and_effort_overrides_pass_unchanged(self) -> None:
        for override in ({"model": "gpt-6-luna"}, {"reasoning_effort": "low"},
                         {"model_reasoning_effort": "low"}):
            with self.subTest(override=override):
                self.assertIsNone(self.invoke({"agent_type": "debug-eshu",
                                               "message": "Diagnose", **override}))

    def test_sibling_project_environment_does_not_select_other_worktree(self) -> None:
        with tempfile.TemporaryDirectory() as sibling:
            payload = {
                "hook_event_name": "PreToolUse", "tool_name": CANONICAL_TOOL,
                "tool_input": {"agent_type": "default", "message": "Work"},
                "cwd": str(ROOT), "model": "gpt-6-luna",
            }
            result = subprocess.run(
                ["/bin/sh", "-c", self.command], input=json.dumps(payload),
                capture_output=True, text=True, cwd=ROOT,
                env={**os.environ, "CODEX_PROJECT_DIR": sibling}, check=True,
            )
            decision = json.loads(result.stdout)["hookSpecificOutput"]
            self.assertEqual("deny", decision["permissionDecision"])

    def test_missing_git_root_blocks_despite_project_environment(self) -> None:
        with tempfile.TemporaryDirectory() as elsewhere:
            result = subprocess.run(
                ["/bin/sh", "-c", self.command], input=json.dumps({
                    "hook_event_name": "PreToolUse", "tool_name": CANONICAL_TOOL,
                    "tool_input": {"agent_type": "default"}, "cwd": elsewhere,
                }),
                capture_output=True, text=True, cwd=elsewhere,
                env={**os.environ, "CODEX_PROJECT_DIR": str(ROOT)},
            )
            self.assertEqual(2, result.returncode)
            self.assertIn("worktree", result.stderr)

    def test_malformed_input_and_missing_manifest_fail_closed(self) -> None:
        decision = self.invoke(["not", "an", "object"])
        self.assertEqual("deny", decision["permissionDecision"])
        malformed = subprocess.run(
            ["/bin/sh", "-c", self.command], input="{not-json",
            capture_output=True, text=True, cwd=self.workspace,
            env={**os.environ, "CODEX_PROJECT_DIR": str(self.workspace)}, check=True,
        )
        self.assertEqual(
            "deny", json.loads(malformed.stdout)["hookSpecificOutput"]["permissionDecision"]
        )
        (self.workspace / ".agents/roles.json").unlink()
        decision = self.invoke({"agent_type": "scan-eshu", "message": "Scan"})
        self.assertEqual("deny", decision["permissionDecision"])

    def test_non_spawn_and_other_workspace_are_quiet(self) -> None:
        self.assertIsNone(self.invoke({"command": "true"}, tool_name="Bash"))
        with tempfile.TemporaryDirectory() as elsewhere:
            self.assertIsNone(self.invoke({"agent_type": "default"}, cwd=elsewhere))


if __name__ == "__main__":
    unittest.main()
