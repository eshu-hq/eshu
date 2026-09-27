#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# pre-enqueue-check.sh -- read-only, fail-closed check a coordinator runs
# immediately before asking the arbiter to approve a merge-queue enqueue
# (#7332). It re-derives, mechanically, the facts an arbiter used to re-derive
# by hand: the reviewed head, merge-tree cleanliness against origin/main AND
# the merge-queue tip, check completion, review threads, and PR-body hygiene.
# Each arm prints one PASS/FAIL line; any FAIL exits 1.
#
# It never writes to GitHub and never touches a branch, the index, or the
# worktree. `git fetch <remote> main` advances the remote-tracking ref
# <remote>/main; the head and queue-tip commits are fetched by SHA, which
# stores objects only; `git merge-tree --write-tree` writes loose objects only.
#
# Hermetic testing: every gh and git call goes through $GH and $GIT (default
# `gh`, `git`), and scripts/dev/test-pre-enqueue-check.sh puts a fake `gh` on
# PATH next to a local bare "origin" repository.
set -uo pipefail
export LC_ALL=C

# usage prints with printf, not a heredoc: a heredoc body over 512 bytes can
# hang bash >= 5.1 (#5019; the heredoc-budget gate enforces it).
usage() {
	# shellcheck disable=SC2016 # literal $(...) in the usage text.
	printf '%s\n' \
		'usage: scripts/dev/pre-enqueue-check.sh <pr-number> <expected-head-sha>' \
		'' \
		'Read-only, fail-closed pre-enqueue check. Arms (each prints PASS or FAIL):' \
		'  pr-state        PR is OPEN, not draft, headRefOid == <expected-head-sha>' \
		'  merge-main      git merge-tree --write-tree origin/main <head> is clean' \
		'  merge-queue     clean against the main merge-queue tip (the last queued' \
		'                  entry that is not this PR); prints file overlap per queued' \
		'                  PR; an empty queue passes with "queue empty"' \
		'  checks          gh pr checks: pending == 0 and fail == 0 by state bucket' \
		'                  (skipping allowed); commit status required-gates-complete' \
		'                  == success; mergeStateStatus == CLEAN' \
		'  threads         unresolved review threads == 0' \
		'  body            at least one closing keyword (Closes/Fixes/Resolves #N),' \
		'                  listed for the caller to confirm, and no AI attribution' \
		'                  (scripts/lib/ai-attribution-pattern.sh, shared with the' \
		'                  no-ai-attribution gate; prose about attribution passes)' \
		'' \
		'This script performs ONE read. The two-consecutive-stable-reads rule for CI' \
		'completion stays with the caller'\''s watcher: run it only after that watcher' \
		'has seen the full check set stable twice.' \
		'' \
		'Exit: 0 all arms PASS; 1 any arm FAIL; 2 usage or missing tool.' \
		'' \
		'Environment:' \
		'  GH_TOKEN            gh auth; callers on a machine whose active gh account cannot' \
		'                      read the repo prefix it, e.g.' \
		'                      GH_TOKEN=$(gh auth token --user <account>) scripts/dev/pre-enqueue-check.sh ...' \
		'  PRE_ENQUEUE_REPO    owner/name override (default: gh repo view)' \
		'  PRE_ENQUEUE_REMOTE  git remote to fetch from (default: origin)' \
		'  GH, GIT             command overrides for testing (default: gh, git)'
}

# shellcheck source=scripts/lib/ai-attribution-pattern.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/ai-attribution-pattern.sh"

GH="${GH:-gh}"
GIT="${GIT:-git}"
REMOTE="${PRE_ENQUEUE_REMOTE:-origin}"

if [[ $# -ne 2 ]]; then
	usage >&2
	exit 2
fi
pr="$1"
want="$2"
if ! [[ "${pr}" =~ ^[0-9]+$ ]]; then
	echo "pre-enqueue-check: pr-number must be numeric, got '${pr}'" >&2
	exit 2
fi
if ! [[ "${want}" =~ ^[0-9a-f]{40}$ ]]; then
	echo "pre-enqueue-check: expected-head-sha must be a full 40-hex SHA, got '${want}'" >&2
	exit 2
fi
for tool in jq "${GH}" "${GIT}"; do
	command -v "${tool}" >/dev/null 2>&1 || {
		echo "pre-enqueue-check: ${tool} is required" >&2
		exit 2
	}
done

failed=()
pass() { printf 'PASS %-11s %s\n' "$1" "$2"; }
fail() {
	printf 'FAIL %-11s %s\n' "$1" "$2"
	failed+=("$1")
}
note() { printf '     %-11s %s\n' "$1" "$2"; }

repo="${PRE_ENQUEUE_REPO:-}"
if [[ -z "${repo}" ]]; then
	repo="$("${GH}" repo view --json nameWithOwner 2>/dev/null | jq -r '.nameWithOwner // empty' 2>/dev/null)"
fi
if [[ -z "${repo}" || "${repo}" != */* ]]; then
	echo "pre-enqueue-check: cannot resolve the repo slug (set PRE_ENQUEUE_REPO=owner/name)" >&2
	exit 2
fi
owner="${repo%%/*}"
name="${repo#*/}"

# ensure_commit <sha>: make <sha> present locally, fetching it by SHA from the
# remote when missing. GitHub serves any reachable SHA (PR heads via
# refs/pull/N/head, merge groups via gh-readonly-queue refs).
ensure_commit() {
	"${GIT}" cat-file -e "$1^{commit}" 2>/dev/null && return 0
	"${GIT}" fetch --quiet --no-tags "${REMOTE}" "$1" >/dev/null 2>&1 || return 1
	"${GIT}" cat-file -e "$1^{commit}" 2>/dev/null
}

# --- arm 1: pr-state ---------------------------------------------------------
pr_json="$("${GH}" pr view "${pr}" --repo "${repo}" \
	--json state,isDraft,headRefOid,mergeStateStatus,body,files 2>/dev/null)"
if ! jq -e 'type == "object"' >/dev/null 2>&1 <<<"${pr_json}"; then
	fail pr-state "gh pr view ${pr} returned no PR JSON"
	pr_json='{}'
else
	state="$(jq -r '.state // "?"' <<<"${pr_json}")"
	draft="$(jq -r '.isDraft | tostring' <<<"${pr_json}")"
	head="$(jq -r '.headRefOid // "?"' <<<"${pr_json}")"
	if [[ "${state}" != OPEN ]]; then
		fail pr-state "state=${state}, want OPEN"
	elif [[ "${draft}" != false ]]; then
		fail pr-state "isDraft=${draft}, want false"
	elif [[ "${head}" != "${want}" ]]; then
		fail pr-state "headRefOid=${head} != expected ${want}"
	else
		pass pr-state "OPEN, ready, head ${want}"
	fi
fi
pr_files="$(jq -r '.files[]?.path' <<<"${pr_json}" | sort -u)"

# --- arm 2: merge-main -------------------------------------------------------
head_ok=1
ensure_commit "${want}" || head_ok=0
if ! "${GIT}" fetch --quiet --no-tags "${REMOTE}" main >/dev/null 2>&1; then
	fail merge-main "git fetch ${REMOTE} main failed"
elif [[ "${head_ok}" -eq 0 ]]; then
	fail merge-main "head ${want} not fetchable from ${REMOTE}"
else
	base="$("${GIT}" rev-parse "${REMOTE}/main" 2>/dev/null)"
	if out="$("${GIT}" merge-tree --write-tree --name-only "${base}" "${want}" 2>&1)"; then
		pass merge-main "clean against ${REMOTE}/main ${base}"
	else
		fail merge-main "conflict against ${REMOTE}/main ${base}: $(sed -n '2,6p' <<<"${out}" | tr '\n' ' ')"
	fi
fi

# --- arm 3: merge-queue ------------------------------------------------------
# shellcheck disable=SC2016 # GraphQL variables, not shell expansions.
queue_query='query($owner:String!,$name:String!){repository(owner:$owner,name:$name){mergeQueue(branch:"main"){entries(first:100){pageInfo{hasNextPage} nodes{position headCommit{oid} pullRequest{number}}}}}}'
queue_json="$("${GH}" api graphql -f query="${queue_query}" -F owner="${owner}" -F name="${name}" 2>/dev/null)"
if ! jq -e '.data.repository' >/dev/null 2>&1 <<<"${queue_json}"; then
	fail merge-queue "merge-queue GraphQL read failed"
elif [[ "$(jq -r '.data.repository.mergeQueue.entries.pageInfo.hasNextPage // false' <<<"${queue_json}")" == true ]]; then
	fail merge-queue "queue has more than 100 entries; cannot see the tip"
else
	others="$(jq -c --argjson pr "${pr}" '[.data.repository.mergeQueue.entries.nodes // [] | .[]
		| select(.pullRequest.number != $pr)] | sort_by(.position)' <<<"${queue_json}")"
	if jq -e --argjson pr "${pr}" '[.data.repository.mergeQueue.entries.nodes // [] | .[]
		| select(.pullRequest.number == $pr)] | length > 0' >/dev/null <<<"${queue_json}"; then
		note merge-queue "PR #${pr} is already queued"
	fi
	if [[ "$(jq 'length' <<<"${others}")" -eq 0 ]]; then
		pass merge-queue "queue empty"
	else
		while IFS=$'\t' read -r qpr qpos; do
			qfiles="$("${GH}" pr view "${qpr}" --repo "${repo}" --json files 2>/dev/null |
				jq -r '.files[]?.path' 2>/dev/null | sort -u)"
			overlap="$(comm -12 <(printf '%s\n' "${pr_files}") <(printf '%s\n' "${qfiles}") | sed '/^$/d' | paste -sd, -)"
			note merge-queue "queued #${qpr} (position ${qpos}) file overlap: ${overlap:-none}"
		done < <(jq -r '.[] | [.pullRequest.number, .position] | @tsv' <<<"${others}")
		# An entry's headCommit is the GitHub-built merge-group commit (main
		# plus every PR up to that entry), not the PR's own head. #7311's
		# entry 26a514a55 has parent main 98394122f; its PR head was
		# f9992c17f. So merging against the last entry covers the whole queue.
		tip="$(jq -r '.[-1].headCommit.oid // empty' <<<"${others}")"
		tip_pr="$(jq -r '.[-1].pullRequest.number' <<<"${others}")"
		if [[ -z "${tip}" ]]; then
			fail merge-queue "queue tip (#${tip_pr}) has no headCommit"
		elif ! ensure_commit "${tip}"; then
			fail merge-queue "queue tip ${tip} (#${tip_pr}) not fetchable from ${REMOTE}"
		elif [[ "${head_ok}" -eq 0 ]]; then
			fail merge-queue "head ${want} not fetchable from ${REMOTE}"
		elif out="$("${GIT}" merge-tree --write-tree --name-only "${tip}" "${want}" 2>&1)"; then
			pass merge-queue "clean against queue tip ${tip} (#${tip_pr})"
		else
			fail merge-queue "conflict against queue tip ${tip} (#${tip_pr}): $(sed -n '2,6p' <<<"${out}" | tr '\n' ' ')"
		fi
	fi
fi

# --- arm 4: checks -----------------------------------------------------------
# gh pr checks exits non-zero while checks pend or fail; the JSON is the
# verdict, so its exit status is deliberately ignored. Counts use gh's bucket
# (its classification of each row's state), never the rendered line text,
# because check NAMES can contain words like "fail" or "pending".
checks_json="$("${GH}" pr checks "${pr}" --repo "${repo}" --json name,state,bucket 2>/dev/null)"
checks_bad=()
if ! jq -e 'type == "array" and length > 0' >/dev/null 2>&1 <<<"${checks_json}"; then
	checks_bad+=("no check rows reported")
else
	total="$(jq 'length' <<<"${checks_json}")"
	pending="$(jq '[.[] | select(.bucket == "pending")] | length' <<<"${checks_json}")"
	failing="$(jq '[.[] | select(.bucket == "fail" or .bucket == "cancel")] | length' <<<"${checks_json}")"
	unknown="$(jq '[.[] | select((.bucket // "") | IN("pass","skipping","pending","fail","cancel") | not)] | length' <<<"${checks_json}")"
	note checks "rows=${total} pending=${pending} fail=${failing} unknown=${unknown}"
	[[ "${pending}" -eq 0 ]] || checks_bad+=("pending=${pending}: $(jq -r '[.[] | select(.bucket == "pending") | .name] | join(", ")' <<<"${checks_json}")")
	[[ "${failing}" -eq 0 ]] || checks_bad+=("fail=${failing}: $(jq -r '[.[] | select(.bucket == "fail" or .bucket == "cancel") | .name] | join(", ")' <<<"${checks_json}")")
	[[ "${unknown}" -eq 0 ]] || checks_bad+=("unclassified=${unknown}")
fi
status_json="$("${GH}" api "repos/${repo}/commits/${want}/status?per_page=100" 2>/dev/null)"
rgc="$(jq -r '[.statuses[]? | select(.context == "required-gates-complete") | .state] | first // "absent"' <<<"${status_json}" 2>/dev/null)"
[[ -n "${rgc}" ]] || rgc="unreadable"
[[ "${rgc}" == success ]] || checks_bad+=("required-gates-complete=${rgc}")
mss="$(jq -r '.mergeStateStatus // "?"' <<<"${pr_json}")"
[[ "${mss}" == CLEAN ]] || checks_bad+=("mergeStateStatus=${mss}")
if [[ ${#checks_bad[@]} -eq 0 ]]; then
	pass checks "complete; required-gates-complete=success; mergeStateStatus=CLEAN"
else
	fail checks "$(printf '%s; ' "${checks_bad[@]}")"
fi

# --- arm 5: threads ----------------------------------------------------------
# shellcheck disable=SC2016 # GraphQL variables, not shell expansions.
threads_query='query($owner:String!,$name:String!,$pr:Int!){repository(owner:$owner,name:$name){pullRequest(number:$pr){reviewThreads(first:100){totalCount pageInfo{hasNextPage} nodes{isResolved}}}}}'
threads_json="$("${GH}" api graphql -f query="${threads_query}" -F owner="${owner}" -F name="${name}" -F pr="${pr}" 2>/dev/null)"
rt='.data.repository.pullRequest.reviewThreads'
if ! jq -e "${rt} | type == \"object\"" >/dev/null 2>&1 <<<"${threads_json}"; then
	fail threads "reviewThreads GraphQL read failed"
elif [[ "$(jq -r "${rt}.pageInfo.hasNextPage" <<<"${threads_json}")" == true ]]; then
	fail threads "more than 100 review threads; cannot verify all resolved"
else
	unresolved="$(jq "[${rt}.nodes[] | select(.isResolved | not)] | length" <<<"${threads_json}")"
	total_threads="$(jq "${rt}.totalCount" <<<"${threads_json}")"
	if [[ "${unresolved}" -eq 0 ]]; then
		pass threads "0 unresolved of ${total_threads}"
	else
		fail threads "${unresolved} unresolved of ${total_threads}"
	fi
fi

# --- arm 6: body -------------------------------------------------------------
body="$(jq -r '.body // ""' <<<"${pr_json}")"
closes="$(rg -oi '\b(close[sd]?|fix(e[sd])?|resolve[sd]?):?[[:space:]]+#[0-9]+' <<<"${body}" | paste -sd, -)"
attrib="$(rg -oi -e "${AI_ATTRIBUTION_PATTERN}" <<<"${body}" | sort -fu | paste -sd, -)"
body_bad=()
[[ -n "${closes}" ]] || body_bad+=("no closing keyword (Closes/Fixes/Resolves #N)")
[[ -z "${attrib}" ]] || body_bad+=("AI attribution: ${attrib}")
if [[ ${#body_bad[@]} -eq 0 ]]; then
	pass body "closing keywords: ${closes} (confirm each is meant to close)"
else
	fail body "$(printf '%s; ' "${body_bad[@]}")"
fi

if [[ ${#failed[@]} -eq 0 ]]; then
	echo "pre-enqueue-check: PASS #${pr} @ ${want}"
	exit 0
fi
echo "pre-enqueue-check: FAIL #${pr} @ ${want} (failed: ${failed[*]})"
exit 1
