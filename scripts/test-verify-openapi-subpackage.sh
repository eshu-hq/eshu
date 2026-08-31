#!/usr/bin/env bash
#
# test-verify-openapi-subpackage.sh — proves verify-openapi.sh still sees a
# route whose handler has moved out of go/internal/query's root into a family
# subpackage.
#
# Why this file exists, separately from test-verify-openapi.sh: epic #6053
# splits go/internal/query's flat ~880-file package into handler-family
# subpackages (#6060). A family's Mount() and its mux.HandleFunc calls move
# into go/internal/query/<family>/, while its openapi_paths_<family>.go
# fragment CANNOT follow — OpenAPISpec() concatenates unexported package-level
# consts, and a Go package boundary follows the directory boundary, so a
# fragment in a subdirectory is no longer reachable from the root package.
#
# That split is exactly what verify-openapi.sh's route scan has to survive.
# The two vectors below pin both directions of it: a moved handler whose
# OpenAPI entry exists must stay GREEN (no phantom orphan), and a moved
# handler with no OpenAPI entry must still go RED (the scan has to keep
# biting at depth, not merely stop complaining).
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
    head -40 "${tmp_root}/last-stdout"
  fi
  if [ -f "${tmp_root}/last-stderr" ]; then
    echo '--- stderr ---'
    head -40 "${tmp_root}/last-stderr"
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

# setup_repo creates a minimal repo with the query and serviceintelhttp
# packages verify-openapi.sh scans, plus one family subpackage under query.
setup_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/go/internal/query/supplychain"
  mkdir -p "${dir}/go/internal/serviceintelhttp"
  mkdir -p "${dir}/scripts"
  echo "$dir"
}

# write_moved_handler writes a handler in the family subpackage, the shape a
# family has after #6060 moves it: its own package, its own Mount().
write_moved_handler() {
  local dir="$1"
  cat > "${dir}/go/internal/query/supplychain/handler.go" << 'GOEOF'
package supplychain

import "net/http"

type Handler struct{}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", h.findings)
}

func (h *Handler) findings(w http.ResponseWriter, r *http.Request) {}
GOEOF
}

# write_root_openapi_fragment writes the openapi_paths_*.go fragment that
# stays behind in package query when the handler moves out.
write_root_openapi_fragment() {
  local dir="$1"
  cat > "${dir}/go/internal/query/openapi_paths_supply_chain.go" << 'GOEOF'
// SPDX-License-Identifier: MIT
package query

const openAPIPathsSupplyChain = `
    "/api/v0/supply-chain/impact/findings": {
      "get": {
        "tags": ["supply-chain"],
        "summary": "Supply chain impact findings",
        "responses": {"200": {"description": "OK"}}
      }
    }
`
GOEOF
}

# ══════════════════════════════════════════════════════════════════════════════
# Vector 1 — green: a handler moved into go/internal/query/<family>/ whose
# OpenAPI entry is present in the root fragment must NOT be reported as an
# orphaned OpenAPI path.
#
# Before the recursive scan landed this FAILED: verify-openapi.sh collected
# route files with `rg --files --max-depth 1`, so the moved Mount() was never
# read, the documented path matched no discovered route, and the gate emitted
# ORPHAN_OPENAPI for a route that is in fact served.
test_moved_handler_with_openapi_entry_green() {
  local dir
  dir="$(setup_repo "moved-handler-documented")"

  write_moved_handler "$dir"
  write_root_openapi_fragment "$dir"

  run_verifier "$dir" \
    "green: moved handler with an OpenAPI entry is not a phantom orphan" \
    "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Vector 2 — red: a handler moved into go/internal/query/<family>/ with NO
# OpenAPI entry must still fail.
#
# This is the half that proves the fix has teeth rather than merely silencing
# vector 1. Under the depth-1 scan this case passed — an undocumented route
# in a subpackage was invisible, so the gate reported a clean surface it had
# never actually read. Deleting the recursion must turn this test red again.
test_moved_handler_without_openapi_entry_red() {
  local dir
  dir="$(setup_repo "moved-handler-undocumented")"

  write_moved_handler "$dir"
  # Deliberately no openapi_paths_supply_chain.go: the route is undocumented.

  run_verifier "$dir" \
    "red: moved handler with no OpenAPI entry still fails the gate" \
    "fail"
}

test_moved_handler_with_openapi_entry_green
test_moved_handler_without_openapi_entry_red

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
printf '%d tests, %d passed, %d failed\n' "$TOTAL" "$PASS" "$FAIL"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
