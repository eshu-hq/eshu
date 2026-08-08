#!/usr/bin/env bash
# shellcheck disable=SC2034  # The reducer_/projector_pid locals are filled
# indirectly by ifa_det_start_bg via printf -v, so shellcheck sees the
# declaration but not the write.
# shellcheck disable=SC2154  # This file is sourced by
# scripts/verify-ifa-fault-injection.sh and reads globals it owns (bin_dir,
# log_dir, work_dir, use_compose, compose_file, wall_times, digests, and the
# sql_* cassette/expected-set paths). Linting it standalone would otherwise
# bury a genuinely new SC2154 in ~20 expected ones.
#
# The two delivery-shaped fault cells from issue #5544, split from #5351's
# original design. Both live here rather than in ifa_fault_injection_cells.sh
# to keep every library under the repo's 500-line cap
# (.agents/skills/generator-script-discipline), matching the split that
# ifa_fault_injection_sql_cells.sh already established for #5555's cells.
#
# NOTE ON CELL NUMBERING: #5544 calls these "cell 6" and "cell 7", numbering
# them after the five original cells. #5555 independently numbered its SQL
# cells 6 and 7 in scripts/verify-ifa-fault-injection.sh's header. The numbers
# collide across issues, so these functions are named for what they do rather
# than where they sit in a sequence -- a positional name would be wrong from
# the day another cell lands.
#
# Like its sibling libraries this is a plain function library, not a script
# (no `set -euo pipefail`; see ifa_fault_injection_driver.sh's identical note).

# cell_duplicatedelivery (#5544 cell 6) proves the materialization write path
# is idempotent under redelivery: the same work item delivered twice must
# converge to the same graph, not double-write edges or dead-letter.
#
# It drains once cleanly, then forces every succeeded reducer row back to
# 'pending' in SQL -- the cell_expirelease precedent, which likewise perturbs
# fact_work_items directly rather than killing a process -- and drains again.
# A queue that redelivers an already-succeeded item is the real-world case
# (at-least-once delivery, a lease that expired after the handler committed
# but before the ack landed); this reproduces it deterministically instead of
# waiting to lose that race in CI.
#
# NON-VACUITY: the redelivery UPDATE must actually match rows. If it matched
# none, the second drain would be a no-op, the digest would trivially equal
# the baseline, and the cell would pass while proving nothing -- the inert-gate
# defect #5555 and #5974 exist to remove. The reset count is asserted > 0
# before the second drain, so an UPDATE that stops matching (a status rename, a
# schema change, a stage rename) fails this cell loudly instead of greening it.
cell_duplicatedelivery() {
	local cell_start
	cell_start=$(date +%s)
	log "cell duplicate-delivery: fresh stack"
	fresh_stack duplicatedelivery
	drive_all_cassettes duplicatedelivery
	local projector_pid reducer_pid reset_count
	ifa_det_start_bg "${log_dir}" "projector-duplicatedelivery" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-duplicatedelivery" reducer_pid "${bin_dir}/eshu-reducer"

	# First drain: establish the fully-materialized, fault-free state that the
	# redelivery below has to be idempotent against.
	run_drain_gate duplicatedelivery
	assert_no_dead_letters duplicatedelivery

	log "duplicate-delivery: force every succeeded reducer row back to pending (redelivery, SQL, no kill)"
	reset_count="$(ifa_fault_redeliver_succeeded "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
		"${ESHU_POSTGRES_DSN}" "${compose_file}")" \
		|| die "duplicate-delivery: redelivery UPDATE failed"
	[[ -n "${reset_count}" && "${reset_count}" -gt 0 ]] \
		|| die "duplicate-delivery: no succeeded reducer rows were redelivered -- non-vacuous precondition failed, the second drain would be a no-op and this cell would pass while proving nothing"
	printf 'duplicate-delivery: non-vacuous: %s succeeded reducer row(s) redelivered as pending\n' "${reset_count}"

	# Second drain: the redelivered work is reprocessed end to end.
	run_drain_gate duplicatedelivery
	assert_no_dead_letters duplicatedelivery
	capture_digest duplicatedelivery

	# The absolute-set assertion matters here for the same reason it does in
	# cell_baseline: digest equality alone is satisfied by empty == empty. A
	# redelivery that dropped the SQL family entirely would still match a
	# baseline that never had it.
	log "duplicate-delivery: assert SQL relationship family materialized edges (absolute set, non-vacuity)"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}" \
		|| die "duplicate-delivery: SQL relationship family materialized edge set did not match the expected set after redelivery -- redelivery either dropped or duplicated edges; do NOT normalize this away"

	assert_matches_baseline duplicatedelivery
	teardown_cell duplicatedelivery
	wall_times[duplicatedelivery]=$(( $(date +%s) - cell_start ))
	printf 'duplicate-delivery: cell wall time: %ss\n' "${wall_times[duplicatedelivery]}"
}

# cell_deltaretract (#5544 cell 7) proves a second generation's retract lands
# correctly under this gate: generation 2 of the SQL family reuses the same
# source_run_id, retracts what generation 1 asserted, and the accumulated edge
# set must match the committed expected-v2 fixture exactly -- proving the
# retract fired AND that nothing still-valid was lost with it.
#
# The drive/drain/assert body is ifa_det_run_sql_delta_live, the same helper
# scripts/verify-ifa-determinism.sh calls, so the two gates cannot drift on
# what "the delta landed correctly" means.
#
# WHY THIS CELL DOES NOT COMPARE TO THE BASELINE DIGEST: every other cell in
# this matrix injects a fault and asserts the graph is UNCHANGED. This one
# deliberately changes the graph -- generation 2 adds and retracts edges, so
# the post-delta digest is expected to differ from the fault-free generation-1
# baseline. Calling assert_matches_baseline here would fail correctly and
# tempt exactly the wrong fix. Its exactness assertion is the expected-v2 set,
# which is strictly stronger than a digest comparison: it names the edges.
cell_deltaretract() {
	local cell_start
	cell_start=$(date +%s)
	log "cell delta-retract: fresh stack"
	fresh_stack deltaretract
	drive_all_cassettes deltaretract
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-deltaretract" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-deltaretract" reducer_pid "${bin_dir}/eshu-reducer"

	# Generation 1 must be fully materialized before the delta is driven,
	# otherwise "the delta retracted it" is indistinguishable from "generation
	# 1 never landed".
	run_drain_gate deltaretract
	assert_no_dead_letters deltaretract
	log "delta-retract: assert generation-1 SQL edges before driving the delta (precondition)"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}" \
		|| die "delta-retract: generation-1 SQL edge set did not match before the delta was driven -- the retract assertion below would be meaningless"

	ifa_det_run_sql_delta_live \
		1 "${bin_dir}" "${sql_delta_cassette}" "${sql_delta_expected_edges}" "${log_dir}" \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"${compose_file}" "${GATE_DRAIN_TIMEOUT}" \
		|| die "delta-retract: SQL delta-live proof failed -- the generation-2 retract did not converge to the expected accumulated edge set"

	assert_no_dead_letters deltaretract
	capture_digest deltaretract

	# The expected-v2 assertion above inspects sql_relationships ONLY, so a
	# generation 2 that also retracted edges in another domain would still pass
	# it. Compare the two canonical dumps structurally, ignoring the nine SQL
	# relationship types the delta is allowed to move.
	#
	# This is deliberately NOT a line diff. The dump is pretty-printed JSON, so
	# one edge spans many lines and the structural lines ("props": {, }, {)
	# carry no type at all -- a line filter reports them as foreign changes and
	# fails a correct run. The first version of this guard did exactly that.
	#
	# The jq walk is schema-agnostic: it collects every edge-shaped object
	# anywhere in the document rather than assuming a top-level key.
	local sql_types stray
	sql_types='["EXECUTES","HAS_COLUMN","INDEXES","MIGRATES","QUERIES_TABLE","READS_FROM","REFERENCES_TABLE","TRIGGERS","WRITES_TO"]'
	command -v jq >/dev/null 2>&1 || die "delta-retract: jq is required to compare the non-SQL graph; without it this check would silently pass"
	local non_sql_filter="[.. | objects | select(has(\"type\") and has(\"from\") and has(\"to\")) | select((${sql_types} | index(.type)) | not) | {from, to, type}] | sort"
	stray="$(diff \
		<(jq -S "${non_sql_filter}" "${work_dir}/graph-baseline.dump") \
		<(jq -S "${non_sql_filter}" "${work_dir}/graph-deltaretract.dump") || true)"
	if [[ -n "${stray}" ]]; then
		printf 'delta-retract: non-SQL edges changed between baseline and post-delta:\n%s\n' "${stray}" >&2
		die "delta-retract: generation 2 changed edges outside the nine SQL relationship types -- the expected-v2 set only covers the SQL family, so this would otherwise pass unnoticed; do NOT widen the expected set to absorb it"
	fi
	printf 'delta-retract: non-SQL edge set unchanged from baseline\n'

	teardown_cell deltaretract
	wall_times[deltaretract]=$(( $(date +%s) - cell_start ))
	printf 'delta-retract: cell wall time: %ss\n' "${wall_times[deltaretract]}"
}
