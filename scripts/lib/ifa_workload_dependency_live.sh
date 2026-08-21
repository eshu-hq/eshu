#!/usr/bin/env bash
# shellcheck shell=bash
# workload_dependency is a maintenance-backed live family. The first drain
# builds workloads while repository dependency projection is still gated.
# Bootstrap maintenance reopens deployment_mapping; the repo_dependency runner
# then writes the exact Repository edges and automatically replays workload
# materialization for the affected repositories. The same drain must therefore
# converge both the three-edge repository prerequisite and the two owned
# Workload DEPENDS_ON edges without a manual reopen.

ifa_workload_dependency_live_drive() {
	local bin_dir="$1" cassette="$2"
	"${bin_dir}/eshu-ifa" drive -cassette "${cassette}"
}

ifa_workload_dependency_live_assert() {
	local bin_dir="$1" expected_edges="$2"
	"${bin_dir}/eshu-ifa" assert-edges -domain workload_dependency -expected "${expected_edges}"
}

ifa_workload_dependency_live_assert_repo_prerequisite() {
	local bin_dir="$1" expected_edges="$2"
	"${bin_dir}/eshu-ifa" assert-edges -domain repo_dependency -expected "${expected_edges}"
}

# The ownership filter is the same one retractWorkloadDependencyEdgesCypher
# uses. Zero here proves the later exact edges cannot be stale state from the
# initial workload drain or a different DEPENDS_ON writer.
ifa_workload_dependency_live_assert_owned_absent() {
	local bin_dir="$1" label="$2" dump_path count
	command -v jq >/dev/null 2>&1 || {
		printf '%s: jq is required for the workload_dependency ownership census\n' "${label}" >&2
		return 1
	}
	dump_path="$(mktemp)" || return 1
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${dump_path}"; then
		rm -f "${dump_path}"
		return 1
	fi
	count="$(jq '[.edges[] | select(.type == "DEPENDS_ON" and .props.evidence_source == "finalization/workloads")] | length' "${dump_path}")" || {
		rm -f "${dump_path}"
		return 1
	}
	rm -f "${dump_path}"
	[[ "${count}" =~ ^[0-9]+$ && "${count}" -eq 0 ]] || {
		printf '%s: expected zero workload-owned DEPENDS_ON edges before repo_dependency replay, got %q\n' "${label}" "${count}" >&2
		return 1
	}
	printf '%s: confirmed zero workload-owned DEPENDS_ON edges before repo_dependency replay\n' "${label}"
}

# Reopen only this fixture's succeeded workload_materialization row for the
# fault cells' deliberate second execution. The standalone cell relies on the
# repo_dependency runner's automatic replay and never calls this helper.
# Scoping by domain alone would retrigger unrelated work; the count is returned
# from the UPDATE itself and fault callers require exactly one row.
ifa_workload_dependency_live_reopen_materialization() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local result rc=0
	result="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"WITH reopened AS (
		   UPDATE fact_work_items
		      SET status = 'pending', lease_owner = NULL, claim_until = NULL,
		          visible_at = now(), updated_at = now()
		    WHERE stage = 'reducer'
		      AND domain = 'workload_materialization'
		      AND scope_id = 'scope-ifa-workload-dependency-family'
		      AND generation_id = 'gen-ifa-workload-dependency-family-1'
		      AND entity_key = 'repo:repo-ifa-workload-dependency-source'
		      AND status = 'succeeded'
		RETURNING 1
		 ) SELECT count(*) FROM reopened;" "${compose_file}")" || rc=$?
	[[ "${rc}" -eq 0 ]] || return "${rc}"
	printf '%s' "${result}" | tr -d '[:space:]'
}

ifa_workload_dependency_live_drain() {
	local label="$1" bin_dir="$2" log_dir="$3" timeout="$4"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-workload-dependency-${label}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-workload-dependency-${label}" reducer_pid "${bin_dir}/eshu-reducer"
	"${bin_dir}/eshu-golden-corpus-gate" -phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json -drain-timeout="${timeout}" || return 1
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || return 1
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || return 1
}

# Args: bin cassette workload-expected repo-expected log compose use dsn file timeout.
ifa_workload_dependency_live_run_standalone_cell() {
	local bin_dir="$1" cassette="$2" expected_edges="$3" repo_expected="$4" log_dir="$5"
	local compose_project="$6" use_compose="$7" compose_file="$9" timeout="${10}"
	local cell_start
	cell_start=$(date +%s)
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${compose_project}" -f "${compose_file}" up -d nornicdb postgres || return 1
		ifa_det_wait_for_backends "${compose_project}" "${compose_file}" || return 1
	fi
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-workload-dependency.log" 2>&1 || return 1
	ifa_workload_dependency_live_drive "${bin_dir}" "${cassette}" || return 1
	ifa_workload_dependency_live_drain pre "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_workload_dependency_live_assert_owned_absent "${bin_dir}" pre-maintenance || return 1
	ifa_repo_dependency_live_run_maintenance_pass workload-dependency "${bin_dir}" "${log_dir}" || return 1
	ifa_workload_dependency_live_drain repo "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_workload_dependency_live_assert_repo_prerequisite "${bin_dir}" "${repo_expected}" || return 1
	ifa_workload_dependency_live_assert "${bin_dir}" "${expected_edges}" || return 1
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${compose_project}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi
	printf 'workload_dependency: standalone cell wall time: %ss\n' "$(( $(date +%s) - cell_start ))"
}
