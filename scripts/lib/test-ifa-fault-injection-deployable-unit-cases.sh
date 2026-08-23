#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# deployable_unit_edges (#5993) structural cases for
# scripts/test-verify-ifa-fault-injection.sh, split out (mirroring
# test-ifa-fault-injection-review-cases.sh) to keep that file under the
# repository's 500-line cap. The parent verifier owns strict mode, fail(),
# and every path variable referenced below (script, driver_lib,
# deployable_unit_live_lib, deployable_unit_diagnostics_lib,
# deployable_unit_lock_lib, deployable_unit_cells_lib).
# deployable_unit_diagnostics_lib (ifa_deployable_unit_live_diagnostics.sh)
# holds the post-maintenance DIAGNOSTIC probes split out of
# ifa_deployable_unit_live.sh to keep THAT file under the same cap --
# their definitions are checked there, while their WIRING (call sites in the
# standalone cell and the fault cells) is still checked against
# deployable_unit_live_lib / deployable_unit_cells_lib, since only the
# function bodies moved. deployable_unit_lock_lib
# (ifa_fault_injection_deployable_unit_lock.sh, #6149) holds the fault
# cells' precondition assertion and lock/release pair, split out of
# deployable_unit_cells_lib for the same 500-line reason -- their
# definitions are checked there, their WIRING against deployable_unit_cells_lib.

# shellcheck source=scripts/lib/test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh
deployable_unit_kill_isolation_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-deployable-unit-kill-isolation-cases.sh"
source "${deployable_unit_kill_isolation_cases_lib}"

require_deployable_unit_live_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${deployable_unit_live_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (deployable-unit live lib), or it survives only inside a comment: ${needle}"
}
require_deployable_unit_diagnostics_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${deployable_unit_diagnostics_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (deployable-unit diagnostics lib), or it survives only inside a comment: ${needle}"
}
require_deployable_unit_lock_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${deployable_unit_lock_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (deployable-unit lock lib), or it survives only inside a comment: ${needle}"
}
require_deployable_unit_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${deployable_unit_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (deployable-unit cells lib), or it survives only inside a comment: ${needle}"
}

# run_ifa_fault_injection_deployable_unit_cases asserts the family-scoped
# baseline cell, its two fault cells, and their dispatch ordering. Wrapped in
# a function (mirroring run_ifa_fault_injection_review_cases) so ${script},
# ${driver_lib}, and the deployable-unit path vars resolve at CALL time from
# the parent's scope, not at source time -- a bare top-level block here would
# silently assert against empty variables.
run_ifa_fault_injection_deployable_unit_cases() {
	run_ifa_fault_injection_deployable_unit_kill_isolation_cases
	# The live gate script actually sources each deployable-unit lib. Moved
	# here from the top-level preamble (mirroring the require_* relocation
	# this whole split exists to demonstrate) to keep the parent structural
	# verifier under the repository's 500-line cap.
	for source_name in \
		ifa_deployable_unit_live.sh \
		ifa_deployable_unit_live_diagnostics.sh \
		ifa_deployable_unit_live_converge.sh \
		ifa_fault_injection_deployable_unit_lock.sh \
		ifa_fault_injection_deployable_unit_cells.sh; do
		[[ "$(_ifa_count_code_matches "scripts/lib/${source_name}" "${sources_lib}")" -ge 1 ]] \
			|| fail "source inventory omits ${source_name}"
	done

	# deployable_unit_edges (#5993): a family-scoped baseline cell plus two
	# fault cells, run after a bootstrap-index maintenance pass
	# (ifa_deployable_unit_live.sh's header explains why); fault cells compare
	# against their OWN baseline.
	require_fixture "deployable-unit cassette path" "testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"
	require_fixture "deployable-unit expected-edge set path" "go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"
	require_fixture "deployable-unit cassette existence guard" "deployable-unit cassette not found"
	require_fixture "deployable-unit expected-edge set existence guard" "deployable-unit expected-edge set not found"
	require_code "deployable-unit MERGE operation_match anchor" 'deployable_unit_edge_operation_match="MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)"'
	require_code "sixth binary: bootstrap-index build" "ifa_det_build_bin \"\${bin_dir}\" bootstrap-index"
	require_driver "deployable-unit drive in every cell" 'eshu-ifa" drive -cassette "${deployable_unit_cassette}" -workers "${drive_workers}"'
	# Four checks against the LIVE LIB itself (ifa_deployable_unit_live.sh),
	# mirroring the documentation family's require_documentation_lib set.
	# Without these, require_deployable_unit_live_lib existed with zero call
	# sites: the mirror asserted the CELLS file's shape (it calls the wrapper
	# function names) but proved nothing about the file that does the actual
	# drive/assert-edges/maintenance-pass work -- someone could gut this
	# family's live proof entirely and the mirror would still pass.
	require_deployable_unit_live_lib "live lib drive command" '"${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers 1'
	require_deployable_unit_live_lib "live lib exact assertion domain" "-domain deployable_unit_edges"
	require_deployable_unit_live_lib "live lib non-vacuity framing" "one-edge exact set"
	require_deployable_unit_live_lib "live lib maintenance-pass invocation" '"${bin_dir}/eshu-bootstrap-index"'
	# Pinned to the CODE SHAPE, not the bare phrase: the phrase
	# "cross-repo relationship resolution started" also appears in this
	# function's own header comment and in its echo/printf diagnostics, so a
	# plain phrase needle would still match after the functional comparison
	# below was deleted -- exactly the "cannot fail" trap this whole item
	# exists to avoid. Each needle is the literal `if [[ ... ]]` condition, so
	# deleting either comparison (not just the phrase) turns this red.
	require_deployable_unit_live_lib "live lib readiness-opened resolution-started check" '"${contents}" != *"cross-repo relationship resolution started"* ]]'
	require_deployable_unit_live_lib "live lib readiness-opened not-gated check" '"${contents}" == *"cross-repo resolution gated"* ]]'
	# Readiness-review follow-up item 3 (#5993): the deferred-backfill fan-in
	# can decline to publish a partition and still exit zero (logging
	# deferred_backfill_fanin_partition_skipped=true ... reason="generation_
	# advanced_since_batch"). When that fires, readiness never opens, the
	# reducer log shows "started" paired with "gated" (the SAME text as the
	# designed pre-maintenance-gate case), and nothing in the output says why.
	# Pinned to the CODE SHAPE (the bash substring comparison against the
	# bootstrap-index maintenance-pass log), not the bare marker phrase alone,
	# and specifically NOT `rg` -- #5974 (cited in this same function's own
	# doc comment) proved rg is absent on the fault-injection CI runner, so a
	# check built on it would exit "command not found" and read as a false
	# negative in the exact function whose docstring already warns against
	# reintroducing that failure mode.
	require_deployable_unit_live_lib "live lib readiness-opened fan-in-skip marker (bash substring, not rg)" '"${bootstrap_contents}" == *"deferred_backfill_fanin_partition_skipped=true"*'
	require_deployable_unit_live_lib "live lib readiness-opened reads the bootstrap-index maintenance-pass log, not the reducer log, for the fan-in marker" 'bootstrap_index_log="${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log"'
	# Post-maintenance intents diagnostic (#5993 follow-up), definition now in
	# the diagnostics lib (ifa_deployable_unit_live_diagnostics.sh, split out
	# to keep the live lib under the 500-line cap): separates the two
	# indistinguishable causes of the live gate's "expected 1, got 0" failure --
	# a writer endpoint-MATCH miss (intents > 0, 0 edges) vs. correlation never
	# admitting a row (intents == 0). Do not count the needles below in prose
	# here; a stale count is exactly the kind of drift this comment keeps
	# tripping on -- read the require_deployable_unit_diagnostics_lib calls
	# directly. What they establish, by property rather than by number: the
	# function exists and states BOTH interpretations (not just a count); the
	# after-probe never gates (the call site, in the live lib and cells lib,
	# has no `|| return 1` on purpose, so the caller's `set -e` is NOT
	# suppressed there -- an unguarded internal query failure would abort the
	# whole cell before the real edge assertion runs) and degrades any of
	# query-failed/empty/non-numeric to one "unavailable" reading rather than
	# a legitimate-looking "0"; the BEFORE probe (assert_empty_before_maintenance,
	# still in the live lib) has the same hazard except it GATES, so it is
	# pinned separately to prove it fails closed with the real exit code and
	# treats empty/non-numeric as unknown, never as a legitimate zero. All
	# three post-maintenance probes in the diagnostics lib share the same
	# unpiped rc-capture opening line by harmonization with the live lib's
	# before-probe (pinned as PER-FILE COUNTS, not a presence needle, since
	# presence alone cannot tell "only one function has this" from "all do").
	# Wiring existence alone would still pass if a call were wired below the
	# final edge assertion it is supposed to precede, or clustered into the
	# wrong cell, so ordering and cell containment are pinned as line-number /
	# enclosing-function checks below rather than as text needles.
	require_deployable_unit_diagnostics_lib "diagnostics lib post-maintenance intents diagnostic definition" "ifa_deployable_unit_live_report_intents_after_maintenance() {"
	# #6149 review (P2a): the printed message used to claim it could
	# discriminate writer-drop from correlation-produced-nothing via a
	# nonzero/zero branch on a count that is always 0 for this family --
	# printing the WRONG cause with confidence on every single run. Dropped
	# the branch language; the message now states plainly that this count
	# cannot discriminate the two causes.
	require_deployable_unit_diagnostics_lib "diagnostics lib post-maintenance intents diagnostic states it cannot discriminate the two causes" "this count is always 0 for this family regardless of outcome"
	rg --quiet --fixed-strings -- 'intents > 0 with 0 edges implicates the writer' "${deployable_unit_diagnostics_lib}" \
		&& fail "the post-maintenance intents diagnostic must not claim it can discriminate writer-drop from correlation-produced-nothing any more (#6149 P2a) in ${deployable_unit_diagnostics_lib}"
	rg --quiet --fixed-strings -- 'intents == 0 implicates correlation' "${deployable_unit_diagnostics_lib}" \
		&& fail "the post-maintenance intents diagnostic must not claim intents==0 implicates correlation any more (#6149 P2a, always false for this family) in ${deployable_unit_diagnostics_lib}"
	require_deployable_unit_diagnostics_lib "diagnostics lib post-maintenance intents diagnostic failure-tolerant reading" 'admitted="unavailable (query failed)"'
	# The after-probe never pipes its query capture (ifa_det_pg is called bare,
	# not through `| tr`), so its own success/failure never depends on the
	# caller having set pipefail -- this pins the collapsed tri-condition
	# (query failed, OR empty, OR non-numeric) that decides whether the
	# reading becomes "unavailable", not just a bare `-z` check that a partial
	# rewrite could satisfy while still leaving a non-numeric value unhandled.
	require_deployable_unit_diagnostics_lib "diagnostics lib post-maintenance intents diagnostic collapses failed/empty/non-numeric to unavailable" '"${admitted_rc}" -ne 0 || -z "${admitted}" || ! "${admitted}" =~ ^[0-9]+$ ]]; then'
	# The BEFORE probe (assert_empty_before_maintenance) used to share the
	# after-probe's rc-capture shape (a shared_projection_intents count via
	# ifa_det_pg), but that query was provably vacuous for this domain: the
	# handler never writes shared_projection_intents at all (#6149), so the
	# count was always 0 whether the readiness gate was open or shut. The
	# BEFORE probe now asserts against the live GRAPH instead (`eshu-ifa
	# graph-dump -out` piped through `jq`, counting CORRELATES_DEPLOYABLE_UNIT
	# edges), so it no longer shares the after-probe's ifa_det_pg shape at
	# all -- the unpiped rc-capture count in the live lib drops to 0.
	#
	# The diagnostics lib's two remaining Postgres-diagnostic functions
	# (report_intents_after_maintenance, report_resolved_deploys_from_count)
	# are UNCHANGED by #6149 and still share that shape, so their count stays
	# pinned at 2.
	local du_unpiped_rc_capture_count du_unpiped_rc_capture_diag_count
	du_unpiped_rc_capture_count="$(rg --fixed-strings --count-matches -- '="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \' "${deployable_unit_live_lib}" || true)"
	[[ "${du_unpiped_rc_capture_count}" -eq 0 ]] \
		|| fail "expected zero unpiped ifa_det_pg rc-capture query shapes in ${deployable_unit_live_lib} (the before-probe moved to a graph-dump-based check in #6149); found ${du_unpiped_rc_capture_count:-0}"
	du_unpiped_rc_capture_diag_count="$(rg --fixed-strings --count-matches -- '="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \' "${deployable_unit_diagnostics_lib}" || true)"
	[[ "${du_unpiped_rc_capture_diag_count}" -eq 2 ]] \
		|| fail "expected the unpiped rc-capture query shape exactly twice in ${deployable_unit_diagnostics_lib} (report_intents_after_maintenance and report_resolved_deploys_from_count); found ${du_unpiped_rc_capture_diag_count:-0}"
	# The BEFORE probe's own four fail-closed branches (jq missing, dump
	# failed, count computation failed, and the empty/non-numeric/non-zero
	# count checks), each still handled distinctly rather than collapsing to
	# a single "unavailable" reading -- it GATES (return 1 fails the cell),
	# so a check that never ran must never silently read as "0".
	require_deployable_unit_live_lib "live lib before-probe dumps the graph, not a Postgres query" '"${bin_dir}/eshu-ifa" graph-dump -out "${ifa_deployable_unit_before_probe_dump_path}"'
	# #6149 live-run finding: a RETURN trap referencing a `local` dump_path
	# aborted the fault-injection matrix with "dump_path: unbound variable"
	# before the kill-worker cell ever ran. The scratch-file variable must
	# stay a global (not `local`), matching marker_dir's known-working
	# EXIT-trap fix in test-ifa-fault-injection-marker-cases.sh.
	rg --quiet --fixed-strings -- 'local count' "${deployable_unit_live_lib}" \
		|| fail "live lib before-probe: expected the before-probe's local declaration to name only 'count', not a dump-path variable, in ${deployable_unit_live_lib} (the scratch dump path must be a global, not local, to survive its RETURN trap)"
	rg --quiet --fixed-strings -- 'local dump_path' "${deployable_unit_live_lib}" \
		&& fail "live lib before-probe: the graph-dump scratch path must not be declared local in ${deployable_unit_live_lib} -- its RETURN trap references it after the local binding would already be torn down (#6149 live-run abort)"
	require_deployable_unit_live_lib "live lib before-probe counts CORRELATES_DEPLOYABLE_UNIT edges via jq" 'select(.type == "CORRELATES_DEPLOYABLE_UNIT")'
	require_deployable_unit_live_lib "live lib before-probe requires jq and fails closed without it" "requires jq, which is not on PATH; treat this as unknown, not as a verdict"
	require_deployable_unit_live_lib "live lib before-probe fails closed on a graph-dump failure" "before-maintenance precondition graph-dump FAILED; treat this as unknown, not as a verdict"
	require_deployable_unit_live_lib "live lib before-probe treats empty output as unknown, not zero" "before-maintenance precondition edge count came back empty; treat this as unknown, not as zero"
	require_deployable_unit_live_lib "live lib before-probe treats non-numeric output as unknown, not zero" "before-maintenance precondition edge count '\${count}' is non-numeric; treat this as unknown, not as zero"
	require_deployable_unit_live_lib "live lib before-probe non-zero verdict names the graph edge type" "expected 0 CORRELATES_DEPLOYABLE_UNIT edges before the maintenance pass, got"
	# All four call sites now pass only bin_dir -- the Postgres-connection args
	# (compose_project/use_compose/dsn/compose_file) the old shared_projection_intents
	# query needed are gone from every call site, not just the definition.
	local du_before_probe_call_count
	du_before_probe_call_count="$(rg --fixed-strings --count-matches -- 'ifa_deployable_unit_live_assert_empty_before_maintenance "${bin_dir}"' "${deployable_unit_live_lib}" "${deployable_unit_cells_lib}" | awk -F: '{sum+=$2} END{print sum+0}')"
	[[ "${du_before_probe_call_count}" -eq 4 ]] \
		|| fail "expected ifa_deployable_unit_live_assert_empty_before_maintenance to be called with only \"\${bin_dir}\" at all 4 call sites (1 standalone cell + 3 fault cells); found ${du_before_probe_call_count:-0}"
	# Readiness-review follow-up item 4 (#5993): the unproven persisted-resolution
	# -> reopened-correlation link presents as readiness=pass, intents=0,
	# edges="got 0" -- IDENTICAL to the designed "correlation produced
	# nothing" case. Two cheap, no-new-mechanism separators turn that single
	# ambiguous outcome into three distinguishable causes:
	#   - did resolution persist anything? a count of resolved_relationships
	#     WHERE relationship_type = 'DEPLOYS_FROM' (relationships.RelDeploysFrom
	#     / edgetype.DeploysFrom), taken where the intents count is taken.
	#   - did the reopen fire? tailing the bootstrap-index maintenance-pass log
	#     for ReopenSucceededReducerWorkItems's per-domain line
	#     (reducer_work_items_reopened domain=deployable_unit_correlation
	#     count=N, logged UNCONDITIONALLY for every domain, even count=0) --
	#     absent means the reopen phase itself never ran; present with
	#     count=0 means it ran and found nothing.
	# Both diagnostic only, never a gate, and both defined in the diagnostics
	# lib (ifa_deployable_unit_live_diagnostics.sh). Bash substring for the
	# reopen marker, NOT rg, for the same #5974 reason
	# ifa_deployable_unit_live_assert_readiness_opened's own doc comment (in
	# the live lib) cites.
	require_deployable_unit_diagnostics_lib "diagnostics lib resolved-DEPLOYS_FROM-count definition" "ifa_deployable_unit_live_report_resolved_deploys_from_count() {"
	require_deployable_unit_diagnostics_lib "diagnostics lib resolved-DEPLOYS_FROM-count query" "SELECT count(*) FROM resolved_relationships WHERE relationship_type = 'DEPLOYS_FROM';"
	require_deployable_unit_diagnostics_lib "diagnostics lib correlation-reopen definition" "ifa_deployable_unit_live_report_correlation_reopen() {"
	require_deployable_unit_diagnostics_lib "diagnostics lib correlation-reopen domain marker text" "reducer_work_items_reopened domain=deployable_unit_correlation count="
	require_deployable_unit_diagnostics_lib "diagnostics lib correlation-reopen uses bash substring, not rg" '"${bootstrap_contents}" == *"${reopen_marker}"*'
	require_deployable_unit_live_lib "live lib standalone cell resolved-DEPLOYS_FROM-count wiring" 'ifa_deployable_unit_live_report_resolved_deploys_from_count "${compose_project}"'
	require_deployable_unit_live_lib "live lib standalone cell correlation-reopen wiring" 'ifa_deployable_unit_live_report_correlation_reopen "${log_dir}" "primary"'
	# Ordering: the standalone determinism cell's post-maintenance intents
	# call must precede ITS OWN final edge assertion (ifa_deployable_unit_live_assert
	# ... || return 1 -- a call moved below it never executes on the only path
	# where the diagnostic matters, the failing path).
	#
	# Containment, not a bare line-number comparison (#5993 convergence-loop
	# follow-up): ifa_deployable_unit_live_converge_edges's own body also
	# calls `ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}"`
	# (its shared parameter names match the standalone cell's local ones
	# verbatim), so a plain rg match now finds that call too and a bare
	# line-number compare breaks the moment a second textually-identical call
	# exists anywhere in the file -- the same containment lesson the fault
	# cells' check below already learned. Map every line to its nearest
	# preceding top-level function definition and require both the report and
	# the assert line to fall inside ifa_deployable_unit_live_run_standalone_cell
	# specifically, not merely appear earlier in the file.
	local du_standalone_fn_at_line_raw du_standalone_fn_ln du_standalone_fn_name
	local -A du_standalone_fn_at_line
	du_standalone_fn_at_line_raw="$(awk '
		/^[A-Za-z_][A-Za-z0-9_]*\(\) \{$/ { sub(/\(\) \{$/, ""); fn = $0 }
		{ print NR, (fn == "" ? "NONE" : fn) }
	' "${deployable_unit_live_lib}")"
	while read -r du_standalone_fn_ln du_standalone_fn_name; do
		du_standalone_fn_at_line["${du_standalone_fn_ln}"]="${du_standalone_fn_name}"
	done <<<"${du_standalone_fn_at_line_raw}"

	local du_standalone_report_lines du_standalone_assert_lines
	du_standalone_report_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_report_intents_after_maintenance "${compose_project}"' "${deployable_unit_live_lib}" | cut -d: -f1 || true))
	du_standalone_assert_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}"' "${deployable_unit_live_lib}" | cut -d: -f1 || true))

	local du_standalone_report_line="" du_standalone_assert_line="" du_ln2
	for du_ln2 in "${du_standalone_report_lines[@]}"; do
		[[ "${du_standalone_fn_at_line[${du_ln2}]:-}" == "ifa_deployable_unit_live_run_standalone_cell" ]] || continue
		du_standalone_report_line="${du_ln2}"
		break
	done
	for du_ln2 in "${du_standalone_assert_lines[@]}"; do
		[[ "${du_standalone_fn_at_line[${du_ln2}]:-}" == "ifa_deployable_unit_live_run_standalone_cell" ]] || continue
		du_standalone_assert_line="${du_ln2}"
		break
	done
	[[ "${du_standalone_report_line}" =~ ^[0-9]+$ && "${du_standalone_assert_line}" =~ ^[0-9]+$ \
		&& "${du_standalone_report_line}" -lt "${du_standalone_assert_line}" ]] \
		|| fail "deployable-unit standalone cell: post-maintenance intents diagnostic (line ${du_standalone_report_line}) must run before the cell's final edge assertion (line ${du_standalone_assert_line}) inside ifa_deployable_unit_live_run_standalone_cell in ${deployable_unit_live_lib}"
	# Fault cells need this diagnostic MORE than the standalone cell, not
	# less: a fault-cell failure has four candidate causes (writer drop,
	# correlation produced nothing, the injected fault did not recover, or
	# recovery raced the assertion) where the standalone cell only has two,
	# and the probe collapses the two hardest to tell apart. All three fault
	# cells (baseline, killworker, failgraphwrite) already call
	# ifa_deployable_unit_live_assert_empty_before_maintenance and run a
	# maintenance pass -- the after-probe was the only missing piece.
	#
	# A bare count==3 plus index-paired ordering (report[i] < assert[i] by
	# SORTED position) is satisfiable by moving all three report calls into
	# ONE cell and leaving the other two with none: count stays 3, and
	# index-pairing holds trivially because all three report calls still
	# precede all three assert calls textually, even though two cells have no
	# diagnostic at all -- probed and confirmed against this exact shape in a
	# scratch copy before writing this check. Containment fixes it: map every
	# line to its nearest preceding top-level function definition
	# (`^[A-Za-z_][A-Za-z0-9_]*\(\) \{$`) and require, for EACH of the three
	# required cells, that cell contain EXACTLY one report call and EXACTLY
	# one assert call, with the report call preceding the assert call --
	# this subsumes the count check entirely (replacing it, not adding
	# alongside it) and cannot be satisfied by clustering calls in one cell.
	local du_fn_at_line_raw du_fn_ln du_fn_name
	local -A du_fn_at_line
	du_fn_at_line_raw="$(awk '
		/^[A-Za-z_][A-Za-z0-9_]*\(\) \{$/ { sub(/\(\) \{$/, ""); fn = $0 }
		{ print NR, (fn == "" ? "NONE" : fn) }
	' "${deployable_unit_cells_lib}")"
	while read -r du_fn_ln du_fn_name; do
		du_fn_at_line["${du_fn_ln}"]="${du_fn_name}"
	done <<<"${du_fn_at_line_raw}"

	local du_report_lines du_assert_lines
	du_report_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_report_intents_after_maintenance "${FAULT_COMPOSE_PROJECT}"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true))
	du_assert_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true))

	local du_cell du_ln du_report_line du_assert_line
	for du_cell in cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit; do
		du_report_line=""
		for du_ln in "${du_report_lines[@]}"; do
			[[ "${du_fn_at_line[${du_ln}]:-}" == "${du_cell}" ]] || continue
			[[ -z "${du_report_line}" ]] \
				|| fail "deployable-unit fault cell ${du_cell}: post-maintenance intents diagnostic wired more than once (lines ${du_report_line} and ${du_ln}) in ${deployable_unit_cells_lib}"
			du_report_line="${du_ln}"
		done
		[[ -n "${du_report_line}" ]] \
			|| fail "post-maintenance intents diagnostic must be wired inside ${du_cell}, not merely present ${#du_report_lines[@]} time(s) somewhere in ${deployable_unit_cells_lib}"

		du_assert_line=""
		for du_ln in "${du_assert_lines[@]}"; do
			[[ "${du_fn_at_line[${du_ln}]:-}" == "${du_cell}" ]] || continue
			[[ -z "${du_assert_line}" ]] \
				|| fail "deployable-unit fault cell ${du_cell}: final edge assertion wired more than once (lines ${du_assert_line} and ${du_ln}) in ${deployable_unit_cells_lib}"
			du_assert_line="${du_ln}"
		done
		[[ -n "${du_assert_line}" ]] \
			|| fail "expected a final deployable_unit_edges assert-edges call inside ${du_cell} in ${deployable_unit_cells_lib}"

		[[ "${du_report_line}" -lt "${du_assert_line}" ]] \
			|| fail "deployable-unit fault cell ${du_cell}: post-maintenance intents diagnostic (line ${du_report_line}) must run before that cell's own final edge assertion (line ${du_assert_line}) in ${deployable_unit_cells_lib}"
	done
	# Readiness-opened observable (readiness-review follow-up, #5993 item 1):
	# baseline already called ifa_deployable_unit_live_assert_readiness_opened;
	# killworker and failgraphwrite did not, even though they are exactly the
	# cells where an injected fault could plausibly stop readiness opening --
	# without it, a C=0/D="got 0" result there cannot distinguish "the fault
	# prevented readiness" from "correlation produced nothing", the ambiguity
	# this observable exists to remove. Pinned the same way as the
	# intents-diagnostic wiring above, reusing the SAME du_fn_at_line map: the
	# exact call text embeds the cell-specific reducer-log label, so a
	# copy-pasted call using the WRONG cell's label already fails a plain text
	# match, and the containment check on top of that catches a call landing
	# in the wrong cell entirely.
	local -A du_readiness_label=(
		[cell_baseline_deployable_unit]="reducer-baseline_deployable_unit"
		[cell_killworker_deployable_unit]="reducer-killworkerdeployableunit-before"
		[cell_failgraphwrite_deployable_unit]="reducer-failgraphwritedeployableunit"
	)
	local du_readiness_line
	for du_cell in cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit; do
		du_readiness_line="$(rg -n --fixed-strings -- "ifa_deployable_unit_live_assert_readiness_opened \"\${log_dir}\" \"${du_readiness_label[${du_cell}]}\"" "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
		[[ "${du_readiness_line}" =~ ^[0-9]+$ ]] \
			|| fail "${du_cell} must call ifa_deployable_unit_live_assert_readiness_opened with label \"${du_readiness_label[${du_cell}]}\" in ${deployable_unit_cells_lib}"
		[[ "${du_fn_at_line[${du_readiness_line}]:-}" == "${du_cell}" ]] \
			|| fail "${du_cell}'s readiness-opened call (line ${du_readiness_line}) is not inside ${du_cell} in ${deployable_unit_cells_lib}"
	done
	# Item 4's two residual-junction separators, wired into all three fault
	# cells and containment-checked the same way as the post-maintenance
	# intents diagnostic and the readiness-opened observable above. The
	# resolved-DEPLOYS_FROM-count call is textually identical across all three
	# cells (same args as the intents diagnostic), so it needs the
	# du_fn_at_line containment check, not a label-text match; the
	# correlation-reopen call embeds each cell's own pass_label, so it is
	# checked the same way as the readiness-opened label above.
	local -A du_pass_label=(
		[cell_baseline_deployable_unit]="baseline_deployable_unit"
		[cell_killworker_deployable_unit]="killworkerdeployableunit"
		[cell_failgraphwrite_deployable_unit]="failgraphwritedeployableunit"
	)
	local du_resolved_lines du_resolved_line du_reopen_line
	du_resolved_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_report_resolved_deploys_from_count "${FAULT_COMPOSE_PROJECT}"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true))
	for du_cell in cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit; do
		du_resolved_line=""
		for du_ln in "${du_resolved_lines[@]}"; do
			[[ "${du_fn_at_line[${du_ln}]:-}" == "${du_cell}" ]] || continue
			du_resolved_line="${du_ln}"
			break
		done
		[[ -n "${du_resolved_line}" ]] \
			|| fail "resolved-DEPLOYS_FROM-count diagnostic must be wired inside ${du_cell} in ${deployable_unit_cells_lib}"

		du_reopen_line="$(rg -n --fixed-strings -- "ifa_deployable_unit_live_report_correlation_reopen \"\${log_dir}\" \"${du_pass_label[${du_cell}]}\"" "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
		[[ "${du_reopen_line}" =~ ^[0-9]+$ ]] \
			|| fail "${du_cell} must call ifa_deployable_unit_live_report_correlation_reopen with pass_label \"${du_pass_label[${du_cell}]}\" in ${deployable_unit_cells_lib}"
		[[ "${du_fn_at_line[${du_reopen_line}]:-}" == "${du_cell}" ]] \
			|| fail "${du_cell}'s correlation-reopen call (line ${du_reopen_line}) is not inside ${du_cell} in ${deployable_unit_cells_lib}"
	done
	# The dispatch-anchor loop (ifa_fault_shard_run-wrapped invocation of the
	# trio) and the baseline-dispatches-first ordering check moved to
	# scripts/lib/test-ifa-fault-injection-shard-cases.sh
	# (run_ifa_fault_injection_deployable_unit_ordering_cases) to keep this
	# file under the line cap -- extraction, not deletion; every comment
	# there is the full, unabridged rationale, including why the ordering
	# check matters to the shard partitioner's one atomic group.
	run_ifa_fault_injection_deployable_unit_ordering_cases "${script}"
	# The generalized twin: proves the same baseline-before-members ordering for
	# EVERY atomic group, so a family that declares one (codeowners today, more
	# later) is covered without editing this file.
	run_ifa_fault_injection_atomic_group_ordering_cases "${script}"
	require_deployable_unit_cells "baseline cell captures digests[baseline_deployable_unit]" "capture_digest baseline_deployable_unit"
	# Same defect as cell 1's baseline: the :-0 default line matched the bare
	# needle, so the real count could vanish and leave the retry proof vacuous.
	require_deployable_unit_cells "baseline cell captures the retry baseline" 'baseline_deployable_unit_retried="$(ifa_fault_count_retried'
	require_deployable_unit_cells "pre-maintenance drain before the maintenance pass" "ifa_deployable_unit_live_assert_empty_before_maintenance"
	require_deployable_unit_cells "maintenance pass invocation" "ifa_deployable_unit_live_run_maintenance_pass"
	require_deployable_unit_cells "kill cell scopes the claimed-row wait to deployable_unit_correlation" '"deployable_unit_correlation")"'
	require_deployable_unit_cells "kill cell proves a retry above the family-scoped baseline" '"${baseline_deployable_unit_retried}"'
	require_deployable_unit_cells "graph-write cell selects queue-retry" '"queue-retry"'
	require_deployable_unit_cells "graph-write cell reads the durable marker, not a log" "ifa_fault_assert_once_fault_marker"
	require_deployable_unit_cells "fault cells compare against the family-scoped baseline, not the shared one" "assert_matches_baseline killworkerdeployableunit baseline_deployable_unit"
	# #6149 P1-2: the kill-worker cell's lock target took THREE tries.
	#   1. shared_projection_intents: this handler never writes it, so the
	#      lock never blocked anything -- failed in CI on a race.
	#   2. graph_projection_phase_state (Handle's last write): the lock DID
	#      engage, but that table has 14 writing handlers against this gate's
	#      4 reducer workers, so the shared pool starved and this family's row
	#      was never claimed at all -- failed a live run a different way.
	#   3. admission_decisions (Handle's second write, still after the graph
	#      write): only 3 writing handlers exist and this gate drives only
	#      this one, so starvation is impossible by construction. That is the
	#      live target, in the split-out lock lib
	#      (ifa_fault_injection_deployable_unit_lock.sh, #6149), not the
	#      cells lib.
	require_deployable_unit_lock_lib "kill cell lock helper targets admission_decisions, not graph_projection_phase_state or shared_projection_intents" "LOCK TABLE admission_decisions IN ACCESS EXCLUSIVE MODE"
	require_deployable_unit_lock_lib "lock-acquired poll checks the admission_decisions relation" "l.relation = 'admission_decisions'::regclass"
	require_deployable_unit_lock_lib "lock helper named for the table it actually holds" "ifa_deployable_unit_start_admission_decisions_lock() {"
	require_deployable_unit_lock_lib "release helper renamed to match" "ifa_deployable_unit_release_admission_decisions_lock() {"
	require_deployable_unit_cells "kill cell calls the renamed start helper" 'ifa_deployable_unit_start_admission_decisions_lock "killworkerdeployableunit" lock_holder_pid'
	require_deployable_unit_cells "kill cell calls the renamed release helper" 'ifa_deployable_unit_release_admission_decisions_lock "killworkerdeployableunit" "${lock_holder_pid}"'
	rg --quiet -- 'ifa_deployable_unit_start_intent_lock|ifa_deployable_unit_release_intent_lock|ifa_deployable_unit_start_phase_state_lock|ifa_deployable_unit_release_phase_state_lock' \
		"${deployable_unit_lock_lib}" "${deployable_unit_cells_lib}" \
		&& fail "an earlier-era lock helper name (shared_projection_intents or graph_projection_phase_state) must not survive"
	rg --quiet -- 'LOCK TABLE shared_projection_intents|LOCK TABLE graph_projection_phase_state' "${deployable_unit_lock_lib}" \
		&& fail "the kill-worker cell must not lock shared_projection_intents or graph_projection_phase_state any more (see #6149) in ${deployable_unit_lock_lib}"
	# The header must state honestly which recovery case this proves: a
	# kill AFTER the graph write (the lock lands after step 1 of Handle), not
	# before it -- do not let this cell claim the stronger pre-write case.
	require_framing "lock helper states the post-write-death consequence honestly" "a POST-write death, not a PRE-write death" "${deployable_unit_lock_lib}"
	# The permanent precondition gate (#6149): replaces a one-off manual
	# pre-check with an assertion that runs every time, in the baseline cell,
	# proving admission_decisions genuinely receives a row for this domain
	# before the fault cells rely on it as a lock target.
	require_deployable_unit_lock_lib "precondition gate definition" "ifa_deployable_unit_require_admission_decisions_written() {"
	require_deployable_unit_lock_lib "precondition gate query" "SELECT count(*) FROM admission_decisions WHERE domain = 'deployable_unit_correlation';"
	require_deployable_unit_lock_lib "precondition gate fails closed on query failure" 'return "${admission_count_rc}"'
	require_deployable_unit_lock_lib "precondition gate treats empty output as unknown, not zero" "admission_decisions precondition query returned empty output; treat that as unknown, not as zero"
	require_deployable_unit_lock_lib "precondition gate treats non-numeric output as unknown, not zero" "admission_decisions precondition query returned non-numeric output"
	require_deployable_unit_lock_lib "precondition gate fails on a genuine zero, not just unknowns" 'if [[ "${admission_count}" == "0" ]]; then'
	require_deployable_unit_cells "baseline cell wires the precondition gate" 'ifa_deployable_unit_require_admission_decisions_written \'
	# Ordering: the precondition gate must run inside the baseline cell,
	# after the post-maintenance drain (so Handle has actually run to
	# completion), same containment style as the other per-cell wiring
	# checks above.
	local du_baseline_fn_at_line_raw du_baseline_fn_ln du_baseline_fn_name
	local -A du_baseline_fn_at_line
	du_baseline_fn_at_line_raw="$(awk '
		/^[A-Za-z_][A-Za-z0-9_]*\(\) \{$/ { sub(/\(\) \{$/, ""); fn = $0 }
		{ print NR, (fn == "" ? "NONE" : fn) }
	' "${deployable_unit_cells_lib}")"
	while read -r du_baseline_fn_ln du_baseline_fn_name; do
		du_baseline_fn_at_line["${du_baseline_fn_ln}"]="${du_baseline_fn_name}"
	done <<<"${du_baseline_fn_at_line_raw}"
	local du_precondition_line du_maintenance_drain_line
	du_precondition_line="$(rg -n --fixed-strings -- 'ifa_deployable_unit_require_admission_decisions_written \' "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
	du_maintenance_drain_line="$(rg -n --fixed-strings -- 'ifa_deployable_unit_live_run_maintenance_pass "baseline_deployable_unit"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
	[[ "${du_precondition_line}" =~ ^[0-9]+$ && "${du_maintenance_drain_line}" =~ ^[0-9]+$ \
		&& "${du_baseline_fn_at_line[${du_precondition_line}]:-}" == "cell_baseline_deployable_unit" \
		&& "${du_precondition_line}" -gt "${du_maintenance_drain_line}" ]] \
		|| fail "ifa_deployable_unit_require_admission_decisions_written must be called inside cell_baseline_deployable_unit, after the maintenance pass, in ${deployable_unit_cells_lib}"

	# #6149 review: ifa_deployable_unit_require_fresh_intents queried
	# shared_projection_intents (always 0 for this family, same vacuous shape
	# as the BEFORE probe P1-1 fixed) even after the review that established
	# why. Repointed to graph-dump + jq, mirroring the BEFORE probe exactly.
	rg --quiet --fixed-strings -- 'SELECT count(*) FROM shared_projection_intents' "${deployable_unit_cells_lib}" \
		&& fail "ifa_deployable_unit_require_fresh_intents must not query shared_projection_intents any more (vacuous for this family -- see #6149) in ${deployable_unit_cells_lib}"
	require_deployable_unit_cells "fresh-intents precondition dumps the graph, not a Postgres query" '"${bin_dir}/eshu-ifa" graph-dump -out "${ifa_deployable_unit_fresh_stack_dump_path}"'
	require_deployable_unit_cells "fresh-intents precondition counts CORRELATES_DEPLOYABLE_UNIT edges via jq" 'select(.type == "CORRELATES_DEPLOYABLE_UNIT")'
	require_deployable_unit_cells "fresh-intents precondition uses its own distinctly-named global dump path, not local" 'ifa_deployable_unit_fresh_stack_dump_path="$(mktemp)"'
	rg --quiet --fixed-strings -- 'local dump_path' "${deployable_unit_cells_lib}" \
		&& fail "fresh-intents precondition's graph-dump scratch path must not be declared local in ${deployable_unit_cells_lib} -- its RETURN trap references it after the local binding would already be torn down (#6149 live-run abort, same class as the BEFORE probe's earlier fix)"
	# All three call sites now pass only cell + bin_dir -- the Postgres args
	# (compose_project/use_compose/dsn/compose_file) the old query needed are
	# gone from every call site, not just the definition.
	local du_fresh_intents_call_count
	du_fresh_intents_call_count="$(rg --fixed-strings --count-matches -- 'ifa_deployable_unit_require_fresh_intents "' "${deployable_unit_cells_lib}" || true)"
	[[ "${du_fresh_intents_call_count}" -eq 3 ]] \
		|| fail "expected ifa_deployable_unit_require_fresh_intents to be called with only cell + bin_dir at all 3 call sites (baseline, killworker, failgraphwrite); found ${du_fresh_intents_call_count:-0}"
}
