#!/usr/bin/env bash
# Moved-base cases for scripts/verify-route-coverage.sh (#10248). Sourced by
# scripts/test-gate-fetch-base-guard.sh after its ok/no helpers and git_env
# exist, and run by the route-coverage gate's test_command beside the rest of
# that file's base-resolution cases.
#
# On a pull_request run the job checks out GitHub's merge ref at fetch-depth 2.
# The gate diffed `origin/$GITHUB_BASE_REF HEAD` with the tip fetched at job
# time, so a file main touched after the ref was built read as a PR change and
# the gate scanned its whole route set: a pre-existing untested route in that
# file failed a PR that never touched it. The gate now diffs the merge ref's
# first parent unless ESHU_ROUTE_COVERAGE_BASE is set.
# shellcheck disable=SC2154 # Parent defines repo_root, tmp_root, ok, no.

# shellcheck source=scripts/lib/test-merge-ref-fixture.sh
. "${repo_root}/scripts/lib/test-merge-ref-fixture.sh"
MREF_TMP="${tmp_root}"

route_mref_handler() {
  printf 'package query\n\nimport "net/http"\n\ntype %sHandler struct{}\n\nfunc (h *%sHandler) Mount(mux *http.ServeMux) {\n  mux.HandleFunc("GET /api/v0/%s/stuff", h.get%sStuff)\n}\n\nfunc (h *%sHandler) get%sStuff(w http.ResponseWriter, r *http.Request) {}\n' \
    "$2" "$2" "$3" "$2" "$2" "$2" >"$1/go/internal/query/$3_handler.go"
}
# B already carries one untested route, the shape the real tree has today.
route_mref_source() {
  local src="${tmp_root}/route-src-$1"
  mkdir -p "${src}/go/internal/query" "${src}/go/cmd/api"
  mref_git -C "${src}" init -q
  route_mref_handler "${src}" Old old
  : >"${src}/go/cmd/api/.keep"
  mref_commit "${src}" baseline
  printf '%s\n' "${src}"
}
# Main edits the file with the untested route (and adds no test for it).
route_mref_main_touches_old() { printf '// main edit\n' >>"$1/go/internal/query/old_handler.go"; }
route_mref_pr_touches_a_doc() { mkdir -p "$1/docs" && printf '# note\n' >"$1/docs/pr-note.md"; }
route_mref_pr_adds_untested() { route_mref_handler "$1" Fresh fresh; }
route_mref_pr_note_only() { route_mref_pr_touches_a_doc "$1"; }

# run_route_mref CLONE [NAME=value ...] runs the real gate over the clone with
# ambient base and event variables cleared. Sets route_out and route_rc.
run_route_mref() {
  local clone="$1"
  shift
  route_rc=0
  route_out="$(env -u ESHU_ROUTE_COVERAGE_BASE -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF \
    ESHU_ROUTE_COVERAGE_REPO_ROOT="${clone}" "$@" \
    bash "${repo_root}/scripts/verify-route-coverage.sh" 2>&1)" || route_rc=$?
}

# expect_route_mref NAME WANT_RC [WANT_TEXT] checks the last run.
expect_route_mref() {
  local name="$1" want_rc="$2" want_text="${3:-}"
  if [[ "${route_rc}" -ne "${want_rc}" ]]; then
    no "route merge ref: ${name} (exit ${route_rc}, want ${want_rc}: ${route_out})"
  elif [[ -n "${want_text}" && "${route_out}" != *"${want_text}"* ]]; then
    no "route merge ref: ${name} (output lacks '${want_text}': ${route_out})"
  else
    ok "route merge ref: ${name}"
  fi
}

route_pr=(GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)

route_moved="$(mref_build_clone route-moved "$(route_mref_source moved)" route_mref_pr_touches_a_doc route_mref_main_touches_old)"
run_route_mref "${route_moved}" "${route_pr[@]}"
expect_route_mref "a file main touched after the ref was built is not the PR's change" 0 "0 routes checked"

mref_git -C "${route_moved}" fetch -q --depth=1 origin main:refs/remotes/origin/main
run_route_mref "${route_moved}" "${route_pr[@]}" ESHU_ROUTE_COVERAGE_BASE=origin/main
expect_route_mref "an explicit base override still wins and restores the moved-tip read" 1 "UNCOVERED: go/internal/query/old_handler.go"

run_route_mref "${route_moved}" GITHUB_EVENT_NAME=push GITHUB_BASE_REF=main
expect_route_mref "a push event keeps the origin tip base" 1 "UNCOVERED: go/internal/query/old_handler.go"

route_ctrl="$(mref_build_clone route-ctrl "$(route_mref_source ctrl)" route_mref_pr_touches_a_doc "")"
run_route_mref "${route_ctrl}" "${route_pr[@]}"
expect_route_mref "control with main unmoved stays green" 0 "0 routes checked"

route_viol="$(mref_build_clone route-viol "$(route_mref_source viol)" route_mref_pr_adds_untested route_mref_main_touches_old)"
run_route_mref "${route_viol}" "${route_pr[@]}"
expect_route_mref "a PR route without a test still fails while main moved" 1 "UNCOVERED: go/internal/query/fresh_handler.go"
[[ "${route_out}" != *"old_handler.go"* ]] \
  && ok "route merge ref: the untouched pre-existing route is not blamed on the PR" \
  || no "route merge ref: the untouched pre-existing route is not blamed on the PR (${route_out})"

route_single="$(mref_build_linear_clone route-single "$(route_mref_source single)" route_mref_pr_adds_untested route_mref_pr_note_only)"
run_route_mref "${route_single}" "${route_pr[@]}"
expect_route_mref "a single-parent HEAD does not trust its parent as the base" 1 "UNCOVERED: go/internal/query/fresh_handler.go"
