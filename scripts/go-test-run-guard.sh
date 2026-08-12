#!/usr/bin/env bash
#
# go-test-run-guard.sh — standalone CLI entry point for
# scripts/lib/go-test-run-guard.sh's go_test_run_guard, for callers that
# cannot source a bash function: specs/ci-gates.v1.yaml `local.command`
# strings and GitHub Actions `run:` steps (#6055).
#
# Usage (run from the go/ module root, or set ESHU_GO_TEST_RUN_GUARD_DIR):
#   scripts/go-test-run-guard.sh <min_matches> <run_pattern> -- <go-test-args...>
#
# Example (mirrors mcp-schema-drift.yml's ReadOnlyTools count check):
#   scripts/go-test-run-guard.sh 1 '^TestReadOnlyTools$' -- ./internal/mcp/ -count=1
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/go-test-run-guard.sh
. "${repo_root}/scripts/lib/go-test-run-guard.sh"

go_dir="${ESHU_GO_TEST_RUN_GUARD_DIR:-${repo_root}/go}"
cd "${go_dir}"

go_test_run_guard "$@"
