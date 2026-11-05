#!/usr/bin/env python3
"""Keep the NornicDB CI legs off until the owner re-enables them.

Owner order, 2026-11-05 (#7331): Neo4j is the supported graph backend, so no
NornicDB leg runs in CI. This gate holds two invariants over the committed
workflows and the gate registry. Neither one keeps a list of exceptions.

1. A live job (one whose ``if`` is not the literal ``${{ false }}``) starts no
   NornicDB backend. A start is a matrix cell or environment that selects the
   ``nornicdb`` backend, a compose ``up`` of the ``nornicdb`` service, or a
   ``docker run`` of a NornicDB image. A re-enabled matrix cell or a new leg
   fails here.
2. A registry row whose CI job is permanently disabled is not blocking and is
   not in a blocking tier. Otherwise the aggregate would wait on a check that
   never runs.

Re-enabling the legs is a deliberate act: follow "Re-enabling NornicDB CI" in
docs/internal/ci-workflow-inventory.md, which removes this gate last.

Usage: verify-nornicdb-ci-off.py [--root REPO_ROOT]
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

OFF_CONDITION = re.compile(r"^if:\s*\$\{\{\s*false\s*\}\}\s*$")
BLOCKING_TIERS = {"pre-commit", "pre-push", "pre-pr", "ci-heavy"}
START_MARKERS = (
    (re.compile(r"graph_backend:\s*\[?[^\n]*\bnornicdb\b"), "selects the nornicdb graph backend"),
    (re.compile(r"graph_service:\s*[\"']?nornicdb\b"), "selects the nornicdb compose service"),
    (re.compile(r"ESHU_GRAPH_BACKEND:\s*[\"']?nornicdb\b"), "sets ESHU_GRAPH_BACKEND to nornicdb"),
    (re.compile(r"--backend[ =][\"']?nornicdb\b"), "runs a runner with --backend nornicdb"),
    (re.compile(r"\bup\b[^\n]*\bnornicdb\b"), "brings up the nornicdb compose service"),
    (re.compile(r"\bdocker run\b[^\n]*nornicdb"), "docker-runs a NornicDB image"),
)


def active_lines(text: str) -> list[str]:
    """Return non-comment lines with trailing YAML comments removed."""
    lines = []
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        lines.append(re.sub(r"\s+#.*$", "", line.rstrip()))
    return lines


def workflow_jobs(text: str) -> dict[str, dict[str, object]]:
    """Split a workflow into jobs keyed by job id (2-space keys under jobs:)."""
    lines = active_lines(text)
    try:
        start = lines.index("jobs:") + 1
    except ValueError:
        return {}
    jobs: dict[str, dict[str, object]] = {}
    current: str | None = None
    for line in lines[start:]:
        if not line.startswith(" "):
            break
        key = re.fullmatch(r"  ([A-Za-z0-9_-]+):", line)
        if key:
            current = key.group(1)
            jobs[current] = {"lines": [], "name": current, "off": False}
            continue
        if current is None:
            continue
        job = jobs[current]
        job["lines"].append(line)  # type: ignore[union-attr]
        stripped = line.strip()
        if line.startswith("    ") and not line.startswith("     "):
            if OFF_CONDITION.match(stripped):
                job["off"] = True
            elif stripped.startswith("name:"):
                job["name"] = stripped[len("name:"):].strip().strip("\"'")
    return jobs


def job_matches(job_id: str, job: dict[str, object], wanted: str) -> bool:
    """Match a registry ci.job to a workflow job id, name, or name template."""
    name = str(job["name"])
    if wanted in (job_id, name):
        return True
    template = re.sub(r"\\\$\\\{\\\{.*?\\\}\\\}", ".+", re.escape(name))
    return re.fullmatch(template, wanted) is not None


def registry_rows(root: Path) -> list[dict[str, object]]:
    """Read id, tier, blocking, and ci.{workflow,job} from the gate fragments."""
    rows: list[dict[str, object]] = []
    for path in sorted((root / "specs/ci-gates.d").glob("gates-*.yaml")):
        row: dict[str, object] | None = None
        in_ci = False
        for line in active_lines(path.read_text(encoding="utf-8")):
            match = re.fullmatch(r"  - id: (\S+)", line)
            if match:
                row = {"id": match.group(1), "tier": "", "blocking": False,
                       "workflow": "", "job": ""}
                rows.append(row)
                in_ci = False
                continue
            if row is None:
                continue
            if re.fullmatch(r"    [a-z_]+:.*", line):
                in_ci = line.strip().startswith("ci:")
            top = re.fullmatch(r"    (tier|blocking): (.+)", line)
            if top:
                value = top.group(2).strip().strip("\"'")
                row[top.group(1)] = (value == "true") if top.group(1) == "blocking" else value
            nested = re.fullmatch(r"      (workflow|job): (.+)", line)
            if in_ci and nested:
                row[nested.group(1)] = nested.group(2).strip().strip("\"'")
    return rows


def check(root: Path) -> list[str]:
    """Return one message per violation of the two invariants."""
    errors: list[str] = []
    workflows: dict[str, dict[str, dict[str, object]]] = {}
    for path in sorted((root / ".github/workflows").glob("*.yml")):
        jobs = workflow_jobs(path.read_text(encoding="utf-8"))
        workflows[path.name] = jobs
        for job_id, job in jobs.items():
            if job["off"]:
                continue
            for line in job["lines"]:  # type: ignore[union-attr]
                for pattern, reason in START_MARKERS:
                    if pattern.search(line):
                        errors.append(
                            f"{path.name}/{job_id}: a live job {reason} ({line.strip()}); "
                            "NornicDB CI is off by owner order (2026-11-05)")
    for row in registry_rows(root):
        jobs = workflows.get(str(row["workflow"]), {})
        for job_id, job in jobs.items():
            if not job["off"] or not job_matches(job_id, job, str(row["job"])):
                continue
            if row["blocking"] or row["tier"] in BLOCKING_TIERS:
                errors.append(
                    f"registry row {row['id']}: CI job {row['workflow']}/{row['job']} is "
                    f"permanently disabled but the row is tier {row['tier']!r}, "
                    f"blocking={row['blocking']}; make it manual and non-blocking")
    return errors


def main() -> int:
    """Run the check against a repository root and print each violation."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=str(Path(__file__).resolve().parent.parent))
    args = parser.parse_args()
    errors = check(Path(args.root))
    for error in errors:
        print(f"verify-nornicdb-ci-off: {error}", file=sys.stderr)
    if errors:
        return 1
    print("verify-nornicdb-ci-off: no live NornicDB CI leg, and every disabled job's row is manual")
    return 0


if __name__ == "__main__":
    sys.exit(main())
