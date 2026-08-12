#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

(
	cd "${repo_root}/go"
	# shellcheck source=scripts/lib/go-test-run-guard.sh
	. "${repo_root}/scripts/lib/go-test-run-guard.sh"
	go test ./internal/queryplan -count=1
	# go_test_run_guard (#6055) asserts the pattern still matches both named
	# tests before running them, so a rename or move that drops the match
	# count to zero fails loudly instead of the bare `go test -run` exiting 0
	# on nothing.
	go_test_run_guard 2 '^(TestHandlerQueryplanManifestBindsProductionBuilders|TestLegacyQueryplanManifestBindsProductionQueries)$' \
		-- ./internal/query -count=1
)

"${repo_root}/scripts/verify-query-plan-profile.sh"

printf 'verify-query-plan-regression: pass\n'
