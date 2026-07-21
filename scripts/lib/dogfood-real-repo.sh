#!/usr/bin/env bash
#
# dogfood-real-repo.sh - shared runner sourced by the per-language
# scripts/dogfood-<lang>.sh wrappers (#5399). Each wrapper names its
# language key and the Go test that proves it: a standing regression test
# under go/internal/parser/<lang>/dogfood_real_repo_test.go that parses the
# committed, app-shaped corpus at tests/fixtures/dogfood/<lang>_real_repo
# and diffs the parser's bucket counts against a checked-in snapshot at
# go/internal/parser/<lang>/testdata/dogfood_real_repo_snapshot.txt.
#
# This is the "real-repo-validated" bar defined in
# docs/public/languages/support-maturity.md#grade-definitions: a committed
# scripts/ dogfood script plus a checked-in expected-output snapshot, fully
# offline-reproducible (a plain `go test` run, zero network calls, zero
# Docker). Any external repository named in a fixture's own header comment
# is recorded there as provenance metadata only -- it is never fetched by
# this script or the test it runs.
#
# Usage: sourced only, never executed directly. Callers set DOGFOOD_UPDATE_SNAPSHOT=1
# before invoking a wrapper to regenerate that language's snapshot after an
# intentional parser change.
set -euo pipefail

run_dogfood_real_repo() {
	local lang_key="$1"  # e.g. dart, scala, swift, java
	local test_name="$2" # e.g. TestDogfoodDartRealRepoSnapshot
	local repo_root
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	local corpus_dir="${repo_root}/tests/fixtures/dogfood/${lang_key}_real_repo"

	if [[ ! -d "${corpus_dir}" ]]; then
		echo "dogfood-${lang_key}: missing committed corpus at ${corpus_dir}" >&2
		exit 1
	fi

	if [[ "${DOGFOOD_UPDATE_SNAPSHOT:-}" == "1" ]]; then
		echo "dogfood-${lang_key}: regenerating testdata/dogfood_real_repo_snapshot.txt from ${corpus_dir}" >&2
	else
		echo "dogfood-${lang_key}: verifying ${test_name} against ${corpus_dir} (offline, no network)" >&2
	fi

	(
		cd "${repo_root}/go"
		go test "./internal/parser/${lang_key}/..." -run "^${test_name}\$" -v -count=1
	)
}
