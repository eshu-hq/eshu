#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-route-coverage.sh"

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
  echo "not ok - $1" >&2
  if [ -f /tmp/eshu-route-coverage.out ]; then
    echo '--- stdout ---' >&2
    head -80 /tmp/eshu-route-coverage.out >&2
  fi
  if [ -f /tmp/eshu-route-coverage.err ]; then
    echo '--- stderr ---' >&2
    head -80 /tmp/eshu-route-coverage.err >&2
  fi
}

# setup_repo creates a minimal git repo with a testable query package.
setup_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/go/internal/query"
  mkdir -p "${dir}/scripts"

  # Copy verifier to the test repo
  cp "$verifier" "${dir}/scripts/verify-route-coverage.sh"
  chmod +x "${dir}/scripts/verify-route-coverage.sh"

  echo "$dir"
}

# Test 1 — green: a handler with a matching test passes the gate
test_green_handler_with_test() {
  local dir
  dir="$(setup_repo "green-handler")"

  # Create a handler with HandleFunc and matching test
  cat > "${dir}/go/internal/query/example_handler.go" << 'GO'
package query

import "net/http"

type ExampleHandler struct{}

func (h *ExampleHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/example/thing", h.getExampleThing)
}

func (h *ExampleHandler) getExampleThing(w http.ResponseWriter, r *http.Request) {}
GO

  cat > "${dir}/go/internal/query/example_handler_test.go" << 'GO'
package query

import "testing"

func TestGetExampleThingReturnsData(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_pass "green: handler with matching test passes"
  else
    record_fail "green: handler with matching test should pass but failed"
  fi
}

# Test 2 — red: a handler without a matching test fails the gate
test_red_handler_without_test() {
  local dir
  dir="$(setup_repo "red-handler")"

  cat > "${dir}/go/internal/query/untested_handler.go" << 'GO'
package query

import "net/http"

type UntestedHandler struct{}

func (h *UntestedHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/untested/stuff", h.getUntestedStuff)
}

func (h *UntestedHandler) getUntestedStuff(w http.ResponseWriter, r *http.Request) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: handler without test should fail but passed"
  else
    record_pass "red: handler without test fails correctly"
  fi
}

# Test 3 — green: short method name matches via concatenated file-stem+method pattern
# e.g. getFamily in collector_readiness.go → search for CollectorReadinessFamily
# matching TestCollectorReadinessFamilyDrilldown
test_green_handler_with_concatenated_name_test() {
  local dir
  dir="$(setup_repo "green-concat")"

  cat > "${dir}/go/internal/query/collector_readiness.go" << 'GO'
package query

import "net/http"

type CollectorReadinessHandler struct{}

func (h *CollectorReadinessHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/collector-readiness/{family}", h.getFamily)
}

func (h *CollectorReadinessHandler) getFamily(w http.ResponseWriter, r *http.Request) {}
GO

  cat > "${dir}/go/internal/query/collector_readiness_test.go" << 'GO'
package query

import "testing"

func TestCollectorReadinessFamilyDrilldown(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_pass "green: concatenated file-stem+method test name matches"
  else
    record_fail "green: concatenated file-stem+method test name should match but failed"
  fi
}

# Test 4 — red: short method name with only-an-unrelated-sibling-test is NOT a match
# e.g. adding a short new method to an already-tested file should still fail
# if no test exists that references the concatenated file-stem+method name
test_red_short_method_only_has_unrelated_sibling_test() {
  local dir
  dir="$(setup_repo "red-short-unrelated")"

  cat > "${dir}/go/internal/query/repo.go" << 'GO'
package query

import "net/http"

type RepoHandler struct{}

func (h *RepoHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/repos", h.listRepos)
  mux.HandleFunc("GET /api/v0/repos/{repo_id}/new", h.getNew)
}

func (h *RepoHandler) listRepos(w http.ResponseWriter, r *http.Request) {}
func (h *RepoHandler) getNew(w http.ResponseWriter, r *http.Request)    {}
GO

  cat > "${dir}/go/internal/query/repo_test.go" << 'GO'
package query

import "testing"

func TestRepoListReposReturnsRepos(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: short method with unrelated sibling should fail but passed"
  else
    record_pass "red: short method with unrelated sibling test correctly fails (RepoNew unmatched)"
  fi
}

test_green_handler_with_test
test_red_handler_without_test
test_green_handler_with_concatenated_name_test
test_red_short_method_only_has_unrelated_sibling_test

printf '\n%d/%d tests passed\n' "$PASS" "$TOTAL"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
