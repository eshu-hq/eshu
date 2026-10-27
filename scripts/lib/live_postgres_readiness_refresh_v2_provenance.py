"""Source-commit and GitHub Actions provenance for readiness refits."""

from __future__ import annotations

import ast
import json
from pathlib import Path
import re
import subprocess

import live_postgres_readiness_weights as weights

WORKFLOW = ".github/workflows/live-postgres-readiness.yml"
INVENTORY = "scripts/lib/live_postgres_readiness_inventory_generated.py"
MODULE_DIR = "scripts/lib/"
SHARD_RE = re.compile(r"live-postgres-readiness \(postgres18\) shard \((\d+)/(\d+)\)")


def _source_text(root: Path, sha: str, path: str) -> str:
    """Read a file from the claimed source commit without executing source code."""
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError(f"invalid source commit SHA: {sha!r}")
    result = subprocess.run(["git", "show", f"{sha}:{path}"], cwd=root,
                            text=True, capture_output=True, check=False)
    if result.returncode:
        raise ValueError(f"source {sha} lacks {path}: {result.stderr.strip()}")
    return result.stdout


def source_inventory(root: Path, sha: str) -> list[str]:
    """Expand only literal generated tables from the claimed source commit."""
    source = ast.parse(_source_text(root, sha, INVENTORY))
    modules = [node.module for node in source.body if isinstance(node, ast.ImportFrom)]
    if not modules or any(module is None or not module.startswith(
            "live_postgres_readiness_inventory_gen_") for module in modules):
        raise ValueError(f"source {sha} has unsupported inventory assembly")
    runs: list[str] = []
    seen: set[str] = set()
    for module in modules:
        table_source = ast.parse(_source_text(root, sha, MODULE_DIR + module + ".py"))
        tables = [node.value for node in table_source.body if isinstance(node, ast.Assign)
                  and any(isinstance(target, ast.Name) and target.id == "TABLE"
                          for target in node.targets)]
        if len(tables) != 1:
            raise ValueError(f"source {sha} has unsupported inventory table {module}")
        try:
            table = ast.literal_eval(tables[0])
        except (ValueError, TypeError, SyntaxError) as error:
            raise ValueError(f"source {sha} has nonliteral inventory {module}") from error
        if not isinstance(table, dict):
            raise ValueError(f"source {sha} has malformed inventory {module}")
        for package, files in table.items():
            if not isinstance(package, str) or not weights.PACKAGE_RE.fullmatch(package):
                raise ValueError(f"source {sha} has malformed inventory package")
            if not isinstance(files, dict):
                raise ValueError(f"source {sha} has malformed inventory files")
            for names in files.values():
                if not isinstance(names, tuple):
                    raise ValueError(f"source {sha} has malformed inventory tests")
                for test in names:
                    run = f"{package}|{test}"
                    if not weights.RUN_RE.fullmatch(run) or run in seen:
                        raise ValueError(f"source {sha} has duplicate or malformed inventory run {run}")
                    seen.add(run)
                    runs.append(run)
    if not runs:
        raise ValueError(f"source {sha} inventory is empty")
    return runs


def source_rows(root: Path, sha: str, plan: list[str],
                count: int) -> dict[int, list[frozenset[str]]]:
    """Rebuild each source-SHA runner row without executing historical code.

    The current planner and splitter interpret the source commit's validated
    weights. A behavior change that cannot reproduce old rows fails closed.
    """
    import live_postgres_readiness_shards as shards
    path = f"specs/live-postgres-readiness-weights.v{2 if count == 5 else 1}.json"
    parsed = json.loads(_source_text(root, sha, path),
                        object_pairs_hook=weights._unique_object,
                        parse_constant=weights._reject_constant)
    if count == 5:
        import live_postgres_readiness_v2 as v2
        model = v2.weights_from_json(parsed)
    else:
        model = weights.weights_from_json(parsed)
    selected: dict[int, list[frozenset[str]]] = {}
    for number in range(1, count + 1):
        selected[number] = []
        for row in shards.subset_tsv(plan, f"{number}/{count}", model):
            package, _, names = row.split("\t")
            runs = [f"{package}|{name}" for name in names.split(" ")]
            if len(set(runs)) != len(runs):
                raise ValueError("source planner emitted duplicate row tests")
            selected[number].append(frozenset(runs))
    return selected


def source_shard_count(root: Path, sha: str) -> int:
    """Bind historical four or activated five shards to the source tree."""
    _source_text(root, sha, WORKFLOW)
    candidate = "specs/live-postgres-readiness-weights.v2.json"
    exists = subprocess.run(["git", "cat-file", "-e", f"{sha}:{candidate}"],
                            cwd=root, capture_output=True, check=False).returncode == 0
    if not exists:
        return 4
    parsed = json.loads(_source_text(root, sha, candidate),
                        object_pairs_hook=weights._unique_object,
                        parse_constant=weights._reject_constant)
    import live_postgres_readiness_v2 as v2
    return v2.weights_from_json(parsed).shard_count


def _jobs(payload: object) -> list[dict[str, object]]:
    """Reject truncated job-list pagination or malformed metadata."""
    if not isinstance(payload, dict) or not isinstance(payload.get("jobs"), list):
        raise ValueError("jobs.json is malformed")
    jobs = payload["jobs"]
    if type(payload.get("total_count")) is not int or payload["total_count"] != len(jobs):
        raise ValueError("jobs.json is partial")
    if any(not isinstance(job, dict) for job in jobs):
        raise ValueError("jobs.json has malformed job")
    ids = [job.get("id") for job in jobs]
    if any(type(identifier) is not int or identifier <= 0 for identifier in ids) or len(set(ids)) != len(ids):
        raise ValueError("jobs.json has invalid or duplicate job ID")
    return jobs


def _successful(job: dict[str, object], run_id: int, sha: str, attempt: int) -> None:
    if (job.get("run_id") != run_id or job.get("head_sha") != sha
            or job.get("run_attempt") != attempt or job.get("status") != "completed"
            or job.get("conclusion") != "success"):
        raise ValueError(f"source job is incomplete, failed, or mismatched: {job.get('name')}")


def _seconds(value: object) -> float:
    from datetime import datetime
    if not isinstance(value, str):
        raise ValueError("job timestamp is missing")
    return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


def read_source(directory: Path, root: Path) -> tuple[str, str, list[str], dict[int, str], list[float]]:
    """Validate a complete successful main run and return its raw shard logs."""
    try:
        run = json.loads((directory / "run.json").read_text(encoding="utf-8"))
        jobs = _jobs(json.loads((directory / "jobs.json").read_text(encoding="utf-8")))
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"source metadata unavailable or malformed: {directory}") from error
    if not isinstance(run, dict):
        raise ValueError("run.json is malformed")
    run_id, sha, attempt = run.get("id"), run.get("head_sha"), run.get("run_attempt")
    for field in ("repository", "head_repository"):
        repo = run.get(field)
        if not isinstance(repo, dict) or repo.get("full_name") != "eshu-hq/eshu":
            raise ValueError("source repository identity differs from eshu-hq/eshu")
    if (type(run_id) is not int or run_id <= 0 or str(run_id) != directory.name
            or type(attempt) is not int
            or attempt < 1 or run.get("head_branch") != "main" or run.get("event") != "push"
            or run.get("status") != "completed" or run.get("conclusion") != "success"
            or run.get("path") != WORKFLOW or not isinstance(sha, str)):
        raise ValueError("source must be a complete successful main push workflow")
    plan = source_inventory(root, sha)
    count = source_shard_count(root, sha)
    if count not in (4, 5):
        raise ValueError(f"source {run_id} has unsupported shard count {count}")
    required = {"queue-selection / select", "live-postgres-readiness hermetic",
                "live-postgres-readiness (postgres18)"}
    names = [job.get("name") for job in jobs]
    if any(names.count(name) != 1 for name in required):
        raise ValueError("source is missing or duplicates required jobs")
    shard_jobs: dict[int, dict[str, object]] = {}
    for job in jobs:
        name = job.get("name")
        if name in required:
            _successful(job, run_id, sha, attempt)
        elif isinstance(name, str) and (match := SHARD_RE.fullmatch(name)):
            number, total = map(int, match.groups())
            if total != count or number in shard_jobs or not 1 <= number <= count:
                raise ValueError("source shard topology is incomplete or duplicated")
            shard_jobs[number] = job
    if set(shard_jobs) != set(range(1, count + 1)):
        raise ValueError("source shard topology is incomplete")
    logs: dict[int, str] = {}
    fixed: list[float] = []
    for number, job in sorted(shard_jobs.items()):
        _successful(job, run_id, sha, attempt)
        steps = job.get("steps")
        proof = [step for step in steps if isinstance(step, dict)
                 and step.get("name") == "Run live readiness proofs"] if isinstance(steps, list) else []
        if len(proof) != 1 or proof[0].get("status") != "completed" or proof[0].get("conclusion") != "success":
            raise ValueError("source proof step is missing or failed")
        overhead = (_seconds(job.get("completed_at")) - _seconds(job.get("started_at"))
                    - _seconds(proof[0].get("completed_at"))
                    + _seconds(proof[0].get("started_at")))
        if overhead < 0:
            raise ValueError("source job has invalid timing boundaries")
        fixed.append(overhead)
        try:
            log = (directory / f"shard{number}.log").read_text(encoding="utf-8")
        except OSError as error:
            raise ValueError(f"source shard {number} log unavailable") from error
        job_name = f"Complete job name: {job['name']}"
        artifact = f"name: readiness-shard-{run_id}-{attempt}-{number}"
        if job_name not in log or artifact not in log:
            raise ValueError(f"source shard {number} log does not match run and job")
        logs[number] = log
    return str(run_id), sha, plan, logs, fixed
