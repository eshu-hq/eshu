#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the Ifa fault-injection helper libraries.
# Sourced by scripts/test-verify-ifa-fault-injection.sh so the top-level static
# verifier stays below the repository's 500-line cap.

test_ifa_fault_sha256() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 | awk '{print $1}'
	else
		sha256sum | awk '{print $1}'
	fi
}

test_ifa_fault_node_hash() {
	printf '%s\n' "$1" | jq -S . | test_ifa_fault_sha256
}

test_ifa_fault_write_collateral_fixture() {
	local output="$1" sql_generation="$2" foreign_type="$3"
	local sql_target_uid="${4:-sql-table-users}"
	local handler_mode="${5:-preserve}"
	local handler_generation="${6:-gen1}"
	local sql_repo root_dir schema_dir sql_file sql_entity sql_entity_alt
	local handler_dir handler_file handler_function foreign_from foreign_to
	local sql_repo_hash sql_entity_hash sql_entity_alt_hash sql_target_hash
	sql_repo="$(printf '{\"labels\":[\"Repository\"],\"props\":{\"generation_id\":\"%s\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${sql_generation}")"
	root_dir="$(printf '{\"labels\":[\"Directory\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${sql_generation}")"
	schema_dir="$(printf '{\"labels\":[\"Directory\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${sql_generation}")"
	sql_file="$(printf '{\"labels\":[\"File\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"file-db-schema\"}}' "${sql_generation}")"
	sql_entity="$(printf '{\"labels\":[\"SqlTable\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"sql-table-users\"}}' "${sql_generation}")"
	sql_entity_alt="$(printf '{\"labels\":[\"SqlTable\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"sql-table-orders\"}}' "${sql_generation}")"
	handler_dir="$(printf '{\"labels\":[\"Directory\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/cmd/api\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${handler_generation}")"
	handler_file="$(printf '{\"labels\":[\"File\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/cmd/api/handlers.go\",\"relative_path\":\"cmd/api/handlers.go\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"file-cmd-api-handlers\"}}' "${handler_generation}")"
	handler_function="$(printf '{\"labels\":[\"Function\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/cmd/api/handlers.go\",\"relative_path\":\"cmd/api/handlers.go\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"content-entity:e_cb021b7a4238\"}}' "${handler_generation}")"
	foreign_from='{"labels":["Service"],"props":{"uid":"service-a"}}'
	foreign_to='{"labels":["Workload"],"props":{"uid":"workload-a"}}'
	if [[ "${sql_generation}" == "gen1" ]]; then
		# Pinned independently by graphdump's real nodeDigest implementation in
		# TestShellCollateralFixtureNodeDigestContract. Do not derive this value
		# with the shell helper: that would make the attribution test circular.
		sql_repo_hash="8ea9d5d8c0eabf08ef3c18ad4b6617a6466c707f7f579bac7017a7b6497d129a"
	else
		sql_repo_hash="$(test_ifa_fault_node_hash "${sql_repo}")"
	fi
	sql_entity_hash="$(test_ifa_fault_node_hash "${sql_entity}")"
	sql_entity_alt_hash="$(test_ifa_fault_node_hash "${sql_entity_alt}")"
	if [[ "${sql_target_uid}" == "sql-table-orders" ]]; then
		sql_target_hash="${sql_entity_alt_hash}"
	else
		sql_target_hash="${sql_entity_hash}"
	fi
	jq -n \
		--argjson sql_repo "${sql_repo}" \
		--argjson root_dir "${root_dir}" \
		--argjson schema_dir "${schema_dir}" \
		--argjson sql_file "${sql_file}" \
		--argjson sql_entity "${sql_entity}" \
		--argjson sql_entity_alt "${sql_entity_alt}" \
		--argjson handler_dir "${handler_dir}" \
		--argjson handler_file "${handler_file}" \
		--argjson handler_function "${handler_function}" \
		--argjson foreign_from "${foreign_from}" \
		--argjson foreign_to "${foreign_to}" \
		--arg sql_repo_hash "${sql_repo_hash}" \
		--arg root_dir_hash "$(test_ifa_fault_node_hash "${root_dir}")" \
		--arg schema_dir_hash "$(test_ifa_fault_node_hash "${schema_dir}")" \
		--arg sql_file_hash "$(test_ifa_fault_node_hash "${sql_file}")" \
		--arg sql_target_hash "${sql_target_hash}" \
		--arg handler_dir_hash "$(test_ifa_fault_node_hash "${handler_dir}")" \
		--arg handler_file_hash "$(test_ifa_fault_node_hash "${handler_file}")" \
		--arg handler_function_hash "$(test_ifa_fault_node_hash "${handler_function}")" \
		--arg foreign_from_hash "$(test_ifa_fault_node_hash "${foreign_from}")" \
		--arg foreign_to_hash "$(test_ifa_fault_node_hash "${foreign_to}")" \
		--arg foreign_type "${foreign_type}" \
		--argjson preserve_handler "$(if [[ "${handler_mode}" == "preserve" ]]; then printf true; else printf false; fi)" \
		'{
			nodes: (
				[$sql_repo, $root_dir, $schema_dir, $sql_file, $sql_entity, $sql_entity_alt, $foreign_from, $foreign_to]
				+ (if $preserve_handler then [$handler_dir, $handler_file, $handler_function] else [] end)
			),
			edges: ([
				{type:"REPO_CONTAINS", from:$sql_repo_hash, to:$root_dir_hash, props:{}},
				{type:"CONTAINS", from:$root_dir_hash, to:$schema_dir_hash, props:{}},
				{type:"CONTAINS", from:$schema_dir_hash, to:$sql_file_hash, props:{}},
				{type:"CONTAINS", from:$sql_file_hash, to:$sql_target_hash, props:{}},
				{type:"EXECUTES", from:$sql_target_hash, to:$sql_target_hash, props:{}},
				{type:"CALLS", from:"code-from", to:"code-to", props:{}},
				{type:$foreign_type, from:$foreign_from_hash, to:$foreign_to_hash, props:{}}
			] + (if $preserve_handler then [
				{type:"CONTAINS", from:$root_dir_hash, to:$handler_dir_hash, props:{}},
				{type:"CONTAINS", from:$handler_dir_hash, to:$handler_file_hash, props:{}},
				{type:"CONTAINS", from:$handler_file_hash, to:$handler_function_hash, props:{}}
			] else [] end))
		}' >"${output}"
}

test_ifa_fault_collateral_compare_is_scoped_and_fail_closed() (
	source "${det_lib}"
	source "${delivery_cells_lib}"
	declare -F ifa_fault_compare_collateral_edges >/dev/null \
		|| fail "delivery cells do not expose ifa_fault_compare_collateral_edges"

	local case_dir rc
	case_dir="$(mktemp -d -t ifa-fault-collateral.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	test_ifa_fault_write_collateral_fixture "${case_dir}/baseline.dump" gen1 DEPENDS_ON
	test_ifa_fault_write_collateral_fixture "${case_dir}/allowed.dump" gen2 DEPENDS_ON
	test_ifa_fault_write_collateral_fixture "${case_dir}/unexpected.dump" gen2 RUNS_IN
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-topology.dump" gen2 DEPENDS_ON sql-table-orders
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-broad-retract.dump" gen2 DEPENDS_ON sql-table-users retract
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-handler-churn.dump" gen2 DEPENDS_ON sql-table-users preserve gen2

	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/allowed.dump" "${case_dir}" \
		|| fail "SQL-owned containment endpoint churn did not pass the collateral comparison"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "unexpected foreign edge change returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-topology.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "SQL-owned containment attachment swap returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-broad-retract.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "out-of-scope handler-path retract returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-handler-churn.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "out-of-scope handler-path property churn returned ${rc}, want collateral-diff status 1"
	printf '{not-json\n' >"${case_dir}/invalid.dump"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/invalid.dump" "${case_dir}/allowed.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] \
		|| fail "invalid graph dump returned ${rc}, want jq/error status 2"
)

test_ifa_fault_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${code_call_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-fault-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41001
	survivor_pid=41002
	bg_pids=("${holder_pid}" "${survivor_pid}")
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() { return 0; }
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_code_call_release_intent_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined code-call lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined code-call lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

run_ifa_fault_injection_review_cases() {
	test_ifa_fault_collateral_compare_is_scoped_and_fail_closed
	test_ifa_fault_released_lock_holder_is_not_torn_down_twice
}
