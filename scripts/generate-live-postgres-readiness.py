#!/usr/bin/env python3
"""Derive the live-postgres-readiness registration from test-file markers.

Adding a live test no longer hand-edits shared files: each enrolled test
file carries its own enrollment marker, and this generator renders every
shared surface from the markers. Re-run it after any marker change
(re-derive on rebase, never hand-edit generated output); the drift gate
fails on any difference. See live_postgres_readiness_gen_scan.py for the
marker contract.

Generated outputs (LC_ALL=C byte order, idempotent):
    specs/live-tests.d/live-tests-readiness.generated.yaml
    specs/live-tests.v1.yaml (fragments entry only; prose stays hand-owned)
    scripts/lib/live_postgres_readiness_inventory_gen_<slug>.py (per package)
    scripts/lib/live_postgres_readiness_inventory_generated.py (PACKAGES)
    scripts/lib/live_postgres_readiness_inventory_helpers.py (sha pins)
    scripts/lib/live_postgres_readiness_env.sh (runner/mirror env lists)
    .github/workflows/live-postgres-readiness.yml (marked env block only)

Hermetic: stdlib only, no Go toolchain, no network, no containers.
Usage: generate-live-postgres-readiness.py [--root DIR] [--check]
"""

import argparse
import pathlib
import sys

LIB = pathlib.Path(__file__).parent / "lib"
sys.path.insert(0, str(LIB))

from live_postgres_readiness_gen_scan import (  # noqa: E402
    check_env_honesty,
    collect,
    fail,
    required_vars,
)
from live_postgres_readiness_gen_render import (  # noqa: E402
    generated_outputs,
    mutate_fragments_block,
    mutate_workflow,
    render_workflow_block,
)


def plan(root: pathlib.Path) -> list[tuple[str, str]]:
    """Compute every generated surface's full content."""
    enrolled, binding = collect(root)
    if not enrolled:
        raise fail("no enrolled readiness tests found")
    check_env_honesty(root, enrolled)
    required, optional = required_vars(enrolled)
    outputs = generated_outputs(root, enrolled, binding, required, optional)
    try:
        v1 = root.joinpath("specs/live-tests.v1.yaml").read_text(
            encoding="utf-8")
    except OSError as error:
        raise fail(f"specs/live-tests.v1.yaml unreadable: {error}") from error
    outputs.append(("specs/live-tests.v1.yaml", mutate_fragments_block(v1)))
    try:
        workflow = root.joinpath(
            ".github/workflows/live-postgres-readiness.yml").read_text(
                encoding="utf-8")
    except OSError as error:
        raise fail(f"workflow unreadable: {error}") from error
    interior = render_workflow_block(required, optional, enrolled)
    outputs.append((".github/workflows/live-postgres-readiness.yml",
                    mutate_workflow(workflow, interior)))
    return outputs


def main() -> int:
    """Render or check every generated registration surface."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=pathlib.Path, default=None)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve() if args.root else pathlib.Path(
        __file__).resolve().parents[1]
    try:
        outputs = plan(root)
    except ValueError as error:
        print(f"generate-live-postgres-readiness: {error}", file=sys.stderr)
        return 1
    if args.check:
        for rel, content in outputs:
            try:
                current = root.joinpath(rel).read_text(encoding="utf-8")
            except OSError:
                current = None
            if current != content:
                print(f"generate-live-postgres-readiness: stale {rel}; "
                      f"regenerate with python3 "
                      f"scripts/generate-live-postgres-readiness.py",
                      file=sys.stderr)
                return 1
        planned = {rel for rel, _ in outputs}
        for path in sorted(root.joinpath(
                "scripts/lib").glob(
                    "live_postgres_readiness_inventory_gen_*.py")):
            rel = path.relative_to(root).as_posix()
            if rel not in planned:
                print(f"generate-live-postgres-readiness: orphan {rel}; "
                      f"regenerate with python3 "
                      f"scripts/generate-live-postgres-readiness.py",
                      file=sys.stderr)
                return 1
        print(f"generate-live-postgres-readiness: registration current "
              f"({len(outputs)} surfaces)")
        return 0
    planned = {rel for rel, _ in outputs}
    for rel, content in outputs:
        root.joinpath(rel).write_text(content, encoding="utf-8")
    for path in sorted(root.joinpath(
            "scripts/lib").glob(
                "live_postgres_readiness_inventory_gen_*.py")):
        rel = path.relative_to(root).as_posix()
        if rel not in planned:
            path.unlink()
            print(f"generate-live-postgres-readiness: removed orphan {rel}")
    print(f"generate-live-postgres-readiness: regenerated {len(outputs)} "
          f"surfaces")
    return 0


if __name__ == "__main__":
    sys.exit(main())
