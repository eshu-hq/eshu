#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_FACTSCHEMA_DIFF_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

go_dir="${ESHU_FACTSCHEMA_DIFF_GO_DIR:-${repo_root}/go}"
schema_dir="${ESHU_FACTSCHEMA_DIFF_SCHEMA_DIR:-}"

extra_args=("-repo-root" "$repo_root")
if [ -n "$schema_dir" ]; then
  extra_args+=("-schema-dir" "$schema_dir")
fi

cd "$go_dir"
go run ./cmd/factschema-diff "${extra_args[@]}" "$@"
