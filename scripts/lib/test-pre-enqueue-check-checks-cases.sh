#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# shellcheck disable=SC2034,SC2154 # RUN_ARGS, target and helpers belong to the sourcing test.
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
rg -q '^PASS checks +complete; go-core-complete,go-race-complete=success; mergeStateStatus=CLEAN$' <<<"${OUT}" || ok=1
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
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q 'checks +rows=7 pending=1 ' <<<"${OUT}" || ok=1
rg -q '^     checks +pending=1: late-page' <<<"${OUT}" || ok=1
rg -q '^PASS checks ' <<<"${OUT}" || ok=1
check "GREEN rows from every page are counted; the pending one is a note, not a failure (#9094)" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# A pending check the ruleset does not require is a note, not a failure (#9094).
for st in in_progress queued; do
	new_case "pending-${st}"
	# shellcheck disable=SC2016 # jq variable, not a shell expansion.
	edit check-runs.json '.check_runs += [{name:"go-race",status:$s,conclusion:null}]' --arg s "${st}"
	run
	ok=0
	[[ "${RC}" -eq 0 ]] || ok=1
	[[ "$(fail_lines)" == 0 ]] || ok=1
	rg -q '^     checks +pending=1: go-race$' <<<"${OUT}" || ok=1
	check "GREEN a ${st} non-required check passes with a pending note" "${ok}"
	[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
done
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
# The aggregate is not ruleset-required (#9094): pending or absent is a note.
new_case rgc-pending
edit status.json '.statuses[0].state = "pending"'
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q '^     checks +required-gates-complete=pending \(not ruleset-required; does not block\)$' <<<"${OUT}" || ok=1
rg -q '^PASS checks +complete; go-core-complete,go-race-complete=success; ' <<<"${OUT}" || ok=1
check "GREEN a pending aggregate passes with a note" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
new_case rgc-absent
edit status.json '.statuses |= map(select(.context != "required-gates-complete"))'
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q '^     checks +required-gates-complete=absent \(not ruleset-required; does not block\)$' <<<"${OUT}" || ok=1
check "GREEN an absent aggregate passes with a note" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# A completed red aggregate fails; a disposition passes it and is printed.
new_case rgc-red
edit status.json '.statuses[0].state = "failure"'
expect_red "required-gates-complete red (commit status)" checks
ok=0
rg -q '^FAIL checks +required-gates-complete=failure \(not ruleset-required; pass --dispositioned required-gates-complete=<issue-or-reason> to accept\); $' <<<"${OUT}" || ok=1
check "RED a red aggregate names the context and the disposition flag" "${ok}"
new_case rgc-red-dispositioned
edit status.json '.statuses[0].state = "failure"'
RUN_ARGS=(--dispositioned "required-gates-complete=#9094 aggregate not required")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q '^     checks +dispositioned: required-gates-complete=#9094 aggregate not required \(applied: failure\)$' <<<"${OUT}" || ok=1
check "GREEN a red aggregate with a disposition passes and prints the disposition" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# The flag repeats; each use is printed.
new_case disposition-repeats
edit status.json '.statuses[0].state = "failure"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"failure"}]'
RUN_ARGS=(--dispositioned "required-gates-complete=#9094" --dispositioned "e2e=#4242 known flake")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q '^     checks +dispositioned: required-gates-complete=#9094 \(applied: failure\)$' <<<"${OUT}" || ok=1
rg -q '^     checks +dispositioned: e2e=#4242 known flake \(applied: 1 failing row\(s\)\)$' <<<"${OUT}" || ok=1
check "GREEN --dispositioned repeats, one printed line per use" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# A disposition names a context; it never excuses another red one.
new_case disposition-other-context
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"failure"}]'
RUN_ARGS=(--dispositioned "required-gates-complete=#9094")
expect_red "red e2e while only the aggregate is dispositioned" checks
ok=0
rg -q '^     checks +dispositioned: required-gates-complete=#9094 \(unused: not red\)$' <<<"${OUT}" || ok=1
check "RED a disposition on another context is printed as unused" "${ok}"
# A ruleset-required context can never be dispositioned: the ruleset blocks it.
new_case disposition-ruleset-required
edit check-runs.json '.check_runs |= map(if .name == "go-core-complete" then .conclusion = "failure" else . end)'
RUN_ARGS=(--dispositioned "go-core-complete=#1")
expect_red "red ruleset-required context with a disposition" checks
ok=0
rg -q '^     checks +dispositioned: go-core-complete=#1 \(ignored: ruleset-required\)$' <<<"${OUT}" || ok=1
check "RED a ruleset-required context ignores its disposition and says so" "${ok}"
bad_n=0
for bad in "no-equals" "=reason" "ctx=" " =reason"; do
	bad_n=$((bad_n + 1))
	new_case "disposition-malformed-${bad_n}"
	RUN_ARGS=(--dispositioned "${bad}")
	run
	ok=0
	[[ "${RC}" -eq 2 ]] || ok=1
	rg -q 'usage: ' <<<"${OUT}" || ok=1
	check "usage error (exit 2) for malformed --dispositioned '${bad}'" "${ok}"
done
# 114 of the 164 live check names contain spaces (queue-selection / select).
# The context splits at the first "=": the name keeps its spaces, the reason may
# hold "=" too. A check-run row that is not in the manifest is judged by row.
new_case disposition-spaced-row
edit check-runs.json '.check_runs += [{name:"queue-selection / select",status:"completed",conclusion:"failure"}]'
expect_red "red spaced-name check with no disposition" checks
new_case disposition-spaced-row-dispositioned
edit check-runs.json '.check_runs += [{name:"queue-selection / select",status:"completed",conclusion:"failure"}]'
RUN_ARGS=(--dispositioned "queue-selection / select=#9094 a=b")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q '^     checks +dispositioned: queue-selection / select=#9094 a=b \(applied: 1 failing row\(s\)\)$' <<<"${OUT}" || ok=1
check "GREEN a spaced-name red check passes with its disposition, which prints applied" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# A spaced name in the manifest takes the context path (check run or status).
new_case disposition-spaced-manifest
printf 'go-core-complete\ngo-race-complete\nqueue-selection / select\n' >"${FIXTURES}/contexts-full.txt"
edit check-runs.json '.check_runs += [{name:"queue-selection / select",status:"completed",conclusion:"failure"}]'
expect_red "red spaced-name manifest context with no disposition" checks
new_case disposition-spaced-manifest-dispositioned
printf 'go-core-complete\ngo-race-complete\nqueue-selection / select\n' >"${FIXTURES}/contexts-full.txt"
edit check-runs.json '.check_runs += [{name:"queue-selection / select",status:"completed",conclusion:"failure"}]'
RUN_ARGS=(--dispositioned "queue-selection / select=#9094")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q '^     checks +dispositioned: queue-selection / select=#9094 \(applied: failure\)$' <<<"${OUT}" || ok=1
check "GREEN a spaced-name manifest context passes with its disposition, which prints applied" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# The required set comes from ci-gates, never from a list in the script
# (#9094): a context the manifest adds is required, and one it drops is not.
new_case ruleset-adds-context
printf 'go-core-complete\ngo-race-complete\ngo-extra-complete\n' >"${FIXTURES}/contexts-ruleset.txt"
printf 'go-core-complete\ngo-race-complete\ngo-extra-complete\nrequired-gates-complete\n' >"${FIXTURES}/contexts-full.txt"
expect_red "ruleset set gained a context the check run lacks" checks
ok=0
rg -q '^FAIL checks +go-extra-complete=absent; $' <<<"${OUT}" || ok=1
rg -q 'contexts --registry .* --ruleset-only$' "${FIXTURES}/ci-gates-calls.log" || ok=1
check "RED a context only the manifest names is required, and ci-gates was asked" "${ok}"
new_case ruleset-drops-context
printf 'go-core-complete\n' >"${FIXTURES}/contexts-ruleset.txt"
printf 'go-core-complete\nrequired-gates-complete\n' >"${FIXTURES}/contexts-full.txt"
edit check-runs.json '.check_runs |= map(select(.name != "go-race-complete"))'
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q '^PASS checks +complete; go-core-complete=success; ' <<<"${OUT}" || ok=1
check "GREEN a context the manifest drops is not required" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# ci-gates failing, or printing no ruleset context, is exit 2, never a pass.
new_case ci-gates-unreadable
printf '1\n' >"${FIXTURES}/contexts.rc"
run
ok=0
[[ "${RC}" -eq 2 ]] || ok=1
rg -q 'cannot read the ruleset-required contexts' <<<"${OUT}" || ok=1
check "usage error (exit 2) when ci-gates cannot list the ruleset contexts" "${ok}"
new_case ci-gates-empty
: >"${FIXTURES}/contexts-ruleset.txt"
run
ok=0
[[ "${RC}" -eq 2 ]] || ok=1
rg -q 'cannot read the ruleset-required contexts' <<<"${OUT}" || ok=1
check "usage error (exit 2) when the ruleset set is empty" "${ok}"
# End to end with the real binary and the real manifest: the live set is the two
# Go umbrellas and the aggregate is not in it.
if go version >/dev/null 2>&1; then
	new_case real-ci-gates
	set +e
	OUT="$(cd "${CLONE}" && env -u PRE_ENQUEUE_CI_GATES "${target}" "${FAKE_PR}" "${HEAD_SHA}" 2>&1)"
	RC=$?
	set -e
	ok=0
	[[ "${RC}" -eq 0 ]] || ok=1
	rg -q '^PASS checks +complete; go-core-complete,go-race-complete=success; ' <<<"${OUT}" || ok=1
	check "GREEN the real ci-gates --ruleset-only yields the two Go umbrellas" "${ok}"
	[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
else
	printf 'SKIP: `go version` failed (go missing or its toolchain/GOROOT is broken); real ci-gates case not run\n'
fi
new_case not-clean
edit pr.json '.mergeStateStatus = "BLOCKED"'
expect_red "mergeStateStatus BLOCKED" checks
# GitHub reports UNSTABLE for a PR whose non-required check completed red or
# cancelled (#9489). The arm accepts UNSTABLE only when every completed red
# context is dispositioned and every ruleset-required context is success, and
# it names the dispositioned contexts in the PASS line.
new_case unstable-dispositioned
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"}]'
RUN_ARGS=(--dispositioned "e2e=#9488 cancelled at the job cap after one rerun")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q '^PASS checks +complete; go-core-complete,go-race-complete=success; mergeStateStatus=UNSTABLE \(dispositioned: e2e\)$' <<<"${OUT}" || ok=1
check "GREEN UNSTABLE with every red dispositioned passes and names the dispositions" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
new_case unstable-not-dispositioned
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"}]'
expect_red "UNSTABLE with a red check and no disposition" checks
ok=0
rg -q '^FAIL checks +fail=1: e2e; mergeStateStatus=UNSTABLE; $' <<<"${OUT}" || ok=1
check "RED UNSTABLE names the undispositioned red and the merge state" "${ok}"
new_case unstable-one-undispositioned
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"},{name:"bench",status:"completed",conclusion:"failure"}]'
RUN_ARGS=(--dispositioned "e2e=#9488")
expect_red "UNSTABLE with one dispositioned red and one undispositioned red" checks
ok=0
rg -q '^FAIL checks +fail=1: bench; mergeStateStatus=UNSTABLE; $' <<<"${OUT}" || ok=1
check "RED the undispositioned red is named and UNSTABLE is not accepted" "${ok}"
new_case unstable-required-red
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit check-runs.json '.check_runs |= map(if .name == "go-core-complete" then .conclusion = "failure" else . end)'
RUN_ARGS=(--dispositioned "go-core-complete=#1")
expect_red "UNSTABLE with a ruleset-required context red, even when named" checks
ok=0
rg -q '^FAIL checks +fail=1: go-core-complete; go-core-complete=failure; mergeStateStatus=UNSTABLE; $' <<<"${OUT}" || ok=1
check "RED a red ruleset-required context keeps UNSTABLE failing" "${ok}"
new_case unstable-no-red
edit pr.json '.mergeStateStatus = "UNSTABLE"'
RUN_ARGS=(--dispositioned "e2e=#9488")
expect_red "UNSTABLE with no completed red check to excuse it" checks
ok=0
rg -q '^FAIL checks +mergeStateStatus=UNSTABLE; $' <<<"${OUT}" || ok=1
check "RED UNSTABLE with nothing dispositioned that is red is not accepted" "${ok}"
# A red commit status outside the manifest is a red context too (#9489 review
# F1): without a disposition it fails the arm even when another red is
# dispositioned, and with one it is excused like any other red.
new_case unstable-undispositioned-status
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"}]'
edit status.json '.statuses += [{context:"external-status",state:"failure"}]'
RUN_ARGS=(--dispositioned "e2e=#9488")
expect_red "UNSTABLE with an undispositioned red commit status outside the manifest" checks
ok=0
rg -q '^FAIL checks +external-status=failure \(commit status; pass --dispositioned external-status=<issue-or-reason> to accept\); mergeStateStatus=UNSTABLE; $' <<<"${OUT}" || ok=1
check "RED the undispositioned red commit status is named and UNSTABLE is not accepted" "${ok}"
new_case unstable-dispositioned-status
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit status.json '.statuses += [{context:"external-status",state:"error"}]'
RUN_ARGS=(--dispositioned "external-status=#4242 external reporter down")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q '^     checks +dispositioned: external-status=#4242 external reporter down \(applied: error\)$' <<<"${OUT}" || ok=1
rg -q '^PASS checks +complete; go-core-complete,go-race-complete=success; mergeStateStatus=UNSTABLE \(dispositioned: external-status\)$' <<<"${OUT}" || ok=1
check "GREEN UNSTABLE with a dispositioned red commit status outside the manifest passes" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
# The same context dispositioned twice is named once in the PASS line.
new_case unstable-dispositioned-twice
edit pr.json '.mergeStateStatus = "UNSTABLE"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"}]'
RUN_ARGS=(--dispositioned "e2e=#9488" --dispositioned "e2e=#9488 again")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q '^PASS checks +complete; go-core-complete,go-race-complete=success; mergeStateStatus=UNSTABLE \(dispositioned: e2e\)$' <<<"${OUT}" || ok=1
check "GREEN a context dispositioned twice is named once in the PASS line" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
new_case blocked-dispositioned
edit pr.json '.mergeStateStatus = "BLOCKED"'
edit check-runs.json '.check_runs += [{name:"e2e",status:"completed",conclusion:"cancelled"}]'
RUN_ARGS=(--dispositioned "e2e=#9488")
expect_red "BLOCKED with every red dispositioned" checks
ok=0
rg -q '^FAIL checks +mergeStateStatus=BLOCKED; $' <<<"${OUT}" || ok=1
check "RED a disposition never makes BLOCKED pass" "${ok}"
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
rg -q 'required-gates-complete=failure \(' <<<"${OUT}" || ok=1
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
