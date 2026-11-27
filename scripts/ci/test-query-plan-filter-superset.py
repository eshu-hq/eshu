#!/usr/bin/env python3
"""Verify .github/leaf-query-plan-filter.yml covers all query-plan registry triggers.

Fails closed if:
1. Any trigger of `query-plan-regression` or `query-methodology-static` in
   specs/ci-gates.d/ is missing from .github/leaf-query-plan-filter.yml,
2. .github/leaf-query-plan-filter.yml carries any negation (!...) pattern,
3. .github/workflows/test.yml's `changes` `id: query_plan` step loses
   `predicate-quantifier: 'some'` or its filter reference, or
4. .github/actions/ci-verify-contracts/action.yml loses `query_plan`
   `default: 'true'` or its `inputs.query_plan == 'true'` guard.
"""

from __future__ import annotations

import re
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
TARGET_GATES = ("query-methodology-static", "query-plan-regression")


def parse_yaml_scalar(raw: str) -> str:
    value = raw.strip()
    if not value:
        raise ValueError("empty trigger item")
    if value[0] in ("'", '"'):
        quote = value[0]
        end = value.find(quote, 1)
        if end == -1:
            raise ValueError(f"unterminated quoted scalar: {raw!r}")
        trailing = value[end + 1 :].strip()
        if trailing and not trailing.startswith("#"):
            raise ValueError(f"unexpected trailing content in trigger: {raw!r}")
        return value[1:end]
    if "#" in value:
        value = value.split("#", 1)[0].strip()
    if not value:
        raise ValueError(f"empty unquoted scalar: {raw!r}")
    return value


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
            in_trigs = False
            gate_trigs = 0
            for line in block.splitlines():
                if re.match(r"^    triggers:\s*$", line):
                    in_trigs = True
                    continue
                if in_trigs:
                    if re.match(r"^    [A-Za-z0-9_-]+:", line):
                        break
                    stripped = line.strip()
                    if not stripped or stripped.startswith("#"):
                        continue
                    if not line.startswith("      - "):
                        raise ValueError(f"malformed trigger line in {m_id.group(1)}: {line!r}")
                    triggers.add(parse_yaml_scalar(line[len("      - ") :]))
                    gate_trigs += 1
            if gate_trigs == 0:
                raise ValueError(f"gate {m_id.group(1)} has no triggers")
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
            item = parse_yaml_scalar(re.sub(r"^\s*-\s+", "", raw))
            if item.startswith("!"):
                raise ValueError(f"negation pattern not allowed in query_plan filter: {item}")
            patterns.append(item)
    if not patterns:
        raise ValueError(f"no query_plan patterns found in {filter_path}")
    return patterns


def extract_job_block(workflow: str, job_name: str) -> str:
    m = re.search(rf"(?m)^  {re.escape(job_name)}:\n(.*?)(?=^  [A-Za-z0-9_-]+:|\Z)", workflow, re.S)
    if not m:
        raise ValueError(f"test.yml missing job: {job_name}")
    return m.group(1)


def extract_step_by_id(job_block: str, step_id: str) -> str:
    steps = re.split(r"(?m)(?=^      - )", job_block)
    for step in steps:
        if re.search(rf"(?m)^        id:\s*{re.escape(step_id)}\s*$", step):
            return step
    raise ValueError(f"changes job missing step with id: {step_id}")


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
    try:
        changes_block = extract_job_block(wf, "changes")
        qp_step = extract_step_by_id(changes_block, "query_plan")
    except ValueError as exc:
        errors.append(str(exc))
        changes_block = ""
        qp_step = ""

    if changes_block and "query_plan: ${{ steps.query_plan.outputs.query_plan }}" not in changes_block:
        errors.append("test.yml changes job missing query_plan output export")
    if qp_step:
        if not re.search(r"(?m)^          predicate-quantifier:\s*(?:some|'some'|\"some\")\s*$", qp_step):
            errors.append("test.yml query_plan step must set predicate-quantifier: 'some'")
        if not re.search(r"(?m)^          filters:\s*\.github/leaf-query-plan-filter\.yml\s*$", qp_step):
            errors.append("test.yml query_plan step must reference .github/leaf-query-plan-filter.yml")

    try:
        vc_block = extract_job_block(wf, "verify-contracts")
    except ValueError as exc:
        errors.append(str(exc))
        vc_block = ""
    if (
        vc_block
        and "query_plan: ${{ github.event_name != 'pull_request' || needs.changes.outputs.query_plan == 'true' }}"
        not in vc_block
    ):
        errors.append("test.yml verify-contracts job missing fail-closed query_plan input wiring")

    action = (root / ".github/actions/ci-verify-contracts/action.yml").read_text(encoding="utf-8")
    m_input = re.search(r"(?m)^  query_plan:\n(.*?)(?=^  [A-Za-z0-9_-]+:|^runs:|\Z)", action, re.S)
    if not m_input or not re.search(r"(?m)^    default:\s*(?:'true'|\"true\")\s*$", m_input.group(1)):
        errors.append("ci-verify-contracts/action.yml query_plan input must default to 'true'")
    m_gate = re.search(
        r"(?m)^    - name: Verify query-plan regression gate\n(.*?)(?=^    - name:|\Z)",
        action,
        re.S,
    )
    if not m_gate or "inputs.query_plan == 'true'" not in m_gate.group(1):
        errors.append("ci-verify-contracts/action.yml regression gate missing inputs.query_plan == 'true'")

    return errors


def run_self_tests(root: Path) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        scratch = Path(tmp)
        for rel in (
            "specs/ci-gates.v1.yaml",
            ".github/leaf-query-plan-filter.yml",
            ".github/workflows/test.yml",
            ".github/actions/ci-verify-contracts/action.yml",
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

        flt = scratch / ".github/leaf-query-plan-filter.yml"
        orig_flt = flt.read_text(encoding="utf-8")

        # Seeded RED 1: dropping a trigger from leaf-query-plan-filter.yml must fail.
        flt.write_text(orig_flt.replace("  - 'go/internal/query/**'\n", ""), encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 1 failed: dropped trigger was accepted")

        # Seeded RED 2: adding a negation pattern must fail.
        flt.write_text(orig_flt + "  - '!go/internal/query/**'\n", encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 2 failed: negation pattern was accepted")
        flt.write_text(orig_flt, encoding="utf-8")

        # Seeded RED 3: mutating predicate-quantifier: 'some' -> 'every' on query_plan must fail.
        wf = scratch / ".github/workflows/test.yml"
        orig_wf = wf.read_text(encoding="utf-8")
        mutated_wf = orig_wf.replace(
            "id: query_plan\n        uses: dorny/paths-filter@v3\n        with:\n          predicate-quantifier: 'some'",
            "id: query_plan\n        uses: dorny/paths-filter@v3\n        with:\n          predicate-quantifier: 'every'",
            1,
        )
        if mutated_wf == orig_wf:
            raise AssertionError("seeded RED 3 anchor did not match test.yml")
        wf.write_text(mutated_wf, encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 3 failed: predicate-quantifier: 'every' was accepted")
        wf.write_text(orig_wf, encoding="utf-8")

        # Seeded RED 4: adding a commented trigger in gates-04.yaml without updating filter must fail.
        g4 = scratch / "specs/ci-gates.d/gates-04.yaml"
        orig_g4 = g4.read_text(encoding="utf-8")
        g4.write_text(
            orig_g4.replace(
                '      - "go/internal/query/**"\n',
                '      - "go/internal/query/**"\n\n      - "go/internal/new_query_surface/**" # inline note\n',
                1,
            ),
            encoding="utf-8",
        )
        if not check_repo(scratch):
            raise AssertionError("seeded RED 4 failed: new commented trigger after blank line was missed")
        g4.write_text(orig_g4, encoding="utf-8")

        # Seeded RED 5: changing action.yml query_plan default to 'false' must fail.
        act = scratch / ".github/actions/ci-verify-contracts/action.yml"
        orig_act = act.read_text(encoding="utf-8")
        act.write_text(orig_act.replace("default: 'true'", "default: 'false'", 1), encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 5 failed: default: 'false' in action.yml was accepted")


def main() -> int:
    errors = check_repo(REPO_ROOT)
    if errors:
        for err in errors:
            print(f"test-query-plan-filter-superset: FAIL: {err}", file=sys.stderr)
        return 1
    run_self_tests(REPO_ROOT)
    print("test-query-plan-filter-superset: PASS (all query-plan triggers covered + 5 seeded RED checks)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
