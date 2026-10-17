#!/usr/bin/env python3
# Live-test ledger integrity check (#6784). Verifies that every
# *_live_test.go file under go/ is classified in specs/live-tests.v1.yaml
# with a valid class and a non-blank reason, and that every ledger row
# names a tracked or present file exactly once. An optional `backends:`
# line (nornicdb, neo4j, or both; default both) pins which backends the
# test can run on.
#
# Invoked by scripts/verify-live-tests-ledger.sh as a file (not a bash
# heredoc) so the heredoc-budget gate stays green: Homebrew bash >= 5.1
# deadlocks on heredoc bodies over 512 bytes.
#
# Usage: verify-live-tests-ledger.py <ledger_path> <repo_root>
import ast
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys

from golden_corpus_identity import (
    PROOFS as IDENTITY_PROOFS,
    verify_inventory as verify_identity,
    verify_routes as verify_identity_routes,
)
from live_tests_registry import load_ledger_text
from live_postgres_migrations import MIGRATION_PATHS
from reducer_contention_events import load_inventory, verify_inventory
from ifa_scheduled_events import (
    load_inventory as load_ifa_scheduled_inventory,
    verify_inventory as verify_ifa_scheduled_inventory,
)
from postgres_scheduled_runner import (
    load_inventory as load_scheduled_inventory,
    verify_inventory as verify_scheduled_inventory,
)


NORNIC_CLI_SOURCE_SHA256 = "df58ba4e0f35fd0cd8308a3a04aee2ce45f1ab0b8e932a15350668a91e6a32fd"
NORNIC_CLI_MAIN = '''def main() -> int:
    """Check runner enrollment, then optionally a captured Go event stream."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-check", action="store_true")
    parser.add_argument("--events", type=Path)
    args = parser.parse_args()
    try:
        inventory = load_inventory()
        verify_source_and_runner(ROOT, inventory)
        if args.events is None:
            return 0
        names = {name for tests in inventory.values() for name in tests}
        return 0 if verify_events(args.events, names, sys.stderr) else 1
    except (OSError, ValueError, re.error) as error:
        print(error, file=sys.stderr)
        return 1'''
NORNIC_CLI_DISPATCH = 'if __name__ == "__main__":\n    raise SystemExit(main())\n'
NORNIC_CLI_IMPORTS = (
    "import argparse", "import json", "from pathlib import Path",
    "import re", "import sys", "from typing import TextIO",
)
NORNIC_CLI_ASSIGNMENTS = (
    "ROOT", "INVENTORY", "RUNNER", "RUNNER_PREFIX", "WORKFLOW",
    "STATIC_WORKFLOW", "REGISTRY", "PACKAGE", "TEST_PATTERN",
    "EXPECTED_FILES", "ROUTED_PATHS",
)
NORNIC_CLI_VALUE_EXPRESSIONS = {
    "ROOT": "Path(__file__).resolve().parents[2]",
    "INVENTORY": 'Path(__file__).with_name("nornic_conformance_inventory.json")',
    "RUNNER": 'Path("scripts/verify_backend_conformance_live.sh")',
    "WORKFLOW": 'Path(".github/workflows/e2e-tests.yml")',
    "STATIC_WORKFLOW": 'Path(".github/workflows/live-backend-tests.yml")',
    "REGISTRY": 'Path("specs/ci-gates.d/gates-09.yaml")',
    "PACKAGE": '"github.com/eshu-hq/eshu/go/internal/storage/cypher"',
    "TEST_PATTERN": r're.compile(r"^func (Test\w+)\(", re.MULTILINE)',
}
NORNIC_CLI_LITERAL_SETS = {
    "EXPECTED_FILES": {
        "go/internal/storage/cypher/cloud_resource_node_writer_heterogeneous_batch_live_test.go",
        "go/internal/storage/cypher/retrying_executor_live_test.go",
        "go/internal/storage/cypher/retrying_executor_relationship_snapshot_live_test.go",
        "go/internal/storage/cypher/semantic_entity_full_repo_retract_live_test.go",
        "go/internal/storage/cypher/semantic_entity_live_test.go",
        "go/internal/storage/cypher/semantic_entity_variable_nornicdb_live_test.go",
        "go/internal/storage/cypher/tfstate_canonical_writer_stale_attrs_live_test.go",
    },
    "ROUTED_PATHS": {
        "go/**", "scripts/verify_backend_conformance_live.sh",
        "scripts/lib/nornic_conformance_events.py",
        "scripts/lib/nornic_conformance_inventory.json",
        "scripts/test-nornic-conformance-events.py",
        "scripts/lib/verify-live-tests-ledger.py", "specs/live-tests.v1.yaml",
        "specs/live-tests.d/**", ".github/workflows/e2e-tests.yml",
        ".github/workflows/live-backend-tests.yml",
        "specs/ci-gates.d/gates-09.yaml",
    },
}
NORNIC_CLI_FUNCTION_SIGNATURES = {
    "_active_lines": "source: str -> list[tuple[int, str]]",
    "_block": "lines: list[tuple[int, str]], key: str, depth: int -> list[tuple[int, str]]",
    "_paths": "lines: list[tuple[int, str]], depth: int -> set[str]",
    "_workflow_paths": "source: str, static: bool -> set[str]",
    "_registry_paths": "source: str -> set[str]",
    "_static_step": "source: str -> None",
    "verify_routing_text": "e2e: str, live: str, registry: str -> None",
    "load_inventory": "path: Path=INVENTORY -> dict[str, list[str]]",
    "verify_source": "root: Path, inventory: dict[str, list[str]] -> None",
    "read_ledger_rows": "root: Path -> dict[str, tuple[str, str, str]]",
    "verify_ledger": "inventory: dict[str, list[str]], rows: dict[str, tuple[str, str, str]] -> None",
    "verify_runner_text": "text: str, inventory: dict[str, list[str]] -> None",
    "verify_workflow_text": "text: str -> None",
    "verify_source_and_runner": "root: Path, inventory: dict[str, list[str]] -> None",
    "verify_events": "path: Path, expected: set[str], output: TextIO -> bool",
    "main": " -> int",
}


def nornic_cli_safe_value(name: str, value: ast.expr) -> bool:
    """Allow only the reviewed, import-safe verifier declarations."""
    expression = NORNIC_CLI_VALUE_EXPRESSIONS.get(name)
    if expression is not None:
        expected = ast.parse(expression, mode="eval").body
        return ast.dump(value) == ast.dump(expected)
    if name == "RUNNER_PREFIX":
        return (isinstance(value, ast.Constant) and isinstance(value.value, str)
                and hashlib.sha256(value.value.encode()).hexdigest()
                == "6c744ac43fb008902f9be6db4df1a39b0465fc128dd168bb4b355583335245af")
    expected = NORNIC_CLI_LITERAL_SETS.get(name)
    if (expected is None or not isinstance(value, ast.Call)
            or not isinstance(value.func, ast.Name)
            or value.func.id != "frozenset" or len(value.args) != 1
            or value.keywords or not isinstance(value.args[0], ast.Set)):
        return False
    items = value.args[0].elts
    return (all(isinstance(item, ast.Constant) and isinstance(item.value, str)
                for item in items)
            and len(items) == len(expected)
            and {item.value for item in items} == expected)


def verify_nornic_cli_activation(path: Path) -> None:
    """Bind the CLI proof path before importing its module."""
    source = path.read_text(encoding="utf-8")
    try:
        tree = ast.parse(source)
        canonical_main = ast.parse(NORNIC_CLI_MAIN).body[0]
        canonical_dispatch = ast.parse(NORNIC_CLI_DISPATCH).body[0]
    except SyntaxError as error:
        raise ValueError(f"invalid Nornic verifier Python syntax: {error}") from error
    mains = [node for node in tree.body
             if isinstance(node, ast.FunctionDef) and node.name == "main"]
    if len(mains) != 1 or ast.dump(mains[0], include_attributes=False) != ast.dump(
            canonical_main, include_attributes=False):
        raise ValueError("Nornic verifier main validation changed")
    if not tree.body or ast.dump(tree.body[-1], include_attributes=False) != ast.dump(
            canonical_dispatch, include_attributes=False):
        raise ValueError("Nornic verifier CLI dispatch changed")
    body = tree.body
    imports_end = 1 + len(NORNIC_CLI_IMPORTS)
    assignments_end = imports_end + len(NORNIC_CLI_ASSIGNMENTS)
    if (len(body) != assignments_end + len(NORNIC_CLI_FUNCTION_SIGNATURES) + 1
            or not isinstance(body[0], ast.Expr)
            or not isinstance(body[0].value, ast.Constant)
            or not isinstance(body[0].value.value, str)):
        raise ValueError("Nornic verifier has extra top-level execution")
    for node, expected in zip(body[1:imports_end], NORNIC_CLI_IMPORTS, strict=True):
        if not isinstance(node, (ast.Import, ast.ImportFrom)) or ast.unparse(node) != expected:
            raise ValueError("Nornic verifier imports changed")
    for node, name in zip(body[imports_end:assignments_end],
                          NORNIC_CLI_ASSIGNMENTS, strict=True):
        if (not isinstance(node, ast.Assign) or len(node.targets) != 1
                or not isinstance(node.targets[0], ast.Name)
                or node.targets[0].id != name
                or not nornic_cli_safe_value(name, node.value)):
            raise ValueError(f"Nornic verifier declaration changed: {name}")
    for node, (name, signature) in zip(
            body[assignments_end:-1], NORNIC_CLI_FUNCTION_SIGNATURES.items(), strict=True):
        if (not isinstance(node, ast.FunctionDef) or node.name != name
                or node.decorator_list or getattr(node, "type_params", ())
                or node.returns is None
                or f"{ast.unparse(node.args)} -> {ast.unparse(node.returns)}" != signature):
            raise ValueError(f"Nornic verifier function signature changed: {name}")
    if hashlib.sha256(source.encode()).hexdigest() != NORNIC_CLI_SOURCE_SHA256:
        raise ValueError("Nornic verifier source changed without reviewed binding")


try:
    verify_nornic_cli_activation(Path(__file__).with_name("nornic_conformance_events.py"))
except (OSError, ValueError) as error:
    sys.exit(f"Nornic verifier CLI activation failed: {error}")

from nornic_conformance_events import (
    load_inventory as load_nornic_inventory,
    verify_ledger as verify_nornic_ledger,
    verify_runner_text as verify_nornic_runner,
    verify_source as verify_nornic_source,
    verify_workflow_text as verify_nornic_workflow,
    verify_routing_text as verify_nornic_routes,
    RUNNER as NORNIC_RUNNER,
    WORKFLOW as NORNIC_WORKFLOW,
    STATIC_WORKFLOW as NORNIC_STATIC_WORKFLOW,
    REGISTRY as NORNIC_REGISTRY,
)

if len(sys.argv) != 3:
    sys.exit(f"usage: {sys.argv[0]} <ledger_path> <repo_root>")
ledger_path, repo_root = sys.argv[1], sys.argv[2]
try:
    text = load_ledger_text(Path(ledger_path))
except ValueError as error:
    sys.exit(str(error))

rows = re.findall(
    r"^  - file: (\S+)\n    tag: (.*)\n    class: (\S+)\n    reason: ([^\n]*)(?:\n    runner: (\S+))?(?:\n    backends: (\S+))?",
    text,
    re.M,
)
if not rows:
    sys.exit("no ledger rows parsed")

seen_files = set()
classes = {}
nornic_rows = {}
legacy_scheduled = []
contention_inventory = load_inventory()
contention_paths = set(contention_inventory)
contention_ledger_paths = set()
scheduled_inventory = load_scheduled_inventory()
scheduled_paths = set(scheduled_inventory)
verified_scheduled_paths = set()
ifa_scheduled_inventory = load_ifa_scheduled_inventory()
ifa_scheduled_paths = set(ifa_scheduled_inventory)
verified_ifa_scheduled_paths = set()
identity_ledger_paths = set()
for path, tag, cls, reason, runner, backends in rows:
    if path in seen_files:
        sys.exit(f"duplicate ledger row: {path}")
    seen_files.add(path)
    classes[path] = cls
    nornic_rows[path] = (cls, runner, backends)
    if cls not in ("ci", "postgres_ci", "dedicated_ci", "verified_scheduled", "scheduled", "retired"):
        sys.exit(f"invalid class {cls!r} for {path}")
    if cls == "postgres_ci":
        readiness = runner == "live-postgres-readiness" and tag.strip() == "~"
        migration = (
            runner == "live-postgres-migrations"
            and tag.strip() == "integration"
            and path in MIGRATION_PATHS
        )
        if not (readiness or migration) or backends:
            sys.exit(f"PostgreSQL CI row needs its verified runner and tag: {path}")
    elif cls == "dedicated_ci":
        if runner == "backend-conformance-nornicdb":
            if tag.strip() != "~" or backends != "nornicdb":
                sys.exit(f"Nornic conformance row needs untagged NornicDB file: {path}")
        elif runner == "reducer-contention":
            contention_ledger_paths.add(path)
            if path not in contention_paths or tag.strip() != "~" or backends:
                sys.exit(f"contention row needs an enrolled untagged PostgreSQL-only file: {path}")
        elif runner == "golden-corpus-identity":
            identity_ledger_paths.add(path)
            if path not in IDENTITY_PROOFS or tag.strip() != "~" or backends:
                sys.exit(f"B-7 identity row needs an enrolled untagged PostgreSQL-only file: {path}")
        elif runner != "query-methodology":
            sys.exit(f"dedicated CI row needs its verified runner: {path}")
        elif path.endswith("methodology_postgres_live_test.go"):
            if backends:
                sys.exit(f"PostgreSQL dedicated row cannot claim graph backends: {path}")
        elif backends != "neo4j":
            sys.exit(f"Neo4j dedicated row needs its truthful backend pin: {path}")
    elif cls == "verified_scheduled":
        if runner == "ifa-scheduled":
            if (path not in ifa_scheduled_paths
                    or tag.strip() != ifa_scheduled_inventory[path]["tag"]
                    or backends != "nornicdb"):
                sys.exit(f"Ifá scheduled row needs its exact runner, tag, and NornicDB backend: {path}")
            verified_ifa_scheduled_paths.add(path)
        else:
            if runner != "postgres-scheduled" or tag.strip() != "~":
                sys.exit(f"verified scheduled row needs its named runner and untagged file: {path}")
            if backends:
                sys.exit(f"verified scheduled row must be PostgreSQL-only: {path}")
            verified_scheduled_paths.add(path)
    elif runner:
        sys.exit(f"unexpected runner {runner!r} for {cls} row: {path}")
    if cls == "scheduled":
        legacy_scheduled.append(path)
    # Backend targeting defaults to both; a row naming a backend-specific
    # test pins the backends it can run on so the runner never schedules
    # a hardcoded-"nornic" test against Neo4j.
    if not backends and cls != "dedicated_ci":
        backends = "both"
    if backends and backends not in ("nornicdb", "neo4j", "both"):
        sys.exit(f"invalid backends {backends!r} for {path}")
    # A YAML `#` after whitespace opens a comment, so an unquoted reason
    # containing ` #` (or starting with `#`) is silently truncated (or
    # nulled) under a real YAML parser while this line regex sees the full
    # text. Fail loudly instead: quote the reason (#7425).
    if not reason.lstrip().startswith(('"', "'")) and (reason.lstrip().startswith("#") or re.search(r"\s#", reason)):
        sys.exit(f"unquoted '#' in reason for {path}: quote the full reason so YAML keeps it")
    if not reason.strip():
        sys.exit(f"blank reason for {path}")
    try:
        with open(os.path.join(repo_root, path)) as handle:
            src = handle.read()
    except OSError:
        src = ""
    build = re.search(r"^//go:build\s+(.+)$", src, re.M)
    actual = build.group(1).strip() if build else ""
    claimed = tag.strip().strip('"')
    if claimed == "~":
        claimed = ""
    if claimed != actual:
        sys.exit(f"tag mismatch for {path}: ledger {tag!r} vs file {actual!r}")

missing_contention = sorted(contention_paths - contention_ledger_paths)
extra_contention = sorted(contention_ledger_paths - contention_paths)
if missing_contention or extra_contention:
    sys.exit(
        "contention inventory/ledger mismatch: "
        f"missing={missing_contention[:5]} extra={extra_contention[:5]}"
    )
try:
    verify_inventory(Path(repo_root), contention_inventory)
except (OSError, ValueError, re.error) as error:
    sys.exit(f"reducer contention runner enrollment failed: {error}")
if verified_scheduled_paths != scheduled_paths:
    sys.exit(
        "verified scheduled rows differ from exact inventory: "
        f"missing={sorted(scheduled_paths - verified_scheduled_paths)}, "
        f"extra={sorted(verified_scheduled_paths - scheduled_paths)}"
    )
try:
    verify_scheduled_inventory(Path(repo_root), scheduled_inventory)
except (OSError, ValueError, re.error) as error:
    sys.exit(f"PostgreSQL scheduled runner enrollment failed: {error}")

if verified_ifa_scheduled_paths != ifa_scheduled_paths:
    sys.exit(
        "Ifá scheduled rows differ from exact inventory: "
        f"missing={sorted(ifa_scheduled_paths - verified_ifa_scheduled_paths)}, "
        f"extra={sorted(verified_ifa_scheduled_paths - ifa_scheduled_paths)}"
    )
try:
    verify_ifa_scheduled_inventory(Path(repo_root), ifa_scheduled_inventory)
except (OSError, ValueError, re.error) as error:
    sys.exit(f"Ifá scheduled runner enrollment failed: {error}")
if identity_ledger_paths != set(IDENTITY_PROOFS):
    sys.exit("B-7 identity inventory/ledger mismatch: "
             f"missing={sorted(set(IDENTITY_PROOFS) - identity_ledger_paths)} "
             f"extra={sorted(identity_ledger_paths - set(IDENTITY_PROOFS))}")
try:
    verify_identity(Path(repo_root))
    verify_identity_routes(Path(repo_root))
except (OSError, ValueError) as error:
    sys.exit(f"B-7 identity runner enrollment failed: {error}")
try:
    nornic_inventory = load_nornic_inventory()
    verify_nornic_ledger(nornic_inventory, nornic_rows)
    verify_nornic_source(Path(repo_root), nornic_inventory)
    verify_nornic_runner((Path(repo_root) / NORNIC_RUNNER).read_text(), nornic_inventory)
    verify_nornic_workflow((Path(repo_root) / NORNIC_WORKFLOW).read_text())
    verify_nornic_routes(
        (Path(repo_root) / NORNIC_WORKFLOW).read_text(),
        (Path(repo_root) / NORNIC_STATIC_WORKFLOW).read_text(),
        (Path(repo_root) / NORNIC_REGISTRY).read_text(),
    )
except (OSError, ValueError, re.error) as error:
    sys.exit(f"Nornic conformance runner enrollment failed: {error}")

# The ledger itself lists the exact grandfathered paths. Bind that readable
# inventory to a fixed digest so a newly scheduled row cannot inherit an
# exemption merely by carrying a plausible reason. Canonical input is sorted,
# unique, UTF-8 repo-relative paths with exactly one LF after each path.
legacy_policy = re.search(
    r"^legacy_scheduled_exemptions:\n"
    r"  baseline_main: ([0-9a-f]{40})\n"
    r"  steward: (\S+)\n"
    r"  tracking_issue: ([1-9][0-9]*)\n"
    r"  count: ([0-9]+)\n"
    r"  sha256: ([0-9a-f]{64})$",
    text,
    re.M,
)
if not legacy_policy:
    sys.exit("missing or invalid legacy scheduled exemption owner, issue, count, or digest")
expected_count = int(legacy_policy.group(4))
canonical_paths = sorted(legacy_scheduled)
actual_digest = hashlib.sha256(
    "".join(f"{path}\n" for path in canonical_paths).encode("utf-8")
).hexdigest()
if expected_count != len(canonical_paths) or actual_digest != legacy_policy.group(5):
    sys.exit(
        "legacy scheduled exemption inventory changed without reviewed disposition: "
        f"count={len(canonical_paths)} sha256={actual_digest}, "
        f"want count={expected_count} sha256={legacy_policy.group(5)}"
    )

# --cached plus --others: a brand-new live test must be at least present on
# disk to be classified. Untracked-but-ignored build output stays excluded
# via --exclude-standard, so only real source surprises surface.
tracked = subprocess.run(
    ["git", "-C", repo_root, "ls-files", "--others", "--exclude-standard",
     "--cached", "go/", "*.go"],
    capture_output=True,
    text=True,
    check=True,
).stdout.split()
live = sorted(p for p in tracked if p.endswith("_live_test.go"))
if not live:
    sys.exit("no live test files found; is REPO_ROOT a repo checkout?")

# A live test under a nonstandard filename evades the *_live_test.go rule:
# any tracked *_test.go carrying a live_* build-tag atom and defining a
# Test func is a live backend proof too and must be classified. Helper-only
# files (live tag, no Test funcs) compile into the test binary and are
# covered transitively through the files that use them.
nonstandard = []
for path in tracked:
    if not path.endswith("_test.go") or path.endswith("_live_test.go"):
        continue
    try:
        with open(os.path.join(repo_root, path)) as handle:
            src = handle.read()
    except OSError:
        continue
    build = re.search(r"^//go:build\s+(.+)$", src, re.M)
    if not build:
        continue
    atoms = set(re.findall(r"[A-Za-z0-9_]+", build.group(1)))
    if not any(a.startswith("live_") for a in atoms):
        continue
    if re.search(r"^func Test\w+\(", src, re.M):
        nonstandard.append(path)
live = sorted(set(live) | set(nonstandard))

# Unclassified live tests surface before stale rows: in a sparse fixture
# tree the planted violation must read as unclassified, not as a row
# naming a file the fixture does not contain.
missing = [p for p in live if p not in seen_files]
if missing:
    sys.exit(f"{len(missing)} unclassified live test(s), first: {missing[:5]}")

for path in sorted(seen_files):
    full = os.path.join(repo_root, path)
    if not os.path.isfile(full):
        sys.exit(f"ledger row names missing file: {path}")

# A retired row names a file that was a live proof and was repaired in
# place, so it still exists but no longer meets the live definition. A
# row for a deleted file is removed in the deleting commit instead: the
# missing-file check above keeps typos fail-closed. Retiring a file that
# is still live hides a live proof, so it fails: repair it first.
still_live = [p for p in live if classes.get(p) == "retired"]
if still_live:
    sys.exit(f"{len(still_live)} retired row(s) name still-live file(s), first: {still_live[:5]}")

retired = {p for p, cls in classes.items() if cls == "retired"}
extra = sorted(seen_files - set(live) - retired)
if extra:
    sys.exit(f"{len(extra)} ledger row(s) name non-live or untracked files, first: {extra[:5]}")

print(f"live-tests ledger ok: {len(rows)} rows, {len(live)} live files classified")
