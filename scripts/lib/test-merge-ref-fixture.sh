#!/usr/bin/env bash
# Shared fixture builder for the moved-base regression cases of the diff-scoped
# gates (#10248). Sourced, never executed.
#
# A pull_request job checks out GitHub's merge ref at fetch-depth 2. The merge
# commit M has two parents: B, the base branch when GitHub built the ref, and P,
# the PR head. The job runs later, so main may hold a commit T that landed after
# B. A gate that diffs against origin/$GITHUB_BASE_REF fetched at job time then
# reads T's changes as the PR's own. These helpers rebuild that exact shape from
# a tiny source repo so each gate's self-test can pin the first-parent base.
#
# The caller owns the source repo: a git repo on branch main whose HEAD is B,
# with an identity configured. Mutations are passed as function names; each is
# called as `fn SRC` with SRC's working tree checked out on the right branch
# and must only write files (this library commits them).
#
# Usage:
#   . "${repo_root}/scripts/lib/test-merge-ref-fixture.sh"
#   clone="$(mref_build_clone moved "$src" pr_change main_change)"   # main moved
#   clone="$(mref_build_clone ctrl  "$src" pr_change "")"            # main stays at B
#
# MREF_TMP must name a writable scratch directory.
# shellcheck disable=SC2154 # MREF_TMP is set by the sourcing test.

mref_git() {
  git -c user.email=eshu-test@invalid -c user.name='eshu test' \
    -c protocol.file.allow=always -c init.defaultBranch=main "$@"
}

# mref_commit SRC MESSAGE stages every change and commits it (allow-empty so a
# mutation that only deletes still produces a commit).
mref_commit() {
  mref_git -C "$1" add -A >/dev/null
  mref_git -C "$1" commit -q --allow-empty -m "$2" >/dev/null
}

# mref_build_clone NAME SRC PR_FN MAIN_FN prints the path of a depth-2 clone of
# the merge commit M = merge(B, PR). MAIN_FN may be empty: main then stays at B
# (the unmoved control). The clone has no origin/main of its own, as in CI; the
# remote's main is T when MAIN_FN is set and B otherwise.
mref_build_clone() {
  local name="$1" src="$2" pr_fn="$3" main_fn="$4" base clone
  mref_git -C "$src" branch -M main >/dev/null
  base="$(mref_git -C "$src" rev-parse HEAD)"

  mref_git -C "$src" checkout -q -b "pr-${name}" >/dev/null
  "$pr_fn" "$src"
  mref_commit "$src" "pr head ${name}"

  mref_git -C "$src" checkout -q main >/dev/null
  if [ -n "$main_fn" ]; then
    "$main_fn" "$src"
    mref_commit "$src" "main moves after the merge ref ${name}"
  fi

  mref_git -C "$src" checkout -q -b "mergeref-${name}" "$base" >/dev/null
  mref_git -C "$src" merge -q --no-ff -m "merge ref ${name}" "pr-${name}" >/dev/null
  # Leave the source on main with T (or B) as the remote's main tip.
  mref_git -C "$src" checkout -q main >/dev/null

  clone="${MREF_TMP}/clone-${name}"
  mref_git clone -q --depth=2 --branch "mergeref-${name}" "file://${src}" "$clone" >/dev/null 2>&1
  printf '%s\n' "$clone"
}

# mref_build_linear_clone NAME SRC FN1 FN2 prints a depth-2 clone of a branch
# with two commits on top of B and no merge commit: HEAD has one parent, and
# that parent (FN1's commit) is the branch's own previous commit, not the base.
mref_build_linear_clone() {
  local name="$1" src="$2" fn1="$3" fn2="$4" clone
  mref_git -C "$src" branch -M main >/dev/null
  mref_git -C "$src" checkout -q -b "linear-${name}" main >/dev/null
  "$fn1" "$src"
  mref_commit "$src" "linear one ${name}"
  "$fn2" "$src"
  mref_commit "$src" "linear two ${name}"
  mref_git -C "$src" checkout -q main >/dev/null

  clone="${MREF_TMP}/clone-${name}"
  mref_git clone -q --depth=2 --branch "linear-${name}" "file://${src}" "$clone" >/dev/null 2>&1
  printf '%s\n' "$clone"
}
