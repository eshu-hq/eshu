#!/usr/bin/env bash
# gate-timing.sh - record where `make pre-push` and `make pre-pr` spend time.
#
# Sourced by scripts/dev/pre-push.sh and scripts/dev/pre-pr.sh, after
# pre_pr_git_state_init has set pre_pr_state_dir. Usage:
#
#   eshu_timing_init <script-name> <base-ref>   once, before the first step
#   eshu_timing_note <name> <seconds> <exit>    from run_step, after each step
#   eshu_timing_meta <key> <value>              optional run context
#   eshu_timing_finish <overall-exit>           once, before the summary prints
#
# Per-gate durations come from the ci-gates `--report-file` the caller already
# writes at $ESHU_TIMING_GATE_REPORT; per-package test seconds come from
# `go test` output the caller tees into $ESHU_TIMING_TEST_LOG / _RACE_LOG.
#
# This is measurement only. Every function returns 0 and a failure inside it
# prints one warning line; it can never turn a green run red or a red one
# green. ESHU_GATE_TIMING=0 turns it off. The tool and the store layout are in
# scripts/dev/gate-timing.py.

ESHU_TIMING_TOOL="${ESHU_TIMING_TOOL:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../dev" && pwd)/gate-timing.py}"

eshu_timing_init() {
	ESHU_TIMING_SCRIPT="${1:-unknown}"
	ESHU_TIMING_BASE="${2:-}"
	ESHU_TIMING_START_EPOCH="$(date +%s)"
	ESHU_TIMING_LOAD_START="$(python3 -I -c 'import os; print(os.getloadavg()[0])' 2>/dev/null || echo -1)"
	local dir="${pre_pr_state_dir:-${TMPDIR:-/tmp}}"
	ESHU_TIMING_STEPS="${dir}/timing-steps.tsv"
	ESHU_TIMING_META="${dir}/timing-meta.tsv"
	ESHU_TIMING_GATE_REPORT="${dir}/timing-gates.json"
	ESHU_TIMING_TEST_LOG="${dir}/timing-test.log"
	ESHU_TIMING_RACE_LOG="${dir}/timing-race.log"
	: >"${ESHU_TIMING_STEPS}" 2>/dev/null || true
	: >"${ESHU_TIMING_META}" 2>/dev/null || true
	: >"${ESHU_TIMING_TEST_LOG}" 2>/dev/null || true
	: >"${ESHU_TIMING_RACE_LOG}" 2>/dev/null || true
	return 0
}

eshu_timing_note() {
	printf '%s\t%s\t%s\n' "$1" "$2" "$3" >>"${ESHU_TIMING_STEPS:-/dev/null}" 2>/dev/null || true
	return 0
}

eshu_timing_meta() {
	printf '%s\t%s\n' "$1" "$2" >>"${ESHU_TIMING_META:-/dev/null}" 2>/dev/null || true
	return 0
}

eshu_timing_finish() {
	[[ "${ESHU_GATE_TIMING:-1}" == "0" ]] && return 0
	[[ -n "${ESHU_TIMING_STEPS:-}" ]] || return 0
	printf '\n'
	python3 -I "${ESHU_TIMING_TOOL}" record \
		--script "${ESHU_TIMING_SCRIPT}" --base "${ESHU_TIMING_BASE}" \
		--overall "${1:-0}" --start-epoch "${ESHU_TIMING_START_EPOCH}" \
		--load-start "${ESHU_TIMING_LOAD_START}" \
		--steps "${ESHU_TIMING_STEPS}" --meta "${ESHU_TIMING_META}" \
		--gate-report "${ESHU_TIMING_GATE_REPORT}" \
		--test-log "${ESHU_TIMING_TEST_LOG}" --race-log "${ESHU_TIMING_RACE_LOG}" ||
		printf 'gate-timing: timing not recorded (recorder exited non-zero)\n' >&2
	return 0
}
