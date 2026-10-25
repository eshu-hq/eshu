#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Check-suite cases for scripts/dev/test-pre-enqueue-check.sh (#8381). Sourced,
# not run: it uses the parent's new_case, edit, run, expect_red and check
# helpers and its ${FIXTURES}, ${OUT}, ${RC} and ${HEAD_SHA} state.
#
# The check-runs endpoint defaults to filter=latest, which keeps the latest run
# per name WITHIN each check suite. A re-triggered workflow on the same SHA adds
# a second suite, and GitHub defines no winner across suites. Rows are
# {id, name, status, conclusion, check_suite:{id}}; the fake gh applies the
# script's --jq projection to them.

# run_row <id> <name> <status> <conclusion-json> <suite>: one check-run row.
run_row() {
	jq -nc --argjson i "$1" --arg n "$2" --arg s "$3" --argjson c "$4" --argjson u "$5" \
		'{id:$i,name:$n,status:$s,conclusion:$c,check_suite:{id:$u}}'
}
hint='older run on the same SHA still counted; GitHub defines no winner across suites; wait for or cancel the orphan \(gh run list --commit [0-9a-f]{40}\)'
# drop_row <name>: remove the default row so a case can add its own suites.
drop_row() {
	# shellcheck disable=SC2016 # jq variable, not a shell expansion.
	edit check-runs.json '.check_runs |= map(select(.name != $n))' --arg n "$1"
}
# add_rows <row-json>...: append rows to the check-runs fixture.
add_rows() {
	local rows
	rows="$(printf '%s\n' "$@" | jq -s '.')"
	# shellcheck disable=SC2016 # jq variable, not a shell expansion.
	edit check-runs.json '.check_runs += $r' --argjson r "${rows}"
}

# S1: an older suite's failure on a required context is superseded by the newest
# suite's success. The context reads success, the superseded row leaves the
# pending/fail tally, the note names the old suite, and the arm passes.
new_case suite-required-older-failure
drop_row go-core-complete
add_rows "$(run_row 1 go-core-complete completed '"failure"' 10)" \
	"$(run_row 2 go-core-complete completed '"success"' 20)"
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
rg -q 'checks +rows=7 pending=0 fail=0 unknown=0$' <<<"${OUT}" || ok=1
rg -q '^     checks +go-core-complete: newest suite 20 wins; superseded suite\(s\): 10$' <<<"${OUT}" || ok=1
rg -q '^     checks +multi-suite: go-core-complete \(2 suites: 10, 20\)$' <<<"${OUT}" || ok=1
rg -q '^PASS checks ' <<<"${OUT}" || ok=1
check "GREEN the newest suite wins: the older required-context failure is not tallied" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# S1b: a queued or in-progress row in a superseded suite of a required context
# is not pending once the newest suite is done; the note still names it.
for st in queued in_progress; do
	new_case "suite-required-older-${st}"
	drop_row go-race-complete
	add_rows "$(run_row 1 go-race-complete "${st}" null 10)" \
		"$(run_row 2 go-race-complete completed '"success"' 20)"
	run
	ok=0
	[[ "${RC}" -eq 0 ]] || ok=1
	rg -q 'checks +rows=7 pending=0 fail=0 unknown=0$' <<<"${OUT}" || ok=1
	rg -q '^     checks +go-race-complete: newest suite 20 wins; superseded suite\(s\): 10$' <<<"${OUT}" || ok=1
	check "GREEN a superseded ${st} required-context row is not pending" "${ok}"
	[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
done

# S2: the newest suite's failure is not hidden by an older success.
new_case suite-required-newer-failure
drop_row go-core-complete
add_rows "$(run_row 1 go-core-complete completed '"success"' 10)" \
	"$(run_row 2 go-core-complete completed '"failure"' 20)"
expect_red "required context passed in an older suite, failed in the newest" checks
ok=0
rg -q 'go-core-complete=failure; ' <<<"${OUT}" || ok=1
rg -q '^     checks +multi-suite: go-core-complete \(2 suites: 10, 20\)$' <<<"${OUT}" || ok=1
check "RED an older success never hides the newest suite's failure" "${ok}"

# S3: a non-required name is never collapsed to its newest suite: a newer pass
# must not hide an older failure (fail-closed, all rows count).
new_case suite-nonrequired-older-failure
add_rows "$(run_row 3 lint-orphan completed '"failure"' 10)" \
	"$(run_row 4 lint-orphan completed '"success"' 20)"
expect_red "non-required name failed in one suite, passed in a newer one" checks
ok=0
rg -q "^FAIL checks +fail=1: lint-orphan -- ${hint}" <<<"${OUT}" || ok=1
rg -q '^     checks +multi-suite: lint-orphan \(2 suites: 10, 20\)$' <<<"${OUT}" || ok=1
check "RED fail=1 stays, the multi-suite line names both suites, no collapse by name" "${ok}"

# S4: one job name defined by 13 workflows is 13 distinct checks.
new_case suite-thirteen-queued
add_rows "$(run_row 100 'queue-selection / select' completed '"success"' 1)"
for n in $(seq 2 14); do
	add_rows "$(run_row $((100 + n)) 'queue-selection / select' queued null "${n}")"
done
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q 'checks +rows=20 pending=13 ' <<<"${OUT}" || ok=1
rg -q "^     checks +pending=13: queue-selection / select -- ${hint}" <<<"${OUT}" || ok=1
rg -q '^     checks +multi-suite: queue-selection / select \(14 suites: ' <<<"${OUT}" || ok=1
rg -q '^PASS checks ' <<<"${OUT}" || ok=1
check "GREEN pending=13 is counted per suite, named multi-suite, and is a note (#9094)" "${ok}"
[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'

# S5: the request never asks for filter=all, and a fixture never repeats a
# (name, suite) pair, so a duplicate row cannot appear from the same suite.
new_case suite-request-shape
run
ok=0
if rg -q 'filter=all' "${FIXTURES}/calls.log"; then ok=1; fi
rg -q 'check-runs\?per_page=100 ' "${FIXTURES}/calls.log" || ok=1
[[ "$(jq '[.check_runs[] | [.name, (.check_suite.id // 0)]] | (length - (unique | length))' "${FIXTURES}/check-runs.json")" -eq 0 ]] || ok=1
check "GREEN the request has no filter=all and rows are unique per (name, suite)" "${ok}"

# S6: a row with no check_suite projects to suite 0. Suite 0 is never
# superseded: a required-context failure with a missing suite stays tallied and
# stays the context's state beside a newer suite's success (fail-closed).
new_case suite-required-null-suite-failure
drop_row go-core-complete
add_rows "$(jq -nc '{id:5,name:"go-core-complete",status:"completed",conclusion:"failure",check_suite:null}')" \
	"$(run_row 6 go-core-complete completed '"success"' 20)"
expect_red "required context failed with a missing check_suite beside a newer suite's success" checks
ok=0
rg -q 'go-core-complete=failure; ' <<<"${OUT}" || ok=1
rg -q '^FAIL checks +fail=1: go-core-complete' <<<"${OUT}" || ok=1
if rg -q 'superseded suite' <<<"${OUT}"; then ok=1; fi
check "RED a missing check_suite is never superseded and the failure is named" "${ok}"
