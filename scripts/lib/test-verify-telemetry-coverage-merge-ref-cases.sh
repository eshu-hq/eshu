#!/usr/bin/env bash
# Moved-base cases for scripts/verify-telemetry-coverage.sh (#10248). Sourced
# at the end of test-verify-telemetry-coverage-shallow-cases.sh, so init_repo,
# tmp_root, repo_root, verifier, verifier_out and verifier_err already exist.
#
# On a pull_request run the job checks out GitHub's merge ref at fetch-depth 2.
# The stage diff ran `git diff origin/$GITHUB_BASE_REF...HEAD`, which needs a
# merge base the shallow clone lacks, so it fell back to the two-dot diff
# against the tip fetched at job time. A stage file that main retired after the
# ref was built then read as a stage the PR added, and failed the PR for a
# missing X1 row. The gate now diffs the merge ref's first parent unless
# ESHU_TELEMETRY_COVERAGE_BASE is set.
# shellcheck disable=SC2154 # Parent defines the names listed above.

# shellcheck source=scripts/lib/test-merge-ref-fixture.sh
. "${repo_root}/scripts/lib/test-merge-ref-fixture.sh"
MREF_TMP="${tmp_root}"

# B carries one stage file that no X1 row names (the real tree has such files);
# the new-stage check only judges files added since the base.
telemetry_mref_source() {
  local src
  src="$(init_repo "$1")"
  printf 'package reducer\n' >"${src}/go/internal/reducer/old_stage.go"
  mref_commit "${src}" 'baseline stage file'
  printf '%s\n' "${src}"
}
telemetry_mref_main_retires_old() { rm -f "$1/go/internal/reducer/old_stage.go"; }
telemetry_mref_pr_touches_a_doc() { printf '# note\n' >"$1/docs/pr-note.md"; }
telemetry_mref_pr_adds_stage() { printf 'package reducer\n' >"$1/go/internal/reducer/new_stage.go"; }

# run_telemetry_mref CLONE [NAME=value ...] runs the real gate over the clone
# with ambient base and event variables cleared. Sets telemetry_rc and
# telemetry_all (stdout and stderr).
run_telemetry_mref() {
  local clone="$1"
  shift
  telemetry_rc=0
  env -u ESHU_TELEMETRY_COVERAGE_BASE -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF \
    ESHU_TELEMETRY_COVERAGE_REPO_ROOT="${clone}" "$@" \
    "${verifier}" >"${verifier_out}" 2>"${verifier_err}" || telemetry_rc=$?
  telemetry_all="$(cat "${verifier_out}" "${verifier_err}")"
}

# expect_telemetry_mref NAME WANT_RC [WANT_TEXT] checks the last run.
expect_telemetry_mref() {
  local name="$1" want_rc="$2" want_text="${3:-}"
  if [ "${telemetry_rc}" -ne "${want_rc}" ]; then
    record_fail "merge ref: ${name} (exit ${telemetry_rc}, want ${want_rc})"
  elif [ -n "${want_text}" ] && [[ "${telemetry_all}" != *"${want_text}"* ]]; then
    record_fail "merge ref: ${name} (output lacks '${want_text}')"
  else
    record_pass "merge ref: ${name}"
  fi
}

telemetry_pr=(GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)
telemetry_old_row="new stage file go/internal/reducer/old_stage.go is not covered"

telemetry_moved="$(mref_build_clone tele-moved "$(telemetry_mref_source tele-moved-src)" telemetry_mref_pr_touches_a_doc telemetry_mref_main_retires_old)"
run_telemetry_mref "${telemetry_moved}" "${telemetry_pr[@]}"
expect_telemetry_mref "a stage file main retired after the ref was built is not the PR's" 0

git -C "${telemetry_moved}" fetch -q --depth=1 origin main:refs/remotes/origin/main
run_telemetry_mref "${telemetry_moved}" "${telemetry_pr[@]}" ESHU_TELEMETRY_COVERAGE_BASE=origin/main
expect_telemetry_mref "an explicit base override still wins and restores the moved-tip read" 1 "${telemetry_old_row}"

run_telemetry_mref "${telemetry_moved}" GITHUB_EVENT_NAME=push GITHUB_BASE_REF=main
expect_telemetry_mref "a push event keeps the origin tip base" 1 "${telemetry_old_row}"

telemetry_ctrl="$(mref_build_clone tele-ctrl "$(telemetry_mref_source tele-ctrl-src)" telemetry_mref_pr_touches_a_doc "")"
run_telemetry_mref "${telemetry_ctrl}" "${telemetry_pr[@]}"
expect_telemetry_mref "control with main unmoved stays green" 0

telemetry_viol="$(mref_build_clone tele-viol "$(telemetry_mref_source tele-viol-src)" telemetry_mref_pr_adds_stage telemetry_mref_main_retires_old)"
run_telemetry_mref "${telemetry_viol}" "${telemetry_pr[@]}"
expect_telemetry_mref "a PR stage without an X1 row still fails while main moved" 1 \
  "new stage file go/internal/reducer/new_stage.go is not covered"
if [[ "${telemetry_all}" == *"old_stage.go"* ]]; then
  record_fail "merge ref: the retired stage is not blamed on the PR"
else
  record_pass "merge ref: the retired stage is not blamed on the PR"
fi

telemetry_single="$(mref_build_linear_clone tele-single "$(telemetry_mref_source tele-single-src)" telemetry_mref_pr_adds_stage telemetry_mref_pr_touches_a_doc)"
run_telemetry_mref "${telemetry_single}" "${telemetry_pr[@]}"
expect_telemetry_mref "a single-parent HEAD does not trust its parent as the base" 1 \
  "new stage file go/internal/reducer/new_stage.go is not covered"
