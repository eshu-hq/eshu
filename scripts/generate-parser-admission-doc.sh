#!/usr/bin/env bash
# Regenerates the parser admission table in
# docs/public/languages/admitted-files.md from the parser registry.
# Pass --check to fail on drift instead of writing.
set -euo pipefail

repo_root="${ESHU_PARSER_ADMISSION_DOC_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

go_dir="${ESHU_PARSER_ADMISSION_DOC_GO_DIR:-${repo_root}/go}"
args=()
if [ "${1:-}" = "--check" ]; then
  args+=(-check)
fi

cd "$go_dir"
go run ./cmd/parser-admission-doc -repo-root "$repo_root" ${args[@]+"${args[@]}"}
