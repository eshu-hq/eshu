#!/usr/bin/env bash
# Collector settle poll cases for test-verify-golden-corpus-gate.sh.
#
# Sourced, never executed: it runs in the caller's shell (which already carries
# `set -euo pipefail` from test-verify-golden-corpus-gate.sh, load-bearing for
# Case F below) and uses the caller's ${script}, ${collector_settle_lib},
# require_in(), and fail(). Extracted so the mirror test stays under the
# 500-line cap, the same reason golden-corpus-phase-timing-cases.sh and
# golden-corpus-matcher-guard-cases.sh are separate lib chunks rather than
# inline in the mirror test.

# A collector that no-ops must not let the gate pass: liveness + facts-landed.
# Both checks live in the collector settle lib chunk, not the orchestrator body.
require_in "collector liveness check" "${collector_settle_lib}" "exited during settle"
# The stop condition must require BOTH every credentialed collector having
# landed at least one scope AND every scope of every cassette having landed --
# anchored as one ANDed condition, not two independent checks that could each
# `break` on their own. A cassette.Source.Next call emits one scope per call
# and a cassette carries 1-6 scopes; the distinct-source count alone is
# satisfied the moment every collector has committed its FIRST scope, well
# before any of them may have committed the rest. Breaking there and killing
# every collector pid would truncate the corpus for whichever collectors were
# still mid-replay, silently, since the gate would still report success -- a
# worse failure than the fixed sleep this lib replaced (that one at least
# failed loudly).
require_in "settle break condition requires BOTH source breadth AND full scope replay" \
	"${collector_settle_lib}" \
	"if (( collector_sources >= GATE_MIN_COLLECTOR_SOURCES && landed_scopes >= GATE_EXPECTED_TOTAL_SCOPES )); then"
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
	'sleep_seconds="${GATE_COLLECTOR_SETTLE_POLL_SECONDS}"'
# The poll sleep must be clamped to the remaining deadline, not the full poll
# interval unconditionally: an earlier version slept
# GATE_COLLECTOR_SETTLE_POLL_SECONDS regardless of how much deadline was left,
# so the loop could overshoot GATE_COLLECTOR_SETTLE_SECONDS by up to one whole
# interval -- e.g. a 3s deadline with a 10s poll interval reported "timed out
# after 10s (deadline 3s)", a message that lied about its own deadline in
# exactly the way the fixed sleep this lib replaced lied about settle time.
if rg --quiet --pcre2 '^\s*sleep\s+"\$\{GATE_COLLECTOR_SETTLE_POLL_SECONDS\}"\s*$' "${collector_settle_lib}"; then
	fail "collector settle poll sleep must be clamped to the remaining deadline (min(poll_interval, remaining)), not the full poll interval unconditionally"
fi
require_in "settle sleep clamped to remaining deadline" "${collector_settle_lib}" \
	"if (( remaining_seconds < sleep_seconds )); then"
# On timeout the failure must report what actually happened: both counts
# reached, both thresholds wanted, and how long it waited -- not just "failed".
require_in "settle timeout reports elapsed duration" "${collector_settle_lib}" \
	"collector settle poll timed out after \${settle_elapsed}s (deadline \${GATE_COLLECTOR_SETTLE_SECONDS}s)"
# On success the observed settle duration must be logged: this is the margin
# data that was invisible under the old fixed sleep (nobody could tell whether a
# "green" run settled in 3s or 19s against the old 20s window).
require_in "settle success reports observed duration" "${collector_settle_lib}" \
	"cassette facts settled in %ss"
# A bare `settle_probe_line="$(pg ...)"` assignment aborts the whole gate under
# set -e the instant a transient docker-exec/psql hiccup makes pg() exit
# non-zero -- before the non-numeric fallback or the deadline ever run. Case F
# below exercises this at runtime; this pins the source-level guard too, since
# a text-only assertion cannot tell a working guard from one a later edit
# silently dropped from the same-line `||` back onto its own line (which would
# reopen the gap while still parsing).
require_in "pg() probe failure is guarded on the same line (set -e safety)" \
	"${collector_settle_lib}" \
	'")" || settle_probe_line=""'

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
# including die()'s, so it never leaks. Every case sets both
# GATE_MIN_COLLECTOR_SOURCES=2 and GATE_EXPECTED_TOTAL_SCOPES=5 unless the case
# is specifically testing one of those thresholds, so pg()'s two-field mock
# output ("<distinct sources> <total scopes>") stays consistent and readable
# across cases.
collector_settle_case_log_dir="$(mktemp -d -t golden-corpus-collector-settle-case.XXXXXX)"
die() { printf 'verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

# Case A: neither count ever reaches its threshold. The poll must fail loudly
# (non-zero exit), bounded by the deadline, reporting both counts actually
# reached and both thresholds wanted -- not raise a threshold, soften the
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
	pg() { printf '0 0'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_EXPECTED_TOTAL_SCOPES=5
	GATE_COLLECTOR_SETTLE_SECONDS=3
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_a_err}"; then
	fail "wait_for_collector_settle must exit non-zero when neither threshold is ever reached"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_a_err}" ||
	fail "wait_for_collector_settle timeout message must report the timeout"
rg --fixed-strings --quiet -- '0 credentialed collector source(s) landed facts (want >= 2), 0 total scopes landed (want >= 5)' \
	"${collector_settle_case_a_err}" ||
	fail "wait_for_collector_settle timeout message must report both counts reached (0, 0) and both thresholds (2, 5)"

# Case B: both thresholds are met on the very first poll. The function must
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
	pg() { printf '2 5'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_EXPECTED_TOTAL_SCOPES=5
	GATE_COLLECTOR_SETTLE_SECONDS=30
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	case_b_start="$(date +%s)"
	wait_for_collector_settle
	printf 'elapsed=%s\n' "$(( $(date +%s) - case_b_start ))"
) >"${collector_settle_case_b_out}" 2>&1; then
	fail "wait_for_collector_settle must exit zero when both thresholds are met"
fi
rg --fixed-strings --quiet -- 'cassette facts settled in' "${collector_settle_case_b_out}" ||
	fail "wait_for_collector_settle must log the observed settle duration on success"
collector_settle_case_b_elapsed="$(sed -n 's/^elapsed=//p' "${collector_settle_case_b_out}")"
[[ -n "${collector_settle_case_b_elapsed}" ]] ||
	fail "test harness: case B did not report its own elapsed time"
[[ "${collector_settle_case_b_elapsed}" -lt 30 ]] ||
	fail "wait_for_collector_settle must return early on the normal path, not wait out a 30s deadline it never needed (took ${collector_settle_case_b_elapsed}s)"

# Case C: pg() returns a transient hiccup as text (empty string / error line)
# while still exiting zero. Polling queries Postgres far more often than the
# old one-shot check did, so a non-numeric response must be treated as
# "not yet landed" for BOTH fields, not corrupt the arithmetic comparison and
# abort the whole gate on a bash syntax error instead of the intended timeout
# message.
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
	GATE_EXPECTED_TOTAL_SCOPES=5
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_c_err}"; then
	fail "wait_for_collector_settle must exit non-zero when pg() never returns a usable pair of counts"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_c_err}" ||
	fail "a non-numeric pg() response must still produce the deadline-timeout message, not a bash arithmetic syntax error"
rg --fixed-strings --quiet -- '0 credentialed collector source(s) landed facts (want >= 2), 0 total scopes landed (want >= 5)' \
	"${collector_settle_case_c_err}" ||
	fail "a non-numeric pg() response must be treated as 0 for both counts, not corrupt the arithmetic comparison"
if rg --fixed-strings --quiet -- 'syntax error' "${collector_settle_case_c_err}"; then
	fail "a non-numeric pg() response corrupted the arithmetic comparison"
fi

# Case D: the poll interval is deliberately larger than the deadline (10s
# poll, 3s deadline). Proves the sleep is clamped to the remaining deadline
# rather than slept unconditionally: the pre-fix version would sleep the full
# 10s before its next deadline check and report "timed out after ~10-13s
# (deadline 3s)" -- a message that lies about its own deadline. Timed from
# OUTSIDE the subshell: wait_for_collector_settle's die() calls exit and never
# returns control to a caller-side timer placed after the call.
collector_settle_case_d_err="${collector_settle_case_log_dir}/case-d.err"
collector_settle_case_d_start="$(date +%s)"
if (
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-d")
	: >"${collector_settle_case_log_dir}/mirror-fake-d.log"
	pg() { printf '0 0'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_EXPECTED_TOTAL_SCOPES=5
	GATE_COLLECTOR_SETTLE_SECONDS=3
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=10
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_d_err}"; then
	fail "wait_for_collector_settle must exit non-zero when neither threshold is reached (case D)"
fi
collector_settle_case_d_elapsed=$(( $(date +%s) - collector_settle_case_d_start ))
rg --fixed-strings --quiet -- 'collector settle poll timed out after 3s (deadline 3s)' \
	"${collector_settle_case_d_err}" ||
	fail "wait_for_collector_settle with a 10s poll interval and a 3s deadline must report timing out at 3s, not overshoot to ~10s: $(cat "${collector_settle_case_d_err}")"
[[ "${collector_settle_case_d_elapsed}" -le 5 ]] ||
	fail "wait_for_collector_settle must honor the 3s deadline despite a 10s poll interval, not wait out the full interval (measured ${collector_settle_case_d_elapsed}s wall-clock)"

# Case E (P1 regression, codex review on #5909): the distinct-source-count
# threshold is met immediately, but total landed scopes stays short of the
# expected total forever -- simulating a collector that committed its FIRST
# scope (satisfying GATE_MIN_COLLECTOR_SOURCES) while cassette.Source.Next
# still has more scopes queued for that same collector. The function must NOT
# declare success here: breaking on the distinct-source count alone and
# killing every collector pid would truncate the corpus for whichever
# collector was still mid-replay. Reproduced against the pre-fix HEAD (single-
# value pg() query, source-count-only break condition) before this case was
# written: that version returned exit 0 immediately, declaring settle complete
# while only 2 of 5 total scopes had landed.
collector_settle_case_e_err="${collector_settle_case_log_dir}/case-e.err"
if (
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-e")
	: >"${collector_settle_case_log_dir}/mirror-fake-e.log"
	# 2 distinct sources meets GATE_MIN_COLLECTOR_SOURCES=2, but total landed
	# scopes is stuck at 2 -- short of GATE_EXPECTED_TOTAL_SCOPES=5 -- on every
	# poll, simulating a collector still mid-replay of its remaining scopes.
	pg() { printf '2 2'; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_EXPECTED_TOTAL_SCOPES=5
	GATE_COLLECTOR_SETTLE_SECONDS=3
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_e_err}"; then
	fail "wait_for_collector_settle must NOT declare success while total scopes (2) is short of the expected total (5), even though the distinct-source threshold (2) is already met -- this is the #5909 P1 truncation regression"
fi
rg --fixed-strings --quiet -- '2 credentialed collector source(s) landed facts (want >= 2), 2 total scopes landed (want >= 5)' \
	"${collector_settle_case_e_err}" ||
	fail "case E timeout message must show the source threshold met (2/2) alongside the scope total still short (2/5)"

# Case F (P2, codex review on #5909): pg() itself fails (non-zero exit), the
# probe-connection-drop case rather than case C's garbled-but-successful text.
# Polling queries Postgres far more often than a one-shot check would, so a
# transient docker-exec/psql failure is a real possibility mid-poll. The
# assertion below is deliberately NOT just "exit code is non-zero": under
# set -e (inherited from test-verify-golden-corpus-gate.sh, which this cases
# file is sourced into), an unguarded `settle_probe_line="$(pg ...)"`
# assignment would abort the subshell immediately on pg()'s failure too --
# also non-zero, but silently, with none of wait_for_collector_settle's own
# reporting. Requiring the specific timeout message on stderr is what proves
# execution survived the failing probe and reached the deadline logic, rather
# than merely proving something, anything, made the subshell exit non-zero.
collector_settle_case_f_err="${collector_settle_case_log_dir}/case-f.err"
if (
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-f")
	: >"${collector_settle_case_log_dir}/mirror-fake-f.log"
	pg() { return 1; }
	GATE_MIN_COLLECTOR_SOURCES=2
	GATE_EXPECTED_TOTAL_SCOPES=5
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	wait_for_collector_settle
) >/dev/null 2>"${collector_settle_case_f_err}"; then
	fail "wait_for_collector_settle must exit non-zero when pg() itself fails"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_f_err}" ||
	fail "a failing pg() probe must survive to wait_for_collector_settle's own timeout message under set -e, not abort the subshell silently on the first failed probe: $(cat "${collector_settle_case_f_err}")"
rg --fixed-strings --quiet -- '0 credentialed collector source(s) landed facts (want >= 2), 0 total scopes landed (want >= 5)' \
	"${collector_settle_case_f_err}" ||
	fail "a failing pg() probe must be treated as 0 for both counts and keep polling until the deadline, not corrupt state"

rm -rf "${collector_settle_case_log_dir}"
unset -f die

collector_settle_cases_completed=1
