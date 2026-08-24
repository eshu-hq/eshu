#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# The #6149 lock-target, precondition-gate and fresh-intents structural cases
# for deployable_unit_edges, split out of
# test-ifa-fault-injection-deployable-unit-cases.sh when that file reached the
# repository's 500-line cap. Nothing here changed in the move; it is the same
# block, in the same order, called from the same place in the same narrative
# position. The parent verifier owns strict mode and fail(); the pin helpers
# (require_deployable_unit_cells, require_deployable_unit_lock_lib,
# require_framing) and the ${deployable_unit_cells_lib} /
# ${deployable_unit_lock_lib} paths are owned by the cases module that sources
# this one, and resolve at call time rather than at source time.
run_ifa_fault_injection_deployable_unit_lock_cases() {
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
