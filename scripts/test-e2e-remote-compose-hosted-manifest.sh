#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LIB="${REPO_ROOT}/scripts/lib/e2e_remote_compose_manifest.sh"
VALIDATOR="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

die() {
	printf 'remote-compose-hosted-manifest-test: %s\n' "$*" >&2
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
hosted_rows=(pagerduty jira grafana prometheus_mimir loki tempo)

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
		proof_id: "remote-compose-hosted-readback-test",
		surfaces: {
			api: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0},
			mcp: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0},
			cli: {status: "pass", checked: 11, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0}
		}
	}' >"${TMP_DIR}/readback.json"
}

write_services() {
	local output="$1" mode="$2"
	case "${mode}" in
		enabled)
			jq -n '[
				"collector-pagerduty",
				"collector-jira",
				"collector-grafana",
				"collector-prometheus-mimir",
				"collector-loki",
				"collector-tempo"
			]' >"${output}"
			;;
		disabled)
			jq -n '[]' >"${output}"
			;;
		*) die "unknown service mode: ${mode}" ;;
	esac
}

write_fact_rows() {
	local output="$1" mode="$2"
	jq -n --arg mode "${mode}" '
		[
			{source_system: "git", fact_kind: "repository", count: 10},
			{source_system: "terraform_state", fact_kind: "terraform_state.resource", count: 5},
			{source_system: "aws", fact_kind: "aws_resource", count: 7},
			{source_system: "oci_registry", fact_kind: "oci_registry.image_manifest", count: 4},
			{source_system: "package_registry", fact_kind: "package_registry.package_version", count: 6},
			{source_system: "sbom_document", fact_kind: "sbom.component", count: 2},
			{source_system: "security_alert", fact_kind: "security_alert.repository_alert", count: 4},
			{source_system: "vulnerability_intelligence", fact_kind: "vulnerability.affected_package", count: 8},
			{source_system: "scanner_worker", fact_kind: "scanner_worker.vulnerability", count: 3},
			{source_system: "confluence", fact_kind: "documentation_source", count: 1},
			{source_system: "reducer", fact_kind: "reducer_package_correlation", count: 10},
			{source_system: "reducer", fact_kind: "reducer_aws_cloud_relationship", count: 5},
			{source_system: "reducer", fact_kind: "reducer_container_image_identity", count: 3},
			{source_system: "reducer", fact_kind: "reducer_sbom_attestation_attachment", count: 3},
			{source_system: "reducer", fact_kind: "reducer_security_alert_reconciliation", count: 4},
			{source_system: "reducer", fact_kind: "reducer_supply_chain_impact_finding", count: 4},
			{source_system: "reducer", fact_kind: "reducer_deployment_correlation", count: 2},
			{source_system: "reducer", fact_kind: "reducer_observability_correlation", count: 1},
			{source_system: "reducer", fact_kind: "reducer_incident_work_item_correlation", count: 1}
		] +
		if $mode == "with_hosted" then [
			{source_system: "pagerduty", fact_kind: "incident.record", count: 1},
			{source_system: "jira", fact_kind: "work_item.record", count: 1},
			{source_system: "grafana", fact_kind: "observability.grafana_dashboard", count: 1},
			{source_system: "prometheus_mimir", fact_kind: "observability.metric_signal", count: 1},
			{source_system: "loki", fact_kind: "observability.log_signal", count: 1},
			{source_system: "tempo", fact_kind: "observability.trace_signal", count: 1}
		] else [] end
	' >"${output}"
}

build_case_manifest() {
	local facts="$1" services="$2" output="$3"
	build_manifest \
		"${facts}" \
		"${TMP_DIR}/workflow.json" \
		"${TMP_DIR}/reducer-counts.json" \
		"${TMP_DIR}/index-status.json" \
		"${services}" \
		"${TMP_DIR}/stats.jsonl" \
		"${TMP_DIR}/readback.json" \
		"${output}"
	"${VALIDATOR}" "${output}" >/dev/null
}

assert_all_hosted_rows() {
	local manifest="$1" status="$2" reason="$3" facts="${4:-0}"
	local row
	for row in "${hosted_rows[@]}"; do
		jq -e --arg row "${row}" --arg status "${status}" --arg reason "${reason}" --argjson facts "${facts}" '
			.collectors[$row].status == $status and
			.collectors[$row].facts == $facts and
			.collectors[$row].reason == $reason
		' "${manifest}" >/dev/null || die "${row} was not classified as ${status}"
	done
}

write_common_inputs

write_services "${TMP_DIR}/services-enabled.json" enabled
write_services "${TMP_DIR}/services-disabled.json" disabled
write_fact_rows "${TMP_DIR}/facts-with-hosted.json" with_hosted
write_fact_rows "${TMP_DIR}/facts-without-hosted.json" without_hosted

build_case_manifest \
	"${TMP_DIR}/facts-with-hosted.json" \
	"${TMP_DIR}/services-enabled.json" \
	"${TMP_DIR}/hosted-pass.json"
jq -e '
	. as $root |
	all(["pagerduty","jira","grafana","prometheus_mimir","loki","tempo"][]; . as $row |
		$root.collectors[$row].status == "pass" and $root.collectors[$row].facts == 1)
' "${TMP_DIR}/hosted-pass.json" >/dev/null || die "enabled hosted collectors with facts did not pass"

build_case_manifest \
	"${TMP_DIR}/facts-without-hosted.json" \
	"${TMP_DIR}/services-enabled.json" \
	"${TMP_DIR}/hosted-failed.json"
assert_all_hosted_rows \
	"${TMP_DIR}/hosted-failed.json" \
	fail \
	"no source facts observed for enabled collector service"

unsupported_hosted_collectors="pagerduty,jira,grafana,prometheus_mimir,loki,tempo"
build_case_manifest \
	"${TMP_DIR}/facts-without-hosted.json" \
	"${TMP_DIR}/services-enabled.json" \
	"${TMP_DIR}/hosted-unsupported-cannot-mask-enabled.json"
unsupported_hosted_collectors=""
assert_all_hosted_rows \
	"${TMP_DIR}/hosted-unsupported-cannot-mask-enabled.json" \
	fail \
	"no source facts observed for enabled collector service"

build_case_manifest \
	"${TMP_DIR}/facts-without-hosted.json" \
	"${TMP_DIR}/services-disabled.json" \
	"${TMP_DIR}/hosted-skipped.json"
assert_all_hosted_rows \
	"${TMP_DIR}/hosted-skipped.json" \
	skipped \
	"collector service disabled in remote Compose profile"

build_case_manifest \
	"${TMP_DIR}/facts-with-hosted.json" \
	"${TMP_DIR}/services-disabled.json" \
	"${TMP_DIR}/hosted-disabled-with-facts.json"
assert_all_hosted_rows \
	"${TMP_DIR}/hosted-disabled-with-facts.json" \
	fail \
	"source facts observed while collector service disabled in remote Compose profile" \
	1

unsupported_hosted_collectors="pagerduty,jira,grafana,prometheus_mimir,loki,tempo"
build_case_manifest \
	"${TMP_DIR}/facts-without-hosted.json" \
	"${TMP_DIR}/services-disabled.json" \
	"${TMP_DIR}/hosted-unsupported.json"
unsupported_hosted_collectors=""
assert_all_hosted_rows \
	"${TMP_DIR}/hosted-unsupported.json" \
	unsupported \
	"collector explicitly unsupported in remote Compose profile"

printf 'e2e remote compose hosted manifest tests passed\n'
