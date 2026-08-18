#!/usr/bin/env bash
# shellcheck disable=SC2154  # This file is sourced by
# scripts/verify-ifa-fault-injection.sh and reads globals it owns
# (bin_dir, log_dir, work_dir, use_compose, compose_file, wall_times,
# baseline_retried, and the *_operation_match anchors). Without this,
# linting the library standalone buries a genuinely new SC2154 in ~30
# expected ones.
# Shared per-cell driver plumbing for scripts/verify-ifa-fault-injection.sh
# (issue #4580 P6 slice S5, extended with the SQL-targeted cells issue #5555
# adds). Extracted out of the driver script itself to keep it under the
# repo's 500-line cap (.agents/skills/generator-script-discipline):
# fresh_stack, drive_all_cassettes, run_drain_gate, assert_no_dead_letters,
# capture_digest, assert_matches_baseline, and teardown_cell are the reusable
# per-cell lifecycle steps every cell function in
# scripts/lib/ifa_fault_injection_cells.sh and
# scripts/lib/ifa_fault_injection_sql_cells.sh calls in sequence.
#
# This file is a plain function library, not a script: it deliberately does
# NOT set `set -euo pipefail` (sourcing it would rebind the caller's shell
# options). Every function here reads driver-owned globals directly
# (bin_dir, log_dir, work_dir, cassette, synth_cassette, sql_cassette,
# rationale_cassette, rationale_expected_edges, rationale_delta_expected_records,
# drive_workers, compose_file, use_compose, FAULT_COMPOSE_PROJECT,
# ESHU_POSTGRES_DSN, GATE_DRAIN_TIMEOUT, digests, bg_pids, log, die) rather
# than taking them as arguments, exactly as they did before this extraction
# -- every caller is the driver script or a cell function sourced into the
# SAME shell process, never a subshell.

# fresh_stack tears down any prior cell's stack and brings up a genuinely
# fresh Postgres + NornicDB pair, then applies schema.
fresh_stack() {
	local cell="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		# The teardown outcome is NOT discarded here. It used to be (`|| true`
		# with both streams to /dev/null), which meant a failed `down -v` let the
		# following `up -d` reattach the previous cell's volumes. Because
		# shared-projection intent IDs are deterministic and completed rows are
		# never reopened, a redelivered family then produces ZERO new graph
		# writes -- yet every downstream assertion still passes on the edges that
		# are already there. The cell reports green while proving nothing. That
		# is the leading explanation for #5974.
		#
		# teardown_cell's own `down -v` (below) stays best-effort: it is cleanup
		# after a finished cell, not the freshness guarantee. This one is the
		# guarantee, so it fails loudly.
		if ! docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" down -v \
			>"${log_dir}/compose-down-${cell}.log" 2>&1; then
			tail -20 "${log_dir}/compose-down-${cell}.log" >&2 || true
			die "${cell}: docker compose down -v failed -- the stack is NOT fresh, and a stale stack makes this cell pass while proving nothing (see ${log_dir}/compose-down-${cell}.log)"
		fi
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres
		log "${cell}: wait for backends"
		ifa_det_wait_for_backends "${FAULT_COMPOSE_PROJECT}" "${compose_file}" \
			|| die "${cell}: Postgres + NornicDB did not become ready within budget"
	fi
	log "${cell}: apply Postgres + graph schema (eshu-bootstrap-data-plane)"
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/bootstrap-data-plane-${cell}.log"; die "${cell}: bootstrap-data-plane failed"; }
}

# drive_all_cassettes drives demo-org, synth-multiscope, SQL, code-call,
# documentation, and rationale family cassettes into the fresh stack and
# asserts the drive actually enqueued work (never a vacuous drain proof). The
# SQL family cassette (#5351) makes cells 2/3 (and the SQL-targeted cells
# #5555 adds) exercise the SQL relationship materialization handler's replay
# through the real durable fault path, not only the GCP resource path.
#
# The deployable-unit family cassette (#5993) is deliberately NOT driven here.
# An earlier version of this comment claimed driving it unconditionally into
# every cell was safe because deployable_unit_correlation's intent finds
# nothing admitted and succeeds immediately without a maintenance pass. That
# claim covered only the drain (residual=0) and the digest -- it never
# reached duplicate-delivery's redelivery UPDATE (`WHERE stage = 'reducer' AND
# status = 'succeeded'`, ifa_fault_redeliver_succeeded), which touches every
# succeeded reducer row regardless of what it admitted, so the extra
# deployable_unit_correlation row this family enqueues into cells that never
# maintenance-pass it was a real, unproven surface, not a covered one (#5993
# review; see cell_duplicatedelivery's live failure on ifa_det_pg's
# now-surfaced stderr for the trigger). drive_deployable_unit_cassette below
# is called only by the three deployable_unit-targeted cells
# (ifa_fault_injection_deployable_unit_cells.sh) that actually need this
# family's rows to exist.
drive_all_cassettes() {
	local cell="$1"
	log "${cell}: drive demo-org cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-${cell}.log" >&2; die "${cell}: eshu-ifa drive (demo-org) failed"; }
	log "${cell}: drive synth-multiscope cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${synth_cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-synth-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-synth-${cell}.log" >&2; die "${cell}: eshu-ifa drive (synth-multiscope) failed"; }
	log "${cell}: drive SQL relationship family cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${sql_cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-sql-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-sql-${cell}.log" >&2; die "${cell}: eshu-ifa drive (SQL relationship family) failed"; }
	ifa_code_call_drive "${cell}" "${bin_dir}" "${code_call_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "${cell}: eshu-ifa drive (code-call family) failed"
	ifa_documentation_drive "${cell}" "${bin_dir}" "${documentation_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "${cell}: eshu-ifa drive (documentation family) failed"
	ifa_rationale_drive "${cell}" "${bin_dir}" "${rationale_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "${cell}: eshu-ifa drive (rationale family) failed"
	local enqueued
	enqueued="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		'SELECT count(*) FROM fact_work_items;' "${compose_file}" | tr -d '[:space:]')"
	[[ -n "${enqueued}" && "${enqueued}" -gt 0 ]] \
		|| die "${cell}: eshu-ifa drive committed but enqueued 0 fact_work_items rows (vacuous drain proof)"
	printf '%s: fact_work_items enqueued: %s\n' "${cell}" "${enqueued}"
}

# drive_deployable_unit_cassette drives the deployable-unit family cassette
# (#5993) into the fresh stack. Called only by the three deployable_unit-
# targeted cells (cell_baseline_deployable_unit, cell_killworker_deployable_unit,
# cell_failgraphwrite_deployable_unit in
# ifa_fault_injection_deployable_unit_cells.sh), immediately after
# drive_all_cassettes -- never unconditionally by every cell, so this family's
# extra succeeded reducer row cannot enlarge duplicate-delivery's redelivery
# UPDATE or any other sibling cell's row set. See drive_all_cassettes' header
# comment for why unconditional driving stopped being safe.
drive_deployable_unit_cassette() {
	local cell="$1"
	log "${cell}: drive deployable-unit family cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${deployable_unit_cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-deployable-unit-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-deployable-unit-${cell}.log" >&2; die "${cell}: eshu-ifa drive (deployable-unit family) failed"; }
}

# assert_rationale_truth binds every cell to an absolute three-record graph
# oracle and the exact durable lifecycle. Digest equality alone cannot detect
# a family that is missing from the baseline and every recovery cell.
assert_rationale_truth() {
	local cell="$1"
	ifa_rationale_assert "${cell}" "${bin_dir}" "${rationale_expected_edges}" \
		|| die "${cell}: rationale graph does not match the three-record exact set"
	ifa_rationale_assert_work_counts "${cell}" "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
		"${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${ifa_rationale_generation_id}" "${ifa_rationale_expected_tuple}" \
		|| die "${cell}: rationale durable lifecycle does not match the exact fixture counts"
}

# assert_rationale_delta_truth is used only after delta-retract's combined SQL
# and rationale delta drain. The other twenty cells remain bound to the
# rationale baseline three-record graph and exact durable tuple through
# run_drain_gate.
assert_rationale_delta_truth() {
	local cell="$1"
	ifa_rationale_assert_delta_truth "${cell}" "${bin_dir}" \
		"${rationale_delta_expected_records}" "${work_dir}/rationale-delta-${cell}.dump" \
		|| die "${cell}: rationale delta truth is not exact-one plus the exact Charge survivor node"
}

# run_drain_gate polls the gate binary to the B-12 residual bound (0), which
# folds in this gate's own dead_letter=0 requirement: factWorkItemsResidualSQL
# (go/cmd/golden-corpus-gate/drains.go) counts a dead_letter row AS residual,
# so a PASS here already proves no durable dead letter survived. The same
# residual query also requires shared_projection_intents.completed_at IS NOT
# NULL for every row, so a PASS here proves the SQL family's async
# shared-projection writes -- including any retried one -- fully converged
# too, not only fact_work_items.
run_drain_gate() {
	local cell="$1"
	log "${cell}: drain projector + reducer (gate polls to the B-12 residual bound)"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
		tail -40 "${log_dir}"/reducer-*"${cell}"*.log 2>/dev/null || true
		tail -40 "${log_dir}/projector-${cell}.log" 2>/dev/null || true
		die "${cell}: drain did not reach the snapshot's residual bound within ${GATE_DRAIN_TIMEOUT}"
	fi
	assert_rationale_truth "${cell}"
}

# assert_no_dead_letters is a second, explicit dead_letter=0 check independent
# of run_drain_gate's implicit one, so a failure here names the actual count
# instead of only "the gate timed out".
assert_no_dead_letters() {
	local cell="$1"
	local count
	count="$(ifa_fault_dead_letter_count "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}")"
	[[ "${count}" -eq 0 ]] || die "${cell}: expected 0 dead_letter rows after recovery, got ${count}"
	printf '%s: dead_letter rows: 0 (recovery converged)\n' "${cell}"
}

# capture_digest canonicalizes the post-drain graph and stores it in digests[cell].
capture_digest() {
	local cell="$1"
	log "${cell}: canonicalize graph (ifa graph-dump)"
	"${bin_dir}/eshu-ifa" graph-dump -out "${work_dir}/graph-${cell}.dump" \
		|| die "${cell}: ifa graph-dump (canonical bytes) failed"
	local d
	d="$("${bin_dir}/eshu-ifa" graph-dump -digest | tr -d '[:space:]')"
	[[ -n "${d}" ]] || die "${cell}: ifa graph-dump -digest returned empty output"
	digests[${cell}]="${d}"
	printf '%s: digest: %s\n' "${cell}" "${d}"
}

# assert_matches_baseline compares digests[cell] to digests[baseline_key]
# (default "baseline", every existing caller's unchanged behavior), printing
# the full canonical-dump diff (never hiding it) on a mismatch. A non-default
# baseline_key lets a family whose fault cells run an extra step the shared
# baseline cell does not (e.g. #5993's bootstrap-index maintenance pass, which
# adds a real edge no digests[baseline] run ever produces) compare against its
# own family-scoped baseline instead of one that would mismatch by
# construction on every run, fault or not.
#
# Args: cell [baseline_key=baseline]
assert_matches_baseline() {
	local cell="$1" baseline_key="${2:-baseline}"
	[[ "${digests[${cell}]}" == "${digests[${baseline_key}]}" ]] && return 0
	printf 'MISMATCH: %s digest (%s) != %s digest (%s)\n' \
		"${cell}" "${digests[${cell}]}" "${baseline_key}" "${digests[${baseline_key}]}" >&2
	printf '\n=== full canonical graph diff: %s vs %s (failure artifact) ===\n' "${baseline_key}" "${cell}" >&2
	diff -u "${work_dir}/graph-${baseline_key}.dump" "${work_dir}/graph-${cell}.dump" >&2 || true
	die "${cell}: graph diverged from the fault-free baseline (${baseline_key}) -- a real recovery/concurrency defect; do NOT retry, lower workers, or otherwise normalize this away"
}

# teardown_cell reaps every backgrounded process this cell started and, when
# this script owns the Compose lifecycle, tears the stack down so the next
# cell starts from a genuinely fresh Postgres + NornicDB pair.
teardown_cell() {
	local cell="$1"
	local barrier_cleanup_rc=0
	if declare -F ifa_documentation_stop_ack_producers >/dev/null; then
		ifa_documentation_stop_ack_producers || barrier_cleanup_rc=$?
	else
		for pid in "${bg_pids[@]:-}"; do
			[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
		done
	fi
	if declare -F ifa_documentation_cleanup_ack_barrier >/dev/null; then
		ifa_documentation_cleanup_ack_barrier "${ifa_documentation_ack_barrier_cell:-${cell}}" \
			|| { local cleanup_rc=$?; [[ "${barrier_cleanup_rc}" -ne 0 ]] || barrier_cleanup_rc="${cleanup_rc}"; }
	fi
	# Each owned producer and holder is joined explicitly above. An unbounded
	# aggregate wait could hang behind a deliberately retained unsafe holder.
	[[ "${barrier_cleanup_rc}" -ne 0 ]] || bg_pids=()
	if [[ "${use_compose}" -eq 1 ]]; then
		log "${cell}: tear down cell (fresh stack for the next cell)"
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi
	[[ "${barrier_cleanup_rc}" -eq 0 ]] \
		|| die "${cell}: documentation ACK barrier teardown failed"
}
