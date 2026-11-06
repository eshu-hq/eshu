#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Hermetic cases for scripts/ci/dirgate-main-alarm.sh (#10215): the post-merge
# dirgate alarm. No GitHub, no network: a fake `gh` on PATH serves recorded
# JSON and log fixtures for each API path and records every mutating call, so
# the cases assert the notify/skip DECISIONS by looking at which writes were
# (and were not) attempted.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/dirgate-main-alarm.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

for tool in jq rg; do
	command -v "${tool}" >/dev/null || {
		echo "test-dirgate-main-alarm: ${tool} is required" >&2
		exit 1
	}
done

pass=0
fail=0
check() {
	local desc="$1" status="$2"
	if [ "${status}" -eq 0 ]; then
		printf 'PASS: %s\n' "${desc}"
		pass=$((pass + 1))
	else
		printf 'FAIL: %s\n' "${desc}"
		fail=$((fail + 1))
	fi
}

if [ ! -x "${target}" ]; then
	echo "test-dirgate-main-alarm: missing executable script at ${target}" >&2
	exit 1
fi

# --- fake gh ---------------------------------------------------------------
# Fixtures live in files under $case_dir; the fake serves them by URL
# fragment and appends one line per mutating call to calls.log:
#   POST <path> || <body-file>
fakebin="${work}/fakebin"
mkdir -p "${fakebin}"
# The fake lives in scripts/gh-fake-dirgate-alarm.sh so no heredoc here
# exceeds the 512-byte budget; assemble it verbatim.
cp "${repo_root}/scripts/gh-fake-dirgate-alarm.sh" "${fakebin}/gh"
chmod +x "${fakebin}/gh"

export GITHUB_REPOSITORY="eshu-hq/eshu"
RUN_ID="424242"
TIP="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
SHORT="aaaaaaaaaa"
RUN_URL="https://github.com/eshu-hq/eshu/actions/runs/424242"

new_case() {
	case_dir="${work}/$1"
	mkdir -p "${case_dir}"
	: >"${case_dir}/calls.log"
	printf '[]\n' >"${case_dir}/comments.json"
	printf '[]\n' >"${case_dir}/prs.json"
	printf '[]\n' >"${case_dir}/issues.json"
	export DIRGATE_ALARM_CASE="${case_dir}"
	export DIRGATE_ALARM_DRY_RUN="false"
	export DIRGATE_ALARM_RUN_URL="${RUN_URL}"
	PATH="${fakebin}:${PATH}"
}

write_event() {
	printf '%s' "$1" >"${case_dir}/event.json"
	export GITHUB_EVENT_PATH="${case_dir}/event.json"
}

push_event() {
	local conclusion="$1"
	write_event "$(printf '{"workflow_run":{"name":"Build Test","event":"push","head_branch":"main","head_sha":"%s","head_repository":{"full_name":"eshu-hq/eshu"},"id":%s,"html_url":"%s","conclusion":"%s"}}' "${TIP}" "${RUN_ID}" "${RUN_URL}" "${conclusion}")"
}

dirgate_jobs() {
	printf '{"jobs":[{"id":77001,"name":"dirgate","conclusion":"%s"},{"id":77002,"name":"go-core","conclusion":"success"}]}' "$1" >"${case_dir}/jobs.json"
}

RED_LOG=$(printf '%s\n' \
	'2026-11-06T10:00:00Z dirgate: mode=ci base=bbbbbbbbbb head-tree=hhhhhhhhhh trailer-range=bbbbbbbbbb..aaaaaaaaaa' \
	'2026-11-06T10:00:01Z dirgate: RED go/internal/telemetry: 67 -> 68 non-test .go files (base -> head), needs +1.' \
	'2026-11-06T10:00:01Z   Move files out, or add this line to the message of the commit that adds them' \
	'2026-11-06T10:00:01Z   (growth already on the base: check main first, a repair PR may already be in flight; otherwise put it in a one-commit repair PR):' \
	'2026-11-06T10:00:01Z     Dirgate-Growth: internal/telemetry +1 #6054 split lands next' \
	'2026-11-06T10:00:02Z dirgate: evaluated 311 directories: 1 RED, 0 NOTE' \
	'2026-11-06T10:00:02Z ##[error]Process completed with exit code 1.')

run_alarm() {
	out_file="${case_dir}/out.txt"
	"${target}" >"${out_file}" 2>&1
	last_rc=$?
}

called() { rg -q "$1" "${case_dir}/calls.log"; }
not_called() { ! rg -q "$1" "${case_dir}/calls.log"; }
out_has() { rg -q "$1" "${case_dir}/out.txt"; }
comment_body_has() {
	local f
	f="$(rg --only-matching 'POST repos/eshu-hq/eshu/issues/[0-9]+/comments \|\| (\S+)' --replace '$1' "${case_dir}/calls.log" | head -1)"
	[ -n "${f:-}" ] && rg -q "$1" "${f}"
}
issue_body_has() {
	local f
	f="$(rg --only-matching 'POST repos/eshu-hq/eshu/issues \|\| (\S+)' --replace '$1' "${case_dir}/calls.log" | head -1)"
	[ -n "${f:-}" ] && rg -q "$1" "${f}"
}

# 1. RED full sweep on a push to main: one comment on the merged PR naming the
#    directory and the missing trailer; exit 0.
new_case red-comments
push_event "failure"
dirgate_jobs "failure"
printf '%s\n' "${RED_LOG}" >"${case_dir}/job.log"
printf '[{"number":10240,"merged_at":"2026-11-06T09:00:00Z"}]' >"${case_dir}/prs.json"
run_alarm
check "red: exits 0" "${last_rc}"
check "red: comments once on the merged PR" "$([ "$(rg -c 'POST repos/eshu-hq/eshu/issues/10240/comments' "${case_dir}/calls.log")" -eq 1 ] && echo 0 || echo 1)"
check "red: comment carries the sha marker" "$(comment_body_has "dirgate-main-alarm:sha=${TIP}" && echo 0 || echo 1)"
check "red: comment names the directory" "$(comment_body_has 'go/internal/telemetry' && echo 0 || echo 1)"
check "red: comment carries the missing trailer" "$(comment_body_has 'Dirgate-Growth: internal/telemetry \+1' && echo 0 || echo 1)"
check "red: comment links the failed run" "$(comment_body_has "${RUN_URL}" && echo 0 || echo 1)"
check "red: opens no issue" "$(not_called 'POST repos/eshu-hq/eshu/issues ' && echo 0 || echo 1)"
check "red: decision line printed" "$(out_has 'state=notify.*action=comment-pr' && echo 0 || echo 1)"

# 2. Clean push to main: conclusion success makes zero API reads past the
#    payload and zero writes.
new_case green-silent
push_event "success"
run_alarm
check "green: exits 0" "${last_rc}"
check "green: decision line printed" "$(out_has 'state=green.*action=none' && echo 0 || echo 1)"
check "green: no write call of any kind" "$(not_called 'POST ' && echo 0 || echo 1)"

# 3. Build Test failed but the dirgate job passed: someone else's red, not ours.
new_case other-job-red
push_event "failure"
dirgate_jobs "success"
run_alarm
check "other-job: exits 0" "${last_rc}"
check "other-job: no write call of any kind" "$(not_called 'POST ' && echo 0 || echo 1)"
check "other-job: decision line printed" "$(out_has 'state=green.*action=none' && echo 0 || echo 1)"

# 4. Not a push to main (schedule): out of scope for this alarm.
new_case schedule-ignored
write_event '{"workflow_run":{"name":"Build Test","event":"schedule","head_branch":"main","head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","head_repository":{"full_name":"eshu-hq/eshu"},"id":424242,"html_url":"https://github.com/eshu-hq/eshu/actions/runs/424242","conclusion":"failure"}}'
run_alarm
check "schedule: exits 0" "${last_rc}"
check "schedule: no write call of any kind" "$(not_called 'POST ' && echo 0 || echo 1)"

# 5. Idempotent: the merged PR already carries this sha's marker comment.
new_case red-idempotent
push_event "failure"
dirgate_jobs "failure"
printf '%s\n' "${RED_LOG}" >"${case_dir}/job.log"
printf '[{"number":10240,"merged_at":"2026-11-06T09:00:00Z"}]' >"${case_dir}/prs.json"
printf '[{"id":9,"body":"<!-- dirgate-main-alarm:sha=%s -->\\nold"}]' "${TIP}" >"${case_dir}/comments.json"
run_alarm
check "idempotent: exits 0" "${last_rc}"
check "idempotent: posts no second comment" "$(not_called 'POST ' && echo 0 || echo 1)"

# 6. No merged PR for the tip (direct push): fall back to a labeled issue.
new_case no-pr-falls-back-to-issue
push_event "failure"
dirgate_jobs "failure"
printf '%s\n' "${RED_LOG}" >"${case_dir}/job.log"
run_alarm
check "fallback: exits 0" "${last_rc}"
check "fallback: opens exactly one issue" "$([ "$(rg -c 'POST repos/eshu-hq/eshu/issues ' "${case_dir}/calls.log")" -eq 1 ] && echo 0 || echo 1)"
check "fallback: issue title names the short sha" "$(issue_body_has "dirgate full sweep red @${SHORT}" && echo 0 || echo 1)"
check "fallback: issue names the directory and trailer" "$(issue_body_has 'Dirgate-Growth: internal/telemetry \+1' && echo 0 || echo 1)"
check "fallback: decision line printed" "$(out_has 'state=notify.*action=open-issue' && echo 0 || echo 1)"

# 7. Failed dirgate job but no RED findings in the log (infra breakage): noisy
#    comments would mislead, so stay silent.
new_case broken-silent
push_event "failure"
dirgate_jobs "failure"
printf '%s\n' '2026-11-06T10:00:00Z dirgate: cannot resolve the comparison: boom' >"${case_dir}/job.log"
run_alarm
check "broken: exits 0" "${last_rc}"
check "broken: no write call of any kind" "$(not_called 'POST ' && echo 0 || echo 1)"
check "broken: decision line printed" "$(out_has 'state=unknown.*action=none' && echo 0 || echo 1)"

# 8. Workflow wiring: the watcher judges completed Build Test runs on main
#    with least-privilege permissions and never cancels an in-progress alarm.
wf="${repo_root}/.github/workflows/dirgate-main-alarm.yml"
check "wiring: workflow file exists" "$([ -f "${wf}" ] && echo 0 || echo 1)"
check "wiring: trigger is completed Build Test runs" "$([ "$(yq -r '.["on"].workflow_run.workflows | join(",")' "${wf}")" = "Build Test" ] && [ "$(yq -r '.["on"].workflow_run.types | join(",")' "${wf}")" = "completed" ] && echo 0 || echo 1)"
check "wiring: trigger branch is main" "$([ "$(yq -r '.["on"].workflow_run.branches | join(",")' "${wf}")" = "main" ] && echo 0 || echo 1)"
check "wiring: permissions are actions-read, contents-read, issues-write, pull-requests-read" "$([ "$(yq -r '.permissions.actions' "${wf}")" = "read" ] && [ "$(yq -r '.permissions.contents' "${wf}")" = "read" ] && [ "$(yq -r '.permissions.issues' "${wf}")" = "write" ] && [ "$(yq -r '.permissions["pull-requests"]' "${wf}")" = "read" ] && echo 0 || echo 1)"
check "wiring: registry triggers cover the fake helper" "$(rg -q 'scripts/gh-fake-dirgate-alarm.sh' "${repo_root}/specs/ci-gates.d/gates-02.yaml" && echo 0 || echo 1)"
check "wiring: one non-cancelling concurrency group" "$([ "$(yq -r '.jobs.alarm.concurrency.group' "${wf}")" = "dirgate-main-alarm" ] && [ "$(yq -r '.jobs.alarm.concurrency."cancel-in-progress"' "${wf}")" = "false" ] && echo 0 || echo 1)"
check "wiring: the job runs the alarm script" "$(rg -q 'run: scripts/ci/dirgate-main-alarm.sh' "${wf}" && echo 0 || echo 1)"

printf 'test-dirgate-main-alarm: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" -eq 0 ]
