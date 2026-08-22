#!/usr/bin/env bash
# shellcheck disable=SC2016
# Hermetic structural checks for workload_dependency's maintenance-backed
# family-scoped baseline, kill/reclaim, and exact graph-write fault cells.

run_ifa_fault_injection_workload_dependency_cases() {
	local repo_root script live_lib cells_lib registry_row cassette fixture_scope fixture_generation reopen_body production_reopen_body kill_body failgraph_body work_items_schema queue_source readiness_source replay_source
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
	live_lib="${repo_root}/scripts/lib/ifa_workload_dependency_live.sh"
	cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_workload_dependency_cells.sh"
	registry_row="${repo_root}/scripts/lib/ifa_family_registry/rows/08_workload_dependency.sh"
	cassette="${repo_root}/testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json"
	work_items_schema="${repo_root}/go/internal/storage/postgres/migrations/005_fact_work_items.sql"
	queue_source="${repo_root}/go/internal/storage/postgres/reducer_queue.go"
	readiness_source="${repo_root}/go/internal/storage/postgres/reducer_queue_readiness_sql.go"
	replay_source="${repo_root}/go/internal/storage/postgres/reducer_queue_replay.go"
	[[ -f "${live_lib}" ]] || fail "missing ${live_lib}"
	[[ -f "${cells_lib}" ]] || fail "missing ${cells_lib}"
	[[ -f "${registry_row}" ]] || fail "missing ${registry_row}"
	[[ -f "${work_items_schema}" ]] || fail "missing ${work_items_schema}"
	[[ -f "${queue_source}" ]] || fail "missing ${queue_source}"
	[[ -f "${readiness_source}" ]] || fail "missing ${readiness_source}"
	[[ -f "${replay_source}" ]] || fail "missing ${replay_source}"
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
	local output_assignment
	output_assignment="$(
		set -euo pipefail
		# shellcheck source=scripts/lib/ifa_fault_injection_workload_dependency_cells.sh
		source "${cells_lib}"
		ifa_workload_dependency_live_reopen_materialization() {
			printf '1|reducer_exact_work_item|pending|0'
		}
		die() {
			printf 'unexpected die: %s\n' "$*" >&2
			exit 1
		}
		FAULT_COMPOSE_PROJECT='test-project'
		use_compose=0
		ESHU_POSTGRES_DSN='test-dsn'
		compose_file='test-compose.yml'
		work_item_id='caller-id-unset'
		reopen_attempt='caller-attempt-unset'
		ifa_workload_dependency_fault_reopen test_cell work_item_id reopen_attempt >/dev/null
		printf '%s|%s' "${work_item_id}" "${reopen_attempt}"
	)" || fail "workload_dependency reopen output-assignment behavior failed"
	[[ "${output_assignment}" == 'reducer_exact_work_item|0' ]] \
		|| fail "workload_dependency reopen did not assign caller output variables (${output_assignment@Q})"
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
		'ifa_fault_assert_once_fault_marker' \
		'ifa_workload_dependency_live_wait_for_claimed_attempt' \
		'ifa_workload_dependency_live_assert_work_item_state' \
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
	production_reopen_body="$(awk '/^const reopenSucceededReducerWorkQuery = `/,/^`/' "${replay_source}")"
	local -a reopen_lifecycle=(
		"SET status = 'pending'|SET status = 'pending'"
		"attempt_count = 0|attempt_count = 0"
		"container_image_identity_v2_authorized_status = CASE|container_image_identity_v2_authorized_status = CASE"
		"WHEN container_image_identity_v2_required THEN 'pending'|WHEN container_image_identity_v2_required THEN 'pending'"
		"container_image_identity_v3_authorized_status = CASE|container_image_identity_v3_authorized_status = CASE"
		"WHEN container_image_identity_v3_required THEN 'pending'|WHEN container_image_identity_v3_required THEN 'pending'"
		"lease_owner = NULL|lease_owner = NULL"
		"claim_until = NULL|claim_until = NULL"
		"visible_at = \$1|visible_at = now()"
		"next_attempt_at = NULL|next_attempt_at = NULL"
		"updated_at = \$1|updated_at = now()"
		"reopened_at = \$1|reopened_at = now()"
		"failure_class = NULL|failure_class = NULL"
		"failure_message = NULL|failure_message = NULL"
		"failure_details = NULL|failure_details = NULL"
	)
	workload_dependency_reopen_matches_production_lifecycle() {
		local candidate="$1" lifecycle_entry production_clause fixture_clause
		for lifecycle_entry in "${reopen_lifecycle[@]}"; do
			production_clause="${lifecycle_entry%%|*}"
			fixture_clause="${lifecycle_entry#*|}"
			[[ -n "${production_clause}" ]] || continue
			printf '%s\n' "${production_reopen_body}" | rg --fixed-strings --quiet -- "${production_clause}" \
				&& printf '%s\n' "${candidate}" | rg --fixed-strings --quiet -- "${fixture_clause}" \
				|| return 1
		done
	}
	workload_dependency_reopen_matches_production_lifecycle "${reopen_body}" \
		|| fail "workload_dependency reopen does not mirror the production succeeded-row lifecycle reset"
	for reopen_clause in \
		"SET status = 'pending'" \
		"attempt_count = 0" \
		"container_image_identity_v2_authorized_status = CASE" \
		"WHEN container_image_identity_v2_required THEN 'pending'" \
		"container_image_identity_v3_authorized_status = CASE" \
		"WHEN container_image_identity_v3_required THEN 'pending'" \
		"lease_owner = NULL" \
		"claim_until = NULL" \
		"visible_at = now()" \
		"next_attempt_at = NULL" \
		"updated_at = now()" \
		"reopened_at = now()" \
		"failure_class = NULL" \
		"failure_message = NULL" \
		"failure_details = NULL"; do
		if workload_dependency_reopen_matches_production_lifecycle "$(printf '%s\n' "${reopen_body}" | rg -v --fixed-strings -- "${reopen_clause}")"; then
			fail "workload_dependency reopen lifecycle omission mutation passed: ${reopen_clause}"
		fi
	done
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
	kill_body="$(awk '/^cell_killworker_workload_dependency\(\)/,/^}/' "${cells_lib}")"
	failgraph_body="$(awk '/^cell_failgraphwrite_workload_dependency\(\)/,/^}/' "${cells_lib}")"
	workload_dependency_kill_has_exact_attempt_proof() {
		printf '%s\n' "$1" | rg -U --pcre2 --quiet -- 'ifa_workload_dependency_fault_reopen "\$\{cell\}" work_item_id reopen_attempt[\s\S]*ifa_workload_dependency_live_wait_for_claimed_attempt[\s\S]*"\$\{work_item_id\}" "\$\(\(reopen_attempt \+ 1\)\)"[\s\S]*ifa_workload_dependency_live_assert_work_item_state[\s\S]*"\$\{work_item_id\}" succeeded "\$\(\(reopen_attempt \+ 2\)\)"'
	}
	workload_dependency_failgraph_has_exact_attempt_proof() {
		printf '%s\n' "$1" | rg -U --pcre2 --quiet -- 'ifa_workload_dependency_fault_reopen "\$\{cell\}" work_item_id reopen_attempt[\s\S]*ifa_workload_dependency_live_assert_work_item_state[\s\S]*"\$\{work_item_id\}" succeeded "\$\(\(reopen_attempt \+ 2\)\)"'
	}
	workload_dependency_kill_has_exact_attempt_proof "${kill_body}" \
		|| fail "workload_dependency killworker must prove exact-row victim and reclaim attempt deltas"
	workload_dependency_failgraph_has_exact_attempt_proof "${failgraph_body}" \
		|| fail "workload_dependency failgraphwrite must prove the exact row retried and succeeded"
	if workload_dependency_kill_has_exact_attempt_proof "${kill_body//work_item_id/wrong_work_item_id}"; then
		fail "workload_dependency killworker wrong-work-item mutation passed"
	fi
	if workload_dependency_kill_has_exact_attempt_proof "${kill_body//reopen_attempt + 1/reopen_attempt + 2}"; then
		fail "workload_dependency killworker wrong claimed-attempt delta mutation passed"
	fi
	if workload_dependency_kill_has_exact_attempt_proof "${kill_body//reopen_attempt + 2/reopen_attempt + 1}"; then
		fail "workload_dependency killworker wrong terminal-attempt delta mutation passed"
	fi
	if workload_dependency_failgraph_has_exact_attempt_proof "${failgraph_body//work_item_id/wrong_work_item_id}"; then
		fail "workload_dependency failgraphwrite wrong-work-item mutation passed"
	fi
	if workload_dependency_failgraph_has_exact_attempt_proof "${failgraph_body//reopen_attempt + 2/reopen_attempt + 1}"; then
		fail "workload_dependency failgraphwrite wrong terminal-attempt delta mutation passed"
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
	if rg --fixed-strings --quiet -- 'IFA_FAMILY_RETRY_BASELINE_VAR[workload_dependency]' "${registry_row}"; then
		fail "workload_dependency custom exact-row cells must not advertise the aggregate retry-baseline oracle"
	fi
	rg --fixed-strings --quiet -- 'IFA_FAMILY_ANCHOR[workload_dependency]="MERGE (source)-[rel:DEPENDS_ON]->(target)"' "${registry_row}" \
		|| fail "workload_dependency registry graph-fault anchor is not the workload writer MERGE"
}
