#!/usr/bin/env python3
"""Verify .github/leaf-go-core-filter.yml covers every go-core registry trigger.

Fails closed if:
1. Any trigger of a registry gate with `ci.workflow == test.yml` and
   `ci.job == "go-core"` (or `.github/actions/**` scanned by
   `test-ci-go-install-retry.sh`) is not covered by `.github/leaf-go-core-filter.yml`,
2. `.github/leaf-go-core-filter.yml` carries any negation (!...) pattern,
3. `.github/workflows/test.yml` loses non-commented `go_core` filter wiring
   (including `predicate-quantifier: 'some'`), `go-core` gate, or `CORE_CHANGED`
   / `LOCKSTEP_CHANGED` wiring on `go-core-complete`, or
4. `test.yml`'s `golangcilint-vX.Y.Z-` cache key version drifts from the
   `golangci-lint@vX.Y.Z` install pin.
"""

from __future__ import annotations

import re
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent


def parse_yaml_scalar(raw: str) -> str:
    value = raw.strip()
    if not value:
        raise ValueError("empty YAML list item")
    if value[0] in ("'", '"'):
        quote = value[0]
        end = value.find(quote, 1)
        if end == -1:
            raise ValueError(f"unterminated quoted scalar: {raw!r}")
        trailing = value[end + 1 :].strip()
        if trailing and not trailing.startswith("#"):
            raise ValueError(f"unexpected trailing content in scalar: {raw!r}")
        return value[1:end]
    if "#" in value:
        value = value.split("#", 1)[0].strip()
    if not value:
        raise ValueError(f"empty unquoted scalar: {raw!r}")
    return value


def load_go_core_triggers(root: Path) -> set[str]:
    manifest = (root / "specs/ci-gates.v1.yaml").read_text(encoding="utf-8")
    fragments = re.findall(r"^  - (ci-gates\.d/[^\s#]+\.ya?ml)\b", manifest, re.M)
    if not fragments:
        raise ValueError("no ci-gates.d fragments found in specs/ci-gates.v1.yaml")
    gate_count = 0
    triggers: set[str] = set()
    for rel in fragments:
        text = (root / "specs" / rel).read_text(encoding="utf-8")
        for block in re.split(r"\n(?=  - id: )", text):
            m_wf = re.search(r"^\s+workflow:\s*([^\n]+)", block, re.M)
            m_job = re.search(r"^\s+job:\s*([^\n]+)", block, re.M)
            if not m_wf or not m_job:
                continue
            wf = parse_yaml_scalar(m_wf.group(1))
            job = parse_yaml_scalar(m_job.group(1))
            if wf != "test.yml" or job != "go-core":
                continue
            gate_count += 1
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
                        raise ValueError(f"malformed go-core trigger line: {line!r}")
                    triggers.add(parse_yaml_scalar(line[len("      - ") :]))
                    gate_trigs += 1
            if gate_trigs == 0:
                raise ValueError("go-core gate missing triggers")
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
            item = parse_yaml_scalar(re.sub(r"^\s*-\s+", "", raw))
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


def extract_job_block(workflow: str, job_name: str) -> str:
    m = re.search(rf"(?m)^  {re.escape(job_name)}:\n(.*?)(?=^  [A-Za-z0-9_-]+:|\Z)", workflow, re.S)
    if not m:
        raise ValueError(f"test.yml missing job: {job_name}")
    return m.group(1)


def extract_step_by_id(job_block: str, step_id: str) -> str:
    for step in re.split(r"(?m)(?=^      - )", job_block):
        if re.search(rf"(?m)^        id:\s*{re.escape(step_id)}\s*$", step):
            return step
    raise ValueError(f"job missing step with id: {step_id}")


def non_comment_lines(text: str) -> list[str]:
    return [ln for ln in text.splitlines() if ln.strip() and not ln.lstrip().startswith("#")]


def check_repo(root: Path) -> list[str]:
    errors: list[str] = []
    try:
        triggers = load_go_core_triggers(root)
        patterns = load_filter_patterns(root / ".github/leaf-go-core-filter.yml")
    except (OSError, ValueError) as exc:
        return [str(exc)]

    required = triggers | {
        ".github/actions/**",
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
    try:
        changes_block = extract_job_block(wf, "changes")
        gc_step = extract_step_by_id(changes_block, "go_core")
        go_core_block = extract_job_block(wf, "go-core")
        complete_block = extract_job_block(wf, "go-core-complete")
    except ValueError as exc:
        return errors + [str(exc)]

    if not any("go_core: ${{ steps.go_core.outputs.go_core }}" in ln for ln in non_comment_lines(changes_block)):
        errors.append("test.yml changes job missing go_core output export")
    gc_active = "\n".join(non_comment_lines(gc_step))
    if "uses: dorny/paths-filter@v3" not in gc_active:
        errors.append("test.yml go_core step must use dorny/paths-filter@v3")
    if not re.search(r"(?m)^          predicate-quantifier:\s*(?:some|'some'|\"some\")\s*$", gc_active):
        errors.append("test.yml go_core step must set predicate-quantifier: 'some'")
    if not re.search(r"(?m)^          filters:\s*\.github/leaf-go-core-filter\.yml\s*$", gc_active):
        errors.append("test.yml go_core step must reference .github/leaf-go-core-filter.yml")

    go_core_active = "\n".join(non_comment_lines(go_core_block))
    if "needs.changes.outputs.code == 'true' && needs.changes.outputs.go_core == 'true'" not in go_core_active:
        errors.append("test.yml go-core job missing go_core if-guard")
    if "needs.changes.outputs.lockstep == 'true'" not in go_core_active:
        errors.append("test.yml go-core job missing lockstep override in if-guard")

    complete_active = "\n".join(non_comment_lines(complete_block))
    if "CORE_CHANGED: ${{ needs.changes.outputs.go_core }}" not in complete_active:
        errors.append("test.yml go-core-complete missing CORE_CHANGED env")
    if "LOCKSTEP_CHANGED: ${{ needs.changes.outputs.lockstep }}" not in complete_active:
        errors.append("test.yml go-core-complete missing LOCKSTEP_CHANGED env")

    m_pin = re.search(r"golangci-lint@(v[0-9]+\.[0-9]+\.[0-9]+)", go_core_active)
    cache_vers = set(re.findall(r"golangcilint-(v[0-9]+\.[0-9]+\.[0-9]+)-", go_core_active))
    if not m_pin:
        errors.append("test.yml go-core missing pinned golangci-lint@vX.Y.Z install")
    elif not cache_vers or cache_vers != {m_pin.group(1)}:
        errors.append(
            f"test.yml golangcilint cache key version(s) {sorted(cache_vers)} != install pin {m_pin.group(1)}"
        )

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

        wf = scratch / ".github/workflows/test.yml"
        orig_wf = wf.read_text(encoding="utf-8")

        # Seeded RED 3: mutating predicate-quantifier: 'some' -> 'every' on go_core must fail.
        mut_q = orig_wf.replace(
            "id: go_core\n        uses: dorny/paths-filter@v3\n        with:\n          predicate-quantifier: 'some'",
            "id: go_core\n        uses: dorny/paths-filter@v3\n        with:\n          predicate-quantifier: 'every'",
            1,
        )
        if mut_q == orig_wf:
            raise AssertionError("seeded RED 3 anchor did not match test.yml")
        wf.write_text(mut_q, encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 3 failed: predicate-quantifier: 'every' was accepted")

        # Seeded RED 4: commenting out CORE_CHANGED must fail.
        wf.write_text(
            orig_wf.replace(
                "CORE_CHANGED: ${{ needs.changes.outputs.go_core }}",
                "# CORE_CHANGED: ${{ needs.changes.outputs.go_core }}",
                1,
            ),
            encoding="utf-8",
        )
        if not check_repo(scratch):
            raise AssertionError("seeded RED 4 failed: commented CORE_CHANGED was accepted")

        # Seeded RED 5: cache key version drift from golangci-lint install pin must fail.
        wf.write_text(orig_wf.replace("golangcilint-v2.12.2-", "golangcilint-v2.11.0-", 1), encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 5 failed: drifted golangcilint cache key version was accepted")
        wf.write_text(orig_wf, encoding="utf-8")

        # Seeded RED 6: quoted workflow + new trigger after blank line in gates-01.yaml must fail.
        g1 = scratch / "specs/ci-gates.d/gates-01.yaml"
        orig_g1 = g1.read_text(encoding="utf-8")
        mut_g1 = orig_g1.replace("      workflow: test.yml\n", '      workflow: "test.yml"\n', 1).replace(
            '      - "go/**"\n',
            '      - "go/**"\n\n      - "new_unlisted_go_core_input/**" # comment\n',
            1,
        )
        if mut_g1 == orig_g1:
            raise AssertionError("seeded RED 6 anchor did not match gates-01.yaml")
        g1.write_text(mut_g1, encoding="utf-8")
        if not check_repo(scratch):
            raise AssertionError("seeded RED 6 failed: quoted workflow + trigger after blank line was missed")


def main() -> int:
    errors = check_repo(REPO_ROOT)
    if errors:
        for err in errors:
            print(f"test-go-core-filter-superset: FAIL: {err}", file=sys.stderr)
        return 1
    run_self_tests(REPO_ROOT)
    print("test-go-core-filter-superset: PASS (all go-core triggers covered + 6 seeded RED checks)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
