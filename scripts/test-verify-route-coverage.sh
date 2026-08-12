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

# setup_repo creates a minimal git repo with a testable query package. Both
# query_dir and api_dir must exist (#6055's existence guard requires it, the
# same as the real repository), even for a fixture that puts no handler
# under go/cmd/api.
#
# The `git init` and baseline commit are load-bearing, not decoration. The
# verifier picks its branch with
# `[ -n "$base" ] && git rev-parse --verify "$base"`, so in a non-git fixture
# that check fails and EVERY test silently takes the no-base-ref fallback --
# including the ones that set ESHU_ROUTE_COVERAGE_BASE specifically to pin the
# git-diff branch CI uses. That is how
# test_green_testdata_fixture_ignored_on_the_git_diff_branch shipped as a false
# green: deleting the git-diff branch's own `grep -v '/testdata/'` left the
# whole suite passing 12/12 (#6055 review finding).
setup_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/go/internal/query"
  mkdir -p "${dir}/go/cmd/api"
  mkdir -p "${dir}/scripts"

  # Copy verifier to the test repo
  cp "$verifier" "${dir}/scripts/verify-route-coverage.sh"
  chmod +x "${dir}/scripts/verify-route-coverage.sh"

  # Baseline commit so "HEAD" resolves. Identity and default branch are passed
  # per-command so the fixture does not depend on this machine's git config.
  git -C "$dir" -c init.defaultBranch=main init -q
  git -C "$dir" add -A
  git -C "$dir" -c user.email=eshu-test@invalid -c user.name='eshu test' \
    commit -qm 'fixture baseline'

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

# Test 5 — red: an untested handler that has MOVED into a subdirectory of
# query_dir must still be caught. #6055: verify-route-coverage.sh's
# no-base-ref fallback scan used `find -maxdepth 1`, which never looks below
# query_dir/api_dir themselves; a handler relocated one directory level down
# (the exact shape a restructure produces) silently dropped out of route
# coverage checking — "0 routes checked, 0 uncovered", exit 0, on a route
# that in fact has no test. This is the fixture every test above already
# exercises through the SAME no-base-ref fallback path (none of these
# fixture repos are git repos, so ESHU_ROUTE_COVERAGE_BASE always resolves
# to the "checking all routes" branch); this case only adds the subdirectory
# placement.
test_red_moved_handler_in_subdirectory_without_test() {
  local dir
  dir="$(setup_repo "red-moved-handler")"
  mkdir -p "${dir}/go/internal/query/subpkg"

  cat > "${dir}/go/internal/query/subpkg/moved_handler.go" << 'GO'
package subpkg

import "net/http"

type MovedHandler struct{}

func (h *MovedHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/moved/thing", h.getMovedThing)
}

func (h *MovedHandler) getMovedThing(w http.ResponseWriter, r *http.Request) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: moved handler without test should fail but passed (would be a silent gate false-green after a move)"
  else
    record_pass "red: moved handler without test fails correctly (route coverage scan is recursive)"
  fi
}

# Test 6 — green (revert of test 5): the SAME moved handler, now with its
# test moved alongside it into the same subdirectory, must pass — proving
# the coverage lookup (not just file discovery) is also recursive, so a
# real, co-located test is not reported as missing.
test_green_moved_handler_with_test_in_same_subdirectory() {
  local dir
  dir="$(setup_repo "green-moved-handler")"
  mkdir -p "${dir}/go/internal/query/subpkg"

  cat > "${dir}/go/internal/query/subpkg/moved_handler.go" << 'GO'
package subpkg

import "net/http"

type MovedHandler struct{}

func (h *MovedHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/moved/thing", h.getMovedThing)
}

func (h *MovedHandler) getMovedThing(w http.ResponseWriter, r *http.Request) {}
GO

  cat > "${dir}/go/internal/query/subpkg/moved_handler_test.go" << 'GO'
package subpkg

import "testing"

func TestGetMovedThingReturnsData(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_pass "green: moved handler with a co-located moved test passes (revert of test 5)"
  else
    record_fail "green: moved handler with a co-located moved test should pass but failed"
  fi
}

# Test 7 — red: an untested handler must NOT be accepted as covered because an
# UNRELATED sibling package elsewhere under query_dir happens to contain a
# fuzzily matching test function name. Before the #6055 recursive change, the
# test-existence rg search was scoped to query_dir/api_dir as a whole (not to
# the handler's own directory), so a handler in query/a/repo.go could be
# marked covered by a coincidental TestRepoNew in query/b/other_test.go that
# has nothing to do with it (a real, reported false-green: "1 routes checked,
# 0 uncovered" on a genuinely untested handler). The fix restricts the search
# to the handler's own directory, depth-limited (--max-depth 1). A test that
# moved WITH its handler into a subpackage is still found -- handler_dir IS
# that directory once both have moved, see test 5/6 above.
# A testdata fixture must not satisfy a real handler's coverage. filepath's
# `find -maxdepth 1` never crossed into a subdirectory, so making this scan
# recursive newly exposes fixture tests -- the same class already guarded in
# parseReducerDir and globFilesRecursive.
# The WALK-side testdata exclusion, which the coverage-lookup test above does not
# reach: that fixture's testdata file is itself _test.go, so !*_test.go already
# excludes it regardless of the testdata glob. This one uses a NON-test .go file
# carrying HandleFunc under testdata/, which only the walk-side glob can filter.
# Without it the walk treats a fixture as a real route and reports it uncovered.
test_green_testdata_non_test_handler_is_not_treated_as_a_route() {
  local dir
  dir="$(setup_repo "green-testdata-walk")"
  mkdir -p "${dir}/go/internal/query/d/testdata"

  cat > "${dir}/go/internal/query/d/testdata/fixture_handler.go" << 'GO'
package testdata

import "net/http"

type FixtureHandler struct{}

func (h *FixtureHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/fixtures/{fixture_id}/probe", h.getProbe)
}

func (h *FixtureHandler) getProbe(w http.ResponseWriter, r *http.Request) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_pass "green: a non-test handler under testdata/ is not treated as a real route"
  else
    record_fail "green: a testdata fixture handler was wrongly scanned as a real route ($(cat /tmp/eshu-route-coverage.out))"
  fi
}

# A NESTED package's test is a different Go package and cannot exercise this
# handler, so it must not satisfy coverage. Scoping to handler_dir was not
# enough while the search stayed recursive beneath it (#6055 review finding).
# The GIT-DIFF branch, which is the path CI actually takes. Every other testdata
# case here runs through the no-base-ref fallback, so an exclusion applied only
# there would pass these tests while leaving CI unguarded (#6055 review finding).
# Seeds a committed baseline, then adds a non-test .go fixture under testdata/ as
# a working-tree change so git diff lists it.
test_green_testdata_fixture_ignored_on_the_git_diff_branch() {
  local dir
  dir="$(setup_repo "green-testdata-gitdiff")"
  mkdir -p "${dir}/go/internal/query/e/testdata"

  cat > "${dir}/go/internal/query/e/testdata/fixture_handler.go" << 'GO'
package testdata

import "net/http"

type FixtureHandler struct{}

func (h *FixtureHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/fixtures/{fixture_id}/probe", h.getProbe)
}

func (h *FixtureHandler) getProbe(w http.ResponseWriter, r *http.Request) {}
GO

  # A REAL handler with its own co-located test. Without it the scan finds
  # nothing and the test would pass on an empty result -- green because the
  # branch ran and excluded the fixture, or green because it scanned zero
  # files, being indistinguishable. Asserting "1 routes checked" separates them.
  cat > "${dir}/go/internal/query/e/widget.go" << 'GO'
package e

import "net/http"

type WidgetHandler struct{}

func (h *WidgetHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/widgets/{widget_id}/probe", h.getProbe)
}

func (h *WidgetHandler) getProbe(w http.ResponseWriter, r *http.Request) {}
GO

  cat > "${dir}/go/internal/query/e/widget_test.go" << 'GO'
package e

import "testing"

func TestWidgetProbe(t *testing.T) {}
GO

  # Stage them: the verifier's git-diff branch reads `git diff HEAD` and
  # `git diff --cached`, and NEITHER lists an untracked file. Leaving these
  # unstaged would put the scan back at zero files -- the same empty-result
  # false green by a different route.
  git -C "$dir" add -A

  # Force the git-diff branch by giving it a real base ref to diff against.
  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  export ESHU_ROUTE_COVERAGE_BASE="HEAD"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    if rg -q '^1 routes checked, 0 uncovered$' /tmp/eshu-route-coverage.out; then
      record_pass "green: a testdata fixture is ignored on the git-diff branch CI uses"
    else
      record_fail "green: git-diff branch did not scan the real handler, so the testdata exclusion was never exercised ($(cat /tmp/eshu-route-coverage.out))"
    fi
  else
    record_fail "green: testdata fixture scanned as a route on the git-diff branch ($(cat /tmp/eshu-route-coverage.out))"
  fi
  unset ESHU_ROUTE_COVERAGE_BASE
}

test_red_nested_package_test_does_not_cover_a_real_handler() {
  local dir
  dir="$(setup_repo "red-nested-package")"
  mkdir -p "${dir}/go/internal/query/child"

  cat > "${dir}/go/internal/query/repo.go" << 'GO'
package query

import "net/http"

type RepoHandler struct{}

func (h *RepoHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/repos/{repo_id}/new", h.getNew)
}

func (h *RepoHandler) getNew(w http.ResponseWriter, r *http.Request) {}
GO

  # A different package one directory down. Its name fuzzily matches, but it
  # cannot reach an unexported method of package query.
  cat > "${dir}/go/internal/query/child/unrelated_test.go" << 'GO'
package child

import "testing"

func TestRepoNew(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: a nested package's test wrongly satisfied a handler's coverage ($(cat /tmp/eshu-route-coverage.out))"
  else
    record_pass "red: a nested package's test does not satisfy a handler's coverage"
  fi
}

test_red_testdata_fixture_test_does_not_cover_a_real_handler() {
  local dir
  dir="$(setup_repo "red-testdata-fixture")"
  mkdir -p "${dir}/go/internal/query/c/testdata"

  cat > "${dir}/go/internal/query/c/widget.go" << 'GO'
package c

import "net/http"

type WidgetHandler struct{}

func (h *WidgetHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/widgets/{widget_id}/spin", h.getSpin)
}

func (h *WidgetHandler) getSpin(w http.ResponseWriter, r *http.Request) {}
GO

  # A fixture that would match the derived search word, but is testdata and so
  # must never count as coverage for the real handler beside it.
  cat > "${dir}/go/internal/query/c/testdata/fixture_test.go" << 'GO'
package testdata

import "testing"

func TestWidgetSpin(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: a testdata fixture test wrongly satisfied a real handler's coverage"
  else
    record_pass "red: a testdata fixture test does not satisfy a real handler's coverage"
  fi
}

test_red_untested_handler_falsely_covered_by_sibling_package_test() {
  local dir
  dir="$(setup_repo "red-sibling-package")"
  mkdir -p "${dir}/go/internal/query/a"
  mkdir -p "${dir}/go/internal/query/b"

  cat > "${dir}/go/internal/query/a/repo.go" << 'GO'
package a

import "net/http"

type RepoHandler struct{}

func (h *RepoHandler) Mount(mux *http.ServeMux) {
  mux.HandleFunc("GET /api/v0/repos/{repo_id}/new", h.getNew)
}

func (h *RepoHandler) getNew(w http.ResponseWriter, r *http.Request) {}
GO

  cat > "${dir}/go/internal/query/b/other_test.go" << 'GO'
package b

import "testing"

// This test has nothing to do with query/a/repo.go's getNew handler; it just
// happens to fuzzily match the "RepoNew" search word the coverage checker
// derives from it.
func TestRepoNew(t *testing.T) {}
GO

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: untested handler wrongly accepted via an unrelated sibling package's fuzzily matching test (cross-package false accept)"
  else
    record_pass "red: untested handler correctly rejected; an unrelated sibling package's fuzzy match does not count as coverage"
  fi
}

# Test 8 — a query_dir that no longer exists at all (the whole surface moved
# or was renamed) must fail loudly rather than silently report "0 routes
# checked, 0 uncovered".
test_red_missing_query_dir_fails_loudly() {
  local dir
  dir="$(setup_repo "red-missing-query-dir")"
  rm -rf "${dir}/go/internal/query"

  export ESHU_ROUTE_COVERAGE_REPO_ROOT="$dir"
  if "${dir}/scripts/verify-route-coverage.sh" >/tmp/eshu-route-coverage.out 2>/tmp/eshu-route-coverage.err; then
    record_fail "red: missing query_dir should fail loudly but passed silently"
  else
    if rg --fixed-strings --quiet "does not exist" /tmp/eshu-route-coverage.err; then
      record_pass "red: missing query_dir fails loudly naming the missing directory"
    else
      record_fail "red: missing query_dir failed, but not with the expected 'does not exist' diagnostic"
    fi
  fi
}

test_green_handler_with_test
test_red_handler_without_test
test_green_handler_with_concatenated_name_test
test_red_short_method_only_has_unrelated_sibling_test
test_red_moved_handler_in_subdirectory_without_test
test_green_moved_handler_with_test_in_same_subdirectory
test_red_untested_handler_falsely_covered_by_sibling_package_test
test_red_testdata_fixture_test_does_not_cover_a_real_handler
test_red_nested_package_test_does_not_cover_a_real_handler
test_green_testdata_fixture_ignored_on_the_git_diff_branch
test_green_testdata_non_test_handler_is_not_treated_as_a_route
test_red_missing_query_dir_fails_loudly

printf '\n%d/%d tests passed\n' "$PASS" "$TOTAL"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
