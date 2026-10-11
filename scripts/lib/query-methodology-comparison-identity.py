#!/usr/bin/env python3
"""Freeze the Git comparison used by the query methodology proof."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path


SHA = re.compile(r"[0-9a-f]{40}\Z")


def git(root: Path, *arguments: str) -> str:
    """Run Git in the proof checkout and preserve failures as actionable errors."""
    result = subprocess.run(
        ["git", *arguments], cwd=root, capture_output=True, text=True, check=False
    )
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise ValueError(f"git {' '.join(arguments)} failed: {detail}; fetch complete history")
    return result.stdout.strip()


def commit(root: Path, value: str, role: str) -> str:
    """Require an available, exact commit object for one identity role."""
    if not SHA.fullmatch(value):
        raise ValueError(f"{role} must be an exact 40-character commit SHA")
    resolved = git(root, "rev-parse", "--verify", value + "^{commit}")
    if resolved != value:
        raise ValueError(f"{role} does not name the exact commit {value}")
    return resolved


def event_value(payload: dict, *keys: str) -> str:
    """Read one required event field without substituting a local ref."""
    value: object = payload
    for key in keys:
        if not isinstance(value, dict) or key not in value:
            raise ValueError(f"missing GitHub event identity: {'.'.join(keys)}")
        value = value[key]
    if not isinstance(value, str):
        raise ValueError(f"GitHub event identity {'.'.join(keys)} must be a SHA string")
    return value


def unique_merge_base(root: Path, candidate: str, target: str) -> str:
    """Reject disconnected or criss-cross histories without a unique base."""
    bases = git(root, "merge-base", "--all", candidate, target).splitlines()
    if len(bases) != 1:
        raise ValueError(f"expected one merge base for {candidate} and {target}, found {len(bases)}")
    return commit(root, bases[0], "merge base")


def first_parent(root: Path, candidate: str) -> str:
    """Name the preceding snapshot of a committed checkout."""
    parents = git(root, "rev-list", "--parents", "-n", "1", candidate).split()
    if len(parents) < 2:
        raise ValueError(f"checkout HEAD {candidate} has no first parent for snapshot proof")
    return commit(root, parents[1], "first parent")


def resolve(root: Path, environment: dict[str, str]) -> dict[str, str | int]:
    """Resolve H, B, and target T once from the checkout and event contract."""
    if Path(git(root, "rev-parse", "--show-toplevel")).resolve() != root.resolve():
        raise ValueError("methodology identity root differs from Git checkout root")
    if git(root, "rev-parse", "--is-shallow-repository") != "false":
        raise ValueError("methodology identity needs full Git history; checkout with fetch-depth: 0")
    candidate = commit(root, git(root, "rev-parse", "HEAD"), "checkout HEAD")
    event = environment.get("GITHUB_EVENT_NAME", "")
    if not event:
        target = commit(root, git(root, "rev-parse", "refs/remotes/origin/main"), "local target main")
        base = unique_merge_base(root, candidate, target)
        mode = "local"
        topology = "feature_merge_base"
    else:
        event_path = environment.get("GITHUB_EVENT_PATH", "")
        if not event_path:
            raise ValueError("GITHUB_EVENT_PATH is required for methodology CI identity")
        try:
            payload = json.loads(Path(event_path).read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise ValueError(f"read GitHub event identity: {error}") from error
        if not isinstance(payload, dict):
            raise ValueError("GitHub event identity must be an object")
        if environment.get("GITHUB_SHA") != candidate:
            raise ValueError("checkout HEAD differs from GitHub event GITHUB_SHA")
        if event == "pull_request":
            target = commit(root, event_value(payload, "pull_request", "base", "sha"), "PR base")
            feature = commit(root, event_value(payload, "pull_request", "head", "sha"), "PR head")
            parents = git(root, "rev-list", "--parents", "-n", "1", candidate).split()
            if len(parents) != 3 or set(parents[1:]) != {target, feature}:
                raise ValueError("checkout HEAD is not the event PR combined merge commit")
            base = target
            topology = "event_combined"
        elif event == "merge_group":
            target = commit(root, event_value(payload, "merge_group", "base_sha"), "merge-group base")
            if event_value(payload, "merge_group", "head_sha") != candidate:
                raise ValueError("checkout HEAD differs from merge-group combined head")
            base = target
            topology = "event_combined"
        elif event == "push":
            if event_value(payload, "ref") != "refs/heads/main":
                raise ValueError("methodology push identity requires refs/heads/main")
            target = commit(root, event_value(payload, "before"), "push before")
            after = payload.get("after")
            if after is not None and after != candidate:
                raise ValueError("checkout HEAD differs from push after")
            base = target
            topology = "event_push_previous"
        elif event in ("schedule", "workflow_dispatch"):
            ref = environment.get("GITHUB_REF", "")
            if ref == "refs/heads/main":
                target = candidate
                base = first_parent(root, candidate)
                topology = "previous_main_snapshot"
            elif event == "workflow_dispatch" and ref.startswith("refs/heads/") and len(ref) > len("refs/heads/"):
                target = commit(root, git(root, "rev-parse", "refs/remotes/origin/main"), "manual target main")
                common = unique_merge_base(root, candidate, target)
                if common == candidate:
                    base = first_parent(root, candidate)
                    topology = "already_integrated_snapshot"
                else:
                    base = common
                    topology = "feature_merge_base"
            else:
                raise ValueError(f"unsupported {event} checkout ref: {ref}")
        else:
            raise ValueError(f"unsupported methodology CI event: {event}")
        if event in ("pull_request", "merge_group", "push") and unique_merge_base(root, candidate, target) != target:
            raise ValueError("event target is not an ancestor of combined checkout HEAD")
        mode = event
    if base == candidate:
        raise ValueError("methodology comparison base equals candidate; no paired revision exists")
    git(root, "merge-base", "--is-ancestor", base, candidate)
    return {"version": 1, "event": mode, "mode": topology, "target": target, "base": base, "candidate": candidate}


def main() -> int:
    """Write a single immutable JSON identity after all validation succeeds."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    arguments = parser.parse_args()
    arguments.output.unlink(missing_ok=True)
    try:
        identity = resolve(arguments.root, os.environ)
        arguments.output.parent.mkdir(parents=True, exist_ok=True)
        temporary = arguments.output.with_suffix(arguments.output.suffix + ".tmp")
        temporary.write_text(json.dumps(identity, indent=2, sort_keys=True) + "\n")
        temporary.replace(arguments.output)
    except (OSError, ValueError) as error:
        print(f"methodology comparison identity: {error}", file=sys.stderr)
        return 1
    print(f"methodology comparison identity: {arguments.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
