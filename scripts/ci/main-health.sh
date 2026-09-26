#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Post-merge main-health watcher (#7111 F2).
#
# Why this exists. A required workflow can go red on `main` after two green PRs
# merge, and nothing told anyone: the guard-test red sat for 4h44m and was
# found by PR authors, and the scheduled `Required Gates` ruleset verifier
# failed every scheduled run with no owner. This script judges the NEWEST main
# commit, keeps exactly one `main is red @<sha>` issue in step with that
# verdict, and publishes a `main-health` commit status on the commit.
#
# Decision, evaluated against the current tip of main every time (never the
# commit of whichever workflow triggered the run, so a slow old run cannot
# resurrect or bury a verdict about a newer commit):
#   red     a required workflow's latest run on the tip failed in a BLOCKING
#           job, or the latest scheduled ruleset verification (`Required
#           Gates`) did not succeed
#           -> upsert the single issue, status failure; when the set of
#              blocking reds changes on an open issue, also comment on it (an
#              issue edit notifies nobody)
#           "Latest run" is judged per triggering event, because a workflow can
#           carry separate verdicts on one commit: Security Scan's push run
#           scans the tree while its workflow_run run scans the published
#           image. Red wins across events. The events that count are push,
#           schedule and workflow_run on the tip with head_branch main;
#           pull_request and merge_group runs never count. A workflow_run
#           run's head_sha/head_branch are main's HEAD when it was created,
#           whatever fired it, so Security Scan stamps the triggering run
#           into its run name ("triggered by <branch> @ <sha>") and only a
#           stamp of <branch> @ <tip> counts; an unstamped run falls back to
#           its own head_sha. A fully skipped run carries no verdict, so it never
#           supersedes an earlier run of the same event that has one.
#           Blocking-ness comes from the registry: a failed job is advisory
#           only when every gate that claims it (same ci.workflow, ci.job or
#           a check_names entry or matrix "job (...)" name) is
#           `blocking: false`. An unclaimed job, or a failed run whose jobs
#           cannot be read, counts as blocking (fail closed). The ruleset
#           verification is not a registry gate and is unconditionally
#           blocking: it guards the required-status mirror itself. Only its
#           `success` counts as green; any other conclusion is red, and no
#           completed scheduled run yet is unknown.
#   green   every required workflow that ran on the tip succeeded or failed
#           only in advisory jobs, none is pending, and the registry's source
#           workflow has a verdict
#           -> close the open issue(s), status success (advisory failures are
#              named in the status and listed in the issue, never red)
#   pending something still running / the source workflow has not registered
#           -> leave the issue alone, status pending
#   unknown a required run was cancelled and never re-run (no verdict), a run
#           listing came back truncated, or no scheduled ruleset verification
#           has completed yet (cannot prove green)
#           -> leave the issue alone, status error
#
# The required workflows are not listed here. They are derived from the two
# files the repo already drift-checks against each other: the workflows that
# `.github/workflows/required-gates.yml` aggregates (its workflow_run trigger)
# plus the registry's aggregating status source workflow
# (specs/ci-gates.v1.yaml required_status_checks[].source_workflow).
#
# Environment:
#   GITHUB_REPOSITORY       owner/name (required)
#   GH_TOKEN                token for `gh` (issues:write, statuses:write, actions:read)
#   MAIN_HEALTH_DRY_RUN     "true" prints the decision and makes NO write call
#   MAIN_HEALTH_BRANCH      branch to judge (default: main)
#   MAIN_HEALTH_REPO_ROOT   policy checkout root (default: this script's repo)
#   MAIN_HEALTH_RUN_URL     target_url for the status when no issue applies
#
# Every gh call goes through gh_get / gh_list / gh_log (reads) or write (the
# one mutator, which returns before calling gh in dry-run mode);
# scripts/test-main-health.sh fails if a gh call appears anywhere else.
#
# API cost per evaluation is bounded and independent of history length: one
# commit read, one ruleset probe (a single run), one filtered run listing per
# event (push, schedule) plus one per required workflow file that has a
# workflow_run trigger, one job listing per failed run, the open issues, and
# the tip's combined status (one read).
#
# Usage: scripts/ci/main-health.sh [--print-required]
# Exit status is 0 whenever the evaluation itself completed, red or green:
# this workflow reporting the red must not itself become another red on main.
set -euo pipefail

repo_root="${MAIN_HEALTH_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
branch="${MAIN_HEALTH_BRANCH:-main}"
dry_run="${MAIN_HEALTH_DRY_RUN:-false}"
label="main-health"
status_context="main-health"
aggregator_yml="${repo_root}/.github/workflows/required-gates.yml"
registry_yml="${repo_root}/specs/ci-gates.v1.yaml"

# shellcheck source=scripts/lib/main-health-policy.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib/main-health-policy.sh"

if [[ "${1:-}" == "--print-required" ]]; then
	required_workflows
	exit 0
fi

repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
if [[ "${dry_run}" != "true" && "${dry_run}" != "false" ]]; then
	echo "MAIN_HEALTH_DRY_RUN must be true or false" >&2
	exit 2
fi

# gh_get reads one page; gh_list follows pagination (only for listings whose
# size is bounded by their filters); gh_log reads a job log, which carries
# ANSI colour: gh >= 2.101 refuses to print escape sequences without
# --allow-escape-sequences, and an older gh without the flag falls back.
gh_get() { gh api "$1"; }
gh_list() { gh api --paginate "$1"; }
gh_log() { gh api --allow-escape-sequences "$1" 2>/dev/null || gh api "$1"; }

# write is the ONLY mutating gh call: every issue, label, pin, comment and
# status write goes through it, and in dry-run mode it reports the write and
# returns before calling gh. Args: description, then the gh api args
# (`-X METHOD path ...` or `graphql ...`).
write() {
	local desc="$1"
	shift
	if [[ "${dry_run}" == "true" ]]; then
		echo "DRY-RUN would: ${desc}" >&2
		return 0
	fi
	gh api "$@"
}

# run_jobs <run-id> prints the run's latest jobs as a JSON array, read once
# per run (classification and the issue body share it). An unreadable listing
# is an empty array, which classify_run treats as blocking.
cache_dir="$(mktemp -d)"
trap 'rm -rf "${cache_dir}"' EXIT
run_jobs() {
	local f="${cache_dir}/jobs-$1.json"
	if [[ ! -f "${f}" ]]; then
		gh_list "repos/${repo}/actions/runs/$1/jobs?filter=latest&per_page=100" 2>/dev/null |
			jq -s '[.[].jobs[]?]' >"${f}" 2>/dev/null || echo '[]' >"${f}"
	fi
	cat "${f}"
}

# classify_run <run-id> <workflow-file> prints "blocking" or "advisory" for a
# failed run: advisory only when it has failed jobs and every one is claimed
# solely by `blocking: false` registry gates of that workflow file.
classify_run() {
	run_jobs "$1" | jq -r --arg f "$2" --argjson gates "${gates_json}" '
		[.[] | select(.conclusion == "failure" or .conclusion == "timed_out")] as $failed
		| if ($failed | length) == 0 then "blocking"
		  else [$failed[] | .name as $n
		        | [$gates[] | select(.workflow == $f and .job != null) | .job as $j
		           | select($j == $n or (.checks | index([$n])) != null or ($n | startswith($j + " (")))]
		        | if length == 0 then true else any(.blocking) end]
		       | if any then "blocking" else "advisory" end end'
}

# failing_jobs <run-id> prints "job-name US failed-step US verdict" lines (US
# is the \x1f unit separator: a non-whitespace IFS keeps an empty step field
# instead of collapsing it) for up to three failed jobs of the run.
failing_jobs() {
	local run_id="$1" jobs_json
	jobs_json="$(run_jobs "${run_id}")"
	jq -r '.[] | select(.conclusion == "failure" or .conclusion == "timed_out")
		| [.id, .name, ([.steps[]? | select(.conclusion == "failure") | .name] | first // "")]
		| map(tostring | gsub("[\u001f\n]"; " ")) | join("\u001f")' <<<"${jobs_json}" |
		head -3 |
		while IFS=$'\x1f' read -r job_id job_name step_name; do
			local line=""
			line="$(gh_log "repos/${repo}/actions/jobs/${job_id}/logs" | verdict_of || true)"
			[[ -z "${line}" && -n "${step_name}" ]] && line="step failed: ${step_name}"
			[[ -z "${line}" ]] && line="no verdict line available (log unreadable)"
			printf '%s\x1f%s\x1f%s\n' "$(printf '%s' "${job_name}" | clean)" "$(printf '%s' "${step_name}" | clean)" "${line}"
		done
}

# --- newest commit on the branch --------------------------------------------
tip="$(gh_get "repos/${repo}/commits/${branch}" | jq -r '.sha')"
[[ "${tip}" =~ ^[0-9a-f]{40}$ ]] || {
	echo "could not resolve the tip of ${branch}" >&2
	exit 1
}
short="${tip:0:10}"

required="$(required_workflows)"
n_required="$(printf '%s\n' "${required}" | awk 'NF' | wc -l | tr -d ' ')"
source_workflow="$(yq '.required_status_checks[] | select(.aggregates_blocking_gates == true) | .source_workflow' "${registry_yml}" | head -1)"

# --- classify each required workflow's latest run on the tip ----------------
# Every listing is filtered server-side (head_sha + branch + event, or the
# workflow file) so its size is bounded by the commit, not by history. A
# listing whose total_count exceeds what came back (the API stops at 1000
# results) marks the evaluation truncated: it can still be red, never green.
runs_json='[]'
truncated=false
fetch_runs() { # listing path
	local page
	page="$(gh_list "$1" | jq -sc '{total: ([.[].total_count // 0] | max // 0), runs: [.[].workflow_runs[]?]}')"
	if [[ "$(jq '(.runs | length) < .total' <<<"${page}")" == "true" ]]; then
		truncated=true
		echo "::warning::run listing truncated: $1" >&2
	fi
	runs_json="$(jq -c --argjson add "$(jq -c '.runs' <<<"${page}")" '. + $add' <<<"${runs_json}")"
}
fetch_runs "repos/${repo}/actions/runs?head_sha=${tip}&branch=${branch}&event=push&per_page=100"
fetch_runs "repos/${repo}/actions/runs?head_sha=${tip}&branch=${branch}&event=schedule&per_page=100"
for wf_file in $(workflow_run_files); do
	fetch_runs "repos/${repo}/actions/workflows/${wf_file}/runs?head_sha=${tip}&branch=${branch}&event=workflow_run&per_page=100"
done
req_json="$(printf '%s\n' "${required}" | jq -R . | jq -sc 'map(select(length > 0))')"
# One entry per (required workflow, event): that event's latest decisive run.
# A workflow_run run whose name carries "triggered by <branch> @ <sha>"
# counts only when that is <branch> @ <tip>: a release-tag scan or an older
# commit's late Publish must not stand in for the tip's own scan. A run
# without the stamp falls back to its own head_sha.
events="$(jq -c --arg branch "${branch}" --arg tip "${tip}" --argjson req "${req_json}" '
	def trigger: (.display_title // "") | [match("triggered by (.+) @ ([0-9a-f]{40})$").captures | map(.string)] | first;
	def classify: .conclusion as $c
		| if .status != "completed" then "pending"
		elif (["failure","timed_out","startup_failure"] | index([$c])) then "red"
		elif (["success","neutral","skipped"] | index([$c])) then "ok"
		else "unknown" end;
	[.[] | select(.head_sha != null and .head_branch == $branch
		and (.event as $e | ["push","schedule","workflow_run"] | index([$e]))
		and (.event != "workflow_run" or (trigger | . == null or . == [$branch, $tip])))] as $runs
	| [$req[] as $name
	   | [$runs[] | select(.name == $name)] | group_by(.event)[]
	   | (map(select(.conclusion != "skipped")) | if length > 0 then . else null end) as $decisive
	   | ($decisive // .) | sort_by([.run_number, .run_attempt]) | last
	   | {workflow: $name, event, run_id: .id, file: ((.path // "") | split("/") | last),
	      url: .html_url, verdict: classify}]' <<<"${runs_json}")"

# A red whose failed jobs are all advisory in the registry becomes "advisory".
gates_json="$(blocking_gates)"
classes='{}'
while IFS=$'\t' read -r rid wf_file; do
	[[ -n "${rid}" ]] || continue
	classes="$(jq -c --arg id "${rid}" --arg c "$(classify_run "${rid}" "${wf_file}")" '. + {($id): $c}' <<<"${classes}")"
done < <(jq -r '.[] | select(.verdict == "red") | "\(.run_id)\t\(.file)"' <<<"${events}")
events="$(jq -c --argjson cls "${classes}" \
	'map(if .verdict == "red" and $cls[(.run_id | tostring)] == "advisory" then .verdict = "advisory" else . end)' <<<"${events}")"

# Per workflow, red wins across events, then pending, unknown, advisory, ok.
verdicts="$(jq -c --argjson req "${req_json}" '
	def rank: {"red": 0, "pending": 1, "unknown": 2, "advisory": 3, "ok": 4}[.];
	. as $ev
	| [$req[] as $name
	   | [$ev[] | select(.workflow == $name)] | sort_by(.verdict | rank) | first as $w
	   | {workflow: $name, event: ($w.event // null), run_id: ($w.run_id // null),
	      url: ($w.url // null), verdict: ($w.verdict // "absent")}]' <<<"${events}")"

# --- scheduled ruleset verification (verify-live-ruleset) -------------------
ruleset_file="$(ruleset_workflow_file)"
# One request for one run: never paginate this probe, since --paginate would
# walk every scheduled run in history (4 a day).
ruleset_json="$(gh_get "repos/${repo}/actions/workflows/${ruleset_file}/runs?event=schedule&status=completed&branch=${branch}&per_page=1" |
	jq -c '.workflow_runs[0] // null')"
# Fail closed: only a success proves the live ruleset still matches the
# registry. Any other conclusion (failure, timed_out, startup_failure,
# cancelled, ...) means drift was not ruled out, so it is red. No completed
# scheduled run yet is unknown: it can never make main green.
ruleset_conclusion="$(jq -r '.conclusion // "none"' <<<"${ruleset_json}")"
ruleset_red=false
ruleset_missing=false
if [[ "${ruleset_json}" == "null" ]]; then
	ruleset_missing=true
elif [[ "${ruleset_conclusion}" != "success" ]]; then
	ruleset_red=true
fi

n_red="$(jq '[.[] | select(.verdict == "red")] | length' <<<"${verdicts}")"
n_pending="$(jq '[.[] | select(.verdict == "pending")] | length' <<<"${verdicts}")"
n_unknown="$(jq '[.[] | select(.verdict == "unknown")] | length' <<<"${verdicts}")"
n_advisory="$(jq '[.[] | select(.verdict == "advisory")] | length' <<<"${events}")"
advisory_names="$(jq -r '[.[] | select(.verdict == "advisory") | .workflow] | unique | join(", ")' <<<"${events}")"
# The blocking red set, recorded in the issue body so the next evaluation can
# tell a changed set (comment) from an unchanged one (quiet edit).
red_set="$(jq -c --argjson r "${ruleset_red}" \
	'[.[] | select(.verdict == "red") | .workflow] | unique + (if $r then ["verify-live-ruleset"] else [] end)' <<<"${events}")"
source_seen="$(jq --arg s "${source_workflow}" '[.[] | select(.workflow == $s and .verdict != "absent")] | length' <<<"${verdicts}")"

if [[ "${n_red}" -gt 0 || "${ruleset_red}" == "true" ]]; then
	state=red
elif [[ "${truncated}" == "true" ]]; then
	state=unknown
elif [[ "${n_pending}" -gt 0 || "${source_seen}" -eq 0 ]]; then
	state=pending
elif [[ "${n_unknown}" -gt 0 || "${ruleset_missing}" == "true" ]]; then
	state=unknown
else
	state=green
fi

# --- open main-health issues -------------------------------------------------
# Only issues this watcher wrote (body marker) are managed; anyone with triage
# can add the label to an unrelated issue, and that issue is never closed here.
issues_json="$(gh_list "repos/${repo}/issues?state=open&labels=${label}&per_page=100" |
	jq -s '[.[][] | select(.pull_request == null and ((.body // "") | startswith("<!-- main-health:sha=")))] | sort_by(.number)')"
primary="$(jq -c 'first // null' <<<"${issues_json}")"
extras="$(jq -r '.[1:][]?.number' <<<"${issues_json}")"

status_state="" status_desc="" status_url="${MAIN_HEALTH_RUN_URL:-https://github.com/${repo}/actions}"
action=noop

close_issue() { # number, node_id, comment
	write "comment on issue #$1" -X POST "repos/${repo}/issues/$1/comments" -f body="$3" >/dev/null || true
	write "close issue #$1" -X PATCH "repos/${repo}/issues/$1" -f state=closed -f state_reason=completed >/dev/null
	if [[ -n "$2" ]]; then
		write "unpin issue #$1" graphql -f query='mutation($id:ID!){unpinIssue(input:{issueId:$id}){issue{number}}}' -f id="$2" >/dev/null 2>&1 || true
	fi
	return 0
}

# event_rows <verdict>: one issue line per failed job of each event run with
# that verdict.
event_rows() {
	local row wf ev rid url job step line
	while IFS= read -r row; do
		wf="$(jq -r '.workflow' <<<"${row}")"
		ev="$(jq -r '.event' <<<"${row}")"
		rid="$(jq -r '.run_id' <<<"${row}")"
		url="$(jq -r '.url' <<<"${row}")"
		while IFS=$'\x1f' read -r job step line; do
			echo "- **${wf}** (${ev} [run](${url})) — job \`${job}\`, step \`${step:-?}\` — verdict: \`${line}\`"
		done < <(failing_jobs "${rid}")
	done < <(jq -c --arg v "$1" '.[] | select(.verdict == $v)' <<<"${events}")
}

# comment_if_red_set_changed <number>: compare this evaluation's blocking red
# set with the one recorded in the open issue and comment when it changed.
# Only names this policy can produce (the required workflows and
# verify-live-ruleset) are read back, so an edited marker cannot inject text.
comment_if_red_set_changed() {
	local universe prev added cleared msg
	universe="$(jq -c '. + ["verify-live-ruleset"]' <<<"${req_json}")"
	prev="$(jq -r '.body // ""' <<<"${primary}" | sed -n 's/^<!-- main-health:red=\(.*\) -->$/\1/p' | head -1 |
		jq -R -c --argjson u "${universe}" '[(fromjson? // [])[]? | strings | select(. as $n | $u | index([$n]))] | unique')"
	prev="${prev:-[]}"
	added="$(jq -r --argjson p "${prev}" '. - $p | join(", ")' <<<"${red_set}")"
	cleared="$(jq -r --argjson r "${red_set}" '. - $r | join(", ")' <<<"${prev}")"
	[[ -n "${added}" || -n "${cleared}" ]] || return 0
	msg="The blocking red set changed at [\`${short}\`](https://github.com/${repo}/commit/${tip})."
	[[ -n "${added}" ]] && msg+=" Newly red: ${added}."
	[[ -n "${cleared}" ]] && msg+=" Cleared: ${cleared}."
	msg+=" Still red: $(jq -r 'join(", ")' <<<"${red_set}")."
	write "comment red-set change on issue #$1" -X POST "repos/${repo}/issues/$1/comments" -f body="${msg}" >/dev/null
}

case "${state}" in
red)
	title="main is red @${short}"
	body="$(
		{
			echo "<!-- main-health:sha=${tip} -->"
			echo "<!-- main-health:red=${red_set} -->"
			echo "\`${branch}\` is red at [\`${short}\`](https://github.com/${repo}/commit/${tip}). This issue is managed by the main-health watcher (scripts/ci/main-health.sh): it is updated as the verdict changes and closes itself once \`${branch}\` is green. Do not edit the body."
			echo
			echo "Failing blocking required workflows on this commit:"
			echo
			event_rows red
			if [[ "${ruleset_red}" == "true" ]]; then
				rid="$(jq -r '.id' <<<"${ruleset_json}")"
				url="$(jq -r '.html_url' <<<"${ruleset_json}")"
				echo "- **verify-live-ruleset** (scheduled \`Required Gates\`, conclusion \`${ruleset_conclusion}\`, [run](${url})) — the live ruleset was not proven to match specs/ci-gates.v1.yaml:"
				while IFS=$'\x1f' read -r job step line; do
					echo "  - job \`${job}\` — verdict: \`${line}\`"
				done < <(failing_jobs "${rid}")
			fi
			if [[ "${n_advisory}" -gt 0 ]]; then
				echo
				echo "Advisory failures (every failed job is \`blocking: false\` in specs/ci-gates.v1.yaml; listed, never red):"
				echo
				event_rows advisory
			fi
		} | sed -e '${/^$/d;}'
	)"
	if [[ "${primary}" == "null" ]]; then
		action=open-issue
		if [[ "${dry_run}" == "true" ]]; then
			echo "DRY-RUN would: open-issue '${title}'"
			issue_url="${status_url}"
		else
			write "create label" -X POST "repos/${repo}/labels" -f name="${label}" -f color=B60205 \
				-f description="Managed by the main-health watcher" >/dev/null 2>&1 || true
			created="$(write "open-issue" -X POST "repos/${repo}/issues" -f title="${title}" -f body="${body}" -f "labels[]=${label}")"
			issue_url="$(jq -r '.html_url' <<<"${created}")"
			node_id="$(jq -r '.node_id' <<<"${created}")"
			write "pin issue" graphql -f query='mutation($id:ID!){pinIssue(input:{issueId:$id}){issue{number}}}' -f id="${node_id}" >/dev/null 2>&1 ||
				echo "::warning::could not pin the main-health issue (pinning is best effort)"
		fi
	else
		number="$(jq -r '.number' <<<"${primary}")"
		issue_url="$(jq -r '.html_url' <<<"${primary}")"
		if [[ "$(jq -r '.title' <<<"${primary}")" == "${title}" && "$(jq -r '.body // ""' <<<"${primary}")" == "${body}" ]]; then
			action=noop
		else
			action=update-issue
			write "update-issue #${number} -> '${title}'" -X PATCH "repos/${repo}/issues/${number}" -f title="${title}" -f body="${body}" >/dev/null
			comment_if_red_set_changed "${number}"
		fi
	fi
	for dup in ${extras}; do
		close_issue "${dup}" "$(jq -r --argjson n "${dup}" '.[] | select(.number == $n) | .node_id' <<<"${issues_json}")" \
			"Duplicate main-health issue; the watcher keeps a single open issue."
	done
	status_state=failure
	red_names="$(jq -r '[.[] | select(.verdict == "red") | .workflow] | join(", ")' <<<"${verdicts}")"
	if [[ "${ruleset_red}" == "true" ]]; then
		red_names="${red_names:+${red_names}, }verify-live-ruleset"
	fi
	status_desc="main is red at ${short}: ${red_names}"
	status_url="${issue_url}"
	;;
green)
	status_state=success
	status_desc="All required workflows green on ${short}"
	[[ "${n_advisory}" -gt 0 ]] && status_desc+="; advisory failure: ${advisory_names}"
	for n in $(jq -r '.[].number' <<<"${issues_json}"); do
		action=close-issue
		close_issue "${n}" "$(jq -r --argjson n "${n}" '.[] | select(.number == $n) | .node_id' <<<"${issues_json}")" \
			"\`${branch}\` is green at ${short}; every required workflow that ran on it succeeded. Closing."
	done
	;;
pending)
	status_state=pending
	status_desc="Waiting for required workflows on ${short}"
	;;
unknown)
	status_state=error
	if [[ "${truncated}" == "true" ]]; then
		status_desc="Run listing for ${short} was truncated; cannot prove main green"
	elif [[ "${ruleset_missing}" == "true" && "${n_unknown}" -eq 0 ]]; then
		status_desc="No completed scheduled ruleset verification yet; cannot prove main green"
	else
		status_desc="A required workflow on ${short} was cancelled and has no verdict; re-run it"
	fi
	;;
esac

# --- commit status (skipped when it would repeat the latest one) ------------
status_desc="$(printf '%s' "${status_desc}" | cut -c1-140)"
# The combined status holds the latest status per context in one read.
last_status="$(gh_get "repos/${repo}/commits/${tip}/status" 2>/dev/null |
	jq --arg c "${status_context}" '[.statuses[]? | select(.context == $c)] | first // {}' || echo '{}')"
if [[ "$(jq -r '.state // ""' <<<"${last_status}")" == "${status_state}" && "$(jq -r '.description // ""' <<<"${last_status}")" == "${status_desc}" ]]; then
	echo "main-health: ${status_context} status already ${status_state} on ${short}; not reposting"
else
	write "post ${status_context}=${status_state} on ${short}" -X POST "repos/${repo}/statuses/${tip}" \
		-f state="${status_state}" -f context="${status_context}" -f description="${status_desc}" \
		-f target_url="${status_url}" >/dev/null
fi

summary="main-health: sha=${tip} state=${state} action=${action} required=${n_required} red=${n_red} advisory=${n_advisory} pending=${n_pending} unknown=${n_unknown} truncated=${truncated} ruleset_red=${ruleset_red} ruleset_conclusion=${ruleset_conclusion} open_issues=$(jq 'length' <<<"${issues_json}")"
echo "${summary}"
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
	{
		echo "### main-health"
		echo
		echo "\`${summary}\`"
		echo
		jq -r '.[] | "- \(.workflow): \(.verdict)\(if .event then " (\(.event))" else "" end)"' <<<"${verdicts}"
		jq -r '.[] | select(.verdict == "advisory") | "- advisory failure: \(.workflow) (\(.event))"' <<<"${events}"
	} >>"${GITHUB_STEP_SUMMARY}"
fi
