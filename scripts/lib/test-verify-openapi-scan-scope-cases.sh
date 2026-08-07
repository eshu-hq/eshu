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

# ══════════════════════════════════════════════════════════════════════════════
# Test 10e — red: an rg hard error (exit 2) while collecting Go files from a
# scan dir must fail the gate closed, not silently scan zero files (#5934
# review). The collection loop used to blanket "|| true" this rg call, which
# swallows exit 1 (expected: "no matching files in this dir") and exit 2 (a
# hard error: unreadable directory, a rejected flag) identically -- so a
# corrupted or unreadable scan silently produced an empty Go file list, and
# the gate reported "OpenAPI surface clean" even though the undocumented
# route below sat right there, unscanned, because scanning it never actually
# ran. Proven with a PATH-shimmed rg that exits 2 whenever invoked with
# "--files" -- the collection step's own signature. Every other rg call in
# this script uses "-o" extraction instead, so the shim never touches them
# (confirmed: `rg -n -- '--files' scripts/verify-openapi.sh` matches exactly
# the one line this test targets). Reaches directly into $tmp_root and
# $verifier (defined by the sourcing script) the same way the known-drift
# hard-error tests in the sibling cases file do, because this needs a shimmed
# PATH that run_verifier() has no parameter for.
# shellcheck disable=SC2154 # tmp_root, verifier: defined by the sourcing script
test_scan_dir_rg_hard_error_fails_closed_red() {
  local dir shim_dir verifier_tmp code real_rg

  dir="$(setup_repo "scan-dir-rg-hard-error")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("POST /api/v0/items", h.createItem)' \
    '}'
  # Deliberately no openapi_paths_*.go file: with a working scan, the
  # undocumented "POST /api/v0/items" route is exactly what MISSING_OPENAPI
  # exists to catch. If the scan silently sees zero files instead, there is
  # nothing left to cross-reference on either side and the gate goes green.

  shim_dir="${tmp_root}/rg-shim-scan-dir-hard-error"
  mkdir -p "$shim_dir"
  real_rg="$(command -v rg)"
  # The single-quoted lines below are literal shell source being written to
  # the shim file, not variables to expand in THIS script.
  # shellcheck disable=SC2016
  {
    printf '#!/usr/bin/env bash\n'
    printf 'for arg in "$@"; do\n'
    printf '  [ "$arg" = "--files" ] && exit 2\n'
    printf 'done\n'
    printf 'exec %q "$@"\n' "$real_rg"
  } > "${shim_dir}/rg"
  chmod +x "${shim_dir}/rg"

  verifier_tmp="${tmp_root}/verifier-tmp-scan-dir-hard-error"
  mkdir -p "$verifier_tmp"

  set +e
  PATH="${shim_dir}:${PATH}" \
    ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" \
    > "${tmp_root}/scan-dir-hard-error-stdout" 2> "${tmp_root}/scan-dir-hard-error-stderr"
  code=$?
  set -e

  if [ "$code" -ne 0 ] && ! rg -q 'OpenAPI surface clean' "${tmp_root}/scan-dir-hard-error-stdout"; then
    record_pass "rg hard error (exit 2) collecting Go files makes the gate fail closed instead of reporting a clean surface"
  else
    record_fail "rg hard error (exit 2) collecting Go files makes the gate fail closed instead of reporting a clean surface (code=$code)"
  fi
}

# ═════════════════════════════════════════════════════════════════════════════
# Test 10f — red: when neither owned route source directory exists, the
# verifier must fail closed instead of declaring an empty OpenAPI surface clean.
# An existing scan directory with no matching Go files remains a valid clean
# input and is covered separately by test_empty_green.
# shellcheck disable=SC2154 # tmp_root, verifier: defined by the sourcing script
test_missing_scan_directories_fail_closed_red() {
  local dir verifier_tmp code stdout_file stderr_file

  dir="${tmp_root}/missing-scan-directories"
  mkdir -p "$dir"
  verifier_tmp="${tmp_root}/verifier-tmp-missing-scan-directories"
  mkdir -p "$verifier_tmp"
  stdout_file="${tmp_root}/missing-scan-directories-stdout"
  stderr_file="${tmp_root}/missing-scan-directories-stderr"

  set +e
  ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" > "$stdout_file" 2> "$stderr_file"
  code=$?
  set -e

  if [ "$code" -ne 0 ] \
    && rg -Fqx 'OPENAPI SCAN FAILED: no owned route source directories found' "$stderr_file" \
    && ! rg -q 'OpenAPI surface clean' "$stdout_file" "$stderr_file"; then
    record_pass "missing owned route source directories fail closed with a stable diagnostic"
  else
    record_fail "missing owned route source directories fail closed with a stable diagnostic (code=$code)"
  fi
}
