#!/usr/bin/env python3
"""Exercise skill-named goal routing at each harness prompt boundary."""

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ROUTER = ROOT / "scripts/goal-role-router.py"


def context(prompt, harness):
    result = subprocess.run(
        ["python3", str(ROUTER), harness],
        input=json.dumps({"prompt": prompt, "hook_event_name": "UserPromptSubmit"}),
        capture_output=True,
        text=True,
        check=True,
        cwd=ROOT,
    )
    return json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"] if result.stdout else ""


class GoalRoleRouterTests(unittest.TestCase):
    def test_claude_goal_file_routes_phases_and_keeps_issue_driver(self):
        with tempfile.TemporaryDirectory() as tmp:
            goal = Path(tmp) / "goal.txt"
            goal.write_text("GOAL: Drive issue #6965 with eshu-issue-driver. Diagnose with eshu-diagnostic-rigor and concurrency-deadlock-rigor, implement a fix using golang-engineering, then use eshu-code-review.")
            output = context(f"/goal {goal}", "claude")
        for marker in ("debug-eshu", "develop-eshu", "review-eshu", "sonnet", "eshu-issue-driver stays", "concurrency-deadlock-rigor is a method"):
            self.assertIn(marker, output)

    def test_muse_goal_text_routes_performance_and_review(self):
        output = context("/goal Drive epic with eshu-issue-driver. Benchmark using eshu-performance-rigor; review final PR using eshu-code-review.", "muse")
        self.assertIn("perf-eshu", output)
        self.assertIn("review-eshu", output)
        self.assertIn("muse-exec", output)
        self.assertNotIn("develop-eshu", output)

    def test_codex_debug_model_is_manifest_binding(self):
        output = context("GOAL: Diagnose the queue with eshu-diagnostic-rigor", "codex")
        self.assertIn("debug-eshu", output)
        self.assertIn("gpt-5.6-terra effort=high", output)
        self.assertIn("codex-exec", output)

    def test_deep_diagnosis_uses_deep_tier(self):
        output = context("/goal Diagnose an intermittent cross-system failure using eshu-diagnostic-rigor", "codex")
        self.assertIn("debug-eshu-deep", output)
        self.assertIn("gpt-6-sol effort=high", output)

    def test_long_inline_goal_is_not_mistaken_for_a_path(self):
        output = context("/goal " + "Drive issue with eshu-issue-driver. " * 20 + "Review using eshu-code-review.", "claude")
        self.assertIn("review-eshu", output)

    def test_non_goal_and_control_prompts_are_quiet(self):
        for prompt in ("please use eshu-code-review", "/goal done", "/goal consent push", "/goal clear"):
            self.assertEqual("", context(prompt, "codex"))

    def test_project_hook_commands_inject_context(self):
        configs = {
            "codex": ROOT / ".codex/hooks.json",
            "claude": ROOT / ".claude/settings.json",
            "muse": ROOT / ".muse/hooks.json",
        }
        payload = json.dumps({"prompt": "/goal Diagnose with eshu-diagnostic-rigor", "hook_event_name": "UserPromptSubmit"})
        for harness, config in configs.items():
            with self.subTest(harness=harness):
                hooks = json.loads(config.read_text())["hooks"]["UserPromptSubmit"][0]["hooks"]
                command = next(hook["command"] for hook in hooks if "goal-role-router.py" in hook["command"])
                result = subprocess.run(
                    ["/bin/sh", "-c", command], input=payload, capture_output=True,
                    text=True, cwd=ROOT, env={**os.environ, "CLAUDE_PROJECT_DIR": str(ROOT)},
                    check=True,
                )
                self.assertIn("debug-eshu", json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"])


if __name__ == "__main__":
    unittest.main()
