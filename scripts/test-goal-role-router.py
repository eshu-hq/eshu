#!/usr/bin/env python3
"""Exercise skill-named goal routing at each harness prompt boundary."""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ROUTER = ROOT / "scripts/goal-role-router.py"


def context(prompt, harness, cwd=None):
    result = subprocess.run(
        ["python3", str(ROUTER), harness],
        input=json.dumps({"prompt": prompt, "cwd": str(cwd or ROOT), "hook_event_name": "UserPromptSubmit"}),
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
            output = context(f"/goal {goal}", "claude", tmp)
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

    def test_implementation_skill_phrasing(self):
        for prompt in ("/goal Apply golang-engineering", "/goal Tighten SDK schema using eshu-contract-rigor"):
            with self.subTest(prompt=prompt):
                self.assertIn("develop-eshu", context(prompt, "codex"))

    def test_diagnosis_writeup_does_not_suggest_developer(self):
        output = context("/goal Write up findings with eshu-diagnostic-rigor", "codex")
        self.assertIn("debug-eshu", output)
        self.assertNotIn("develop-eshu", output)

    def test_capitalized_skill_name_routes(self):
        output = context("/goal Diagnose with Eshu-Diagnostic-Rigor", "codex")
        self.assertIn("debug-eshu", output)

    def test_empty_skill_catalog_is_quiet(self):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            (work / "scripts").mkdir()
            (work / ".agents/skills").mkdir(parents=True)
            shutil.copyfile(ROOT / ".agents/roles.json", work / ".agents/roles.json")
            copy = work / "scripts/goal-role-router.py"
            shutil.copyfile(ROUTER, copy)
            result = subprocess.run(
                ["python3", str(copy), "codex"],
                input=json.dumps({"prompt": "/goal Diagnose with eshu-diagnostic-rigor"}),
                capture_output=True, text=True, cwd=work, check=True,
            )
            self.assertEqual("", result.stdout)

    def test_non_goal_and_control_prompts_are_quiet(self):
        for prompt in ("please use eshu-code-review", "/goal done", "/goal consent push", "/goal clear"):
            self.assertEqual("", context(prompt, "codex"))

    def test_project_hook_commands_inject_context(self):
        configs = {
            "codex": ROOT / ".codex/hooks.json",
            "claude": ROOT / ".claude/settings.json",
            "muse": ROOT / ".muse/hooks.json",
        }
        payload = json.dumps({
            "session_id": "codex-wire-probe", "turn_id": "turn-1",
            "transcript_path": None, "cwd": str(ROOT), "model": "gpt-6-luna",
            "permission_mode": "bypassPermissions",
            "prompt": "/goal Diagnose with eshu-diagnostic-rigor",
            "hook_event_name": "UserPromptSubmit",
        })
        self.assertIn("hooks = true", (ROOT / ".codex/config.toml").read_text())
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

    def test_prompt_wrappers_fail_quiet_outside_repo(self):
        configs = (ROOT / ".codex/hooks.json", ROOT / ".claude/settings.json", ROOT / ".muse/hooks.json")
        with tempfile.TemporaryDirectory() as elsewhere:
            for config in configs:
                with self.subTest(config=config):
                    hooks = json.loads(config.read_text())["hooks"]["UserPromptSubmit"][0]["hooks"]
                    command = next(hook["command"] for hook in hooks if "goal-role-router.py" in hook["command"])
                    env = {key: value for key, value in os.environ.items()
                           if key not in {"CODEX_PROJECT_DIR", "CLAUDE_PROJECT_DIR"}}
                    result = subprocess.run(
                        ["/bin/sh", "-c", command], input=json.dumps({"prompt": "/goal Diagnose with eshu-diagnostic-rigor"}),
                        capture_output=True, text=True, cwd=elsewhere, env=env,
                    )
                    self.assertEqual(0, result.returncode)
                    self.assertEqual("", result.stdout)

    def test_muse_wrapper_fails_quiet_without_router_or_python(self):
        hooks = json.loads((ROOT / ".muse/hooks.json").read_text())["hooks"]["UserPromptSubmit"][0]["hooks"]
        command = next(hook["command"] for hook in hooks if "goal-role-router.py" in hook["command"])
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            bin_dir = work / "bin"
            bin_dir.mkdir()
            fake_git = bin_dir / "git"
            fake_git.write_text(f"#!/bin/sh\nprintf '%s\\n' '{work}'\n")
            fake_git.chmod(0o755)
            fake_python = bin_dir / "python3"
            fake_python.symlink_to(sys.executable)
            payload = json.dumps({"prompt": "/goal Diagnose with eshu-diagnostic-rigor"})
            env = {**os.environ, "PATH": str(bin_dir)}
            for case in ("missing router", "missing python"):
                with self.subTest(case=case):
                    if case == "missing python":
                        fake_python.unlink()
                        (work / "scripts").mkdir()
                        (work / "scripts/goal-role-router.py").touch()
                    result = subprocess.run(
                        ["/bin/sh", "-c", command], input=payload,
                        capture_output=True, text=True, cwd=work, env=env,
                    )
                    self.assertEqual(0, result.returncode)
                    self.assertEqual("", result.stdout)
                    self.assertEqual("", result.stderr)

    def test_prepared_goal_survives_refresh(self):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            (work / ".claude").mkdir()
            prepared = work / "goal.txt"
            prepared.write_text("Drive issue with eshu-issue-driver.\nDiagnose using eshu-diagnostic-rigor.\nReview with eshu-code-review.\n")
            hook = ROOT / ".claude/hooks/goal-refresh.sh"
            def submit(prompt):
                payload = json.dumps({
                    "session_id": "router-refresh-test", "prompt_id": "p1",
                    "cwd": str(work), "prompt": prompt,
                    "hook_event_name": "UserPromptSubmit",
                })
                return subprocess.run(
                    ["bash", str(hook)], input=payload, capture_output=True,
                    text=True, cwd=ROOT, check=True,
                ).stdout
            submit("/goal goal.txt")
            stored = (work / ".claude/active-goal.router-refresh-test").read_text()
            self.assertIn("eshu-diagnostic-rigor", stored)
            self.assertIn("eshu-code-review", stored)
            refreshed = json.loads(submit("continue"))["hookSpecificOutput"]["additionalContext"]
            self.assertIn("eshu-diagnostic-rigor", refreshed)
            self.assertIn("eshu-code-review", refreshed)

    def test_malformed_manifest_does_not_block_prompt(self):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            (work / "scripts").mkdir()
            (work / ".agents/skills").mkdir(parents=True)
            (work / ".agents/roles.json").write_text("{malformed")
            copy = work / "scripts/goal-role-router.py"
            shutil.copyfile(ROUTER, copy)
            result = subprocess.run(
                ["python3", str(copy), "codex"],
                input=json.dumps({"prompt": "/goal Diagnose with eshu-diagnostic-rigor"}),
                capture_output=True, text=True, cwd=work,
            )
            self.assertEqual(0, result.returncode)
            self.assertEqual("", result.stdout)

    def test_prepared_goal_rejects_outside_and_secret_paths(self):
        with tempfile.TemporaryDirectory() as workspace, tempfile.TemporaryDirectory() as elsewhere:
            outside = Path(elsewhere) / "goal.txt"
            outside.write_text("Diagnose with eshu-diagnostic-rigor")
            secret = Path(workspace) / ".env"
            secret.write_text("Diagnose with eshu-diagnostic-rigor")
            self.assertEqual("", context(f"/goal {outside}", "codex", workspace))
            self.assertEqual("", context(f"/goal {secret}", "codex", workspace))

    def test_claude_scratchpad_goal_is_supported(self):
        with tempfile.TemporaryDirectory(prefix="claude-", dir=tempfile.gettempdir()) as session_root:
            slug = str(ROOT.resolve()).replace("/", "-")
            scratchpad = Path(session_root) / slug / "session-1" / "scratchpad"
            scratchpad.mkdir(parents=True)
            goal = scratchpad / "goal.txt"
            goal.write_text("Diagnose with eshu-diagnostic-rigor")
            self.assertIn("debug-eshu", context(f"/goal {goal}", "claude", ROOT))


if __name__ == "__main__":
    unittest.main()
