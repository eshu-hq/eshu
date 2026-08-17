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
	local node_generation="gen-2" handler_node_generation="gen-2"
	if [[ "${sql_generation}" == "gen1" ]]; then
		node_generation="gen-1"
	fi
	if [[ "${handler_generation}" == "gen1" ]]; then
		handler_node_generation="gen-1"
	fi
	local default_containment_generation="gen-2"
	if [[ "${sql_generation}" == "gen1" ]]; then
		default_containment_generation="gen-1"
	fi
	local containment_generation="${7:-${default_containment_generation}}"
	local foreign_evidence="${8:-stable}"
	local edge_evidence_source="${9:-projector/canonical}"
	local foreign_generation="${10:-gen-1}"
	local mixed_containment_generation="${11:-gen-1}"
	local omit_owned_generation="${12:-false}"
	local preserved_mapped_generation="${13:-gen-1}"
	local sql_file_language="${14:-sql}"
	local default_sql_index_table="public.orders"
	if [[ "${sql_generation}" == "gen1" ]]; then
		default_sql_index_table="public.users"
	fi
	local sql_index_table="${15:-${default_sql_index_table}}"
	local sql_repo root_dir schema_dir sql_file sql_entity sql_entity_alt
	local sql_index handler_dir handler_file handler_function foreign_from foreign_to
	local sql_repo_hash sql_entity_hash sql_entity_alt_hash sql_target_hash
	local sql_index_hash
	sql_repo="$(printf '{\"labels\":[\"Repository\"],\"props\":{\"generation_id\":\"%s\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${node_generation}")"
	root_dir="$(printf '{\"labels\":[\"Directory\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${node_generation}")"
	schema_dir="$(printf '{\"labels\":[\"Directory\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${node_generation}")"
	sql_file="$(printf '{\"labels\":[\"File\"],\"props\":{\"generation_id\":\"%s\",\"lang\":\"%s\",\"language\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"file-db-schema\"}}' \
		"${node_generation}" "${sql_file_language}" "${sql_file_language}")"
	sql_entity="$(printf '{\"labels\":[\"SqlTable\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"sql-table-users\"}}' "${node_generation}")"
	sql_entity_alt="$(printf '{\"labels\":[\"SqlTable\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"sql-table-orders\"}}' "${node_generation}")"
	sql_index="$(printf '{\"labels\":[\"SqlIndex\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/db/schema.sql\",\"relative_path\":\"db/schema.sql\",\"repo_id\":\"repo-ifa-sql-family\",\"table_name\":\"%s\",\"uid\":\"content-entity:sql-idx-users-email\"}}' \
		"${node_generation}" "${sql_index_table}")"
	handler_dir="$(printf '{\"labels\":[\"Directory\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/cmd/api\",\"repo_id\":\"repo-ifa-sql-family\"}}' "${handler_node_generation}")"
	handler_file="$(printf '{\"labels\":[\"File\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/cmd/api/handlers.go\",\"relative_path\":\"cmd/api/handlers.go\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"file-cmd-api-handlers\"}}' "${handler_node_generation}")"
	handler_function="$(printf '{\"labels\":[\"Function\"],\"props\":{\"generation_id\":\"%s\",\"path\":\"/repo/cmd/api/handlers.go\",\"relative_path\":\"cmd/api/handlers.go\",\"repo_id\":\"repo-ifa-sql-family\",\"uid\":\"content-entity:e_cb021b7a4238\"}}' "${handler_node_generation}")"
	foreign_from='{"labels":["Service"],"props":{"uid":"service-a"}}'
	# The uid deliberately collides with the SQL repo ID. Ordinary-node uid/id
	# values must never establish repository membership.
	foreign_to='{"labels":["Workload"],"props":{"uid":"repo-ifa-sql-family"}}'
	if [[ "${sql_generation}" == "gen1" ]]; then
		# Pinned independently by graphdump's real nodeDigest implementation in
		# TestShellCollateralFixtureNodeDigestContract. Do not derive this value
		# with the shell helper: that would make the attribution test circular.
		sql_repo_hash="b3af008c122a125c24d4885578d29af6f24471d748db246d9d30a4fd8c1281f0"
	else
		sql_repo_hash="$(test_ifa_fault_node_hash "${sql_repo}")"
	fi
	sql_entity_hash="$(test_ifa_fault_node_hash "${sql_entity}")"
	sql_entity_alt_hash="$(test_ifa_fault_node_hash "${sql_entity_alt}")"
	sql_index_hash="$(test_ifa_fault_node_hash "${sql_index}")"
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
		--argjson sql_index "${sql_index}" \
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
		--arg sql_index_hash "${sql_index_hash}" \
		--arg handler_dir_hash "$(test_ifa_fault_node_hash "${handler_dir}")" \
		--arg handler_file_hash "$(test_ifa_fault_node_hash "${handler_file}")" \
		--arg handler_function_hash "$(test_ifa_fault_node_hash "${handler_function}")" \
		--arg foreign_from_hash "$(test_ifa_fault_node_hash "${foreign_from}")" \
		--arg foreign_to_hash "$(test_ifa_fault_node_hash "${foreign_to}")" \
		--arg foreign_type "${foreign_type}" \
		--arg containment_generation "${containment_generation}" \
		--arg foreign_evidence "${foreign_evidence}" \
		--arg edge_evidence_source "${edge_evidence_source}" \
		--arg foreign_generation "${foreign_generation}" \
		--arg mixed_containment_generation "${mixed_containment_generation}" \
		--argjson omit_owned_generation "${omit_owned_generation}" \
		--arg preserved_mapped_generation "${preserved_mapped_generation}" \
		--argjson preserve_handler "$(if [[ "${handler_mode}" == "preserve" ]]; then printf true; else printf false; fi)" \
		'{
			nodes: (
				[$sql_repo, $root_dir, $schema_dir, $sql_file, $sql_entity, $sql_entity_alt, $sql_index, $foreign_from, $foreign_to]
				+ (if $preserve_handler then [$handler_dir, $handler_file, $handler_function] else [] end)
			),
			edges: ([
				{type:"REPO_CONTAINS", from:$sql_repo_hash, to:$root_dir_hash, props:{generation_id:$containment_generation, evidence_source:$edge_evidence_source}},
				{type:"CONTAINS", from:$root_dir_hash, to:$schema_dir_hash, props:{generation_id:$containment_generation, evidence_source:$edge_evidence_source}},
				{type:"CONTAINS", from:$schema_dir_hash, to:$sql_file_hash, props:({evidence_source:$edge_evidence_source} + (if $omit_owned_generation then {} else {generation_id:$containment_generation} end))},
				{type:"CONTAINS", from:$sql_file_hash, to:$sql_target_hash, props:{generation_id:$containment_generation, evidence_source:$edge_evidence_source}},
				{type:"CONTAINS", from:$sql_file_hash, to:$sql_index_hash, props:{generation_id:$containment_generation, evidence_source:$edge_evidence_source}},
				{type:"EXECUTES", from:$sql_target_hash, to:$sql_target_hash, props:{}},
				{type:"CALLS", from:"code-from", to:"code-to", props:{}},
				{type:"CONTAINS", from:$root_dir_hash, to:$foreign_to_hash, props:{generation_id:$mixed_containment_generation, evidence_source:$edge_evidence_source}},
				{type:$foreign_type, from:$foreign_from_hash, to:$foreign_to_hash, props:{generation_id:$foreign_generation, evidence_source:$edge_evidence_source, evidence:$foreign_evidence}}
			] + (if $preserve_handler then [
				{type:"CONTAINS", from:$root_dir_hash, to:$handler_dir_hash, props:{generation_id:$preserved_mapped_generation, evidence_source:$edge_evidence_source}},
				{type:"CONTAINS", from:$handler_dir_hash, to:$handler_file_hash, props:{generation_id:"gen-1", evidence_source:$edge_evidence_source}},
				{type:"CONTAINS", from:$handler_file_hash, to:$handler_function_hash, props:{generation_id:"gen-1", evidence_source:$edge_evidence_source}}
			] else [] end))
		}' >"${output}"
}

test_ifa_fault_collateral_compare_is_scoped_and_fail_closed() (
	source "${det_lib}"
	source "${collateral_nodes_lib}"
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
		"${case_dir}/unexpected-foreign-props.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 changed
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/nonprojector-baseline.dump" gen1 DEPENDS_ON sql-table-users preserve gen1 gen-1 stable reducer/code-interproc
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-nonprojector-generation.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable reducer/code-interproc
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-noncontained-generation.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable projector/canonical gen-2
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-owned-generation.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-unexpected
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-baseline-generation.dump" gen1 DEPENDS_ON sql-table-users preserve gen1 gen-2
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-missing-owned-generation.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable projector/canonical gen-1 gen-1 true
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-unrefreshed-mutable.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-1
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-refreshed-preserved.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable projector/canonical gen-1 gen-1 false gen-2
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-mapped-file-property.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable projector/canonical gen-1 gen-1 false gen-1 CORRUPTED
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-sql-index-table.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable projector/canonical gen-1 gen-1 false gen-1 sql public.payments
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-foreign-endpoint-containment.dump" gen2 DEPENDS_ON sql-table-users preserve gen1 gen-2 stable projector/canonical gen-1 gen-2
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-topology.dump" gen2 DEPENDS_ON sql-table-orders
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-broad-retract.dump" gen2 DEPENDS_ON sql-table-users retract
	test_ifa_fault_write_collateral_fixture \
		"${case_dir}/unexpected-handler-churn.dump" gen2 DEPENDS_ON sql-table-users preserve gen2
	jq '(.edges[] | select(.type == "CALLS") | .props).confidence = "CORRUPTED"' \
		"${case_dir}/allowed.dump" >"${case_dir}/unexpected-code-call-property.dump"

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
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-foreign-props.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "unexpected foreign edge property change returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-code-call-property.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "unexpected code-call edge property change returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/nonprojector-baseline.dump" "${case_dir}/unexpected-nonprojector-generation.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "non-projector generation_id churn returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-noncontained-generation.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "non-contained canonical generation_id churn returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-owned-generation.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] \
		|| fail "unexpected SQL-owned generation_id returned ${rc}, want fail-closed status 2"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/unexpected-baseline-generation.dump" "${case_dir}/allowed.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] \
		|| fail "baseline SQL-owned gen-2 returned ${rc}, want fail-closed status 2"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-missing-owned-generation.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "missing changed-side SQL-owned generation_id returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-unrefreshed-mutable.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] \
		|| fail "changed both-mapped gen-1 returned ${rc}, want fail-closed status 2"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-refreshed-preserved.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] \
		|| fail "changed one-mapped gen-2 returned ${rc}, want fail-closed status 2"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-mapped-file-property.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "mapped File language corruption returned ${rc}, want collateral-diff status 1"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-sql-index-table.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] \
		|| fail "unexpected SqlIndex table_name returned ${rc}, want fail-closed status 2"
	rc=0
	ifa_fault_compare_collateral_edges \
		"${case_dir}/baseline.dump" "${case_dir}/unexpected-foreign-endpoint-containment.dump" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "foreign-endpoint containment generation_id churn returned ${rc}, want collateral-diff status 1"
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

test_ifa_fault_assert_node_compare_status() {
	local baseline_dump="$1" changed_dump="$2" case_dir="$3" want_rc="$4" label="$5"
	local baseline_identities="${case_dir}/hostile-baseline-identities.json"
	local changed_identities="${case_dir}/hostile-changed-identities.json"
	ifa_fault_write_repo_node_identities \
		"${baseline_dump}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
		"${baseline_identities}" || fail "${label}: could not map baseline identities"
	ifa_fault_write_repo_node_identities \
		"${changed_dump}" repo-ifa-sql-family db/schema.sql /repo/db/schema.sql \
		"${changed_identities}" || fail "${label}: could not map changed identities"
	local rc=0
	ifa_fault_compare_collateral_nodes \
		"${baseline_dump}" "${changed_dump}" \
		"${baseline_identities}" "${changed_identities}" "${case_dir}" \
		>/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq "${want_rc}" ]] \
		|| fail "${label}: full-node comparison returned ${rc}, want ${want_rc}"
}

test_ifa_fault_collateral_nodes_preserve_full_multiset() (
	source "${det_lib}"
	source "${collateral_nodes_lib}"
	source "${delivery_cells_lib}"
	local case_dir rc
	case_dir="$(mktemp -d -t ifa-fault-collateral-nodes.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	test_ifa_fault_write_collateral_fixture "${case_dir}/baseline.dump" gen1 DEPENDS_ON
	test_ifa_fault_write_collateral_fixture "${case_dir}/allowed.dump" gen2 DEPENDS_ON
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/allowed.dump" "${case_dir}" 0 \
		"exact generation and SqlIndex transitions"

	jq '(.nodes[] | select(.props.uid? == "file-db-schema") | .props).unexpected = "extra"' \
		"${case_dir}/allowed.dump" >"${case_dir}/extra-prop.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/extra-prop.dump" "${case_dir}" 1 "extra mapped property"
	jq 'del(.nodes[] | select(.props.uid? == "file-db-schema") | .props.language)' \
		"${case_dir}/allowed.dump" >"${case_dir}/missing-prop.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/missing-prop.dump" "${case_dir}" 1 "missing mapped property"
	jq '(.nodes[] | select(.props.uid? == "file-db-schema") | .labels) = ["CorruptedFile"]' \
		"${case_dir}/allowed.dump" >"${case_dir}/label-drift.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/label-drift.dump" "${case_dir}" 1 "mapped label drift"
	jq '(.nodes[] | select(.props.uid? == "file-db-schema") | .props.repo_id) = "other-repo"' \
		"${case_dir}/allowed.dump" >"${case_dir}/owner-drift.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/owner-drift.dump" "${case_dir}" 1 "mapped owner drift"
	jq '(.nodes[] | select(.props.uid? == "file-db-schema") | .props.generation_id) = "gen-unknown"' \
		"${case_dir}/allowed.dump" >"${case_dir}/wrong-generation.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/wrong-generation.dump" "${case_dir}" 2 "mapped generation drift"
	jq '.nodes |= map(select(.props.uid? != "file-db-schema"))' \
		"${case_dir}/allowed.dump" >"${case_dir}/missing-node.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/missing-node.dump" "${case_dir}" 1 "missing mapped node"
	jq '.nodes += [(.nodes[] | select(.props.uid? == "file-db-schema"))]' \
		"${case_dir}/allowed.dump" >"${case_dir}/duplicate-node.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/duplicate-node.dump" "${case_dir}" 2 "duplicate mapped node"
	jq '(.nodes[] | select(.props.uid? == "service-a") | .props).tier = "corrupted"' \
		"${case_dir}/allowed.dump" >"${case_dir}/unmapped-drift.dump"
	test_ifa_fault_assert_node_compare_status \
		"${case_dir}/baseline.dump" "${case_dir}/unmapped-drift.dump" "${case_dir}" 1 "unmapped node drift"

	printf '[]\n' >"${case_dir}/malformed-identities.json"
	rc=0
	ifa_fault_write_collateral_nodes \
		"${case_dir}/baseline.dump" "${case_dir}/malformed-identities.json" \
		"${case_dir}/malformed-output.json" baseline >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || fail "malformed node identity map returned ${rc}, want status 2"
)

test_ifa_code_call_fresh_stack_intent_guard_is_typed_and_fail_closed() (
	source "${code_call_cells_lib}"
	declare -F ifa_code_call_require_fresh_intents >/dev/null \
		|| fail "code-call cells do not expose ifa_code_call_require_fresh_intents"

	local query_output query_rc output rc
	ifa_det_pg() {
		printf '%s' "${query_output}"
		return "${query_rc}"
	}

	query_output="ignored"
	query_rc=7
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -eq 7 ]] || fail "failed code-call precondition query returned ${rc}, want original exit 7"
	[[ "${output}" == *"precondition query FAILED (exit 7)"* ]] \
		|| fail "failed code-call precondition query did not preserve exit 7: ${output}"
	[[ "${output}" != *"survived fresh_stack"* ]] \
		|| fail "failed code-call precondition query was misreported as stale intents: ${output}"

	query_output=""
	query_rc=0
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "empty code-call precondition count was accepted"
	[[ "${output}" == *"returned empty output"* ]] \
		|| fail "empty code-call precondition count was not diagnosed distinctly: ${output}"

	query_output="not-a-count"
	query_rc=0
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "non-numeric code-call precondition count was accepted"
	[[ "${output}" == *"returned non-numeric output"* ]] \
		|| fail "non-numeric code-call precondition count was not diagnosed distinctly: ${output}"

	query_output=$' 3\n'
	query_rc=0
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "stale code-call intents were accepted"
	[[ "${output}" == *"3 code_calls intent row(s) survived fresh_stack"* ]] \
		|| fail "stale code-call intents were not reported as stale: ${output}"

	query_output=$' 0\n'
	query_rc=0
	ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml >/dev/null \
		|| fail "zero code-call intents did not continue"
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

# The documentation_edges cases (#5994) live in
# test-ifa-fault-injection-documentation-cases.sh, a sibling file: adding
# them inline here would have pushed this file past the repository's 500-line
# cap, and the sql/code_call cells already split per-family into their own
# scripts/lib/ifa_fault_injection_<family>_cells.sh files, so a per-family
# split of their hermetic review cases matches existing precedent rather than
# inventing a new one. scripts/test-verify-ifa-fault-injection.sh (the
# top-level verifier this file is sourced by) must source that sibling file
# too, the same way it already sources this one.

run_ifa_fault_injection_review_cases() {
	test_ifa_fault_collateral_compare_is_scoped_and_fail_closed
	test_ifa_fault_collateral_nodes_preserve_full_multiset
	test_ifa_code_call_fresh_stack_intent_guard_is_typed_and_fail_closed
	test_ifa_fault_released_lock_holder_is_not_torn_down_twice
	test_ifa_documentation_fresh_stack_edge_guard_is_typed_and_fail_closed
}
