#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
GENERATOR="${REPO_ROOT}/scripts/e2e_corpus_coverage.sh"
MANIFEST_VALIDATOR="${REPO_ROOT}/scripts/verify_e2e_evidence_manifest.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

expect_pass() {
	local label="$1"
	shift
	if ! "$@" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${TMP_DIR}/${label}.err" >&2
		exit 1
	fi
}

expect_fail_with() {
	local label="$1"
	local expected="$2"
	shift 2
	if "$@" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${expected}" "${TMP_DIR}/${label}.err"; then
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${TMP_DIR}/${label}.err" >&2
		exit 1
	fi
}

write_current_discovery() {
	local path="$1"
	jq -n '{
		schema_version: 1,
		mode: "representative",
		repository_count: 29,
		ecosystems: {
			npm: 87,
			pypi: 4,
			composer: 1
		},
		evidence_families: {
			terraform_iac: 89,
			kubernetes_iac: 397,
			image_sbom: 17,
			deployment: 38,
			vulnerability: 88,
			observability: 20,
			work_item: 6
		}
	}' >"${path}"
}

write_complete_discovery() {
	local path="$1"
	jq -n '{
		schema_version: 1,
		mode: "representative",
		repository_count: 24,
		ecosystems: {
			npm: 8,
			gomod: 3,
			pypi: 4,
			maven: 2,
			composer: 1,
			rubygems: 1,
			cargo: 1,
			nuget: 1
		},
		evidence_families: {
			terraform_iac: 9,
			kubernetes_iac: 12,
			image_sbom: 3,
			deployment: 5,
			vulnerability: 8,
			observability: 2,
			incident: 1,
			work_item: 2
		}
	}' >"${path}"
}

write_manifest_with_coverage() {
	local coverage="$1"
	local output="$2"
	jq -n --slurpfile coverage "${coverage}" '
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
		status: "fail",
		run: {
			id: "e2e-corpus-coverage-test",
			kind: "clean",
			commit: "1234567890abcdef",
			image_tag_candidate: "v0.0.3-pre-release-test",
			backend: {kind: "nornicdb"}
		},
		corpus: {
			mode: "representative",
			repository_count: ($coverage[0].repository_count // 0),
			coverage: $coverage[0]
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
	}' >"${output}"
}

current_input="${TMP_DIR}/current-discovery.json"
current_output="${TMP_DIR}/current-coverage.json"
write_current_discovery "${current_input}"
expect_pass current_coverage "${GENERATOR}" --input "${current_input}" --output "${current_output}"
jq -e '
	.schema_version == 1 and
	.mode == "representative" and
	.repository_count == 29 and
	.ecosystems.npm == {status: "pass", count: 87} and
	.ecosystems.pypi == {status: "pass", count: 4} and
	.ecosystems.composer == {status: "pass", count: 1} and
	.ecosystems.gomod.status == "fail" and
	.ecosystems.gomod.count == 0 and
	.ecosystems.gomod.reason == "not observed in current private representative corpus" and
	(.ecosystems.gomod.issue_refs == ["#1249"]) and
	.ecosystems.maven.status == "fail" and
	.ecosystems.rubygems.status == "fail" and
	.ecosystems.cargo.status == "fail" and
	.ecosystems.nuget.status == "fail" and
	.evidence_families.terraform_iac == {status: "pass", count: 89} and
	.evidence_families.kubernetes_iac == {status: "pass", count: 397} and
	.evidence_families.image_sbom == {status: "pass", count: 17} and
	.evidence_families.deployment == {status: "pass", count: 38} and
	.evidence_families.vulnerability == {status: "pass", count: 88} and
	.evidence_families.observability == {status: "pass", count: 20} and
	.evidence_families.work_item == {status: "pass", count: 6} and
	.evidence_families.incident.status == "fail" and
	.evidence_families.incident.reason == "not observed in current private representative corpus"
' "${current_output}" >/dev/null || {
	printf 'current aggregate coverage was not rendered as expected\n' >&2
	jq . "${current_output}" >&2
	exit 1
}

current_manifest="${TMP_DIR}/current-manifest.json"
write_manifest_with_coverage "${current_output}" "${current_manifest}"
expect_pass current_manifest "${MANIFEST_VALIDATOR}" "${current_manifest}"

complete_input="${TMP_DIR}/complete-discovery.json"
complete_output="${TMP_DIR}/complete-coverage.json"
write_complete_discovery "${complete_input}"
expect_pass complete_coverage "${GENERATOR}" --input "${complete_input}" --output "${complete_output}"
jq -e '
	([.ecosystems[], .evidence_families[]] | all(.status == "pass")) and
	([.ecosystems[], .evidence_families[]] | all(.count > 0))
' "${complete_output}" >/dev/null || {
	printf 'complete coverage did not satisfy all required slots\n' >&2
	jq . "${complete_output}" >&2
	exit 1
}

small_input="${TMP_DIR}/small-discovery.json"
jq '.repository_count = 19' "${complete_input}" >"${small_input}"
expect_fail_with small_representative "representative corpus requires repository_count between 20 and 50" \
	"${GENERATOR}" --input "${small_input}" --output "${TMP_DIR}/small-coverage.json"

private_input="${TMP_DIR}/private-discovery.json"
jq '.repository_name = "private-owner/private-repo"' "${complete_input}" >"${private_input}"
expect_fail_with private_discovery "input looks like private data" \
	"${GENERATOR}" --input "${private_input}" --output "${TMP_DIR}/private-coverage.json"

printf 'e2e corpus coverage tests passed\n'
