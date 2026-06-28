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
# and the phase boundary epochs phase_bootstrap_start/end, phase_collect_start/end,
# phase_first_drain_start/end, phase_maintenance_start/end, phase_graph_query_start.

emit_phase_timings_and_flags() {
	# The graph_query phase is bounded here (API + MCP startup), deliberately
	# excluding the gate's own assertion time — that is gate overhead, not pipeline
	# work.
	local phase_graph_query_end
	phase_graph_query_end="$(date +%s)"

	phase_timings_file="${log_dir}/phase-timings.json"
	cat >"${phase_timings_file}" <<JSON
{
  "schema_version": "1",
  "phases": {
    "bootstrap": $(( phase_bootstrap_end - phase_bootstrap_start )),
    "collect": $(( phase_collect_end - phase_collect_start )),
    "first_drain": $(( phase_first_drain_end - phase_first_drain_start )),
    "maintenance_drains": $(( phase_maintenance_end - phase_maintenance_start )),
    "graph_query": $(( phase_graph_query_end - phase_graph_query_start ))
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
