#!/usr/bin/env python3
"""Exercise frozen methodology comparison identities against disposable Git DAGs."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


RESOLVER = Path(__file__).resolve().parent / "lib/query-methodology-comparison-identity.py"


class ComparisonIdentityTest(unittest.TestCase):
    """Check real Git ancestry and event identities, including combined heads."""

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git("init", "--quiet", "--initial-branch=main")
        self.git("config", "user.name", "Eshu Test")
        self.git("config", "user.email", "eshu-test@example.invalid")
        self.git("config", "gc.auto", "0")
        self.git("config", "maintenance.auto", "false")
        self.migrations = self.root / "go/internal/storage/postgres/migrations"
        self.migrations.mkdir(parents=True)
        (self.migrations / "001.sql").write_text("SELECT 1;\n")
        self.commit("baseline")
        self.base = self.git("rev-parse", "HEAD")
        self.git("branch", "feature")
        (self.migrations / "169.sql").write_text("SELECT 169;\n")
        self.commit("target main")
        self.target = self.git("rev-parse", "HEAD")
        self.git("update-ref", "refs/remotes/origin/main", self.target)
        self.git("checkout", "--quiet", "feature")
        (self.migrations / "207.sql").write_text("SELECT 207;\n")
        self.commit("feature head")
        self.feature = self.git("rev-parse", "HEAD")

    def git(self, *arguments: str) -> str:
        """Run Git only in this test's disposable repository."""
        command = subprocess.run(
            ["git", *arguments], cwd=self.root, capture_output=True, text=True, check=False
        )
        self.assertEqual(command.returncode, 0, command.stderr)
        return command.stdout.strip()

    def commit(self, message: str) -> None:
        """Commit the fixture's current source tree."""
        self.git("add", ".")
        self.git("commit", "--quiet", "-m", message)

    def resolve(
        self, event: str = "", payload: dict | None = None, ref: str = "",
        cwd: Path | None = None, github_sha: str | None = None,
    ) -> tuple[subprocess.CompletedProcess[str], Path]:
        """Invoke the real resolver and return its archived identity path."""
        output = self.root / "identity.json"
        environment = os.environ.copy()
        for key in ("GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_SHA", "GITHUB_REF"):
            environment.pop(key, None)
        if event:
            event_file = self.root / "event.json"
            event_file.write_text(json.dumps(payload or {}))
            environment.update(
                GITHUB_EVENT_NAME=event,
                GITHUB_EVENT_PATH=str(event_file),
                GITHUB_SHA=github_sha or self.git("rev-parse", "HEAD"),
                GITHUB_REF=ref,
            )
        result = subprocess.run(
            [sys.executable, str(RESOLVER), "--root", str(self.root), "--output", str(output)],
            cwd=cwd or self.root / "go/internal/storage/postgres/migrations",
            env=environment,
            capture_output=True,
            text=True,
            check=False,
        )
        return result, output

    def test_local_feature_freezes_unique_merge_base(self) -> None:
        result, path = self.resolve()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(path.read_text()),
            {"version": 1, "event": "local", "mode": "feature_merge_base", "target": self.target, "base": self.base, "candidate": self.feature},
        )
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        self.assertEqual(json.loads(path.read_text())["target"], self.target)

    def test_root_and_nested_invocation_resolve_same_identity(self) -> None:
        nested, path = self.resolve()
        self.assertEqual(nested.returncode, 0, nested.stderr)
        first = json.loads(path.read_text())
        from_root, path = self.resolve(cwd=self.root)
        self.assertEqual(from_root.returncode, 0, from_root.stderr)
        self.assertEqual(json.loads(path.read_text()), first)

    def test_pull_request_uses_event_base_and_actual_combined_checkout(self) -> None:
        self.git("merge", "--quiet", "--no-edit", "main")
        combined = self.git("rev-parse", "HEAD")
        result, path = self.resolve(
            "pull_request",
            {"pull_request": {"base": {"sha": self.target}, "head": {"sha": self.feature}}},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.target)
        self.assertEqual(json.loads(path.read_text())["candidate"], combined)
        self.assertTrue((self.migrations / "169.sql").is_file())
        self.assertTrue((self.migrations / "207.sql").is_file())

    def test_merge_group_uses_event_base_and_actual_combined_checkout(self) -> None:
        self.git("merge", "--quiet", "--no-edit", "main")
        combined = self.git("rev-parse", "HEAD")
        result, path = self.resolve(
            "merge_group", {"merge_group": {"base_sha": self.target, "head_sha": combined}}
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.target)
        self.assertEqual(json.loads(path.read_text())["candidate"], combined)

    def test_push_main_uses_event_before(self) -> None:
        self.git("merge", "--quiet", "--no-edit", "main")
        combined = self.git("rev-parse", "HEAD")
        result, path = self.resolve("push", {"before": self.target, "ref": "refs/heads/main"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.target)
        self.assertEqual(json.loads(path.read_text())["candidate"], combined)

    def test_schedule_main_uses_first_parent_before_parity(self) -> None:
        self.git("checkout", "--quiet", "main")
        result, path = self.resolve("schedule", {}, "refs/heads/main", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.base)
        self.assertEqual(json.loads(path.read_text())["target"], self.target)
        self.assertEqual(json.loads(path.read_text())["mode"], "previous_main_snapshot")

    def test_manual_main_uses_first_parent(self) -> None:
        self.git("checkout", "--quiet", "main")
        result, path = self.resolve("workflow_dispatch", {}, "refs/heads/main")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.base)
        self.assertEqual(json.loads(path.read_text())["mode"], "previous_main_snapshot")

    def test_manual_divergent_feature_uses_merge_base(self) -> None:
        result, path = self.resolve("workflow_dispatch", {}, "refs/heads/feature")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.base)
        self.assertEqual(json.loads(path.read_text())["target"], self.target)
        self.assertEqual(json.loads(path.read_text())["mode"], "feature_merge_base")

    def test_manual_integrated_feature_uses_first_parent(self) -> None:
        self.git("checkout", "--quiet", "main")
        (self.migrations / "210.sql").write_text("SELECT 210;\n")
        self.commit("main after integrated feature")
        advanced_main = self.git("rev-parse", "HEAD")
        self.git("update-ref", "refs/remotes/origin/main", advanced_main)
        self.git("checkout", "--quiet", "feature")
        self.git("merge", "--quiet", "--no-edit", "main")
        integrated = self.git("rev-parse", "HEAD")
        self.git("checkout", "--quiet", "main")
        self.git("merge", "--quiet", "feature")
        self.git("update-ref", "refs/remotes/origin/main", self.git("rev-parse", "HEAD"))
        self.git("checkout", "--quiet", "feature")
        self.assertEqual(self.git("rev-parse", "HEAD"), integrated)
        result, path = self.resolve("workflow_dispatch", {}, "refs/heads/feature")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(path.read_text())["base"], self.feature)
        self.assertEqual(json.loads(path.read_text())["mode"], "already_integrated_snapshot")

    def test_schedule_rejects_unknown_ref(self) -> None:
        result, path = self.resolve("schedule", {}, "refs/heads/feature")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(path.exists())

    def test_missing_event_base_fails_without_local_fallback(self) -> None:
        self.git("merge", "--quiet", "--no-edit", "main")
        result, path = self.resolve("pull_request", {"pull_request": {"base": {}, "head": {"sha": self.feature}}})
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(path.exists())

    def test_zero_base_and_mismatched_event_head_fail(self) -> None:
        self.git("merge", "--quiet", "--no-edit", "main")
        for payload, github_sha in (
            ({"pull_request": {"base": {"sha": "0" * 40}, "head": {"sha": self.feature}}}, None),
            ({"pull_request": {"base": {"sha": self.target}, "head": {"sha": self.feature}}}, "f" * 40),
        ):
            with self.subTest(payload=payload, github_sha=github_sha):
                result, path = self.resolve("pull_request", payload, github_sha=github_sha)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(path.exists())

    def test_shallow_history_fails_before_local_fallback(self) -> None:
        shallow = self.root / "shallow-clone"
        result = subprocess.run(
            ["git", "clone", "--quiet", "--depth=1", "--branch=feature", f"file://{self.root}", str(shallow)],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        identity = shallow / "identity.json"
        result = subprocess.run(
            [sys.executable, str(RESOLVER), "--root", str(shallow), "--output", str(identity)],
            cwd=shallow, capture_output=True, text=True, check=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("full Git history", result.stderr)
        self.assertFalse(identity.exists())

    def test_schedule_root_commit_has_no_prior_snapshot(self) -> None:
        self.git("checkout", "--quiet", "--orphan", "rootonly")
        self.git("rm", "-rf", ".")
        (self.root / "root.txt").write_text("root\n")
        self.commit("root only")
        result, path = self.resolve("schedule", {}, "refs/heads/main", cwd=self.root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no first parent", result.stderr)
        self.assertFalse(path.exists())

    def test_criss_cross_history_rejects_multiple_merge_bases(self) -> None:
        self.git("checkout", "--quiet", "-b", "left", self.base)
        (self.root / "left.txt").write_text("left\n")
        self.commit("left")
        left = self.git("rev-parse", "HEAD")
        self.git("checkout", "--quiet", "-b", "right", self.base)
        (self.root / "right.txt").write_text("right\n")
        self.commit("right")
        right = self.git("rev-parse", "HEAD")
        self.git("checkout", "--quiet", "left")
        self.git("merge", "--quiet", "--no-edit", right)
        self.git("checkout", "--quiet", "right")
        self.git("merge", "--quiet", "--no-edit", left)
        self.git("update-ref", "refs/remotes/origin/main", self.git("rev-parse", "HEAD"))
        self.git("checkout", "--quiet", "left")
        self.assertEqual(len(self.git("merge-base", "--all", "HEAD", "origin/main").splitlines()), 2)
        result, path = self.resolve()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("one merge base", result.stderr)
        self.assertFalse(path.exists())

    def test_unavailable_base_fails_without_local_fallback(self) -> None:
        self.git("merge", "--quiet", "--no-edit", "main")
        result, path = self.resolve(
            "pull_request",
            {"pull_request": {"base": {"sha": "a" * 40}, "head": {"sha": self.feature}}},
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(path.exists())


if __name__ == "__main__":
    unittest.main()
