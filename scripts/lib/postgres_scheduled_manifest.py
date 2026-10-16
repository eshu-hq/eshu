#!/usr/bin/env python3
"""Build source-bound manifests for scheduled PostgreSQL live proofs."""

import hashlib
from pathlib import Path
import re


def source_digest(root: Path, inventory: dict[str, dict[str, object]]) -> str:
    """Bind the manifest to bytes of every enrolled source file."""
    digest = hashlib.sha256()
    for relative in sorted(inventory):
        source_hash = hashlib.sha256((root / relative).read_bytes()).hexdigest()
        digest.update(f"{relative}\0{source_hash}\n".encode("utf-8"))
    return digest.hexdigest()


def build_manifest(
    inventory: dict[str, dict[str, object]], event: str, run_id: str,
    run_attempt: str, run_url: str, reviewed_head_sha: str,
    checkout_sha: str, backend_identity: str, event_sha: str | None = None,
    source_root: Path | None = None,
    local_simulation: bool = False,
    *, inventory_path: Path, packages: dict[str, str], image: str,
) -> dict[str, object]:
    """Describe one hosted attempt without recording credentials or DSNs."""
    if event not in {"pull_request", "schedule", "workflow_dispatch"}:
        raise ValueError("invalid hosted event")
    if not re.fullmatch(r"[1-9][0-9]*", run_id or ""):
        raise ValueError("invalid hosted run ID")
    if not re.fullmatch(r"[1-9][0-9]*", run_attempt or ""):
        raise ValueError("invalid hosted run attempt")
    expected_url = (
        f"https://github.com/eshu-hq/eshu/actions/runs/{run_id}"
        f"/attempts/{run_attempt}"
    )
    if run_url != expected_url:
        raise ValueError("hosted run URL does not match run ID and attempt")
    if not re.fullmatch(r"[0-9a-f]{40}", reviewed_head_sha or ""):
        raise ValueError("invalid reviewed head SHA")
    if not re.fullmatch(r"[0-9a-f]{40}", checkout_sha or ""):
        raise ValueError("invalid checkout SHA")
    event_sha = event_sha or checkout_sha
    if event_sha != checkout_sha:
        raise ValueError("event SHA differs from actual checkout SHA")
    if event != "pull_request" and reviewed_head_sha != checkout_sha:
        raise ValueError("non-PR reviewed head differs from checkout SHA")
    if not re.match(r"^PostgreSQL 18\.6\b", backend_identity or ""):
        raise ValueError("observed backend identity must be PostgreSQL 18.6")
    if "\n" in backend_identity or "postgres://" in backend_identity:
        raise ValueError("invalid backend identity")
    groups = {
        group: {
            "files": sum(row["group"] == group for row in inventory.values()),
            "tests": sum(
                len(row["tests"]) for row in inventory.values()
                if row["group"] == group
            ),
        }
        for group in packages
    }
    return {
        "schema": "postgres-scheduled-proof/v1",
        "proof_origin": "local_simulation" if local_simulation else "hosted",
        "event": event,
        "run_id": run_id,
        "run_attempt": int(run_attempt),
        "run_url": run_url,
        "reviewed_head_sha": reviewed_head_sha,
        "event_sha": event_sha,
        "checkout_sha": checkout_sha,
        "inventory_sha256": hashlib.sha256(inventory_path.read_bytes()).hexdigest(),
        "source_sha256": source_digest(
            source_root or Path(__file__).resolve().parents[2], inventory
        ),
        "groups": groups,
        "postgres_image": image,
        "postgres_identity": backend_identity,
    }
