#!/usr/bin/env python3
"""Keep the NornicDB CI legs off until the owner re-enables them.

Owner order, 2026-11-05 (#7331): Neo4j is the supported graph backend, so no
NornicDB leg runs in CI. This gate holds two invariants over the committed
workflows and the gate registry. Neither one keeps a list of exceptions.

1. A live job (one whose ``if`` is not the literal ``${{ false }}``) starts no
   NornicDB backend. A start is found five ways, all read from the committed
   files; the scanners live in ``scripts/lib/nornicdb_ci_off_scan.py``:
   (a) a workflow line that selects ``nornicdb`` (a matrix value under any
   key, an environment value, ``--backend nornicdb``), assigns
   ``ESHU_GRAPH_BACKEND=nornicdb`` inside a command, brings up the ``nornicdb``
   compose service, or ``docker run``s a NornicDB image, read after ``\\``
   continuation lines are joined and after a shell variable set to a literal
   earlier in the same step is substituted; (b) the same start markers, and an
   ``export ESHU_GRAPH_BACKEND=nornicdb``, inside a ``scripts/*.sh`` or ``*.py``
   file the job runs, or inside any script that file runs in turn (a cycle ends
   the walk; there is no depth cap), comments skipped; (c) the same reads over
   a local composite action (``uses: ./.github/actions/<name>``) the job calls,
   and over any action that one calls, with the scripts those actions run;
   (d) a ``--shard "${{ matrix.X }}/N"`` run whose matrix value the
   live-backend planner (``live_backend_shards``) assigns NornicDB runs.
   A re-enabled job, matrix cell, or shard fails here whatever the exact-text
   pins around it say. Each message names the workflow, the job, and the
   file and line.

   Stated limits (the guard does not claim these): a service name or backend
   computed at run time (``$(...)``, a function argument, a ``${X:-nornicdb}``
   default) is not resolved; Python scripts are read only for process-spawning
   calls, not for environment assignments; a reusable workflow
   (``uses: ./.github/workflows/x.yml``) is read as its own file, not as part
   of the caller; and a remote action or image is not opened.
2. A registry row whose CI job is permanently disabled is not blocking and is
   not in a blocking tier. Otherwise the aggregate would wait on a check that
   never runs.

Re-enabling the legs is a deliberate act: follow "Re-enabling NornicDB CI" in
docs/internal/ci-workflow-inventory.md, which removes this gate last.

Usage: verify-nornicdb-ci-off.py [--root REPO_ROOT]
"""

from __future__ import annotations

import argparse
import importlib.util
import re
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import nornicdb_ci_off_scan as scan  # noqa: E402

OFF_CONDITION = re.compile(r"^if:\s*(?:\$\{\{\s*false\s*\}\}|false)\s*$")
BLOCKING_TIERS = {"pre-commit", "pre-push", "pre-pr", "ci-heavy"}
SHARD_RUN = re.compile(r"--shard[ =]\"?\$\{\{\s*matrix\.(\w+)\s*\}\}/(\d+)\"?")


def shard_nornicdb_runs(root: Path, matrix_var: str, total: str,
                        job_lines: list[str]) -> list[str]:
    """Return matrix shards of a job that the live-backend planner gives NornicDB runs.

    The shard values come from the job's ``<matrix_var>: [..]`` line. The plan
    comes from the same ledger extractor and expander the CI aggregate uses, and
    the owner of the partition rule (``partition_runs``) says which backend each
    shard runs, so no shard number is hard-coded here.
    """
    values: list[str] = []
    for line in job_lines:
        match = re.fullmatch(rf"\s*{re.escape(matrix_var)}:\s*\[([^\]]*)\]", line)
        if match:
            values = [v.strip().strip("\"'") for v in match.group(1).split(",") if v.strip()]
    module_path = root / "scripts/lib/live_backend_shards.py"
    targets = root / "scripts/lib/live_backend_test_targets.py"
    if not values:
        return []
    if not module_path.is_file() or not targets.is_file():
        return [f"{value}/{total}: cannot evaluate, planner scripts are missing"
                for value in values]
    spec = importlib.util.spec_from_file_location("live_backend_shards_under_check", module_path)
    module = importlib.util.module_from_spec(spec)  # type: ignore[arg-type]
    spec.loader.exec_module(module)  # type: ignore[union-attr]
    extracted = subprocess.run(
        [sys.executable, str(targets), "specs/live-tests.v1.yaml", str(root)],
        cwd=root, text=True, capture_output=True, check=False)
    if extracted.returncode != 0:
        return [f"{value}/{total}: cannot evaluate, ledger extraction failed: "
                f"{extracted.stderr.strip()}" for value in values]
    plan = module.expand_targets(extracted.stdout.splitlines(), "both")
    flagged = []
    for value in values:
        try:
            runs = module.partition_runs(plan, f"{value}/{total}")
        except ValueError:
            continue
        if any(run.startswith("nornicdb|") for run in runs):
            flagged.append(f"{value}/{total}")
    return flagged


def workflow_jobs(text: str) -> dict[str, dict[str, object]]:
    """Split a workflow into jobs keyed by job id (2-space keys under jobs:)."""
    numbered = scan.numbered_active(text)
    lines = [line for _, line in numbered]
    try:
        start = lines.index("jobs:") + 1
    except ValueError:
        return {}
    jobs: dict[str, dict[str, object]] = {}
    current: str | None = None
    for number, line in numbered[start:]:
        if not line.startswith(" "):
            break
        key = re.fullmatch(r"  ([A-Za-z0-9_-]+):", line)
        if key:
            current = key.group(1)
            jobs[current] = {"lines": [], "numbered": [], "name": current, "off": False}
            continue
        if current is None:
            continue
        job = jobs[current]
        job["lines"].append(line)  # type: ignore[union-attr]
        job["numbered"].append((number, line))  # type: ignore[union-attr]
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
        for _, line in scan.numbered_active(path.read_text(encoding="utf-8")):
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


def job_violations(root: Path, workflow: str, job_id: str,
                   numbered: scan.Numbered) -> list[str]:
    """Return violations found in a live job, its composite actions, scripts, and shards."""
    errors: list[str] = []
    for unit in scan.job_units(root, f".github/workflows/{workflow}", numbered):
        errors.extend(scan.unit_violations(root, workflow, job_id, unit))
    lines = [line for _, line in numbered]
    for line in lines:
        shard = SHARD_RUN.search(line)
        if shard:
            for flagged in shard_nornicdb_runs(root, shard.group(1), shard.group(2), lines):
                errors.append(
                    f"{workflow}/{job_id}: a live job runs shard {flagged}, and the "
                    f"live-backend plan assigns that shard NornicDB runs; {scan.OFF_NOTE}")
    return errors


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
            errors.extend(job_violations(root, path.name, job_id, job["numbered"]))  # type: ignore[arg-type]
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
