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


from verify_live_tests_nornic import (
    load_nornic_inventory,
    verify_nornic_ledger,
    verify_nornic_runner,
    verify_nornic_source,
    verify_nornic_workflow,
    verify_nornic_routes,
    NORNIC_RUNNER,
    NORNIC_WORKFLOW,
    NORNIC_STATIC_WORKFLOW,
    NORNIC_REGISTRY,
)
if len(sys.argv) != 3:
    sys.exit(f"usage: {sys.argv[0]} <ledger_path> <repo_root>")
ledger_path, repo_root = sys.argv[1], sys.argv[2]
# Check the proof subjects before importing their module. A top-level successful
# exit in that module must not bypass the ledger's independent source read.
REPLAY_PROOF_SOURCE_SHA256 = {
    "scripts/verify-replay-tier.sh": (
        "c444a52095236b608cda143bf4bdbbedc9302e9ed23f4980bcdefeac8f81056b"
    ),
    "scripts/lib/replay_tier_enrollment.py": (
        "9a588e169c81abc96b363adced5c60918b76b9ac7bfdcda210939121880d6822"
    ),
    "scripts/lib/replay_tier_routes.py": (
        "63fe0db747ce2039cb0cb6412cfd88bd6ec91dc1b869080d5ca41ef3dbbc75b4"
    ),
}
replay_sources: dict[str, str] = {}
for name, expected_digest in REPLAY_PROOF_SOURCE_SHA256.items():
    try:
        source = (Path(repo_root) / name).read_bytes()
    except OSError as error:
        sys.exit(f"replay proof source integrity read failed: {name}: {error}")
    actual_digest = hashlib.sha256(source).hexdigest()
    if actual_digest != expected_digest:
        sys.exit(f"replay proof source integrity mismatch: {name}")
    if name.endswith(".py"):
        replay_sources[name] = source.decode("utf-8")

# Importing the CLI also imports its local route checker. Both modules must
# have inert declarations before either can run and vacate the ledger check.
def _safe_replay_value(node: ast.AST, declared: set[str]) -> bool:
    """Allow only import-time inert constants and reviewed string splitting."""
    if isinstance(node, ast.Constant):
        return True
    if isinstance(node, ast.Name):
        return node.id in declared
    if isinstance(node, ast.BinOp):
        return (isinstance(node.op, ast.Add)
                and _safe_replay_value(node.left, declared)
                and _safe_replay_value(node.right, declared))
    if isinstance(node, (ast.Tuple, ast.List, ast.Set)):
        return all(_safe_replay_value(item, declared) for item in node.elts)
    if isinstance(node, ast.Dict):
        return all(key is not None and _safe_replay_value(key, declared)
                   and _safe_replay_value(value, declared)
                   for key, value in zip(node.keys, node.values))
    # The route module's two table constants split a literal, not a computed
    # object. No other call is allowed during import.
    if (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
            and not node.args and not node.keywords):
        inner = node.func.value
        if (node.func.attr == "splitlines" and isinstance(inner, ast.Call)
                and isinstance(inner.func, ast.Attribute)
                and inner.func.attr == "strip"
                and isinstance(inner.func.value, ast.Constant)
                and isinstance(inner.func.value.value, str)
                and len(inner.args) == 1 and not inner.keywords
                and isinstance(inner.args[0], ast.Constant)
                and inner.args[0].value == "\n"):
            return True
    return False


def _verified_replay_tree(
    source: str, expected_imports: str, has_dispatch: bool
) -> ast.Module:
    """Reject imports and declarations that can execute before validation."""
    try:
        tree = ast.parse(source)
    except SyntaxError as error:
        sys.exit(f"replay proof source integrity syntax error: {error}")
    imports = ast.parse(expected_imports).body
    prefix = tree.body[:len(imports) + 1]
    if (len(prefix) != len(imports) + 1
            or not isinstance(prefix[0], ast.Expr)
            or not isinstance(prefix[0].value, ast.Constant)
            or not isinstance(prefix[0].value.value, str)
            or [ast.dump(node) for node in prefix[1:]]
            != [ast.dump(node) for node in imports]):
        sys.exit("replay proof source integrity imports are not closed")
    declarations = tree.body[len(prefix):-1] if has_dispatch else tree.body[len(prefix):]
    declared = set()
    for node in declarations:
        if isinstance(node, ast.FunctionDef):
            if (node.decorator_list or node.args.defaults
                    or any(value is not None for value in node.args.kw_defaults)
                    or getattr(node, "type_params", ())):
                sys.exit("replay proof source integrity has executable function signature")
            continue
        if (isinstance(node, ast.Assign) and len(node.targets) == 1
                and isinstance(node.targets[0], ast.Name)
                and node.targets[0].id.isupper()
                and _safe_replay_value(node.value, declared)):
            declared.add(node.targets[0].id)
            continue
        sys.exit("replay proof source integrity has executable top-level code")
    return tree


_verified_replay_tree(replay_sources["scripts/lib/replay_tier_routes.py"], """
from __future__ import annotations
from pathlib import Path
import re
""", False)
replay_tree = _verified_replay_tree(
    replay_sources["scripts/lib/replay_tier_enrollment.py"], """
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
""", True)
dispatch = replay_tree.body[-1]
expected_test = ast.parse('__name__ == "__main__"', mode="eval").body
expected_exit = ast.parse("SystemExit(main())", mode="eval").body
if (not isinstance(dispatch, ast.If) or dispatch.orelse
        or ast.dump(dispatch.test) != ast.dump(expected_test)
        or len(dispatch.body) != 1 or not isinstance(dispatch.body[0], ast.Raise)
        or not isinstance(dispatch.body[0].exc, ast.Call)
        or ast.dump(dispatch.body[0].exc) != ast.dump(expected_exit)):
    sys.exit("replay proof source integrity CLI dispatch is inactive")
main_functions = [node for node in replay_tree.body
                  if isinstance(node, ast.FunctionDef) and node.name == "main"]
if (len(main_functions) != 1 or len(main_functions[0].body) < 2
        or not isinstance(main_functions[0].body[-2], ast.Try)
        or not isinstance(main_functions[0].body[-1], ast.Return)
        or not isinstance(main_functions[0].body[-1].value, ast.Constant)
        or main_functions[0].body[-1].value.value != 0
        or any(isinstance(node, (ast.If, ast.For, ast.While, ast.With,
                                 ast.Return, ast.Raise, ast.Try))
               for node in main_functions[0].body[:-2])):
    sys.exit("replay proof source integrity main returns before validation")
for statement in main_functions[0].body[:-2]:
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
    sys.exit("replay proof source integrity main has an early exit")

from replay_tier_enrollment import load_inventory as load_replay_inventory  # noqa: E402
from replay_tier_enrollment import verify_ledger as verify_replay_ledger  # noqa: E402

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
try:
    replay_inventory = load_replay_inventory(Path(repo_root))
except (OSError, ValueError) as error:
    sys.exit(f"replay-tier enrollment failed: {error}")
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
        elif runner == "replay-tier":
            if path not in replay_inventory["files"] or tag.strip() != "~" or backends != "nornicdb":
                sys.exit(f"replay-tier row needs an enrolled untagged NornicDB file: {path}")
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

try:
    verify_replay_ledger(rows, replay_inventory)
except ValueError as error:
    sys.exit(str(error))

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
