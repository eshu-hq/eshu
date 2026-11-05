#!/usr/bin/env python3
"""Base commit and base drift for the per-change weights gate (#10171).

``wt.delta_errors`` needs the drift of the commit a change starts from. This
module picks that commit and measures its drift from its own plan and weights,
so no list of grandfathered tests is kept anywhere. Every failure to find or
read the base raises ``BaseUnavailable``: the gate never falls back to the
whole-plan pool or to ``HEAD~1``, because either would pass or fail a change
for debt it did not write.
"""

from __future__ import annotations

from dataclasses import dataclass
import io
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
from typing import Mapping

import live_postgres_readiness_weights as wt

OVERRIDE_ENV = "ESHU_READINESS_DRIFT_BASE"
RESULTS_SCRIPT = "scripts/lib/live_postgres_readiness_results.py"
ARCHIVE_PATHS = ("scripts/lib", "specs/live-postgres-readiness-weights.v1.json",
                 "specs/live-postgres-readiness-weights.v2.json")
WEIGHTS_V1 = "specs/live-postgres-readiness-weights.v1.json"
LIST_TIMEOUT_SECONDS = 120


class BaseUnavailable(Exception):
    """The change's base commit, plan, or weights could not be read."""


@dataclass(frozen=True)
class Base:
    """The commit a change is measured against and how it was chosen."""

    commit: str
    how: str


def _git(repo: Path, *args: str) -> subprocess.CompletedProcess[str]:
    """Run git in ``repo`` without raising on a non-zero exit."""
    return subprocess.run(["git", "-C", str(repo), *args], text=True,
                          capture_output=True, check=False)


def _commit(repo: Path, rev: str) -> str | None:
    """Resolve ``rev`` to a commit id, or None when it names no commit here."""
    proc = _git(repo, "rev-parse", "--verify", "--quiet", f"{rev}^{{commit}}")
    return proc.stdout.strip() if proc.returncode == 0 and proc.stdout.strip() else None


def _first_parent(repo: Path) -> str | None:
    """Return HEAD's first parent when its object is present."""
    proc = _git(repo, "rev-list", "--parents", "-n", "1", "HEAD")
    fields = proc.stdout.split() if proc.returncode == 0 else []
    return _commit(repo, fields[1]) if len(fields) > 1 else None


def _is_merge_commit(repo: Path) -> bool:
    """Report whether HEAD has two parents in this clone."""
    proc = _git(repo, "rev-list", "--parents", "-n", "1", "HEAD")
    return proc.returncode == 0 and len(proc.stdout.split()) == 3


def resolve_base(repo: Path, env: Mapping[str, str]) -> Base:
    """Pick the commit this change is measured against.

    Order: the ``ESHU_READINESS_DRIFT_BASE`` override; the first parent of a
    pull-request merge ref, which is the base branch as GitHub combined it and
    does not move when the base branch does; the previous commit on a push
    event; the merge base with ``origin/main`` for a local branch.

    Raises:
        BaseUnavailable: No base can be read, for example a depth-1 checkout.
    """
    override = env.get(OVERRIDE_ENV, "")
    if override:
        commit = _commit(repo, override)
        if commit is None:
            raise BaseUnavailable(f"{OVERRIDE_ENV}={override!r} names no commit in this clone")
        return Base(commit, f"{OVERRIDE_ENV}")
    event = env.get("GITHUB_EVENT_NAME", "")
    if event == "pull_request" and _is_merge_commit(repo):
        parent = _first_parent(repo)
        if parent:
            return Base(parent, "first parent of the pull-request merge ref")
    if event == "push":
        parent = _first_parent(repo)
        if parent:
            return Base(parent, "previous commit of the push")
    if event in ("pull_request", "push"):
        # A CI checkout that lost its parents must not fall through to a
        # merge base with a ref that happens to equal HEAD: that reads as an
        # empty change and passes every PR.
        raise BaseUnavailable(
            f"the {event} checkout has no parent commit to measure this change against "
            f"(the hermetic job needs fetch-depth: 2; set {OVERRIDE_ENV} to override)")
    merge_base = _git(repo, "merge-base", "origin/main", "HEAD")
    if merge_base.returncode == 0 and merge_base.stdout.strip():
        return Base(merge_base.stdout.strip(), "merge base with origin/main")
    raise BaseUnavailable(
        "no base commit to measure this change against: the clone has neither the merge "
        "ref's parents nor origin/main (CI checkouts need fetch-depth: 2; locally run "
        f"git fetch origin main or set {OVERRIDE_ENV})")


def _extract(repo: Path, commit: str, target: Path) -> None:
    """Extract the planner inputs of ``commit`` into ``target``."""
    present = [path for path in ARCHIVE_PATHS
               if _git(repo, "cat-file", "-e", f"{commit}:{path}").returncode == 0]
    proc = subprocess.run(["git", "-C", str(repo), "archive", "--format=tar", commit,
                           *present], capture_output=True, check=False)
    if proc.returncode != 0:
        raise BaseUnavailable(f"cannot read the weights and planner at {commit}: "
                              f"{proc.stderr.decode(errors='replace').strip()}")
    with tarfile.open(fileobj=io.BytesIO(proc.stdout)) as archive:
        archive.extractall(target, filter="data")


def _plan(root: Path, commit: str) -> list[str]:
    """Run the base's own ``list-packages`` and flatten it to plan lines."""
    proc = subprocess.run([sys.executable, str(root / RESULTS_SCRIPT), "list-packages"],
                          cwd=root, text=True, capture_output=True, check=False,
                          timeout=LIST_TIMEOUT_SECONDS)
    if proc.returncode != 0:
        raise BaseUnavailable(f"list-packages failed at {commit} (exit {proc.returncode}): "
                              f"{proc.stderr.strip()}")
    runs = []
    for row in proc.stdout.splitlines():
        fields = row.split("\t")
        if len(fields) != 3:
            raise BaseUnavailable(f"malformed list-packages row at {commit}: {row!r}")
        runs.extend(f"{fields[0]}|{test}" for test in fields[2].split(" "))
    if not runs:
        raise BaseUnavailable(f"list-packages printed no tests at {commit}")
    return runs


def load_base_drift(repo: Path, commit: str) -> wt.Drift:
    """Measure the drift of ``commit``'s plan against ``commit``'s weights.

    Raises:
        BaseUnavailable: The commit lacks a readable plan or weights file.
    """
    with tempfile.TemporaryDirectory(prefix="readiness-drift-base-") as tmp:
        root = Path(tmp)
        _extract(repo, commit, root)
        weights_path = root / WEIGHTS_V1
        if not weights_path.exists():
            raise BaseUnavailable(f"{WEIGHTS_V1} is absent at {commit}: no weights to measure")
        try:
            weights = wt.load_weights(weights_path)
            return wt.drift_report(_plan(root, commit), weights)
        except ValueError as error:
            raise BaseUnavailable(f"weights or plan at {commit} are unusable: {error}")
