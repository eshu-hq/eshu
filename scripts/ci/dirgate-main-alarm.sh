#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Dirgate full-sweep alarm (#10215). After #10214 scoped the dirgate absolute
# check on pull_request and pre-push to the directories the change touched,
# the full sweep that still runs on push to main is the only alarm for a lost
# Dirgate-Growth trailer -- but a red main push notifies no one. This script
# derives the notification from that push run's dirgate job result and puts it
# where the merger looks: one comment on the merged PR naming the directory
# and the missing trailer, or (no merged PR, e.g. a direct push) one labeled
# fallback issue. A clean push produces nothing, and an unrelated red job or
# a broken (non-RED) dirgate run stays silent.
#
# Trigger: workflow_run completed of "Build Test", branches [main] (see
# .github/workflows/dirgate-main-alarm.yml). workflow_run always executes the
# default-branch version, so it checks out nothing and never runs PR code.
# Trusted-publisher shape mirrors scripts/ci/main-health.sh: every gh call is
# a read except the single notifier, which returns before calling gh in
# dry-run mode. Exit status is 0 whenever the evaluation itself completed, red
# or not, so this watcher never adds a red row of its own to main.
#
# Environment:
#   GITHUB_REPOSITORY       owner/name (required)
#   GITHUB_EVENT_PATH       workflow_run payload (required)
#   DIRGATE_ALARM_DRY_RUN   "true" prints the decision and the writes it would
#                           make, and makes none (default "false")
#   DIRGATE_ALARM_RUN_URL   target_url override for hermetic tests (default "")
set -euo pipefail

label="dirgate-main-alarm"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
dry_run="${DIRGATE_ALARM_DRY_RUN:-false}"
event_file="${GITHUB_EVENT_PATH:?GITHUB_EVENT_PATH is required}"
run_url_override="${DIRGATE_ALARM_RUN_URL:-}"

# shellcheck source=scripts/lib/main-health-policy.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../lib" && pwd)/main-health-policy.sh"

[[ "${dry_run}" == "true" || "${dry_run}" == "false" ]] || {
	echo "DIRGATE_ALARM_DRY_RUN must be true or false" >&2
	exit 2
}

# notify is the ONLY mutating call: one comment or issue write. In dry-run
# mode it reports the write and returns before calling gh.
notify() {
	local desc="$1"
	shift
	if [[ "${dry_run}" == "true" ]]; then
		echo "DRY-RUN would: ${desc}" >&2
		return 0
	fi
	gh api "$@"
}

decide() {
	local state="$1" action="$2"
	if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
		printf '### dirgate-main-alarm\n\n`dirgate-main-alarm: sha=%s state=%s action=%s`\n' "${sha}" "${state}" "${action}" >>"${GITHUB_STEP_SUMMARY}"
	fi
	printf 'dirgate-main-alarm: sha=%s state=%s action=%s\n' "${sha}" "${state}" "${action}"
}

# --- read the trigger -------------------------------------------------------
name="$(jq -r '.workflow_run.name // ""' "${event_file}")"
event="$(jq -r '.workflow_run.event // ""' "${event_file}")"
branch="$(jq -r '.workflow_run.head_branch // ""' "${event_file}")"
head_repo="$(jq -r '.workflow_run.head_repository.full_name // ""' "${event_file}")"
sha="$(jq -r '.workflow_run.head_sha // ""' "${event_file}")"
run_id="$(jq -r '.workflow_run.id // ""' "${event_file}")"
conclusion="$(jq -r '.workflow_run.conclusion // ""' "${event_file}")"
run_url="$(jq -r '.workflow_run.html_url // ""' "${event_file}")"
[[ -n "${run_url_override}" ]] && run_url="${run_url_override}"
short="${sha:0:10}"

# Only a push to main in this repository is in scope: a schedule or dispatch
# full sweep stays covered by the generic main-health watcher, and any other
# workflow, branch, fork, or event never wakes this alarm.
if [[ "${name}" != "Build Test" || "${event}" != "push" || "${branch}" != "main" || "${head_repo}" != "${repo}" ]]; then
	decide "ignored" "none"
	exit 0
fi
[[ "${sha}" =~ ^[0-9a-f]{40}$ ]] || {
	echo "dirgate-main-alarm: payload has no head sha" >&2
	decide "unknown" "none"
	exit 0
}

# A clean push produces nothing: no further API reads, no writes.
case "${conclusion}" in
	success | neutral | skipped)
		decide "green" "none"
		exit 0
		;;
	failure | timed_out | startup_failure) ;;
	*)
		decide "unknown" "none"
		exit 0
		;;
esac

# --- the notification derives from the dirgate job result, not a list ------
jobs_json="$(gh api "repos/${repo}/actions/runs/${run_id}/jobs?filter=latest&per_page=100")"
dirgate_job="$(printf '%s' "${jobs_json}" | jq -c '[.jobs[]? | select(.name == "dirgate")] | last // null')"
job_conclusion="$(printf '%s' "${dirgate_job}" | jq -r '.conclusion // ""')"
job_id="$(printf '%s' "${dirgate_job}" | jq -r '.id // ""')"
case "${job_conclusion}" in
	failure | timed_out) ;;
	*)
		decide "green" "none"
		exit 0
		;;
esac

# --- extract what the merger needs: directory + missing trailer ------------
# The job log carries timestamps and ANSI, so strip the timestamp prefix first
# (as main-health's verdict_of does), then keep each RED finding head line and
# each generated Dirgate-Growth suggestion. A failed dirgate job with no RED
# line is infra breakage, not a lost trailer: stay silent rather than mislead.
log_raw="$(gh api "repos/${repo}/actions/jobs/${job_id}/logs")"
reds="$(printf '%s' "${log_raw}" |
	sed -E 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z //' |
	rg '^dirgate: RED ' || true)"
trailers="$(printf '%s' "${log_raw}" |
	sed -E 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z //' |
	rg 'Dirgate-Growth: [^ ]+ \+[0-9]+' --only-matching || true)"
if [[ -z "${reds}" ]]; then
	decide "unknown" "none"
	exit 0
fi

body_file="$(mktemp)"
trap 'rm -f "${body_file}"' EXIT
{
	printf '<!-- dirgate-main-alarm:sha=%s -->\n' "${sha}"
	printf 'The dirgate full sweep went red on the push to `main` at [`%s`](https://github.com/%s/commit/%s) ([failed run](%s)).\n' "${short}" "${repo}" "${sha}" "${run_url}"
	printf '\nA squash merge with an empty body (or a dropped trailer) lands growth no later PR fails: the `pull_request` check only judges directories the change touched. Repair it in a one-commit PR:\n\n'
	while IFS= read -r line; do
		printf -- '- `%s`\n' "$(printf '%s' "${line}" | clean)"
	done < <(printf '%s\n' "${reds}")
	printf '\nSuggested waiver (replace `#<issue>` with the repair PR number):\n\n'
	while IFS= read -r line; do
		printf -- '- `%s`\n' "$(printf '%s' "${line}" | clean)"
	done < <(printf '%s\n' "${trailers}")
} >"${body_file}"
body="$(cat "${body_file}")"

# --- notify: the merged PR first, a labeled issue as fallback ---------------
prs_json="$(gh api "repos/${repo}/commits/${sha}/pulls?per_page=100")"
pr_number="$(printf '%s' "${prs_json}" | jq -r '[.[] | select(.merged_at != null)] | first | .number // ""')"
if [[ -n "${pr_number}" && "${pr_number}" != "null" ]]; then
	comments_json="$(gh api "repos/${repo}/issues/${pr_number}/comments?per_page=100")"
	if printf '%s' "${comments_json}" | jq -e --arg m "dirgate-main-alarm:sha=${sha}" 'any(.[]; (.body // "" | contains($m)))' >/dev/null; then
		decide "notify" "none"
		exit 0
	fi
	notify "comment on PR #${pr_number}" -X POST "repos/${repo}/issues/${pr_number}/comments" -f body="${body}" >/dev/null
	decide "notify" "comment-pr"
	exit 0
fi

title="dirgate full sweep red @${short}"
issues_json="$(gh api "repos/${repo}/issues?state=open&labels=${label}&per_page=100")"
if printf '%s' "${issues_json}" | jq -e --arg t "${title}" 'any(.[]; .title == $t)' >/dev/null; then
	decide "notify" "none"
	exit 0
fi
notify "open ${label} issue" -X POST "repos/${repo}/issues" -f title="${title}" -f body="${body}" -f "labels[]=${label}" >/dev/null
decide "notify" "open-issue"
