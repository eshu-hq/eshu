"""Shared fixtures for replay-tier enrollment tests."""

from __future__ import annotations

from pathlib import Path
import shutil


ROOT = Path(__file__).resolve().parents[2]


def copy_route_sources(destination: Path) -> None:
    """Copy the production route inputs for controlled mutations."""
    for name in (".github/workflows/verify-replay-tier.yml",
                 ".github/workflows/live-backend-tests.yml",
                 ".github/workflows/test.yml",
                 "specs/ci-pr-workflows.d/verify-replay-tier.yml",
                 "specs/ci-pr-workflows.d/live-backend-tests.yml",
                 "specs/ci-gates.d/gates-15.yaml",
                 "specs/ci-gates.d/gates-09.yaml", "scripts/dev/pre-pr.sh"):
        target = destination / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / name, target)
    shutil.copytree(ROOT / "specs/live-tests.d", destination / "specs/live-tests.d")
