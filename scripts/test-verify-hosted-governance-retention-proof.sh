#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-retention-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

write_safe_proof() {
	local output="$1"
	jq -n '
		{
			schema_version: 1,
			proof_id: "retention-proof-test",
			generated_at: "2026-06-10T00:00:00Z",
			scenarios: [
				{
					name: "configured_retention",
					status: "pass",
					retention_mode: "configured",
					deletion_state: "not_requested",
					reason_codes: ["retention_configured"],
					api_status: "pass",
					mcp_status: "pass",
					agreement_status: "pass",
					checked_count: 2,
					mismatch_count: 0,
					data_class_counts: {facts: 12, content: 0, audit_events: 4}
				},
				{
					name: "local_not_configured",
					status: "pass",
					retention_mode: "not_configured",
					deletion_state: "not_requested",
					reason_codes: ["local_no_policy"],
					api_status: "pass",
					mcp_status: "pass",
					agreement_status: "pass",
					checked_count: 2,
					mismatch_count: 0,
					data_class_counts: {facts: 0, content: 0, audit_events: 0}
				},
				{
					name: "deletion_pending",
					status: "pass",
					retention_mode: "metadata_only",
					deletion_state: "pending",
					reason_codes: ["source_deleted"],
					api_status: "pass",
					mcp_status: "pass",
					agreement_status: "pass",
					checked_count: 2,
					mismatch_count: 0,
					data_class_counts: {facts: 8, content: 0, audit_events: 2}
				},
				{
					name: "deletion_complete",
					status: "pass",
					retention_mode: "metadata_only",
					deletion_state: "complete",
					reason_codes: ["retention_expired"],
					api_status: "pass",
					mcp_status: "pass",
					agreement_status: "pass",
					checked_count: 2,
					mismatch_count: 0,
					data_class_counts: {facts: 3, content: 0, audit_events: 1}
				},
				{
					name: "graph_rebuild_required",
					status: "pass",
					retention_mode: "metadata_only",
					deletion_state: "repairing_graph",
					reason_codes: ["graph_rebuild_required"],
					api_status: "pass",
					mcp_status: "pass",
					agreement_status: "pass",
					checked_count: 2,
					mismatch_count: 0,
					data_class_counts: {facts: 5, content: 0, audit_events: 1}
				}
			],
			security: {
				raw_policy_exported: false,
				raw_payload_exported: false,
				secret_scan: "passed",
				private_locator_scan: "passed",
				public_artifact_review: "passed"
			}
		}
	' >"${output}"
}

expect_pass() {
	local label="$1"
	local input="$2"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if ! "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	jq -e '
		.status == "pass" and
		.proof_id == "retention-proof-test" and
		.scenario_count == 5 and
		.parity_checked_count == 10 and
		.parity_mismatch_count == 0 and
		(.scenarios | length == 5)
	' "${out_json}" >/dev/null
	rg --fixed-strings --quiet 'Hosted governance retention proof' "${out_md}"
	rg --fixed-strings --quiet 'Raw policies, tenants, repositories' "${out_md}"
}

expect_fail() {
	local label="$1"
	local input="$2"
	local expected="$3"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" || {
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	}
}

safe_input="${tmp_dir}/safe.json"
write_safe_proof "${safe_input}"
expect_pass safe "${safe_input}"

missing_input="${tmp_dir}/missing.json"
jq 'del(.scenarios[] | select(.name == "graph_rebuild_required"))' "${safe_input}" >"${missing_input}"
expect_fail missing "${missing_input}" "missing required retention scenarios: graph_rebuild_required"

parity_input="${tmp_dir}/parity.json"
jq '(.scenarios[] | select(.name == "deletion_pending") | .mismatch_count) = 1' "${safe_input}" >"${parity_input}"
expect_fail parity "${parity_input}" "retention scenario parity must pass with zero mismatches"

private_input="${tmp_dir}/private.json"
jq '(.scenarios[] | select(.name == "configured_retention") | .note) = "https://internal.example.invalid/path"' "${safe_input}" >"${private_input}"
expect_fail private "${private_input}" "input looks like private data"

raw_input="${tmp_dir}/raw.json"
jq '.security.raw_policy_exported = true' "${safe_input}" >"${raw_input}"
expect_fail raw "${raw_input}" "security review did not pass"

printf 'hosted governance retention proof tests passed\n'
