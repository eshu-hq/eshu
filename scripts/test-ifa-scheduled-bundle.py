#!/usr/bin/env python3
"""Seeded missing and tampered Ifá receipt tests."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent.parent
LIB = ROOT / "scripts/lib"
sys.path.insert(0, str(LIB))
SPEC = importlib.util.spec_from_file_location(
    "ifa_scheduled_bundle", LIB / "ifa_scheduled_bundle.py"
)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def nornic_identity(container_id: str, http_port: str, bolt_port: str) -> dict[str, object]:
    """Make a synthetic pinned identity for bundle validation."""
    return {
        "container_id": container_id * 64,
        "image_id": "sha256:" + "1" * 64,
        "resolved_image_id": "sha256:" + "1" * 64,
        "repo_digests": ["ghcr.io/eshu-hq/nornicdb-amd64-cpu@" + MODULE.NORNIC_DIGEST],
        "image_pin": "ghcr.io/eshu-hq/nornicdb-amd64-cpu@" + MODULE.NORNIC_DIGEST,
        "component": "NornicDB",
        "version": "1.3.3",
        "http_host_ip": "127.0.0.1",
        "http_host_port": http_port,
        "bolt_host_ip": "127.0.0.1",
        "bolt_host_port": bolt_port,
    }


class BundleTest(unittest.TestCase):
    """Only complete receipts with every named event can pass."""

    def test_missing_tampered_and_local_receipt(self) -> None:
        """Reject missing files, altered source checksum, and skipped tests."""
        inventory = MODULE.load_inventory()
        checkout_sha = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
        ).strip()
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            manifest = {
                "schema": "ifa-scheduled-proof/v1",
                "environment": "local_simulation",
                "run_url": "local://ifa-scheduled-writer-exercise",
                "run_id": "local-writer-exercise",
                "run_attempt": 1,
                "event": "local_proof",
                "reviewed_head_sha": checkout_sha,
                "checkout_sha": checkout_sha,
                "inventory_sha256": MODULE.inventory_digest(ROOT),
                "primary": nornic_identity("b", "37474", "37687"),
                "mismatch": nornic_identity("c", "38474", "38687"),
                "postgres": {
                    "container_id": "d" * 64,
                    "image_id": "sha256:" + "2" * 64,
                    "resolved_image_id": "sha256:" + "2" * 64,
                    "repo_digests": [
                        "ghcr.io/eshu-hq/ci-postgres-alpine@" + MODULE.POSTGRES_DIGEST
                    ],
                    "image_pin": "ghcr.io/eshu-hq/ci-postgres-alpine@" + MODULE.POSTGRES_DIGEST,
                    "version": "18.6",
                },
            }
            manifest_path = directory / "manifest.json"
            manifest_path.write_text(json.dumps(manifest))
            for package, filename in MODULE.PACKAGES:
                names = sorted(
                    name for row in inventory.values()
                    if row["package"] == package for name in row["tests"]
                )
                rows = [
                    {"Package": package, "Action": action, "Test": test}
                    for test in names for action in ("run", "pass")
                ]
                rows.append({"Package": package, "Action": "pass"})
                (directory / filename).write_text(
                    "\n".join(json.dumps(row) for row in rows) + "\n"
                )
            self.assertTrue(MODULE.verify_bundle(ROOT, directory, allow_local=True))
            hosted = {
                **manifest,
                "environment": "hosted",
                "event": "pull_request",
                "run_url": "https://github.com/eshu-hq/eshu/actions/runs/123/attempts/1",
                "run_id": "123",
                "primary": nornic_identity("b", "17474", "17687"),
                "mismatch": nornic_identity("c", "18474", "18687"),
            }
            manifest_path.write_text(json.dumps(hosted))
            with mock.patch.dict(os.environ, {"GITHUB_ACTIONS": "false"}):
                self.assertTrue(MODULE.verify_bundle(ROOT, directory))
            event_path = directory / "event.json"
            event_path.write_text(json.dumps({
                "pull_request": {"head": {"sha": "a" * 40}}
            }))
            hosted["reviewed_head_sha"] = "a" * 40
            manifest_path.write_text(json.dumps(hosted))
            context = {
                "GITHUB_ACTIONS": "true",
                "GITHUB_EVENT_NAME": "pull_request",
                "GITHUB_RUN_ID": "123",
                "GITHUB_RUN_ATTEMPT": "1",
                "GITHUB_EVENT_PATH": str(event_path),
                "GITHUB_SHA": checkout_sha,
            }
            with mock.patch.dict(os.environ, context):
                self.assertTrue(MODULE.verify_bundle(ROOT, directory))
            with mock.patch.dict(os.environ, {**context, "GITHUB_SHA": "f" * 40}):
                with self.assertRaises(ValueError):
                    MODULE.verify_bundle(ROOT, directory)
            manifest_path.write_text(json.dumps({
                **hosted, "reviewed_head_sha": "b" * 40
            }))
            with mock.patch.dict(os.environ, context):
                with self.assertRaises(ValueError):
                    MODULE.verify_bundle(ROOT, directory)
            manifest_path.write_text(json.dumps(hosted))
            with mock.patch.dict(os.environ, {
                "GITHUB_ACTIONS": "true",
                "GITHUB_EVENT_NAME": "pull_request",
                "GITHUB_RUN_ID": "999",
                "GITHUB_RUN_ATTEMPT": "1",
            }):
                with self.assertRaises(ValueError):
                    MODULE.verify_bundle(ROOT, directory)
            manifest_path.write_text(json.dumps(manifest))
            with self.assertRaises(ValueError):
                MODULE.verify_bundle(ROOT, directory)
            manifest_path.write_text(json.dumps({
                **manifest, "inventory_sha256": "0" * 64
            }))
            with self.assertRaises(ValueError):
                MODULE.verify_bundle(ROOT, directory, allow_local=True)
            manifest_path.write_text(json.dumps(manifest))
            reducer = directory / "reducer.jsonl"
            original = reducer.read_text()
            reducer.write_text(original.replace('"Action": "pass"', '"Action": "skip"', 1))
            with self.assertRaises(ValueError):
                MODULE.verify_bundle(ROOT, directory, allow_local=True)
            reducer.write_text(original)
            manifest_path.unlink()
            with self.assertRaises(OSError):
                MODULE.verify_bundle(ROOT, directory, allow_local=True)
            manifest_path.write_text(json.dumps(manifest))
            reducer.unlink()
            with self.assertRaises((OSError, ValueError)):
                MODULE.verify_bundle(ROOT, directory, allow_local=True)


if __name__ == "__main__":
    unittest.main()
