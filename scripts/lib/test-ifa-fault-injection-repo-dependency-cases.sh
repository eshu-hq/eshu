#!/usr/bin/env bash
# Hermetic structural proof for repo_dependency's maintenance-backed trio.
# The partition-lease control tests
# (run_ifa_repo_dependency_partition_lease_controls) moved to
# scripts/lib/test-ifa-fault-injection-repo-dependency-lease-cases.sh once
# this file crossed the repository's 500-line cap; that sibling is sourced
# alongside this one and the call site here is unchanged.
ifa_repo_dependency_body_has_order() {
	local body="$1" previous=0 needle line
	shift
	for needle in "$@"; do
		line="$(printf '%s\n' "${body}" | rg -n --fixed-strings -- "${needle}" | head -1 | cut -d: -f1)"
		[[ "${line}" =~ ^[0-9]+$ && "${line}" -gt "${previous}" ]] || return 1
		previous="${line}"
	done
}

ifa_repo_dependency_prepare_has_required_order() {
	ifa_repo_dependency_body_has_order "$1" \
		'fresh_stack "${cell}"' \
		'drive_all_cassettes "${cell}"' \
		'ifa_repo_dependency_live_drive "${bin_dir}" "${repo_dependency_cassette}"' \
		'ifa_det_start_bg "${log_dir}" "projector-${cell}-pre"' \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-pre"' \
		'run_drain_gate "${cell}-pre"'
}

ifa_repo_dependency_graph_terminal_owner_is_exact() {
	[[ "$1" == *'ifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}-before"'* ]]
}

ifa_repo_dependency_failgraphwrite_has_required_order() {
	ifa_repo_dependency_body_has_order "$1" \
		ifa_repo_dependency_fault_prepare \
		ifa_fault_write_once_script \
		'ifa_det_start_bg "${log_dir}" "projector-${cell}"' \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-before"' \
		'env "ESHU_IFA_FAULT_SCRIPT=${fault_script}" "${tagged_bin_dir}/eshu-reducer"' \
		ifa_repo_dependency_fault_wait_for_once_marker \
		'ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_dir}/reducer-${cell}-before.log" "${reducer_before}" "${CLAIMED_ROW_WAIT_TIMEOUT}"' \
		'ifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" pre_fault_partition_leases' \
		'ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL' \
		'ifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" post_fault_partition_leases' \
		ifa_repo_dependency_fault_require_partition_lease_subset \
		'ifa_repo_dependency_fault_expire_partition_leases "${cell}" "${reducer_before}" "${post_fault_partition_leases}"' \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-after" reducer_after "${bin_dir}/eshu-reducer"' \
		run_drain_gate ifa_fault_assert_once_fault_marker \
		ifa_repo_dependency_fault_assert_terminal capture_digest assert_matches_baseline teardown_cell
}

run_ifa_repo_dependency_fault_marker_wait_controls() (
	local mode=live cell=test_cell reducer_pid=876543 budget=1 kill_checks=0
	local state_dir fault_script anchor operation sibling_anchor single_row_anchor marker
	state_dir="$(mktemp -d)" || return 1
	trap 'rm -rf "${state_dir}"' EXIT
	fault_script="${state_dir}/fault.json"
	anchor=$'target_repo.generation_id = row.generation_id\nMERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)\nSET rel.confidence = row.confidence'
	operation=$'UNWIND $rows AS row\nMERGE (source_repo:Repository {id: row.repo_id})\nMERGE (target_repo:Repository {id: row.target_repo_id})\nON CREATE SET target_repo.evidence_source = row.evidence_source,\n              target_repo.generation_id = row.generation_id\nMERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)\nSET rel.confidence = row.confidence,\n    rel.resolution_source = row.resolution_source'
	sibling_anchor=$'UNWIND $rows AS row\nMATCH (source_repo:Repository {id: row.repo_id})\nMATCH (target_repo:Repository {id: row.target_repo_id})\nMERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)'
	single_row_anchor=$'MATCH (source_repo:Repository {id: $source_repo_id})\nMATCH (target_repo:Repository {id: $target_repo_id})\nMERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)\nSET rel.resolution_source = $resolution_source'
	marker="${fault_script}.restart-sentinel.once-fired"

	ifa_fault_assert_once_fault_marker() {
		local script_path="$1" expected="$2" contents
		[[ -f "${script_path}.restart-sentinel.once-fired" ]] || return 1
		contents="$(<"${script_path}.restart-sentinel.once-fired")" || return 1
		[[ "${contents}" == *"${expected}"* ]] || return 2
	}
	kill() {
		kill_checks=$((kill_checks + 1))
		[[ "$1" == "-0" && "$2" == "${reducer_pid}" && "${mode}" != "dead" ]] || return 1
		[[ "${mode}" != "dies-after-marker" || "${kill_checks}" -lt 2 ]]
	}
	sleep() { :; }

	printf '%s\n' "${operation}" >"${marker}"
	ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
	[[ "${kill_checks}" -eq 2 ]] || return 1
	mode=wrong-marker
	kill_checks=0
	printf '%s\n' 'MERGE (wrong)-[:EDGE]->(target)' >"${marker}"
	! ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
	printf '%s\n' "${sibling_anchor}" >"${marker}"
	! ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
	printf '%s\n' "${single_row_anchor}" >"${marker}"
	! ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
	mode=absent
	kill_checks=0
	rm -f "${marker}"
	! ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
	mode=dead
	kill_checks=0
	printf '%s\n' "${operation}" >"${marker}"
	! ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
	mode=dies-after-marker
	kill_checks=0
	! ifa_repo_dependency_fault_wait_for_once_marker \
		"${cell}" "${fault_script}" "${anchor}" 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${reducer_pid}" "${budget}" || return 1
)

run_ifa_repo_dependency_fault_script_json_controls() (
	local state_dir fault_script invalid_script anchor roundtrip
	state_dir="$(mktemp -d)" || return 1
	trap 'rm -rf "${state_dir}"' EXIT
	fault_script="${state_dir}/fault.json"
	invalid_script="${state_dir}/invalid.json"
	anchor=$'row.note = "quoted\\value"\nMERGE (source)-[:DEPENDS_ON]->(target)'
	ifa_fault_write_once_script "${fault_script}" "${anchor}" queue-retry || return 1
	jq -e . "${fault_script}" >/dev/null || return 1
	roundtrip="$(jq -er '.faults[0].trigger.operation_match' "${fault_script}")" || return 1
	[[ "${roundtrip}" == "${anchor}" ]] || return 1
	[[ "$(jq -r '.faults[0].target.lane' "${fault_script}")" == queue-retry ]] || return 1
	! ifa_fault_write_once_script "${invalid_script}" "${anchor}" invalid-lane || return 1
	[[ ! -e "${invalid_script}" ]] || return 1
	! ifa_fault_write_once_script "" "${anchor}" queue-retry || return 1
)

run_ifa_repo_dependency_graph_terminal_owner_controls() (
	local terminal_label="" events="" projector_pid reducer_before reducer_after failgraphwrite_repo_dependency=0
	local bin_dir=/fake/bin tagged_bin_dir=/fake/tagged log_dir=/fake/log work_dir=/fake/work
	local CLAIMED_ROW_WAIT_TIMEOUT=1
	local wall_times=()
	: "${failgraphwrite_repo_dependency}"
	ifa_repo_dependency_fault_prepare() { :; }
	ifa_fault_write_once_script() { :; }
	ifa_det_start_bg() { printf -v "$3" '%s' 765432; }
	ifa_repo_dependency_fault_wait_for_once_marker() { :; }
	ifa_repo_dependency_fault_wait_for_quarantine_telemetry() { :; }
	ifa_repo_dependency_fault_capture_partition_leases() { printf -v "$3" '%s' $'repo_dependency\t0\t4\towner'; }
	ifa_det_stop_join_untrack_bg_pid() { :; }
	ifa_repo_dependency_fault_require_partition_lease_subset() { :; }
	ifa_repo_dependency_fault_expire_partition_leases() { :; }
	run_drain_gate() { events="${events} drain"; }
	ifa_fault_assert_once_fault_marker() { :; }
	assert_no_dead_letters() { events="${events} dead"; }
	ifa_repo_dependency_live_assert_readiness_state() { terminal_label="$2"; events="${events} ready"; }
	ifa_repo_dependency_live_assert() { events="${events} graph"; }
	capture_digest() { :; }
	assert_matches_baseline() { :; }
	teardown_cell() { :; }
	repo_dependency_expected_edges=/fake/edges.json cell_failgraphwrite_repo_dependency || return 1
	[[ "${terminal_label}" == reducer-failgraphwrite_repo_dependency-before ]] || return 1
	[[ "${events}" == ' drain dead ready graph' ]] || return 1
)

run_ifa_repo_dependency_quarantine_telemetry_controls() (
	local mode=live reducer_pid=765432 budget=1 state_dir log_path marker_path record_tmp
	local fault_phrase='ifa fault: fail-graph-write-once-then-succeed (queue-retry) injected one failure for graph-write call #'
	state_dir="$(mktemp -d)" || return 1
	trap 'rm -rf "${state_dir}"' EXIT
	log_path="${state_dir}/reducer-failgraphwrite_repo_dependency-before.log"
	marker_path="${state_dir}/fault.restart-sentinel.once-fired"
	record_tmp="${state_dir}/record.tmp"
	sleep() { :; }
	kill() {
		[[ "$1" == "-0" && "$2" == "${reducer_pid}" && "${mode}" != "dead" ]]
	}
	write_record() {
		jq -cn \
			--arg domain "$1" --arg class "$2" --arg reason "$3" \
			--argjson duration "$4" --arg error "$5" \
			'{message:"repo dependency projection cycle failed",domain:$domain,failure_class:$class,lease_quarantined:true,quarantine_reason:$reason,quarantine_duration_seconds:$duration,error:$error}' \
			>"${log_path}"
	}
	local correct_error="quarantine repo dependency lease for 5m0s: ${fault_phrase}1"
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 300 "${correct_error}"
	ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1

	printf '%s\n' 'marker-fired' >"${marker_path}"
	: >"${log_path}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 300 "${correct_error}"
	jq -c '.message = "wrong message"' "${log_path}" >"${record_tmp}" && mv "${record_tmp}" "${log_path}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 300 "${correct_error}"
	jq -c '.lease_quarantined = false' "${log_path}" >"${record_tmp}" && mv "${record_tmp}" "${log_path}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record wrong_domain repo_dependency_projection_lease_quarantined cycle_error 300 "${correct_error}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency wrong_class cycle_error 300 "${correct_error}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency repo_dependency_projection_lease_quarantined wrong_reason 300 "${correct_error}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 299 "${correct_error}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 300 'quarantine without the injected fault phrase'
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 300 "${correct_error}"
	printf '%s\n' "$(<"${log_path}")" >>"${log_path}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
	mode=dead
	write_record repo_dependency repo_dependency_projection_lease_quarantined cycle_error 300 "${correct_error}"
	! ifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_path}" "${reducer_pid}" "${budget}" || return 1
)

# run_ifa_repo_dependency_partition_lease_controls moved to
# scripts/lib/test-ifa-fault-injection-repo-dependency-lease-cases.sh once this
# file crossed the 500-line cap -- still called below, unchanged.
run_ifa_fault_injection_repo_dependency_cases() {
	local root script shard cells live lease_lib fault_lib fixtures expected_cassette expected_edges baseline_line kill_line graph_line prerequisite_line maintenance_line lock_line claim_line release_line
	local gated_body terminal_body kill_body graph_body prerequisite_body prepare_body edge_type needle
	local graph_lifecycle_lines lifecycle_line omitted_graph_body misordered_graph_body repo_batch_anchor anchor_count production_go_files production_go_count scan_lines mutated_scan_lines
	local drive_line repo_drive_line omitted_drive_body misordered_drive_body
	root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${root}/scripts/verify-ifa-fault-injection.sh"
	shard="${root}/scripts/lib/ifa_fault_shard.sh"
	cells="${IFA_REPO_DEPENDENCY_CELLS_UNDER_TEST:-${root}/scripts/lib/ifa_fault_injection_repo_dependency_cells.sh}"
	live="${IFA_REPO_DEPENDENCY_LIVE_UNDER_TEST:-${root}/scripts/lib/ifa_repo_dependency_live.sh}"
	lease_lib="${IFA_REPO_DEPENDENCY_LEASE_UNDER_TEST:-${cells}}"
	fault_lib="${root}/scripts/lib/ifa_fault_injection_common.sh"
	fixtures="${root}/scripts/lib/ifa_family_fixtures.sh"
	expected_cassette="${root}/testdata/cassettes/repodependency/ifa-repo-dependency-family.json"
	expected_edges="${root}/go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"
	# shellcheck source=scripts/lib/ifa_family_fixtures.sh
	source "${fixtures}"
	[[ "${repo_dependency_cassette}" == "${expected_cassette}" && -f "${repo_dependency_cassette}" ]] || return 1
	[[ "${repo_dependency_expected_edges}" == "${expected_edges}" && -f "${repo_dependency_expected_edges}" ]] || return 1
	jq -e '
		(.scopes | length) == 7 and
		([.scopes[].facts | length] | add) == 18 and
		([.scopes[0:6][].facts | length] | all(. == 1)) and
		.scopes[6].metadata.repo_id == "repo-ifa-repo-dependency-source" and
		([.scopes[6].facts[] | select(.fact_kind == "shared_followup") | .payload.reducer_domain] | sort) == ["deployment_mapping", "workload_materialization"]
	' "${expected_cassette}" >/dev/null || return 1
	[[ -f "${cells}" ]] || { printf 'repo_dependency cells missing: %s\n' "${cells}" >&2; return 1; }
	[[ -f "${lease_lib}" ]] || { printf 'repo_dependency lease helper missing: %s\n' "${lease_lib}" >&2; return 1; }
	# shellcheck source=scripts/lib/ifa_repo_dependency_live.sh
	source "${live}"
	# shellcheck source=scripts/lib/ifa_fault_injection_repo_dependency_cells.sh
	source "${lease_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	for cell in cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency; do
		rg --line-regexp --quiet -- "ifa_fault_shard_run ${cell}" "${script}" || return 1
		rg --fixed-strings --quiet -- "${cell}" "${shard}" || return 1
	done
	rg --fixed-strings --quiet -- 'cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency' "${shard}" || return 1
	rg --fixed-strings --quiet -- 'materialize-platform-prerequisite' "${live}" || return 1
	prerequisite_body="$(rg -U --pcre2 --only-matching -- '(?ms)^ifa_repo_dependency_live_materialize_platform_prerequisite\(\) \{.*?^\}' "${live}")"
	[[ -n "${prerequisite_body}" ]] || return 1
	[[ "${prerequisite_body}" == *'ifa_repo_dependency_live_prerequisite_output_is_exact "${output}"'* ]] || return 1
	[[ "${prerequisite_body}" != *"expected_id"* ]] || return 1
	for output in \
		'platform_id=platform:kubernetes:none:cluster/prod-cluster:none:none verified=10' \
		'platform_id=platform:kubernetes:none:cluster/prod-cluster:none:none-suffix verified=1' \
		$'platform_id=platform:kubernetes:none:cluster/prod-cluster:none:none verified=1\nextra'; do
		! ifa_repo_dependency_live_prerequisite_output_is_exact "${output}" || return 1
	done
	ifa_repo_dependency_live_prerequisite_output_is_exact \
		'platform_id=platform:kubernetes:none:cluster/prod-cluster:none:none verified=1' || return 1
	gated_body="$(rg -U --pcre2 --only-matching -- '(?ms)^ifa_repo_dependency_live_assert_gated\(\) \{.*?^\}' "${live}")"
	[[ -n "${gated_body}" ]] || return 1
	[[ "${gated_body}" == *'count="$(jq '* ]] || return 1
	[[ "${gated_body}" == *'.props.evidence_source == "resolver/cross-repo"'* ]] || return 1
	for edge_type in PROVISIONS_DEPENDENCY_FOR USES_MODULE DISCOVERS_CONFIG_IN DEPENDS_ON DEPLOYS_FROM READS_CONFIG_FROM RUNS_ON; do
		[[ "${gated_body}" == *"\"${edge_type}\""* ]] || return 1
	done
	local fake_bin fixture edge_json
	fake_bin="$(mktemp -d)" || return 1
	fixture="${fake_bin}/graph.json"
	printf '%s\n' '#!/usr/bin/env bash' 'cp "${IFA_REPO_DEPENDENCY_GRAPH_FIXTURE}" "$3"' >"${fake_bin}/eshu-ifa"
	chmod +x "${fake_bin}/eshu-ifa"
	edge_json='{"type":"RUNS_ON","from":"workload","to":"platform","props":{"evidence_source":"reducer/workload-materialization"}}'
	printf '{"edges":[%s],"nodes":[]}\n' "${edge_json}" >"${fixture}"
	IFA_REPO_DEPENDENCY_GRAPH_FIXTURE="${fixture}" ifa_repo_dependency_live_assert_gated "${fake_bin}" || return 1
	for edge_type in PROVISIONS_DEPENDENCY_FOR USES_MODULE DISCOVERS_CONFIG_IN DEPENDS_ON DEPLOYS_FROM READS_CONFIG_FROM RUNS_ON; do
		edge_json="{\"type\":\"${edge_type}\",\"from\":\"source\",\"to\":\"target\",\"props\":{\"evidence_source\":\"resolver/cross-repo\"}}"
		printf '{"edges":[%s],"nodes":[]}\n' "${edge_json}" >"${fixture}"
		! IFA_REPO_DEPENDENCY_GRAPH_FIXTURE="${fixture}" ifa_repo_dependency_live_assert_gated "${fake_bin}" || return 1
	done
	rm -rf "${fake_bin}"
	terminal_body="$(rg -U --pcre2 --only-matching -- '(?ms)^ifa_repo_dependency_fault_assert_terminal\(\) \{.*?^\}' "${cells}")"
	prepare_body="$(rg -U --pcre2 --only-matching -- '(?ms)^ifa_repo_dependency_fault_prepare\(\) \{.*?^\}' "${cells}")"
	kill_body="$(rg -U --pcre2 --only-matching -- '(?ms)^cell_killworker_repo_dependency\(\) \{.*?^\}' "${cells}")"
	graph_body="$(rg -U --pcre2 --only-matching -- '(?ms)^cell_failgraphwrite_repo_dependency\(\) \{.*?^\}' "${cells}")"
	[[ -n "${terminal_body}" && -n "${prepare_body}" && -n "${kill_body}" && -n "${graph_body}" ]] || return 1
	ifa_repo_dependency_prepare_has_required_order "${prepare_body}" || return 1
	repo_batch_anchor=$'target_repo.generation_id = row.generation_id\nMERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)\nSET rel.confidence = row.confidence'
	scan_lines="$(rg --pcre2 --only-matching -- '^[[:space:]]*(?:production_go_files|anchor_count)=.*$' "${BASH_SOURCE[0]}")" || return 1
	[[ "$(printf '%s\n' "${scan_lines}" | rg --fixed-strings --count -- '"${root}"')" == 2 ]] || return 1
	mutated_scan_lines="$(printf '%s\n' "${scan_lines}" | sed '/^[[:space:]]*anchor_count=/s#"${root}"#"${root}/go"#')"
	[[ "$(printf '%s\n' "${mutated_scan_lines}" | rg --fixed-strings --count -- '"${root}"')" == 1 ]] || return 1
	production_go_files="$(rg --files "${root}" -g '*.go' -g '!*_test.go')" || return 1
	production_go_count="$(printf '%s\n' "${production_go_files}" | wc -l | tr -d '[:space:]')"
	[[ "${production_go_count}" =~ ^[0-9]+$ && "${production_go_count}" -gt 100 ]] || return 1
	! printf '%s\n' "${production_go_files}" | rg --quiet -- '_test\.go$' || return 1
	[[ "${production_go_files}" == *'/go/internal/storage/cypher/canonical.go'* ]] || return 1
	[[ "${production_go_files}" == *'/go/internal/reducer/workload_materializer.go'* ]] || return 1
	anchor_count="$(rg -U -g '*.go' -g '!*_test.go' --fixed-strings --json -- "${repo_batch_anchor}" "${root}" \
		| jq -s '[.[] | select(.type == "match")] | length')"
	[[ "${anchor_count}" == 1 ]] || return 1
	rg --fixed-strings --quiet -- 'target_repo.generation_id = row.generation_id\nMERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)\nSET rel.confidence = row.confidence' "${cells}" || return 1
	rg --fixed-strings --quiet -- "'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)'" "${cells}" || return 1
	! rg --fixed-strings --quiet -- 'anchor="MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)"' "${cells}" || return 1
	drive_line=$'\tdrive_all_cassettes "${cell}"'
	repo_drive_line=$'\tifa_repo_dependency_live_drive "${bin_dir}" "${repo_dependency_cassette}" || die "${cell}: cassette drive failed"'
	[[ "${prepare_body}" == *"${drive_line}"* && "${prepare_body}" == *"${repo_drive_line}"* ]] || return 1
	omitted_drive_body="${prepare_body/"${drive_line}"$'\n'/}"
	! ifa_repo_dependency_prepare_has_required_order "${omitted_drive_body}" || return 1
	misordered_drive_body="${omitted_drive_body/"${repo_drive_line}"/"${repo_drive_line}"$'\n'"${drive_line}"}"
	! ifa_repo_dependency_prepare_has_required_order "${misordered_drive_body}" || return 1
	for needle in assert_no_dead_letters ifa_repo_dependency_live_assert_readiness_state ifa_repo_dependency_live_assert; do
		[[ "${terminal_body}" == *"${needle}"* ]] || return 1
	done
	ifa_repo_dependency_body_has_order "${terminal_body}" \
		assert_no_dead_letters ifa_repo_dependency_live_assert_readiness_state \
		'ifa_repo_dependency_live_assert "${bin_dir}" "${repo_dependency_expected_edges}"' || return 1
	for needle in ifa_repo_dependency_fault_prepare ifa_fault_start_shared_intent_lock ifa_fault_wait_for_claimed 'deployment_mapping' pre_kill_partition_leases ' KILL ' post_kill_partition_leases ifa_repo_dependency_fault_require_partition_lease_subset ifa_repo_dependency_fault_expire_partition_leases ifa_fault_release_shared_intent_lock 'reducer-${cell}-after' run_drain_gate ifa_repo_dependency_fault_assert_terminal ifa_fault_assert_retried_above capture_digest assert_matches_baseline teardown_cell; do
		[[ "${kill_body}" == *"${needle}"* ]] || return 1
	done
	ifa_repo_dependency_body_has_order "${kill_body}" \
		ifa_repo_dependency_fault_prepare \
		'ifa_det_start_bg "${log_dir}" "projector-${cell}"' \
		ifa_fault_start_shared_intent_lock \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-before"' \
		ifa_fault_wait_for_claimed \
		'ifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" pre_kill_partition_leases' \
		'ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL' \
		'ifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" post_kill_partition_leases' \
		ifa_repo_dependency_fault_require_partition_lease_subset \
		'ifa_repo_dependency_fault_expire_partition_leases "${cell}" "${reducer_before}" "${post_kill_partition_leases}"' \
		ifa_fault_release_shared_intent_lock \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-after"' \
		run_drain_gate ifa_repo_dependency_fault_assert_terminal \
		ifa_fault_assert_retried_above capture_digest assert_matches_baseline teardown_cell || return 1
	for needle in ifa_repo_dependency_fault_prepare 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' ifa_fault_write_once_script ESHU_IFA_FAULT_SCRIPT run_drain_gate ifa_fault_assert_once_fault_marker ifa_repo_dependency_fault_assert_terminal capture_digest assert_matches_baseline teardown_cell; do
		[[ "${graph_body}" == *"${needle}"* ]] || return 1
	done
	ifa_repo_dependency_failgraphwrite_has_required_order "${graph_body}" || return 1
	graph_lifecycle_lines=$'ifa_repo_dependency_fault_wait_for_once_marker\nifa_repo_dependency_fault_wait_for_quarantine_telemetry "${log_dir}/reducer-${cell}-before.log" "${reducer_before}" "${CLAIMED_ROW_WAIT_TIMEOUT}"\nifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" pre_fault_partition_leases\nifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL\nifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" post_fault_partition_leases\nifa_repo_dependency_fault_require_partition_lease_subset\nifa_repo_dependency_fault_expire_partition_leases "${cell}" "${reducer_before}" "${post_fault_partition_leases}"\nifa_det_start_bg "${log_dir}" "reducer-${cell}-after" reducer_after "${bin_dir}/eshu-reducer"'
	while IFS= read -r lifecycle_line; do
		omitted_graph_body="${graph_body/"${lifecycle_line}"/}"
		! ifa_repo_dependency_failgraphwrite_has_required_order "${omitted_graph_body}" || return 1
		misordered_graph_body="${lifecycle_line}"$'\n'"${omitted_graph_body}"
		! ifa_repo_dependency_failgraphwrite_has_required_order "${misordered_graph_body}" || return 1
	done <<<"${graph_lifecycle_lines}"
	rg --fixed-strings --line-regexp --quiet -- $'\tifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}"' "${cells}" || return 1
	rg --fixed-strings --line-regexp --quiet -- $'\tifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}-after"' "${cells}" || return 1
	ifa_repo_dependency_graph_terminal_owner_is_exact "${graph_body}" || return 1
	local wrong_terminal_body="${graph_body/'ifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}-before"'/'ifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}-after"'}"
	! ifa_repo_dependency_graph_terminal_owner_is_exact "${wrong_terminal_body}" || return 1
	rg --fixed-strings --quiet -- 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' "${cells}" || return 1
	rg --fixed-strings --quiet -- 'baseline_deployment_mapping_retried' "${cells}" || return 1
	rg --fixed-strings --quiet -- 'ifa_repo_dependency_pre_gate_dump_path' "${live}" || return 1
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
	run_ifa_repo_dependency_partition_lease_controls || return 1
	run_ifa_repo_dependency_fault_marker_wait_controls || return 1
	run_ifa_repo_dependency_fault_script_json_controls || return 1
	run_ifa_repo_dependency_graph_terminal_owner_controls || return 1
	run_ifa_repo_dependency_quarantine_telemetry_controls || return 1
	# 49 = 1 shared cell_baseline + 48 IFA_FAULT_ALL_CELLS entries
	# (scripts/lib/ifa_fault_shard.sh). #5995/#6000/#5997 added the shared
	# symbol-runtime baseline and three graph-write cells; #6208 adds the
	# trio's three runner-lease kill/reclaim cells; #6309 adds the two
	# direct-family trios (kubernetes_namespace_environment,
	# iam_instance_profile_role).
	[[ "$("${script}" --list-cells | wc -l | tr -d '[:space:]')" == 49 ]] || return 1
}
