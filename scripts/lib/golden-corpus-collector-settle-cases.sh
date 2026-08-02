#!/usr/bin/env bash
# Collector settle poll cases for test-verify-golden-corpus-gate.sh.
#
# Sourced, never executed: it runs in the caller's shell and uses the caller's
# ${script}, ${collector_settle_lib}, require_in(), and fail(). Extracted so
# the mirror test stays under the 500-line cap, the same reason
# golden-corpus-phase-timing-cases.sh and golden-corpus-matcher-guard-cases.sh
# are separate lib chunks rather than inline in the mirror test.

# A collector that no-ops must not let the gate pass: liveness + facts-landed.
# Both checks live in the collector settle lib chunk, not the orchestrator body.
require_in "collector liveness check" "${collector_settle_lib}" "exited during settle"
# Anchored on the arithmetic guard, not on its message: the phrase "credentialed
# collector source" also appears in the success printf one line below, so a
# needle on the phrase has two homes and deleting the guard leaves it green.
require_in "cassette facts landed check" "${collector_settle_lib}" \
	"(( collector_sources >= GATE_MIN_COLLECTOR_SOURCES ))"
# The settle window must be POLLED, not slept for a fixed duration: a fixed
# `sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"` has zero margin once host load or
# Docker I/O contention slows down fact commit (the exact regression this
# lib fixed -- see the lib's own header). GATE_COLLECTOR_SETTLE_POLL_SECONDS is
# a real, separate interval var, so this guard is anchored on the deadline var
# specifically, not on the word "sleep" in general (the poll's own interval
# sleep must stay).
if rg --quiet --pcre2 'sleep\s+"?\$\{GATE_COLLECTOR_SETTLE_SECONDS\}"?\s*$' "${script}" "${collector_settle_lib}"; then
	fail "collector settle must be polled, not slept for a fixed duration"
fi
require_in "settle poll interval is a distinct var" "${collector_settle_lib}" \
	'sleep "${GATE_COLLECTOR_SETTLE_POLL_SECONDS}"'
# On timeout the failure must report what actually happened: the count reached,
# the threshold wanted, and how long it waited -- not just "failed". A poll that
# times out silently or with a vague message is strictly worse than the fixed
# sleep it replaced.
require_in "settle timeout reports elapsed duration" "${collector_settle_lib}" \
	"collector settle poll timed out after \${settle_elapsed}s (deadline \${GATE_COLLECTOR_SETTLE_SECONDS}s)"
# On success the observed settle duration must be logged: this is the margin
# data that was invisible under the old fixed sleep (nobody could tell whether a
# "green" run settled in 3s or 19s against the old 20s window).
require_in "settle success reports observed duration" "${collector_settle_lib}" \
	"cassette facts settled in %ss"

# Genuinely exercises wait_for_collector_settle (scripts/lib/golden-corpus-
# collector-settle.sh) against a mocked pg()/die(), mirroring the executable-
# test bar test-verify-golden-corpus-gate.sh already sets for
# host_tcp_port_open and the vulnerability-suppression lib: a text-only
# assertion cannot tell a working poll from a broken one (e.g. an inverted
# threshold comparison, or a deadline that never fires). Each case runs in a
# subshell under `if`, not a bare statement -- the mirror test's own
# `set -euo pipefail` would otherwise abort the whole test the moment Case A's
# deliberately-failing subshell returns non-zero, the same reason the
# host_tcp_port_open closed-port case is wrapped in `if`. A caller-side trap
# kills the fake collector background pid on every subshell exit path,
# including die()'s, so it never leaks.
collector_settle_case_log_dir="$(mktemp -d -t golden-corpus-collector-settle-case.XXXXXX)"
die() { printf 'verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

# Case A: the count never reaches the threshold. The poll must fail loudly
# (non-zero exit), bounded by the deadline, reporting the count actually
# reached and the threshold wanted -- not raise the threshold, soften the
# assertion, or retry past the deadline quietly.
collector_settle_case_a_err="${collector_settle_case_log_dir}/case-a.err"
if (
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-a")
	: >"${collector_settle_case_log_dir}/mirror-fake-a.log"
	pg() { printf '0'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_COLLECTOR_SETTLE_SECONDS=3
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_a_err}"; then
	fail "wait_for_collector_settle must exit non-zero when the threshold is never reached"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_a_err}" ||
	fail "wait_for_collector_settle timeout message must report the timeout"
rg --fixed-strings --quiet -- 'only 0 credentialed collector source(s) landed facts; want >= 2' \
	"${collector_settle_case_a_err}" ||
	fail "wait_for_collector_settle timeout message must report the count reached (0) and threshold (2)"

# Case B: the threshold is met on the very first poll. The function must
# return success EARLY -- well under a deadline it was given plenty of room
# to hit -- proving the fix actually gets faster in the common case rather
# than only failing correctly in the broken case.
collector_settle_case_b_out="${collector_settle_case_log_dir}/case-b.out"
if ! (
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-b")
	: >"${collector_settle_case_log_dir}/mirror-fake-b.log"
	pg() { printf '2'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_COLLECTOR_SETTLE_SECONDS=30
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	case_b_start="$(date +%s)"
	wait_for_collector_settle
	printf 'elapsed=%s\n' "$(( $(date +%s) - case_b_start ))"
) >"${collector_settle_case_b_out}" 2>&1; then
	fail "wait_for_collector_settle must exit zero when the threshold is met"
fi
rg --fixed-strings --quiet -- 'cassette facts settled in' "${collector_settle_case_b_out}" ||
	fail "wait_for_collector_settle must log the observed settle duration on success"
collector_settle_case_b_elapsed="$(sed -n 's/^elapsed=//p' "${collector_settle_case_b_out}")"
[[ -n "${collector_settle_case_b_elapsed}" ]] ||
	fail "test harness: case B did not report its own elapsed time"
[[ "${collector_settle_case_b_elapsed}" -lt 30 ]] ||
	fail "wait_for_collector_settle must return early on the normal path, not wait out a 30s deadline it never needed (took ${collector_settle_case_b_elapsed}s)"

# Case C: pg() returns a transient hiccup (empty string / error text) instead
# of a count. Polling queries Postgres far more often than the old one-shot
# check did, so a non-numeric response must be treated as "not yet landed",
# not corrupt the `(( collector_sources >= ... ))` arithmetic and abort the
# whole gate on a bash syntax error instead of the intended timeout message.
collector_settle_case_c_err="${collector_settle_case_log_dir}/case-c.err"
if (
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-c")
	: >"${collector_settle_case_log_dir}/mirror-fake-c.log"
	pg() { printf 'ERROR:  relation "ingestion_scopes" does not exist'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_c_err}"; then
	fail "wait_for_collector_settle must exit non-zero when pg() never returns a usable count"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_c_err}" ||
	fail "a non-numeric pg() response must still produce the deadline-timeout message, not a bash arithmetic syntax error"
rg --fixed-strings --quiet -- 'only 0 credentialed collector source(s) landed facts; want >= 2' \
	"${collector_settle_case_c_err}" ||
	fail "a non-numeric pg() response must be treated as 0 landed sources, not corrupt the arithmetic comparison"
if rg --fixed-strings --quiet -- 'syntax error' "${collector_settle_case_c_err}"; then
	fail "a non-numeric pg() response corrupted the collector_sources arithmetic comparison"
fi

rm -rf "${collector_settle_case_log_dir}"
unset -f die

collector_settle_cases_completed=1
