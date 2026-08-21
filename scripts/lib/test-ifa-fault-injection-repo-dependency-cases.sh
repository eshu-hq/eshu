#!/usr/bin/env bash
# Hermetic structural proof for repo_dependency's maintenance-backed trio.
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

run_ifa_fault_injection_repo_dependency_cases() {
	local root script shard cells live fixtures expected_cassette expected_edges baseline_line kill_line graph_line prerequisite_line maintenance_line lock_line claim_line release_line
	local gated_body terminal_body kill_body graph_body prerequisite_body prepare_body edge_type needle
	local drive_line repo_drive_line omitted_drive_body misordered_drive_body
	root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${root}/scripts/verify-ifa-fault-injection.sh"
	shard="${root}/scripts/lib/ifa_fault_shard.sh"
	cells="${IFA_REPO_DEPENDENCY_CELLS_UNDER_TEST:-${root}/scripts/lib/ifa_fault_injection_repo_dependency_cells.sh}"
	live="${IFA_REPO_DEPENDENCY_LIVE_UNDER_TEST:-${root}/scripts/lib/ifa_repo_dependency_live.sh}"
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
	# shellcheck source=scripts/lib/ifa_repo_dependency_live.sh
	source "${live}"
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
	for needle in ifa_repo_dependency_fault_prepare ifa_fault_start_shared_intent_lock ifa_fault_wait_for_claimed 'deployment_mapping' ' KILL ' ifa_fault_release_shared_intent_lock 'reducer-${cell}-after' run_drain_gate ifa_repo_dependency_fault_assert_terminal ifa_fault_assert_retried_above capture_digest assert_matches_baseline teardown_cell; do
		[[ "${kill_body}" == *"${needle}"* ]] || return 1
	done
	ifa_repo_dependency_body_has_order "${kill_body}" \
		ifa_repo_dependency_fault_prepare \
		'ifa_det_start_bg "${log_dir}" "projector-${cell}"' \
		ifa_fault_start_shared_intent_lock \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-before"' \
		ifa_fault_wait_for_claimed \
		'ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL' \
		ifa_fault_release_shared_intent_lock \
		'ifa_det_start_bg "${log_dir}" "reducer-${cell}-after"' \
		run_drain_gate ifa_repo_dependency_fault_assert_terminal \
		ifa_fault_assert_retried_above capture_digest assert_matches_baseline teardown_cell || return 1
	for needle in ifa_repo_dependency_fault_prepare 'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' ifa_fault_write_once_script ESHU_IFA_FAULT_SCRIPT run_drain_gate ifa_fault_assert_once_fault_marker ifa_repo_dependency_fault_assert_terminal capture_digest assert_matches_baseline teardown_cell; do
		[[ "${graph_body}" == *"${needle}"* ]] || return 1
	done
	ifa_repo_dependency_body_has_order "${graph_body}" \
		ifa_repo_dependency_fault_prepare \
		'MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)' \
		ifa_fault_write_once_script \
		'ESHU_IFA_FAULT_SCRIPT=${fault_script}' \
		run_drain_gate ifa_fault_assert_once_fault_marker \
		ifa_repo_dependency_fault_assert_terminal capture_digest assert_matches_baseline teardown_cell || return 1
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
	[[ "$("${script}" --list-cells | wc -l | tr -d '[:space:]')" == 24 ]] || return 1
}
