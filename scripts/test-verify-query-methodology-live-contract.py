#!/usr/bin/env python3
"""Seed failures in the dedicated methodology live-test contract."""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts/lib/verify-query-methodology-live-contract.py"
MCP_ROOT = "TestImportDependencyMethodologyMCPTerminalCapLive"
MCP_REASON = (
    "reason: isolated Neo4j MCP terminal-cap root; required PASS event "
    "in blocking query-plan-regression"
)
PG_REASON = (
    "reason: isolated PostgreSQL emitted-statement root; required PASS "
    "event in blocking query-plan-regression"
)
FILES = (
    "specs/live-tests.v1.yaml",
    ".pre-commit-config.yaml",
    "specs/ci-gates.v1.yaml",
    "scripts/verify-query-methodology.sh",
    "scripts/verify-query-plan-regression.sh",
    ".github/workflows/test.yml",
    "go/internal/mcp/methodology_live_test.go",
    *(f"go/internal/query/methodology_graph_{part}_live_test.go" for part in (
        "artifact", "cases", "cycle_cap", "fixture", "measure", "oracle", "profile"
    )),
    "go/internal/query/methodology_graph_live_test.go",
    "go/internal/query/methodology_postgres_live_test.go",
)


class ContractTest(unittest.TestCase):
    """Exercise source, runner, route, and actual Go JSON event bindings."""

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for name in FILES:
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)

    def check(self, *args: str) -> subprocess.CompletedProcess[str]:
        """Run the production validator on a disposable source tree."""
        return subprocess.run(
            ["python3", str(CHECKER), "--root", str(self.root), *args],
            capture_output=True,
            text=True,
            check=False,
        )

    def mutate(self, name: str, old: str, new: str) -> None:
        """Replace one unique contract fragment in a fixture file."""
        path = self.root / name
        source = path.read_text()
        self.assertEqual(source.count(old), 1, f"ambiguous fixture anchor: {old}")
        path.write_text(source.replace(old, new, 1))

    def hook_file_pattern(self) -> str:
        """Read the actual query-methodology pre-commit selection pattern."""
        config = (self.root / ".pre-commit-config.yaml").read_text()
        match = re.search(
            r"^      - id: query-methodology-static\n"
            r"(?:(?!^      - id:).)*?^        files: '([^']+)'",
            config,
            re.M | re.S,
        )
        self.assertIsNotNone(match, "query-methodology-static hook missing")
        return match.group(1)

    def test_ci_installs_executable_hook_runner(self) -> None:
        """Hosted verify-contracts must install the test's CLI dependency."""
        workflow = (self.root / ".github/workflows/test.yml").read_text()
        job = workflow.split("  verify-contracts:\n", 1)[1].split(
            "\n  go-core:", 1
        )[0]
        self.assertIn("uses: actions/setup-python@v6", job)
        self.assertIn("python -m pip install pre-commit==4.6.2", job)

    def test_hook_selects_every_owned_input(self) -> None:
        """A change to any proof input must invoke the static hook."""
        pattern = self.hook_file_pattern()
        owned = (
            "go/internal/mcp/methodology_live_test.go",
            "go/internal/query/methodology_graph_cases_live_test.go",
            "go/internal/queryplan/pilot_contract.go",
            "go/internal/graph/schema_neo4j.go",
            "go/internal/storage/postgres/migrations/070_cloud_resource_owner_page_index.sql",
            "specs/live-tests.v1.yaml",
            ".pre-commit-config.yaml",
            ".github/workflows/test.yml",
            "scripts/lib/verify-live-tests-ledger.py",
            "scripts/lib/verify-query-methodology-live-contract.py",
            "scripts/test-verify-query-methodology-live-contract.py",
            "scripts/verify-query-methodology.sh",
            "scripts/test-verify-query-methodology.sh",
            "scripts/lib/go-test-run-guard.sh",
            "scripts/lib/live-gate-lock.sh",
            "scripts/lib/test-verify-query-methodology-go.sh",
            "scripts/lib/test-verify-query-methodology-docker.sh",
        )
        for name in owned:
            with self.subTest(path=name):
                self.assertIsNotNone(re.fullmatch(pattern, name), name)

    def test_actual_pre_commit_route(self) -> None:
        """Run the real static entry through file-only pre-commit selection."""
        pattern = self.hook_file_pattern()
        config = self.root / ".pre-commit-config.yaml"
        config.write_text(
            "repos:\n  - repo: local\n    hooks:\n"
            "      - id: query-methodology-static\n"
            "        name: query methodology production contracts\n"
            "        entry: scripts/verify-query-methodology.sh --static\n"
            "        language: script\n"
            f"        files: '{pattern}'\n"
            "        pass_filenames: false\n"
        )
        for name in (
            "scripts/lib/verify-query-methodology-live-contract.py",
            "scripts/lib/verify-live-tests-ledger.py",
            "scripts/test-verify-query-methodology-live-contract.py",
            "scripts/lib/go-test-run-guard.sh",
        ):
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)
        fake_go = self.root / "bin/go"
        fake_go.parent.mkdir()
        shutil.copyfile(
            ROOT / "scripts/lib/test-verify-query-methodology-go.sh", fake_go
        )
        fake_go.chmod(0o755)
        (self.root / "scripts/verify-query-methodology.sh").chmod(0o755)
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        subprocess.run(["git", "add", "."], cwd=self.root, check=True)
        environment = os.environ.copy()
        environment["PATH"] = f"{fake_go.parent}:{environment['PATH']}"
        environment["SHIM_LOG"] = str(self.root / "go-shim.log")

        def run_hook(path: str) -> subprocess.CompletedProcess[str]:
            """Run the actual pre-commit selector on one changed path."""
            return subprocess.run(
                [
                    "pre-commit", "run", "--config", str(config),
                    "query-methodology-static", "--files", path,
                ],
                cwd=self.root,
                env=environment,
                capture_output=True,
                text=True,
                check=False,
            )

        healthy_config = config.read_text()
        planted_config = healthy_config.replace(
            r"|go/internal/mcp/methodology_live_test\.go", "", 1
        )
        self.assertNotEqual(planted_config, healthy_config)
        config.write_text(planted_config)
        missed = run_hook("go/internal/mcp/methodology_live_test.go")
        self.assertIn("Skipped", missed.stdout)
        config.write_text(healthy_config)

        for name in (
            "go/internal/mcp/methodology_live_test.go",
            "specs/live-tests.v1.yaml",
            ".pre-commit-config.yaml",
            ".github/workflows/test.yml",
            "scripts/lib/verify-live-tests-ledger.py",
            "scripts/lib/verify-query-methodology-live-contract.py",
            "scripts/test-verify-query-methodology-live-contract.py",
        ):
            with self.subTest(path=name):
                result = run_hook(name)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("Passed", result.stdout)
                self.assertNotIn("Skipped", result.stdout)

        source = self.root / "go/internal/mcp/methodology_live_test.go"
        source.write_text(
            source.read_text().replace(
                "//go:build queryplan_profile_live", "//go:build integration", 1
            )
        )
        result = run_hook("go/internal/mcp/methodology_live_test.go")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("actual build tag differs", result.stdout + result.stderr)

    def test_clean_contract(self) -> None:
        self.assertEqual(self.check().returncode, 0)

    def test_missing_or_unknown_runner(self) -> None:
        self.mutate(
            "specs/live-tests.v1.yaml",
            MCP_REASON + "\n    runner: query-methodology",
            MCP_REASON + "\n    runner: phantom",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_wrong_tag(self) -> None:
        self.mutate(
            "go/internal/mcp/methodology_live_test.go",
            "//go:build queryplan_profile_live",
            "//go:build integration",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_extra_root_in_helper(self) -> None:
        path = self.root / "go/internal/query/methodology_graph_cases_live_test.go"
        path.write_text(path.read_text() + "\nfunc TestUnselected(t *testing.T) {}\n")
        self.assertNotEqual(self.check().returncode, 0)

    def test_deleted_required_helper(self) -> None:
        (self.root / "go/internal/query/methodology_graph_cases_live_test.go").unlink()
        self.assertNotEqual(self.check().returncode, 0)

    def test_removed_runner_invocation(self) -> None:
        self.mutate(
            "scripts/verify-query-methodology.sh",
            "methodology_live_test_run TestImportDependencyMethodologyMCPTerminalCapLive",
            "# methodology live invocation removed",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_removed_blocking_workflow_call(self) -> None:
        self.mutate(
            ".github/workflows/test.yml",
            "scripts/verify-query-plan-regression.sh",
            "true # planted workflow skip",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_missing_runner(self) -> None:
        self.mutate(
            "specs/live-tests.v1.yaml",
            MCP_REASON + "\n    runner: query-methodology",
            MCP_REASON,
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_wrong_package(self) -> None:
        self.mutate("go/internal/mcp/methodology_live_test.go", "package mcp", "package query")
        self.assertNotEqual(self.check().returncode, 0)

    def test_wrong_backend(self) -> None:
        self.mutate(
            "specs/live-tests.v1.yaml",
            MCP_REASON + "\n    runner: query-methodology\n    backends: neo4j",
            MCP_REASON + "\n    runner: query-methodology\n    backends: both",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_omitted_root(self) -> None:
        self.mutate(
            "go/internal/mcp/methodology_live_test.go",
            "func TestImportDependencyMethodologyMCPTerminalCapLive(t *testing.T)",
            "func TestRenamedMethodology(t *testing.T)",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_changed_go_invocation(self) -> None:
        self.mutate(
            "scripts/verify-query-methodology.sh",
            'go test -json -tags "$tag" -run "^${root_name}$"',
            'go test -v -tags "$tag" -run "^${root_name}$"',
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_removed_gate_selection(self) -> None:
        path = self.root / "specs/ci-gates.v1.yaml"
        source = path.read_text()
        start = source.index("  - id: query-plan-regression\n")
        before, body = source[:start], source[start:]
        anchor = '      - "go/internal/mcp/methodology_live_test.go"'
        self.assertIn(anchor, body)
        replacement = '      - "go/internal/mcp/other_live_test.go"'
        path.write_text(before + body.replace(anchor, replacement, 1))
        self.assertNotEqual(self.check().returncode, 0)

    def test_missing_isolation_setup(self) -> None:
        self.mutate(
            "scripts/verify-query-methodology.sh",
            'export ESHU_QUERY_METHODOLOGY_LIVE=1 ESHU_QUERYPLAN_PROFILE_ISOLATED=1',
            'export ESHU_QUERY_METHODOLOGY_LIVE=1',
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_real_go_json_events(self) -> None:
        """Check Go's actual run, zero-match, skip, and fail event shapes."""
        module = self.root / "event-proof"
        module.mkdir()
        (module / "go.mod").write_text("module example.com/methodology-event-proof\n\ngo 1.26.0\n")
        (module / "proof_test.go").write_text(
            'package proof\nimport ("os"; "testing")\n'
            'func TestImportDependencyMethodologyMCPTerminalCapLive(t *testing.T) { '
            'switch os.Getenv("PROOF_MODE") { case "skip": t.Skip("planted"); '
            'case "fail": t.Fatal("planted") } }\n'
        )
        events = self.root / "real-events.jsonl"
        for mode, selected, expected in (
            ("pass", MCP_ROOT, True),
            ("zero", "TestNoSuchRoot", False),
            ("skip", MCP_ROOT, False),
            ("fail", MCP_ROOT, False),
        ):
            with self.subTest(mode=mode):
                environment = os.environ.copy()
                environment["PROOF_MODE"] = mode
                result = subprocess.run(
                    ["go", "test", "-json", "-run", f"^{selected}$", ".", "-count=1"],
                    cwd=module, env=environment, capture_output=True, text=True, check=False,
                )
                self.assertEqual(result.returncode == 0, mode != "fail", result.stderr)
                events.write_text(result.stdout)
                checked = self.check("--events", str(events), "--root-name", MCP_ROOT)
                self.assertEqual(checked.returncode == 0, expected, checked.stderr)

    def test_removed_pass_event_guard(self) -> None:
        self.mutate(
            "scripts/verify-query-methodology.sh",
            (
                'python3 "$repo_root/scripts/lib/'
                'verify-query-methodology-live-contract.py" --events "$events" '
                '--root-name "$root_name"'
            ),
            'true # planted event bypass',
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_removed_workflow_self_test(self) -> None:
        self.mutate(
            ".github/workflows/test.yml",
            "python3 scripts/test-verify-query-methodology-live-contract.py",
            "true # planted self-test bypass",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_postgres_cannot_claim_graph_backend(self) -> None:
        self.mutate(
            "specs/live-tests.v1.yaml",
            PG_REASON + "\n    runner: query-methodology",
            PG_REASON + "\n    runner: query-methodology\n    backends: neo4j",
        )
        self.assertNotEqual(self.check().returncode, 0)

    def test_go_json_events(self) -> None:
        root_name = "TestImportDependencyMethodologyMCPTerminalCapLive"
        events = self.root / "events.jsonl"
        for name, lines, passes in (
            (
                "pass",
                [
                    {"Action": "run", "Test": root_name},
                    {"Action": "pass", "Test": root_name},
                ],
                True,
            ),
            ("zero", [], False),
            ("skip", [{"Action": "skip", "Test": root_name}], False),
            ("fail", [{"Action": "fail", "Test": root_name}], False),
        ):
            with self.subTest(name=name):
                events.write_text("".join(json.dumps(line) + "\n" for line in lines))
                result = self.check("--events", str(events), "--root-name", root_name)
                self.assertEqual(result.returncode == 0, passes, result.stderr)


if __name__ == "__main__":
    unittest.main()
