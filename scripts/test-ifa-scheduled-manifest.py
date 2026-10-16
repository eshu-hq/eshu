#!/usr/bin/env python3
"""Seeded contract tests for retained Ifá hosted proof metadata."""

import importlib.util
from pathlib import Path
import sys
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent.parent
MODULE_PATH = ROOT / "scripts/lib/ifa_scheduled_manifest.py"
SPEC = importlib.util.spec_from_file_location("ifa_scheduled_manifest", MODULE_PATH)
assert SPEC and SPEC.loader
sys.path.insert(0, str(MODULE_PATH.parent))
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def nornic_identity(container_id: str, http_port: str, bolt_port: str) -> dict[str, object]:
    """Make one independently observed-looking NornicDB identity."""
    return {
        "container_id": container_id * 64,
        "image_id": "sha256:" + "1" * 64,
        "resolved_image_id": "sha256:" + "1" * 64,
        "repo_digests": ["ghcr.io/eshu-hq/nornicdb-amd64-cpu@" + MODULE.NORNIC_DIGEST],
        "image_pin": "ghcr.io/eshu-hq/nornicdb-amd64-cpu@" + MODULE.NORNIC_DIGEST,
        "http_host_ip": "127.0.0.1",
        "http_host_port": http_port,
        "bolt_host_ip": "127.0.0.1",
        "bolt_host_port": bolt_port,
        "component": "NornicDB",
        "version": "1.3.3",
    }


class ManifestTest(unittest.TestCase):
    """A green receipt binds the reviewed revision and observed backends."""

    def test_complete_receipt_and_missing_evidence(self) -> None:
        """Reject missing metadata, same endpoint, and unpinned images."""
        env = {
            "GITHUB_SERVER_URL": "https://github.com",
            "GITHUB_REPOSITORY": "eshu-hq/eshu",
            "GITHUB_RUN_ID": "123",
            "GITHUB_RUN_ATTEMPT": "2",
            "GITHUB_EVENT_NAME": "pull_request",
            "GITHUB_SHA": "e" * 40,
        }
        event = {"pull_request": {"head": {"sha": "a" * 40}}}
        primary = nornic_identity("b", "17474", "17687")
        mismatch = nornic_identity("c", "18474", "18687")
        postgres = {
            "container_id": "d" * 64,
            "image_id": "sha256:" + "2" * 64,
            "resolved_image_id": "sha256:" + "2" * 64,
            "repo_digests": ["ghcr.io/eshu-hq/ci-postgres-alpine@" + MODULE.POSTGRES_DIGEST],
            "image_pin": "ghcr.io/eshu-hq/ci-postgres-alpine@" + MODULE.POSTGRES_DIGEST,
            "version": "18.6",
        }

        def build(
            current_env: dict = env,
            current_event: dict = event,
            inventory_bytes: bytes = b"reviewed inventory",
            current_primary: dict = primary,
            current_mismatch: dict = mismatch,
            current_postgres: dict = postgres,
        ) -> dict:
            return MODULE.build_manifest(
                current_env, current_event, "e" * 40, inventory_bytes,
                current_primary, current_mismatch, current_postgres,
            )

        manifest = build()
        tagged_pin = "ghcr.io/eshu-hq/nornicdb-amd64-cpu:reviewed@" + MODULE.NORNIC_DIGEST
        self.assertEqual(
            build(current_primary={**primary, "image_pin": tagged_pin})["primary"]["image_pin"],
            tagged_pin,
        )
        self.assertEqual(manifest["reviewed_head_sha"], "a" * 40)
        self.assertEqual(manifest["checkout_sha"], "e" * 40)
        self.assertEqual(manifest["event"], "pull_request")
        self.assertEqual(manifest["run_attempt"], 2)
        self.assertNotEqual(
            manifest["primary"]["container_id"], manifest["mismatch"]["container_id"]
        )
        for changed in (
            {**env, "GITHUB_RUN_ATTEMPT": ""},
            {**env, "GITHUB_EVENT_NAME": "push"},
            {**env, "GITHUB_SHA": "f" * 40},
            {**env, "GITHUB_SHA": ""},
        ):
            with self.subTest(changed=changed):
                with self.assertRaises(ValueError):
                    build(current_env=changed)
        for change in (
            {"current_event": {}},
            {"inventory_bytes": b""},
            {"current_mismatch": primary},
            {"current_primary": {**primary, "version": ""}},
            {"current_mismatch": {**mismatch, "http_host_port": "17474"}},
            {"current_postgres": {**postgres, "image_id": "wrong"}},
            {"current_postgres": {**postgres, "resolved_image_id": "sha256:" + "3" * 64}},
            {"current_postgres": {**postgres, "repo_digests": []}},
            {"current_postgres": {
                **postgres, "repo_digests": ["wrong@" + MODULE.POSTGRES_DIGEST]
            }},
            {"current_postgres": {**postgres, "image_pin": "wrong@" + MODULE.POSTGRES_DIGEST}},
            {"current_postgres": {
                **postgres,
                "image_pin": "ghcr.io/eshu-hq/ci-postgres-alpine@sha256:" + "0" * 64,
            }},
            {"current_primary": {**primary, "bolt_host_ip": "0.0.0.0"}},
        ):
            with self.subTest(change=change):
                with self.assertRaises(ValueError):
                    build(**change)

    def test_docker_collection_binds_canonical_reference(self) -> None:
        """Inspect the canonical image independently of the container pin."""
        ports = (
            '{"7474/tcp":[{"HostIp":"127.0.0.1","HostPort":"37474"}],'
            '"7687/tcp":[{"HostIp":"127.0.0.1","HostPort":"37687"}]}'
        )
        pin = "ghcr.io/eshu-hq/nornicdb-amd64-cpu:tag@" + MODULE.NORNIC_DIGEST
        canonical = "ghcr.io/eshu-hq/nornicdb-amd64-cpu@" + MODULE.NORNIC_DIGEST
        responses = [
            mock.Mock(stdout=f"{'b' * 64}|sha256:{'1' * 64}|{pin}|{ports}\n"),
            mock.Mock(stdout=f"sha256:{'1' * 64}|[\"{canonical}\"]\n"),
        ]
        with mock.patch.object(MODULE.subprocess, "run", side_effect=responses) as run:
            identity = MODULE.docker_identity("proof", canonical)
        self.assertEqual(identity["resolved_image_id"], identity["image_id"])
        self.assertEqual(identity["repo_digests"], [canonical])
        self.assertEqual(run.call_args_list[1].args[0][-1], canonical)


if __name__ == "__main__":
    unittest.main()
