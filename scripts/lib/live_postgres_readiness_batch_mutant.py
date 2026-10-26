#!/usr/bin/env python3
"""Build disposable mutants for the PostgreSQL readiness self-test seeds."""

from __future__ import annotations

import pathlib
import sys


RUNNER_ANCHOR = 'run_runner() { bash "${runner}" 2>&1; }\n'
RUNNER_REPLACEMENT = r'''export ESHU_SEED_FIRST_KEY="$(pkg_key "${first_pkg}")"
export ESHU_SEED_LAST_KEY="$(pkg_key "${last_pkg}")"
export ESHU_SEED_MIDDLE="${packages[$((${#packages[@]} / 2))]}"
export ESHU_SEED_REAL_PYTHON="$(command -v python3)"
"${ESHU_SEED_REAL_PYTHON}" "${repo_root}/scripts/lib/live_postgres_readiness_batch_mutant.py" \
  write-wrapper-file "${seed_dir}/bin/python3" || fail "batch wrapper could not be written"
chmod +x "${seed_dir}/bin/python3"
run_runner() { bash "${runner}" 2>&1; }
'''
OLD_ORDER = """check_each_proof

# Both grouped package faults report every package. Their seeded mutants
# exercise a middle package without changing the isolated per-proof checks.
check_batched_package_failures
"""
NEW_ORDER = """check_batched_package_failures

# The isolated proof cases remain after the targeted disposable mutation.
check_each_proof
"""
CALLBACK_ANCHOR = (
    'printf \'%s\\n\' "${pkg}" >>"${ESHU_FAKE_DIR}/calls"; '
    'declare -A enrolled; matched=0\n'
)
CALLBACK_REPLACEMENT = r'''if [[ "${pkg}" == "${ESHU_SEED_MIDDLE}" ]] &&
  ! rg -qF '"Test":' "${ESHU_FAKE_DIR}/${ESHU_SEED_FIRST_KEY}.jsonl" &&
  ! rg -qF '"Test":' "${ESHU_FAKE_DIR}/${ESHU_SEED_LAST_KEY}.jsonl"; then
  : # Suppress only the middle callback in the all-package zero-test case.
else
  printf '%s\n' "${pkg}" >>"${ESHU_FAKE_DIR}/calls"
fi
declare -A enrolled; matched=0
'''
WRAPPER = r'''#!/usr/bin/env bash
set -u
if [[ $# -ge 4 && ${2:-} == verify-results && ${4:-} == "${ESHU_SEED_MIDDLE}" ]] &&
  rg -qF '"Action":"fail","Package"' "${ESHU_FAKE_DIR}/${ESHU_SEED_FIRST_KEY}.jsonl" &&
  rg -qF '"Action":"fail","Package"' "${ESHU_FAKE_DIR}/${ESHU_SEED_LAST_KEY}.jsonl"; then
  output="$("${ESHU_SEED_REAL_PYTHON}" "$@" 2>&1)"
  status=$?
  [[ ${SEED_MUTANT_KIND:-} == status ]] && status=0
  if [[ ${SEED_MUTANT_KIND:-} == diagnostic ]]; then
    output="$(printf '%s\n' "${output}" | rg -vF "package terminal (${ESHU_SEED_MIDDLE})" || true)"
  fi
  printf '%s\n' "${output}"
  exit "${status}"
fi
exec "${ESHU_SEED_REAL_PYTHON}" "$@"
'''


def replace_once(source: str, old: str, new: str) -> str:
    """Replace a unique fixture anchor, failing closed when it drifts.

    Args:
        source: Fixture source to mutate.
        old: Required original fragment.
        new: Mutant fragment.

    Returns:
        The source with its unique anchor replaced.

    Raises:
        ValueError: The anchor is missing or duplicated.
    """
    if source.count(old) != 1:
        raise ValueError("batch mutant anchor missing or duplicated")
    return source.replace(old, new)


def mutate_target(path: pathlib.Path, kind: str) -> None:
    """Write one disposable self-test mutation while preserving all checks.

    Args:
        path: The copied self-test script to mutate.
        kind: The seeded failure mode: status, diagnostic, or callback.

    Raises:
        ValueError: The kind or an expected source anchor is invalid.
    """
    if kind not in {"status", "diagnostic", "callback"}:
        raise ValueError(f"unknown batch mutant kind: {kind}")
    source = path.read_text(encoding="utf-8")
    source = replace_once(source, RUNNER_ANCHOR, RUNNER_REPLACEMENT)
    # The disposable copy reaches its targeted fault before the long isolated
    # proof phase. The actual self-test retains its original check order.
    source = replace_once(source, OLD_ORDER, NEW_ORDER)
    if kind == "callback":
        source = replace_once(source, CALLBACK_ANCHOR, CALLBACK_REPLACEMENT)
    path.write_text(source, encoding="utf-8")


def main(args: list[str]) -> int:
    """Dispatch the mutation or write the wrapper used by a copied self-test."""
    if len(args) == 3 and args[0] == "mutate-target":
        mutate_target(pathlib.Path(args[1]), args[2])
        return 0
    if len(args) == 2 and args[0] == "write-wrapper-file":
        pathlib.Path(args[1]).write_text(WRAPPER, encoding="utf-8")
        return 0
    print("usage: batch_mutant.py mutate-target PATH KIND | write-wrapper-file PATH", file=sys.stderr)
    return 2


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except (OSError, ValueError) as error:
        print(f"batch mutant: {error}", file=sys.stderr)
        sys.exit(1)
