#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Post-merge main-health watcher (#7111 F2).
#
# Why this exists. A required workflow can go red on `main` after two green PRs
# merge, and nothing told anyone: the guard-test red sat for 4h44m and was
# found by PR authors, and the scheduled `Required Gates` ruleset verifier
# failed 30/30 runs with no owner. This script judges the NEWEST main commit,
# keeps exactly one `main is red @<sha>` issue in step with that verdict, and
# publishes a `main-health` commit status on the commit.
#
# Decision, evaluated against the current tip of main every time (never the
# commit of whichever workflow triggered the run, so a slow old run cannot
# resurrect or bury a verdict about a newer commit):
#   red     any required workflow's latest run on the tip failed, or the latest
#           scheduled ruleset verification (`Required Gates`) failed
#           -> upsert the single issue, status failure
#   green   every required workflow that ran on the tip succeeded, none is
#           pending, and the registry's source workflow has a verdict
#           -> close the open issue(s), status success
#   pending something still running / the source workflow has not registered
#           -> leave the issue alone, status pending
#   unknown a required run was cancelled and never re-run (no verdict)
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

# required_workflows prints the display names of every workflow whose verdict
# on main counts, one per line.
required_workflows() {
	{
		yq '.on.workflow_run.workflows[]' "${aggregator_yml}"
		yq '.required_status_checks[] | select(.aggregates_blocking_gates == true) | .source_workflow' "${registry_yml}"
	} | awk 'NF && $0 != "null"' | sort -u
}

# ruleset_workflow_file prints the workflow file that owns the scheduled
# ruleset verification: the aggregating status check's workflow.
ruleset_workflow_file() {
	yq '.required_status_checks[] | select(.aggregates_blocking_gates == true) | .workflow' "${registry_yml}" | head -1
}

if [[ "${1:-}" == "--print-required" ]]; then
	required_workflows
	exit 0
fi

repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
if [[ "${dry_run}" != "true" && "${dry_run}" != "false" ]]; then
	echo "MAIN_HEALTH_DRY_RUN must be true or false" >&2
	exit 2
fi

# clean makes untrusted log text safe to embed in an issue: control characters
# and ANSI removed, no @mentions, no backticks, bounded length.
clean() {
	sed -e $'s/\x1b\\[[0-9;]*[A-Za-z]//g' | tr -d '\000-\010\013-\037' |
		awk '{ gsub(/@/, "(at)"); gsub(/`/, "\x27"); print }' | cut -c1-300
}

gh_read() { gh api --paginate "$1"; }

# write runs a mutating gh call, or only reports it in dry-run mode.
write() { # description, then the gh api args
	local desc="$1"
	shift
	if [[ "${dry_run}" == "true" ]]; then
		echo "DRY-RUN would: ${desc}" >&2
		return 0
	fi
	gh api -X "$@"
}

# verdict_of prints the single most useful line from a job log: the first Go
# test failure or panic if there is one, else the last line before the first
# Actions error annotation, else that annotation.
verdict_of() {
	sed -e 's/^[0-9T:.Z-]* //' |
		awk '
			/^(--- FAIL:|FAIL[ \t]|FAIL$|panic: )/ { if (!fail) fail = $0 }
			/^##\[error\]/ { if (!err) { err = $0; sub(/^##\[error\]/, "", err); before = prev } }
			NF && !/^##\[/ { prev = $0 }
			END {
				if (fail) print fail
				else if (err ~ /^Process completed with exit code/ && before != "") print before
				else if (err != "") print err
			}' | clean
}

# failing_jobs <run-id> prints "job-name<TAB>failed-step<TAB>verdict" lines for
# up to three failed jobs of the run.
failing_jobs() {
	local run_id="$1" jobs_json
	jobs_json="$(gh_read "repos/${repo}/actions/runs/${run_id}/jobs?filter=latest&per_page=100" 2>/dev/null | jq -s '[.[].jobs[]?]')" || jobs_json='[]'
	jq -r '.[] | select(.conclusion == "failure" or .conclusion == "timed_out")
		| [.id, .name, ([.steps[]? | select(.conclusion == "failure") | .name] | first // "")] | @tsv' <<<"${jobs_json}" |
		head -3 |
		while IFS=$'\t' read -r job_id job_name step_name; do
			local line=""
			line="$(gh api "repos/${repo}/actions/jobs/${job_id}/logs" 2>/dev/null | verdict_of || true)"
			[[ -z "${line}" && -n "${step_name}" ]] && line="step failed: ${step_name}"
			[[ -z "${line}" ]] && line="no verdict line available (log unreadable)"
			printf '%s\t%s\t%s\n' "$(printf '%s' "${job_name}" | clean)" "$(printf '%s' "${step_name}" | clean)" "${line}"
		done
}

# --- newest commit on the branch --------------------------------------------
tip="$(gh api "repos/${repo}/commits/${branch}" | jq -r '.sha')"
[[ "${tip}" =~ ^[0-9a-f]{40}$ ]] || {
	echo "could not resolve the tip of ${branch}" >&2
	exit 1
}
short="${tip:0:10}"

required="$(required_workflows)"
source_workflow="$(yq '.required_status_checks[] | select(.aggregates_blocking_gates == true) | .source_workflow' "${registry_yml}" | head -1)"

# --- classify each required workflow's latest run on the tip ----------------
runs_json="$(gh_read "repos/${repo}/actions/runs?head_sha=${tip}&per_page=100" | jq -s '[.[].workflow_runs[]?]')"
verdicts="$(jq -c --arg branch "${branch}" --argjson req "$(printf '%s\n' "${required}" | jq -R . | jq -s .)" '
	[.[] | select(.event == "push" and .head_branch == $branch)] as $runs
	| [$req[] as $name
	   | ($runs | map(select(.name == $name)) | sort_by([.run_number, .run_attempt]) | last) as $r
	   | {workflow: $name, run_id: ($r.id // null), url: ($r.html_url // null),
	      verdict: (if $r == null then "absent"
	        elif $r.status != "completed" then "pending"
	        elif (["failure","timed_out","startup_failure"] | index($r.conclusion)) then "red"
	        elif (["success","neutral","skipped"] | index($r.conclusion)) then "ok"
	        else "unknown" end)}]' <<<"${runs_json}")"

# --- scheduled ruleset verification (verify-live-ruleset) -------------------
ruleset_file="$(ruleset_workflow_file)"
ruleset_json="$(gh_read "repos/${repo}/actions/workflows/${ruleset_file}/runs?event=schedule&status=completed&per_page=1" |
	jq -sc '[.[].workflow_runs[]?] | first // null')"
ruleset_red=false
[[ "$(jq -r '.conclusion // ""' <<<"${ruleset_json}")" == "failure" ]] && ruleset_red=true

n_red="$(jq '[.[] | select(.verdict == "red")] | length' <<<"${verdicts}")"
n_pending="$(jq '[.[] | select(.verdict == "pending")] | length' <<<"${verdicts}")"
n_unknown="$(jq '[.[] | select(.verdict == "unknown")] | length' <<<"${verdicts}")"
source_seen="$(jq --arg s "${source_workflow}" '[.[] | select(.workflow == $s and .verdict != "absent")] | length' <<<"${verdicts}")"

if [[ "${n_red}" -gt 0 || "${ruleset_red}" == "true" ]]; then
	state=red
elif [[ "${n_pending}" -gt 0 || "${source_seen}" -eq 0 ]]; then
	state=pending
elif [[ "${n_unknown}" -gt 0 ]]; then
	state=unknown
else
	state=green
fi

# --- open main-health issues -------------------------------------------------
issues_json="$(gh_read "repos/${repo}/issues?state=open&labels=${label}&per_page=100" |
	jq -s '[.[][] | select(.pull_request == null)] | sort_by(.number)')"
primary="$(jq -c 'first // null' <<<"${issues_json}")"
extras="$(jq -r '.[1:][]?.number' <<<"${issues_json}")"

status_state="" status_desc="" status_url="${MAIN_HEALTH_RUN_URL:-https://github.com/${repo}/actions}"
action=noop

close_issue() { # number, node_id, comment
	write "close issue #$1" POST "repos/${repo}/issues/$1/comments" -f body="$3" >/dev/null || true
	write "close issue #$1" PATCH "repos/${repo}/issues/$1" -f state=closed -f state_reason=completed >/dev/null
	[[ -n "$2" && "${dry_run}" != "true" ]] &&
		{ gh api graphql -f query='mutation($id:ID!){unpinIssue(input:{issueId:$id}){issue{number}}}' -f id="$2" >/dev/null 2>&1 || true; }
	return 0
}

case "${state}" in
red)
	title="main is red @${short}"
	body="$(
		{
			echo "<!-- main-health:sha=${tip} -->"
			echo "\`${branch}\` is red at [\`${short}\`](https://github.com/${repo}/commit/${tip}). This issue is managed by the main-health watcher (scripts/ci/main-health.sh): it is updated as the verdict changes and closes itself once \`${branch}\` is green. Do not edit the body."
			echo
			echo "Failing required workflows on this commit:"
			echo
			while IFS= read -r row; do
				wf="$(jq -r '.workflow' <<<"${row}")"
				rid="$(jq -r '.run_id' <<<"${row}")"
				url="$(jq -r '.url' <<<"${row}")"
				while IFS=$'\t' read -r job step line; do
					echo "- **${wf}** ([run](${url})) — job \`${job}\`, step \`${step:-?}\` — verdict: \`${line}\`"
				done < <(failing_jobs "${rid}")
			done < <(jq -c '.[] | select(.verdict == "red")' <<<"${verdicts}")
			if [[ "${ruleset_red}" == "true" ]]; then
				rid="$(jq -r '.id' <<<"${ruleset_json}")"
				url="$(jq -r '.html_url' <<<"${ruleset_json}")"
				echo "- **verify-live-ruleset** (scheduled \`Required Gates\`, [run](${url})) — the live ruleset no longer matches specs/ci-gates.v1.yaml:"
				while IFS=$'\t' read -r job step line; do
					echo "  - job \`${job}\` — verdict: \`${line}\`"
				done < <(failing_jobs "${rid}")
			fi
		} | sed -e '${/^$/d;}'
	)"
	if [[ "${primary}" == "null" ]]; then
		action=open-issue
		if [[ "${dry_run}" == "true" ]]; then
			echo "DRY-RUN would: open-issue '${title}'"
			issue_url="${status_url}"
		else
			write "create label" POST "repos/${repo}/labels" -f name="${label}" -f color=B60205 \
				-f description="Managed by the main-health watcher" >/dev/null 2>&1 || true
			created="$(write "open-issue" POST "repos/${repo}/issues" -f title="${title}" -f body="${body}" -f "labels[]=${label}")"
			issue_url="$(jq -r '.html_url' <<<"${created}")"
			node_id="$(jq -r '.node_id' <<<"${created}")"
			gh api graphql -f query='mutation($id:ID!){pinIssue(input:{issueId:$id}){issue{number}}}' -f id="${node_id}" >/dev/null 2>&1 ||
				echo "::warning::could not pin the main-health issue (pinning is best effort)"
		fi
	else
		number="$(jq -r '.number' <<<"${primary}")"
		issue_url="$(jq -r '.html_url' <<<"${primary}")"
		if [[ "$(jq -r '.title' <<<"${primary}")" == "${title}" && "$(jq -r '.body // ""' <<<"${primary}")" == "${body}" ]]; then
			action=noop
		else
			action=update-issue
			write "update-issue #${number} -> '${title}'" PATCH "repos/${repo}/issues/${number}" -f title="${title}" -f body="${body}" >/dev/null
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
	status_desc="A required workflow on ${short} was cancelled and has no verdict; re-run it"
	;;
esac

# --- commit status (skipped when it would repeat the latest one) ------------
status_desc="$(printf '%s' "${status_desc}" | cut -c1-140)"
last_status="$(gh_read "repos/${repo}/commits/${tip}/statuses?per_page=100" 2>/dev/null |
	jq -s --arg c "${status_context}" '[.[][] | select(.context == $c)] | first // {}' || echo '{}')"
if [[ "$(jq -r '.state // ""' <<<"${last_status}")" == "${status_state}" && "$(jq -r '.description // ""' <<<"${last_status}")" == "${status_desc}" ]]; then
	echo "main-health: ${status_context} status already ${status_state} on ${short}; not reposting"
else
	write "post ${status_context}=${status_state} on ${short}" POST "repos/${repo}/statuses/${tip}" \
		-f state="${status_state}" -f context="${status_context}" -f description="${status_desc}" \
		-f target_url="${status_url}" >/dev/null
fi

summary="main-health: sha=${tip} state=${state} action=${action} required=$(printf '%s' "${required}" | wc -l | tr -d ' ')+1 red=${n_red} pending=${n_pending} unknown=${n_unknown} ruleset_red=${ruleset_red} open_issues=$(jq 'length' <<<"${issues_json}")"
echo "${summary}"
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
	{
		echo "### main-health"
		echo
		echo "\`${summary}\`"
		echo
		jq -r '.[] | "- \(.workflow): \(.verdict)"' <<<"${verdicts}"
	} >>"${GITHUB_STEP_SUMMARY}"
fi
