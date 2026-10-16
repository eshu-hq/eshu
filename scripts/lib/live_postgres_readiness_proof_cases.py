#!/usr/bin/env python3
"""Build the readiness self-test's fake events and check every proof by name.

The hermetic self-test (scripts/test-run-live-postgres-readiness-tests.sh)
feeds one tab-separated record per enrolled proof on stdin, in the runner's
package order: package path, test name, and the path of that package's
all-pass event file (its test-name list sits beside it as `.names`).

`snapshot` writes those all-pass files: a run and a pass event per test,
then one passing package terminal per package. The self-test builds them
once per proof list and copies them for each case (#8241).

`check` plants three faults per proof in a copy of its package file (the
pass event turned into a skip, then into a fail, then every event of the
proof removed) and runs the results verifier's verify_results on it in this
one process. Each case must fail and name the proof, as the self-test's
former per-proof runner loop required; the self-test's runner cases cover
the runner's own plumbing for every package. The first case that passes or
does not name its proof prints the former loop's message and exits 3; a
clean run prints the number of cases checked so the caller can refuse a
vacuous pass.

Usage: live_postgres_readiness_proof_cases.py snapshot < records
       live_postgres_readiness_proof_cases.py check RESULTS_PY WORK_DIR < records
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


def read_records() -> list[tuple[str, str, pathlib.Path]]:
    """Return the (package, test, events file) records on stdin.

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


def write_snapshot(records: list[tuple[str, str, pathlib.Path]]) -> int:
    """Write each package's all-pass events file and test-name list.

    Args:
        records: The proof records, grouped by package in runner order.

    Returns:
        0 after writing the files.
    """
    events: dict[pathlib.Path, list[str]] = {}
    names: dict[pathlib.Path, list[str]] = {}
    packages: dict[pathlib.Path, str] = {}
    for package, name, path in records:
        packages.setdefault(path, package)
        names.setdefault(path, []).append(f"{name}\n")
        events.setdefault(path, []).append(
            f'{{"Action":"run","Test":"{name}"}}\n'
            f'{{"Action":"pass","Test":"{name}","Elapsed":0.25}}\n')
    for path, package in packages.items():
        terminal = package.removeprefix("./")
        events[path].append(
            f'{{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/{terminal}","Elapsed":1.0}}\n')
        path.write_text("".join(events[path]), encoding="utf-8")
        path.with_suffix(".names").write_text("".join(names[path]), encoding="utf-8")
    return 0


def check_records(records: list[tuple[str, str, pathlib.Path]], results_path: pathlib.Path,
                  work_dir: pathlib.Path) -> int:
    """Check every proof record; print the failure or the case count.

    Args:
        records: The proof records with their all-pass events files.
        results_path: The live_postgres_readiness_results.py under test.
        work_dir: A scratch directory for the faulted events file.

    Returns:
        0 when every case failed closed by name, 3 at the first that did not.
    """
    results = load_results(results_path)
    case_path = work_dir / "proof-case.jsonl"
    checked = 0
    for package, name, path in records:
        baseline = path.read_text(encoding="utf-8")
        for action in ("skip", "fail", "missing"):
            case_path.write_text(plant(baseline, name, action), encoding="utf-8")
            message = check_case(results, case_path, package, name, action)
            if message:
                print(message)
                return 3
            checked += 1
    print(checked)
    return 0


def main() -> int:
    """Dispatch the snapshot and check modes."""
    if sys.argv[1:2] == ["snapshot"] and len(sys.argv) == 2:
        return write_snapshot(read_records())
    if sys.argv[1:2] == ["check"] and len(sys.argv) == 4:
        return check_records(read_records(), pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]))
    print("usage: live_postgres_readiness_proof_cases.py snapshot < records | "
          "check RESULTS_PY WORK_DIR < records", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
