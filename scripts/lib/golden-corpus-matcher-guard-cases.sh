#!/usr/bin/env bash
# #5837 structural fix: committed negative tests for the placement guard
# itself (scripts/lib/golden-corpus-mirror-matcher.sh), sourced by
# test-verify-golden-corpus-gate.sh. Runs in the caller's shell and uses the
# caller's fail(), and the matcher's count_homes/require_in/resolve_unique_line
# (matcher_lib is sourced before this file). Extracted to its own lib chunk,
# not folded into golden-corpus-phase-timing-cases.sh (already at 460 lines),
# to keep both files under the repo's 500-line cap.
#
# Why this file exists
# ---------------------
# The exactly-one-non-comment-home invariant and the bracket-placement guard
# built on it (golden-corpus-phase-timing-cases.sh:447-458) have been evaded
# THREE times across review rounds -- a comment-unaware matcher, a hardcoded
# `#` that missed `.sql` `--` comments, a needle with two real homes, and a
# heredoc-body decoy that defeated a `--max-count=1` first-match lookup --
# and every one of those was found by a human or reviewer running a SCRATCH
# probe, not by anything committed and run in CI. A future "simplification"
# of resolve_unique_line back to a first-match lookup (dropping the
# `${#home_lines[@]}` count check and returning `home_lines[0]` unconditionally)
# would pass every gate this repo already runs. These cases exist so that
# exact regression turns CI red instead.
#
# Each case is a synthetic, throwaway fixture file, not the real
# verify-golden-corpus-gate.sh -- so these guard the MATCHER's own contract,
# independent of whatever the orchestrator happens to contain today. Cases 3
# and 4 additionally replicate the bracket-placement comparison
# (golden-corpus-phase-timing-cases.sh:455-458) against a fixture built the
# same shape as the orchestrator's real excluded-span block, to prove the
# comparison still fires when resolve_unique_line resolves clean (non-decoy)
# line numbers for a relocated bracket. That replica alone tests only ITSELF,
# not the production file -- deleting the real comparison at 455-458 left the
# whole mirror suite green, because neither case's body ever referenced the
# production file. Each case therefore also carries a require_in anchor
# straight on its own `(( ... ))` comparison line in
# golden-corpus-phase-timing-cases.sh, so deleting that line fails this file
# directly, independent of the fixture-based behavioral replica above it.

# Case 1: a comment mentioning the needle sits above two GENUINE (non-comment)
# occurrences of it. The comment must be excluded -- proving comment-awareness
# does not accidentally erase real duplication -- and the two real occurrences
# must still resolve "ambiguous".
matcher_guard_comment_decoy_dir="$(mktemp -d)"
(
	fixture="${matcher_guard_comment_decoy_dir}/comment-decoy.sh"
	cat >"${fixture}" <<'EOF'
#!/usr/bin/env bash
# NOTE: phase_graph_query_start= is stamped from pipeline_end -- documented
# here for context, not a real assignment.
phase_graph_query_start=$(date +%s)
phase_graph_query_start=$(( now - 999 ))
EOF
	output="$(resolve_unique_line "start assignment" "${fixture}" 'phase_graph_query_start=' 2>&1)" && {
		printf 'resolve_unique_line must die "ambiguous" on two genuine occurrences behind a comment decoy, not resolve to line %s\n' \
			"${output}" >&2
		exit 1
	}
	printf '%s\n' "${output}" | rg --fixed-strings -- 'ambiguous' >/dev/null || {
		printf 'wrong diagnostic for comment-decoy ambiguity: %s\n' "${output}" >&2
		exit 1
	}
) || fail "resolve_unique_line does not catch a comment decoy hiding a genuine duplicate assignment (#5837 structural gap, case 1)"
rm -rf "${matcher_guard_comment_decoy_dir}"

# Case 2: a heredoc-body decoy at column 1 -- the exact shape that defeated
# the predecessor `--max-count=1 | cut -d: -f1` lookup (golden-corpus-phase-
# timing-cases.sh:423-432). The decoy line is real, column-1, non-comment
# text, so it is a genuine second home, not something comment-awareness can
# exclude.
matcher_guard_heredoc_decoy_dir="$(mktemp -d)"
(
	fixture="${matcher_guard_heredoc_decoy_dir}/heredoc-decoy.sh"
	cat >"${fixture}" <<'EOF'
#!/usr/bin/env bash
cat <<'HEREDOC' >/dev/null
phase_graph_query_start=999
HEREDOC
phase_graph_query_start=$(date +%s)
EOF
	output="$(resolve_unique_line "start assignment" "${fixture}" 'phase_graph_query_start=' 2>&1)" && {
		printf 'resolve_unique_line must die "ambiguous" on a heredoc-body decoy, not resolve to line %s\n' \
			"${output}" >&2
		exit 1
	}
	printf '%s\n' "${output}" | rg --fixed-strings -- 'ambiguous' >/dev/null || {
		printf 'wrong diagnostic for heredoc-body decoy: %s\n' "${output}" >&2
		exit 1
	}
) || fail "resolve_unique_line does not catch a heredoc-body decoy at column 1 (#5837 structural gap, case 2)"
rm -rf "${matcher_guard_heredoc_decoy_dir}"

# Case 3: the excluded-span bracket relocated wholly ABOVE the
# phase_graph_query_start= assignment -- both stamps moved together, no
# decoy involved, so resolve_unique_line resolves every needle cleanly. This
# proves the PLACEMENT comparison itself (not just resolve_unique_line's
# uniqueness check) still fires: the bracket sits at real, unambiguous line
# numbers, and the comparison must still catch that the excluded-span open
# stamp comes before the window it is supposed to exclude from.
matcher_guard_bracket_above_dir="$(mktemp -d)"
(
	fixture="${matcher_guard_bracket_above_dir}/bracket-above-start.sh"
	cat >"${fixture}" <<'EOF'
#!/usr/bin/env bash
phase_graph_query_excluded_starts+=("$(date +%s)")
golden_suppression_verify_producer_truth
phase_graph_query_excluded_ends+=("$(date +%s)")
phase_graph_query_start=$(date +%s)
emit_phase_timings_and_flags
EOF
	start_line="$(resolve_unique_line "start assignment" "${fixture}" 'phase_graph_query_start=')"
	open_line="$(resolve_unique_line "excluded open stamp" "${fixture}" \
		'phase_graph_query_excluded_starts+=("$(date +%s)")')"
	output="$( (
		(( start_line < open_line )) ||
			fail "phase_graph_query excluded-span open stamp (line ${open_line}) is not after phase_graph_query_start= (line ${start_line})"
	) 2>&1 )" && {
		printf 'placement guard must die when the bracket sits wholly above phase_graph_query_start= (start=%s open=%s)\n' \
			"${start_line}" "${open_line}" >&2
		exit 1
	}
	printf '%s\n' "${output}" | rg --fixed-strings -- 'is not after phase_graph_query_start=' >/dev/null || {
		printf 'wrong diagnostic for a bracket relocated above the start assignment: %s\n' "${output}" >&2
		exit 1
	}
) || fail "placement guard does not catch an excluded-span bracket relocated wholly above phase_graph_query_start= (#5837 structural gap, case 3)"
rm -rf "${matcher_guard_bracket_above_dir}"

# #5837 P2 review: everything above proves resolve_unique_line resolves a
# relocated bracket's line numbers correctly against a SYNTHETIC fixture built
# to the same shape as the orchestrator's real excluded-span block -- it does
# not touch the PRODUCTION file at all. `rg -n 'repo_root|\$\{script\}|
# phase-timing-cases|verify-golden-corpus'` across this case's body finds
# nothing: deleting the real comparison at golden-corpus-phase-timing-cases.sh
# 455-458 leaves this whole mirror suite at exit 0, because the case only ever
# asserts its own inline replica of the comparison, never the production
# line. Anchor directly on the real comparison line, the same require_in
# convention test-verify-golden-corpus-gate.sh already uses for its other
# extracted-lib assertions (e.g. lines 263, 268, 271, 353).
require_in "phase_graph_query start-vs-excluded-open placement comparison exists in production" \
	"${repo_root}/scripts/lib/golden-corpus-phase-timing-cases.sh" \
	'(( phase_graph_query_start_line < phase_graph_query_excluded_open_line )) ||'

# Case 4: the excluded-span bracket relocated wholly BELOW
# emit_phase_timings_and_flags -- the mirror image of case 3. Again no decoy;
# every needle resolves to one clean line, so this isolates the PLACEMENT
# comparison's other half: the excluded-span close stamp must sit before the
# call that consumes it.
matcher_guard_bracket_below_dir="$(mktemp -d)"
(
	fixture="${matcher_guard_bracket_below_dir}/bracket-below-emit.sh"
	cat >"${fixture}" <<'EOF'
#!/usr/bin/env bash
phase_graph_query_start=$(date +%s)
emit_phase_timings_and_flags
phase_graph_query_excluded_starts+=("$(date +%s)")
golden_suppression_verify_producer_truth
phase_graph_query_excluded_ends+=("$(date +%s)")
EOF
	close_line="$(resolve_unique_line "excluded close stamp" "${fixture}" \
		'phase_graph_query_excluded_ends+=("$(date +%s)")')"
	emit_line="$(resolve_unique_line "emit invocation" "${fixture}" 'emit_phase_timings_and_flags')"
	output="$( (
		(( close_line < emit_line )) ||
			fail "phase_graph_query excluded-span close stamp (line ${close_line}) is not before emit_phase_timings_and_flags (line ${emit_line})"
	) 2>&1 )" && {
		printf 'placement guard must die when the bracket sits wholly below emit_phase_timings_and_flags (close=%s emit=%s)\n' \
			"${close_line}" "${emit_line}" >&2
		exit 1
	}
	printf '%s\n' "${output}" | rg --fixed-strings -- 'is not before emit_phase_timings_and_flags' >/dev/null || {
		printf 'wrong diagnostic for a bracket relocated below emit_phase_timings_and_flags: %s\n' "${output}" >&2
		exit 1
	}
) || fail "placement guard does not catch an excluded-span bracket relocated wholly below emit_phase_timings_and_flags (#5837 structural gap, case 4)"
rm -rf "${matcher_guard_bracket_below_dir}"

# #5837 P2 review: the mirror image of case 3's anchor above -- case 4 also
# only ever asserts its own inline replica against a synthetic fixture, never
# the production comparison. Anchor directly on the real comparison line.
require_in "phase_graph_query excluded-close-vs-emit placement comparison exists in production" \
	"${repo_root}/scripts/lib/golden-corpus-phase-timing-cases.sh" \
	'(( phase_graph_query_excluded_close_line < phase_graph_query_emit_line )) ||'

# Case 5: a genuinely missing anchor must die "missing", naming the anchor
# and the file, and must NOT be a silent pipefail abort with no diagnostic
# (#5837 P2-2: the predecessor `rg ... | cut` pipeline aborted on a genuine
# zero-match before this function's own diagnostic ran). Exercised through
# both call shapes real callers use: require_in (count_homes, via
# require_in_text's `homes="$(count_homes ...)" || exit 1`) and a bare
# `$( )`-wrapped resolve_unique_line, the same two shapes P2-1's fix
# (golden-corpus-mirror-matcher.sh) is proven against.
matcher_guard_missing_anchor_dir="$(mktemp -d)"
(
	fixture="${matcher_guard_missing_anchor_dir}/missing-anchor.sh"
	cat >"${fixture}" <<'EOF'
#!/usr/bin/env bash
some_other_assignment=1
EOF
	require_output="$(require_in "start assignment" "${fixture}" 'phase_graph_query_start=' 2>&1)" && {
		printf 'require_in must die "missing" on a genuinely absent anchor, not silently pass: %s\n' "${require_output}" >&2
		exit 1
	}
	printf '%s\n' "${require_output}" | rg --fixed-strings -- 'missing start assignment' >/dev/null || {
		printf 'require_in missing-anchor diagnostic does not name the label: %s\n' "${require_output}" >&2
		exit 1
	}
	printf '%s\n' "${require_output}" | rg --fixed-strings -- "$(basename "${fixture}")" >/dev/null || {
		printf 'require_in missing-anchor diagnostic does not name the file: %s\n' "${require_output}" >&2
		exit 1
	}
	resolve_output="$(resolve_unique_line "start assignment" "${fixture}" 'phase_graph_query_start=' 2>&1)" && {
		printf 'resolve_unique_line must die "missing" on a genuinely absent anchor, not resolve to line %s\n' \
			"${resolve_output}" >&2
		exit 1
	}
	printf '%s\n' "${resolve_output}" | rg --fixed-strings -- 'missing start assignment' >/dev/null || {
		printf 'resolve_unique_line missing-anchor diagnostic does not name the label: %s\n' "${resolve_output}" >&2
		exit 1
	}
) || fail "the matcher does not die \"missing\", naming the anchor and file, on a genuinely absent needle (#5837 structural gap, case 5)"
rm -rf "${matcher_guard_missing_anchor_dir}"

# Case 6 (#5837 P2-1 regression guard): a needle carrying a literal \E must
# die with exactly the \E-rejection message and abort -- not continue past
# build_home_pattern's rejection with an empty pattern and reinterpret it as
# match-everything, and not fall through to a second, misleading "missing"
# verdict either (found writing THIS case: require_in_text's own
# `homes="$(count_homes ...)"` had the identical masked-assignment exposure
# one layer up, closed alongside P2-1 with an explicit `|| exit 1` there
# too). Exercised through both call shapes P2-1's fix is proven against:
# require_in (which now nests three nested-substitution-assignment layers
# deep at count_homes -> build_home_pattern, all now sentinel-guarded) and a
# bare `$( )`-wrapped resolve_unique_line.
matcher_guard_backslash_e_dir="$(mktemp -d)"
(
	fixture="${matcher_guard_backslash_e_dir}/backslash-e.sh"
	cat >"${fixture}" <<'EOF'
#!/usr/bin/env bash
line one
line two
line three
EOF
	require_output="$(require_in "probe label" "${fixture}" 'foo\Ebar' 2>&1)" && {
		printf 'require_in must die on a needle carrying a literal \\E, not silently pass: %s\n' "${require_output}" >&2
		exit 1
	}
	require_message_count="$(printf '%s\n' "${require_output}" | rg --count -- 'test-verify-golden-corpus-gate:' || true)"
	[[ "${require_message_count:-0}" -eq 1 ]] || {
		printf 'require_in on a literal \\E needle must print exactly ONE message, got %s: %s\n' \
			"${require_message_count:-0}" "${require_output}" >&2
		exit 1
	}
	printf '%s\n' "${require_output}" | rg --fixed-strings -- 'needle carries a literal \E' >/dev/null || {
		printf 'require_in \\E diagnostic missing expected message: %s\n' "${require_output}" >&2
		exit 1
	}
	resolve_output="$(resolve_unique_line "probe label" "${fixture}" 'foo\Ebar' 2>&1)" && {
		printf 'resolve_unique_line must die on a needle carrying a literal \\E, not resolve to line %s\n' \
			"${resolve_output}" >&2
		exit 1
	}
	resolve_message_count="$(printf '%s\n' "${resolve_output}" | rg --count -- 'test-verify-golden-corpus-gate:' || true)"
	[[ "${resolve_message_count:-0}" -eq 1 ]] || {
		printf 'resolve_unique_line on a literal \\E needle must print exactly ONE message, got %s: %s\n' \
			"${resolve_message_count:-0}" "${resolve_output}" >&2
		exit 1
	}
	printf '%s\n' "${resolve_output}" | rg --fixed-strings -- 'needle carries a literal \E' >/dev/null || {
		printf 'resolve_unique_line \\E diagnostic missing expected message: %s\n' "${resolve_output}" >&2
		exit 1
	}
) || fail "the matcher does not reject a literal-\\E needle with exactly one message and an abort (#5837 P2-1 regression guard, case 6)"
rm -rf "${matcher_guard_backslash_e_dir}"

matcher_guard_cases_completed=1
