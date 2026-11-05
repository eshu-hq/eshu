#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# shellcheck disable=SC2154,SC2034 # globals are set by, and superseded_error is read by, the sourcing script.
#
# Superseded-attempt read for the checks arm of scripts/dev/pre-enqueue-check.sh
# (#10148). Sourced, not run: it uses the caller's GH, repo and want.
#
# The check-runs endpoint's default filter=latest returns only the newest run of
# each job, so a rerun that goes green hides the completed red attempt before
# it. The merge rule (docs/internal/agent-git-hygiene.md) says no gate on the
# head may have completed red without a disposition, whichever attempt it was.
# Run 41991514999 on PR #10143 was the case: attempt 1 of the reducer contention
# gate failed, attempt 2 passed, and the latest-only read reported fail=0.
#
# GitHub documents filter=all as "all check runs" (REST: List check runs for a
# Git reference). A rerun inside one check suite appears there as a second row
# with its own check run id. A row of filter=all whose id is not in the latest
# read is therefore a superseded attempt. Rows without an id never match.

# superseded_json is a JSON array of {id,name,conclusion,completed_at} for
# every superseded attempt that completed failure, timed_out or action_required;
# superseded_error holds the reason when the read failed (empty otherwise).
# cancelled is not listed: the merge rule says "A cancelled gate is not red:
# rerun it", so a cancelled attempt followed by a rerun is the documented path.
superseded_json='[]'
superseded_error=""

# load_superseded_reds <latest-rows-json>: fill superseded_json from a second,
# filter=all read, comparing check run ids against the rows already read with
# the default filter. Returns 1 when the read fails; the caller must fail the
# arm, never read a failed read as "no superseded attempts".
load_superseded_reds() {
	local latest_json="$1" all_err all_raw all_rc all_json
	superseded_json='[]'
	superseded_error=""
	all_err="$(mktemp)"
	all_raw="$("${GH}" api --paginate "repos/${repo}/commits/${want}/check-runs?per_page=100&filter=all" \
		--jq '.check_runs[] | {id,name,status,conclusion,completed_at}' 2>"${all_err}")"
	all_rc=$?
	if [[ "${all_rc}" -ne 0 ]]; then
		superseded_error="gh api check-runs filter=all read failed (exit ${all_rc}): $(head -n1 "${all_err}" | cut -c1-160)"
		rm -f "${all_err}"
		return 1
	fi
	rm -f "${all_err}"
	if ! all_json="$(jq -s 'if all(.[]; type == "object") then . else error("not rows") end' <<<"${all_raw}" 2>/dev/null)"; then
		superseded_error="gh api check-runs filter=all returned unparseable rows"
		return 1
	fi
	superseded_json="$(jq -c --argjson latest "${latest_json}" '
		([$latest[] | .id | select(. != null)]) as $ids
		| [.[] | select(.id != null and .status == "completed"
			and (.conclusion | IN("failure","timed_out","action_required")))
		  | select(.id as $i | ($ids | index($i)) == null)
		  | {id,name,conclusion,completed_at}]' <<<"${all_json}")" || {
		superseded_json='[]'
		superseded_error="could not compare filter=all rows with the latest read"
		return 1
	}
}

# superseded_names_json: the distinct names in superseded_json, as a JSON array.
superseded_names_json() {
	jq -c '[.[].name] | unique' <<<"${superseded_json}"
}

# superseded_entry <name>: "name (check run 2001, failure, <time>)", one
# parenthesis group per superseded attempt of that check, joined with "; ".
superseded_entry() {
	jq -r --arg n "$1" '[.[] | select(.name == $n)] | sort_by(.id)
		| map("(check run \(.id), \(.conclusion), \(.completed_at // "time unknown"))") | join("; ")' <<<"${superseded_json}"
}
