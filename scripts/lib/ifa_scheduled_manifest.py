#!/usr/bin/env python3
"""Capture immutable hosted metadata for the Ifá scheduled live proof."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from typing import Any, Mapping
import urllib.request


ROOT = Path(__file__).resolve().parents[2]
NORNIC_DIGEST = "sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
POSTGRES_DIGEST = "sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
NORNIC_REPOSITORY = "ghcr.io/eshu-hq/nornicdb-amd64-cpu"
POSTGRES_REPOSITORY = "ghcr.io/eshu-hq/ci-postgres-alpine"
SHA_PATTERN = re.compile(r"[0-9a-f]{40}")
CONTAINER_PATTERN = re.compile(r"[0-9a-f]{64}")


def required(value: object, label: str) -> str:
    """Return a nonempty string or fail the receipt."""
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"missing {label}")
    return value.strip()


def verify_identity(
    identity: Mapping[str, Any], repository: str, digest: str, label: str
) -> None:
    """Bind an observed container to the reviewed immutable image pin."""
    container_id = required(identity.get("container_id"), f"{label} container ID")
    if not CONTAINER_PATTERN.fullmatch(container_id):
        raise ValueError(f"invalid {label} container ID")
    image_id = required(identity.get("image_id"), f"{label} image ID")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
        raise ValueError(f"invalid {label} image ID")
    if identity.get("resolved_image_id") != image_id:
        raise ValueError(f"{label} container image differs from inspected image")
    canonical = f"{repository}@{digest}"
    repo_digests = identity.get("repo_digests")
    if not isinstance(repo_digests, list) or canonical not in repo_digests:
        raise ValueError(f"{label} inspected image lacks reviewed repository digest")
    image_pin = required(identity.get("image_pin"), f"{label} image pin")
    if not re.fullmatch(rf"{re.escape(repository)}(?::[^/@]+)?@{re.escape(digest)}", image_pin):
        raise ValueError(f"{label} image pin differs from reviewed digest")
    required(identity.get("version"), f"{label} version")


def verify_endpoint(identity: Mapping[str, Any], http_port: str, bolt_port: str) -> None:
    """Bind each queried HTTP endpoint to its distinct Docker container."""
    if (identity.get("http_host_ip") != "127.0.0.1" or
            identity.get("http_host_port") != http_port or
            identity.get("bolt_host_ip") != "127.0.0.1" or
            identity.get("bolt_host_port") != bolt_port):
        raise ValueError("observed NornicDB endpoint mapping drifted")


def verify_backends(
    primary: Mapping[str, Any],
    mismatch: Mapping[str, Any],
    postgres: Mapping[str, Any],
    primary_ports: tuple[str, str],
    mismatch_ports: tuple[str, str],
) -> None:
    """Require distinct, observed pinned services at their declared ports."""
    verify_identity(primary, NORNIC_REPOSITORY, NORNIC_DIGEST, "primary NornicDB")
    verify_identity(mismatch, NORNIC_REPOSITORY, NORNIC_DIGEST, "mismatch NornicDB")
    verify_identity(postgres, POSTGRES_REPOSITORY, POSTGRES_DIGEST, "PostgreSQL")
    if primary["container_id"] == mismatch["container_id"]:
        raise ValueError("primary and mismatch endpoints share a container")
    if primary_ports[0] == mismatch_ports[0] or primary_ports[1] == mismatch_ports[1]:
        raise ValueError("primary and mismatch endpoint ports must differ")
    verify_endpoint(primary, *primary_ports)
    verify_endpoint(mismatch, *mismatch_ports)
    if primary.get("component") != "NornicDB" or mismatch.get("component") != "NornicDB":
        raise ValueError("observed graph backend is not NornicDB")
    if not required(postgres.get("version"), "PostgreSQL version").startswith("18.6"):
        raise ValueError("observed PostgreSQL is not version 18.6")


def build_manifest(
    env: Mapping[str, str],
    event: Mapping[str, Any],
    checkout_sha: str,
    inventory_bytes: bytes,
    primary: Mapping[str, Any],
    mismatch: Mapping[str, Any],
    postgres: Mapping[str, Any],
) -> dict[str, object]:
    """Validate the complete hosted revision and backend receipt."""
    event_name = required(env.get("GITHUB_EVENT_NAME"), "event name")
    if event_name not in {"schedule", "workflow_dispatch", "pull_request"}:
        raise ValueError("unexpected hosted event")
    run_id = required(env.get("GITHUB_RUN_ID"), "run ID")
    attempt = required(env.get("GITHUB_RUN_ATTEMPT"), "run attempt")
    if not run_id.isdecimal() or not attempt.isdecimal() or int(run_id) < 1 or int(attempt) < 1:
        raise ValueError("invalid hosted run identity")
    server = required(env.get("GITHUB_SERVER_URL"), "GitHub server").rstrip("/")
    repository = required(env.get("GITHUB_REPOSITORY"), "GitHub repository")
    if server != "https://github.com" or repository != "eshu-hq/eshu":
        raise ValueError("unexpected hosted repository")
    if event_name == "pull_request":
        pull_request = event.get("pull_request")
        head = pull_request.get("head") if isinstance(pull_request, dict) else None
        reviewed_sha = head.get("sha") if isinstance(head, dict) else None
    else:
        reviewed_sha = env.get("GITHUB_SHA")
    if not isinstance(reviewed_sha, str) or not SHA_PATTERN.fullmatch(reviewed_sha):
        raise ValueError("missing reviewed head SHA")
    if not SHA_PATTERN.fullmatch(checkout_sha):
        raise ValueError("missing actual checkout SHA")
    event_sha = env.get("GITHUB_SHA")
    if not isinstance(event_sha, str) or not SHA_PATTERN.fullmatch(event_sha):
        raise ValueError("missing GitHub event SHA")
    if checkout_sha != event_sha:
        raise ValueError("actual checkout differs from GitHub event revision")
    if not inventory_bytes:
        raise ValueError("missing inventory bytes")
    verify_backends(primary, mismatch, postgres, ("17474", "17687"), ("18474", "18687"))
    return {
        "schema": "ifa-scheduled-proof/v1",
        "environment": "hosted",
        "run_url": f"{server}/{repository}/actions/runs/{run_id}/attempts/{attempt}",
        "run_id": run_id,
        "run_attempt": int(attempt),
        "event": event_name,
        "reviewed_head_sha": reviewed_sha,
        "checkout_sha": checkout_sha,
        "inventory_sha256": hashlib.sha256(inventory_bytes).hexdigest(),
        "primary": dict(primary),
        "mismatch": dict(mismatch),
        "postgres": dict(postgres),
    }


def build_local_manifest(
    checkout_sha: str,
    inventory_bytes: bytes,
    primary: Mapping[str, Any],
    mismatch: Mapping[str, Any],
    postgres: Mapping[str, Any],
) -> dict[str, object]:
    """Label a read-only local writer exercise as non-hosted evidence."""
    if not SHA_PATTERN.fullmatch(checkout_sha) or not inventory_bytes:
        raise ValueError("missing local checkout or inventory")
    verify_backends(
        primary, mismatch, postgres,
        (primary["http_host_port"], primary["bolt_host_port"]),
        (mismatch["http_host_port"], mismatch["bolt_host_port"]),
    )
    return {
        "schema": "ifa-scheduled-proof/v1",
        "environment": "local_simulation",
        "run_url": "local://ifa-scheduled-writer-exercise",
        "run_id": "local-writer-exercise",
        "run_attempt": 1,
        "event": "local_proof",
        "reviewed_head_sha": checkout_sha,
        "checkout_sha": checkout_sha,
        "inventory_sha256": hashlib.sha256(inventory_bytes).hexdigest(),
        "primary": dict(primary),
        "mismatch": dict(mismatch),
        "postgres": dict(postgres),
    }


def docker_identity(container: str, canonical: str) -> dict[str, Any]:
    """Bind a running container to an independently inspected image reference."""
    result = subprocess.run(
        ["docker", "inspect", "--format",
         "{{.Id}}|{{.Image}}|{{.Config.Image}}|{{json .NetworkSettings.Ports}}",
         container],
        check=True,
        capture_output=True,
        text=True,
    )
    fields = result.stdout.strip().split("|")
    if len(fields) != 4:
        raise ValueError("malformed Docker identity")
    identity: dict[str, Any] = dict(
        zip(("container_id", "image_id", "image_pin"), fields[:3], strict=True)
    )
    image = subprocess.run(
        ["docker", "image", "inspect", "--format", "{{.Id}}|{{json .RepoDigests}}", canonical],
        check=True,
        capture_output=True,
        text=True,
    )
    image_fields = image.stdout.strip().split("|", 1)
    if len(image_fields) != 2:
        raise ValueError("malformed Docker image inspection")
    identity["resolved_image_id"] = image_fields[0]
    identity["repo_digests"] = json.loads(image_fields[1])
    ports = json.loads(fields[3])
    if not isinstance(ports, dict):
        raise ValueError("malformed Docker port mapping")
    for internal_port, name in (("7474/tcp", "http"), ("7687/tcp", "bolt")):
        bindings = ports.get(internal_port)
        if isinstance(bindings, list) and len(bindings) == 1:
            identity[f"{name}_host_ip"] = bindings[0].get("HostIp", "")
            identity[f"{name}_host_port"] = bindings[0].get("HostPort", "")
    return identity


def nornic_component(port: int) -> dict[str, str]:
    """Observe the graph engine name and version through its live HTTP API."""
    statement = "CALL dbms.components() YIELD name, versions RETURN name, versions"
    payload = json.dumps({"statements": [{"statement": statement}]}).encode()
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}/db/nornic/tx/commit",
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        decoded = json.load(response)
    if decoded.get("errors") or len(decoded.get("results", [])) != 1:
        raise ValueError("graph component query failed")
    result = decoded["results"][0]
    if result.get("columns") != ["name", "versions"] or len(result.get("data", [])) != 1:
        raise ValueError("graph component response malformed")
    row = result["data"][0].get("row")
    if (not isinstance(row, list) or len(row) != 2 or
            not isinstance(row[1], list) or len(row[1]) != 1):
        raise ValueError("graph component row malformed")
    return {
        "component": required(row[0], "graph component"),
        "version": required(row[1][0], "graph version"),
    }


def postgres_version(container: str) -> str:
    """Observe the disposable PostgreSQL server version inside its container."""
    result = subprocess.run(
        ["docker", "exec", container, "psql", "-At", "-U", "eshu",
         "-d", "postgres", "-c", "SHOW server_version"],
        check=True,
        capture_output=True,
        text=True,
    )
    return required(result.stdout, "PostgreSQL version")


def main() -> int:
    """Write one validated manifest to the proof artifact directory."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--primary", required=True)
    parser.add_argument("--mismatch", required=True)
    parser.add_argument("--postgres", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--local-proof", action="store_true")
    args = parser.parse_args()
    primary = docker_identity(args.primary, f"{NORNIC_REPOSITORY}@{NORNIC_DIGEST}")
    mismatch = docker_identity(args.mismatch, f"{NORNIC_REPOSITORY}@{NORNIC_DIGEST}")
    if args.local_proof and os.environ.get("GITHUB_ACTIONS") == "true":
        raise ValueError("local proof mode is forbidden on GitHub Actions")
    primary_port = int(primary["http_host_port"]) if args.local_proof else 17474
    mismatch_port = int(mismatch["http_host_port"]) if args.local_proof else 18474
    primary.update(nornic_component(primary_port))
    mismatch.update(nornic_component(mismatch_port))
    postgres = docker_identity(args.postgres, f"{POSTGRES_REPOSITORY}@{POSTGRES_DIGEST}")
    postgres["version"] = postgres_version(args.postgres)
    checkout_sha = subprocess.run(
        ["git", "rev-parse", "HEAD"], check=True, capture_output=True, text=True, cwd=ROOT
    ).stdout.strip()
    inventory_bytes = (ROOT / "scripts/lib/ifa_scheduled_inventory.json").read_bytes()
    if args.local_proof:
        manifest = build_local_manifest(checkout_sha, inventory_bytes, primary, mismatch, postgres)
    else:
        event_path = Path(required(os.environ.get("GITHUB_EVENT_PATH"), "GitHub event payload"))
        event = json.loads(event_path.read_text(encoding="utf-8"))
        manifest = build_manifest(
            os.environ, event, checkout_sha, inventory_bytes, primary, mismatch, postgres
        )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"wrote Ifá proof manifest: {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
