#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_CONTRACTTEST_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

go_dir="${ESHU_CONTRACTTEST_GO_DIR:-${repo_root}/go}"
spec="${ESHU_CONTRACTTEST_SPEC:-${repo_root}/specs/collector_fact_contract.v1.yaml}"

cd "$go_dir"
go run ./internal/collector/contracttest/gen \
  -repo-root "$repo_root" \
  -spec "$spec"

gofmt -w "${repo_root}/go/internal/collector/contracttest/contract_data.go"
