#!/usr/bin/env python3
"""Verify .github/leaf-query-plan-filter.yml covers all query-plan registry triggers.

Fails closed if:
1. Any trigger of `query-plan-regression` or `query-methodology-static` in
   specs/ci-gates.d/ is missing from .github/leaf-query-plan-filter.yml,
2. .github/leaf-query-plan-filter.yml carries any negation (!...) pattern, or
3. .github/workflows/test.yml stops wiring the filter into verify-contracts.
"""

from __future__ import annotations

import re
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
TARGET_GATES = ("query-methodology-static", "query-plan-regression")


def load_registry_triggers(root: Path) -> set[str]:
    manifest = (root / "specs/ci-gates.v1.yaml").read_text(encoding="utf-8")
    fragments = re.findall(r"^  - (ci-gates\.d/gates-[0-9]+\.yaml)$", manifest, re.M)
    if not fragments:
        raise ValueError("no ci-gates.d fragments found in specs/ci-gates.v1.yaml")
    found_gates: set[str] = set()
    triggers: set[str] = set()
    for rel in fragments:
        text = (root / "specs" / rel).read_text(encoding="utf-8")
        for block in re.split(r"\n(?=  - id: )", text):
            m_id = re.search(r"^  - id:\s*(\S+)", block, re.M)
            if not m_id or m_id.group(1) not in TARGET_GATES:
                continue
            found_gates.add(m_id.group(1))
            m_trigs = re.search(r"^    triggers:\n((?:      (?:- |#).*\n)+)", block, re.M)
            if not m_trigs:
                raise ValueError(f"gate {m_id.group(1)} has no triggers block")
            for item in re.findall(r'^      - "?([^\n"]+)"?$', m_trigs.group(1), re.M):
                triggers.add(item.strip().strip("'\""))
    missing_gates = set(TARGET_GATES) - found_gates
    if missing_gates:
        raise ValueError(f"missing expected registry gates: {sorted(missing_gates)}")
    return triggers


def load_filter_patterns(filter_path: Path) -> list[str]:
    lines = filter_path.read_text(encoding="utf-8").splitlines()
    in_key = False
    patterns: list[str] = []
    for raw in lines:
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if not raw.startswith(" ") and raw.endswith(":"):
            in_key = stripped == "query_plan:"
            continue
        if in_key and re.match(r"^\s*-\s+", raw):
            item = re.sub(r"^\s*-\s+", "", raw).strip()
            if "#" in item and not (item.startswith("'") or item.startswith('"')):
                item = item.split("#", 1)[0].strip()
            item = item.strip("'\"")
            if item.startswith("!"):
                raise ValueError(f"negation pattern not allowed in query_plan filter: {item}")
            patterns.append(item)
    if not patterns:
        raise ValueError(f"no query_plan patterns found in {filter_path}")
    return patterns


def check_repo(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        triggers = load_registry_triggers(root)
        patterns = set(load_filter_patterns(root / ".github/leaf-query-plan-filter.yml"))
    except (OSError, ValueError) as exc:
        return [str(exc)]

    required = triggers | {
        ".github/actions/ci-verify-contracts/action.yml",
        ".github/leaf-query-plan-filter.yml",
        "scripts/ci/test-query-plan-filter-superset.py",
    }
    missing = sorted(required - patterns)
    if missing:
        errors.append(
            "leaf-query-plan-filter.yml is missing required trigger(s): " + ", ".join(missing)
        )

    wf = (root / ".github/workflows/test.yml").read_text(encoding="utf-8")
    for needle in (
        "query_plan: ${{ steps.query_plan.outputs.query_plan }}",
        "filters: .github/leaf-query-plan-filter.yml",
        "query_plan: ${{ github.event_name != 'pull_request' || needs.changes.outputs.query_plan == 'true' }}",
    ):
        if needle not in wf:
            errors.append(f"test.yml missing required query_plan wiring: {needle}")

    return errors


def run_self_tests(root: Path) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        scratch = Path(tmp)
        for rel in (
            "specs/ci-gates.v1.yaml",
            ".github/leaf-query-plan-filter.yml",
            ".github/workflows/test.yml",
        ):
            dest = scratch / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text((root / rel).read_text(encoding="utf-8"), encoding="utf-8")
        for frag in (root / "specs/ci-gates.d").glob("gates-*.yaml"):
            dest = scratch / "specs/ci-gates.d" / frag.name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text(frag.read_text(encoding="utf-8"), encoding="utf-8")

        # Clean copy must pass.
        clean_errors = check_repo(scratch)
        if clean_errors:
            raise AssertionError(f"clean scratch failed: {clean_errors}")

        # Seeded RED 1: dropping a trigger from leaf-query-plan-filter.yml must fail.
        flt = scratch / ".github/leaf-query-plan-filter.yml"
        orig_flt = flt.read_text(encoding="utf-8")
        flt.write_text(orig_flt.replace("  - 'go/internal/query/**'\n", ""), encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 1 failed: dropped trigger was accepted")

        # Seeded RED 2: adding a negation pattern must fail.
        flt.write_text(orig_flt + "  - '!go/internal/query/**'\n", encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 2 failed: negation pattern was accepted")
        flt.write_text(orig_flt, encoding="utf-8")

        # Seeded RED 3: disconnecting test.yml wiring must fail.
        wf = scratch / ".github/workflows/test.yml"
        orig_wf = wf.read_text(encoding="utf-8")
        wf.write_text(
            orig_wf.replace("filters: .github/leaf-query-plan-filter.yml", "filters: .github/other.yml"),
            encoding="utf-8",
        )
        if not check_repo(scratch):
            raise AssertionError("seeded RED 3 failed: disconnected test.yml wiring was accepted")


def main() -> int:
    errors = check_repo(REPO_ROOT)
    if errors:
        for err in errors:
            print(f"test-query-plan-filter-superset: FAIL: {err}", file=sys.stderr)
        return 1
    run_self_tests(REPO_ROOT)
    print("test-query-plan-filter-superset: PASS (all query-plan triggers covered + 3 seeded RED checks)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
