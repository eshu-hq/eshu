#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Hermetic cases for scripts/ci/main-health.sh (#7111 F2), the post-merge
# main-health watcher. No GitHub, no network: a fake `gh` on PATH serves
# recorded JSON for each API path and logs every call it receives as one JSON
# array per line, so the cases assert the upsert/close DECISIONS by looking at
# which writes were (and were not) attempted.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/main-health.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

for tool in jq yq rg; do
	command -v "${tool}" >/dev/null || {
		echo "test-main-health: ${tool} is required" >&2
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
	echo "test-main-health: missing executable script at ${target}" >&2
	exit 1
fi

# shellcheck source=scripts/lib/test-main-health-fake.sh
. "${repo_root}/scripts/lib/test-main-health-fake.sh"

# 1. red, no open issue: exactly one issue is opened with the failing job and
#    verdict line, a failure status is posted on the tip.
new_case red-opens "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' in_progress '')"
failing_job 1 501 'go-core (shard 2)' $'2026-09-25T10:00:00.1234567Z --- FAIL: TestGuardIndexKeyWrites (0.01s)\n2026-09-25T10:00:01.0000000Z ##[error]Process completed with exit code 1.'
run_watcher
check "red: watcher exits 0" "${last_rc}"
check "red: opens exactly one issue" "$([ "$(count_calls "${ISSUES_POST}")" -eq 1 ] && echo 0 || echo 1)"
check "red: issue title names the short sha" "$(called 'main is red @aaaaaaaaaa' && echo 0 || echo 1)"
check "red: issue body names the failing workflow, job and verdict line" "$(called 'Build Test.*go-core \(shard 2\).*--- FAIL: TestGuardIndexKeyWrites' && echo 0 || echo 1)"
check "red: pinned via graphql (best effort)" "$(called '^api graphql' && echo 0 || echo 1)"
check "red: failure status posted on the tip under main-health" "$(called "${STATUS_POST}${TIP}.*state=failure.*context=main-health" && echo 0 || echo 1)"
check "red: decision line printed" "$(out_has 'state=red.*action=open-issue' && echo 0 || echo 1)"

# 2. dry run: same red input, decision printed, zero writes.
dry_run_watcher
check "dry-run: exits 0" "${last_rc}"
check "dry-run: no write call of any kind" "$(not_called "${ANY_WRITE}" && echo 0 || echo 1)"
check "dry-run: prints the would-be action" "$(out_has 'DRY-RUN.*open-issue' && echo 0 || echo 1)"

# 3. idempotent: an open issue for the same sha with the same body is left alone.
new_case red-idempotent "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
run_watcher
new_body_issue="$(jq -cn --rawfile b "${case_dir}/last-body.txt" \
	'{number:41,node_id:"N41",title:"main is red @aaaaaaaaaa",state:"open",html_url:"https://github.example/eshu-hq/eshu/issues/41",body:$b,labels:[{name:"main-health"}]}')"
set_issues "${new_body_issue}"
: >"${case_dir}/calls.log"
run_watcher
check "idempotent: second run opens no second issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"
check "idempotent: second run patches nothing when the body is unchanged" "$(not_called "${ISSUE_PATCH}" && echo 0 || echo 1)"
check "idempotent: decision is noop" "$(out_has 'action=noop' && echo 0 || echo 1)"

# 4. red on a newer sha while an issue for the older sha is open: retitle the
#    one issue, never open a second.
new_case red-newer-sha "${NEXT}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "newer red sha: no second issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"
check "newer red sha: the one issue is retitled to the new sha" "$(called "$(patch_of 41)title=main is red @bbbbbbbbbb" && echo 0 || echo 1)"

# 5. newer green sha supersedes an older red: issue is closed, status success.
new_case green-supersedes "${NEXT}"
green_runs
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "green supersedes red: issue closed" "$(called "$(patch_of 41)state=closed" && echo 0 || echo 1)"
check "green supersedes red: success status on the newest sha" "$(called "${STATUS_POST}${NEXT}.*state=success.*context=main-health" && echo 0 || echo 1)"
check "green supersedes red: no new issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"

# 6. green with no open issue: only the status, no issue writes.
new_case green-quiet "${TIP}"
green_runs
run_watcher
check "green, nothing open: no issue writes" "$(not_called "${ISSUES_POST}|${ISSUE_PATCH}" && echo 0 || echo 1)"
check "green, nothing open: success status posted" "$(called "state=success" && echo 0 || echo 1)"

# 7. a status identical to the latest main-health status is not reposted.
echo '[{"context":"main-health","state":"success","description":"All required workflows green on aaaaaaaaaa"}]' >"${case_dir}/statuses.json"
: >"${case_dir}/calls.log"
run_watcher
check "unchanged status is not reposted" "$(not_called "${STATUS_POST}" && echo 0 || echo 1)"

# 8. pending: a still-running workflow with no red leaves the older issue open.
new_case pending-holds "${NEXT}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' in_progress '')" \
	"$(run 3 'Frontend' completed success)"
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "pending: issue is not closed" "$(not_called "${ISSUE_PATCH}|${ISSUES_POST}" && echo 0 || echo 1)"
check "pending: pending status posted" "$(called "state=pending" && echo 0 || echo 1)"

# 9. the source workflow has no run yet: cannot call it green.
new_case sentinel-missing "${NEXT}"
set_runs "$(run 2 'Static Contract Gates' completed success)"
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "no Build Test verdict yet: not green, issue kept" "$(not_called "${ISSUE_PATCH}" && echo 0 || echo 1)"
check "no Build Test verdict yet: pending" "$(called "state=pending" && echo 0 || echo 1)"

# 10. flapping: an older red superseded by a newer green run of the SAME
#     workflow is green; the reverse is red.
new_case flap-red-then-green "${TIP}"
set_runs "$(run 1 'Build Test' completed failure 10 1)" \
	"$(run 11 'Build Test' completed success 10 2)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
run_watcher
check "flap red->green (rerun attempt 2): green" "$(out_has 'state=green' && echo 0 || echo 1)"
new_case flap-green-then-red "${TIP}"
set_runs "$(run 1 'Build Test' completed success 10 1)" \
	"$(run 11 'Build Test' completed failure 10 2)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 11 502 'go-core' $'##[error]Process completed with exit code 1.'
run_watcher
check "flap green->red (rerun attempt 2): red" "$(out_has 'state=red' && echo 0 || echo 1)"

# 11. cancelled is not red and not green: no issue change, error status.
new_case cancelled-unproven "${NEXT}"
set_runs "$(run 1 'Build Test' completed cancelled)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "cancelled: issue untouched" "$(not_called "${ISSUE_PATCH}|${ISSUES_POST}" && echo 0 || echo 1)"
check "cancelled: error (no verdict) status" "$(called "state=error" && echo 0 || echo 1)"

# 12. a required workflow with no run on this sha (path-filtered) is not red.
new_case path-filtered "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)"
run_watcher
check "no run for a path-filtered workflow: still green" "$(out_has 'state=green' && echo 0 || echo 1)"

# 13. runs of other events (PR runs on the same sha) never count.
new_case pr-runs-ignored "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 4 'Frontend' completed failure 9 | jq -c '.event="pull_request"')"
run_watcher
check "pull_request run on the same sha is ignored" "$(out_has 'state=green' && echo 0 || echo 1)"

# 14. verify-live-ruleset failure routes to the same issue while CI is green.
new_case ruleset-red "${TIP}"
green_runs
ruleset_run failure
failing_job 70 701 'verify-live-ruleset' $'ruleset 19745843 (main protection) does not own one merge_queue rule\n##[error]Process completed with exit code 1.'
run_watcher
check "ruleset verifier red with green CI: red" "$(out_has 'state=red' && echo 0 || echo 1)"
check "ruleset verifier red: routed into the main-health issue with its verdict line" "$(called 'verify-live-ruleset.*does not own one merge_queue rule' && echo 0 || echo 1)"
check "ruleset verifier red: still exactly one issue" "$([ "$(count_calls "${ISSUES_POST}")" -eq 1 ] && echo 0 || echo 1)"

# P2-1: the ruleset probe fails closed. Any conclusion but success is red
# (the verifier never ran to a verdict, so drift cannot be ruled out), and
# no completed scheduled run yet is unknown: never green, issue untouched.
for concl in startup_failure timed_out cancelled; do
	new_case "ruleset-${concl}" "${TIP}"
	green_runs
	ruleset_run "${concl}"
	run_watcher
	check "P2-1: ruleset verification ${concl} with green CI: red" "$(ok out_has 'state=red.*ruleset_red=true')"
	check "P2-1: ruleset ${concl}: the issue names the conclusion" "$(ok rg -qF "verify-live-ruleset** (scheduled \`Required Gates\`, conclusion \`${concl}\`" "${case_dir}/last-body.txt")"
done
new_case ruleset-none "${NEXT}"
green_runs
ruleset_run none
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "P2-1: no completed ruleset verification: unknown, not green" "$(ok out_has 'state=unknown')"
check "P2-1: no completed ruleset verification: the issue stays open" "$(ok not_called "$(patch_of 41)state=closed")"

# 15. two open main-health issues: keep the oldest, close the duplicate.
new_case duplicates "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
set_issues "$(open_issue 50 'main is red @dup' "${TIP}")" "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
run_watcher
check "duplicates: no new issue" "$(not_called "${ISSUES_POST}" && echo 0 || echo 1)"
check "duplicates: the newer duplicate is closed" "$(called "$(patch_of 50)state=closed" && echo 0 || echo 1)"
check "duplicates: the oldest issue (41) is kept open" "$(not_called "$(patch_of 41)state=closed" && echo 0 || echo 1)"

# 16. pin failure is not fatal.
new_case pin-fails "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'##[error]Process completed with exit code 1.'
echo graphql >"${case_dir}/write-fails"
run_watcher
check "pin failure: watcher still exits 0 and opened the issue" "$([ "${last_rc}" -eq 0 ] && called "${ISSUES_POST}" && echo 0 || echo 1)"

# 17. log text cannot ping people or break markdown: @ and backticks neutralised.
new_case sanitised "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'--- FAIL: TestX `boom` cc @some-user please look'
run_watcher
check "verdict is sanitised (no raw @user mention)" "$(called '@some-user' && echo 1 || echo 0)"

# 18. derivation: the required list is read from the policy files, and against
#     the REAL repo it is non-empty and includes the registry's source workflow.
listed="$(MAIN_HEALTH_REPO_ROOT="${repo_root}" "${target}" --print-required)"
check "real repo: derived required list is non-empty" "$([ -n "${listed}" ] && echo 0 || echo 1)"
check "real repo: derived list includes the aggregator's source workflow" "$(printf '%s\n' "${listed}" | rg -qx 'Build Test' && echo 0 || echo 1)"
expected_n="$(yq '.on.workflow_run.workflows | length' "${repo_root}/.github/workflows/required-gates.yml")"
check "real repo: derived list covers every workflow the aggregator listens to" "$([ "$(printf '%s\n' "${listed}" | wc -l | tr -d ' ')" -ge "${expected_n}" ] && echo 0 || echo 1)"

# 20. F1: the ruleset probe asks for one run in one request (the paginated
#     per_page=1 probe walked the whole history: 208 calls, 57 s live), and a
#     whole evaluation makes a bounded number of gh calls.
new_case f1-bounded "${TIP}"
green_runs
run_watcher
check "F1: ruleset probe asks for a single run" "$(ok called '^api repos/eshu-hq/eshu/actions/workflows/required-gates.yml/runs\?.*per_page=1')"
check "F1: ruleset probe is never paginated" "$(ok not_called '--paginate.*required-gates.yml/runs')"
check "F1: a green evaluation makes at most 8 gh calls" "$([ "$(wc -l <"${case_dir}/calls.log" | tr -d ' ')" -le 8 ] && echo 0 || echo 1)"
check "P3-2: the status probe is one read of the combined status" "$(ok called "^api repos/eshu-hq/eshu/commits/${TIP}/status$")"
check "P3-2: the per-status listing is never walked" "$(ok not_called 'commits/[0-9a-f]+/statuses')"

# 21. F2: Security Scan's image scan runs on the workflow_run event. Recorded
#     shape of main 022c9bd255 (live): push run success, the workflow_run
#     image scan failed, three NEWER workflow_run runs skipped (Publish runs of
#     other events), and a merge_group run on the queue branch.
new_case f2-image-scan-red "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 36201469063 'Security Scan' completed success 9933 1 push)" \
	"$(run 36203153460 'Security Scan' completed skipped 9940 1 workflow_run)" \
	"$(run 36203450563 'Security Scan' completed failure 9941 1 workflow_run)" \
	"$(run 36204191106 'Security Scan' completed skipped 9948 1 workflow_run)" \
	"$(run 36204217502 'Security Scan' completed skipped 9949 1 workflow_run)" \
	"$(run 36204225922 'Security Scan' completed skipped 9950 1 workflow_run)" \
	"$(run 36200006134 'Security Scan' completed success 9927 1 merge_group | jq -c '.head_branch="gh-readonly-queue/main/pr-7180-850b2f0666"')"
failing_job 36203450563 108294812538 'Trivy image scan (ghcr.io/eshu-hq/eshu)' \
	$'2026-09-26T00:05:08.3729932Z \e[33mWARN\e[0m\tUsing severities from other vendors\n2026-09-26T00:05:08.3764772Z To suppress version checks, run Trivy scans with the --skip-version-check flag\n2026-09-26T00:05:08.3829032Z ##[error]Process completed with exit code 1.' \
	'Run Trivy image scan'
run_watcher
# N2: trivy-image is `blocking: false` in the registry (post-publish
# evidence), so this chronic red alone never makes main red: it is reported.
check "N2: a chronic advisory red alone is green" "$(ok out_has 'state=green')"
check "N2: newer skipped workflow_run runs do not shadow the advisory failure" "$(ok out_has ' advisory=1 ')"
check "N2: the green status carries an advisory note naming Security Scan" \
	"$(ok called "${STATUS_POST}.*state=success.*description=.*advisory failure.*Security Scan")"
check "N2: an advisory red alone opens no issue" "$(ok not_called "${ISSUES_POST}")"

# N2: a blocking red plus the advisory image scan: red, the image scan is in
# an "Advisory failures" section (verdict read from an ANSI log), and neither
# the status nor the recorded red set names it.
new_case f2-image-scan-in-red "${TIP}"
set_runs "$(jq -c '.workflow_runs[] | select(.name != "Build Test")' "${work}/f2-image-scan-red/runs.json")" \
	"$(run 1 'Build Test' completed failure)"
failing_job 1 501 'go-core (shard 2)' $'--- FAIL: TestGuard (0.01s)'
cp "${work}/f2-image-scan-red/"{jobs-36203450563.json,log-108294812538.txt} "${case_dir}/"
run_watcher
check "N2: blocking red plus advisory red: red" "$(ok out_has 'state=red.*red=1 .*advisory=1 ')"
check "N2: the advisory failure is listed under Advisory failures" \
	"$(ok rg -q -U 'Advisory failures[^\n]*\n(.*\n)*.*Security Scan.*Trivy image scan' "${case_dir}/last-body.txt")"
check "N2: the verdict line is read from an ANSI-coloured log" "$(ok rg -qF 'To suppress version checks' "${case_dir}/last-body.txt")"
check "N2: the failure status names only the blocking red" \
	"$(ok called "${STATUS_POST}.*state=failure.*description=main is red at aaaaaaaaaa: Build Test -")"
check "N2: the recorded red set holds only the blocking red" "$(ok rg -qF '<!-- main-health:red=["Build Test"] -->' "${case_dir}/last-body.txt")"

new_case f2-image-scan-fixed "${TIP}"
green_runs
set_runs "$(cat "${case_dir}/../f2-image-scan-red/runs.json" | jq -c '.workflow_runs[]')" \
	"$(run 36205000000 'Security Scan' completed success 9955 1 workflow_run)" \
	"$(run 36205000001 'Security Scan' completed skipped 9956 1 workflow_run)"
run_watcher
check "F2: a newer successful image scan supersedes the failed one" "$(ok out_has 'state=green')"

new_case f2-schedule-red "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 4 'Security Scan' completed success 20 1 push)" \
	"$(run 5 'Security Scan' completed failure 21 1 schedule)"
failing_job 5 505 'gosec (Go static analysis)' $'##[error]Process completed with exit code 1.'
run_watcher
check "F2: a failed scheduled run of a required workflow on the tip is red" "$(ok out_has 'state=red')"

# N4: red wins ACROSS events. The failing workflow_run run is older than a
# successful schedule run of the same workflow on the same tip; latest-wins
# across events would call this green.
new_case n4-red-wins-across-events "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 40 'Security Scan' completed success 40 1 push)" \
	"$(run 50 'Security Scan' completed failure 50 1 workflow_run)" \
	"$(run 60 'Security Scan' completed success 60 1 schedule)"
failing_job 50 550 'gosec (Go static analysis)' $'--- FAIL: TestSec (0.01s)'
run_watcher
check "N4: an older red workflow_run run beats a newer green schedule run: red" "$(ok out_has 'state=red')"

# P3 (N3): a workflow_run run reports main's HEAD as its head_sha whatever
# fired it, so Security Scan stamps the TRIGGERING run's branch and sha into
# its run name. Only a scan of main's image at the tip counts: a newer
# successful scan of a release tag's image (same sha) or of an older commit's
# late Publish must not supersede the failed scan of the tip's `:main` image.
new_case p3-trigger-stamp "${TIP}"
stamp() { jq -c --arg t "Security Scan: triggered by $1 @ $2" '.display_title = $t'; }
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 40 'Security Scan' completed success 40 1 push)" \
	"$(run 50 'Security Scan' completed failure 50 1 workflow_run | stamp main "${TIP}")" \
	"$(run 51 'Security Scan' completed success 51 1 workflow_run | stamp v1.2.3 "${TIP}")" \
	"$(run 52 'Security Scan' completed success 52 1 workflow_run | stamp main "${NEXT}")"
cp "${work}/f2-image-scan-red/log-108294812538.txt" "${case_dir}/"
jq -c '.jobs[0].id = 108294812538' "${work}/f2-image-scan-red/jobs-36203450563.json" >"${case_dir}/jobs-50.json"
run_watcher
check "P3: a release-tag or older-commit image scan never supersedes the tip's main scan" "$(ok out_has ' advisory=1 ')"

# N2: blocking-ness fails closed. A failed job no registry gate claims, or a
# failed run whose jobs cannot be read, counts as blocking.
new_case n2-unregistered-job "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed failure)"
run_watcher
check "N2: a failed run with unreadable jobs is blocking: red" "$(ok out_has 'state=red')"
failing_job 3 503 'lint (console)' $'##[error]Process completed with exit code 1.'
run_watcher
check "N2: a failed job no registry gate claims is blocking: red" "$(ok out_has 'state=red')"

# N2: a change in the BLOCKING red set on an open issue posts a comment (an
# issue edit notifies nobody); an unchanged set, or an advisory-only change,
# does not. Names in the comment come from policy, never from the old body.
two_red() {
	set_runs "$(run 1 'Build Test' completed failure)" \
		"$(run 2 'Static Contract Gates' completed "$1")" \
		"$(run 3 'Frontend' completed success)"
	failing_job 1 501 'go-core' $'--- FAIL: TestA (0.01s)'
	failing_job 2 502 'static' $'--- FAIL: TestB (0.01s)'
}
new_case n2-new-blocking-red "${NEXT}"
two_red failure
set_issues "$(red_issue 41 'main is red @aaaaaaaaaa' "${TIP}" '["Build Test"]')"
run_watcher
check "N2: a new blocking red on an open issue posts one comment" "$([ "$(count_calls "${COMMENT_POST}")" -eq 1 ] && echo 0 || echo 1)"
check "N2: the comment names the newly red workflow" "$(ok called 'issues/41/comments .*[Nn]ewly red.*Static Contract Gates')"
check "N2: the body is still updated" "$(ok called "$(patch_of 41)title=main is red @bbbbbbbbbb")"
dry_run_watcher
check "N2: dry run posts no comment" "$(ok not_called "${ANY_WRITE}")"
check "N2: dry run reports the comment it would post" "$(ok rg -q 'DRY-RUN would: comment .*#41' "${case_dir}/err.txt")"
new_case n2-unchanged-set "${NEXT}"
two_red success
set_issues "$(red_issue 41 'main is red @aaaaaaaaaa' "${TIP}" '["Build Test"]')"
run_watcher
check "N2: an unchanged red set on a newer sha patches the issue" "$(ok called "$(patch_of 41)title=main is red @bbbbbbbbbb")"
check "N2: an unchanged red set posts no comment" "$(ok not_called "${COMMENT_POST}")"
new_case n2-cleared "${NEXT}"
two_red success
set_issues "$(red_issue 41 'main is red @aaaaaaaaaa' "${TIP}" '["Build Test","Frontend"]')"
run_watcher
check "N2: a cleared red while another stays red posts a comment naming it" "$(ok called 'issues/41/comments .*[Cc]leared.*Frontend')"
new_case n2-forged-marker "${NEXT}"
two_red success
set_issues "$(red_issue 41 'main is red @aaaaaaaaaa' "${TIP}" '["Build Test","@evil <b>x</b>"]')"
run_watcher
check "N2: a name outside policy in the old marker is ignored: no comment" "$(ok not_called "${COMMENT_POST}")"
check "N2: a forged marker name never reaches a write" "$(ok not_called '@evil')"

new_case f2-queue-ignored "${TIP}"
set_runs "$(run 1 'Build Test' completed success)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)" \
	"$(run 6 'Build Test' completed failure 30 1 merge_group | jq -c '.head_branch="gh-readonly-queue/main/pr-1-aaaa"')" \
	"$(run 7 'Build Test' completed failure 31 1 workflow_dispatch | jq -c '.head_branch="feature"')"
run_watcher
check "F2: merge_group and off-main runs on the same sha never count" "$(ok out_has 'state=green')"

# 22. F3: dry run makes zero writes on EVERY path, not only open-issue.
dry_case() { # <case-name> <description>
	case_dir="${work}/$1"
	export FIXTURES="${case_dir}"
	dry_run_watcher
	check "F3: dry-run $2: no write call of any kind" "$(ok not_called "${ANY_WRITE}")"
	check "F3: dry-run $2: still prints the decision" "$(ok out_has '^main-health: sha=')"
}
dry_case green-supersedes "close path"
dry_case red-newer-sha "update path"
dry_case duplicates "duplicate-close path"
dry_case ruleset-red "ruleset-red open path"
dry_case f2-image-scan-in-red "advisory-section open path"
echo '[{"context":"main-health","state":"failure","description":"stale"}]' >"${work}/green-quiet/statuses.json"
dry_case green-quiet "changed-status path"

# 23. F5: run listings are filtered server-side (head_sha + branch + event),
#     never the unfiltered per-sha listing (609 runs / 7 pages live).
case_dir="${work}/f1-bounded"
export FIXTURES="${case_dir}"
listings="$(calls | rg 'actions/(runs|workflows/[^/ ]+/runs)\?' | rg -v 'required-gates.yml/runs' || true)"
check "F5: the tip's runs are listed" "$([ -n "${listings}" ] && echo 0 || echo 1)"
check "F5: every run listing filters head_sha, branch and event server-side" \
	"$(printf '%s\n' "${listings}" | awk -v t="head_sha=${TIP}" 'NF && !(index($0, t) && index($0, "branch=main") && index($0, "event=")) { bad = 1 } END { exit bad }' && echo 0 || echo 1)"
check "F5: workflow_run runs are listed per workflow file, never repo-wide" \
	"$(ok not_called '^api.* repos/eshu-hq/eshu/actions/runs\?.*event=workflow_run')"
new_case f5-truncated "${NEXT}"
green_runs
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}")"
echo 1500 >"${case_dir}/total-count"
run_watcher
check "F5: a truncated run listing is never green" "$(ok out_has 'state=unknown')"
check "F5: a truncated run listing leaves the issue open" "$(ok not_called "$(patch_of 41)state=closed")"

# 24. P3: an empty failed-step field does not shift the verdict into the step.
new_case p3-empty-step "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'--- FAIL: TestY (0.01s)' ''
run_watcher
check "P3: no failed step -> step '?' and the verdict stays the verdict" "$(ok called 'step `\?` — verdict: `--- FAIL: TestY')"

# 25. P3: only watcher-authored issues (body marker) are managed.
new_case p3-foreign-issue "${TIP}"
green_runs
set_issues "$(open_issue 41 'main is red @aaaaaaaaaa' "${TIP}" | jq -c '.body="a human report labelled main-health"')"
run_watcher
check "P3: a labelled issue without the watcher marker is never closed" "$(ok not_called "${ISSUE_PATCH}")"

# 26. P3: ANSI, carriage return and backticks cannot reach the issue body,
#     and the summary counts the derived required list exactly.
new_case p3-sanitise "${TIP}"
set_runs "$(run 1 'Build Test' completed failure)" \
	"$(run 2 'Static Contract Gates' completed success)" \
	"$(run 3 'Frontend' completed success)"
failing_job 1 501 'go-core' $'--- FAIL: TestZ \e[31m`red`\e[0m\r [x](http://e) #12'
run_watcher
check "P3: body has no escape or carriage-return byte" "$(! rg -q $'[\x1b\r]' "${case_dir}/last-body.txt" && echo 0 || echo 1)"
check "P3: backticks in log text are neutralised" "$(ok rg -qF "TestZ 'red'" "${case_dir}/last-body.txt")"
check "P3: summary counts the derived required list exactly" "$(ok out_has ' required=4 ')"

# 19. static mirror of the workflow and of the script's write discipline.
# shellcheck source=scripts/lib/test-main-health-workflow.sh
. "${repo_root}/scripts/lib/test-main-health-workflow.sh"

printf '\ntest-main-health: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" -eq 0 ]
