#!/usr/bin/env bash
#
# golden-corpus-maintenance-drains.sh — deferred-maintenance + drain cycles for
# the golden corpus gate orchestrator (scripts/verify-golden-corpus-gate.sh).
# Extracted into a lib chunk so the orchestrator stays under the 500-line cap.
#
# run_maintenance_drain_cycles() runs the maintenance/drain loop described
# below. Requires (set by the orchestrator before the call): bin_dir, log_dir,
# GATE_DRAIN_TIMEOUT, and the log(), die(), and start_bg() functions.

# Deferred maintenance + drain, run TWICE — the ingester's post-shard-drain
# pattern (RunDeferredRelationshipMaintenance) loops continuously; the gate
# approximates that with two cycles so additive correlation domains converge.
# Each maintenance pass re-runs bootstrap-index's post-collection phase: it
# re-backfills relationship evidence with the collector facts now present, replays
# the succeeded code_import_repo_edge work items, and replays the correlation
# domains (deployable_unit_correlation). Cycle 1's drain produces the resolved
# DEPLOYS_FROM relationships (deployment_mapping -> cross-repo resolution) and the
# cross-repo DEPENDS_ON edge; cycle 2's drain re-runs deployable_unit_correlation
# now that the resolved relationships it consumes exist, producing
# CORRELATES_DEPLOYABLE_UNIT. Collection/projection re-runs are idempotent (facts
# dedupe by stable key; schema is IF NOT EXISTS). The final cycle is the asserted
# B-7(a) drain.
#
# Three cycles, not two (#5426). supply_chain_impact reads ci_cd_run_correlation,
# which reads container_image_identity -- a three-link chain, all reopened
# concurrently in one slice, so the reopen order sequences nothing. Two cycles
# happened to suffice, but with ZERO margin: measured by running this loop with
# a single pass, the now-required assertion fails outright --
#
#   [FAIL] mcp:list_supply_chain_impact_findings:
#          result item missing required field "environment_evidence"
#
# -- while two passes go green. A required B-7 assertion sitting exactly on the
# convergence boundary reds main the first time drain ordering shifts, so the
# third cycle buys the margin the chain's depth calls for. Cost is small: the
# maintenance_drains phase measured 4s at one pass, 11s at two, and 14s at the
# three this ships, against a ~158s gate.
#
# WHAT THIS LOOP DOES AND DOES NOT PROVE (#5846). It drives eshu-bootstrap-index,
# never eshu-ingester, so it does not exercise
# RunDeferredRelationshipMaintenanceAfterShardDrain -- the entrypoint production
# actually runs. That was a false green: the correlation reopen had only the
# bootstrap caller, so these assertions passed while normal ingestion replayed
# nothing. Both runtimes now replay the same domain list
# (postgres.CrossScopeCorrelationReopenDomains) through the same SQL, so what
# these passes prove carries over to the ingester except for the call site
# itself. Covering the call site needs an eshu-ingester run here, with a
# collector source and shard/barrier configuration this gate does not have.
run_maintenance_drain_cycles() {
	local maintenance_pass
	for maintenance_pass in 1 2 3; do
		log "deferred maintenance pass ${maintenance_pass}: re-run bootstrap-index maintenance"
		"${bin_dir}/eshu-bootstrap-index" >"${log_dir}/bootstrap-index-maint-${maintenance_pass}.log" 2>&1 \
			|| { tail -40 "${log_dir}/bootstrap-index-maint-${maintenance_pass}.log"; die "deferred maintenance pass ${maintenance_pass} failed"; }

		log "B-7(a) drains — maintenance drain ${maintenance_pass} (background; gate polls to terminal)"
		# Every shared_projection_intents domain must reach terminal — including
		# code_calls, whose held-intent deadlock (#3865) is fixed. No domain is
		# quarantined as advisory. -require-populated-domains guards against premature
		# convergence: the reducer runs in the background and the poll could otherwise
		# read an empty 0/0 before it emits any intents and pass on an unreduced
		# pipeline. repo_dependency is the domain the corpus reliably produces (and the
		# B-13/#3859 gate), so the drain is accepted only once it has been observed
		# populated and then drained.
		# Do NOT add platform_infra here: -require-populated-domains counts
		# shared_projection_intents domains, but nothing emits into the platform_infra
		# shared-projection domain (EmitPlatformInfraIntents has no live caller).
		# rc-26 PROVISIONS_PLATFORM is materialized inside the deployment_mapping
		# handler (a fact_work_items domain), which the fact_work_items residual=0
		# drain assertion already gates on — so requiring platform_infra here can
		# never be satisfied and the rc-26 graph assertion already proves the edge.
		start_bg projector projector_pid "${bin_dir}/eshu-projector"
		start_bg reducer reducer_pid "${bin_dir}/eshu-reducer"
		if ! "${bin_dir}/eshu-golden-corpus-gate" \
			-phase=drains \
			-snapshot=testdata/golden/e2e-20repo-snapshot.json \
			-require-populated-domains="repo_dependency" \
			-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
			tail -30 "${log_dir}/reducer.log" || true
			tail -30 "${log_dir}/projector.log" || true
			die "maintenance drain ${maintenance_pass} failed"
		fi
		kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	done
}
