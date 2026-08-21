#!/usr/bin/env bash
# shellcheck disable=SC2016
# Hermetic structural checks for workload_dependency's maintenance-backed
# family-scoped baseline, kill/reclaim, and exact graph-write fault cells.

run_ifa_fault_injection_workload_dependency_cases() {
	local repo_root script live_lib cells_lib registry_row cassette fixture_scope fixture_generation reopen_body failgraph_body work_items_schema queue_source readiness_source
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
	live_lib="${repo_root}/scripts/lib/ifa_workload_dependency_live.sh"
	cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_workload_dependency_cells.sh"
	registry_row="${repo_root}/scripts/lib/ifa_family_registry/rows/08_workload_dependency.sh"
	cassette="${repo_root}/testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json"
	work_items_schema="${repo_root}/go/internal/storage/postgres/migrations/005_fact_work_items.sql"
	queue_source="${repo_root}/go/internal/storage/postgres/reducer_queue.go"
	readiness_source="${repo_root}/go/internal/storage/postgres/reducer_queue_readiness_sql.go"
	[[ -f "${live_lib}" ]] || fail "missing ${live_lib}"
	[[ -f "${cells_lib}" ]] || fail "missing ${cells_lib}"
	[[ -f "${registry_row}" ]] || fail "missing ${registry_row}"
	[[ -f "${work_items_schema}" ]] || fail "missing ${work_items_schema}"
	[[ -f "${queue_source}" ]] || fail "missing ${queue_source}"
	[[ -f "${readiness_source}" ]] || fail "missing ${readiness_source}"
	rg --quiet -- '^[[:space:]]+payload JSONB NOT NULL ' "${work_items_schema}" \
		|| fail "fact_work_items schema no longer stores reducer entity identity in JSONB payload"
	if rg --quiet -- '^[[:space:]]+entity_key[[:space:]]' "${work_items_schema}"; then
		fail "fact_work_items unexpectedly has a physical entity_key column; revisit the reopen query contract"
	fi
	rg --quiet --fixed-strings -- 'payload["entity_key"] = intent.EntityKey' "${queue_source}" \
		|| fail "reducer enqueue no longer stores entity_key inside fact_work_items payload"
	rg --quiet --fixed-strings -- ".payload->>'entity_key'" "${readiness_source}" \
		|| fail "production readiness SQL no longer reads entity_key from fact_work_items payload"
	bash -n "${live_lib}" || fail "ifa_workload_dependency_live.sh has a syntax error"
	bash -n "${cells_lib}" || fail "ifa_fault_injection_workload_dependency_cells.sh has a syntax error"
	for cell in baseline killworker failgraphwrite; do
		rg --quiet -- "^ifa_fault_shard_run cell_${cell}_workload_dependency$" "${script}" \
			|| fail "fault gate does not dispatch cell_${cell}_workload_dependency through the shard wrapper"
	done
	for needle in \
		'ifa_workload_dependency_fault_prepare' \
		'ifa_workload_dependency_live_assert_repo_prerequisite' \
		'ifa_workload_dependency_live_assert "${bin_dir}" "${workload_dependency_expected_edges}"' \
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
	reopen_body="$(awk '/^ifa_workload_dependency_live_reopen_materialization\(\)/,/^}/' "${live_lib}")"
	workload_dependency_reopen_has_exact_replay_key() {
		printf '%s\n' "$1" | rg --quiet -- "^[[:space:]]+AND payload->>'entity_key' = 'repo:repo-ifa-workload-dependency-source'$"
	}
	workload_dependency_reopen_has_exact_replay_key "${reopen_body}" \
		|| fail "workload_dependency reopen must target the production repo_dependency replay entity key"
	if workload_dependency_reopen_has_exact_replay_key "$(printf '%s\n' "${reopen_body}" | rg -v --fixed-strings -- "payload->>'entity_key' = 'repo:repo-ifa-workload-dependency-source'")"; then
		fail "workload_dependency reopen entity-key omission mutation passed"
	fi
	if workload_dependency_reopen_has_exact_replay_key "$(printf '%s\n' "${reopen_body}" | awk '{ sub(/payload->>\047entity_key\047/, "entity_key"); print }')"; then
		fail "workload_dependency reopen wrong entity-key expression mutation passed"
	fi
	if workload_dependency_reopen_has_exact_replay_key "${reopen_body//repo:repo-ifa-workload-dependency-source/repo:wrong-source}"; then
		fail "workload_dependency reopen wrong entity-key value mutation passed"
	fi
	failgraph_body="$(awk '/^cell_failgraphwrite_workload_dependency\(\)/,/^}/' "${cells_lib}")"
	workload_dependency_failgraph_has_retry_proof() {
		printf '%s\n' "$1" | rg --quiet -- '^[[:space:]]*ifa_fault_assert_retried_above ' \
			&& printf '%s\n' "$1" | rg --quiet -- '^[[:space:]]*"\$\{compose_file\}" "\$\{baseline_workload_dependency_retried\}" 15 workload_materialization \\$'
	}
	workload_dependency_failgraph_has_retry_proof "${failgraph_body}" \
		|| fail "workload_dependency failgraphwrite must prove workload_materialization retried above baseline"
	if workload_dependency_failgraph_has_retry_proof "${failgraph_body//ifa_fault_assert_retried_above/ifa_fault_count_retried}"; then
		fail "workload_dependency failgraphwrite retry-assertion omission mutation passed"
	fi
	if workload_dependency_failgraph_has_retry_proof "${failgraph_body//baseline_workload_dependency_retried/wrong_retry_baseline}"; then
		fail "workload_dependency failgraphwrite wrong-baseline mutation passed"
	fi
	local automatic_assert_line reopen_line
	automatic_assert_line="$(rg -n --fixed-strings -- 'ifa_workload_dependency_live_assert "${bin_dir}" "${workload_dependency_expected_edges}"' "${cells_lib}" | head -1 | cut -d: -f1 || true)"
	reopen_line="$(rg -n --fixed-strings -- 'ifa_workload_dependency_fault_reopen "${cell}"' "${cells_lib}" | head -1 | cut -d: -f1 || true)"
	[[ "${automatic_assert_line}" =~ ^[0-9]+$ && "${reopen_line}" =~ ^[0-9]+$ && "${automatic_assert_line}" -lt "${reopen_line}" ]] \
		|| fail "workload_dependency fault cells must prove automatic exact-set convergence before deliberate reopen"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_BLOCKER_KIND[workload_dependency]="table_lock:fact_records"' "${registry_row}" \
		|| fail "workload_dependency registry blocker is not fact_records"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_WAIT_KEY[workload_dependency]="workload_materialization"' "${registry_row}" \
		|| fail "workload_dependency registry wait key is not workload_materialization"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_ANCHOR[workload_dependency]="MERGE (source)-[rel:DEPENDS_ON]->(target)"' "${registry_row}" \
		|| fail "workload_dependency registry graph-fault anchor is not the workload writer MERGE"
}
