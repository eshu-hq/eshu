#!/usr/bin/env bash
# Scan-scope cases for verify-openapi.sh's HandleFunc file collection
# (#5762 round 8, P1-2).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's setup_repo() and run_verifier() from scripts/test-verify-openapi.sh,
# the same way the two known-drift case files do.
#
# The collection line is three globs on one `rg --files` call:
#
#   rg --files --max-depth 1 -g '*.go' -g '!*_test.go' -g '!openapi_*.go' "$dir"
#
# Round 7 fixtured only `!*_test.go` and argued the other two were equivalent
# mutants not worth a fixture. Both arguments were wrong. The repo has 11
# non-"openapi_paths_*.go" files whose names start with "openapi_" (run
# `rg --files -g 'openapi_*.go' -g '!*_test.go' -g '!openapi_paths_*.go'
# go/internal/query | wc -l`), so `!openapi_*.go` excludes real files today,
# and both flags are trivially fixturable on a synthetic repo. The two cases
# below are the fixtures; each was proven by dropping its flag from a copy of
# the verifier and watching only that case go red.

# ══════════════════════════════════════════════════════════════════════════════
# Test 10c — green: a HandleFunc registration in a non-"openapi_paths_" file
# whose name starts with "openapi_" is excluded from the scan. Those files hold
# OpenAPI component/schema string constants, not route registrations; scanning
# them would make a fragment that merely quotes a route look like a second
# registration of it. Dropping `-g '!openapi_*.go'` makes this fixture exit 1.
test_scan_excludes_openapi_prefixed_files_green() {
  local dir
  dir="$(setup_repo "openapi-prefixed-excluded")"

  cat > "${dir}/go/internal/query/openapi_helpers.go" << 'GOEOF'
package query

import "net/http"

func mountHelper(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/should-not-be-scanned-helper", func(w http.ResponseWriter, _ *http.Request) {})
}
GOEOF

  run_verifier "$dir" "HandleFunc inside an openapi_*.go file is excluded from the scan, exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 10d — green: a HandleFunc registration in a subpackage of a scan dir is
# excluded. A subpackage owns its own routes and its own OpenAPI fragments (or
# none), so pulling its registrations into this package's drift diff reports
# routes the package never registered. Dropping `--max-depth 1` makes this
# fixture exit 1.
test_scan_excludes_subdirectories_green() {
  local dir
  dir="$(setup_repo "subdirectory-excluded")"

  mkdir -p "${dir}/go/internal/query/sub"
  cat > "${dir}/go/internal/query/sub/h.go" << 'GOEOF'
package sub

import "net/http"

func Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/should-not-be-scanned-sub", func(w http.ResponseWriter, _ *http.Request) {})
}
GOEOF

  run_verifier "$dir" "HandleFunc inside a scan-dir subpackage is excluded from the scan, exits 0" "pass"
}
