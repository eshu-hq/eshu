"""Validate the Nornic CLI source before importing its verifier."""

from __future__ import annotations

import ast
import hashlib
from pathlib import Path

NORNIC_CLI_SOURCE_SHA256 = "05b25b2d14a12e07cae607b487608786093d8d50ee98b125cb3b782b21d0c136"
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
        return (
            0
            if verify_events(args.events, names, PACKAGES, sys.stderr)
            else 1
        )
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
    "STATIC_WORKFLOW", "REGISTRY", "PACKAGES", "RUNNER_PACKAGES",
    "TEST_PATTERN", "EXPECTED_FILES", "ROUTED_PATHS",
)
NORNIC_CLI_VALUE_EXPRESSIONS = {
    "ROOT": "Path(__file__).resolve().parents[2]",
    "INVENTORY": 'Path(__file__).with_name("nornic_conformance_inventory.json")',
    "RUNNER": 'Path("scripts/verify_backend_conformance_live.sh")',
    "WORKFLOW": 'Path(".github/workflows/e2e-tests.yml")',
    "STATIC_WORKFLOW": 'Path(".github/workflows/live-backend-tests.yml")',
    "REGISTRY": 'Path("specs/ci-gates.d/gates-09.yaml")',
    "TEST_PATTERN": r're.compile(r"^func (Test\w+)\(", re.MULTILINE)',
}
NORNIC_CLI_LITERAL_SETS = {
    "EXPECTED_FILES": {
        "go/internal/storage/cypher/cloud/resource/node_writer_heterogeneous_batch_live_test.go",
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
        "scripts/lib/verify-live-tests-ledger.py",
        "scripts/lib/nornic_cli_activation_guard.py",
        "scripts/test-nornic-ledger-guard-boundary.py",
        "specs/live-tests.v1.yaml",
        "specs/live-tests.d/**", ".github/workflows/e2e-tests.yml",
        ".github/workflows/live-backend-tests.yml",
        "specs/ci-gates.d/gates-09.yaml",
    },
    "PACKAGES": {
        "github.com/eshu-hq/eshu/go/internal/storage/cypher",
        "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/resource",
    },
    "RUNNER_PACKAGES": {
        "./internal/storage/cypher",
        "./internal/storage/cypher/cloud/resource",
    },
}
NORNIC_CLI_FUNCTION_SIGNATURES = {
    "_active_lines": "source: str -> list[tuple[int, str]]",
    "_block": "lines: list[tuple[int, str]], key: str, depth: int -> list[tuple[int, str]]",
    "_paths": "lines: list[tuple[int, str]], depth: int -> set[str]",
    "_workflow_paths": "source: str, static: bool, fragment: str -> set[str]",
    "_fragment_narrowings": "fragment: str -> set[str]",
    "_registry_paths": "source: str -> set[str]",
    "_static_step": "source: str -> None",
    "verify_routing_text": "e2e: str, live: str, registry: str, e2e_fragment: str | None=None, live_fragment: str | None=None -> None",
    "load_inventory": "path: Path=INVENTORY -> dict[str, list[str]]",
    "verify_source": "root: Path, inventory: dict[str, list[str]] -> None",
    "read_ledger_rows": "root: Path -> dict[str, tuple[str, str, str]]",
    "verify_ledger": "inventory: dict[str, list[str]], rows: dict[str, tuple[str, str, str]] -> None",
    "verify_source_and_runner": "root: Path, inventory: dict[str, list[str]] -> None",
    "verify_events": "path: Path, expected: set[str], packages: frozenset[str], output: TextIO -> bool",
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



def verify_nornic_enrollment(
    root: Path, rows: dict[str, tuple[str, str, str]], source_path: Path
) -> None:
    """Check the Nornic CLI before its module import and validate its routes."""
    try:
        verify_nornic_cli_activation(source_path)
    except (OSError, ValueError) as error:
        raise ValueError(f"Nornic verifier CLI activation failed: {error}") from error
    from nornic_conformance_events import (
        load_inventory, verify_ledger, verify_source, verify_routing_text,
        RUNNER, WORKFLOW, STATIC_WORKFLOW, REGISTRY,
    )
    from nornic_conformance_runner import (
        verify_runner_text, verify_workflow_text,
    )
    inventory = load_inventory()
    verify_ledger(inventory, rows)
    verify_source(root, inventory)
    verify_runner_text((root / RUNNER).read_text(), inventory)
    verify_workflow_text((root / WORKFLOW).read_text(),
                         (root / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text())
    verify_routing_text(
        (root / WORKFLOW).read_text(),
        (root / STATIC_WORKFLOW).read_text(),
        (root / REGISTRY).read_text(),
        (root / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text(),
        (root / "specs/ci-pr-workflows.d/live-backend-tests.yml").read_text(),
    )
