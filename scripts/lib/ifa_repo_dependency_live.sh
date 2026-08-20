#!/usr/bin/env bash
# shellcheck shell=bash
# repo_dependency is intentionally a standalone live cell. The resolver has
# no Platform until the fixture-only production seam runs after the first
# drain; therefore it is not a worker-count determinism claim.

ifa_repo_dependency_live_drive() {
	local bin_dir="$1" cassette="$2"
	"${bin_dir}/eshu-ifa" drive -cassette "${cassette}"
}

ifa_repo_dependency_live_assert() {
	local bin_dir="$1" expected_edges="$2"
	"${bin_dir}/eshu-ifa" assert-edges -domain repo_dependency -expected "${expected_edges}"
}

ifa_repo_dependency_live_materialize_platform_prerequisite() {
	local bin_dir="$1" output expected_id
	expected_id="platform:kubernetes:none:cluster/prod-cluster:none:none"
	output="$("${bin_dir}/eshu-ifa" materialize-platform-prerequisite \
		-repo-id repo-ifa-repo-dependency-source -kind kubernetes \
		-name prod-cluster -locator cluster/prod-cluster)" || return 1
	printf '%s\n' "${output}"
	[[ "${output}" == *"platform_id=${expected_id}"* && "${output}" == *"verified=1"* ]]
}

# ifa_repo_dependency_live_assert_gated requires zero resolver-owned edges
# across every registered repo_dependency type before the prerequisite.
ifa_repo_dependency_live_assert_gated() {
	local bin_dir="$1" count
	ifa_repo_dependency_pre_gate_dump_path="$(mktemp)" || return 1
	trap 'rm -f "${ifa_repo_dependency_pre_gate_dump_path:-}"' RETURN
	command -v jq >/dev/null 2>&1 || { echo "repo_dependency: jq is required for the pre-maintenance zero-edge proof" >&2; return 1; }
	"${bin_dir}/eshu-ifa" graph-dump -out "${ifa_repo_dependency_pre_gate_dump_path}" || return 1
	count="$(jq '[.edges[] | select(.type == "PROVISIONS_DEPENDENCY_FOR" or .type == "USES_MODULE" or .type == "DISCOVERS_CONFIG_IN" or .type == "DEPENDS_ON" or .type == "DEPLOYS_FROM" or .type == "READS_CONFIG_FROM" or .type == "RUNS_ON")] | length' "${ifa_repo_dependency_pre_gate_dump_path}")" || return 1
	[[ "${count}" =~ ^[0-9]+$ && "${count}" == "0" ]]
}

# Args: log_dir label expected_gated. The resolver always logs "started";
# before maintenance it must also log "gated", afterwards it must not.
ifa_repo_dependency_live_assert_readiness_state() {
	local log_dir="$1" label="$2" expected_gated="$3" reducer_log contents
	reducer_log="${log_dir}/${label}.log"
	[[ -f "${reducer_log}" ]] || return 1
	contents="$(cat "${reducer_log}")" || return 1
	[[ "${contents}" == *"cross-repo relationship resolution started"* ]] || return 1
	if [[ "${expected_gated}" == "1" ]]; then
		[[ "${contents}" == *"cross-repo resolution gated"* ]]
	else
		[[ "${contents}" != *"cross-repo resolution gated"* ]]
	fi
}

ifa_repo_dependency_live_run_maintenance_pass() {
	local label="$1" bin_dir="$2" log_dir="$3"
	# Reuse the gate's shared bootstrap-index configuration, not raw Cypher.
	ifa_deployable_unit_live_run_maintenance_pass "repo-dependency-${label}" "${bin_dir}" "${log_dir}"
}

ifa_repo_dependency_live_drain() {
	local label="$1" bin_dir="$2" log_dir="$3" timeout="$4"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-repo-dependency-${label}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-repo-dependency-${label}" reducer_pid "${bin_dir}/eshu-reducer"
	"${bin_dir}/eshu-golden-corpus-gate" -phase=drains -snapshot=testdata/golden/e2e-20repo-snapshot.json -drain-timeout="${timeout}" || return 1
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || return 1
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || return 1
}

# Args: bin_dir cassette expected log_dir compose_project use_compose dsn file timeout.
ifa_repo_dependency_live_run_standalone_cell() {
	local bin_dir="$1" cassette="$2" expected_edges="$3" log_dir="$4"
	local compose_project="$5" use_compose="$6" postgres_dsn="$7" compose_file="$8" timeout="$9"
	local cell_start
	cell_start=$(date +%s)
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${compose_project}" -f "${compose_file}" up -d nornicdb postgres || return 1
		ifa_det_wait_for_backends "${compose_project}" "${compose_file}" || return 1
	fi
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-repo-dependency.log" 2>&1 || return 1
	ifa_repo_dependency_live_drive "${bin_dir}" "${cassette}" || return 1
	ifa_repo_dependency_live_drain pre "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_repo_dependency_live_assert_readiness_state "${log_dir}" "reducer-repo-dependency-pre" 1 || return 1
	ifa_repo_dependency_live_assert_gated "${bin_dir}" || return 1
	# Reducer is stopped by the drain above: create exactly the missing Platform
	# before maintenance reopens deferred cross-repository relationship work.
	ifa_repo_dependency_live_materialize_platform_prerequisite "${bin_dir}" || return 1
	ifa_repo_dependency_live_run_maintenance_pass primary "${bin_dir}" "${log_dir}" || return 1
	ifa_repo_dependency_live_drain post "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_repo_dependency_live_assert_readiness_state "${log_dir}" "reducer-repo-dependency-post" 0 || return 1
	ifa_repo_dependency_live_assert "${bin_dir}" "${expected_edges}" || return 1
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${compose_project}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi
	printf 'repo_dependency: standalone cell wall time: %ss\n' "$(( $(date +%s) - cell_start ))"
}
