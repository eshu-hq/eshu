#!/usr/bin/env python3
"""Verify .github/workflows/ifa-determinism-gate.yml on.push.paths is a superset
of specs/ci-pr-workflows.d/ifa-determinism-gate.yml paths, with a seeded RED/GREEN
check."""

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
    pm = re.search(r"^    paths:\s*\n((?:      - .*\n)*)", m.group(1), re.M)
    if not pm:
        raise ValueError("missing on.push.paths list in workflow")
    return [
        line.split("-", 1)[1].strip().strip("'\"")
        for line in pm.group(1).splitlines()
        if line.strip().startswith("-")
    ]


def extract_spec_paths(spec_text: str) -> list[str]:
    before_jobs = spec_text.split("  jobs:", 1)[0]
    pm = re.search(r"^  paths:\s*\n((?:  - .*\n)*)", before_jobs, re.M)
    if not pm:
        raise ValueError("missing paths list in specs/ci-pr-workflows.d/ifa-determinism-gate.yml")
    return [
        line.split("-", 1)[1].strip().strip("'\"")
        for line in pm.group(1).splitlines()
        if line.strip().startswith("-")
    ]


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


def uncovered_spec_paths(push_paths: list[str], spec_paths: list[str]) -> list[str]:
    regexes = [glob_to_regex(p) for p in push_paths]
    missing: list[str] = []
    for sp in spec_paths:
        sample = sp.replace("/**", "/sample/file.go").replace("*", "sample")
        if not any(rx.match(sample) for rx in regexes):
            missing.append(sp)
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

    # Seeded RED check: dropping go/internal/ifa/** must fail loudly.
    seeded = [p for p in push_paths if p != "go/internal/ifa/**"]
    seeded_missing = uncovered_spec_paths(seeded, spec_paths)
    if not seeded_missing:
        print("FAIL: seeded violation (dropped go/internal/ifa/**) was not caught", file=sys.stderr)
        return 1

    # Live GREEN check: every spec path must be covered.
    missing = uncovered_spec_paths(push_paths, spec_paths)
    if missing:
        print(f"FAIL: {len(missing)} spec path(s) not covered by ifa-determinism-gate.yml on.push.paths:", file=sys.stderr)
        for p in missing[:20]:
            print(f"  - {p}", file=sys.stderr)
        return 1

    print(
        f"PASS: ifa-determinism-gate.yml on.push.paths ({len(push_paths)} globs) covers "
        f"all {len(spec_paths)} spec paths (seeded drop caught {len(seeded_missing)} paths)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
