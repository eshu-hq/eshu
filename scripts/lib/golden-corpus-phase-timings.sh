#!/usr/bin/env bash
#
# golden-corpus-phase-timings.sh — B-11 (#3804) helper for the golden corpus
# gate orchestrator (scripts/verify-golden-corpus-gate.sh). Extracted into a lib
# chunk so the orchestrator stays under the 500-line cap as gate phases accrue.
#
# emit_phase_timings_and_flags writes the observed per-phase wall-clock to
# phase-timings.json and decides the per-phase regression flags for the gate
# binary. It reads the phase_* epoch globals the orchestrator captures inline and
# exports two globals back: phase_timings_file (path) and phase_flags (array).
#
# Requires (set by the orchestrator before the call): log_dir, repo_root, log(),
# die(), and the phase boundary epochs phase_bootstrap_start/end,
# phase_collect_start/end, phase_first_drain_start/end,
# phase_maintenance_start/end, phase_graph_query_start.
#
# Optionally phase_graph_query_excluded_starts/phase_graph_query_excluded_ends
# (bash ARRAYS, see the accumulation note below); both default to empty so a
# caller that brackets nothing still emits a sane phase. Each array must carry
# the same number of entries -- one exclusion bracket per matching pair of
# elements at the same index, opened with `phase_graph_query_excluded_starts+=(
# "$(date +%s)")` and closed with `phase_graph_query_excluded_ends+=("$(date
# +%s)")` around the work to exclude.
#
# # Why arrays, not a single start/end pair (#5837 round-8)
#
# The single-pair scalar predecessor of this interface
# (phase_graph_query_excluded_start/_end, two plain variables) could not
# accumulate: if the orchestrator ever needed to exclude a SECOND span of
# assertion work from the same phase, re-assigning the same two variables for
# the second bracket would silently DISCARD the first bracket's span --
# under-subtraction, no error, every gate green, and the phase would go back to
# billing that first excluded span to pipeline work. Arrays let each bracket
# append its own pair without touching any other bracket's entries, and the
# accounting below sums every pair rather than trusting there is only one.

emit_phase_timings_and_flags() {
	# The graph_query phase is bounded here (API + MCP startup), deliberately
	# excluding the gate's own assertion time — that is gate overhead, not pipeline
	# work.
	local phase_graph_query_end phase_graph_query_window phase_graph_query_excluded
	local -a excluded_starts excluded_ends
	local excluded_count excluded_i bracket_span
	phase_graph_query_end="$(date +%s)"

	# #5837: assertion work that runs BETWEEN a bracket's open and close stamps has
	# to be subtracted, or it is silently billed to pipeline startup. #5465's
	# suppression producer proof sits inside one such bracket and is floored at a
	# fixed 20s by its own expiry wait (golden_suppression_expiry_epoch = now +
	# 20), so counting it pushes graph_query past its 8s effective ceiling (3s
	# baseline + 5s absolute_slack_seconds) on ANY host — 20 > 8 by construction, a
	# required failure with no pipeline slowdown behind it. See
	# docs/internal/evidence/5837-aws-drift-reopen.md, "Golden-gate phase-timing
	# note"; the ~23s figure quoted elsewhere is illustrative, not captured.
	# The orchestrator brackets that proof; everything bracketed is excluded here.
	#
	# #5837 P2 review: this used to clamp a negative excluded span to 0 and emit
	# whatever fell out, silently. go/cmd/golden-corpus-gate/timing.go only
	# asserts observedSecs <= ceiling (an upper bound), so an emitted NEGATIVE
	# phase duration passed a gated, non-advisory check with nobody looking.
	# Reproduced: over-exclusion emitted graph_query=-7, and an _end stamp set
	# with _start deleted emitted roughly -1.7e9 (an unset epoch defaults to 0,
	# so the subtraction reads as "excluded from the Unix epoch"). Both are
	# impossible accounting states, not measurements, so they die here instead of
	# emitting a number. A caller that brackets nothing (both arrays empty) keeps
	# emitting the plain window exactly as before this check existed.
	#
	# The `${arr[@]+"${arr[@]}"}` form below is deliberate, not decorative: under
	# `set -u` (which the orchestrator turns on), a bare `"${arr[@]}"` on an
	# array the caller never declared aborts the whole gate instead of treating
	# "no brackets" as zero brackets. `${arr[@]+...}` only expands when the array
	# is set, so an undeclared array collapses to nothing rather than erroring.
	excluded_starts=(${phase_graph_query_excluded_starts[@]+"${phase_graph_query_excluded_starts[@]}"})
	excluded_ends=(${phase_graph_query_excluded_ends[@]+"${phase_graph_query_excluded_ends[@]}"})

	if (( ${#excluded_starts[@]} != ${#excluded_ends[@]} )); then
		die "phase_graph_query_excluded_starts/_ends have different lengths (${#excluded_starts[@]} vs ${#excluded_ends[@]}); every exclusion bracket must open and close in a matching pair, and an unequal count means one bracket in this run never closed (or never opened) (#5837)"
	fi

	phase_graph_query_window=$(( phase_graph_query_end - phase_graph_query_start ))
	phase_graph_query_excluded=0
	excluded_count=${#excluded_starts[@]}
	for (( excluded_i = 0; excluded_i < excluded_count; excluded_i++ )); do
		bracket_span=$(( excluded_ends[excluded_i] - excluded_starts[excluded_i] ))
		if (( bracket_span < 0 )); then
			die "phase_graph_query exclusion bracket #${excluded_i} has a negative span (${bracket_span}s, start=${excluded_starts[excluded_i]} end=${excluded_ends[excluded_i]}); its own end stamp precedes its own start stamp (#5837)"
		fi
		phase_graph_query_excluded=$(( phase_graph_query_excluded + bracket_span ))
	done
	if (( phase_graph_query_excluded > phase_graph_query_window )); then
		die "phase_graph_query total excluded span (${phase_graph_query_excluded}s across ${excluded_count} bracket(s)) does not fit inside the ${phase_graph_query_window}s measured window (start=${phase_graph_query_start} end=${phase_graph_query_end}); the orchestrator's exclusion brackets are wider in total than the phase they correct (#5837)"
	fi

	phase_timings_file="${log_dir}/phase-timings.json"
	cat >"${phase_timings_file}" <<JSON
{
  "schema_version": "1",
  "phases": {
    "bootstrap": $(( phase_bootstrap_end - phase_bootstrap_start )),
    "collect": $(( phase_collect_end - phase_collect_start )),
    "first_drain": $(( phase_first_drain_end - phase_first_drain_start )),
    "maintenance_drains": $(( phase_maintenance_end - phase_maintenance_start )),
    "graph_query": $(( phase_graph_query_window - phase_graph_query_excluded ))
  }
}
JSON
	log "per-phase timings: $(tr -d '\n ' <"${phase_timings_file}")"

	# Wire the macro per-phase regression check only when the baseline exists. The
	# first capture run (no baseline yet) still emits phase-timings.json above so
	# the baseline can be seeded from it; LoadPhaseBaseline would otherwise fail the
	# gate. Default to advisory because the default runner is shared CI, whose
	# hardware variance exceeds the band; a controlled validation host sets
	# GATE_PHASE_REGRESSION_ADVISORY=false to make the per-phase check blocking.
	local phase_baseline="${GATE_PHASE_BASELINE:-testdata/golden/e2e-baseline.json}"
	phase_flags=()
	if [[ -f "${repo_root}/${phase_baseline}" || -f "${phase_baseline}" ]]; then
		phase_flags+=(-phase-timings-file="${phase_timings_file}" -phase-baseline-file="${phase_baseline}")
		if [[ "${GATE_PHASE_REGRESSION_ADVISORY:-true}" == "true" ]]; then
			phase_flags+=(-phase-regression-advisory)
		fi
	else
		log "no phase baseline at ${phase_baseline}; emitted phase-timings.json for seeding (per-phase check skipped)"
	fi
}
