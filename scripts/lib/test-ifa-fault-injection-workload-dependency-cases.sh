#!/usr/bin/env bash
# shellcheck disable=SC2016
# Hermetic structural checks for workload_dependency's maintenance-backed
# family-scoped baseline, kill/reclaim, and exact graph-write fault cells.

run_ifa_fault_injection_workload_dependency_cases() {
	local repo_root script live_lib cells_lib registry_row cassette fixture_scope fixture_generation
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
	live_lib="${repo_root}/scripts/lib/ifa_workload_dependency_live.sh"
	cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_workload_dependency_cells.sh"
	registry_row="${repo_root}/scripts/lib/ifa_family_registry/rows/08_workload_dependency.sh"
	cassette="${repo_root}/testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json"
	[[ -f "${live_lib}" ]] || fail "missing ${live_lib}"
	[[ -f "${cells_lib}" ]] || fail "missing ${cells_lib}"
	[[ -f "${registry_row}" ]] || fail "missing ${registry_row}"
	bash -n "${live_lib}" || fail "ifa_workload_dependency_live.sh has a syntax error"
	bash -n "${cells_lib}" || fail "ifa_fault_injection_workload_dependency_cells.sh has a syntax error"
	for cell in baseline killworker failgraphwrite; do
		rg --quiet -- "^ifa_fault_shard_run cell_${cell}_workload_dependency$" "${script}" \
			|| fail "fault gate does not dispatch cell_${cell}_workload_dependency through the shard wrapper"
	done
	for needle in \
		'ifa_workload_dependency_fault_prepare' \
		'ifa_workload_dependency_live_assert_repo_prerequisite' \
		'ifa_workload_dependency_live_assert_owned_absent' \
		'ifa_workload_dependency_live_reopen_materialization' \
		'ESHU_REDUCER_CLAIM_DOMAIN=workload_materialization' \
		'ifa_fault_wait_for_claimed' \
		'ifa_fault_assert_once_fault_marker' \
		'baseline_workload_dependency_retried' \
		'assert_matches_baseline "${cell}" baseline_workload_dependency'; do
		rg --fixed-strings --quiet -- "${needle}" "${cells_lib}" \
			|| fail "workload_dependency fault cells missing ${needle}"
	done
	fixture_scope="$(jq -er \
		'[.scopes[] | select([.facts[] | select(.fact_kind == "shared_followup" and .stable_fact_key == "shared_followup:repo-ifa-workload-dependency-source:workload_materialization" and .payload.reducer_domain == "workload_materialization")] | length == 1)] | if length == 1 then .[0].scope_id else error("want exactly one source workload_materialization scope") end' \
		"${cassette}")" || fail "workload_dependency cassette has no unique workload_materialization source scope_id"
	fixture_generation="$(jq -er \
		'[.scopes[] | select([.facts[] | select(.fact_kind == "shared_followup" and .stable_fact_key == "shared_followup:repo-ifa-workload-dependency-source:workload_materialization" and .payload.reducer_domain == "workload_materialization")] | length == 1)] | if length == 1 then .[0].generation_id else error("want exactly one source workload_materialization scope") end' \
		"${cassette}")" || fail "workload_dependency cassette has no unique workload_materialization source generation_id"
	for fixture_filter in \
		"scope_id = '${fixture_scope}'" \
		"generation_id = '${fixture_generation}'"; do
		rg --fixed-strings --quiet -- "${fixture_filter}" "${live_lib}" \
			|| fail "workload_dependency reopen is not fixture-scoped by ${fixture_filter}"
	done
	rg --fixed-strings --quiet -- 'IFA_FAMILY_BLOCKER_KIND[workload_dependency]="table_lock:fact_records"' "${registry_row}" \
		|| fail "workload_dependency registry blocker is not fact_records"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_WAIT_KEY[workload_dependency]="workload_materialization"' "${registry_row}" \
		|| fail "workload_dependency registry wait key is not workload_materialization"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_ANCHOR[workload_dependency]="MERGE (source)-[rel:DEPENDS_ON]->(target)"' "${registry_row}" \
		|| fail "workload_dependency registry graph-fault anchor is not the workload writer MERGE"
}
