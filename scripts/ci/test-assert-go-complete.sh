#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
assertion="${repo_root}/scripts/ci/assert-go-complete.sh"
sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
other=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
run_case() {
  local expect="$1"; shift
  if env LANE=go-race LANE_RESULT=skipped CHANGES_RESULT=skipped PLAN_RESULT=success MODE=reuse \
    HEAD_SHA="$sha" PR_HEAD_SHA="$sha" SOURCE_SHA="$other" TREE_SHA="$sha" SELECTED=false \
    GITHUB_EVENT_NAME=pull_request "$@" bash "$assertion" >/dev/null 2>&1; then
    test "$expect" = pass
  else
    test "$expect" = fail
  fi
}
run_case pass env
run_case fail env PLAN_RESULT=failure
run_case fail env SOURCE_SHA=
run_case fail env MODE=full
run_case pass env MODE=smoke
run_case fail env MODE=smoke SELECTED=true
run_case pass env MODE=smoke SELECTED=true LANE_RESULT=success
printf 'assert-go-complete contract PASS\n'
