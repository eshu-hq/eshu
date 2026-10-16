#!/usr/bin/env python3
"""Load and checksum the closed scheduled PostgreSQL source inventory."""

import hashlib
import json
from pathlib import Path, PurePosixPath

VERSION = "postgres-scheduled-inventory/v1"
FRAGMENT_DIR = "postgres_scheduled_inventory.d"


def _fragment_paths(index_path: Path) -> list[str]:
    """Require every JSON fragment under the local directory exactly once."""
    index = json.loads(index_path.read_text(encoding="utf-8"))
    if not isinstance(index, dict) or set(index) != {"version", "fragments"}:
        raise ValueError("scheduled inventory index has unknown or missing fields")
    if index["version"] != VERSION:
        raise ValueError("scheduled inventory version changed")
    paths = index["fragments"]
    if not isinstance(paths, list) or not paths or not all(
        isinstance(path, str) for path in paths
    ):
        raise ValueError("scheduled inventory fragments must be nonempty")
    if paths != sorted(set(paths)):
        raise ValueError("scheduled inventory fragments must be sorted and unique")
    for path in paths:
        pure = PurePosixPath(path)
        if (pure.parts[:1] != (FRAGMENT_DIR,) or len(pure.parts) != 2
                or pure.suffix != ".json" or pure.name in {".", ".."}
                or str(pure) != path):
            raise ValueError(f"invalid scheduled inventory fragment path: {path}")
    directory = index_path.parent / FRAGMENT_DIR
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError("scheduled inventory fragment directory missing")
    actual = sorted(f"{FRAGMENT_DIR}/{p.name}" for p in directory.iterdir())
    if actual != paths:
        raise ValueError("scheduled inventory fragments differ from index")
    for path in paths:
        fragment = index_path.parent / path
        if fragment.is_symlink() or not fragment.is_file():
            raise ValueError(f"invalid scheduled inventory fragment: {path}")
    return paths


def load_inventory(index_path: Path) -> dict[str, dict[str, object]]:
    """Flatten exact fragments, rejecting duplicate or empty source rows."""
    inventory: dict[str, dict[str, object]] = {}
    for path in _fragment_paths(index_path):
        rows = json.loads((index_path.parent / path).read_text(encoding="utf-8"))
        if not isinstance(rows, dict) or not rows:
            raise ValueError(f"empty scheduled inventory fragment: {path}")
        for source, row in rows.items():
            if not isinstance(source, str) or source in inventory:
                raise ValueError(f"duplicate scheduled inventory source: {source}")
            if not isinstance(row, dict) or set(row) != {"group", "package", "tests"}:
                raise ValueError(f"invalid scheduled inventory row: {source}")
            inventory[source] = row
    return inventory


def inventory_digest(index_path: Path) -> str:
    """Bind the versioned index, fragment paths, and all fragment bytes."""
    paths = _fragment_paths(index_path)
    digest = hashlib.sha256()
    for path in [index_path.name, *paths]:
        data = (index_path.parent / path).read_bytes()
        digest.update(f"{path}\0{len(data)}\0".encode("utf-8"))
        digest.update(data)
    return digest.hexdigest()
