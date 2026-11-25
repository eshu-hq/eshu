#!/usr/bin/env bash
# shellcheck shell=bash
# The supply-chain impact family cell (#10880 PR-C2). The impact domain writes no
# graph, so this cell compares a Postgres digest (supports plus winners) instead of
# a graph dump. It is ONE fault-free cell: the real eshu-projector and eshu-reducer
# at the gate's four workers drain the three committed cassettes of the family with
# the finding-fact reject trigger armed, and the cell passes when
#   - the digest equals the pin shared with the in-process family
#     (testdata/golden/ifa-impact-family-digest.txt),
#   - no work item is dead-lettered,
#   - no write was rejected by the trigger (no work item carries the
#     supply_chain_impact_fact_write_stopped failure class), and
#   - the trigger still reads enabled afterwards.
# The faults (a killed worker, an expired lease, a stale pass, a duplicate delivery)
# run in process on a real lease: go/internal/storage/postgres/
# supply_chain_impact_family_faults_live_test.go. This gate's lease is a hardcoded
# minute and its fault seam wraps the graph executor, which this domain never writes.

# Same list, same order as impactdigest.Cassettes
# (go/internal/ifa/impact/digest/digest.go); go/cmd/ifa/impact_digest_test.go pins them.
impact_family_cassettes=(
	"testdata/cassettes/vulnerabilityintelligence/supply-chain-demo.json"
	"testdata/cassettes/sbomattestation/supply-chain-demo.json"
	"testdata/cassettes/packageregistry/supply-chain-demo.json"
)
impact_family_digest_pin="testdata/golden/ifa-impact-family-digest.txt"

# ifa_impact_family_psql prints one scalar from the stack's Postgres.
ifa_impact_family_psql() {
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "$1" "${compose_file}" | tr -d '[:space:]'
}

ifa_impact_family_assert_armed_and_unrejected() {
	local label="$1" armed rejected
	armed="$(ifa_impact_family_psql "SELECT tgenabled FROM pg_trigger WHERE tgrelid = 'fact_records'::regclass AND tgname = 'fact_records_reject_supply_chain_impact_finding' AND NOT tgisinternal;")"
	[[ "${armed}" == "O" ]] \
		|| die "${label}: the finding-fact reject trigger reads '${armed}', want O (enabled): the cell must run armed"
	rejected="$(ifa_impact_family_psql "SELECT count(*) FROM fact_work_items WHERE failure_class = 'supply_chain_impact_fact_write_stopped';")"
	[[ "${rejected}" == "0" ]] \
		|| die "${label}: ${rejected} work items were rejected by the finding-fact write stop, want 0: a pass still writes finding facts"
}

cell_impact_family() {
	local cell_start cassette_path n=0
	cell_start=$(date +%s)
	log "cell impact-family: fresh stack"
	fresh_stack impactfamily
	for cassette_path in "${impact_family_cassettes[@]}"; do
		n=$((n + 1))
		log "impactfamily: drive ${cassette_path} (-workers ${drive_workers})"
		"${bin_dir}/eshu-ifa" drive -cassette "${cassette_path}" -workers "${drive_workers}" \
			>"${log_dir}/ifa-drive-impactfamily-${n}.log" 2>&1 \
			|| { tail -40 "${log_dir}/ifa-drive-impactfamily-${n}.log" >&2; die "impact-family: eshu-ifa drive (${cassette_path}) failed"; }
	done
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-impactfamily" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-impactfamily" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate_no_rationale impactfamily
	assert_no_dead_letters impactfamily
	ifa_impact_family_assert_armed_and_unrejected impact-family
	local pin
	pin="$(rg -v '^\s*(#|$)' -m1 "${impact_family_digest_pin}" | tr -d '[:space:]' || true)"
	local digest_args=(-resweep)
	[[ -z "${pin}" ]] || digest_args+=(-expect "${pin}")
	"${bin_dir}/eshu-ifa" impact-digest "${digest_args[@]}" \
		|| die "impact-family: the supports+winners digest does not equal the pinned in-process digest (${impact_family_digest_pin})"
	[[ -n "${pin}" ]] \
		|| die "impact-family: ${impact_family_digest_pin} pins no digest yet; pin the digest printed above from a fault-free in-process run, so this cell compares against it"
	teardown_cell impactfamily
	wall_times[impactfamily]=$(( $(date +%s) - cell_start ))
	printf 'impact-family: cell wall time: %ss\n' "${wall_times[impactfamily]}"
}
