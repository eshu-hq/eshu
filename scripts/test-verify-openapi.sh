#!/usr/bin/env bash
#
# test-verify-openapi.sh — focused assertion vectors for verify-openapi.sh.
# Creates synthetic repositories with known route/OpenAPI state and asserts
# the verifier exits as expected.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-openapi.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
TOTAL=0

record_pass() {
  PASS=$((PASS + 1))
  TOTAL=$((TOTAL + 1))
  printf 'ok - %s\n' "$1"
}

record_fail() {
  FAIL=$((FAIL + 1))
  TOTAL=$((TOTAL + 1))
  printf 'not ok - %s\n' "$1"
  if [ -f "${tmp_root}/last-stdout" ]; then
    echo '--- stdout ---'
    head -80 "${tmp_root}/last-stdout"
  fi
  if [ -f "${tmp_root}/last-stderr" ]; then
    echo '--- stderr ---'
    head -80 "${tmp_root}/last-stderr"
  fi
}

run_verifier() {
  local dir="$1" label="$2" expect="$3"
  local verifier_tmp="${tmp_root}/verifier-tmp"
  mkdir -p "$verifier_tmp"
  set +e
  ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" \
    > "${tmp_root}/last-stdout" 2> "${tmp_root}/last-stderr"
  local code=$?
  set -e
  if [ "$code" -eq 0 ] && [ "$expect" = "pass" ]; then
    record_pass "$label"
  elif [ "$code" -ne 0 ] && [ "$expect" = "fail" ]; then
    record_pass "$label"
  else
    record_fail "$label (code=$code, expected $expect)"
  fi
}

# setup_repo creates a minimal repo with query and serviceintelhttp packages.
setup_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/go/internal/query"
  mkdir -p "${dir}/scripts"
  echo "$dir"
}

write_handler() {
  local dir="$1" filename="$2"
  cat > "${dir}/go/internal/query/${filename}" << 'GOEOF'
package query

import "net/http"

GOEOF
  shift 2
  printf '%s\n' "$@" >> "${dir}/go/internal/query/${filename}"
}

write_openapi_path() {
  local dir="$1" filename="$2"
  shift 2
  {
    printf '// SPDX-License-Identifier: MIT\npackage query\n\n'
    printf '%s\n' "$@"
  } > "${dir}/go/internal/query/${filename}"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 1 — green: empty repo (no routes, no openapi) exits 0
test_empty_green() {
  local dir
  dir="$(setup_repo "empty")"

  # No handler files, no openapi_paths files — clean by definition.
  run_verifier "$dir" "empty repo exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 2 — green: matching route and openapi entry exits 0
test_matching_green() {
  local dir
  dir="$(setup_repo "matching")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/health", h.health)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_health.go" \
    'openAPIPathsHealth = `' \
    '    "/api/v0/health": {' \
    '      "get": {' \
    '        "tags": ["health"],' \
    '        "summary": "Health",' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "matching route+openapi exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 3 — red: HandleFunc without OpenAPI entry exits non-zero
test_missing_openapi_red() {
  local dir
  dir="$(setup_repo "missing-openapi")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("POST /api/v0/items", h.createItem)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_other.go" \
    'openAPIPathsOther = `' \
    '    "/api/v0/unrelated": {' \
    '      "get": {' \
    '        "tags": ["unrelated"],' \
    '        "summary": "Unrelated",' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "HandleFunc without OpenAPI entry exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 4 — red: OpenAPI entry without HandleFunc exits non-zero
test_orphan_openapi_red() {
  local dir
  dir="$(setup_repo "orphan-openapi")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/a", h.a)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_both.go" \
    'openAPIPathsBoth = `' \
    '    "/api/v0/a": {' \
    '      "get": {' \
    '        "tags": ["a"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    },' \
    '    "/api/v0/b": {' \
    '      "post": {' \
    '        "tags": ["b"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "OpenAPI without HandleFunc exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 5 — green: multiple matching routes (GET + POST) exits 0
test_multiple_matching_green() {
  local dir
  dir="$(setup_repo "multiple")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/repos", h.list)' \
    '	mux.HandleFunc("POST /api/v0/repos", h.create)' \
    '	mux.HandleFunc("DELETE /api/v0/repos/{id}", h.delete)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_repos.go" \
    'openAPIPathsRepos = `' \
    '    "/api/v0/repos": {' \
    '      "get": {' \
    '        "tags": ["repos"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      },' \
    '      "post": {' \
    '        "tags": ["repos"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    },' \
    '    "/api/v0/repos/{id}": {' \
    '      "delete": {' \
    '        "tags": ["repos"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "multiple GET+POST+DELETE matching exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 6 — green: route constants (variable-based HandleFunc) exit 0
test_route_constants_green() {
  local dir
  dir="$(setup_repo "route-constants")"

  write_handler "$dir" "freshness.go" \
    'const freshnessRoute = "GET /api/v0/freshness/generations"' \
    '' \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc(freshnessRoute, h.listGenerations)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_freshness.go" \
    'openAPIPathsFreshness = `' \
    '    "/api/v0/freshness/generations": {' \
    '      "get": {' \
    '        "tags": ["freshness"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "route constant resolved, exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 7 — green: string concatenation HandleFunc resolved, exits 0
test_string_concat_green() {
  local dir
  dir="$(setup_repo "string-concat")"

  write_handler "$dir" "iac.go" \
    'const planRoute = "/api/v0/replatforming/plans"' \
    '' \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("POST "+planRoute, h.handlePlan)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_plans.go" \
    'openAPIPathsPlans = `' \
    '    "/api/v0/replatforming/plans": {' \
    '      "post": {' \
    '        "tags": ["replatforming"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "string concat resolved, exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 8 — green: router health route covered in OpenAPI
test_health_in_openapi_green() {
  local dir
  dir="$(setup_repo "health")"

  write_handler "$dir" "handler.go" \
    'func Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {})' \
    '}'

  write_openapi_path "$dir" "openapi_paths_health.go" \
    'openAPIPathsHealth = `' \
    '    "/health": {' \
    '      "get": {' \
    '        "tags": ["health"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "health route in OpenAPI exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 9 — red: planted drift is detected
test_planted_drift_red() {
  local dir
  dir="$(setup_repo "planted-drift")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/known", h.known)' \
    '	mux.HandleFunc("POST /api/v0/planted-route", h.planted)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_known.go" \
    'openAPIPathsKnown = `' \
    '    "/api/v0/known": {' \
    '      "get": {' \
    '        "tags": ["known"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "planted HandleFunc without OpenAPI exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 10 — green: after drift removal exits 0
test_drift_removed_green() {
  local dir
  dir="$(setup_repo "drift-removed")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/known", h.known)' \
    '}'

  write_openapi_path "$dir" "openapi_paths_known.go" \
    'openAPIPathsKnown = `' \
    '    "/api/v0/known": {' \
    '      "get": {' \
    '        "tags": ["known"],' \
    '        "responses": {"200": {"description": "OK"}}' \
    '      }' \
    '    }' \
    '`'

  run_verifier "$dir" "drift removed exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Run all tests

test_empty_green
test_matching_green
test_missing_openapi_red
test_orphan_openapi_red
test_multiple_matching_green
test_route_constants_green
test_string_concat_green
test_health_in_openapi_green
test_planted_drift_red
test_drift_removed_green

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
printf '%d tests, %d passed, %d failed\n' "$TOTAL" "$PASS" "$FAIL"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
