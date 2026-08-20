#!/usr/bin/env bash
# Hermetic structural proof for repo_dependency's maintenance-backed trio.
run_ifa_fault_injection_repo_dependency_cases() {
	local root script shard cells live fixtures expected_cassette expected_edges baseline_line kill_line graph_line prerequisite_line maintenance_line lock_line claim_line release_line
	root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${root}/scripts/verify-ifa-fault-injection.sh"
	shard="${root}/scripts/lib/ifa_fault_shard.sh"
	cells="${root}/scripts/lib/ifa_fault_injection_repo_dependency_cells.sh"
	live="${root}/scripts/lib/ifa_repo_dependency_live.sh"
	fixtures="${root}/scripts/lib/ifa_family_fixtures.sh"
	expected_cassette="${root}/testdata/cassettes/repodependency/ifa-repo-dependency-family.json"
	expected_edges="${root}/go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"
	# shellcheck source=scripts/lib/ifa_family_fixtures.sh
	source "${fixtures}"
	[[ "${repo_dependency_cassette}" == "${expected_cassette}" && -f "${repo_dependency_cassette}" ]] || return 1
	[[ "${repo_dependency_expected_edges}" == "${expected_edges}" && -f "${repo_dependency_expected_edges}" ]] || return 1
	[[ -f "${cells}" ]] || { printf 'repo_dependency cells missing: %s\n' "${cells}" >&2; return 1; }
	for cell in cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency; do
		rg --line-regexp --quiet -- "ifa_fault_shard_run ${cell}" "${script}" || return 1
		rg --fixed-strings --quiet -- "${cell}" "${shard}" || return 1
	done
	rg --fixed-strings --quiet -- 'cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency' "${shard}" || return 1
	rg --fixed-strings --quiet -- 'materialize-platform-prerequisite' "${live}" || return 1
	rg --fixed-strings --quiet -- 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${cells}" || return 1
	rg --fixed-strings --quiet -- 'baseline_deployment_mapping_retried' "${cells}" || return 1
	rg --fixed-strings --quiet -- 'ifa_repo_dependency_pre_gate_dump_path' "${live}" || return 1
	rg --fixed-strings --quiet -- 'PROVISIONS_DEPENDENCY_FOR' "${live}" || return 1
	rg --fixed-strings --quiet -- 'READS_CONFIG_FROM' "${live}" || return 1
	rg --fixed-strings --quiet -- 'RUNS_ON' "${live}" || return 1
	rg --fixed-strings --quiet -- 'cross-repo relationship resolution started' "${live}" || return 1
	rg --fixed-strings --quiet -- 'cross-repo resolution gated' "${live}" || return 1
	rg --fixed-strings --quiet -- '-domain repo_dependency -expected "${expected_edges}"' "${live}" || return 1
	! rg --fixed-strings --quiet -- 'CrossRepoEvidenceSource' "${live}" || return 1
	! rg --fixed-strings --quiet -- 'assert_retried_above' <(rg -n -A 28 '^cell_failgraphwrite_repo_dependency\(\)' "${cells}") || return 1
	baseline_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_baseline_repo_dependency' "${script}" | cut -d: -f1)"
	kill_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_killworker_repo_dependency' "${script}" | cut -d: -f1)"
	graph_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_failgraphwrite_repo_dependency' "${script}" | cut -d: -f1)"
	[[ "${baseline_line}" -lt "${kill_line}" && "${kill_line}" -lt "${graph_line}" ]] || return 1
	prerequisite_line="$(rg -n --fixed-strings -- 'ifa_repo_dependency_live_materialize_platform_prerequisite' "${cells}" | head -1 | cut -d: -f1)"
	maintenance_line="$(rg -n --fixed-strings -- 'ifa_repo_dependency_live_run_maintenance_pass' "${cells}" | head -1 | cut -d: -f1)"
	[[ "${prerequisite_line}" -lt "${maintenance_line}" ]] || return 1
	lock_line="$(rg -n --fixed-strings -- 'ifa_fault_start_shared_intent_lock' "${cells}" | cut -d: -f1)"
	claim_line="$(rg -n -A 14 '^cell_killworker_repo_dependency\(\)' "${cells}" | rg --fixed-strings -- 'deployment_mapping' | head -1 | cut -d- -f1)"
	release_line="$(rg -n --fixed-strings -- 'ifa_fault_release_shared_intent_lock' "${cells}" | cut -d: -f1)"
	[[ "${lock_line}" -lt "${claim_line}" && "${claim_line}" -lt "${release_line}" ]] || return 1
	[[ "$("${script}" --list-cells | wc -l | tr -d '[:space:]')" == 24 ]] || return 1
}
