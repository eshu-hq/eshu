#!/usr/bin/env python3
"""Generate a narrow CI image overlay from effective Docker Compose JSON.

The rendered config is read only from stdin. It can contain interpolated secrets,
so this program never writes the input or includes it in diagnostics.
"""

import argparse
import hashlib
import json
import os
import pathlib
import re
import sys
from typing import Any


_PIN = re.compile(r"^ghcr\.io/[a-z0-9][a-z0-9._/-]*(?::[a-zA-Z0-9._-]+)?@sha256:[0-9a-f]{64}$")
_REQUIRED = frozenset({"postgres-alpine", "neo4j-community", "xx", "golang", "alpine"})


class OverlayError(ValueError):
    """A stack cannot safely use the approved CI mirrors."""


def _object(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise OverlayError(f"{label} must be an object")
    return value


def _approved_images(ledger: dict[str, Any]) -> tuple[dict[str, str], dict[str, str]]:
    """Validate ledger pins and return runtime and build mappings."""
    if ledger.get("version") != 1 or not isinstance(ledger.get("images"), list):
        raise OverlayError("unsupported CI image ledger")
    ids: set[str] = set()
    runtime: dict[str, str] = {}
    build_args: dict[str, str] = {}
    for raw in ledger["images"]:
        row = _object(raw, "ledger image")
        image_id = row.get("id")
        destination = row.get("destination")
        digest = row.get("digest")
        if not isinstance(image_id, str) or image_id in ids:
            raise OverlayError("invalid or duplicate ledger image id")
        ids.add(image_id)
        if not isinstance(destination, str) or not isinstance(digest, str):
            raise OverlayError(f"missing mirror pin for {image_id}")
        pin = f"{destination}@{digest}"
        if not _PIN.fullmatch(pin):
            raise OverlayError(f"invalid GHCR digest pin for {image_id}")
        if image_id in _REQUIRED and os.environ.get("ESHU_CI_IMAGES") == "1":
            env_name = row.get("env")
            if not isinstance(env_name, str) or os.environ.get(env_name) != pin:
                raise OverlayError(f"missing or mismatched CI mirror variable for {image_id}")
        sources = row.get("runtime_sources", [])
        if not isinstance(sources, list):
            raise OverlayError(f"invalid runtime sources for {image_id}")
        if image_id in {"postgres-alpine", "neo4j-community"} and not sources:
            raise OverlayError(f"missing required runtime source for {image_id}")
        for source in sources:
            if not isinstance(source, str) or not source or source in runtime:
                raise OverlayError(f"invalid or duplicate runtime source for {image_id}")
            runtime[source] = pin
        build_arg = row.get("build_arg")
        if build_arg is not None:
            if not isinstance(build_arg, str) or build_arg in build_args:
                raise OverlayError(f"invalid build arg for {image_id}")
            build_args[build_arg] = pin
    if not _REQUIRED.issubset(ids) or set(build_args) != {
        "ESHU_XX_IMAGE", "ESHU_GO_IMAGE", "ESHU_ALPINE_IMAGE"
    }:
        raise OverlayError("required CI mirror pins are missing")
    return runtime, build_args


def _is_docker_hub(image: str) -> bool:
    """Recognize implicit and explicit Docker Hub image references."""
    if image.startswith(("docker.io/", "index.docker.io/")):
        return True
    first = image.split("/", 1)[0]
    return "." not in first and ":" not in first and first != "localhost"


def build_overlay(
    config: dict[str, Any], ledger: dict[str, Any], root: pathlib.Path
) -> dict[str, Any]:
    """Map every covered runtime and canonical local build in the rendered stack."""
    runtime, build_args = _approved_images(ledger)
    services = _object(config.get("services"), "rendered services")
    if not services:
        raise OverlayError("rendered stack has no services")
    overlay: dict[str, Any] = {"services": {}}
    canonical_root = root.resolve(strict=True)
    canonical_dockerfile = (canonical_root / "Dockerfile").resolve(strict=True)
    supported_dockerfile = pathlib.Path(__file__).resolve().parents[2] / "Dockerfile"
    for name, raw in services.items():
        if not isinstance(name, str):
            raise OverlayError("invalid service name")
        service = _object(raw, "rendered service")
        changes: dict[str, Any] = {}
        image = service.get("image")
        if image is not None:
            if not isinstance(image, str) or not image:
                raise OverlayError(f"invalid image in service {name}")
            if image in runtime:
                changes["image"] = runtime[image]
            elif image in runtime.values():
                pass  # Already approved, as with an explicit pin override.
            elif _is_docker_hub(image):
                raise OverlayError(f"uncovered Docker Hub image in service {name}")
            elif not (image.startswith("ghcr.io/eshu-hq/nornicdb-") and _PIN.fullmatch(image)):
                raise OverlayError(f"unapproved image in service {name}")
        build = service.get("build")
        if build is not None:
            build = _object(build, f"build for service {name}")
            if "dockerfile_inline" in build:
                raise OverlayError(f"inline Dockerfile is not approved in service {name}")
            context = build.get("context")
            dockerfile = build.get("dockerfile", "Dockerfile")
            if not isinstance(context, str) or not isinstance(dockerfile, str):
                raise OverlayError(f"invalid build identity in service {name}")
            context_path = pathlib.Path(context).resolve()
            dockerfile_path = pathlib.Path(dockerfile)
            if not dockerfile_path.is_absolute():
                dockerfile_path = context_path / dockerfile_path
            if context_path != canonical_root or dockerfile_path.resolve() != canonical_dockerfile:
                raise OverlayError(f"unapproved build identity in service {name}")
            actual_digest = hashlib.sha256(canonical_dockerfile.read_bytes()).digest()
            supported_digest = hashlib.sha256(supported_dockerfile.read_bytes()).digest()
            if actual_digest != supported_digest:
                raise OverlayError(f"unsupported Dockerfile content in service {name}")
            existing_args = _object(build.get("args", {}), f"build args for service {name}")
            for key in build_args:
                if key in existing_args and existing_args[key] != build_args[key]:
                    raise OverlayError(f"conflicting build image argument in service {name}")
            changes["build"] = {"args": build_args}
        if changes:
            overlay["services"][name] = changes
    return overlay


def main() -> int:
    """Parse effective config and print only the safe override JSON."""
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True, type=pathlib.Path)
    parser.add_argument("--ledger", required=True, type=pathlib.Path)
    args = parser.parse_args()
    try:
        ledger = _object(json.loads(args.ledger.read_text(encoding="utf-8")), "ledger")
        config = _object(json.load(sys.stdin), "rendered config")
        overlay = build_overlay(config, ledger, args.root)
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"ci-image-compose: {error}", file=sys.stderr)
        return 1
    json.dump(overlay, sys.stdout, separators=(",", ":"))
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
