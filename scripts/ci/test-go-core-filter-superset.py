#!/usr/bin/env python3
"""Verify .github/leaf-go-core-filter.yml covers every go-core registry trigger.

Fails closed if:
1. Any trigger of a registry gate with `ci.workflow == test.yml` and
   `ci.job == "go-core"` is not covered by .github/leaf-go-core-filter.yml,
2. .github/leaf-go-core-filter.yml carries any negation (!...) pattern, or
3. .github/workflows/test.yml stops wiring `go_core` into `changes`, `go-core`,
   and `go-core-complete`.
"""

from __future__ import annotations

import re
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent


def load_go_core_triggers(root: Path) -> set[str]:
    manifest = (root / "specs/ci-gates.v1.yaml").read_text(encoding="utf-8")
    fragments = re.findall(r"^  - (ci-gates\.d/gates-[0-9]+\.yaml)$", manifest, re.M)
    if not fragments:
        raise ValueError("no ci-gates.d fragments found in specs/ci-gates.v1.yaml")
    gate_count = 0
    triggers: set[str] = set()
    for rel in fragments:
        text = (root / "specs" / rel).read_text(encoding="utf-8")
        for block in re.split(r"\n(?=  - id: )", text):
            m_wf = re.search(r"^\s+workflow:\s*(\S+)", block, re.M)
            m_job = re.search(r'^\s+job:\s*"?([^\n"]+)"?', block, re.M)
            if not m_wf or not m_job:
                continue
            if m_wf.group(1).strip() != "test.yml" or m_job.group(1).strip() != "go-core":
                continue
            gate_count += 1
            m_trigs = re.search(r"^    triggers:\n((?:      (?:- |#).*\n)+)", block, re.M)
            if not m_trigs:
                raise ValueError("go-core gate missing triggers block")
            for item in re.findall(r'^      - "?([^\n"]+)"?$', m_trigs.group(1), re.M):
                triggers.add(item.strip().strip("'\""))
    if gate_count == 0:
        raise ValueError("no test.yml go-core gates found in specs/ci-gates.d")
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
            in_key = stripped == "go_core:"
            continue
        if in_key and re.match(r"^\s*-\s+", raw):
            item = re.sub(r"^\s*-\s+", "", raw).strip()
            if "#" in item and not (item.startswith("'") or item.startswith('"')):
                item = item.split("#", 1)[0].strip()
            item = item.strip("'\"")
            if item.startswith("!"):
                raise ValueError(f"negation pattern not allowed in go_core filter: {item}")
            patterns.append(item)
    if not patterns:
        raise ValueError(f"no go_core patterns found in {filter_path}")
    return patterns


def pattern_covers_trigger(pattern: str, trigger: str) -> bool:
    if pattern == trigger:
        return True
    if pattern.endswith("/**"):
        prefix = pattern[:-3]
        return trigger == prefix or trigger.startswith(prefix + "/")
    return False


def check_repo(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        triggers = load_go_core_triggers(root)
        patterns = load_filter_patterns(root / ".github/leaf-go-core-filter.yml")
    except (OSError, ValueError) as exc:
        return [str(exc)]

    required = triggers | {
        ".github/leaf-go-core-filter.yml",
        "scripts/ci/assert-go-complete.sh",
        "scripts/ci/test-assert-go-complete.sh",
        "scripts/ci/test-go-core-filter-superset.py",
    }
    missing = sorted(
        t for t in required if not any(pattern_covers_trigger(p, t) for p in patterns)
    )
    if missing:
        errors.append(
            "leaf-go-core-filter.yml does not cover required go-core trigger(s): "
            + ", ".join(missing)
        )

    wf = (root / ".github/workflows/test.yml").read_text(encoding="utf-8")
    for needle in (
        "go_core: ${{ steps.go_core.outputs.go_core }}",
        "filters: .github/leaf-go-core-filter.yml",
        "needs.changes.outputs.code == 'true' && needs.changes.outputs.go_core == 'true'",
        "CORE_CHANGED: ${{ needs.changes.outputs.go_core }}",
    ):
        if needle not in wf:
            errors.append(f"test.yml missing required go_core wiring: {needle}")

    return errors


def run_self_tests(root: Path) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        scratch = Path(tmp)
        for rel in (
            "specs/ci-gates.v1.yaml",
            ".github/leaf-go-core-filter.yml",
            ".github/workflows/test.yml",
        ):
            dest = scratch / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text((root / rel).read_text(encoding="utf-8"), encoding="utf-8")
        for frag in (root / "specs/ci-gates.d").glob("gates-*.yaml"):
            dest = scratch / "specs/ci-gates.d" / frag.name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text(frag.read_text(encoding="utf-8"), encoding="utf-8")

        clean_errors = check_repo(scratch)
        if clean_errors:
            raise AssertionError(f"clean scratch failed: {clean_errors}")

        flt = scratch / ".github/leaf-go-core-filter.yml"
        orig_flt = flt.read_text(encoding="utf-8")

        # Seeded RED 1: dropping a required trigger must fail.
        flt.write_text(orig_flt.replace("  - 'go/**'\n", ""), encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 1 failed: dropped go/** trigger was accepted")

        # Seeded RED 2: adding a negation pattern must fail.
        flt.write_text(orig_flt + "  - '!go/**'\n", encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 2 failed: negation pattern was accepted")
        flt.write_text(orig_flt, encoding="utf-8")

        # Seeded RED 3: disconnecting test.yml wiring must fail.
        wf = scratch / ".github/workflows/test.yml"
        orig_wf = wf.read_text(encoding="utf-8")
        wf.write_text(
            orig_wf.replace("CORE_CHANGED: ${{ needs.changes.outputs.go_core }}", "# removed"),
            encoding="utf-8",
        )
        if not check_repo(scratch):
            raise AssertionError("seeded RED 3 failed: missing CORE_CHANGED wiring was accepted")


def main() -> int:
    errors = check_repo(REPO_ROOT)
    if errors:
        for err in errors:
            print(f"test-go-core-filter-superset: FAIL: {err}", file=sys.stderr)
        return 1
    run_self_tests(REPO_ROOT)
    print("test-go-core-filter-superset: PASS (all go-core triggers covered + 3 seeded RED checks)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
