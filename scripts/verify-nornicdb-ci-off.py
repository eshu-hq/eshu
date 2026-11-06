#!/usr/bin/env python3
"""Keep the NornicDB CI legs off until the owner re-enables them.

Owner order, 2026-11-05 (#7331): Neo4j is the supported graph backend, so no
NornicDB leg runs in CI. This gate holds two invariants over the committed
workflows and the gate registry. Neither one keeps a list of exceptions.

1. A live job (one whose ``if`` is not the literal ``${{ false }}``) starts no
   NornicDB backend. A start is found four ways, all read from the committed
   files: (a) a workflow line that selects ``nornicdb`` (a matrix value under
   any key, an environment value, ``--backend nornicdb``), brings up the
   ``nornicdb`` compose service, or ``docker run``s a NornicDB image; (b) the
   same start markers inside a ``scripts/*.sh`` or ``*.py`` file the job runs,
   or inside a script that file runs one level deeper, read across ``\\``
   continuation lines and Python argument lists, comments skipped; (c) a
   ``--shard "${{ matrix.X }}/N"`` run whose matrix value the live-backend
   planner (``live_backend_shards``) assigns NornicDB runs. A re-enabled job,
   matrix cell, or shard fails here whatever the exact-text pins around it say.
2. A registry row whose CI job is permanently disabled is not blocking and is
   not in a blocking tier. Otherwise the aggregate would wait on a check that
   never runs.

Re-enabling the legs is a deliberate act: follow "Re-enabling NornicDB CI" in
docs/internal/ci-workflow-inventory.md, which removes this gate last.

Usage: verify-nornicdb-ci-off.py [--root REPO_ROOT]
"""

from __future__ import annotations

import argparse
import ast
import importlib.util
import os
import re
import subprocess
import sys
from pathlib import Path

OFF_CONDITION = re.compile(r"^if:\s*(?:\$\{\{\s*false\s*\}\}|false)\s*$")
BLOCKING_TIERS = {"pre-commit", "pre-push", "pre-pr", "ci-heavy"}
START_MARKERS = (
    (re.compile(r"graph_backend:\s*\[?[^\n]*\bnornicdb\b"), "selects the nornicdb graph backend"),
    (re.compile(r"graph_service:\s*[\"']?nornicdb\b"), "selects the nornicdb compose service"),
    (re.compile(r"ESHU_GRAPH_BACKEND:\s*[\"']?nornicdb\b"), "sets ESHU_GRAPH_BACKEND to nornicdb"),
    (re.compile(r"--backend[ =][\"']?nornicdb\b"), "runs a runner with --backend nornicdb"),
    (re.compile(r"\bup\b[^\n]*\bnornicdb\b"), "brings up the nornicdb compose service"),
    (re.compile(r"\bdocker run\b[^\n]*nornicdb"), "docker-runs a NornicDB image"),
)
MATRIX_VALUE = "lists nornicdb as a matrix value"
START_MARKERS += (
    (re.compile(r"^\s*(?:-\s*)?\w+:\s*\[[^\]]*\bnornicdb\b"), MATRIX_VALUE),
    (re.compile(r"^\s*-\s*\w+:\s*[\"']?nornicdb[\"']?\s*$"), MATRIX_VALUE),
)
# The script a statement runs: its first word after env assignments and an
# interpreter, so a script merely named in a string or an argument is not followed.
RUNS_SCRIPT = re.compile(
    r"^(?:-\s+)?(?:run:\s*)?(?:\w+=[^\s\"'$()]+\s+)*"
    r"(?:(?:bash|sh|source|exec|python3?|\.)\s+)?(?:-[\w-]+\s+)*[\"']?"
    r"((?:\$\{?\w+\}?/)?[A-Za-z0-9_./-]+\.(?:sh|py))[\"']?(?:\s|$)")
STATEMENT_SPLIT = re.compile(r"&&|\|\||;|\||\bthen\b|\bdo\b")
SCRIPT_DOCKER_RUN = re.compile(r"^docker run\b.*nornicdb", re.IGNORECASE)
# A shell statement starts a backend only when the statement itself is the
# command. A line that merely quotes the command (an assertion in a self-test,
# a regex, a usage string) starts with something else and does not match.
_LEAD = r"^(?:\w+=[^\s\"'$()]+\s+)*(?:bash\s+|sh\s+|\./)?"
SCRIPT_START_MARKERS = (
    (re.compile(_LEAD + r"(?:\S*compose\.sh|docker[ -]compose)\b.*\bup\b.*\bnornicdb\b"),
     "brings up the nornicdb compose service"),
    (re.compile(_LEAD + r"\S*scripts/\S+\s.*--backend[ =][\"']?nornicdb\b"),
     "runs a runner with --backend nornicdb"),
)
PY_SPAWNERS = {"run", "Popen", "call", "check_call", "check_output", "system", "popen"}
PY_COMMAND_START = re.compile(
    r"\bdocker run\b.*nornicdb|\bdocker[ -]compose\b.*\bup\b.*\bnornicdb\b", re.IGNORECASE)
SHARD_RUN = re.compile(r"--shard[ =]\"?\$\{\{\s*matrix\.(\w+)\s*\}\}/(\d+)\"?")


def active_lines(text: str) -> list[str]:
    """Return non-comment lines with trailing YAML comments removed."""
    lines = []
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        lines.append(re.sub(r"\s+#.*$", "", line.rstrip()))
    return lines


def python_starts(text: str) -> list[str]:
    """Return the process-spawning calls of a Python script that start NornicDB.

    The script is parsed, not scanned as text: a string that only mentions a
    command (a fixture, a regex, a docstring) is not a call and is skipped.
    A call counts when the string constants in its arguments, joined with
    spaces, read as ``docker run ... nornicdb`` or a compose ``up ... nornicdb``.
    An argument that is a variable is not resolved; the shell and workflow
    checks still see the common cases.
    """
    try:
        tree = ast.parse(text)
    except SyntaxError as err:
        return [f"cannot be parsed ({err.msg}), so its docker use is unknown"]
    starts = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        name = func.attr if isinstance(func, ast.Attribute) else getattr(func, "id", "")
        if name not in PY_SPAWNERS:
            continue
        words = [n.value for arg in node.args for n in ast.walk(arg)
                 if isinstance(n, ast.Constant) and isinstance(n.value, str)]
        command = " ".join(words)
        if PY_COMMAND_START.search(command):
            starts.append(f"starts NornicDB from Python ({command[:80]})")
    return starts


def script_starts(text: str, python: bool = False) -> list[str]:
    """Return the statements of a script that start NornicDB.

    A shell script is read statement by statement after backslash-newline
    continuations are joined, so a multi-line ``docker run -d ... "${IMAGE}"``
    reads as one statement. Comments are skipped, and only a statement that is
    itself the command counts (see ``SCRIPT_START_MARKERS``).
    """
    if python:
        return python_starts(text)
    joined = re.sub(r"\\\n\s*", " ", text)
    starts = []
    for line in joined.splitlines():
        statement = line.strip()
        if statement.startswith("#"):
            continue
        if SCRIPT_DOCKER_RUN.match(statement):
            starts.append(f"docker-runs a NornicDB image ({statement[:80]})")
            continue
        for pattern, reason in SCRIPT_START_MARKERS:
            if pattern.search(statement):
                starts.append(f"{reason} ({statement[:80]})")
                break
    return starts


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


def command_words(text: str) -> list[str]:
    """Return the script paths a shell or workflow text runs as commands."""
    words = []
    joined = re.sub(r"\\\n\s*", " ", text)
    for line in joined.splitlines():
        if line.strip().startswith("#"):
            continue
        for statement in STATEMENT_SPLIT.split(line):
            match = RUNS_SCRIPT.match(statement.strip())
            if match:
                words.append(match.group(1))
    return words


def python_command_words(text: str) -> list[str]:
    """Return the script paths a Python script passes to a process-spawning call."""
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return []
    words = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            func = node.func
            name = func.attr if isinstance(func, ast.Attribute) else getattr(func, "id", "")
            if name in PY_SPAWNERS:
                words += [n.value for arg in node.args for n in ast.walk(arg)
                          if isinstance(n, ast.Constant) and isinstance(n.value, str)
                          and re.fullmatch(r"[A-Za-z0-9_./-]+\.(?:sh|py)", n.value)]
    return words


def script_refs(root: Path, words: list[str], base: Path | None) -> list[str]:
    """Resolve script words to repo-relative paths of files that exist in the tree.

    A word resolves against the repository root, the ``scripts`` directory, and
    (for a reference inside a script) that script's own directory. A leading
    ``${VAR}/`` is dropped first.
    """
    found: list[str] = []
    for word in words:
        token = re.sub(r"^\$\{?\w+\}?/", "", word)
        candidates = [root / token, root / "scripts" / token]
        if base is not None:
            candidates.append(base / token)
        for candidate in candidates:
            if candidate.is_file():
                rel = Path(os.path.normpath(candidate)).relative_to(root).as_posix()
                if rel not in found:
                    found.append(rel)
                break
    return found


def script_violations(root: Path, workflow: str, job_id: str, lines: list[str]) -> list[str]:
    """Return starts found in the scripts a live job runs, one level deeper too."""
    errors: list[str] = []
    seen: set[str] = set()
    queue = [(ref, None) for ref in script_refs(root, command_words("\n".join(lines)), None)]
    while queue:
        ref, parent = queue.pop(0)
        if ref in seen:
            continue
        seen.add(ref)
        body = (root / ref).read_text(encoding="utf-8")
        via = f" (run by {parent})" if parent else ""
        for start in script_starts(body, python=ref.endswith(".py")):
            errors.append(
                f"{workflow}/{job_id}: a live job runs {ref}{via}, which {start}; "
                "NornicDB CI is off by owner order (2026-11-05)")
        if parent is None:
            inner_words = python_command_words(body) if ref.endswith(".py") else command_words(body)
            queue.extend((inner, ref) for inner in script_refs(root, inner_words, (root / ref).parent))
    return errors


def invoked_starts(root: Path, workflow: str, job_id: str, lines: list[str]) -> list[str]:
    """Return violations found in the scripts and shards a live job drives."""
    errors = script_violations(root, workflow, job_id, lines)
    for line in lines:
        shard = SHARD_RUN.search(line)
        if shard:
            for flagged in shard_nornicdb_runs(root, shard.group(1), shard.group(2), lines):
                errors.append(
                    f"{workflow}/{job_id}: a live job runs shard {flagged}, and the "
                    "live-backend plan assigns that shard NornicDB runs; NornicDB CI is "
                    "off by owner order (2026-11-05)")
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
            for line in job["lines"]:  # type: ignore[union-attr]
                for pattern, reason in START_MARKERS:
                    if pattern.search(line):
                        errors.append(
                            f"{path.name}/{job_id}: a live job {reason} ({line.strip()}); "
                            "NornicDB CI is off by owner order (2026-11-05)")
                        break
            errors.extend(invoked_starts(root, path.name, job_id, job["lines"]))  # type: ignore[arg-type]
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
