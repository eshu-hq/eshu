#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LIB="${REPO_ROOT}/scripts/lib/e2e_remote_compose_manifest.sh"
VALIDATOR="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

die() {
	printf 'remote-compose-reducer-manifest-test: %s\n' "$*" >&2
	exit 1
}

# shellcheck source=scripts/lib/e2e_remote_compose_manifest.sh
source "${LIB}"

run_kind="clean"
commit_override="123456789abc"
image_tag_candidate="v0.0.3-pre-release-test"
backend_kind="nornicdb"
corpus_mode="representative"
repository_count=24
unsupported_hosted_collectors=""
unsupported_reducers=""
corpus_coverage="${TMP_DIR}/corpus-coverage.json"
runtime_volume_proof="${TMP_DIR}/runtime-volume.json"

write_common_inputs() {
	jq -n '{
		ecosystems: {
			npm: {status: "pass", count: 3},
			gomod: {status: "pass", count: 2},
			pypi: {status: "pass", count: 2},
			maven: {status: "pass", count: 2},
			composer: {status: "pass", count: 1},
			rubygems: {status: "pass", count: 1},
			cargo: {status: "pass", count: 1},
			nuget: {status: "pass", count: 1}
		},
		evidence_families: {
			terraform_iac: {status: "pass", count: 2},
			kubernetes_iac: {status: "pass", count: 2},
			image_sbom: {status: "pass", count: 2},
			deployment: {status: "pass", count: 2},
			vulnerability: {status: "pass", count: 4},
			observability: {status: "pass", count: 3},
			incident: {status: "pass", count: 1},
			work_item: {status: "pass", count: 1}
		}
	}' >"${corpus_coverage}"
	jq -n '{
		schema_version: 1,
		run_kind: "clean",
		clean_volume_state: "reset_before_run",
		backing_stores: {
			nornicdb_data: {status: "pass", before: "absent", after: "present"},
			postgres_data: {status: "pass", before: "absent", after: "present"},
			eshu_data: {status: "pass", before: "absent", after: "present"}
		}
	}' >"${runtime_volume_proof}"
	jq -n '{queue: {pending: 0, in_flight: 0, retrying: 0, failed: 0, dead_letter: 0}}' \
		>"${TMP_DIR}/index-status.json"
	jq -n '["scanner-worker","collector-pagerduty","collector-jira","collector-grafana","collector-prometheus-mimir","collector-loki","collector-tempo"]' \
		>"${TMP_DIR}/services.json"
	printf '{"Name":"eshu","CPUPerc":"1.0%%","MemUsage":"64MiB / 1GiB"}\n' \
		>"${TMP_DIR}/stats.jsonl"
	jq -n '[
		{collector_kind: "git", status: "completed", count: 1},
		{collector_kind: "terraform_state", status: "completed", count: 1}
	]' >"${TMP_DIR}/workflow.json"
	jq -n '[
		{reducer: "terraform_iac_relationships", source_facts: 5, reducer_facts: 5}
	]' >"${TMP_DIR}/reducer-counts.json"
	jq -n '{
		schema_version: 1,
		proof_id: "remote-compose-reducer-readback-test",
		surfaces: {
			api: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0},
			mcp: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0},
			cli: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0}
		}
	}' >"${TMP_DIR}/readback.json"
}

write_fact_rows() {
	local output="$1"
	jq -n '[
		{source_system: "git", fact_kind: "repository", count: 10},
		{source_system: "terraform_state", fact_kind: "terraform_state.resource", count: 5},
		{source_system: "aws", fact_kind: "aws_resource", count: 7},
		{source_system: "oci_registry", fact_kind: "oci_registry.image_manifest", count: 4},
		{source_system: "package_registry", fact_kind: "package_registry.package_version", count: 6},
		{source_system: "sbom_document", fact_kind: "sbom.component", count: 2},
		{source_system: "security_alert", fact_kind: "security_alert.repository_alert", count: 4},
		{source_system: "vulnerability_intelligence", fact_kind: "vulnerability.affected_package", count: 8},
		{source_system: "scanner_worker", fact_kind: "scanner_worker.warning", count: 3},
		{source_system: "confluence", fact_kind: "documentation_source", count: 1},
		{source_system: "pagerduty", fact_kind: "incident.record", count: 1},
		{source_system: "jira", fact_kind: "work_item.record", count: 1},
		{source_system: "grafana", fact_kind: "observability.observed_dashboard", count: 1},
		{source_system: "prometheus_mimir", fact_kind: "observability.observed_target", count: 1},
		{source_system: "loki", fact_kind: "observability.observed_log_signal", count: 1},
		{source_system: "tempo", fact_kind: "observability.observed_trace_signal", count: 1},
		{source_system: "reducer", fact_kind: "reducer_package_correlation", count: 10},
		{source_system: "reducer", fact_kind: "reducer_aws_cloud_relationship", count: 5},
		{source_system: "reducer", fact_kind: "reducer_container_image_identity", count: 3},
		{source_system: "reducer", fact_kind: "reducer_sbom_attestation_attachment", count: 3},
		{source_system: "reducer", fact_kind: "reducer_security_alert_reconciliation", count: 4},
		{source_system: "reducer", fact_kind: "reducer_supply_chain_impact_finding", count: 4},
		{source_system: "reducer", fact_kind: "reducer_deployment_correlation", count: 2},
		{source_system: "reducer", fact_kind: "reducer_observability_correlation", count: 1},
		{source_system: "reducer", fact_kind: "reducer_incident_work_item_correlation", count: 1}
	]' >"${output}"
}

build_case_manifest() {
	local facts="$1" readback="$2" output="$3"
	build_manifest \
		"${facts}" \
		"${TMP_DIR}/workflow.json" \
		"${TMP_DIR}/reducer-counts.json" \
		"${TMP_DIR}/index-status.json" \
		"${TMP_DIR}/services.json" \
		"${TMP_DIR}/stats.jsonl" \
		"${readback}" \
		"${output}"
}

write_common_inputs
facts="${TMP_DIR}/facts.json"
write_fact_rows "${facts}"

present="${TMP_DIR}/present.json"
build_case_manifest "${facts}" "${TMP_DIR}/readback.json" "${present}"
"${VALIDATOR}" "${present}" >/dev/null
jq -e '
	.status == "pass" and
	.reducers.terraform_iac_relationships.source_facts == 5 and
	.reducers.terraform_iac_relationships.reducer_facts == 5 and
	.collectors.sbom_document.status == "pass" and
	.collectors.sbom_document.facts == 2 and
	.collectors.scanner_worker.status == "pass" and
	.collectors.scanner_worker.source_facts == 3 and
	.collectors.scanner_worker.warnings == 3 and
	.reducers.vulnerability_matching.status == "pass" and
	.reducers.vulnerability_matching.reducer_facts == 4 and
	.reducers.vulnerability_matching.readback.api.status == "pass"
' "${present}" >/dev/null || die "present reducer classification was not preserved"

observability_source_kind_mapped="${TMP_DIR}/observability-source-kind-mapped.json"
jq 'map(if (.fact_kind | startswith("observability.")) then .source_system = "git" else . end)' \
	"${facts}" >"${TMP_DIR}/facts-observability-source-kind-mapped.json"
build_case_manifest "${TMP_DIR}/facts-observability-source-kind-mapped.json" "${TMP_DIR}/readback.json" "${observability_source_kind_mapped}"
jq -e '
	.reducers.observability_correlation.status == "pass" and
	.reducers.observability_correlation.source_facts == 11 and
	.reducers.observability_correlation.reducer_facts == 1
' "${observability_source_kind_mapped}" >/dev/null \
	|| die "observability source fact-kind mapping was not counted"

observability_aws_input_mapped="${TMP_DIR}/observability-aws-input-mapped.json"
jq 'map(select(.fact_kind | startswith("observability.") | not))' "${facts}" \
	>"${TMP_DIR}/facts-observability-aws-input-mapped.json"
build_case_manifest "${TMP_DIR}/facts-observability-aws-input-mapped.json" "${TMP_DIR}/readback.json" "${observability_aws_input_mapped}"
jq -e '
	.reducers.observability_correlation.status == "pass" and
	.reducers.observability_correlation.source_facts == 7 and
	.reducers.observability_correlation.reducer_facts == 1
' "${observability_aws_input_mapped}" >/dev/null \
	|| die "observability AWS source input mapping was not counted"

scanner_runtime_only="${TMP_DIR}/scanner-runtime-only.json"
jq 'map(select(.source_system != "scanner_worker"))' "${facts}" >"${TMP_DIR}/facts-without-scanner-source.json"
build_case_manifest "${TMP_DIR}/facts-without-scanner-source.json" "${TMP_DIR}/readback.json" "${scanner_runtime_only}"
jq -e '
	.status == "partial" and
	.collectors.scanner_worker.status == "skipped" and
	.collectors.scanner_worker.source_facts == 0 and
	.collectors.scanner_worker.warnings == 0 and
	.collectors.scanner_worker.runtime_status == "pass" and
	.collectors.scanner_worker.reason == "scanner-worker runtime healthy but no source-evidence claims completed in this proof"
' "${scanner_runtime_only}" >/dev/null || die "runtime-only scanner-worker proof was not classified as skipped source evidence"

scanner_completed_without_evidence="${TMP_DIR}/scanner-completed-without-evidence.json"
jq '. + [{collector_kind: "scanner_worker", status: "completed", count: 1}]' \
	"${TMP_DIR}/workflow.json" >"${TMP_DIR}/workflow-scanner-completed.json"
build_manifest \
	"${TMP_DIR}/facts-without-scanner-source.json" \
	"${TMP_DIR}/workflow-scanner-completed.json" \
	"${TMP_DIR}/reducer-counts.json" \
	"${TMP_DIR}/index-status.json" \
	"${TMP_DIR}/services.json" \
	"${TMP_DIR}/stats.jsonl" \
	"${TMP_DIR}/readback.json" \
	"${scanner_completed_without_evidence}"
jq -e '
	.status == "fail" and
	.collectors.scanner_worker.status == "fail" and
	.collectors.scanner_worker.completed_claims == 1 and
	.collectors.scanner_worker.reason == "completed scanner-worker claims emitted no source or warning facts"
' "${scanner_completed_without_evidence}" >/dev/null || die "completed scanner-worker claim without evidence was not classified as failed source evidence"

missing="${TMP_DIR}/missing.json"
jq 'map(select(.fact_kind != "reducer_supply_chain_impact_finding"))' "${facts}" >"${TMP_DIR}/facts-missing.json"
build_case_manifest "${TMP_DIR}/facts-missing.json" "${TMP_DIR}/readback.json" "${missing}"
jq -e '
	.status == "fail" and
	.reducers.vulnerability_matching.status == "fail" and
	.reducers.vulnerability_matching.reason == "no reducer evidence observed"
' "${missing}" >/dev/null || die "missing reducer evidence was not explicit"

incident_work_item_source_only="${TMP_DIR}/incident-work-item-source-only.json"
jq 'map(select(.fact_kind != "reducer_incident_work_item_correlation"))' "${facts}" >"${TMP_DIR}/facts-incident-source-only.json"
build_case_manifest "${TMP_DIR}/facts-incident-source-only.json" "${TMP_DIR}/readback.json" "${incident_work_item_source_only}"
"${VALIDATOR}" "${incident_work_item_source_only}" >/dev/null
jq -e '
	.status == "partial" and
	.reducers.incident_work_item_correlation.status == "unsupported" and
	.reducers.incident_work_item_correlation.source_facts == 2 and
	.reducers.incident_work_item_correlation.reducer_facts == 0 and
	.reducers.incident_work_item_correlation.reason == "incident and work-item evidence is source and API read-model evidence today; no reducer-owned incident work-item correlation fact is implemented" and
	(.reducers.incident_work_item_correlation.issue_refs | index("#1249"))
' "${incident_work_item_source_only}" >/dev/null \
	|| die "source-only incident/work-item evidence was not classified explicitly"

incident_work_item_missing_reducer_and_readback="${TMP_DIR}/incident-work-item-missing-reducer-and-readback.json"
write_missing_readback_proof "${TMP_DIR}/readback-incident-missing.json"
build_case_manifest "${TMP_DIR}/facts-incident-source-only.json" "${TMP_DIR}/readback-incident-missing.json" "${incident_work_item_missing_reducer_and_readback}"
jq -e '
	.status == "fail" and
	.reducers.incident_work_item_correlation.status == "fail" and
	.reducers.incident_work_item_correlation.source_facts == 2 and
	.reducers.incident_work_item_correlation.reducer_facts == 0 and
	.reducers.incident_work_item_correlation.reason == "no reducer evidence observed"
' "${incident_work_item_missing_reducer_and_readback}" >/dev/null \
	|| die "missing incident/work-item reducer evidence was not classified before readback"

unsupported="${TMP_DIR}/unsupported.json"
jq 'map(select(.fact_kind != "reducer_incident_work_item_correlation"))' "${facts}" >"${TMP_DIR}/facts-unsupported.json"
unsupported_reducers="incident_work_item_correlation"
build_case_manifest "${TMP_DIR}/facts-unsupported.json" "${TMP_DIR}/readback.json" "${unsupported}"
unsupported_reducers=""
jq -e '
	.status == "partial" and
	.reducers.incident_work_item_correlation.status == "unsupported" and
	.reducers.incident_work_item_correlation.reason == "reducer path explicitly unsupported in remote Compose profile"
' "${unsupported}" >/dev/null || die "unsupported reducer path was not explicit"

missing_readback="${TMP_DIR}/missing-readback.json"
write_missing_readback_proof "${TMP_DIR}/readback-missing.json"
build_case_manifest "${facts}" "${TMP_DIR}/readback-missing.json" "${missing_readback}"
jq -e '
	.status == "fail" and
	.readback.api.status == "fail" and
	.reducers.vulnerability_matching.reason == "API/MCP readback proof missing or failed"
' "${missing_readback}" >/dev/null || die "missing readback proof was not explicit"

printf 'e2e remote compose reducer manifest tests passed\n'
