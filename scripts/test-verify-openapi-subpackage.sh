#!/usr/bin/env bash
#
# test-verify-openapi-subpackage.sh — proves verify-openapi.sh still sees a
# route whose handler has moved out of go/internal/query's root into a family
# subpackage.
#
# Why this file exists, separately from test-verify-openapi.sh: epic #6053
# splits go/internal/query's flat ~880-file package into handler-family
# subpackages (#6060). A family's Mount() and its mux.HandleFunc calls move
# into go/internal/query/<family>/, and the route scan has to keep finding
# them at depth.
#
# The fragments move too, as of #6642 part C. They used to be pinned to the
# root because OpenAPISpec() concatenated UNEXPORTED package-level consts and
# a Go package boundary follows the directory boundary; they now live in
# go/internal/query/openapi/paths/<family>/ and export their constants, so
# openapi/spec.go reaches them across that boundary. Both layouts have to be
# scanned: the flat one still exists in these fixtures and in any tree that
# has not been migrated.
#
# The three vectors below pin all of it: a moved handler whose OpenAPI entry
# exists must stay GREEN (no phantom orphan), a moved handler with no OpenAPI
# entry must still go RED (the scan has to keep biting at depth, not merely
# stop complaining), and a fragment that has itself moved into the nested
# openapi/ tree must be found (deleting the fragment recursion must turn that
# one red).
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

# write_nested_openapi_fragment writes the same fragment in the shape it has
# after #6642 part C: an EXPORTED const in its own leaf package under
# go/internal/query/openapi/paths/<family>/. Note the basename no longer
# starts with "openapi", which is why verify-openapi.sh needs a directory
# exclusion and not only the "!openapi_*.go" basename one.
write_nested_openapi_fragment() {
  local dir="$1"
  mkdir -p "${dir}/go/internal/query/openapi/paths/supplychain"
  cat > "${dir}/go/internal/query/openapi/paths/supplychain/impact_findings.go" << 'GOEOF'
// SPDX-License-Identifier: MIT
package supplychain

const ImpactFindings = `
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

# ══════════════════════════════════════════════════════════════════════════════
# Vector 3 — green: the fragment itself has moved into
# go/internal/query/openapi/paths/<family>/ (#6642 part C). The route is
# documented there and nowhere else, so the gate must find it.
#
# Under the depth-1 `"$query_dir"/openapi_paths_*.go` glob this FAILS: the
# glob matches nothing, the OpenAPI route set is empty, and the served route
# reports as MISSING_OPENAPI. It is the direct regression test for the
# fragment recursion, and it also pins the scan exclusion -- without
# "!**/openapi/**" the fragment file is read as a HandleFunc source instead.
test_nested_openapi_fragment_is_found_green() {
  local dir
  dir="$(setup_repo "nested-fragment-documented")"

  write_moved_handler "$dir"
  write_nested_openapi_fragment "$dir"

  run_verifier "$dir" \
    "green: a fragment moved into openapi/paths/<family>/ is still scanned" \
    "pass"
}

test_moved_handler_with_openapi_entry_green
test_moved_handler_without_openapi_entry_red
test_nested_openapi_fragment_is_found_green

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
printf '%d tests, %d passed, %d failed\n' "$TOTAL" "$PASS" "$FAIL"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
