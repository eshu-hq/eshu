#!/usr/bin/env python3
"""Hash Compose inputs so a prepared CI overlay cannot outlive them."""

import hashlib
import os
import pathlib
import re
import sys


_CI_IMAGE_ENV = frozenset({
    "ESHU_CI_IMAGES", "POSTGRES_IMAGE", "NEO4J_IMAGE", "ESHU_XX_IMAGE",
    "ESHU_GO_IMAGE", "ESHU_ALPINE_IMAGE", "NORNICDB_IMAGE",
})


def signature(helper_root: pathlib.Path, root: pathlib.Path,
              arguments: list[str]) -> str:
    """Return a digest of the original stack, image pins, and helper code."""
    hasher = hashlib.sha256()
    hasher.update(repr((str(root), arguments)).encode())
    files = set(root.glob("docker-compose*.yaml")) | set(root.glob("docker-compose*.yml"))
    files.update({
        root / "Dockerfile", root / ".env", helper_root / "specs/ci-images.v1.json",
    })
    for index, argument in enumerate(arguments[:-1]):
        if argument in {"-f", "--file", "--env-file"}:
            files.add(pathlib.Path(arguments[index + 1]).resolve())
    for argument in arguments:
        if argument.startswith(("--file=", "--env-file=")):
            files.add(pathlib.Path(argument.split("=", 1)[1]).resolve())
    for path in sorted(files):
        hasher.update(str(path).encode())
        if path.is_file():
            content = path.read_bytes()
            hasher.update(content)
            for name in re.findall(rb"\$(?:\{)?([A-Za-z_][A-Za-z0-9_]*)", content):
                key = name.decode()
                hasher.update(repr((key, os.environ.get(key))).encode())
        else:
            hasher.update(b"<missing>")
    for path in (
        helper_root / "scripts/lib/ci-image-compose.sh",
        helper_root / "scripts/lib/ci_image_compose.py",
        helper_root / "scripts/lib/ci_image_compose_signature.py",
    ):
        hasher.update(str(path).encode())
        hasher.update(path.read_bytes())
    for name, value in sorted(os.environ.items()):
        if name.startswith(("COMPOSE_", "DOCKER_")) or name in _CI_IMAGE_ENV:
            hasher.update(repr((name, value)).encode())
    return hasher.hexdigest()


def main() -> None:
    """Print the signature for a stack root and ordered Compose arguments."""
    helper_root = pathlib.Path(sys.argv[1]).resolve()
    root = pathlib.Path(sys.argv[2]).resolve()
    print(signature(helper_root, root, sys.argv[3:]))


if __name__ == "__main__":
    main()
