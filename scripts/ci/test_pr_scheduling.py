#!/usr/bin/env python3
"""Check that the central PR caller owns every migrated synchronize leaf."""

from __future__ import annotations

import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / ".github" / "workflows"
FRAGMENTS = ROOT / "specs" / "ci-pr-workflows.d"


def check_leaf(name: str, source: str) -> None:
    """Reject a direct PR listener or an unguarded callable payload job."""
    if re.search(r"^  pull_request:\s*$", source, re.MULTILINE):
        raise ValueError(f"{name}: direct PR trigger remains")
    if "  workflow_call:\n" not in source:
        raise ValueError(f"{name}: workflow_call missing")
    for input_name in ("plan_jobs", "plan_gates", "plan_mode"):
        if f"      {input_name}:\n" not in source:
            raise ValueError(f"{name}: {input_name} input missing")
    if "contains(fromJSON(inputs.plan_jobs" not in source:
        raise ValueError(f"{name}: selected-job guard missing")
    if name != "postgres-scheduled-live.yml":
        group = f"ci-leaf-{name.removesuffix('.yml')}-"
        if group not in source:
            raise ValueError(f"{name}: distinct concurrency group missing")


def main() -> None:
    """Verify the caller-to-leaf inventory and a seeded duplicate trigger."""
    fragments = sorted(FRAGMENTS.glob("*.yml"))
    if len(fragments) != 28:
        raise ValueError(f"expected 28 migrated leaves, got {len(fragments)}")
    central = (WORKFLOWS / "test.yml").read_text(encoding="utf-8")
    if "  pr-plan:\n" not in central or "ci-gates-bin\" pr-plan" not in central:
        raise ValueError("Build Test has no trusted PR planner")
    for fragment in fragments:
        name = fragment.name
        source = (WORKFLOWS / name).read_text(encoding="utf-8")
        check_leaf(name, source)
        caller = f"call-{fragment.stem}:"
        if len(re.findall(r"^  " + re.escape(caller), central, re.MULTILINE)) != 1:
            raise ValueError(f"{name}: expected one central call job")
        if f"uses: ./.github/workflows/{name}" not in central:
            raise ValueError(f"{name}: caller does not call its leaf")
    if len(re.findall(r"^  call-[a-z0-9-]+:", central, re.MULTILINE)) != len(fragments):
        raise ValueError("central caller has an unregistered call job")
    first = fragments[0].name
    source = (WORKFLOWS / first).read_text(encoding="utf-8")
    try:
        check_leaf(first, source.replace("  workflow_call:\n", "  pull_request:\n  workflow_call:\n", 1))
    except ValueError:
        pass
    else:
        raise ValueError("seeded duplicate PR trigger was accepted")
    print("PR scheduling wiring PASS")


if __name__ == "__main__":
    main()
