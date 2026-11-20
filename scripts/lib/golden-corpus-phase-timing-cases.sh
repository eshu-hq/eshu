#!/usr/bin/env bash
# B-11 per-phase timing cases for test-verify-golden-corpus-gate.sh (#5837).
#
# Sourced, never executed: it runs in the caller's shell and uses the caller's
# ${timing_lib}, fail(), require() and require_lib(). Extracted so the mirror
# test stays under the 500-line cap.
#
# The `golden-corpus-*` name is deliberate: the trigger lists already glob
# scripts/lib/golden-corpus-*.sh, so this file is gated without registry work.
#
# What these cases defend
# -----------------------
# phase_graph_query is stamped from pipeline_end to a mark taken inside
# emit_phase_timings_and_flags, and testdata/golden/e2e-baseline.json scopes it
# to eshu-api plus eshu-mcp-server startup -- it "excludes the gate's own
# assertion time". #5465's suppression producer proof
# (golden_suppression_verify_producer_truth) runs BETWEEN those two stamps, and
# it floors itself at its expiry waits (#7740): golden_suppression_prepare_expiry_payload
# sets golden_suppression_expiry_epoch = now + 20, the hidden leg arms a second
# window, and golden_suppression_wait_for_expiry then blocks until each deadline,
# so the block cannot finish faster however quick the host is.
#
# Unbracketed, those waits were billed to pipeline startup, against a 3s baseline
# and an 8s effective ceiling (baseline_seconds=3 + absolute_slack_seconds=5,
# testdata/golden/e2e-baseline.json). That is advisory on shared CI only because
# the lane defaults GATE_PHASE_REGRESSION_ADVISORY=true; a controlled host
# running it blocking fails on it, with nothing in the pipeline actually slower.
#
# No measurement is needed to prove that: any wait > 8 by construction, on any
# host and any corpus. The "~4s -> ~23s" figure quoted around this change is an
# illustrative local reading with no committed phase-timings.json, host, or
# manifest behind it -- see (#5837),
# "Golden-gate phase-timing note", for the source-only proof and for what would
# have to be captured to make that figure evidence.
#
# The fix is arithmetic, not a wider number: the orchestrator brackets the proof
# with phase_graph_query_excluded_starts/_ends (bash arrays, one bracket per
# matching pair of entries -- see golden-corpus-phase-timings.sh for why arrays
# rather than a single scalar pair) and the timing lib subtracts the sum of
# every bracket's span. Do NOT "fix" a future timing warning here by widening
# the baseline or deleting a bracket -- append a new bracket for the new
# in-window block instead.
#
# D13: the suppression producer proof queried the live in-tree API, so the
# proof AND its exclusion bracket leave the orchestrator together (serving
# truth moves to the cutover lane). The timing lib defaults empty brackets
# sane, the graph_query window still measures API + MCP startup, and the
# behavioural arithmetic cases below keep pinning the lib. The structural
# bracket checks and the region/placement guards leave with the bracket.

# require_lib: require_in against the phase-timing lib. The non-comment
# anchoring matters here too — that lib's own header prose names
# phase-timings.json, so a whole-file match is satisfied with no emission code.
require_lib() {
	require_in "$1" "${timing_lib}" "$2"
}
# Anchored on the ASSIGNMENT, not the bare filename: the else-branch log message
# ("emitted phase-timings.json for seeding") is a second non-comment home, so a
# bare-filename needle stays green when the emission target is renamed or the
# whole emission block is replaced.
# shellcheck disable=SC2016  # the needle is the literal lib source line
require_lib "phase-timings emission" 'phase_timings_file="${log_dir}/phase-timings.json"'
require_lib "phase baseline default" "e2e-baseline.json"
require_lib "per-phase gate flag" "-phase-timings-file="

# ---------------------------------------------------------------------------
# Structural: the lib subtracts the excluded span (the orchestrator bracket
# left with the D13 suppression-proof removal; the lib default covers the
# empty case, and the behavioural cases below keep proving the arithmetic).
# ---------------------------------------------------------------------------
# shellcheck disable=SC2016
require_lib "graph_query window is measured" "phase_graph_query_window=\$(( phase_graph_query_end - phase_graph_query_start ))"
require_lib "graph_query subtracts the excluded span" \
	"phase_graph_query_window - phase_graph_query_excluded"

# ---------------------------------------------------------------------------
# Behavioural: a 23s window holding a 20s bracketed span must emit
# graph_query=3, not 23.
#
# The structural needles above are satisfied by the right lines existing; only
# executing the helper shows the arithmetic is actually applied. Reverting the
# subtraction makes this emit 23.
# ---------------------------------------------------------------------------
phase_timing_behavior_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_behavior_dir}"
	# An absolute path that does not exist, so the helper takes its seeding
	# branch and emits phase-timings.json without needing the gate binary.
	# Deliberately not shadowing repo_root: the absent absolute path already
	# misses both arms of the helper's baseline test.
	GATE_PHASE_BASELINE="${phase_timing_behavior_dir}/absent-baseline.json"
	log() { :; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 23 ))
	phase_graph_query_excluded_starts=("$(( now - 21 ))")
	phase_graph_query_excluded_ends=("$(( now - 1 ))")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	emit_phase_timings_and_flags
	observed="$(jq -r '.phases.graph_query' "${phase_timings_file}")"
	# The end stamp is taken inside the helper, so allow one second of slack.
	(( observed >= 3 && observed <= 4 )) || {
		printf 'graph_query must exclude the bracketed span: got %s, want 3 (23s window - 20s excluded)\n' \
			"${observed}" >&2
		exit 1
	}
) || fail "phase-timing lib does not subtract the bracketed excluded span (#5837)"
rm -rf "${phase_timing_behavior_dir}"

# ---------------------------------------------------------------------------
# #5837 round-8 review: a SECOND exclusion bracket must ACCUMULATE with the
# first, not silently discard it. The predecessor interface
# (phase_graph_query_excluded_start/_end, two scalar variables) could not
# express two brackets at all: re-assigning the same two names for a second
# bracket just overwrote the first one's stamps, so only the LAST bracket's
# span was ever subtracted -- an under-subtraction with every gate green and
# nothing to catch it.
#
# Two non-overlapping brackets inside one 40s window, 3s and 4s wide: summing
# gives 7s excluded (window - excluded = 33). Discarding the first bracket
# (the pre-accumulator bug) would instead subtract only 4s (window - excluded
# = 36); discarding the second would subtract only 3s (= 37). Both wrong
# answers are far enough from 33 that host-clock slack cannot hide the bug.
# ---------------------------------------------------------------------------
phase_timing_two_brackets_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_two_brackets_dir}"
	GATE_PHASE_BASELINE="${phase_timing_two_brackets_dir}/absent-baseline.json"
	log() { :; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 40 ))
	# Bracket 1: [now-35, now-32], a 3s span.
	# Bracket 2: [now-20, now-16], a 4s span.
	phase_graph_query_excluded_starts=("$(( now - 35 ))" "$(( now - 20 ))")
	phase_graph_query_excluded_ends=("$(( now - 32 ))" "$(( now - 16 ))")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	emit_phase_timings_and_flags
	observed="$(jq -r '.phases.graph_query' "${phase_timings_file}")"
	# The end stamp is taken inside the helper, so allow one second of slack.
	(( observed >= 33 && observed <= 34 )) || {
		printf 'two brackets must accumulate (3s + 4s = 7s excluded from a 40s window): got %s, want 33\n' \
			"${observed}" >&2
		exit 1
	}
) || fail "phase-timing lib does not accumulate two exclusion brackets (#5837)"
rm -rf "${phase_timing_two_brackets_dir}"

# An unbracketed span must NOT be subtracted away into a negative or absurd
# phase: a caller that brackets nothing still emits the plain window. This is
# what keeps the ${:-0} defaults honest rather than merely present.
phase_timing_unbracketed_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_unbracketed_dir}"
	GATE_PHASE_BASELINE="${phase_timing_unbracketed_dir}/absent-baseline.json"
	log() { :; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 7 ))
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	emit_phase_timings_and_flags
	observed="$(jq -r '.phases.graph_query' "${phase_timings_file}")"
	(( observed >= 7 && observed <= 8 )) || {
		printf 'unbracketed graph_query must emit the plain window: got %s, want 7\n' \
			"${observed}" >&2
		exit 1
	}
) || fail "phase-timing lib mishandles an unbracketed graph_query window (#5837)"
rm -rf "${phase_timing_unbracketed_dir}"

# A zero-width bracket (both markers explicitly SET, to the same instant) must
# be distinguished from the unbracketed case above: both being set must not
# trip the half-set guard below, and an excluded span of exactly 0 is a
# legitimate accounting state, not a die. This is the "zero" half of
# "zero-vs-unset must keep working".
phase_timing_zero_width_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_zero_width_dir}"
	GATE_PHASE_BASELINE="${phase_timing_zero_width_dir}/absent-baseline.json"
	log() { :; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 7 ))
	phase_graph_query_excluded_starts=("${now}")
	phase_graph_query_excluded_ends=("${now}")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	emit_phase_timings_and_flags
	observed="$(jq -r '.phases.graph_query' "${phase_timings_file}")"
	(( observed >= 7 && observed <= 8 )) || {
		printf 'a zero-width (both-set, equal) bracket must not change the plain window: got %s, want 7\n' \
			"${observed}" >&2
		exit 1
	}
) || fail "phase-timing lib mishandles a zero-width exclusion bracket with both markers set (#5837)"
rm -rf "${phase_timing_zero_width_dir}"

# ---------------------------------------------------------------------------
# #5837 P2 review: an impossible accounting state must be a hard failure, not
# a silently clamped or negative phase duration.
# go/cmd/golden-corpus-gate/timing.go only asserts `observedSecs <= ceiling`
# (an upper bound), so a negative duration used to pass a gated, non-advisory
# check with nobody looking. Reproduced pre-fix: over-exclusion emitted
# graph_query=-7, and an _end stamp set with _start deleted emitted roughly
# -1.7e9 (the unset epoch defaulted to 0, so the subtraction read as "excluded
# from the Unix epoch"). Both die now instead of emitting a number.
# ---------------------------------------------------------------------------

# Over-exclusion: the excluded span cannot exceed the window it claims to
# correct. A 3s window holding a 10s bracketed span used to silently emit -7.
phase_timing_over_exclusion_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_over_exclusion_dir}"
	GATE_PHASE_BASELINE="${phase_timing_over_exclusion_dir}/absent-baseline.json"
	log() { :; }
	die() { printf 'DIE: %s\n' "$*" >&2; exit 1; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 3 ))
	phase_graph_query_excluded_starts=("$(( now - 10 ))")
	phase_graph_query_excluded_ends=("${now}")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	die_output="$(emit_phase_timings_and_flags 2>&1)" && {
		printf 'over-exclusion must die, not silently emit a negative phase duration\n' >&2
		exit 1
	}
	printf '%s\n' "${die_output}" | rg --fixed-strings -- 'does not fit inside' >/dev/null || {
		printf 'over-exclusion die message missing expected diagnostic: %s\n' "${die_output}" >&2
		exit 1
	}
) || fail "phase-timing lib does not fail on an excluded span wider than its window (#5837)"
rm -rf "${phase_timing_over_exclusion_dir}"

# Negative exclusion: one bracket's own end stamp precedes its own start stamp
# (a caller-side bracket bug, not an over-wide one), producing a NEGATIVE
# bracket span that is still numerically inside the window -- so it cannot be
# caught by the over-exclusion (`excluded > window`) aggregate check alone. A
# 10s window with a -5s bracket used to silently emit window - excluded =
# 10 - (-5) = 15, an inflated phase duration LONGER than the window that
# supposedly contains it, with nothing to catch it because
# go/cmd/golden-corpus-gate/timing.go only asserts an upper bound. This case
# is the round-8 review regression guard for the per-bracket
# `bracket_span < 0` check specifically: deleting just that check (leaving the
# aggregate `phase_graph_query_excluded > phase_graph_query_window` check
# alone) does not change the over-exclusion case above, since -5 is not
# greater than 10, but it lets this case fall through and emit 15.
phase_timing_negative_exclusion_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_negative_exclusion_dir}"
	GATE_PHASE_BASELINE="${phase_timing_negative_exclusion_dir}/absent-baseline.json"
	log() { :; }
	die() { printf 'DIE: %s\n' "$*" >&2; exit 1; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 10 ))
	phase_graph_query_excluded_starts=("${now}")
	phase_graph_query_excluded_ends=("$(( now - 5 ))")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	die_output="$(emit_phase_timings_and_flags 2>&1)" && {
		printf 'negative exclusion (end before start) must die, not silently emit an inflated phase duration\n' >&2
		exit 1
	}
	printf '%s\n' "${die_output}" | rg --fixed-strings -- 'has a negative span' >/dev/null || {
		printf 'negative exclusion die message missing expected diagnostic: %s\n' "${die_output}" >&2
		exit 1
	}
) || fail "phase-timing lib does not fail on a negative excluded span (end stamp before start stamp) (#5837)"
rm -rf "${phase_timing_negative_exclusion_dir}"

# Unequal-length arrays, starts short: only phase_graph_query_excluded_ends
# has an entry. An unequal count means some bracket in this run opened
# without closing, or closed without opening -- the array-based analogue of
# the old scalar interface's half-set pair.
phase_timing_half_set_start_missing_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_half_set_start_missing_dir}"
	GATE_PHASE_BASELINE="${phase_timing_half_set_start_missing_dir}/absent-baseline.json"
	log() { :; }
	die() { printf 'DIE: %s\n' "$*" >&2; exit 1; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 7 ))
	phase_graph_query_excluded_ends=("${now}")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	die_output="$(emit_phase_timings_and_flags 2>&1)" && {
		printf 'unequal-length exclusion arrays (starts short) must die, not silently treat the missing entry as absent\n' >&2
		exit 1
	}
	printf '%s\n' "${die_output}" | rg --fixed-strings -- 'have different lengths' >/dev/null || {
		printf 'unequal-length (starts short) die message missing expected diagnostic: %s\n' "${die_output}" >&2
		exit 1
	}
) || fail "phase-timing lib does not fail when phase_graph_query_excluded_starts is shorter than _ends (#5837)"
rm -rf "${phase_timing_half_set_start_missing_dir}"

# Unequal-length arrays, ends short: the reproduced #5837 review case -- only
# phase_graph_query_excluded_starts has an entry (e.g. an _end append deleted
# by a later edit), the array-based analogue of the scalar interface's
# half-set pair that used to read the missing end as epoch 0.
phase_timing_half_set_end_missing_dir="$(mktemp -d)"
(
	log_dir="${phase_timing_half_set_end_missing_dir}"
	GATE_PHASE_BASELINE="${phase_timing_half_set_end_missing_dir}/absent-baseline.json"
	log() { :; }
	die() { printf 'DIE: %s\n' "$*" >&2; exit 1; }
	now="$(date +%s)"
	phase_bootstrap_start="${now}"; phase_bootstrap_end="${now}"
	phase_collect_start="${now}"; phase_collect_end="${now}"
	phase_first_drain_start="${now}"; phase_first_drain_end="${now}"
	phase_maintenance_start="${now}"; phase_maintenance_end="${now}"
	phase_graph_query_start=$(( now - 7 ))
	phase_graph_query_excluded_starts=("${now}")
	# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
	. "${timing_lib}"
	die_output="$(emit_phase_timings_and_flags 2>&1)" && {
		printf 'unequal-length exclusion arrays (ends short) must die, not silently treat the missing entry as absent\n' >&2
		exit 1
	}
	printf '%s\n' "${die_output}" | rg --fixed-strings -- 'have different lengths' >/dev/null || {
		printf 'unequal-length (ends short) die message missing expected diagnostic: %s\n' "${die_output}" >&2
		exit 1
	}
) || fail "phase-timing lib does not fail when phase_graph_query_excluded_ends is shorter than _starts (#5837)"
rm -rf "${phase_timing_half_set_end_missing_dir}"

# ---------------------------------------------------------------------------
# D13: the exclusion bracket (and its region/placement guards) left the
# orchestrator with the suppression-proof removal — there is no bracket left
# to place or bound. The timing lib defaults empty brackets sane, the
# behavioural arithmetic cases above keep pinning the lib, and the serving
# proof (with its own timing story) moves to the cutover lane.
# ---------------------------------------------------------------------------

phase_timing_cases_completed=1
