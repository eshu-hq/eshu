#!/usr/bin/env python3
"""Verify .github/workflows/ifa-determinism-gate.yml on.push.paths is a superset
of specs/ci-pr-workflows.d/ifa-determinism-gate.yml paths, with seeded RED/GREEN
checks for dropped globs, `!` negations, and narrowed extension globs."""

from __future__ import annotations

import pathlib
import re
import sys

REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
WORKFLOW_PATH = REPO_ROOT / ".github/workflows/ifa-determinism-gate.yml"
SPEC_PATH = REPO_ROOT / "specs/ci-pr-workflows.d/ifa-determinism-gate.yml"


def extract_push_paths(workflow_text: str) -> list[str]:
    m = re.search(r"^  push:\s*\n((?:    .*\n|\n)*)", workflow_text, re.M)
    if not m:
        raise ValueError("missing on.push block in workflow")
    pm = re.search(r"^    paths:\s*\n((?:(?:      (?:- .*|#.*)|\s*)\n)*)", m.group(1), re.M)
    if not pm:
        raise ValueError("missing on.push.paths list in workflow")
    paths: list[str] = []
    for raw in pm.group(1).splitlines():
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.startswith("-"):
            val = stripped.split("-", 1)[1].strip()
            # Strip inline comments outside quotes.
            if not (val.startswith("'") or val.startswith('"')) and " #" in val:
                val = val.split(" #", 1)[0].strip()
            paths.append(val.strip("'\""))
    return paths


def extract_spec_paths(spec_text: str) -> list[str]:
    before_jobs = spec_text.split("  jobs:", 1)[0]
    pm = re.search(r"^  paths:\s*\n((?:(?:  (?:- .*|#.*)|\s*)\n)*)", before_jobs, re.M)
    if not pm:
        raise ValueError("missing paths list in specs/ci-pr-workflows.d/ifa-determinism-gate.yml")
    paths: list[str] = []
    for raw in pm.group(1).splitlines():
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.startswith("-"):
            paths.append(stripped.split("-", 1)[1].strip().strip("'\""))
    return paths


def glob_to_regex(pattern: str) -> re.Pattern[str]:
    i = 0
    out = ["^"]
    n = len(pattern)
    while i < n:
        if pattern[i : i + 3] == "**/":
            out.append("(?:.*/)?")
            i += 3
        elif pattern[i : i + 2] == "**":
            out.append(".*")
            i += 2
        elif pattern[i] == "*":
            out.append("[^/]*")
            i += 1
        elif pattern[i] == "?":
            out.append("[^/]")
            i += 1
        else:
            out.append(re.escape(pattern[i]))
            i += 1
    out.append("$")
    return re.compile("".join(out))


def probe_samples(spec_path: str) -> list[str]:
    """Generate concrete probe paths covering direct non-Go and deep nested files."""
    if spec_path.startswith("**/"):
        tail = spec_path[3:]
        bases = [tail, f"nested/dir/{tail}"]
    else:
        bases = [spec_path]

    samples: list[str] = []
    for base in bases:
        if "/**" in base:
            samples.append(base.replace("/**", "/a.txt").replace("*", "sample"))
            samples.append(base.replace("/**", "/a/b/c.bin").replace("*", "sample"))
        else:
            samples.append(base.replace("*", "sample"))
    return samples


def validate_superset(push_paths: list[str], spec_paths: list[str]) -> list[str]:
    negations = [p for p in push_paths if p.startswith("!")]
    if negations:
        return [f"negation not allowed in on.push.paths: {p}" for p in negations]
    regexes = [glob_to_regex(p) for p in push_paths]
    missing: list[str] = []
    for sp in spec_paths:
        for sample in probe_samples(sp):
            if not any(rx.match(sample) for rx in regexes):
                missing.append(f"{sp} (unmatched probe: {sample})")
                break
    return missing


def main() -> int:
    push_paths = extract_push_paths(WORKFLOW_PATH.read_text(encoding="utf-8"))
    spec_paths = extract_spec_paths(SPEC_PATH.read_text(encoding="utf-8"))
    if not push_paths:
        print("FAIL: empty on.push.paths", file=sys.stderr)
        return 1
    if not spec_paths:
        print("FAIL: empty spec paths", file=sys.stderr)
        return 1

    # Seeded RED 1: dropping go/internal/ifa/** must fail loudly.
    dropped = [p for p in push_paths if p != "go/internal/ifa/**"]
    dropped_missing = validate_superset(dropped, spec_paths)
    if not dropped_missing:
        print("FAIL: seeded violation (dropped go/internal/ifa/**) was not caught", file=sys.stderr)
        return 1

    # Seeded RED 2: adding a `!` negation must fail loudly.
    negated_missing = validate_superset(push_paths + ["!go/internal/ifa/**"], spec_paths)
    if not negated_missing:
        print("FAIL: seeded violation (! negation) was not caught", file=sys.stderr)
        return 1

    # Seeded RED 3: narrowing a non-Go tree glob to *.go must fail loudly.
    narrowed = [
        "testdata/cassettes/**/*.go" if p == "testdata/cassettes/**" else p
        for p in push_paths
    ]
    narrowed_missing = validate_superset(narrowed, spec_paths)
    if not narrowed_missing:
        print("FAIL: seeded violation (narrowed testdata/cassettes/**/*.go) was not caught", file=sys.stderr)
        return 1

    # Live GREEN check: every spec path must be covered.
    missing = validate_superset(push_paths, spec_paths)
    if missing:
        print(f"FAIL: {len(missing)} spec path(s) not covered by ifa-determinism-gate.yml on.push.paths:", file=sys.stderr)
        for p in missing[:20]:
            print(f"  - {p}", file=sys.stderr)
        return 1

    print(
        f"PASS: ifa-determinism-gate.yml on.push.paths ({len(push_paths)} globs) covers "
        f"all {len(spec_paths)} spec paths (seeded checks caught drop={len(dropped_missing)}, "
        f"negation={len(negated_missing)}, narrowed={len(narrowed_missing)})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
