"""Regression tests for scheduled proof artifacts across partial reruns."""

import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent.parent
LIBRARY = ROOT / "scripts/lib/postgres_scheduled_runner.py"
sys.path.insert(0, str(LIBRARY.parent))
SPEC = importlib.util.spec_from_file_location("postgres_scheduled_runner", LIBRARY)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)

LEG_FILES = {
    "setup": ("manifest.json",),
    "freshness-scale": ("freshness_links_scale.jsonl",),
    "freshness-a": ("freshness_links_a.jsonl",),
    "freshness-b": ("freshness_links_b.jsonl",),
    "ack": ("ack_family.jsonl",),
    "bulk": tuple(f"{group}.jsonl" for group in MODULE.PACKAGES
                  if group not in {"freshness_links_scale", "freshness_links_a",
                                   "freshness_links_b", "ack_family"}),
}


def seed_artifact(root: Path, leg: str, run_id: str, attempt: int,
                  content: str = "proof") -> Path:
    """Create one downloaded artifact directory with its exact leg files."""
    artifact = root / f"scheduled-proof-streams-{leg}-{run_id}-{attempt}"
    artifact.mkdir()
    for name in LEG_FILES[leg]:
        value = (json.dumps({"run_id": run_id, "run_attempt": attempt})
                 if leg == "setup" else content)
        (artifact / name).write_text(value, encoding="utf-8")
    return artifact


class ScheduledRerunTest(unittest.TestCase):
    """A partial rerun may reuse only same-run, same-head proof streams."""

    def test_new_assembly_files_trigger_pr_proof(self) -> None:
        paths = {
            "scripts/lib/postgres_scheduled_artifacts.py",
            "scripts/test-postgres-scheduled-rerun.py",
        }
        self.assertTrue(paths.issubset(MODULE.PR_PATHS))
        fragment = (ROOT / MODULE.FRAGMENT).read_text(encoding="utf-8")
        self.assertTrue(all(f"  - {path}\n" in fragment for path in paths))

    def test_assembly_command_cannot_be_replaced_with_echo(self) -> None:
        workflow = (ROOT / MODULE.WORKFLOW).read_text(encoding="utf-8")
        aggregate = MODULE.job_blocks(workflow)["scheduled-aggregate"]
        command = "          python3 scripts/lib/postgres_scheduled_runner.py \\"
        self.assertIn(command, aggregate)
        altered = aggregate.replace(command, "          echo assembly-skipped", 1)
        steps = re.split(r"(?m)^      - name:", altered)
        with self.assertRaisesRegex(ValueError, "assembly"):
            MODULE.verify_aggregate_bundle(steps)

    def test_partial_rerun_selects_latest_leg_and_retains_sources(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            downloads, bundle = root / "downloads", root / "bundle"
            downloads.mkdir()
            for leg in LEG_FILES:
                seed_artifact(downloads, leg, "123", 1, "attempt-1")
            seed_artifact(downloads, "freshness-b", "123", 2, "attempt-2")
            MODULE.assemble_proof_bundle(downloads, bundle, "123", 2)
            self.assertEqual((bundle / "freshness_links_b.jsonl").read_text(),
                             "attempt-2")
            self.assertEqual((bundle / "freshness_links_a.jsonl").read_text(),
                             "attempt-1")
            sources = json.loads((bundle / "artifact_sources.json").read_text())
            self.assertEqual(sources["run_id"], "123")
            self.assertEqual(sources["run_attempt"], 2)
            self.assertEqual(sources["legs"]["freshness-b"], 2)
            self.assertEqual(sources["legs"]["setup"], 1)

    def test_full_rerun_uses_current_attempt_for_every_leg(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            downloads, bundle = root / "downloads", root / "bundle"
            downloads.mkdir()
            for leg in LEG_FILES:
                seed_artifact(downloads, leg, "123", 1, "old")
                seed_artifact(downloads, leg, "123", 2, "current")
            MODULE.assemble_proof_bundle(downloads, bundle, "123", 2)
            sources = json.loads((bundle / "artifact_sources.json").read_text())
            self.assertEqual(set(sources["legs"].values()), {2})
            self.assertEqual((bundle / "freshness_links_a.jsonl").read_text(),
                             "current")

    def test_missing_or_future_leg_cannot_fill_bundle(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            downloads, bundle = root / "downloads", root / "bundle"
            downloads.mkdir()
            for leg in LEG_FILES:
                if leg != "ack":
                    seed_artifact(downloads, leg, "123", 1)
            seed_artifact(downloads, "ack", "999", 1)
            with self.assertRaisesRegex(ValueError, "missing.*ack"):
                MODULE.assemble_proof_bundle(downloads, bundle, "123", 2)
            seed_artifact(downloads, "ack", "123", 3)
            with self.assertRaisesRegex(ValueError, "future.*ack"):
                MODULE.assemble_proof_bundle(downloads, bundle, "123", 2)
            self.assertFalse(bundle.exists())

    def test_incomplete_latest_leg_cannot_fall_back(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            downloads, bundle = root / "downloads", root / "bundle"
            downloads.mkdir()
            for leg in LEG_FILES:
                seed_artifact(downloads, leg, "123", 1)
            latest = seed_artifact(downloads, "freshness-b", "123", 2)
            (latest / "freshness_links_b.jsonl").unlink()
            with self.assertRaisesRegex(ValueError, "freshness-b"):
                MODULE.assemble_proof_bundle(downloads, bundle, "123", 2)

    def test_partial_rerun_verifies_full_bundle_and_rejects_cross_head(self) -> None:
        inventory = MODULE.load_inventory()
        manifest = MODULE.build_manifest(
            inventory, "pull_request", "123", "1",
            "https://github.com/eshu-hq/eshu/actions/runs/123/attempts/1",
            "a" * 40, "b" * 40, "PostgreSQL 18.6 on aarch64-linux",
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            downloads, bundle = root / "downloads", root / "bundle"
            downloads.mkdir()
            for leg in LEG_FILES:
                seed_artifact(downloads, leg, "123", 1)
            seed_artifact(downloads, "freshness-b", "123", 2)
            (downloads / "scheduled-proof-streams-setup-123-1" /
             "manifest.json").write_text(json.dumps(manifest))
            for leg, files in LEG_FILES.items():
                for filename in files:
                    if filename == "manifest.json":
                        continue
                    group = filename.removesuffix(".jsonl")
                    tests = sorted(test for row in inventory.values()
                                   if row["group"] == group
                                   for test in row["tests"])
                    events = [
                        {"Action": action, "Package": MODULE.PACKAGES[group],
                         "Test": test}
                        for test in tests for action in ("run", "pass")
                    ] + [{"Action": "pass", "Package": MODULE.PACKAGES[group]}]
                    content = "\n".join(json.dumps(event) for event in events) + "\n"
                    (downloads / f"scheduled-proof-streams-{leg}-123-1" /
                     filename).write_text(content)
                    if leg == "freshness-b":
                        (downloads / "scheduled-proof-streams-freshness-b-123-2" /
                         filename).write_text(content)
            MODULE.assemble_proof_bundle(downloads, bundle, "123", 2)
            event_path = root / "event.json"
            event_path.write_text(json.dumps({
                "pull_request": {"head": {"sha": "a" * 40}},
            }))
            env = {
                "GITHUB_ACTIONS": "true",
                "GITHUB_REPOSITORY": "eshu-hq/eshu",
                "GITHUB_EVENT_NAME": "pull_request",
                "GITHUB_RUN_ID": "123",
                "GITHUB_RUN_ATTEMPT": "2",
                "GITHUB_SHA": "b" * 40,
                "GITHUB_EVENT_PATH": str(event_path),
            }
            with mock.patch.dict(os.environ, env, clear=True):
                MODULE.verify_proof_bundle(bundle, inventory)
                sources_path = bundle / "artifact_sources.json"
                sources = json.loads(sources_path.read_text())
                sources_path.write_text(json.dumps({
                    **sources, "legs": {**sources["legs"], "ack": 3},
                }))
                with self.assertRaisesRegex(ValueError, "source attempt"):
                    MODULE.verify_proof_bundle(bundle, inventory)
                sources_path.write_text(json.dumps(sources))
                event_path.write_text(json.dumps({
                    "pull_request": {"head": {"sha": "c" * 40}},
                }))
                with self.assertRaisesRegex(ValueError, "reviewed head SHA"):
                    MODULE.verify_proof_bundle(bundle, inventory)
                event_path.write_text(json.dumps({
                    "pull_request": {"head": {"sha": "a" * 40}},
                }))
                (bundle / "freshness_links_b.jsonl").write_text("not-json\n")
                with self.assertRaisesRegex(ValueError, "event stream"):
                    MODULE.verify_proof_bundle(bundle, inventory)

    def test_duplicate_stream_in_latest_artifact_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            downloads, bundle = root / "downloads", root / "bundle"
            downloads.mkdir()
            for leg in LEG_FILES:
                seed_artifact(downloads, leg, "123", 1)
            bulk = downloads / "scheduled-proof-streams-bulk-123-1"
            (bulk / "ack_family.jsonl").write_text("duplicate")
            with self.assertRaisesRegex(ValueError, "bulk"):
                MODULE.assemble_proof_bundle(downloads, bundle, "123", 1)

    def test_prior_attempt_manifest_still_requires_exact_run_and_head(self) -> None:
        inventory = MODULE.load_inventory()
        manifest = MODULE.build_manifest(
            inventory, "pull_request", "123", "1",
            "https://github.com/eshu-hq/eshu/actions/runs/123/attempts/1",
            "a" * 40, "b" * 40, "PostgreSQL 18.6 on aarch64-linux",
        )
        with tempfile.TemporaryDirectory() as directory:
            event_path = Path(directory) / "event.json"
            event_path.write_text(json.dumps({
                "pull_request": {"head": {"sha": "a" * 40}},
            }))
            env = {
                "GITHUB_ACTIONS": "true",
                "GITHUB_REPOSITORY": "eshu-hq/eshu",
                "GITHUB_EVENT_NAME": "pull_request",
                "GITHUB_RUN_ID": "123",
                "GITHUB_RUN_ATTEMPT": "2",
                "GITHUB_SHA": "b" * 40,
                "GITHUB_EVENT_PATH": str(event_path),
            }
            with mock.patch.dict(os.environ, env, clear=True):
                MODULE.verify_hosted_environment(manifest)
                os.environ["GITHUB_RUN_ID"] = "999"
                with self.assertRaisesRegex(ValueError, "run ID"):
                    MODULE.verify_hosted_environment(manifest)
                os.environ["GITHUB_RUN_ID"] = "123"
                event_path.write_text(json.dumps({
                    "pull_request": {"head": {"sha": "c" * 40}},
                }))
                with self.assertRaisesRegex(ValueError, "reviewed head SHA"):
                    MODULE.verify_hosted_environment(manifest)
                os.environ["GITHUB_RUN_ATTEMPT"] = "0"
                with self.assertRaisesRegex(ValueError, "run attempt"):
                    MODULE.verify_hosted_environment(manifest)


if __name__ == "__main__":
    unittest.main()
