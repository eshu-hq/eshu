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
# it floors itself at 20s: golden_suppression_prepare_payloads sets
# golden_suppression_expiry_epoch = now + 20 and golden_suppression_wait_for_expiry
# then blocks until that deadline, so the block cannot finish faster however
# quick the host is.
#
# Unbracketed, that 20s was billed to pipeline startup, against a 3s baseline
# and an 8s effective ceiling (baseline_seconds=3 + absolute_slack_seconds=5,
# testdata/golden/e2e-baseline.json). That is advisory on shared CI only because
# the lane defaults GATE_PHASE_REGRESSION_ADVISORY=true; a controlled host
# running it blocking fails on it, with nothing in the pipeline actually slower.
#
# No measurement is needed to prove that: 20 > 8 by construction, on any host
# and any corpus. The "~4s -> ~23s" figure quoted around this change is an
# illustrative local reading with no committed phase-timings.json, host, or
# manifest behind it -- see docs/internal/evidence/5837-aws-drift-reopen.md,
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
# Structural: the bracket exists in the orchestrator, and the lib subtracts it.
#
# Both halves are required. The bracket alone leaves the stamps dead code, and
# the subtraction alone has nothing to subtract; either one on its own is a
# silent no-op that still emits a plausible-looking phase number.
# ---------------------------------------------------------------------------
# shellcheck disable=SC2016  # the needles are literal source lines
require "suppression proof excluded-span open" 'phase_graph_query_excluded_starts+=("$(date +%s)")'
# shellcheck disable=SC2016
require "suppression proof excluded-span close" 'phase_graph_query_excluded_ends+=("$(date +%s)")'
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
# #5837 P2 review: bracket PLACEMENT was unasserted. The two stamps existing
# somewhere in the orchestrator (already asserted above via `require`) does
# not pin that they actually surround
# golden_suppression_verify_producer_truth, nor that nothing else
# long-running rides inside the exclusion. Moving the stamps to span the
# whole phase would emit graph_query=0 forever with every gate still green --
# the exact "the gate measured something other than what it claimed" class
# this change exists to fix.
# ---------------------------------------------------------------------------
# shellcheck disable=SC2016  # the range boundaries are literal source lines
phase_graph_query_excluded_region_range='/phase_graph_query_excluded_starts+=("$(date +%s)")/,/phase_graph_query_excluded_ends+=("$(date +%s)")/'
require_region "excluded-span brackets the suppression producer proof, not something wider" \
	"${phase_graph_query_excluded_region_range}" "golden_suppression_verify_producer_truth"

# Presence guard, ALLOWLIST not denylist (#5837 round-8 review): the previous
# version of this guard rejected the region only if it contained "start_bg" or
# "readyz" -- two specific strings pulled from the one incident that had
# already happened. Any THIRD block inserted between the stamps (a new health
# check, a new setup call, anything not named start_bg or readyz) would pass
# this guard silently, get deducted from graph_query forever, and every gate
# would stay green while the phase measured less than it claimed to. An
# allowlist closes that: every non-comment, non-blank line in the region must
# be one of the three lines this bracket is supposed to contain.
phase_graph_query_excluded_region="$(sed -n "${phase_graph_query_excluded_region_range}p" "${script}")"
[[ -n "${phase_graph_query_excluded_region}" ]] ||
	fail "empty phase_graph_query excluded region in $(basename "${script}")"
# shellcheck disable=SC2016  # literal source lines, not shell expansions
phase_graph_query_excluded_region_allowed=(
	'phase_graph_query_excluded_starts+=("$(date +%s)")'
	'golden_suppression_verify_producer_truth'
	'phase_graph_query_excluded_ends+=("$(date +%s)")'
)
# Process substitution, not a `<<<` here-string: a here-string feeding a
# while-read loop is known to hang under Homebrew bash 5.3.15 once the body
# crosses a size threshold. This region is 3 lines today, but the pattern
# below is the one the rest of this repo's shell code standardizes on, so a
# future larger region does not silently reintroduce that hang.
while IFS= read -r phase_graph_query_excluded_region_line; do
	# Pure-bash trim (no sed/awk): strips leading/trailing [:space:] without
	# touching a BSD-sed bracket-expression edge case elsewhere in this repo
	# (a `\t` inside `[ \t]` on macOS sed has silently corrupted output before).
	phase_graph_query_excluded_region_trimmed="${phase_graph_query_excluded_region_line#"${phase_graph_query_excluded_region_line%%[![:space:]]*}"}"
	phase_graph_query_excluded_region_trimmed="${phase_graph_query_excluded_region_trimmed%"${phase_graph_query_excluded_region_trimmed##*[![:space:]]}"}"
	[[ -z "${phase_graph_query_excluded_region_trimmed}" ]] && continue
	[[ "${phase_graph_query_excluded_region_trimmed}" == \#* ]] && continue
	phase_graph_query_excluded_region_line_allowed=0
	for phase_graph_query_excluded_region_candidate in "${phase_graph_query_excluded_region_allowed[@]}"; do
		if [[ "${phase_graph_query_excluded_region_trimmed}" == "${phase_graph_query_excluded_region_candidate}" ]]; then
			phase_graph_query_excluded_region_line_allowed=1
			break
		fi
	done
	(( phase_graph_query_excluded_region_line_allowed )) ||
		fail "phase_graph_query excluded region contains an unexpected line -- everything inside these stamps is silently deducted from graph_query forever, so only the suppression producer proof call belongs here: ${phase_graph_query_excluded_region_trimmed} (#5837)"
done < <(printf '%s\n' "${phase_graph_query_excluded_region}")

# ---------------------------------------------------------------------------
# #5837 P2 review follow-up: everything above guards region CONTENT (what
# rides between the two stamp lines), not region PLACEMENT (whether the whole
# bracket sits inside the graph_query window at all). A bracket relocated
# wholly ABOVE phase_graph_query_start= -- both stamps moved together, same
# three allowed lines, same relative order -- passes every check above
# unchanged: sed's pattern-range match finds the same two lines wherever they
# sit in the file. The runtime `excluded > window` die
# (golden-corpus-phase-timings.sh:90-92) is what actually catches that shape
# today, not this static gate. This block closes the static gap by asserting
# the bracket's own source line numbers fall strictly between the
# phase_graph_query_start= assignment and the emit_phase_timings_and_flags
# call that consumes it.
#
# #5837 round-9+ review: an earlier version of this block anchored each needle
# with `^...`/`--line-regexp` and picked the FIRST match via `rg --max-count=1
# | cut -d: -f1`. That defeats a COMMENT decoy (comments cannot open with the
# needle text), but not a HEREDOC-BODY decoy: a `cat <<'EOF' >/dev/null` /
# `phase_graph_query_start=999` / `EOF` block placed above the real assignment
# is a real, column-1, non-comment line that satisfies `^phase_graph_query_start=`
# just as well as the genuine one, and `--max-count=1` silently bound to
# whichever line rg listed first -- reproduced: a bracket relocated wholly
# above the real start assignment, with a matching heredoc decoy placed even
# earlier, passed this guard at exit 0 with the real placement genuinely wrong.
# Enumerating decoy shapes (a comment, then a heredoc body, and whatever comes
# next) does not terminate; requiring UNIQUENESS does. resolve_unique_line
# (scripts/lib/golden-corpus-mirror-matcher.sh) is the SAME exactly-one-
# non-comment-home invariant the `require`/`require_lib` calls above already
# apply to these same needles (see phase_graph_query_excluded_region_allowed),
# now also returning the resolved line number: a decoy of ANY shape that adds
# a second non-comment home turns the lookup "ambiguous" and dies loudly
# instead of silently binding to the wrong line. It also replaces the
# `rg ... | cut` pipeline that, under this file's `set -o pipefail` caller,
# aborted the whole script on a genuine zero-match BEFORE reaching this
# block's own diagnostic (#5837 P2-2, reproduced with trailing whitespace
# defeating the old `--line-regexp` lookup) -- resolve_unique_line guards its
# own `rg` call and reports "missing ..." instead.
# ---------------------------------------------------------------------------
phase_graph_query_start_line="$(resolve_unique_line \
	"phase_graph_query_start= assignment" "${script}" 'phase_graph_query_start=')"
phase_graph_query_excluded_open_line="$(resolve_unique_line \
	"phase_graph_query_excluded_starts+= stamp" "${script}" "${phase_graph_query_excluded_region_allowed[0]}")"
phase_graph_query_excluded_close_line="$(resolve_unique_line \
	"phase_graph_query_excluded_ends+= stamp" "${script}" "${phase_graph_query_excluded_region_allowed[2]}")"
phase_graph_query_emit_line="$(resolve_unique_line \
	"emit_phase_timings_and_flags invocation" "${script}" 'emit_phase_timings_and_flags')"
(( phase_graph_query_start_line < phase_graph_query_excluded_open_line )) ||
	fail "phase_graph_query excluded-span open stamp (line ${phase_graph_query_excluded_open_line}) is not after phase_graph_query_start= (line ${phase_graph_query_start_line}) in $(basename "${script}") -- a bracket relocated above the start assignment mismeasures the window without tripping any content check"
(( phase_graph_query_excluded_close_line < phase_graph_query_emit_line )) ||
	fail "phase_graph_query excluded-span close stamp (line ${phase_graph_query_excluded_close_line}) is not before emit_phase_timings_and_flags (line ${phase_graph_query_emit_line}) in $(basename "${script}")"

phase_timing_cases_completed=1
