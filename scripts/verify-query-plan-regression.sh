#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

(
	cd "${repo_root}/go"
	go test ./internal/queryplan -count=1
	go test ./internal/query \
		-run '^TestHandlerQueryplanManifestBindsProductionBuilders$' -count=1
)

"${repo_root}/scripts/verify-query-plan-profile.sh"

printf 'verify-query-plan-regression: pass\n'
