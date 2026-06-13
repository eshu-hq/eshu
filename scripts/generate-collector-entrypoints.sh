#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

go_dir="${ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR:-${repo_root}/go}"
manifest="${ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST:-${repo_root}/go/internal/collector/entrypoints/collector_entrypoints.yaml}"

cd "$go_dir"
go run ./internal/collector/entrypoints/cmd/collector-entrypoints-gen \
  -repo-root "$repo_root" \
  -manifest "$manifest"
