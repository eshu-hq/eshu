#!/usr/bin/env bash
# Behaviour tests for verify-root-cause-evidence.sh.
#
# Each case builds a throwaway git repo, commits a base, then commits a change
# and runs the gate against it. Testing through real commits rather than a
# stubbed diff matters: the gate's whole contract is "added lines only", and a
# stub cannot catch a regression that starts reading whole-file content.
#
# The gate is advisory, so it exits 0 in every case. These tests assert on the
# REPORTED VERDICT in its output, not the exit code. When the gate is promoted
# to blocking, the exit-code assertions become meaningful and should be added
# here in the same change.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="$script_dir/verify-root-cause-evidence.sh"

failures=0
cases_run=0
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# Every case below builds a throwaway git repo; the daemon keys on its
# Trace2 socket env. GIT_TRACE2_EVENT=0 disables the event stream (same
# convention as test-verify-moved-file-refs.sh); PERF likewise.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0

report_pass() { printf '  ok  %s\n' "$1"; }
report_fail() { printf '  FAIL %s\n    %s\n' "$1" "$2"; failures=$((failures + 1)); }

# Runs the gate over a repo whose HEAD commit adds $2 to $1, and echoes output.
run_case() {
  local rel_path="$1" content="$2" tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/eshu-rce-test.XXXXXX")"
  (
    cd "$tmp"
    git init -q .
    git config user.email t@example.com
    git config user.name t
    mkdir -p "$(dirname "$rel_path")"
    printf 'base\n' >"$rel_path"
    git add -A && git commit -q -m base
    printf '%s\n' "$content" >>"$rel_path"
    git add -A && git commit -q -m change
  )
  ESHU_ROOT_CAUSE_EVIDENCE_REPO_ROOT="$tmp" \
    ESHU_ROOT_CAUSE_EVIDENCE_BASE="HEAD~1" \
    bash "$gate" 2>&1 || true
  rm -rf "$tmp"
}

expect_contains() {
  local name="$1" haystack="$2" needle="$3"
  cases_run=$((cases_run + 1))
  if printf '%s' "$haystack" | rg -q -F -- "$needle"; then
    report_pass "$name"
  else
    report_fail "$name" "expected output to contain: $needle"
  fi
}

expect_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  cases_run=$((cases_run + 1))
  if printf '%s' "$haystack" | rg -q -F -- "$needle"; then
    report_fail "$name" "expected output NOT to contain: $needle"
  else
    report_pass "$name"
  fi
}

printf 'test-verify-root-cause-evidence: running\n'

# A bare causal claim is the case this gate exists for.
out="$(run_case 'docs/internal/evidence/case.md' 'The root cause is a stale index.')"
expect_contains 'flags a causal claim with no marker' "$out" 'causal claim without recorded evidence'

# The same claim with a substantive marker passes.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence: reproduced 3 of 4 runs; EXPLAIN shows a seq scan on the
same query that used the index before the migration landed.')"
expect_contains 'accepts a causal claim with substantive evidence' "$out" 'carry observed evidence'

# A marker with nothing behind it is the obvious way to game a marker gate.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence: confirmed.')"
expect_contains 'rejects a marker with too little substance' "$out" 'too little after it'

# Hedged language must NOT trip the gate. Punishing honest labelling would
# invert the behaviour this is meant to encourage, so this is a core case.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The suspected cause is a stale index, but this is one theory and unproven.')"
expect_contains 'ignores hedged, explicitly-unproven wording' "$out" 'carry observed evidence'

# The parenthetical qualifier convention the performance markers already use.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence (#5717): reproduced across four runs with the node present
in every one and the edge absent in three of them.')"
expect_contains 'accepts a parenthetical qualifier before the colon' "$out" 'carry observed evidence'

# Out of scope: same prose, wrong directory. Keeps the gate off ordinary
# explanatory writing in public docs.
out="$(run_case 'docs/public/reference/thing.md' 'The root cause is a stale index.')"
expect_not_contains 'ignores files outside the evidence directory' "$out" 'causal claim without recorded evidence'

# Fixtures must never satisfy or trip the gate.
out="$(run_case 'docs/internal/evidence/testdata/sample.md' 'The root cause is a stale index.')"
expect_not_contains 'ignores testdata fixtures' "$out" 'causal claim without recorded evidence'

# A qualifier containing a colon must not be counted as evidence. `${line#*:}`
# cuts at the URL's colon, leaving enough characters to clear the threshold
# while the real body is two words.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence (https://example.com/issues/12345678901234567890): confirmed.')"
expect_contains 'measures the body after a colon-bearing qualifier' "$out" 'too little after it'

# Multi-commit branch: a causal claim in an EARLIER commit must still be seen.
# A HEAD~1 default silently narrows the window to the last commit and passes
# for the wrong reason -- the repeated default-base trap in this repo.
multi_commit_case() {
  local tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/eshu-rce-multi.XXXXXX")"
  (
    cd "$tmp"
    git init -q -b main .
    git config user.email t@example.com
    git config user.name t
    mkdir -p docs/internal/evidence
    printf 'base\n' >docs/internal/evidence/case.md
    git add -A && git commit -q -m base
    git branch -q -M main
    git remote add origin . 2>/dev/null || true
    git update-ref refs/remotes/origin/main HEAD
    # Commit 1 of the branch carries the causal claim, with no marker.
    printf 'The root cause is a stale index.\n' >>docs/internal/evidence/case.md
    git add -A && git commit -q -m 'claim'
    # Commit 2 touches something else entirely.
    printf 'unrelated line\n' >>docs/internal/evidence/case.md
    git add -A && git commit -q -m 'unrelated'
  )
  ESHU_ROOT_CAUSE_EVIDENCE_REPO_ROOT="$tmp" bash "$gate" 2>&1 || true
  rm -rf "$tmp"
}
out="$(multi_commit_case)"
expect_contains 'sees a claim from an earlier commit on a multi-commit branch' \
  "$out" 'causal claim without recorded evidence'

# Moved-base cases (#10248). On a pull_request run the job checks out GitHub's
# merge ref at fetch-depth 2. The gate diffed origin/$GITHUB_BASE_REF fetched at
# job time, so a claim line that main rewrote after the ref was built read as
# an added line of this PR and was reported against a doc the PR never touched.
# It now diffs the merge ref's first parent unless the base override is set.
# shellcheck source=scripts/lib/test-merge-ref-fixture.sh
. "$script_dir/lib/test-merge-ref-fixture.sh"
MREF_TMP="$(mktemp -d "${TMPDIR:-/tmp}/eshu-rce-mref.XXXXXX")"
trap 'rm -rf "$MREF_TMP"' EXIT

rce_mref_source() {
  local src="$MREF_TMP/src-$1"
  mkdir -p "$src/docs/internal/evidence"
  mref_git -C "$src" init -q
  printf 'The root cause is a stale index.\n' >"$src/docs/internal/evidence/case.md"
  mref_commit "$src" baseline
  printf '%s\n' "$src"
}
# Main rewrites the claim line after the merge ref was built.
rce_mref_main_rewrites_claim() {
  printf 'The root cause is a stale cache.\n' >"$1/docs/internal/evidence/case.md"
}
rce_mref_pr_touches_a_doc() { mkdir -p "$1/docs" && printf '# note\n' >"$1/docs/pr-note.md"; }
rce_mref_pr_adds_claim() {
  printf 'The root cause is a stale index.\nThe root cause is a missing lease.\n' \
    >"$1/docs/internal/evidence/case.md"
}

# rce_mref_run CLONE [NAME=value ...] runs the gate over the clone with ambient
# base and event variables cleared and prints its output.
rce_mref_run() {
  local clone="$1"
  shift
  env -u ESHU_ROOT_CAUSE_EVIDENCE_BASE -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF \
    ESHU_ROOT_CAUSE_EVIDENCE_REPO_ROOT="$clone" "$@" bash "$gate" 2>&1 || true
}

rce_flag='causal claim without recorded evidence'
rce_pr=(GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)

rce_moved="$(mref_build_clone rce-moved "$(rce_mref_source moved)" rce_mref_pr_touches_a_doc rce_mref_main_rewrites_claim)"
out="$(rce_mref_run "$rce_moved" "${rce_pr[@]}")"
expect_not_contains 'merge ref: a claim main rewrote after the ref was built is not part of the PR change' "$out" "$rce_flag"
expect_contains 'merge ref: the moved run reports the clean verdict' "$out" 'carry observed evidence'

git -C "$rce_moved" fetch -q --depth=1 origin main:refs/remotes/origin/main
out="$(rce_mref_run "$rce_moved" "${rce_pr[@]}" ESHU_ROOT_CAUSE_EVIDENCE_BASE=origin/main)"
expect_contains 'merge ref: an explicit base override still wins and restores the moved-tip read' "$out" "$rce_flag"

out="$(rce_mref_run "$rce_moved" GITHUB_EVENT_NAME=push GITHUB_BASE_REF=main)"
expect_contains 'merge ref: a push event keeps the origin tip base' "$out" "$rce_flag"

rce_ctrl="$(mref_build_clone rce-ctrl "$(rce_mref_source ctrl)" rce_mref_pr_touches_a_doc "")"
out="$(rce_mref_run "$rce_ctrl" "${rce_pr[@]}")"
expect_not_contains 'merge ref: control with main unmoved reports no claim' "$out" "$rce_flag"

rce_viol="$(mref_build_clone rce-viol "$(rce_mref_source viol)" rce_mref_pr_adds_claim rce_mref_main_rewrites_claim)"
out="$(rce_mref_run "$rce_viol" "${rce_pr[@]}")"
expect_contains 'merge ref: a claim the PR adds is still reported while main moved' "$out" "$rce_flag"
expect_contains 'merge ref: the PR-added claim is the one named' "$out" 'docs/internal/evidence/case.md'

rce_single="$(mref_build_linear_clone rce-single "$(rce_mref_source single)" rce_mref_pr_adds_claim rce_mref_pr_touches_a_doc)"
out="$(rce_mref_run "$rce_single" "${rce_pr[@]}")"
expect_contains 'merge ref: a single-parent HEAD does not trust its parent as the base' "$out" "$rce_flag"

printf '\ntest-verify-root-cause-evidence: %d case(s), %d failure(s)\n' "$cases_run" "$failures"
[ "$failures" -eq 0 ] || exit 1
