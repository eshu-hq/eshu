#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# checks-arm cases for scripts/dev/test-pre-enqueue-check.sh (#7332, #8381).
# Sourced, not run: it uses the parent's new_case, edit, run, expect_red and
# check helpers and its ${FIXTURES}, ${OUT}, ${RC} and ${HEAD_SHA} state. Check
# rows come from `gh api repos/.../commits/<sha>/check-runs`; the fake gh
# rejects `gh pr checks --json` the way gh 2.46.0 does.

# gh 2.46.0 has no `gh pr checks --json` (#8381). The fake rejects it exactly
# as that gh does; the checks arm must pass a green PR by reading check runs
# through `gh api`, and must never call `gh pr checks`.
new_case gh-no-pr-checks-json
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q '^PASS checks +complete; required-gates-complete=success; mergeStateStatus=CLEAN$' <<<"${OUT}" || ok=1
if rg -q 'no check rows reported|unknown flag' <<<"${OUT}"; then ok=1; fi
if rg -q '^pr checks' "${FIXTURES}/calls.log"; then ok=1; fi
rg -q '^api --paginate repos/eshu-hq/eshu/commits/[0-9a-f]{40}/check-runs\?per_page=100 ' "${FIXTURES}/calls.log" || ok=1
check "GREEN checks arm passes without gh pr checks --json (gh 2.46.0)" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# --paginate returns one object per page; rows on every page count.
new_case two-pages
jq -c '.check_runs |= .[:3]' "${FIXTURES}/check-runs.json" >"${FIXTURES}/check-runs.page1"
jq -c '.check_runs |= .[3:] | .check_runs += [{name:"late-page",status:"in_progress",conclusion:null}]' \
	"${FIXTURES}/check-runs.json" >"${FIXTURES}/check-runs.page2"
cat "${FIXTURES}/check-runs.page1" "${FIXTURES}/check-runs.page2" >"${FIXTURES}/check-runs.json"
expect_red "pending check on the second page" checks
ok=0
rg -q 'checks +rows=7 pending=1 ' <<<"${OUT}" || ok=1
rg -q '^FAIL checks +pending=1: late-page; ' <<<"${OUT}" || ok=1
check "RED rows from every page are counted" "${ok}"

new_case pending
edit check-runs.json '.check_runs += [{name:"go-race",status:"in_progress",conclusion:null}]'
expect_red "pending check" checks
new_case queued-run
edit check-runs.json '.check_runs += [{name:"go-race",status:"queued",conclusion:null}]'
expect_red "queued (not started) check run" checks
new_case failing
edit check-runs.json '.check_runs += [{name:"go-core",status:"completed",conclusion:"failure"}]'
expect_red "failing check" checks
new_case cancelled
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"}]'
expect_red "cancelled check" checks
for conclusion in timed_out action_required; do
	new_case "run-${conclusion}"
	# shellcheck disable=SC2016 # jq variable, not a shell expansion.
	edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:$c}]' --arg c "${conclusion}"
	expect_red "check run concluded ${conclusion}" checks
	ok=0
	rg -q "^FAIL checks +fail=1: e2e; " <<<"${OUT}" || ok=1
	check "RED the ${conclusion} row is counted as a failure and named" "${ok}"
done
new_case no-checks
edit check-runs.json '.check_runs = []'
expect_red "no check rows" checks
# A zero-row read is an error, not "pending": it must say so.
ok=0
rg -q '^FAIL checks +no check rows reported for [0-9a-f]{40}; ' <<<"${OUT}" || ok=1
check "RED zero check rows is named an error, not pending" "${ok}"
for ctx in go-core-complete go-race-complete; do
	new_case "missing-${ctx}"
	# shellcheck disable=SC2016 # jq variable, not a shell expansion.
	edit check-runs.json '.check_runs |= map(select(.name != $c))' --arg c "${ctx}"
	expect_red "required context ${ctx} absent" checks
	ok=0
	rg -q "^FAIL checks +${ctx}=absent; \$" <<<"${OUT}" || ok=1
	check "RED the absent ${ctx} is named" "${ok}"
done
new_case required-not-success
edit check-runs.json '.check_runs |= map(if .name == "go-race-complete" then .conclusion = "skipped" else . end)'
expect_red "required context concluded skipped, not success" checks
ok=0
rg -q '^FAIL checks +go-race-complete=skipped; $' <<<"${OUT}" || ok=1
check "RED a skipped required context is named with its conclusion" "${ok}"
# gh itself failing on the check-runs read is a named error, never a silent
# empty read.
new_case check-runs-read-error
printf '1\n' >"${FIXTURES}/check-runs.rc"
expect_red "gh api check-runs read that errors" checks
ok=0
rg -q '^FAIL checks +gh api check-runs read failed \(exit 1\)' <<<"${OUT}" || ok=1
check "RED a failed check-runs read is named, not reported as zero rows" "${ok}"
new_case rgc-pending
edit status.json '.statuses[0].state = "pending"'
expect_red "required-gates-complete pending" checks
new_case rgc-absent
edit status.json '.statuses |= map(select(.context != "required-gates-complete"))'
expect_red "required-gates-complete absent" checks
new_case not-clean
edit pr.json '.mergeStateStatus = "BLOCKED"'
expect_red "mergeStateStatus BLOCKED" checks
new_case unknown-bucket
edit check-runs.json '.check_runs += [{name:"future-check",status:"completed",conclusion:"stale"}]'
expect_red "check row with an unrecognized conclusion" checks
# Precedence: a required context that is both a check run and a commit status
# is judged by the check run. A failing run must not be masked by a success
# status; a success run needs no status at all.
new_case rgc-run-failure-status-success
edit check-runs.json '.check_runs += [{name:"required-gates-complete",status:"completed",conclusion:"failure"}]'
expect_red "required-gates-complete failed as a check run but success as a status" checks
ok=0
rg -q 'required-gates-complete=failure; ' <<<"${OUT}" || ok=1
check "RED the check run wins over a success status and the failure is named" "${ok}"
new_case rgc-run-success-no-status
edit check-runs.json '.check_runs += [{name:"required-gates-complete",status:"completed",conclusion:"success"}]'
edit status.json '.statuses = []'
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
check "GREEN required-gates-complete as a success check run passes without a status" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
