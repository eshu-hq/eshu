#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LIB="${REPO_ROOT}/scripts/lib/e2e_evidence_manifest.sh"
WRAPPER="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"

if [[ ! -f "${LIB}" ]]; then
	printf 'missing manifest validator library at %s\n' "${LIB}" >&2
	exit 1
fi

# shellcheck source=scripts/lib/e2e_evidence_manifest.sh
source "${LIB}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

write_valid_manifest() {
	local path="$1"
	jq -n '
	def reducer($source; $count): {
		status: "pass",
		source_facts: $source,
		reducer_facts: $count,
		count: $count,
		readback: {
			api: {status: "pass", checked: 12, failed: 0, truncated: 0},
			mcp: {status: "pass", checked: 12, failed: 0, truncated: 0}
		}
	};
	{
		schema_version: 1,
		status: "pass",
		run: {
			id: "e2e-manifest-test-run",
			kind: "clean",
			commit: "1234567890abcdef",
			image_tag_candidate: "v0.0.3-pre-release-test",
			backend: {
				kind: "nornicdb",
				digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"
			}
		},
		corpus: {
			mode: "representative",
			repository_count: 24,
			coverage: {
				ecosystems: {
					npm: {status: "pass", count: 3},
					gomod: {status: "pass", count: 3},
					pypi: {status: "pass", count: 2},
					maven: {status: "pass", count: 2},
					composer: {status: "pass", count: 2},
					rubygems: {status: "pass", count: 1},
					cargo: {status: "pass", count: 1},
					nuget: {status: "pass", count: 1}
				},
				evidence_families: {
					terraform_iac: {status: "pass", count: 3},
					kubernetes_iac: {status: "pass", count: 2},
					image_sbom: {status: "pass", count: 2},
					deployment: {status: "pass", count: 2},
					relationship_evidence: {status: "pass", count: 2},
					vulnerability: {status: "pass", count: 4},
					observability: {status: "pass", count: 1},
					incident: {status: "pass", count: 1},
					work_item: {status: "pass", count: 1}
				}
			}
		},
		runtimes: {
			schema_bootstrap: {status: "pass"},
			api: {status: "pass"},
			mcp_server: {status: "pass"},
			ingester: {status: "pass"},
			resolution_engine: {status: "pass"},
			workflow_coordinator: {status: "pass"},
			hosted_collectors: {status: "pass"},
			scanner_worker: {status: "pass"}
		},
		collectors: {
			git: {status: "pass", facts: 10},
			terraform_state: {status: "pass", facts: 5},
			aws_cloud: {status: "pass", facts: 7},
			oci_registry: {status: "pass", facts: 4},
			package_registry: {status: "pass", facts: 6},
			sbom_document: {status: "pass", source_facts: 2},
			provider_security_alerts: {status: "pass", facts: 4},
			vulnerability_intelligence: {status: "pass", facts: 8},
			scanner_worker: {status: "pass", warnings: 1},
			confluence: {status: "pass", facts: 1},
			pagerduty: {status: "pass", facts: 1},
			jira: {status: "pass", facts: 1},
			grafana: {status: "pass", facts: 1},
			prometheus_mimir: {status: "pass", facts: 1},
			loki: {status: "pass", facts: 1},
			tempo: {status: "pass", facts: 1}
		},
		reducers: {
			repository_dependencies: reducer(10; 10),
			terraform_iac_relationships: reducer(5; 5),
			aws_cloud_relationships: reducer(7; 5),
			oci_image_identity: reducer(4; 3),
			sbom_attachment: reducer(2; 3),
			vulnerability_matching: reducer(8; 4),
			provider_alert_reconciliation: reducer(4; 4),
			supply_chain_impact: reducer(8; 4),
			deployment_correlation: reducer(6; 2),
			observability_correlation: reducer(4; 1),
			incident_work_item_correlation: reducer(2; 1)
		},
		readback: {
			api: {status: "pass", checked: 12, failed: 0, truncated: 0},
			mcp: {status: "pass", checked: 12, failed: 0, truncated: 0},
			cli: {status: "pass", checked: 6, failed: 0, truncated: 0}
		},
		queue: {
			pending: 0,
			in_flight: 0,
			retrying: 0,
			failed: 0,
			dead_letter: 0
		},
		observability: {
			pprof_status: "reachable",
			logs_status: "captured",
			resource_snapshot_status: "captured"
		},
		privacy: {status: "pass"},
		follow_up_issues: []
	}' >"${path}"
}

expect_pass() {
	local label="$1"
	local path="$2"
	if ! validate_e2e_evidence_manifest "${path}" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${TMP_DIR}/${label}.err" >&2
		exit 1
	fi
}

expect_fail() {
	local label="$1"
	local path="$2"
	local expected="$3"
	if validate_e2e_evidence_manifest "${path}" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${TMP_DIR}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,120p' "${TMP_DIR}/${label}.err" >&2; exit 1; }
}

valid="${TMP_DIR}/valid.json"
write_valid_manifest "${valid}"
expect_pass valid "${valid}"
if [[ ! -x "${WRAPPER}" ]]; then
	printf 'missing executable wrapper at %s\n' "${WRAPPER}" >&2
	exit 1
fi
"${WRAPPER}" "${valid}" >"${TMP_DIR}/wrapper.out" 2>"${TMP_DIR}/wrapper.err" \
	|| { printf 'expected wrapper to validate valid manifest\n' >&2; sed -n '1,120p' "${TMP_DIR}/wrapper.err" >&2; exit 1; }

missing_api="${TMP_DIR}/missing-api.json"
jq 'del(.runtimes.api)' "${valid}" >"${missing_api}"
expect_fail missing_api "${missing_api}" "missing required evidence: runtimes.api"

missing_mcp_readback="${TMP_DIR}/missing-mcp-readback.json"
jq 'del(.readback.mcp)' "${valid}" >"${missing_mcp_readback}"
expect_fail missing_mcp_readback "${missing_mcp_readback}" "missing required evidence: readback.mcp"

missing_collector="${TMP_DIR}/missing-collector.json"
jq 'del(.collectors.git)' "${valid}" >"${missing_collector}"
expect_fail missing_collector "${missing_collector}" "missing required evidence: collectors.git"

missing_sbom_document="${TMP_DIR}/missing-sbom-document.json"
jq 'del(.collectors.sbom_document)' "${valid}" >"${missing_sbom_document}"
expect_fail missing_sbom_document "${missing_sbom_document}" "missing required evidence: collectors.sbom_document"

deprecated_sbom_attestation="${TMP_DIR}/deprecated-sbom-attestation.json"
jq '.collectors.sbom_attestation = {status: "pass", facts: 2}' "${valid}" >"${deprecated_sbom_attestation}"
expect_fail deprecated_sbom_attestation "${deprecated_sbom_attestation}" "collectors.sbom_attestation is ambiguous"

sbom_document_without_facts="${TMP_DIR}/sbom-document-without-facts.json"
jq '.collectors.sbom_document = {status: "pass", source_facts: 0}' "${valid}" >"${sbom_document_without_facts}"
expect_fail sbom_document_without_facts "${sbom_document_without_facts}" "collectors.sbom_document pass requires source_facts > 0 or facts > 0"

scanner_worker_without_evidence="${TMP_DIR}/scanner-worker-without-evidence.json"
jq '.collectors.scanner_worker = {status: "pass", facts: 0, warnings: 0}' "${valid}" >"${scanner_worker_without_evidence}"
expect_fail scanner_worker_without_evidence "${scanner_worker_without_evidence}" "collectors.scanner_worker pass requires facts > 0, source_facts > 0, or warnings > 0"

missing_reducer="${TMP_DIR}/missing-reducer.json"
jq 'del(.reducers.repository_dependencies)' "${valid}" >"${missing_reducer}"
expect_fail missing_reducer "${missing_reducer}" "missing required evidence: reducers.repository_dependencies"

reducer_missing_source="${TMP_DIR}/reducer-missing-source.json"
jq 'del(.reducers.vulnerability_matching.source_facts)' "${valid}" >"${reducer_missing_source}"
expect_fail reducer_missing_source "${reducer_missing_source}" "reducers.vulnerability_matching must include source_facts > 0, reducer_facts > 0, and API/MCP readback pass"

reducer_missing_readback="${TMP_DIR}/reducer-missing-readback.json"
jq 'del(.reducers.vulnerability_matching.readback.mcp)' "${valid}" >"${reducer_missing_readback}"
expect_fail reducer_missing_readback "${reducer_missing_readback}" "reducers.vulnerability_matching must include source_facts > 0, reducer_facts > 0, and API/MCP readback pass"

missing_corpus="${TMP_DIR}/missing-corpus.json"
jq 'del(.corpus.coverage.ecosystems.npm)' "${valid}" >"${missing_corpus}"
expect_fail missing_corpus "${missing_corpus}" "missing required evidence: corpus.coverage.ecosystems.npm"

missing_relationship_evidence="${TMP_DIR}/missing-relationship-evidence.json"
jq 'del(.corpus.coverage.evidence_families.relationship_evidence)' "${valid}" >"${missing_relationship_evidence}"
expect_fail missing_relationship_evidence "${missing_relationship_evidence}" "missing required evidence: corpus.coverage.evidence_families.relationship_evidence"

zero_ecosystem_coverage="${TMP_DIR}/zero-ecosystem-coverage.json"
jq '.corpus.coverage.ecosystems.gomod = {status: "pass", count: 0}' "${valid}" >"${zero_ecosystem_coverage}"
expect_fail zero_ecosystem_coverage "${zero_ecosystem_coverage}" "corpus.coverage.ecosystems.gomod pass requires count > 0"

zero_evidence_family_coverage="${TMP_DIR}/zero-evidence-family-coverage.json"
jq '.corpus.coverage.evidence_families.incident = {status: "pass", count: 0}' "${valid}" >"${zero_evidence_family_coverage}"
expect_fail zero_evidence_family_coverage "${zero_evidence_family_coverage}" "corpus.coverage.evidence_families.incident pass requires count > 0"

unsupported_ecosystem_coverage="${TMP_DIR}/unsupported-ecosystem-coverage.json"
jq '.status = "partial" | .corpus.coverage.ecosystems.gomod = {status: "unsupported", reason: "representative corpus does not yet include Go module coverage", issue_refs: ["#1249"]}' "${valid}" >"${unsupported_ecosystem_coverage}"
expect_pass unsupported_ecosystem_coverage "${unsupported_ecosystem_coverage}"

missing_pprof="${TMP_DIR}/missing-pprof.json"
jq '.observability.pprof_status = "missing"' "${valid}" >"${missing_pprof}"
expect_fail missing_pprof "${missing_pprof}" "observability.pprof_status must be reachable"

retrying_queue="${TMP_DIR}/retrying-queue.json"
jq '.queue.retrying = 1' "${valid}" >"${retrying_queue}"
expect_fail retrying_queue "${retrying_queue}" "queue.retrying must be 0"

private_key="${TMP_DIR}/private-key.json"
jq '.repository = "private-org/private-repo"' "${valid}" >"${private_key}"
expect_fail private_key "${private_key}" "looks like private data"

private_value="${TMP_DIR}/private-value.json"
jq '.follow_up_issues = ["https://github.com/private-org/private-repo/issues/1"]' "${valid}" >"${private_value}"
expect_fail private_value "${private_value}" "looks like private data"

unsupported_partial="${TMP_DIR}/unsupported-partial.json"
jq '.status = "partial" | .collectors.confluence = {status: "unsupported", reason: "not configured in this representative corpus", issue_refs: ["#1230"]}' "${valid}" >"${unsupported_partial}"
expect_pass unsupported_partial "${unsupported_partial}"

unsupported_pass="${TMP_DIR}/unsupported-pass.json"
jq '.collectors.confluence = {status: "unsupported", reason: "not configured in this representative corpus", issue_refs: ["#1230"]}' "${valid}" >"${unsupported_pass}"
expect_fail unsupported_pass "${unsupported_pass}" "top-level status pass cannot include unsupported evidence"

unsupported_without_reason="${TMP_DIR}/unsupported-without-reason.json"
jq '.status = "partial" | .collectors.confluence = {status: "unsupported"}' "${valid}" >"${unsupported_without_reason}"
expect_fail unsupported_without_reason "${unsupported_without_reason}" "collectors.confluence classified as unsupported without reason"

printf 'e2e evidence manifest tests passed\n'
