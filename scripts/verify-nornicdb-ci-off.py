#!/usr/bin/env python3
"""Keep the NornicDB CI legs off until the owner re-enables them.

Owner order, 2026-11-05 (#7331): Neo4j is the supported graph backend, so no
NornicDB leg runs in CI. This gate holds two invariants over the committed
workflows and the gate registry. Neither one keeps a list of exceptions.

1. A live job (one whose ``if`` is not the literal ``${{ false }}``) starts no
   NornicDB backend. A start is found four ways, all read from the committed
   files; the scanners live in ``scripts/lib/nornicdb_ci_off_scan.py`` and
   ``scripts/lib/nornicdb_ci_off_shell.py``:
   (a) the workflow-level lines outside ``jobs:`` (a top-level ``env:``) are
   read with the same markers whenever the workflow has a live job, and a
   workflow input that defaults to ``nornicdb`` counts when a live job reads
   ``inputs.<name>``. A workflow line that selects ``nornicdb`` (a matrix value under any
   key, an environment value, ``--backend nornicdb``, an expression that picks
   it for a backend key), assigns ``ESHU_GRAPH_BACKEND=nornicdb`` inside a
   command, brings up the ``nornicdb`` compose service or profile, or
   ``docker run``s a NornicDB image, read after ``\\`` continuation lines are
   joined, after a shell variable set to a literal earlier in the same step is
   substituted, and ignoring case; or a job ``services:``/``container:`` image
   that names NornicDB. Workflows are ``*.yml`` and ``*.yaml``, and the
   indentation is read from the file;
   (b) the same start markers, and an ``export ESHU_GRAPH_BACKEND=nornicdb``,
   inside a ``*.sh`` or ``*.py`` file the job runs, or inside any script that
   file runs in turn (a cycle ends the walk; there is no depth cap), comments
   skipped. A shell line is split at ``&&``, ``;``, ``|`` and ``then`` outside
   quotes, ``sudo``, ``timeout N``, ``env`` and the like are dropped, and a
   ``bash -c '...'`` body is read, so ``cd x && docker ...`` counts;
   (c) the same reads over a local composite action
   (``uses: ./.github/actions/<name>``) the job calls, and over any action
   that one calls, with the scripts those actions run;
   (d) a ``--shard "${{ matrix.X }}/N"`` run whose matrix value the
   live-backend planner (``live_backend_shards``) assigns NornicDB runs.
   A re-enabled job, matrix cell, or shard fails here whatever the exact-text
   pins around it say. Each message names the workflow, the job, and the
   file and line. A workflow with no block-style ``jobs:`` mapping, or a job
   written inline without ``uses:``, fails too: the guard cannot read it.

   A step ``name:`` is a label: the bring-up and ``docker run`` markers skip it.
   ``docker container run``, ``podman run``, ``uses: docker://...nornicdb...``
   and ``compose -f <file named nornicdb> up`` count. A file that cannot be
   decoded fails with ``cannot read <path>``.

   Stated limits (the guard does not claim these; test-verify-nornicdb-ci-off-
   forms.py pins them): a service name or backend computed at run time
   (``$(...)``, a function argument, a ``${X:-nornicdb}`` default assigned to a
   variable first) is not resolved; a compose file's own default service
   (``docker compose -f <file> up`` with no service named) is not opened; an
   expression that picks the backend is read only on the keys ``backend``,
   ``graph_backend``, ``graph_service`` and ``ESHU_GRAPH_BACKEND``; a command
   substitution inside a double-quoted string is part of that string; Python
   scripts are read only for process-spawning calls, not for environment
   assignments; a reusable workflow (``uses: ./.github/workflows/x.yml``) is
   read as its own file, not as part of the caller; and a remote action or
   image is not opened; a YAML alias is not resolved; a script with no ``.sh`` or
   ``.py`` extension, a Makefile target, and a Go helper are not opened; quoting
   nested past two levels is not parsed; ``echo nornicdb | xargs docker run`` is
   not joined. A dead step (``if: false``) inside a live job is still
   flagged: only a job-level ``if`` switches a job off.
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


def indent_of(line: str) -> int:
    """Return the number of leading spaces of a line."""
    return len(line) - len(line.lstrip(" "))


def jobs_span(numbered: scan.Numbered) -> tuple[int, int, int] | None:
    """Return the top indent, and the first and end index of the lines under ``jobs:``.

    ``None`` means the file has no block-style ``jobs:`` mapping at its top level.
    """
    top = min((indent_of(line) for _, line in numbered), default=0)
    start = next((i + 1 for i, (_, line) in enumerate(numbered)
                  if line.strip() == "jobs:" and indent_of(line) == top), None)
    if start is None:
        return None
    end = next((i for i in range(start, len(numbered)) if indent_of(numbered[i][1]) <= top),
               len(numbered))
    return top, start, end


def workflow_level_lines(text: str) -> scan.Numbered:
    """Return the lines of a workflow outside its ``jobs:`` block.

    Top-level ``env:``, ``defaults:`` and ``on:`` apply to every job, so a
    backend set there is a start for each live job, whichever side of
    ``jobs:`` it is written on.
    """
    numbered = scan.numbered_active(text)
    span = jobs_span(numbered)
    if span is None:
        return []
    return numbered[:span[1] - 1] + numbered[span[2]:]


def input_default_hits(workflow: str, region: scan.Numbered,
                       live_lines: list[str]) -> list[str]:
    """Return ``default: nornicdb`` inputs that a live job reads.

    The input name is the nearest key above the ``default:`` line at a lower
    indent. A live job reads it as ``inputs.<name>`` (which also covers
    ``github.event.inputs.<name>``) or ``inputs['<name>']``.
    """
    hits = []
    text = "\n".join(live_lines)
    for index, (number, line) in enumerate(region):
        if not re.fullmatch(r"\s*default:\s*[\"']?nornicdb[\"']?", line, re.IGNORECASE):
            continue
        owner = next((m.group(1) for _, above in reversed(region[:index])
                      if indent_of(above) < indent_of(line)
                      and (m := re.fullmatch(r"\s*([\w-]+):\s*", above))), None)
        if owner and re.search(rf"\binputs(?:\.{re.escape(owner)}\b|\[[\"']{re.escape(owner)}[\"']\])",
                               text):
            hits.append(f"{workflow}: the workflow input {owner} defaults to nornicdb "
                        f"(.github/workflows/{workflow}:{number}: {line.strip()}) and a live "
                        f"job reads it; {scan.OFF_NOTE}")
    return hits


def workflow_level_violations(workflow: str, text: str,
                              live: list[dict[str, object]]) -> list[str]:
    """Return starts made above or below ``jobs:`` when at least one job is live."""
    if not live:
        return []
    region = workflow_level_lines(text)
    unit = scan.Unit(f".github/workflows/{workflow}", region, None, "the workflow level")
    errors = [f"{workflow}: {hit}, which applies to every live job; {scan.OFF_NOTE}"
              for hit in scan.unit_marker_hits(unit)]
    live_lines = [line for job in live for line in job["lines"]]  # type: ignore[union-attr]
    return errors + input_default_hits(workflow, region, live_lines)


def workflow_jobs(text: str) -> dict[str, dict[str, object]]:
    """Split a workflow into jobs keyed by job id.

    Indentation is read from the file, not assumed: the job keys sit at the
    indentation of the first line under ``jobs:``, and a job's own keys (``if``,
    ``name``) at the indentation of the first line under that job. A job id may
    be quoted. A job written inline (``id: {..}``) is kept as one line, so the
    start markers still read it. Only a reusable-workflow call (``uses:``) may
    be inline, because it has no steps, container, or services and its callee is
    scanned as its own file; any other inline job carries ``unreadable`` and the
    guard reports it.
    """
    numbered = scan.numbered_active(text)
    span = jobs_span(numbered)
    if span is None:
        return {}
    top, start, _ = span
    jobs: dict[str, dict[str, object]] = {}
    current: dict[str, object] | None = None
    job_indent: int | None = None
    key_indent: int | None = None
    for number, line in numbered[start:]:
        indent = indent_of(line)
        if indent <= top:
            break
        if job_indent is None:
            job_indent = indent
        key = re.fullmatch(r"\s*([\"']?)([A-Za-z0-9_.-]+)\1:[ ]*(.*)", line)
        if indent == job_indent and key:
            inline = key.group(3)
            current = {"lines": [line] if inline else [],
                       "numbered": [(number, line)] if inline else [],
                       "name": key.group(2), "off": False,
                       "unreadable": line.strip() if inline and "uses:" not in inline else ""}
            jobs[key.group(2)] = current
            key_indent = None
            continue
        if current is None:
            continue
        current["lines"].append(line)  # type: ignore[union-attr]
        current["numbered"].append((number, line))  # type: ignore[union-attr]
        if key_indent is None:
            key_indent = indent
        stripped = line.strip()
        if indent == key_indent:
            if OFF_CONDITION.match(stripped):
                current["off"] = True
            elif stripped.startswith("name:"):
                current["name"] = stripped[len("name:"):].strip().strip("\"'")
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
        for _, line in scan.numbered_active(scan.read_text(path)):
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
    for path in sorted(p for p in (root / ".github/workflows").glob("*.y*ml")
                       if p.suffix in (".yml", ".yaml")):
        try:
            text = scan.read_text(path)
        except scan.ReadError as err:
            errors.append(f"{path.name}: {err}, so the guard cannot tell whether it starts "
                          f"NornicDB; {scan.OFF_NOTE}")
            continue
        jobs = workflow_jobs(text)
        workflows[path.name] = jobs
        if not jobs:
            errors.append(f"{path.name}: no block-style `jobs:` mapping was found, so the "
                          f"guard cannot tell whether it starts NornicDB; {scan.OFF_NOTE}")
        for job_id, job in jobs.items():
            if job["unreadable"]:
                errors.append(f"{path.name}/{job_id}: the job is written inline "
                              f"({job['unreadable'][:60]!r}), so the guard cannot read it; "
                              f"write it as a block mapping")
                continue
            if job["off"]:
                continue
            try:
                errors.extend(job_violations(root, path.name, job_id, job["numbered"]))  # type: ignore[arg-type]
            except scan.ReadError as err:
                errors.append(f"{path.name}/{job_id}: {err}, so the guard cannot tell whether "
                              f"the job starts NornicDB; {scan.OFF_NOTE}")
        errors.extend(workflow_level_violations(
            path.name, text, [j for j in jobs.values() if not j["off"] and not j["unreadable"]]))
    try:
        rows = registry_rows(root)
    except scan.ReadError as err:
        errors.append(f"{err}, so the guard cannot check the registry rows of disabled jobs")
        rows = []
    for row in rows:
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
