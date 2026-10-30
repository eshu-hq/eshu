#!/usr/bin/env python3
"""Verify a complete Ifá hosted manifest and both Go JSON event streams."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from ifa_scheduled_events import load_inventory, verify_inventory
from ifa_scheduled_manifest import verify_backends
from reducer_contention_events import verify_events


PACKAGES = (
    ("github.com/eshu-hq/eshu/go/internal/replay/offlinetier", "repo.jsonl"),
    ("github.com/eshu-hq/eshu/go/cmd/reducer", "reducer.jsonl"),
)
ROOT = Path(__file__).resolve().parents[2]
NORNIC_DIGEST = "sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
POSTGRES_DIGEST = "sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
SHA_PATTERN = re.compile(r"[0-9a-f]{40}")


def inventory_digest(root: Path) -> str:
    """Hash the exact inventory bytes read by this checkout."""
    data = (root / "scripts/lib/ifa_scheduled_inventory.json").read_bytes()
    return hashlib.sha256(data).hexdigest()


def verify_manifest(root: Path, manifest: dict[str, object], allow_local: bool) -> None:
    """Reject an incomplete, stale, or locally simulated hosted receipt."""
    if manifest.get("schema") != "ifa-scheduled-proof/v1":
        raise ValueError("wrong Ifá manifest schema")
    if manifest.get("inventory_sha256") != inventory_digest(root):
        raise ValueError("Ifá manifest inventory checksum drifted")
    checkout = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=root, check=True, capture_output=True, text=True
    ).stdout.strip()
    if manifest.get("checkout_sha") != checkout:
        raise ValueError("Ifá manifest checkout SHA differs from this revision")
    reviewed = manifest.get("reviewed_head_sha")
    if not isinstance(reviewed, str) or not SHA_PATTERN.fullmatch(reviewed):
        raise ValueError("missing reviewed head SHA")
    environment = manifest.get("environment")
    if environment == "hosted":
        if manifest.get("event") not in {"schedule", "workflow_dispatch", "pull_request"}:
            raise ValueError("wrong hosted event")
        run_id = manifest.get("run_id")
        attempt = manifest.get("run_attempt")
        if (not isinstance(run_id, str) or not run_id.isdecimal() or
                not isinstance(attempt, int) or attempt < 1):
            raise ValueError("missing hosted run identity")
        expected_url = f"https://github.com/eshu-hq/eshu/actions/runs/{run_id}/attempts/{attempt}"
        if manifest.get("run_url") != expected_url:
            raise ValueError("wrong hosted run URL")
        if os.environ.get("GITHUB_ACTIONS") == "true":
            if (manifest.get("event") != os.environ.get("GITHUB_EVENT_NAME") or
                    run_id != os.environ.get("GITHUB_RUN_ID") or
                    attempt != int(os.environ.get("GITHUB_RUN_ATTEMPT", "0"))):
                raise ValueError("manifest run identity differs from GitHub context")
            event_sha = os.environ.get("GITHUB_SHA")
            if not isinstance(event_sha, str) or not SHA_PATTERN.fullmatch(event_sha):
                raise ValueError("missing GitHub event SHA")
            if checkout != event_sha:
                raise ValueError("manifest checkout differs from GitHub event revision")
            event_path = Path(os.environ["GITHUB_EVENT_PATH"])
            event_payload = json.loads(event_path.read_text(encoding="utf-8"))
            if manifest["event"] == "pull_request":
                event_head = event_payload["pull_request"]["head"]["sha"]
            else:
                event_head = os.environ.get("GITHUB_SHA")
            if reviewed != event_head:
                raise ValueError("manifest reviewed head differs from GitHub event")
        primary_ports = ("17474", "17687")
        mismatch_ports = ("18474", "18687")
    elif environment == "local_simulation" and allow_local:
        if (manifest.get("event") != "local_proof" or
                manifest.get("run_url") != "local://ifa-scheduled-writer-exercise"):
            raise ValueError("local exercise lacks explicit non-hosted label")
        primary_ports = ("37474", "37687")
        mismatch_ports = ("38474", "38687")
    else:
        raise ValueError("local simulation is not a hosted proof")
    primary = manifest.get("primary")
    mismatch = manifest.get("mismatch")
    postgres = manifest.get("postgres")
    if not all(isinstance(item, dict) for item in (primary, mismatch, postgres)):
        raise ValueError("missing observed backend identities")
    verify_backends(primary, mismatch, postgres, primary_ports, mismatch_ports)


def verify_bundle(root: Path, directory: Path, *, allow_local: bool = False) -> bool:
    """Check the exact source enrollment, receipt, and both named streams."""
    inventory = load_inventory(root / "scripts/lib/ifa_scheduled_inventory.json")
    verify_inventory(root, inventory)
    manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise ValueError("Ifá manifest must be an object")
    verify_manifest(root, manifest, allow_local)
    for package, filename in PACKAGES:
        expected = {
            name for row in inventory.values() if row["package"] == package
            for name in row["tests"]
        }
        if not expected or not verify_events(directory / filename, package, expected, sys.stderr):
            raise ValueError(f"Ifá bundle lacks passing named events: {package}")
    print(f"Ifá bundle PASS: eight files, nine tests, manifest {manifest['environment']}")
    return True


def main() -> int:
    """Verify a hosted receipt, or an explicitly labeled local writer exercise."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--allow-local-proof", action="store_true")
    args = parser.parse_args()
    if args.allow_local_proof and os.environ.get("GITHUB_ACTIONS") == "true":
        raise ValueError("local proof mode is forbidden on GitHub Actions")
    try:
        return 0 if verify_bundle(ROOT, args.directory, allow_local=args.allow_local_proof) else 1
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
