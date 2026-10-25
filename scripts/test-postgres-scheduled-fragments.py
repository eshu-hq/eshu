"""Fail-closed tests for the scheduled PostgreSQL inventory fragments."""

import hashlib
import json
import os
import re
import subprocess
from pathlib import Path
import sys
import shutil
from unittest import mock
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts/lib"))
import postgres_scheduled_runner as runner  # noqa: E402


def copy_contract(root: Path) -> None:
    """Copy workflow routing sources into an inventory mutation checkout."""
    for relative in (runner.FRAGMENT, runner.BULK_ACTION):
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes((ROOT / relative).read_bytes())


class InventoryFragmentTest(unittest.TestCase):
    """The index and every listed fragment form one closed source contract."""

    def test_committed_index_and_enrolled_groups(self) -> None:
        """The reviewed inventory contains all 284 tests in 138 files."""
        index = json.loads(runner.INVENTORY.read_text(encoding="utf-8"))
        self.assertEqual(index["version"], "postgres-scheduled-inventory/v1")
        self.assertEqual(len(index["fragments"]), 19)
        inventory = runner.load_inventory()
        self.assertEqual(len(inventory), 138)
        self.assertEqual(sum(len(row["tests"]) for row in inventory.values()), 284)
        for group, files, tests in (
            ("admin", 6, 8), ("secret_lines", 9, 24),
            ("reachability", 1, 6), ("completion", 1, 1),
            ("claim", 13, 36), ("liveness", 7, 18), ("fanout", 3, 3),
            ("search", 5, 8), ("readiness", 4, 6),
        ):
            rows = [row for row in inventory.values() if row["group"] == group]
            self.assertEqual((len(rows), sum(len(row["tests"]) for row in rows)),
                             (files, tests))

    def test_containerimage_has_real_bootstrap_and_exact_source_closure(self) -> None:
        """Keep the complete identity-writer family and its bootstrap route."""
        inventory = runner.load_inventory()
        rows = {path: row for path, row in inventory.items()
                if row["group"] == "containerimage"}
        self.assertEqual(len(rows), 8)
        self.assertEqual(sum(len(row["tests"]) for row in rows.values()), 15)
        fragment = (ROOT / runner.FRAGMENT).read_text()
        self.assertIn("go/internal/reducer/containerimage/*.go", fragment)
        action = (ROOT / runner.BULK_ACTION).read_text()
        args = ("-args -containerimage-bootstrap=1 "
                "-containerimage-database=eshu_containerimage_scheduled")
        self.assertIn(args, action)
        runner.verify_inventory(ROOT, inventory)
        for old, new in (
            ("-containerimage-bootstrap=1", "-containerimage-bootstrap=0"),
            ("-containerimage-database=eshu_containerimage_scheduled",
             "-containerimage-database=eshu_existing"),
            ("CREATE DATABASE eshu_containerimage_scheduled", "CREATE DATABASE eshu_existing"),
            ("containerimage.jsonl", "missing-containerimage.jsonl"),
        ):
            with self.subTest(old=old):
                mutated = action.replace(old, new)
                self.assertNotEqual(mutated, action)
                original_read = Path.read_text
                def read(path: Path, *args: object, **kwargs: object) -> str:
                    if path == ROOT / runner.BULK_ACTION:
                        return mutated
                    return original_read(path, *args, **kwargs)
                digest = hashlib.sha256(mutated.encode()).hexdigest()
                with mock.patch.object(Path, "read_text", read), \
                     mock.patch.object(runner, "BULK_ACTION_SHA256", digest):
                    with self.assertRaises(ValueError):
                        runner.verify_inventory(ROOT, inventory)

    def test_runtime_fallback_index_uses_existing_admin_group(self) -> None:
        """Keep the new migrator proof in the existing disposable storage leg."""
        path = ("go/internal/storage/postgres/"
                "supply_chain_runtime_filter_index_restart_live_test.go")
        inventory = runner.load_inventory()
        self.assertEqual(inventory.get(path), {
            "group": "disposable_storage",
            "package": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
            "tests": ["TestSupplyChainRuntimeFallbackIndexRestartSafetyLive"],
        })
        self.assertIn("ESHU_TEST_SUPPLY_CHAIN_RUNTIME_INDEX_POSTGRES",
                      runner.ADMIN_ENVS["disposable_storage"])
        runner.verify_inventory(ROOT, inventory)

    def test_bad_fragment_index_rejected(self) -> None:
        """Missing, duplicate, and traversal fragments fail before enrollment."""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fragment_dir = root / "postgres_scheduled_inventory.d"
            fragment_dir.mkdir()
            fragment = fragment_dir / "one.json"
            fragment.write_text('{"a": {}}\n', encoding="utf-8")
            index = root / "postgres_scheduled_inventory.json"
            for fragments in ([], ["postgres_scheduled_inventory.d/missing.json"],
                              ["postgres_scheduled_inventory.d/one.json"] * 2,
                              ["../outside.json"]):
                index.write_text(json.dumps({"version": "postgres-scheduled-inventory/v1",
                                             "fragments": fragments}), encoding="utf-8")
                with self.subTest(fragments=fragments), self.assertRaises(ValueError):
                    runner.load_inventory(index)

    def test_unlisted_duplicate_and_changed_fragment(self) -> None:
        """The manifest binds the closed fragment set and each fragment byte."""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            index = root / runner.INVENTORY.name
            index.write_bytes(runner.INVENTORY.read_bytes())
            source_dir = runner.INVENTORY.parent / "postgres_scheduled_inventory.d"
            shutil.copytree(source_dir, root / source_dir.name)
            original = runner.inventory_digest(index)
            claim = root / source_dir.name / "claim.json"
            claim.write_text(claim.read_text() + "\n", encoding="utf-8")
            self.assertNotEqual(runner.inventory_digest(index), original)
            (root / source_dir.name / "unlisted.json").write_text('{"x": {}}')
            with self.assertRaisesRegex(ValueError, "differ from index"):
                runner.load_inventory(index)
            (root / source_dir.name / "unlisted.json").unlink()
            saved_claim = claim.read_text()
            claim.write_text("{}\n")
            with self.assertRaisesRegex(ValueError, "empty.*fragment"):
                runner.load_inventory(index)
            claim.write_text(saved_claim)
            rows = json.loads(claim.read_text())
            other = json.loads((root / source_dir.name / "liveness.json").read_text())
            source, row = next(iter(other.items()))
            rows[source] = row
            claim.write_text(json.dumps(rows), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "duplicate.*source"):
                runner.load_inventory(index)

    def test_new_group_workflow_failures(self) -> None:
        """Missing source, selector, DB, opt-in, or stream checks fail."""
        inventory = runner.load_inventory()
        original = (ROOT / runner.WORKFLOW).read_text(encoding="utf-8")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            copy_contract(root)
            for source in inventory:
                target = root / source
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((ROOT / source).read_bytes())
            workflow = root / runner.WORKFLOW
            workflow.parent.mkdir(parents=True)
            source = next(path for path, row in inventory.items()
                          if row["group"] == "claim")
            changed = root / source
            changed.write_text(changed.read_text().replace("func Test", "func Gone", 1))
            workflow.write_text(original)
            with self.assertRaisesRegex(ValueError, "source Test inventory"):
                runner.verify_inventory(root, inventory)
            changed.write_bytes((ROOT / source).read_bytes())
            for old, new, message in (
                ("CREATE DATABASE eshu_liveness_scheduled",
                 "CREATE DATABASE eshu_claim_scheduled", "fresh database"),
                ("-run '^(TestFailBounds", "-run '^(TestMissing|TestFailBounds",
                 "selector"),
                ('ESHU_SEARCH_INDEX_PARTITION_LIVE: "1"',
                 'ESHU_SEARCH_INDEX_PARTITION_LIVE: "0"', "search partition"),
                ('${{ runner.temp }}/proof-bundle/readiness.jsonl\n', "", "artifact"),
                ("--group fanout", "--group claim", "event verifier"),
                ("    - name: Run claim proofs\n",
                 "    - name: Run claim proofs\n      if: false\n", "non-fail-closed"),
                ("    - name: Run claim proofs\n",
                 "    - name: Run claim proofs\n      continue-on-error: true\n",
                 "non-fail-closed"),
                ("          python3 scripts/test-postgres-scheduled-fragments.py\n",
                 "          # python3 scripts/test-postgres-scheduled-fragments.py\n",
                 "harness"),
                ("  - " + source + "\n", "", "PR paths"),
                ("  - go/internal/storage/postgres/"
                 "reducer_queue_domain_fairness_test.go\n", "", "PR paths"),
                ('ESHU_CHANGED_SINCE_LINK_SCALE_PROOF: "p3"',
                 'ESHU_CHANGED_SINCE_LINK_SCALE_PROOF: "p8b"', "freshness"),
                ('ESHU_7127_W11_COST: "1"',
                 'ESHU_7127_W11_COST: "0"', "freshness"),
                ("ESHU_POSTGRES_TEST_DSN: postgres://eshu:change-me@localhost:15432/"
                 "eshu_freshness_a?sslmode=disable\n"
                 '          ESHU_7127_W11_COST: "1"',
                 "ESHU_POSTGRES_TEST_DSN: postgres://eshu:change-me@localhost:15432/"
                 "eshu_freshness_a?sslmode=disable",
                 "freshness"),
                ("-timeout=30m ./internal/storage/postgres/freshness/links",
                 "-timeout=6m ./internal/storage/postgres/freshness/links",
                 "selector"),
                ("ESHU_RETENTION_TIMING_MODE=build go test",
                 "ESHU_RETENTION_TIMING_MODE=run go test", "freshness"),
                ("< ../docs/internal/evidence/7127-link-writer-fixture.sql",
                 "< /tmp/7127-link-writer-fixture-missing.sql", "freshness"),
            ):
                action = root / runner.BULK_ACTION
                fragment = root / runner.FRAGMENT
                subject = (workflow if old in original else
                           action if old in action.read_text() else fragment)
                source = subject.read_text()
                self.assertIn(old, source)
                changed_text = source.replace(old, new, 1)
                subject.write_text(changed_text)
                digest = hashlib.sha256(action.read_bytes()).hexdigest()
                with self.subTest(message=message), mock.patch.object(
                        runner, "BULK_ACTION_SHA256", digest), self.assertRaisesRegex(ValueError, message):
                    runner.verify_inventory(root, inventory)
                subject.write_text(source)

    def test_each_disposable_step_has_its_own_admin_env(self) -> None:
        inventory = runner.load_inventory()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            copy_contract(root)
            for target in inventory:
                copy = root / target
                copy.parent.mkdir(parents=True, exist_ok=True)
                copy.write_text((ROOT / target).read_text())
            workflow = root / runner.WORKFLOW
            workflow.parent.mkdir(parents=True)
            workflow.write_text((ROOT / runner.WORKFLOW).read_text())
            action = root / runner.BULK_ACTION
            original = action.read_text()
            removed = (
                "        ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN: "
                "postgres://eshu:change-me@localhost:15432/"
                "postgres?sslmode=disable\n"
            )
            self.assertEqual(original.count(removed), 3)
            action.write_text(original.replace(removed, "", 1))
            with mock.patch.object(
                    runner, "BULK_ACTION_SHA256",
                    hashlib.sha256(action.read_bytes()).hexdigest()):
                with self.assertRaisesRegex(ValueError, "missing admin DSN"):
                    runner.verify_inventory(root, inventory)

    def test_admin_replay_fence_opt_in_is_required(self) -> None:
        """The admin replay proof must run with its live fixture flag."""
        inventory = runner.load_inventory()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            copy_contract(root)
            for target in inventory:
                copy = root / target
                copy.parent.mkdir(parents=True, exist_ok=True)
                copy.write_text((ROOT / target).read_text())
            workflow = root / runner.WORKFLOW
            workflow.parent.mkdir(parents=True)
            workflow.write_text((ROOT / runner.WORKFLOW).read_text())
            action = root / runner.BULK_ACTION
            original = action.read_text()
            flag = '        ESHU_ADMIN_REPLAY_FENCE_LIVE: "1"\n'
            self.assertEqual(original.count(flag), 1)
            action.write_text(original.replace(flag, "", 1))
            with mock.patch.object(
                    runner, "BULK_ACTION_SHA256",
                    hashlib.sha256(action.read_bytes()).hexdigest()):
                with self.assertRaisesRegex(ValueError, "admin replay fence opt-in missing"):
                    runner.verify_inventory(root, inventory)

    @mock.patch.dict("os.environ", {"GITHUB_ACTIONS": "false"})
    def test_missing_new_stream_and_local_as_hosted_fail(self) -> None:
        """The complete stream bundle requires every group and hosted origin."""
        inventory = runner.load_inventory()
        manifest = runner.build_manifest(
            inventory, "pull_request", "123", "1",
            "https://github.com/eshu-hq/eshu/actions/runs/123/attempts/1",
            "a" * 40, "b" * 40, "PostgreSQL 18.6 local fixture",
            local_simulation=True,
        )
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "manifest.json").write_text(json.dumps(manifest))
            for group, package in runner.PACKAGES.items():
                tests = sorted(test for row in inventory.values()
                               if row["group"] == group for test in row["tests"])
                events = [{"Action": action, "Package": package, "Test": test}
                          for test in tests for action in ("run", "pass")]
                events.append({"Action": "pass", "Package": package})
                (root / f"{group}.jsonl").write_text(
                    "\n".join(json.dumps(event) for event in events) + "\n")
            runner.verify_proof_bundle(root, inventory, allow_local_simulation=True)
            with self.assertRaisesRegex(ValueError, "local simulation"):
                runner.verify_proof_bundle(root, inventory)
            (root / "readiness.jsonl").unlink()
            with self.assertRaisesRegex(ValueError, "missing.*readiness"):
                runner.verify_proof_bundle(root, inventory, allow_local_simulation=True)


    def test_preproof_executables_are_exact(self) -> None:
        """No setup command may change reviewed test bytes before proof."""
        workflow = (ROOT / runner.WORKFLOW).read_text(encoding="utf-8")
        mutations = (
            ("        uses: actions/setup-go@v6\n", "        uses: actions/setup-go@v5\n"),
            ("        run: scripts/ci/go-mod-download-retry.sh\n",
             "        run: |\n          scripts/ci/go-mod-download-retry.sh\n"
             "          python3 -c 'print(1)'\n"),
            ("          python3 scripts/lib/postgres_scheduled_runner.py\n",
             "          python3 scripts/lib/postgres_scheduled_runner.py\n"
             "          python3 -c 'print(1)'\n"),
            ("            --backend-identity \"$postgres_identity\"\n",
             "            --backend-identity \"$postgres_identity\"\n"
             "          python3 -c 'print(1)'\n"),
            ("-c 'CREATE DATABASE eshu_crossplane_scheduled'\n",
             "-c 'CREATE DATABASE eshu_crossplane_scheduled'\n"
             "        run: python3 -c 'print(1)'\n"),
        )
        inventory = runner.load_inventory()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            copy_contract(root)
            for relative in inventory:
                copy = root / relative
                copy.parent.mkdir(parents=True, exist_ok=True)
                copy.write_bytes((ROOT / relative).read_bytes())
            target = root / runner.WORKFLOW
            target.parent.mkdir(parents=True)
            action = root / runner.BULK_ACTION
            action_source = action.read_text()
            for old, new in mutations:
                with self.subTest(old=old):
                    subject = target if old in workflow else action
                    source = workflow if subject == target else action_source
                    self.assertIn(old, source)
                    subject.write_text(source.replace(old, new, 1))
                    with self.assertRaises(ValueError):
                        runner.verify_inventory(root, inventory)
                    subject.write_text(source)


    def test_go_proof_guard_rejects_dirty_checkout(self) -> None:
        """The actual pre-test shell rejects tracked and untracked source drift."""
        action = (ROOT / runner.BULK_ACTION).read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^    - name: Run Crossplane proofs\n.*?^      run: \|\n"
            r"(.*?)(?=^        go test)", action)
        self.assertIsNotNone(match)
        script = "\n".join(line[8:] for line in match.group(1).splitlines())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "checkout"
            root.mkdir()
            (root / ".gitignore").write_text("go.work\n", encoding="utf-8")
            for relative in ("go/proof_test.go", "sdk/proof.go", "scripts/ci/go-mod-download-retry.sh",
                             ".github/workflows/postgres-scheduled-live.yml"):
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(
                    'func TestProof(t *testing.T) { t.Fatal("reached") }\n'
                    if relative == "go/proof_test.go" else "reviewed\n")
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            subprocess.run(["git", "add", "-A"], cwd=root, check=True)
            subprocess.run(["git", "-c", "user.name=Proof", "-c",
                            "user.email=proof@example.invalid", "commit", "-qm", "fixture"],
                           cwd=root, check=True)
            def run(goenv: str = "off") -> int:
                """Execute the workflow's real source guard in the fixture repo."""
                return subprocess.run(["bash", "-c", script], cwd=root / "go",
                                      env={**{key: value for key, value in os.environ.items()
                                              if key not in ("GOFLAGS", "GOWORK")},
                                           "GOENV": goenv},
                                      check=False, capture_output=True).returncode
            self.assertEqual(run(), 0)
            target = root / "go/proof_test.go"
            target.write_text('func TestProof(t *testing.T) { return }\n')
            self.assertNotEqual(run(), 0)
            subprocess.run(["git", "add", "go/proof_test.go"], cwd=root, check=True)
            self.assertNotEqual(run(), 0)
            subprocess.run(["git", "restore", "--staged", "go/proof_test.go"],
                           cwd=root, check=True)
            target.write_text('func TestProof(t *testing.T) { t.Fatal("reached") }\n')
            (root / "go/new_test.go").write_text("new\n")
            self.assertNotEqual(run(), 0)
            (root / "go/new_test.go").unlink()
            sdk = root / "sdk/proof.go"
            sdk.write_text("changed\n")
            self.assertNotEqual(run(), 0, "SDK source drift must fail the real guard")
            sdk.write_text("reviewed\n")
            workspace = root / "go.work"
            workspace.write_text("go 1.26.9\n")
            with self.subTest(configuration="ignored workspace"):
                self.assertNotEqual(run(), 0, "ignored workspace must fail")
            workspace.unlink()
            ancestor_workspace = Path(temporary) / "go.work"
            ancestor_workspace.write_text("go 1.26.9\nuse ./checkout/go\n")
            with self.subTest(configuration="ancestor workspace"):
                self.assertNotEqual(run(), 0, "ancestor workspace must fail")
            ancestor_workspace.unlink()
            goenv_file = Path(temporary) / "go-env"
            goenv_file.write_text("GOFLAGS=-run=^$\n")
            with self.subTest(configuration="persisted GOFLAGS"):
                self.assertNotEqual(run(str(goenv_file)), 0,
                                    "persisted GOFLAGS must fail")
            helper = root / "scripts/ci/go-mod-download-retry.sh"
            helper.write_text("changed\n")
            self.assertNotEqual(run(), 0)

class CgoEnvAllowlistTest(unittest.TestCase):
    """#9066: the grammar allows exactly the reviewed workflow-level env stanza."""

    STANZA = 'env:\n  CGO_CFLAGS: "-O2 -g -std=gnu17"\n'

    def test_only_the_reviewed_env_stanza_is_allowed(self) -> None:
        workflow = (ROOT / runner.WORKFLOW).read_text(encoding="utf-8")
        action = (ROOT / runner.BULK_ACTION).read_text(encoding="utf-8")
        self.assertIn(self.STANZA, workflow)
        runner.verify_canonical_workflow(runner.expand_bulk_action(workflow, action))
        for label, stanza in (
            ("other key", 'env:\n  OTHER: "1"\n'),
            ("other value", 'env:\n  CGO_CFLAGS: "-O3"\n'),
            ("extra key", self.STANZA + '  BASH_ENV: "/tmp/x"\n'),
        ):
            with self.subTest(label=label):
                mutated = workflow.replace(self.STANZA, stanza, 1)
                with self.assertRaises(ValueError):
                    runner.verify_canonical_workflow(
                        runner.expand_bulk_action(mutated, action))


if __name__ == "__main__":
    unittest.main()
