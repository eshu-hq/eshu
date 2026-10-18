#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# pre-enqueue-check.sh -- read-only, fail-closed check a coordinator runs
# immediately before asking the arbiter to approve a merge (#7332). It was
# written for a merge-queue enqueue; main has had no merge queue since
# 2026-10-16 (#8288), so the merge-queue arm reads no queue and passes with
# "queue empty". It re-derives, mechanically, the facts an arbiter used to re-derive
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
		'  merge-main      git merge-tree --write-tree origin/main <head> is clean;' \
		'                  notes base drift (commits past the merge base, files' \
		'                  both sides changed)' \
		'  merge-queue     clean against the main merge-queue tip (the last queued' \
		'                  entry that is not this PR); prints file overlap per queued' \
		'                  PR; an empty or absent queue (main has none) passes with' \
		'                  "queue empty"' \
		'  checks          check runs for the head, read with gh api (REST) because gh' \
		'                  2.46.0 has no gh pr checks --json: every run completed, none' \
		'                  failure/cancelled/timed_out/action_required (skipped and' \
		'                  neutral allowed), at least one row; go-core-complete,' \
		'                  go-race-complete and required-gates-complete (check run or' \
		'                  commit status) == success; mergeStateStatus == CLEAN; a PR' \
		'                  already in the merge queue is labeled IN-GROUP and accepts' \
		'                  UNKNOWN (GitHub reports it for a queued PR) or CLEAN' \
		'  threads         unresolved review threads == 0' \
		'  body            at least one issue reference (#N) or closing keyword' \
		'                  (Closes/Fixes/Resolves #N), labeled for the caller to' \
		'                  confirm, no AI attribution' \
		'                  (scripts/lib/ai-attribution-pattern.sh, shared with the' \
		'                  no-ai-attribution gate; prose about attribution passes),' \
		'                  and no environment or organization identifier' \
		'                  (scripts/lib/private-identifier-pattern.sh, shared with the' \
		'                  no-private-identifiers gate)' \
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
# shellcheck source=scripts/lib/private-identifier-pattern.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/private-identifier-pattern.sh"

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
	# The merge base comes first: with none there is nothing to merge-tree or
	# measure drift against, and the arm reports that one cause, not two.
	mb="$("${GIT}" merge-base "${base}" "${want}" 2>/dev/null)"
	if [[ -z "${mb}" ]]; then
		fail merge-main "no merge base between ${REMOTE}/main and ${want}"
	else
		if out="$("${GIT}" merge-tree --write-tree --name-only "${base}" "${want}" 2>&1)"; then
			pass merge-main "clean against ${REMOTE}/main ${base}"
		else
			fail merge-main "conflict against ${REMOTE}/main ${base}: $(sed -n '2,6p' <<<"${out}" | tr '\n' ' ')"
		fi
		# Base drift: how far main has moved past the merge base, and which
		# files both sides changed. A clean merge-tree can still hide a
		# regenerated artifact (ci-gates.md) that must be rebuilt, so the
		# arbiter needs this even when the merge is clean. Each git call is
		# checked: a failed diff must not print "none".
		if ahead="$("${GIT}" rev-list --count "${mb}..${base}" 2>/dev/null)" &&
			main_files="$("${GIT}" diff --name-only "${mb}" "${base}" 2>/dev/null)" &&
			head_files="$("${GIT}" diff --name-only "${mb}" "${want}" 2>/dev/null)"; then
			both="$(comm -12 <(sort -u <<<"${main_files}") <(sort -u <<<"${head_files}") | sed '/^$/d' | paste -sd, -)"
			note merge-main "base drift: ${ahead} commit(s) on ${REMOTE}/main since the merge base; files changed on both sides: ${both:-none}"
		else
			fail merge-main "base drift could not be computed from merge base ${mb}"
		fi
	fi
fi

# --- arm 3: merge-queue ------------------------------------------------------
# in_group records whether this PR is already a merge-queue entry; the checks
# arm reads it (GitHub reports mergeStateStatus UNKNOWN for a queued PR).
# main has no merge queue (#8288): GraphQL returns mergeQueue null, the entries
# read as empty, and the arm passes with "queue empty".
in_group=0
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
		in_group=1
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
# Check rows come from the REST check-runs endpoint, not `gh pr checks --json`:
# gh 2.46.0 has no --json there (#8381). --paginate with --jq emits one compact
# row per check run across every page. REST, not GraphQL: a wide GraphQL check
# query times out on this repository. The endpoint's default filter=latest
# returns the latest run per name within each check suite; a re-triggered
# workflow on the same SHA adds a second suite, and every suite's rows count
# except for the three required contexts, where the newest suite wins. Never
# filter=all, and never collapse by name: one job name defined by 13 workflows
# is 13 distinct checks, and a newer pass must not hide an older failure. A run
# is judged by its status and conclusion, never by its name, because check NAMES
# can contain words like "fail" or "pending". A zero-row read is an error, never
# "pending".
runs_err="$(mktemp)"
trap 'rm -f "${runs_err}"' EXIT
checks_bad=()
runs_raw="$("${GH}" api --paginate "repos/${repo}/commits/${want}/check-runs?per_page=100" \
	--jq '.check_runs[] | {id,name,status,conclusion,suite:(.check_suite.id // 0)}' 2>"${runs_err}")"
runs_rc=$?
checks_json='[]'
if [[ "${runs_rc}" -ne 0 ]]; then
	checks_bad+=("gh api check-runs read failed (exit ${runs_rc}): $(head -n1 "${runs_err}" | cut -c1-160)")
elif ! checks_json="$(jq -s 'if all(.[]; type == "object") then . else error("not rows") end' <<<"${runs_raw}" 2>/dev/null)"; then
	checks_json='[]'
	checks_bad+=("gh api check-runs returned unparseable rows")
elif [[ "$(jq 'length' <<<"${checks_json}")" -eq 0 ]]; then
	checks_bad+=("no check rows reported for ${want}")
else
	# completed + success/skipped/neutral is fine; completed + failure,
	# cancelled, timed_out or action_required fails; not completed is pending;
	# any other conclusion (stale, null, ...) is unclassified and fails closed.
	# All rows count here, across every suite: fail-closed.
	total="$(jq 'length' <<<"${checks_json}")"
	failing_sel='select(.status == "completed" and (.conclusion | IN("failure","cancelled","timed_out","action_required")))'
	pending_sel='select(.status != "completed")'
	pending="$(jq "[.[] | ${pending_sel}] | length" <<<"${checks_json}")"
	failing="$(jq "[.[] | ${failing_sel}] | length" <<<"${checks_json}")"
	unknown="$(jq '[.[] | select(.status == "completed" and (.conclusion | IN("success","skipped","neutral","failure","cancelled","timed_out","action_required") | not))] | length' <<<"${checks_json}")"
	note checks "rows=${total} pending=${pending} fail=${failing} unknown=${unknown}"
	# A name with rows in more than one suite has no GitHub-defined winner.
	while IFS= read -r multi_line; do
		[[ -z "${multi_line}" ]] || note checks "multi-suite: ${multi_line}"
	done < <(jq -r '[group_by(.name)[] | (map(.suite) | unique) as $s | select(($s | length) > 1)
		| "\(.[0].name) (\($s | length) suites: \($s | map(tostring) | join(", ")))"] | .[]' <<<"${checks_json}")
	multi_names="$(jq -c '[group_by(.name)[] | select((map(.suite) | unique | length) > 1) | .[0].name]' <<<"${checks_json}")"
	orphan_hint=" -- older run on the same SHA still counted; GitHub defines no winner across suites; wait for or cancel the orphan (gh run list --commit ${want})"
	# bad_entry <label> <count> <selector>: one checks_bad entry for the rows a
	# selector picks; the orphan hint is appended when a picked name is multi-suite.
	bad_entry() {
		local names hits hint=""
		names="$(jq -r "[.[] | $3 | .name] | unique | join(\", \")" <<<"${checks_json}")"
		hits="$(jq --argjson m "${multi_names}" "[.[] | $3 | .name | select(IN(\$m[]))] | length" <<<"${checks_json}")"
		[[ "${hits}" -eq 0 ]] || hint="${orphan_hint}"
		checks_bad+=("$1=$2: ${names}${hint}")
	}
	[[ "${pending}" -eq 0 ]] || bad_entry pending "${pending}" "${pending_sel}"
	[[ "${failing}" -eq 0 ]] || bad_entry fail "${failing}" "${failing_sel}"
	[[ "${unknown}" -eq 0 ]] || checks_bad+=("unclassified=${unknown}")
fi
status_json="$("${GH}" api "repos/${repo}/commits/${want}/status?per_page=100" 2>/dev/null)"
# The three ruleset-required contexts must each be success. go-core-complete and
# go-race-complete publish as check runs, required-gates-complete as a commit
# status. A check run of that name wins; the status is the fallback. Each is
# pinned to exactly one workflow and job (specs/ci-gates.v1.yaml,
# required_status_checks), so two suites on one SHA are two runs of the same
# workflow, and only the NEWEST suite (max suite id, then all its rows) decides
# the context. GitHub: "Using the same job name in multiple workflows can cause
# ambiguous status check results" (About protected branches). A superseded
# suite is named in a note; it still counts in the all-rows tally above.
for ctx in go-core-complete go-race-complete required-gates-complete; do
	ctx_state="$(jq -r --arg c "${ctx}" '
		[.[] | select(.name == $c)] as $all
		| if ($all | length) == 0 then "absent"
		  else ($all | map(.suite) | max) as $top
		  | [$all[] | select(.suite == $top)] as $r
		  | if all($r[]; .status == "completed" and .conclusion == "success") then "success"
		    elif any($r[]; .status != "completed") then "pending"
		    else ([$r[] | select(.conclusion != "success") | .conclusion] | first // "unknown") end end' <<<"${checks_json}" 2>/dev/null)"
	superseded="$(jq -r --arg c "${ctx}" '
		[.[] | select(.name == $c)] as $all
		| ($all | map(.suite) | max) as $top
		| [$all[] | select(.suite != $top and (.status != "completed" or .conclusion != "success")) | .suite]
		| unique | map(tostring) | join(", ")' <<<"${checks_json}" 2>/dev/null)"
	if [[ -n "${superseded}" ]]; then
		note checks "${ctx}: newest suite $(jq -r --arg c "${ctx}" '[.[] | select(.name == $c) | .suite] | max' <<<"${checks_json}") wins; superseded suite(s): ${superseded}"
	fi
	if [[ "${ctx_state}" == absent ]]; then
		ctx_state="$(jq -r --arg c "${ctx}" '[.statuses[]? | select(.context == $c) | .state] | first // "absent"' <<<"${status_json}" 2>/dev/null)"
		[[ -n "${ctx_state}" ]] || ctx_state="unreadable"
	fi
	[[ "${ctx_state}" == success ]] || checks_bad+=("${ctx}=${ctx_state}")
done
mss="$(jq -r '.mergeStateStatus // "?"' <<<"${pr_json}")"
# A PR already in a merge group reads mergeStateStatus UNKNOWN (PR #8000,
# rulings a9-135.2, a9-139.2): the row is labeled IN-GROUP and accepts only
# UNKNOWN or CLEAN; any other value fails. Any other PR still needs CLEAN.
group_label=""
if [[ "${in_group}" -eq 1 ]]; then
	group_label="IN-GROUP: "
	[[ "${mss}" == UNKNOWN || "${mss}" == CLEAN ]] || checks_bad+=("mergeStateStatus=${mss}")
else
	[[ "${mss}" == CLEAN ]] || checks_bad+=("mergeStateStatus=${mss}")
fi
if [[ ${#checks_bad[@]} -eq 0 && -n "${group_label}" ]]; then
	pass checks "${group_label}complete; required-gates-complete=success; mergeStateStatus=${mss} (PR is in a merge group)"
elif [[ ${#checks_bad[@]} -eq 0 ]]; then
	pass checks "complete; required-gates-complete=success; mergeStateStatus=CLEAN"
else
	fail checks "${group_label}$(printf '%s; ' "${checks_bad[@]}")"
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
references="$(rg -oi '#[0-9]+' <<<"${body}" | sort -fu | paste -sd, -)"
closing_references="$(rg -oi '#[0-9]+' <<<"${closes}" | sort -fu | paste -sd, -)"
non_closing_references="${references}"
while IFS= read -r closing_reference; do
	[[ -n "${closing_reference}" ]] || continue
	non_closing_references="$(tr ',' '\n' <<<"${non_closing_references}" |
		rg -Fvx -- "${closing_reference}" | paste -sd, -)"
done <<<"$(tr ',' '\n' <<<"${closing_references}")"
# rg exits 1 for "no match" and 2+ for an error (e.g. an invalid pattern); an
# error must fail the arm, never read as a clean body.
attrib_hits="$(rg -oi -e "${AI_ATTRIBUTION_PATTERN}" <<<"${body}")"
attrib_rc=$?
attrib="$(sort -fu <<<"${attrib_hits}" | sed '/^$/d' | paste -sd, -)"
# The private-identifier scan follows the same exit-code rule. The report names
# the body line numbers only: the failure text reaches a terminal, a PR comment
# and CI logs, and must not repeat the identifier it refuses.
private_rc=0
private_hits="$(rg -n -e "${PRIVATE_IDENTIFIER_PATTERN}" <<<"${body}")" || private_rc=$?
private_lines="$(cut -d: -f1 <<<"${private_hits}" | sed '/^$/d' | paste -sd, -)"
body_bad=()
[[ -n "${closes}" || -n "${references}" ]] || body_bad+=("no issue reference (#N or Closes/Fixes/Resolves #N)")
if [[ "${attrib_rc}" -gt 1 ]]; then
	body_bad+=("attribution scan failed (rg exit ${attrib_rc}); body not verified")
elif [[ -n "${attrib}" ]]; then
	body_bad+=("AI attribution: ${attrib}")
fi
if [[ "${private_rc}" -gt 1 ]]; then
	body_bad+=("private-identifier scan failed (rg exit ${private_rc}); body not verified")
elif [[ -n "${private_lines}" ]]; then
	body_bad+=("environment or organization identifier on body line(s) ${private_lines}")
fi
if [[ ${#body_bad[@]} -eq 0 ]]; then
	body_note=""
	if [[ -n "${closes}" ]]; then
		body_note="closing keywords: ${closes} (confirm each is meant to close)"
	fi
	if [[ -n "${non_closing_references}" ]]; then
		[[ -z "${body_note}" ]] || body_note+="; "
		body_note+="non-closing references: ${non_closing_references} (do not close on merge)"
	fi
	pass body "${body_note}"
else
	fail body "$(printf '%s; ' "${body_bad[@]}")"
fi

if [[ ${#failed[@]} -eq 0 ]]; then
	echo "pre-enqueue-check: PASS #${pr} @ ${want}"
	exit 0
fi
echo "pre-enqueue-check: FAIL #${pr} @ ${want} (failed: ${failed[*]})"
exit 1
