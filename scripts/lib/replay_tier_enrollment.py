"""Close replay-tier source, selection, ledger, and Go event contracts."""

from __future__ import annotations

import ast
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from replay_tier_routes import verify_routes


PACKAGE_PREFIX = "github.com/eshu-hq/eshu/go/internal/"
REPLAY_PACKAGE = PACKAGE_PREFIX + "replay/offlinetier"
REDUCER_PACKAGE = PACKAGE_PREFIX + "reducer"
QUERY_PACKAGE = PACKAGE_PREFIX + "query"
ADDITIONAL_TIER_TESTS = {
    REPLAY_PACKAGE: {
        "TestOfflineReplayTierGraphTruth",
        "TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch",
        "TestNornicDBChainedOptionalMatchEvaluatesSecondHop",
    },
    REDUCER_PACKAGE: {"TestReducerProvenanceReplayTombstoneGraphTruth"},
}
BLAST_TESTS = {
    QUERY_PACKAGE: {
        "TestSQLTableBlastRadiusEveryBranchContributesLive",
        "TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive",
        "TestSQLTableBlastRadiusReportsUnseededBranchMissingLive",
    },
}
SOURCE_PATH = "go/internal/replay/offlinetier/"
RUNNER_PATH = "scripts/verify-replay-tier.sh"
ENROLLMENT_PATH = "scripts/lib/replay_tier_enrollment.py"


def load_inventory(root: Path) -> dict[str, object]:
    """Load the committed closed set used by both runner and ledger gate."""
    path = root / "scripts/lib/replay_tier_inventory.json"
    inventory = json.loads(path.read_text(encoding="utf-8"))
    verify_inventory(root, inventory)
    return inventory


def expected_tests(inventory: dict[str, object]) -> dict[str, set[str]]:
    """Return the package-qualified tests required in the first invocation."""
    files = inventory["files"]
    if not isinstance(files, dict):
        raise ValueError("invalid replay source inventory")
    replay = {name for entry in files.values() for name in entry["tests"]}
    return {REPLAY_PACKAGE: replay | ADDITIONAL_TIER_TESTS[REPLAY_PACKAGE],
            REDUCER_PACKAGE: set(ADDITIONAL_TIER_TESTS[REDUCER_PACKAGE])}


def verify_inventory(root: Path, inventory: dict[str, object]) -> None:
    """Check exactly 23 unchanged source files, 27 declarations, and selector."""
    files = inventory.get("files")
    if not isinstance(files, dict) or len(files) != 23 or inventory.get("file_count") != 23:
        raise ValueError("replay source inventory must contain exactly 23 files")
    tests = []
    for filename, entry in files.items():
        if not filename.startswith(SOURCE_PATH) or not filename.endswith("_live_test.go"):
            raise ValueError(f"invalid replay source path: {filename}")
        path = root / filename
        source = path.read_bytes()
        if hashlib.sha256(source).hexdigest() != entry.get("sha256"):
            raise ValueError(f"replay source changed: {filename}")
        declarations = re.findall(rb"^func (Test\w+)\(", source, re.M)
        names = [name.decode("ascii") for name in declarations]
        if names != entry.get("tests"):
            raise ValueError(f"replay declarations changed: {filename}")
        tests.extend(names)
    if len(tests) != 27 or len(set(tests)) != 27 or inventory.get("test_count") != 27:
        raise ValueError("replay source must declare 27 unique tests")
    selector = inventory.get("selector")
    expected_selector = (set(tests) - {"TestDeltaTombstoneGraphTruth",
                                      "TestDeltaTombstoneNegativeControlBrokenRetraction"})
    expected_selector |= {"TestDeltaTombstone"}
    expected_selector |= ADDITIONAL_TIER_TESTS[REPLAY_PACKAGE]
    expected_selector |= ADDITIONAL_TIER_TESTS[REDUCER_PACKAGE]
    if (not isinstance(selector, str)
            or set(selector.split("|")) != expected_selector
            or len(selector.split("|")) != 30):
        raise ValueError("replay selector does not name exactly the enrolled and retained tests")
    source_pins = inventory.get("proof_source_sha256")
    if (not isinstance(source_pins, dict)
            or set(source_pins) != {RUNNER_PATH, ENROLLMENT_PATH}):
        raise ValueError("replay proof source integrity pins are missing")
    for name, expected_digest in source_pins.items():
        if (not isinstance(expected_digest, str)
                or re.fullmatch(r"[0-9a-f]{64}", expected_digest) is None
                or hashlib.sha256((root / name).read_bytes()).hexdigest() != expected_digest):
            raise ValueError(f"replay proof source integrity mismatch: {name}")
    runner = (root / RUNNER_PATH).read_text(encoding="utf-8")
    verify_runner(runner, selector)
    verify_source_guard_activation(
        (root / ENROLLMENT_PATH).read_text(encoding="utf-8")
    )
    verify_routes(root)


def verify_reviewed_source(root: Path) -> None:
    """Reject unreviewed checkout contents and ambient Go source overrides."""
    for name in ("GOFLAGS", "GOWORK"):
        if os.environ.get(name):
            raise ValueError(f"shell {name} must be empty for reviewed source")
    top = subprocess.run(("git", "rev-parse", "--show-toplevel"), cwd=root,
                         capture_output=True, text=True, check=False)
    if top.returncode != 0 or Path(top.stdout.strip()).resolve() != root.resolve():
        raise ValueError("source root is not the reviewed Git checkout")
    status = subprocess.run(
        ("git", "status", "--porcelain=v1", "--untracked-files=all",
         "--ignore-submodules=none"), cwd=root, capture_output=True,
        text=True, check=False,
    )
    if status.returncode != 0 or status.stdout:
        raise ValueError("source checkout has staged, modified, or untracked files")
    effective = subprocess.run(("go", "env", "GOFLAGS", "GOWORK"), cwd=root / "go",
                               capture_output=True, text=True, check=False)
    if effective.returncode != 0:
        raise ValueError("could not inspect effective Go configuration")
    values = effective.stdout.splitlines()
    if len(values) != 2:
        raise ValueError("invalid effective Go configuration response")
    for name, value in zip(("GOFLAGS", "GOWORK"), values):
        if value:
            raise ValueError(f"effective {name} must be empty for reviewed source")


def verify_source_guard_activation(source: str) -> None:
    """Require the reviewed-source guard and unconditional CLI call sites."""
    required = (
        '("git", "status", "--porcelain=v1", "--untracked-files=all",',
        '("go", "env", "GOFLAGS", "GOWORK")',
        'if os.environ.get(name):',
    )
    if any(item not in source for item in required):
        raise ValueError("reviewed-source guard implementation is disabled")
    tree = ast.parse(source)
    functions = {node.name: node for node in tree.body
                 if isinstance(node, ast.FunctionDef)}
    guard = functions.get("verify_reviewed_source")
    main_function = functions.get("main")
    if guard is None or main_function is None:
        raise ValueError("reviewed-source guard implementation is missing")
    dispatch = tree.body[-1]
    if (not isinstance(dispatch, ast.If) or dispatch.orelse
            or ast.dump(dispatch.test) != ast.dump(
                ast.parse('__name__ == "__main__"', mode="eval").body)
            or len(dispatch.body) != 1
            or not isinstance(dispatch.body[0], ast.Raise)
            or not isinstance(dispatch.body[0].exc, ast.Call)
            or ast.dump(dispatch.body[0].exc) != ast.dump(
                ast.parse("SystemExit(main())", mode="eval").body)):
        raise ValueError("reviewed-source CLI dispatch is not unconditional")
    if any(isinstance(node, ast.Return) for node in ast.walk(guard)):
        raise ValueError("reviewed-source guard has an early return")
    try_blocks = [node for node in main_function.body if isinstance(node, ast.Try)]
    if (len(try_blocks) != 1 or main_function.body[-2] is not try_blocks[0]
            or not isinstance(main_function.body[-1], ast.Return)
            or not isinstance(main_function.body[-1].value, ast.Constant)
            or main_function.body[-1].value.value != 0
            or any(isinstance(node, (ast.If, ast.For, ast.While, ast.With,
                                     ast.Return, ast.Raise, ast.Try))
                   for node in main_function.body[:-2])):
        raise ValueError("reviewed-source guard CLI activation is incomplete")
    for statement in main_function.body[:-2]:
        if (isinstance(statement, ast.Import)
                and [alias.name for alias in statement.names] == ["argparse"]):
            continue
        if isinstance(statement, ast.Expr) and isinstance(statement.value, ast.Constant):
            continue
        if isinstance(statement, (ast.Expr, ast.Assign)) and isinstance(statement.value, ast.Call):
            call = statement.value
            if (isinstance(call.func, ast.Attribute)
                    and isinstance(call.func.value, ast.Name)
                    and (call.func.value.id, call.func.attr) in {
                        ("argparse", "ArgumentParser"), ("parser", "add_argument"),
                        ("parser", "parse_args")
                    }):
                continue
        raise ValueError("reviewed-source guard CLI prelude has an early exit")
    actions = try_blocks[0].body

    def calls_guard(node: ast.stmt) -> bool:
        return (isinstance(node, ast.Expr)
                and isinstance(node.value, ast.Call)
                and isinstance(node.value.func, ast.Name)
                and node.value.func.id == "verify_reviewed_source"
                and len(node.value.args) == 1
                and isinstance(node.value.args[0], ast.Attribute)
                and node.value.args[0].attr == "root")
    if not actions or not calls_guard(actions[0]):
        raise ValueError("reviewed-source guard must precede CLI work")
    event_blocks = [node for node in actions if isinstance(node, ast.If)
                    and isinstance(node.test, ast.Attribute)
                    and node.test.attr == "events"]
    if (len(event_blocks) != 1 or len(event_blocks[0].body) < 3
            or not calls_guard(event_blocks[0].body[2])):
        raise ValueError("reviewed-source guard must follow event checks")


def verify_runner(runner: str, selector: str) -> None:
    """Bind both Go invocations to JSON and direct-exit checking."""
    if runner.count("-run '" + selector + "'") != 1:
        raise ValueError("runner selector differs from the closed replay inventory")
    if runner.count("-count=1 -v -json") != 2:
        raise ValueError("both replay invocations must emit Go JSON events")
    required = (
        r'^python3 scripts/lib/replay_tier_enrollment\.py --root "\$\{repo_root\}" '
        r'--events "\$\{TIER_JSON\}" --go-exit "\$\{tier_status\}" '
        r'>"\$\{TIER_LOG\}" \|\| die ',
        r'^python3 scripts/lib/replay_tier_enrollment\.py --root "\$\{repo_root\}" '
        r'--events "\$\{BLAST_JSON\}" --go-exit "\$\{blast_status\}" '
        r'--blast >"\$\{BLAST_LOG\}" \|\| die ',
        r'^\) >"\$\{TIER_JSON\}" 2>"\$\{TIER_ERR\}"$',
        r'^\) >"\$\{BLAST_JSON\}" 2>"\$\{BLAST_ERR\}"$',
        r'^cat "\$\{TIER_JSON\}"$',
        r'^cat "\$\{BLAST_JSON\}"$',
    )
    for pattern in required:
        if not re.search(pattern, runner, re.M):
            raise ValueError("runner event verifier, direct exit, or stderr separation disabled")
    guard = ('python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" '
             '--reviewed-source')
    if runner.count("\n" + guard + "\n") != 5:
        raise ValueError("runner reviewed-source checks are missing or bypassed")
    positions = [match.start() for match in re.finditer(re.escape("\n" + guard + "\n"), runner)]
    anchors = (
        'log "starting lean NornicDB container',
        'log "running focused offline replay tier tests',
        'python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --events "${TIER_JSON}"',
        'log "running sql_table blast-radius branch proof',
        'python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --events "${BLAST_JSON}"',
    )
    anchor_positions = [runner.index(anchor) for anchor in anchors]
    if not (positions[0] < anchor_positions[0] < positions[1] < anchor_positions[1]
            < anchor_positions[2] < positions[2] < anchor_positions[3] < positions[3]
            < anchor_positions[4] < positions[4]):
        raise ValueError("runner reviewed-source checks are out of order")


def verify_ledger(rows: list[tuple[str, str, str, str, str, str]],
                  inventory: dict[str, object]) -> None:
    """Require every enrolled row and reject demotion even with a repinned digest."""
    files = inventory["files"]
    if not isinstance(files, dict):
        raise ValueError("invalid replay source inventory")
    by_path = {row[0]: row for row in rows}
    enrolled = {path for path, row in by_path.items() if row[4] == "replay-tier"}
    if enrolled != set(files):
        raise ValueError("replay-tier ledger rows differ from source inventory")
    for path in files:
        row = by_path[path]
        if row[1].strip() != "~" or row[2] != "dedicated_ci" or row[5] != "nornicdb":
            raise ValueError(f"replay-tier ledger policy mismatch: {path}")


def verify_events(path: Path, expected: dict[str, set[str]], go_exit: int) -> None:
    """Require a single ordered run/pass and package pass for each selected test."""
    if go_exit != 0:
        raise ValueError(f"go test exit status {go_exit}")
    states = {(package, name): "missing" for package, names in expected.items()
              for name in names}
    package_passes = set()
    started = set()
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid Go JSON event at line {line_number}") from error
        if (not isinstance(event, dict)
                or not isinstance(event.get("Action"), str)
                or not isinstance(event.get("Package"), str)):
            raise ValueError(f"invalid Go JSON event at line {line_number}")
        action, package = event["Action"], event["Package"]
        name = event.get("Test")
        if name is not None and not isinstance(name, str):
            raise ValueError(f"invalid Go test name at line {line_number}")
        if package not in expected:
            if name and any(name == selected or name.startswith(selected + "/")
                            for names in expected.values() for selected in names):
                raise ValueError(f"selected test reported under wrong package: {name}")
            continue
        if name is None:
            if action == "start":
                if package in started:
                    raise ValueError(f"duplicate package start: {package}")
                started.add(package)
            elif action == "pass":
                if package in package_passes or package not in started:
                    raise ValueError(f"invalid package pass: {package}")
                package_passes.add(package)
            elif action in ("fail", "skip"):
                raise ValueError(f"selected package {action}: {package}")
            elif action != "output":
                raise ValueError(f"invalid package event: {action}")
            continue
        top = name.split("/", 1)[0]
        if top not in expected[package]:
            if action in ("run", "pass", "fail", "skip"):
                raise ValueError(f"unexpected selected-package test: {package}/{name}")
            continue
        if name != top:
            if action in ("fail", "skip"):
                raise ValueError(f"selected child {action}: {package}/{name}")
            continue
        key = package, name
        if action == "run":
            if states[key] != "missing" or package not in started:
                raise ValueError(f"duplicate or unstarted test: {package}/{name}")
            states[key] = "running"
        elif action == "pass":
            if states[key] != "running" or package in package_passes:
                raise ValueError(f"invalid test pass: {package}/{name}")
            states[key] = "passed"
        elif action in ("fail", "skip"):
            raise ValueError(f"selected test {action}: {package}/{name}")
        elif action not in ("output", "pause", "cont"):
            raise ValueError(f"invalid test event: {action}")
    if set(expected) != package_passes or any(state != "passed" for state in states.values()):
        raise ValueError("missing selected test or package terminal pass")


def main() -> int:
    """Verify the committed inventory or a captured Go event stream."""
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--events", type=Path)
    parser.add_argument("--go-exit", type=int, default=0)
    parser.add_argument("--blast", action="store_true")
    parser.add_argument("--routes", action="store_true")
    parser.add_argument("--reviewed-source", action="store_true")
    args = parser.parse_args()
    try:
        verify_reviewed_source(args.root)
        if args.reviewed_source:
            return 0
        inventory = load_inventory(args.root)
        if args.routes:
            verify_routes(args.root)
        if args.events:
            expected = BLAST_TESTS if args.blast else expected_tests(inventory)
            verify_events(args.events, expected, args.go_exit)
            verify_reviewed_source(args.root)
            for line in args.events.read_text(encoding="utf-8").splitlines():
                event = json.loads(line)
                if event.get("Action") == "output":
                    output = event.get("Output", "")
                    if not isinstance(output, str):
                        raise ValueError("invalid Go output event")
                    sys.stdout.write(output)
    except (OSError, ValueError) as error:
        parser.exit(1, f"replay enrollment: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
