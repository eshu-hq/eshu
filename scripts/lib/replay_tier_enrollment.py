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
    runner = (root / RUNNER_PATH).read_text(encoding="utf-8")
    verify_runner(runner, selector)
    verify_source_guard_activation(
        (root / "scripts/lib/replay_tier_enrollment.py").read_text(encoding="utf-8")
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
    if any(isinstance(node, ast.Return) for node in ast.walk(guard)):
        raise ValueError("reviewed-source guard has an early return")
    try_blocks = [node for node in main_function.body if isinstance(node, ast.Try)]
    if len(try_blocks) != 1:
        raise ValueError("reviewed-source guard CLI activation is incomplete")
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


ROUTED_PATHS = (
    "scripts/lib/replay_tier_enrollment.py",
    "scripts/lib/replay_tier_inventory.json",
    "scripts/test-replay-tier-enrollment.py",
    "scripts/test-replay-tier-ledger-policy.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "specs/live-tests.v1.yaml",
    "specs/live-tests.d/**",
    "specs/ci-gates.d/gates-15.yaml",
)


def verify_workflow_liveness(workflow: str) -> None:
    """Require the existing CI job to execute every reviewed proof step."""
    if (not all(re.search(pattern, workflow, re.M) for pattern in (
        r"^  merge_group:\n    types: \[checks_requested\]$",
        r"^  push:\n    branches: \[ main \]$",
        r"^  pull_request:\n    branches: \[ main \]$",
    )) or any(len(re.findall(pattern, workflow, re.M)) != 1 for pattern in (
        r"^on:.*$", r"^jobs:.*$", r"^  merge_group:.*$",
        r"^  push:.*$", r"^  pull_request:.*$",
    ))):
        raise ValueError("workflow events no longer select the replay gate")
    sections = workflow.split("\njobs:\n", 1)
    if len(sections) != 2:
        raise ValueError("workflow jobs block is missing")
    job_rows = re.findall(
        r"(?ms)^  ([a-z][a-z0-9-]*):\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)",
        sections[1],
    )
    if [name for name, _ in job_rows] != ["queue-selection", "replay-tier"]:
        raise ValueError("workflow queue or replay job is missing or replaced")
    queue, replay = (body for _, body in job_rows)
    queue_fields = re.findall(r"(?m)^    ([a-z][a-z0-9-]*):(?: ([^\n]+))?$", queue)
    replay_fields = re.findall(r"(?m)^    ([a-z][a-z0-9-]*):(?: ([^\n]+))?$", replay)
    if (queue_fields != [("uses", "./.github/workflows/queue-selection.yml"),
                         ("with", "")]
            or re.findall(r"(?m)^      ([a-z][a-z0-9-]*):(?: ([^\n]+))?$",
                          queue) != [("workflow", "verify-replay-tier.yml")]
            or replay_fields != [
                ("needs", "queue-selection"),
                ("name", "Offline replay tier vs real NornicDB"),
                ("runs-on", "ubuntu-latest"),
                ("timeout-minutes", "15"),
                ("steps", ""),
            ]):
        raise ValueError("workflow replay job or queue selection is disabled")
    steps_text = replay.split("    steps:\n", 1)[1]
    steps = re.findall(r"(?ms)^      - name: ([^\n]+)\n(.*?)(?=^      - |\Z)",
                       steps_text)
    if len(steps) != 7 or len(re.findall(r"(?m)^      - ", steps_text)) != 7:
        raise ValueError("workflow proof steps are missing or replaced")
    selection = ("${{ github.event_name != 'merge_group' || "
                 "(contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), "
                 "'Offline replay tier vs real NornicDB')) }}")
    expected = (
        ("Require queue selection to succeed",
         "${{ always() && github.event_name == 'merge_group' }}",
         "run", 'test "${{ needs.queue-selection.result }}" = success'),
        ("Check out code", selection, "uses", "actions/checkout@v5"),
        ("Set up Go", selection, "uses", "actions/setup-go@v6"),
        ("Install contract dependencies", selection, "run",
         "scripts/ci/install-apt-packages.sh ripgrep"),
        ("Pre-warm Go modules", selection, "run",
         "scripts/ci/go-mod-download-retry.sh"),
        ("Verify replay tier contract", selection, "run",
         "bash scripts/test-verify-replay-tier.sh"),
        ("Run offline replay tier against real NornicDB", selection, "run",
         "bash scripts/verify-replay-tier.sh"),
    )
    for (name, body), (want_name, want_if, command_key, command) in zip(steps, expected):
        fields = re.findall(r"(?m)^        ([a-z][a-z0-9-]*):(?: ([^\n]+))?$",
                            body)
        want_fields = [("if", want_if), (command_key, command)]
        if name == "Set up Go":
            want_fields.append(("with", ""))
            if re.findall(
                r"(?m)^          ([a-z][a-z0-9-]*):(?: ([^\n]+))?$", body
            ) != [("go-version-file", "go/go.mod"),
                  ("cache-dependency-path", "go/go.sum")]:
                raise ValueError("workflow Go setup no longer uses reviewed module")
        if name != want_name or fields != want_fields:
            raise ValueError(f"workflow proof step disabled or altered: {want_name}")


def verify_routes(root: Path) -> None:
    """Require metadata-only PRs to select the existing blocking replay job."""
    workflow = (root / ".github/workflows/verify-replay-tier.yml").read_text()
    verify_workflow_liveness(workflow)
    registry = (root / "specs/ci-gates.d/gates-15.yaml").read_text()
    prepr = (root / "scripts/dev/pre-pr.sh").read_text()
    if "  pull_request:" not in workflow or "\nconcurrency:" not in workflow:
        raise ValueError("workflow lacks active pull request routing")
    if "  - id: replay-tier\n" not in registry:
        raise ValueError("registry lacks replay-tier gate")
    if "run_or_defer replay-tier " not in prepr:
        raise ValueError("pre-pr lacks replay-tier selector")
    pull_request = workflow.split("  pull_request:", 1)[1].split("\nconcurrency:", 1)[0]
    replay = registry.split("  - id: replay-tier\n", 1)[1].split("\n  - id:", 1)[0]
    selector = prepr.split("run_or_defer replay-tier ", 1)[1].split("\n", 2)[1]
    for path in ROUTED_PATHS:
        if f"- '{path}'" not in pull_request:
            raise ValueError(f"workflow lacks replay metadata trigger: {path}")
        if path.startswith("scripts/lib/replay_tier_"):
            registry_path = "scripts/lib/replay_tier_*"
            prepr_path = "scripts/lib/(replay_tier_"
        elif path == "scripts/lib/verify-live-tests-ledger.py":
            registry_path = "scripts/lib/verify-live-tests-ledger.py"
            prepr_path = "verify-live-tests-ledger"
        elif path.startswith("scripts/test-replay-tier-"):
            registry_path = "scripts/test-replay-tier-*.py"
            prepr_path = "scripts/test-replay-tier-.*"
        elif path.startswith("specs/live-tests"):
            registry_path = path
            prepr_path = "specs/live-tests"
        else:
            registry_path = path
            prepr_path = "specs/ci-gates"
        if f'- "{registry_path}"' not in replay:
            raise ValueError(f"registry lacks replay metadata trigger: {path}")
        if prepr_path not in selector:
            raise ValueError(f"pre-pr lacks replay metadata trigger: {path}")


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
