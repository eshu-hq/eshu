#!/usr/bin/env bash
# Base-commit resolution for the diff-scoped verifier gates.
#
# Sourced, never executed. A gate that only inspects the files a branch
# changed has to answer one question first: which commit is "before"? Nine
# verifier scripts each carry their own copy of that answer, and the two traps
# documented below were found and patched in four of those copies separately.
# This file is the one copy the parser-relationship kit now uses; the other
# eight still have theirs inline.
#
# Usage:
#
#   # shellcheck source=scripts/lib/gate-diff-base.sh
#   . "$script_dir/lib/gate-diff-base.sh"
#   eshu_gate_resolve_diff_base "verify-my-gate" "$repo_root" \
#     "${ESHU_MY_GATE_BASE:-}"
#   base="$eshu_gate_diff_base"
#
# The answer comes back in a global rather than on stdout, because the
# no-base-available case prints an operator-facing skip line that a command
# substitution would swallow into the variable.

# eshu_gate_fetch_base fetches a base ref into repo_root without changing a
# complete clone's depth (#8078). Every worktree shares one .git/shallow, so
# a --depth fetch from a lane shallows all of them and fails
# query-methodology-static everywhere until someone repairs it; a full fetch
# never shallows. In an already-shallow checkout (CI fetch-depth) it keeps
# the bounded --depth=1 fetch. Arguments after repo_root pass through to git
# fetch. It reports git fetch's status, so callers keep their || true shape.
eshu_gate_fetch_base() {
  local repo_root="$1"
  shift
  if [ "$(git -C "$repo_root" rev-parse --is-shallow-repository 2>/dev/null)" = "false" ]; then
    git -C "$repo_root" fetch --no-tags origin "$@" >/dev/null 2>&1
  else
    git -C "$repo_root" fetch --no-tags --depth=1 origin "$@" >/dev/null 2>&1
  fi
}

# eshu_gate_merge_ref_parent prints the first parent of a pull-request merge
# ref, or nothing when HEAD is not one. GitHub builds refs/pull/N/merge from the
# base branch's tip at that moment and runs the job later; parent 1 is the base
# branch as it was combined with the PR, and it stays put when the base branch
# moves on. Passing it as the gate's base override diffs a PR against what it
# was merged with, where origin/$GITHUB_BASE_REF fetched at job time also
# contains commits that landed after the ref was built and reads them as the
# PR's own removals or additions (#10131). Only a pull_request event with a
# two-parent HEAD and a present parent object qualifies; a local run, a push,
# and a depth-1 checkout print nothing and keep the resolver's own order.
eshu_gate_merge_ref_parent() {
  local repo_root="$1" line
  [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ] || return 0
  line="$(git -C "$repo_root" rev-list --parents -n 1 HEAD 2>/dev/null)" || return 0
  # Word-splitting is the point: "<head> <parent1> <parent2>".
  # shellcheck disable=SC2086
  set -- $line
  [ "$#" -eq 3 ] || return 0
  git -C "$repo_root" cat-file -e "$2^{commit}" 2>/dev/null || return 0
  printf '%s\n' "$2"
}

# eshu_gate_resolve_diff_base picks the commit a gate diffs HEAD against and
# stores it in eshu_gate_diff_base (empty when no base is available at all).
#
# Arguments: gate name for the skip message, repo root, and the gate's own
# ESHU_*_BASE override (may be empty).
#
# Preference order: the override, then origin/$GITHUB_BASE_REF in CI, then the
# merge base with origin/main, then HEAD~1 as a last resort.
#
# The fetch refspec is load-bearing. `git fetch origin <branch>` with no
# `<src>:<dst>` destination only updates FETCH_HEAD, never
# refs/remotes/origin/<branch>. CI jobs that check out shallow (test.yml's
# verify-contracts job uses fetch-depth: 2) have no origin/<base> ref of their
# own, so without the destination refspec below origin/$GITHUB_BASE_REF never
# resolved, the merge-base branch found no origin/main either, and every PR run
# silently used HEAD~1: the tip commit alone.
#
# HEAD~1 is a last resort, not a default, for the same reason. `make pre-pr`
# and the gate registry both invoke these gates with no base pinned, and a
# HEAD~1 default scopes a gate to the last commit, so a violation introduced in
# an earlier commit of a multi-commit branch escapes whenever the tip commit is
# innocuous. On a branch based on a squash-merge commit it is worse: HEAD~1
# diffs the merge's files instead of the branch's own. HEAD~1 survives only for
# a shallow clone, a missing origin remote, or a fresh fixture repo.
eshu_gate_resolve_diff_base() {
  local gate_name="$1"
  local repo_root="$2"
  local base="${3:-}"
  local merge_base

  if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
    eshu_gate_fetch_base "$repo_root" \
      "$GITHUB_BASE_REF:refs/remotes/origin/$GITHUB_BASE_REF" || true
    if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
      base="origin/$GITHUB_BASE_REF"
    fi
  fi

  if [ -z "$base" ]; then
    if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
      merge_base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || true)"
      # A merge base equal to HEAD means the branch adds no commits of its own,
      # so the window would be empty -- narrower than HEAD~1. Leave base unset.
      if [ -n "$merge_base" ] &&
        [ "$merge_base" != "$(git -C "$repo_root" rev-parse HEAD 2>/dev/null)" ]; then
        base="$merge_base"
      fi
    fi
  fi

  if [ -z "$base" ]; then
    if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
      base="HEAD~1"
    else
      printf '%s: no base commit available, skipping diff checks\n' "$gate_name"
      base=""
    fi
  fi

  # shellcheck disable=SC2034  # read by the sourcing gate script.
  eshu_gate_diff_base="$base"
}
