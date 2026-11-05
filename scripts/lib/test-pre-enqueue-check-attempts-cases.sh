#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# shellcheck disable=SC2034,SC2154 # RUN_ARGS, target and helpers belong to the sourcing test.
#
# Superseded-attempt cases for scripts/dev/test-pre-enqueue-check.sh (#10148).
# Sourced, not run: it uses the parent's new_case, edit, run, expect_red and
# check helpers and its ${FIXTURES}, ${OUT} and ${RC} state.
#
# The default check-runs read returns only the latest attempt of each job, so a
# rerun that goes green hides the red attempt before it. The script makes a
# second read with filter=all; the fake gh serves it from check-runs-all.json
# (see test-pre-enqueue-check-fake-gh.sh). Run 41991514999 on PR #10143 was the
# real case: attempt 1 of the reducer contention gate failed, attempt 2 passed.
GATE="call-reducer-contention-gate / reducer contention gate"

# seed_attempts <name> <first-conclusion> [second-conclusion]: the latest read
# holds the second attempt (check run 2002); the filter=all read also holds the
# first (check run 2001), the way GitHub lists a rerun inside one check suite.
seed_attempts() {
	local name="$1" first="$2" second="${3:-success}"
	# shellcheck disable=SC2016 # jq variables, not shell expansions.
	edit check-runs.json '.check_runs += [{id:2002,name:$n,status:"completed",conclusion:$c,check_suite:{id:9}}]' \
		--arg n "${name}" --arg c "${second}"
	# shellcheck disable=SC2016 # jq variables, not shell expansions.
	jq --arg n "${name}" --arg c "${first}" '.check_runs += [{id:2001,name:$n,status:"completed",conclusion:$c,
		completed_at:"2026-11-05T08:42:36Z",check_suite:{id:9}}]' "${FIXTURES}/check-runs.json" >"${FIXTURES}/check-runs-all.json"
}

# The seeded red-then-green run: attempt 1 red, attempt 2 green on the same
# head. The latest read is all green, so only the filter=all read can see it.
new_case attempt-red-then-green
seed_attempts "${GATE}" failure
expect_red "red attempt superseded by a green rerun" checks
ok=0
rg -q "^FAIL checks +superseded-red=1: ${GATE} \(check run 2001, failure, 2026-11-05T08:42:36Z\); " <<<"${OUT}" || ok=1
rg -q -F -- "pass --dispositioned ${GATE}=<issue-or-reason> to accept" <<<"${OUT}" || ok=1
check "RED the superseded red names the check, the attempt's check run, and the disposition flag" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# The orchestrator names the accepted red attempt; the arm passes and says so.
new_case attempt-red-then-green-dispositioned
seed_attempts "${GATE}" failure
RUN_ARGS=(--dispositioned "${GATE}=#10148 cap-sized red, rerun green")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q -F "dispositioned: ${GATE}=#10148 cap-sized red, rerun green (applied: 1 superseded red attempt(s))" <<<"${OUT}" || ok=1
rg -q "^PASS checks +complete; " <<<"${OUT}" || ok=1
check "GREEN a dispositioned superseded red passes and prints the disposition" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# A disposition for another check does not excuse this one.
new_case attempt-wrong-disposition
seed_attempts "${GATE}" failure
RUN_ARGS=(--dispositioned "other gate=#1 unrelated")
expect_red "superseded red with a disposition for a different check" checks

# The clean single-attempt case stays green: every row is in both reads.
new_case attempt-single
seed_attempts "${GATE}" success
rm -f "${FIXTURES}/check-runs-all.json"
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
if rg -q 'superseded' <<<"${OUT}"; then ok=1; fi
check "GREEN a single attempt per job passes with no superseded note" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# A superseded green attempt is not a finding.
new_case attempt-green-then-green
seed_attempts "${GATE}" success
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
check "GREEN a superseded green attempt passes" "${ok}"

# A cancelled attempt followed by a rerun is the documented remedy (the merge
# rule: "A cancelled gate is not red: rerun it"), so it must not fail the arm.
new_case attempt-cancelled-then-green
seed_attempts "${GATE}" cancelled
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
check "GREEN a cancelled attempt that was rerun green passes" "${ok}"

# timed_out and action_required attempts are completed reds like failure.
for conclusion in timed_out action_required; do
	new_case "attempt-${conclusion}"
	seed_attempts "${GATE}" "${conclusion}"
	expect_red "superseded ${conclusion} attempt" checks
	ok=0
	rg -q "^FAIL checks +superseded-red=1: ${GATE} \(check run 2001, ${conclusion}, " <<<"${OUT}" || ok=1
	check "RED the superseded ${conclusion} row is named with its conclusion" "${ok}"
done

# A ruleset-required umbrella that went red and was rerun green is still a
# completed red on the head. The ruleset only reads the latest attempt, so it
# does not block, and a disposition (which the latest-attempt arm ignores for a
# ruleset-required context) is the accepted way through here.
new_case attempt-required-context
seed_attempts "go-core-complete" failure
expect_red "superseded red attempt of a ruleset-required context" checks
new_case attempt-required-context-dispositioned
seed_attempts "go-core-complete" failure
RUN_ARGS=(--dispositioned "go-core-complete=#10148 flaky runner, rerun green")
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q -F "dispositioned: go-core-complete=#10148 flaky runner, rerun green (applied: 1 superseded red attempt(s))" <<<"${OUT}" || ok=1
check "GREEN a dispositioned superseded red of a required context passes" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# A disposition that only matches a superseded red must not also excuse an
# UNSTABLE merge state: UNSTABLE needs a red in the LATEST attempt.
new_case attempt-unstable-not-excused
seed_attempts "${GATE}" failure
edit pr.json '.mergeStateStatus = "UNSTABLE"'
RUN_ARGS=(--dispositioned "${GATE}=#10148 cap-sized red, rerun green")
expect_red "UNSTABLE with only a superseded red dispositioned" checks
ok=0
rg -q '^FAIL checks +mergeStateStatus=UNSTABLE; $' <<<"${OUT}" || ok=1
check "RED UNSTABLE is not excused by a superseded-red disposition" "${ok}"

# The filter=all read failing is a named error, never a silent pass.
new_case attempt-read-error
seed_attempts "${GATE}" failure
printf '1\n' >"${FIXTURES}/check-runs-all.rc"
expect_red "gh api check-runs filter=all read that errors" checks
ok=0
rg -q '^FAIL checks +gh api check-runs filter=all read failed \(exit 1\)' <<<"${OUT}" || ok=1
check "RED a failed filter=all read is named, not read as no superseded attempts" "${ok}"

# The superseded read is the only addition; every call stays a read.
ok=0
rg -q '^api --paginate repos/eshu-hq/eshu/commits/[0-9a-f]{40}/check-runs\?per_page=100&filter=all ' "${FIXTURES}/calls.log" || ok=1
check "the superseded-attempt read is a REST filter=all check-runs call" "${ok}"
