#!/usr/bin/env python3
"""Check every enrolled readiness proof by name in one process.

The hermetic self-test (scripts/lib/test-run-live-postgres-readiness-proofs.sh)
feeds one tab-separated record per enrolled proof on stdin: the proof's Go
package path, its test name, and the path of that package's all-pass go test
JSON event file.

For each proof this plants three faults in an in-memory copy of that package's
events (the pass event turned into a skip, then into a fail, then every event
of the proof removed) and runs the results verifier's verify_results on it.
Each case must fail and name the proof, as the self-test's former per-proof
subprocess loop required (#8241). The full-runner cases stay in the self-test:
this helper covers only the verifier's per-proof accounting.

The first case that passes or does not name its proof prints the former
loop's message and exits 3. A clean run prints the number of cases checked so
the caller can refuse a vacuous pass.

Usage: live_postgres_readiness_proof_cases.py RESULTS_PY WORK_DIR < records
"""

import contextlib
import importlib.util
import io
import pathlib
import sys
import types

ACTIONS = ("skip", "fail", "missing")


def load_results(path: pathlib.Path) -> types.ModuleType:
    """Imports the results verifier at path together with its sibling modules.

    Args:
        path: The live_postgres_readiness_results.py to load.

    Returns:
        The loaded module, which exposes PACKAGES and verify_results.

    Raises:
        ImportError: The file cannot be loaded as a module.
    """
    sys.path.insert(0, str(path.parent))
    spec = importlib.util.spec_from_file_location("readiness_results_under_test", path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load results verifier {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def plant(baseline: str, name: str, action: str) -> str:
    """Returns the baseline events with one fault planted for a test.

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


def names_line(out: str, want: str) -> bool:
    """Reports whether a verifier output line starts with want.

    The old self-test shell asserted the line start, so a test whose name
    only ends like another's cannot satisfy the check.

    Args:
        out: The captured verifier output.
        want: The "<name>: <verdict>" text a line must begin with.

    Returns:
        True when some line of out begins with want.
    """
    return "\n" + want in "\n" + out


def check_case(results: types.ModuleType, case_path: pathlib.Path, package: str,
               name: str, action: str) -> str:
    """Runs one planted fault through verify_results.

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
        if not names_line(out, f"{name}: missing"):
            return f"missing {name} events not named: {out}"
        return ""
    if status == 0:
        return f"{name} {action} event passed"
    if not names_line(out, f"{name}: {action.upper()}"):
        return f"{name} {action} not named: {out}"
    return ""


def read_records() -> list[tuple[str, str, pathlib.Path]]:
    """Reads the (package, test, events file) records from stdin.

    Returns:
        One tuple per proof, in input order.

    Raises:
        ValueError: A record is not three non-empty tab-separated fields.
    """
    records = []
    for record in sys.stdin.read().splitlines():
        fields = record.split("\t")
        if len(fields) != 3 or not all(fields):
            raise ValueError(f"malformed proof record: {record!r}")
        records.append((fields[0], fields[1], pathlib.Path(fields[2])))
    return records


def check_records(records: list[tuple[str, str, pathlib.Path]],
                  results_path: pathlib.Path, work_dir: pathlib.Path) -> int:
    """Checks every proof record and prints the failure or the case count.

    Args:
        records: The proof records with their all-pass events files.
        results_path: The live_postgres_readiness_results.py under test.
        work_dir: A scratch directory for the faulted events file.

    Returns:
        0 when every case failed closed by name, 3 at the first that did not.
    """
    results = load_results(results_path)
    case_path = work_dir / "proof-case.jsonl"
    baselines: dict[pathlib.Path, str] = {}
    checked = 0
    for package, name, path in records:
        if path not in baselines:
            baselines[path] = path.read_text(encoding="utf-8")
        for action in ACTIONS:
            case_path.write_text(plant(baselines[path], name, action), encoding="utf-8")
            message = check_case(results, case_path, package, name, action)
            if message:
                print(message)
                return 3
            checked += 1
    print(checked)
    return 0


def main() -> int:
    """Parses the arguments and checks the proof records on stdin."""
    if len(sys.argv) != 3:
        print("usage: live_postgres_readiness_proof_cases.py RESULTS_PY WORK_DIR < records",
              file=sys.stderr)
        return 2
    return check_records(read_records(), pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]))


if __name__ == "__main__":
    sys.exit(main())
