#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_FACT_KIND_REGISTRY_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

go_dir="${ESHU_FACT_KIND_REGISTRY_GO_DIR:-${repo_root}/go}"
spec="${ESHU_FACT_KIND_REGISTRY_SPEC:-${repo_root}/specs/fact-kind-registry.v1.yaml}"

cd "$go_dir"
go run ./cmd/fact-kind-registry \
  -repo-root "$repo_root" \
  -spec "$spec" \
  -check
