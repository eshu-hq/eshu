#!/usr/bin/env bash
# Resolve the single open pull request that owns a head SHA, for
# required-gates.yml's "Resolve pull request identity" step.
#
# Prints the PR number on stdout. Fails closed (exit 1) unless exactly one open
# PR matches, because the caller publishes a required commit status and must
# never guess which PR it is describing.
#
# WHY THIS IS A SCRIPT AND NOT INLINE jq
#
# The selector this replaces filtered `commits/<sha>/pulls` by
# `base.ref == <default branch>`. That endpoint returns every open PR
# CONTAINING the commit, which on a stacked branch is the lane itself plus every
# lane above it in the train. A stacked PR's base is another feature branch, so
# the filter matched zero, the step exited 1, the aggregate job was skipped and
# never wrote code=, and the publisher -- which runs under `if: !cancelled()` --
# read an empty AGGREGATE_CODE and fell through to its catch-all. Every stacked
# PR in the repo showed a red required-gates-complete that was not a failing
# gate at all: it was a selection that matched nothing and a consumer that could
# not tell "no answer" from "bad answer". Run 33308583604 recorded it as
# "expected exactly one open main PR for 6a6a14b775f0...; got 0".
#
# It lived inline, so nothing local ever exercised it and the break was
# invisible until it reached CI. Extracted here so
# scripts/test-resolve-required-gates-pr.sh can replay real payload shapes
# against it, in the same fixture-mirror style as the sibling
# scripts/verify-live-required-status-checks.sh.
#
# Inputs:
#   HEAD_SHA           required; the commit whose owning PR we want.
#   GITHUB_REPOSITORY  required unless ESHU_PULLS_JSON is set; owner/repo.
#   ESHU_PULLS_JSON    optional; path to a file holding the
#                      `commits/<sha>/pulls` response. Set by the tests so they
#                      run hermetically. When unset the payload is fetched with
#                      `gh api`.
set -euo pipefail

: "${HEAD_SHA:?HEAD_SHA is required}"

if [[ -n "${ESHU_PULLS_JSON:-}" ]]; then
	if [[ ! -f "${ESHU_PULLS_JSON}" ]]; then
		echo "resolve-required-gates-pr: ESHU_PULLS_JSON does not exist: ${ESHU_PULLS_JSON}" >&2
		exit 1
	fi
	pulls="$(cat "${ESHU_PULLS_JSON}")"
else
	: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required when ESHU_PULLS_JSON is unset}"
	pulls="$(gh api "repos/${GITHUB_REPOSITORY}/commits/${HEAD_SHA}/pulls")"
fi

# head.sha identifies the owning PR in the normal case; when it is not
# unique (two open PRs can share a head commit), the count -ne 1 branch
# below fails closed rather than guessing. That fail-closed branch is what
# makes the non-unique case safe -- case 4 of
# scripts/test-resolve-required-gates-pr.sh models it. base.ref is not
# unique within the set either, and for a stacked lane no element of the
# set carries a default-branch value of it at all.
matches="$(jq -c --arg sha "${HEAD_SHA}" \
	'[.[] | select(.state == "open" and .head.sha == $sha)]' <<<"${pulls}")"
count="$(jq 'length' <<<"${matches}")"

if [[ "${count}" -ne 1 ]]; then
	echo "resolve-required-gates-pr: expected exactly one open PR with head ${HEAD_SHA}; got ${count}" >&2
	exit 1
fi

jq -r '.[0].number' <<<"${matches}"
