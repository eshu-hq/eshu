#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# shellcheck disable=SC2154 # globals are set by the sourcing script.
#
# The checks arm of scripts/dev/pre-enqueue-check.sh (#8381, #9094). Sourced,
# not run: run_checks_arm uses the caller's GH, repo, want, pr_json, in_group,
# pass, fail and note, plus ruleset_contexts, manifest_contexts, disp_ctx and
# disp_reason from pre-enqueue-check-contexts.sh.
#
# The merge rule (docs/internal/agent-git-hygiene.md): every ruleset-required
# context is success on this head, and no completed red check is left without a
# disposition. So:
#   - a ruleset-required context that is pending, red, absent or not success fails;
#   - a check the ruleset does not require, pending or absent, is a note;
#   - a completed red one fails unless --dispositioned <context>=<reason> names
#     it, and each disposition is printed in the arm output;
#   - a failure or error commit status outside the manifest is a red context
#     with the same disposition rule;
#   - outside a merge group, mergeStateStatus must be CLEAN, or UNSTABLE when at
#     least one completed red context is dispositioned and nothing else fails
#     (#9489). GitHub reports UNSTABLE for exactly that state, a non-required
#     check that completed red, so the disposition must reach the merge-state
#     comparison. BLOCKED, DIRTY, BEHIND, DRAFT and UNKNOWN still fail.

# run_checks_arm: print the checks PASS/FAIL line (and notes) for the head.
#
# Check rows come from the REST check-runs endpoint, not `gh pr checks --json`:
# gh 2.46.0 has no --json there (#8381). --paginate with --jq emits one compact
# row per check run across every page. REST, not GraphQL: a wide GraphQL check
# query times out on this repository. The endpoint's default filter=latest
# returns the latest run per name within each check suite; a re-triggered
# workflow on the same SHA adds a second suite, and every suite's rows count
# except for the manifest contexts, where the newest suite wins (older suites'
# rows of those names leave the tally, see tally_json). Never filter=all, and
# never collapse by name: one job name defined by 13 workflows is 13 distinct
# checks, and a newer pass must not hide an older failure. A run is judged by its
# status and conclusion, never by its name, because check NAMES can contain words
# like "fail" or "pending". A zero-row read is an error, never "pending".
run_checks_arm() {
	local runs_err runs_raw runs_rc checks_json tally_json generic_json total
	local failing_sel pending_sel pending failing failing_bad unknown multi_names orphan_hint
	local manifest_json ruleset_json nonruleset_json elig_json
	local -a checks_bad=()
	failing_sel='select(.status == "completed" and (.conclusion | IN("failure","cancelled","timed_out","action_required")))'
	pending_sel='select(.status != "completed")'
	runs_err="$(mktemp)"
	generic_json='[]'
	manifest_json="$(json_array "${manifest_contexts[@]}")"
	ruleset_json="$(json_array "${ruleset_contexts[@]}")"
	nonruleset_json="$(jq -c --argjson r "${ruleset_json}" '[.[] | select(IN($r[]) | not)]' <<<"${manifest_json}")"
	# Dispositions that can excuse a row: the ruleset blocks its own contexts
	# whatever anyone writes, so those are ignored (and printed as ignored).
	elig_json="$(json_array ${disp_ctx[@]+"${disp_ctx[@]}"} | jq -c --argjson r "${ruleset_json}" '[.[] | select(IN($r[]) | not)]')"
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
		# cancelled, timed_out or action_required is red; not completed is pending;
		# any other conclusion (stale, null, ...) is unclassified and fails closed.
		# All rows count here, across every suite: fail-closed. The one exception is
		# the manifest contexts (each pinned to one workflow and job, see the
		# context loop below): only their newest suite's rows are tallied, so a
		# stale failure in an older suite does not keep the arm red. Suite 0 is a
		# row with no check_suite: it is never superseded and always tallied.
		tally_json="$(jq -c --argjson req "${manifest_json}" '
			(reduce .[] as $r ({}; if $r.suite == 0 then . else .[$r.name] = ([(.[$r.name] // 0), $r.suite] | max) end)) as $top
			| [.[] | select(((.name | IN($req[])) | not) or .suite == 0 or .suite == $top[.name])]' <<<"${checks_json}")"
		# Contexts the ruleset does not require are judged by the context loop
		# below (check run or status, newest suite, dispositions), not as rows.
		generic_json="$(jq -c --argjson n "${nonruleset_json}" '[.[] | select((.name | IN($n[])) | not)]' <<<"${tally_json}")"
		total="$(jq 'length' <<<"${checks_json}")"
		pending="$(jq "[.[] | ${pending_sel}] | length" <<<"${generic_json}")"
		failing="$(jq "[.[] | ${failing_sel}] | length" <<<"${generic_json}")"
		failing_bad="$(jq --argjson d "${elig_json}" "[.[] | ${failing_sel} | select((.name | IN(\$d[])) | not)] | length" <<<"${generic_json}")"
		unknown="$(jq '[.[] | select(.status == "completed" and (.conclusion | IN("success","skipped","neutral","failure","cancelled","timed_out","action_required") | not))] | length' <<<"${generic_json}")"
		note checks "rows=${total} pending=${pending} fail=${failing} unknown=${unknown}"
		# A name with rows in more than one suite has no GitHub-defined winner.
		while IFS= read -r multi_line; do
			[[ -z "${multi_line}" ]] || note checks "multi-suite: ${multi_line}"
		done < <(jq -r '[group_by(.name)[] | (map(.suite) | unique) as $s | select(($s | length) > 1)
			| "\(.[0].name) (\($s | length) suites: \($s | map(tostring) | join(", ")))"] | .[]' <<<"${checks_json}")
		multi_names="$(jq -c '[group_by(.name)[] | select((map(.suite) | unique | length) > 1) | .[0].name]' <<<"${checks_json}")"
		orphan_hint=" -- older run on the same SHA still counted; GitHub defines no winner across suites; wait for or cancel the orphan (gh run list --commit ${want})"
		# rows_entry <label> <count> <selector> [extra-jq-args...]: "label=count:
		# names", plus the orphan hint when a picked name is multi-suite.
		rows_entry() {
			local label="$1" count="$2" sel="$3" names hits hint=""
			shift 3
			names="$(jq -r "[.[] | ${sel} | .name] | unique | join(\", \")" "$@" <<<"${generic_json}")"
			hits="$(jq --argjson m "${multi_names}" "[.[] | ${sel} | .name | select(IN(\$m[]))] | length" "$@" <<<"${generic_json}")"
			[[ "${hits}" -eq 0 ]] || hint="${orphan_hint}"
			printf '%s=%s: %s%s' "${label}" "${count}" "${names}" "${hint}"
		}
		# Pending rows of checks the ruleset does not require never block (#9094).
		[[ "${pending}" -eq 0 ]] || note checks "$(rows_entry pending "${pending}" "${pending_sel}")"
		[[ "${failing_bad}" -eq 0 ]] ||
			checks_bad+=("$(rows_entry fail "${failing_bad}" "${failing_sel} | select((.name | IN(\$d[])) | not)" --argjson d "${elig_json}")")
		[[ "${unknown}" -eq 0 ]] || checks_bad+=("unclassified=${unknown}")
	fi
	local status_json ctx ctx_state superseded red_states="" s_ctx s_state
	status_json="$("${GH}" api "repos/${repo}/commits/${want}/status?per_page=100" 2>/dev/null)"
	# A commit status outside the manifest (another reporter) has no check-run
	# row, so the tally above never sees it. A failure or error one is a red
	# context like any other: it fails the arm unless --dispositioned names it,
	# and a dispositioned one is excused (#9489 review F1). An unreadable status
	# read already fails closed through the manifest's own status contexts.
	while IFS=$'\t' read -r s_ctx s_state; do
		[[ -n "${s_ctx}" ]] || continue
		red_states+="${s_ctx}"$'\t'"${s_state}"$'\n'
		if ! in_list "${s_ctx}" ${disp_ctx[@]+"${disp_ctx[@]}"}; then
			checks_bad+=("${s_ctx}=${s_state} (commit status; pass --dispositioned ${s_ctx}=<issue-or-reason> to accept)")
		fi
	done < <(jq -r --argjson m "${manifest_json}" '[.statuses[]? | select((.context | IN($m[])) | not)
		| select(.state == "failure" or .state == "error")] | unique_by(.context) | .[]
		| "\(.context)\t\(.state)"' <<<"${status_json}" 2>/dev/null)
	# Every manifest context is read here. go-core-complete and go-race-complete
	# publish as check runs, required-gates-complete as a commit status. A check
	# run of that name wins; the status is the fallback. Each is pinned to exactly
	# one workflow and job (specs/ci-gates.v1.yaml, required_status_checks), so two
	# suites on one SHA are two runs of the same workflow, and only the NEWEST suite
	# (max suite id, then all its rows) decides the context. GitHub: "Using the
	# same job name in multiple workflows can cause ambiguous status check
	# results" (About protected branches). A superseded suite is named in a note
	# and its rows are excluded from the tally above.
	for ctx in "${manifest_contexts[@]}"; do
		ctx_state="$(jq -r --arg c "${ctx}" '
			[.[] | select(.name == $c)] as $all
			| if ($all | length) == 0 then "absent"
			  else ($all | map(.suite) | max) as $top
			  | [$all[] | select(.suite == $top or .suite == 0)] as $r
			  | if all($r[]; .status == "completed" and .conclusion == "success") then "success"
			    elif any($r[]; .status != "completed") then "pending"
			    else ([$r[] | select(.conclusion != "success") | .conclusion] | first // "unknown") end end' <<<"${checks_json}" 2>/dev/null)"
		superseded="$(jq -r --arg c "${ctx}" '
			[.[] | select(.name == $c)] as $all
			| ($all | map(.suite) | max) as $top
			| [$all[] | select(.suite != $top and .suite != 0 and (.status != "completed" or .conclusion != "success")) | .suite]
			| unique | map(tostring) | join(", ")' <<<"${checks_json}" 2>/dev/null)"
		if [[ -n "${superseded}" ]]; then
			note checks "${ctx}: newest suite $(jq -r --arg c "${ctx}" '[.[] | select(.name == $c) | .suite] | max' <<<"${checks_json}") wins; superseded suite(s): ${superseded}"
		fi
		if [[ "${ctx_state}" == absent ]]; then
			ctx_state="$(jq -r --arg c "${ctx}" '[.statuses[]? | select(.context == $c) | .state] | first // "absent"' <<<"${status_json}" 2>/dev/null)"
			[[ -n "${ctx_state}" ]] || ctx_state="unreadable"
		fi
		if in_list "${ctx}" "${ruleset_contexts[@]}"; then
			[[ "${ctx_state}" == success ]] || checks_bad+=("${ctx}=${ctx_state}")
			continue
		fi
		# Not ruleset-required: success, skipped and neutral are fine; pending and
		# absent are a note; anything else is red and needs a disposition.
		case "${ctx_state}" in
		success | skipped | neutral) ;;
		pending | absent) note checks "${ctx}=${ctx_state} (not ruleset-required; does not block)" ;;
		*)
			red_states+="${ctx}"$'\t'"${ctx_state}"$'\n'
			if ! in_list "${ctx}" ${disp_ctx[@]+"${disp_ctx[@]}"}; then
				checks_bad+=("${ctx}=${ctx_state} (not ruleset-required; pass --dispositioned ${ctx}=<issue-or-reason> to accept)")
			fi
			;;
		esac
	done
	local i state rows red_state
	# excused: the dispositioned contexts that were completed red, in flag order.
	local -a excused=()
	for ((i = 0; i < ${#disp_ctx[@]}; i++)); do
		ctx="${disp_ctx[i]}"
		if in_list "${ctx}" "${ruleset_contexts[@]}"; then
			state="ignored: ruleset-required"
		elif red_state="$(RED_CTX="${ctx}" awk -F'\t' '$1 == ENVIRON["RED_CTX"] {print $2; exit}' <<<"${red_states}")" && [[ -n "${red_state}" ]]; then
			state="applied: ${red_state}"
		else
			rows="$(jq -r --arg c "${ctx}" "[.[] | select(.name == \$c) | ${failing_sel}] | length" <<<"${generic_json}" 2>/dev/null)"
			if [[ "${rows:-0}" -gt 0 ]]; then
				state="applied: ${rows} failing row(s)"
			else
				state="unused: not red"
			fi
		fi
		# A context dispositioned twice is excused (and named) once.
		[[ "${state}" != applied:* ]] || in_list "${ctx}" ${excused[@]+"${excused[@]}"} || excused+=("${ctx}")
		note checks "dispositioned: ${ctx}=${disp_reason[i]} (${state})"
	done
	local mss group_label="" ruleset_list excused_list="" unstable_ok=0
	mss="$(jq -r '.mergeStateStatus // "?"' <<<"${pr_json}")"
	# A PR already in a merge group reads mergeStateStatus UNKNOWN (PR #8000,
	# rulings a9-135.2, a9-139.2): the row is labeled IN-GROUP and accepts only
	# UNKNOWN or CLEAN; any other value fails. Any other PR needs CLEAN, or
	# UNSTABLE when every completed red context is dispositioned (#9489): the
	# arm has found nothing else wrong (checks_bad is empty, so every
	# ruleset-required context is success and no red is undispositioned) and at
	# least one dispositioned context really was red. UNSTABLE with no excused
	# red has no known cause here, so it fails closed.
	if [[ "${in_group}" -eq 1 ]]; then
		group_label="IN-GROUP: "
		[[ "${mss}" == UNKNOWN || "${mss}" == CLEAN ]] || checks_bad+=("mergeStateStatus=${mss}")
	elif [[ "${mss}" == UNSTABLE && ${#checks_bad[@]} -eq 0 && ${#excused[@]} -gt 0 ]]; then
		unstable_ok=1
		excused_list="$(printf '%s, ' "${excused[@]}")"
		excused_list="${excused_list%, }"
	else
		[[ "${mss}" == CLEAN ]] || checks_bad+=("mergeStateStatus=${mss}")
	fi
	ruleset_list="$(IFS=,; echo "${ruleset_contexts[*]}")"
	if [[ ${#checks_bad[@]} -eq 0 && -n "${group_label}" ]]; then
		pass checks "${group_label}complete; ${ruleset_list}=success; mergeStateStatus=${mss} (PR is in a merge group)"
	elif [[ ${#checks_bad[@]} -eq 0 && "${unstable_ok}" -eq 1 ]]; then
		pass checks "complete; ${ruleset_list}=success; mergeStateStatus=UNSTABLE (dispositioned: ${excused_list})"
	elif [[ ${#checks_bad[@]} -eq 0 ]]; then
		pass checks "complete; ${ruleset_list}=success; mergeStateStatus=CLEAN"
	else
		fail checks "${group_label}$(printf '%s; ' "${checks_bad[@]}")"
	fi
	rm -f "${runs_err}"
}
