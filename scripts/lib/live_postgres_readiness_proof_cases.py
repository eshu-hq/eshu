#!/usr/bin/env python3
"""Check that every enrolled readiness proof fails closed by name.

The hermetic self-test (scripts/test-run-live-postgres-readiness-tests.sh)
feeds one tab-separated record per enrolled proof on stdin: package path,
test name, and the path of that package's all-pass baseline event file. For
each proof, in input order, this helper plants three faults in a copy of the
baseline (the proof's pass event turned into a skip, then into a fail, then
every event of the proof removed) and runs the results verifier's
verify_results on it in this one process. Each case must fail and name the
proof, exactly as the self-test's former per-proof runner loop required.

One process replaces three full runner invocations per proof (#8241): the
per-package runner cases in the self-test still cover the runner's own
plumbing. The first case that passes or does not name its proof stops the
check with the same message the former loop printed; a clean run prints the
number of cases checked so the caller can refuse a vacuous pass.

Usage: live_postgres_readiness_proof_cases.py RESULTS_PY WORK_DIR < records
"""

import contextlib
import importlib.util
import io
import pathlib
import sys
from types import ModuleType


def load_results(path: pathlib.Path) -> ModuleType:
    """Import the results verifier at path with its sibling modules.

    Args:
        path: The live_postgres_readiness_results.py to load.

    Returns:
        The loaded module, which exposes PACKAGES and verify_results.
    """
    sys.path.insert(0, str(path.parent))
    spec = importlib.util.spec_from_file_location("readiness_results_under_test", path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load results verifier {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def plant(baseline: str, name: str, action: str) -> str:
    """Return the baseline events with one fault planted for test name.

    Args:
        baseline: The package's all-pass go test JSON event stream.
        name: The enrolled test to fault.
        action: "skip" or "fail" turns the test's pass event into that
            action; "missing" drops every event of the test.

    Returns:
        The faulted event stream.
    """
    lines = baseline.splitlines(keepends=True)
    if action == "missing":
        return "".join(line for line in lines if f'"Test":"{name}"' not in line)
    old = f'"Action":"pass","Test":"{name}"'
    new = f'"Action":"{action}","Test":"{name}"'
    return "".join(line.replace(old, new, 1) for line in lines)


def check_case(results: ModuleType, case_path: pathlib.Path, package: str,
               name: str, action: str) -> str:
    """Run one planted fault through verify_results.

    Args:
        results: The loaded results verifier module.
        case_path: The faulted event file to verify.
        package: The proof's Go package path.
        name: The faulted test name.
        action: The planted fault: "skip", "fail", or "missing".

    Returns:
        An empty string when the case failed closed naming the proof,
        otherwise the self-test failure message.
    """
    captured = io.StringIO()
    with contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
        status = results.verify_results(case_path, package, results.PACKAGES)
    out = captured.getvalue()
    if action == "missing":
        if status == 0:
            return f"missing {name} events passed"
        if f"{name}: missing" not in out:
            return f"missing {name} events not named: {out}"
        return ""
    if status == 0:
        return f"{name} {action} event passed"
    if f"{name}: {action.upper()}" not in out:
        return f"{name} {action} not named: {out}"
    return ""


def main() -> int:
    """Check every proof record on stdin; print the failure or the case count."""
    if len(sys.argv) != 3:
        print("usage: live_postgres_readiness_proof_cases.py RESULTS_PY WORK_DIR < records",
              file=sys.stderr)
        return 2
    results = load_results(pathlib.Path(sys.argv[1]))
    case_path = pathlib.Path(sys.argv[2]) / "proof-case.jsonl"
    checked = 0
    for record in sys.stdin.read().splitlines():
        fields = record.split("\t")
        if len(fields) != 3 or not all(fields):
            print(f"malformed proof record: {record!r}")
            return 1
        package, name, baseline_path = fields
        baseline = pathlib.Path(baseline_path).read_text(encoding="utf-8")
        for action in ("skip", "fail", "missing"):
            case_path.write_text(plant(baseline, name, action), encoding="utf-8")
            message = check_case(results, case_path, package, name, action)
            if message:
                print(message)
                return 1
            checked += 1
    print(checked)
    return 0


if __name__ == "__main__":
    sys.exit(main())
