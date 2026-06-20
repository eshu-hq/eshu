#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

(
	cd "${repo_root}/go"
	go test ./internal/queryplan -count=1
)

printf 'verify-query-plan-regression: pass\n'
