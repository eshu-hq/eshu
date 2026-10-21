#!/usr/bin/env bash
# Regression test for #8078: no verifier gate may shallow the shared clone.
#
# Nine verifier fetch sites resolve a missing base with
# `git fetch --no-tags --depth=1 origin ...` into $repo_root. In a complete
# shared clone that fetch writes .git/shallow, which every worktree shares,
# so one lane's base lookup flips all lanes to shallow and fails
# query-methodology-static until someone repairs it with --unshallow.
#
# eshu_gate_fetch_base (scripts/lib/gate-diff-base.sh) is the one fetch path
# the converted gates use: a full fetch in a complete clone (depth never
# changes), a bounded --depth=1 fetch only when the clone is already shallow
# (CI fetch-depth checkouts). This suite pins both halves behaviorally on
# local file:// fixture clones: no network, no shared state.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/gate-diff-base.sh
. "${repo_root}/scripts/lib/gate-diff-base.sh"

tmp_root="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path (fixtures and their gate runs inherit
# this env). GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. Teardown retries stay as
# belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

ok() {
  PASS=$((PASS + 1))
  printf 'ok - %s\n' "$1"
}

no() {
  FAIL=$((FAIL + 1))
  printf 'not ok - %s\n' "$1" >&2
}

git_env() {
  export GIT_CONFIG_NOSYSTEM=1 HOME="${tmp_root}/home"
  mkdir -p "${HOME}"
  git config --global user.email "test@example.invalid"
  git config --global user.name "Eshu Fetch Guard Test"
  git config --global init.defaultBranch main
  git config --global protocol.file.allow always
}

# init_origin <name> builds a fixture origin with main plus a side branch.
init_origin() {
  local dir="${tmp_root}/$1"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  printf 'one\n' >"${dir}/file.txt"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m one
  git -C "${dir}" checkout -q -b side
  printf 'two\n' >"${dir}/file.txt"
  git -C "${dir}" commit -q -am two
  git -C "${dir}" checkout -q main
  printf '%s\n' "${dir}"
}

git_env
origin="$(init_origin origin)"
git clone -q "file://${origin}" "${tmp_root}/full"
git clone -q --depth 1 "file://${origin}" "${tmp_root}/shallow"

# Case 1: a fetch into a complete clone must not shallow it. Drop the
# remote-tracking ref first so the fetch does real work, as a lane's base
# lookup does when origin/<base> is absent.
git -C "${tmp_root}/full" update-ref -d refs/remotes/origin/side
if eshu_gate_fetch_base "${tmp_root}/full" "side:refs/remotes/origin/side" >/dev/null 2>&1 &&
  git -C "${tmp_root}/full" rev-parse --verify --quiet "origin/side^{commit}" >/dev/null &&
  [[ "$(git -C "${tmp_root}/full" rev-parse --is-shallow-repository)" == "false" ]]; then
  ok "complete clone stays complete and resolves the base"
else
  no "complete clone stays complete and resolves the base"
fi

# Case 2: a fetch into an already-shallow clone keeps the bounded depth path
# (CI fetch-depth checkouts) and still resolves the base.
git -C "${tmp_root}/shallow" update-ref -d refs/remotes/origin/side 2>/dev/null || true
if eshu_gate_fetch_base "${tmp_root}/shallow" "side:refs/remotes/origin/side" >/dev/null 2>&1 &&
  git -C "${tmp_root}/shallow" rev-parse --verify --quiet "origin/side^{commit}" >/dev/null &&
  [[ "$(git -C "${tmp_root}/shallow" rev-parse --is-shallow-repository)" == "true" ]]; then
  ok "shallow clone stays shallow and resolves the base"
else
  no "shallow clone stays shallow and resolves the base"
fi

# Case 3: an unfetchable ref still fails, so callers keep their || true shape.
if eshu_gate_fetch_base "${tmp_root}/full" "no-such-branch:refs/remotes/origin/no-such-branch" >/dev/null 2>&1; then
  no "unfetchable ref fails"
else
  ok "unfetchable ref fails"
fi

printf '%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
