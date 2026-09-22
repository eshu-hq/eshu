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
# The stop condition is a subset assertion over the launched (scope_id,
# generation_id) pairs, not a count. Two count thresholds (distinct sources >=
# N, generations >= M) were each satisfiable with a collector still mid-commit:
# migration 115 seeds an eshu:global scope and generation on every bootstrap,
# so the population always carried one more of each than the launched set, and
# the kill after the break rolled back the in-flight commit (#6965). Case G
# below plants exactly that population.
require_in "settle break requires every launched pair, echoed back by the probe" \
	"${collector_settle_lib}" \
	"if (( probed_total == launched_total && present_pairs == launched_total )); then"
if rg --quiet --fixed-strings -- 'GATE_MIN_COLLECTOR_SOURCES' "${collector_settle_lib}"; then
	fail "collector settle must not break on a distinct-source count threshold (#6965)"
fi
require_in "settle probe joins launched pairs to scope_generations" \
	"${collector_settle_lib}" \
	"LEFT JOIN scope_generations AS landed"
require_in "settle probe matches on generation_id, not just scope" \
	"${collector_settle_lib}" \
	"AND landed.generation_id = launched.generation_id"
require_in "settle probe counts collector-committed pending rows" \
	"${collector_settle_lib}" \
	"AND landed.status IN ('pending', 'active', 'completed', 'superseded');"
# The settle window must be POLLED, not slept for a fixed duration: a fixed
# `sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"` has zero margin once host load or
# Docker I/O contention slows down fact commit. GATE_COLLECTOR_SETTLE_POLL_SECONDS
# is a real, separate interval var, so this guard is anchored on the deadline
# var specifically, not on the word "sleep" in general.
if rg --quiet --pcre2 'sleep\s+"?\$\{GATE_COLLECTOR_SETTLE_SECONDS\}"?\s*$' "${script}" "${collector_settle_lib}"; then
	fail "collector settle must be polled, not slept for a fixed duration"
fi
require_in "settle poll interval is a distinct var" "${collector_settle_lib}" \
	'sleep_seconds="${GATE_COLLECTOR_SETTLE_POLL_SECONDS}"'
# The poll sleep must be clamped to the remaining deadline, or the loop can
# overshoot GATE_COLLECTOR_SETTLE_SECONDS by a whole interval and report a
# deadline it did not honour.
if rg --quiet --pcre2 '^\s*sleep\s+"\$\{GATE_COLLECTOR_SETTLE_POLL_SECONDS\}"\s*$' "${collector_settle_lib}"; then
	fail "collector settle poll sleep must be clamped to the remaining deadline (min(poll_interval, remaining)), not the full poll interval unconditionally"
fi
require_in "settle sleep clamped to remaining deadline" "${collector_settle_lib}" \
	"if (( remaining_seconds < sleep_seconds )); then"
require_in "settle timeout reports elapsed duration" "${collector_settle_lib}" \
	"collector settle poll timed out after \${settle_elapsed}s (deadline \${GATE_COLLECTOR_SETTLE_SECONDS}s)"
require_in "settle success reports observed duration" "${collector_settle_lib}" \
	"cassette facts settled in %ss"
# A bare `settle_probe_line="$(pg ...)"` assignment aborts the whole gate under
# set -e the instant a transient docker-exec/psql hiccup makes pg() exit
# non-zero. Case F exercises this at runtime; this pins the same-line guard.
require_in "pg() probe failure is guarded on the same line (set -e safety)" \
	"${collector_settle_lib}" \
	'")" || settle_probe_line=""'

# Genuinely exercises wait_for_collector_settle against a mocked pg()/die(): a
# text-only assertion cannot tell a working poll from a broken one. Each case
# runs in a subshell under `if`, because the mirror test's own `set -euo
# pipefail` would otherwise abort on a deliberately failing case. The case sets
# pg(), GATE_EXPECTED_SCOPE_PAIRS and the two timing vars, then calls
# collector_settle_run_case, whose EXIT trap kills the fake collector on every
# path including die()'s. Mocked probe answers use the lib's one-row shape:
# "<present> <launched> <missing,...>".
collector_settle_case_log_dir="$(mktemp -d -t golden-corpus-collector-settle-case.XXXXXX)"
die() { printf 'verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }
collector_settle_two_pairs=("scope-1"$'\t'"gen-1" "scope-2"$'\t'"gen-2")

collector_settle_run_case() {
	local name="$1"
	log_dir="${collector_settle_case_log_dir}"
	# shellcheck source=scripts/lib/golden-corpus-collector-settle.sh
	. "${collector_settle_lib}"
	collector_pids=()
	collector_names=()
	trap 'for p in "${collector_pids[@]:-}"; do kill "$p" >/dev/null 2>&1 || true; done' EXIT
	sleep 20 &
	collector_pids+=("$!")
	collector_names+=("mirror-fake-${name}")
	: >"${collector_settle_case_log_dir}/mirror-fake-${name}.log"
	wait_for_collector_settle
}

# Case A: no launched pair ever lands. The poll must fail loudly, bounded by
# the deadline, naming every missing pair.
collector_settle_case_a_err="${collector_settle_case_log_dir}/case-a.err"
if (
	pg() { printf '0 2 scope-1/gen-1,scope-2/gen-2'; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=3
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case a
) >/dev/null 2>"${collector_settle_case_a_err}"; then
	fail "wait_for_collector_settle must exit non-zero when no launched pair ever lands"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_a_err}" ||
	fail "wait_for_collector_settle timeout message must report the timeout"
rg --fixed-strings --quiet -- '0 of 2 launched scope generations landed; missing: scope-1/gen-1,scope-2/gen-2' \
	"${collector_settle_case_a_err}" ||
	fail "wait_for_collector_settle timeout message must report the landed count and name every missing pair"

# Case B: every launched pair is present on the first poll. The function must
# return success EARLY, well under a deadline it had plenty of room to hit.
collector_settle_case_b_out="${collector_settle_case_log_dir}/case-b.out"
if ! (
	pg() { printf '2 2 '; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=30
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	case_b_start="$(date +%s)"
	collector_settle_run_case b
	printf 'elapsed=%s\n' "$(( $(date +%s) - case_b_start ))"
) >"${collector_settle_case_b_out}" 2>&1; then
	fail "wait_for_collector_settle must exit zero when every launched pair has landed"
fi
rg --fixed-strings --quiet -- 'cassette facts settled in' "${collector_settle_case_b_out}" ||
	fail "wait_for_collector_settle must log the observed settle duration on success"
rg --fixed-strings --quiet -- 'all 2 launched scope generations landed' "${collector_settle_case_b_out}" ||
	fail "wait_for_collector_settle must report the launched-pair count on success"
collector_settle_case_b_elapsed="$(sed -n 's/^elapsed=//p' "${collector_settle_case_b_out}")"
[[ -n "${collector_settle_case_b_elapsed}" ]] ||
	fail "test harness: case B did not report its own elapsed time"
[[ "${collector_settle_case_b_elapsed}" -lt 30 ]] ||
	fail "wait_for_collector_settle must return early on the normal path, not wait out a 30s deadline it never needed (took ${collector_settle_case_b_elapsed}s)"

# Case C: pg() returns a transient error as text while exiting zero. A
# non-numeric answer must read as "nothing landed", never as "nothing missing",
# and must not corrupt the arithmetic into a bash syntax error.
collector_settle_case_c_err="${collector_settle_case_log_dir}/case-c.err"
if (
	pg() { printf 'ERROR:  relation "scope_generations" does not exist'; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case c
) >/dev/null 2>"${collector_settle_case_c_err}"; then
	fail "wait_for_collector_settle must exit non-zero when pg() never returns a usable answer"
fi
rg --fixed-strings --quiet -- '0 of 2 launched scope generations landed; missing: (probe returned no usable answer)' \
	"${collector_settle_case_c_err}" ||
	fail "a non-numeric pg() response must be treated as nothing landed and say so: $(cat "${collector_settle_case_c_err}")"
if rg --fixed-strings --quiet -- 'syntax error' "${collector_settle_case_c_err}"; then
	fail "a non-numeric pg() response corrupted the arithmetic comparison"
fi

# Case D: the poll interval (10s) is larger than the deadline (3s). The sleep
# must be clamped to the remaining deadline. Timed from OUTSIDE the subshell:
# die() calls exit and never returns control to a timer after the call.
collector_settle_case_d_err="${collector_settle_case_log_dir}/case-d.err"
collector_settle_case_d_start="$(date +%s)"
if (
	pg() { printf '0 2 scope-1/gen-1,scope-2/gen-2'; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=3
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=10
	collector_settle_run_case d
) >/dev/null 2>"${collector_settle_case_d_err}"; then
	fail "wait_for_collector_settle must exit non-zero when no launched pair lands (case D)"
fi
collector_settle_case_d_elapsed=$(( $(date +%s) - collector_settle_case_d_start ))
rg --pcre2 --quiet -- 'collector settle poll timed out after [3-5]s \(deadline 3s\)' \
	"${collector_settle_case_d_err}" ||
	fail "wait_for_collector_settle with a 10s poll interval and a 3s deadline must report timing out within the accepted 3-5s scheduler margin, not overshoot to ~10s: $(cat "${collector_settle_case_d_err}")"
[[ "${collector_settle_case_d_elapsed}" -le 5 ]] ||
	fail "wait_for_collector_settle must honor the 3s deadline despite a 10s poll interval, not wait out the full interval (measured ${collector_settle_case_d_elapsed}s wall-clock)"

# Case E: one collector committed and the other has not. The poll must keep
# waiting and name exactly the pair still in flight.
collector_settle_case_e_err="${collector_settle_case_log_dir}/case-e.err"
if (
	pg() { printf '1 2 scope-2/gen-2'; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case e
) >/dev/null 2>"${collector_settle_case_e_err}"; then
	fail "wait_for_collector_settle must NOT settle while one launched pair is still missing"
fi
rg --fixed-strings --quiet -- '1 of 2 launched scope generations landed; missing: scope-2/gen-2' \
	"${collector_settle_case_e_err}" ||
	fail "case E timeout message must name the one pair still in flight: $(cat "${collector_settle_case_e_err}")"

# Case E2: every launched pair reads as present, but the probe joined a
# different row set (three rows for two launched pairs: a duplicated VALUES row
# can cover for a missing one). present == launched holds, so only the echoed
# launched count stops this from reading as settled. A short answer such as
# "1 1" would not test that guard: present != launched already refuses it.
if (
	pg() { printf '2 3 '; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case e2
) >/dev/null 2>&1; then
	fail "wait_for_collector_settle must NOT settle when the probe's echoed launched count (3) differs from the launched set (2), even with present == launched"
fi

# Case F: pg() itself fails (non-zero exit). Requiring the timeout message
# proves execution survived the failing probe under set -e and reached the
# deadline logic, rather than merely proving something made the subshell exit.
collector_settle_case_f_err="${collector_settle_case_log_dir}/case-f.err"
if (
	pg() { return 1; }
	GATE_EXPECTED_SCOPE_PAIRS=("${collector_settle_two_pairs[@]}")
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case f
) >/dev/null 2>"${collector_settle_case_f_err}"; then
	fail "wait_for_collector_settle must exit non-zero when pg() itself fails"
fi
rg --fixed-strings --quiet -- 'collector settle poll timed out after' "${collector_settle_case_f_err}" ||
	fail "a failing pg() probe must survive to wait_for_collector_settle's own timeout message under set -e: $(cat "${collector_settle_case_f_err}")"

# shellcheck source=scripts/lib/golden-corpus-collector-settle-population-case.sh disable=SC2154  # repo_root is parent-owned.
. "${repo_root}/scripts/lib/golden-corpus-collector-settle-population-case.sh"

# Case H: an empty launched set would make the subset assertion vacuously
# true. It must die before polling.
collector_settle_case_h_err="${collector_settle_case_log_dir}/case-h.err"
if (
	pg() { printf '0 0 '; }
	GATE_EXPECTED_SCOPE_PAIRS=()
	GATE_COLLECTOR_SETTLE_SECONDS=2
	GATE_COLLECTOR_SETTLE_POLL_SECONDS=1
	collector_settle_run_case h
) >/dev/null 2>"${collector_settle_case_h_err}"; then
	fail "wait_for_collector_settle must refuse an empty launched set, not pass vacuously"
fi
rg --fixed-strings --quiet -- 'no launched (scope_id, generation_id) pairs' "${collector_settle_case_h_err}" ||
	fail "case H must die with the empty-launched-set message: $(cat "${collector_settle_case_h_err}")"

rm -rf "${collector_settle_case_log_dir}"
unset -f die collector_settle_run_case

collector_settle_cases_completed=1
