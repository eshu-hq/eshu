#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-proof-artifact.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

write_valid_proof() {
	local path="$1"
	jq -n --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
		{
			schema_version: 1,
			proof_id: "hosted-governance-proof-test",
			generated_at: $generated_at,
			mode: "remote_compose",
			auth: {
				unauthenticated_status: "pass",
				permission_denied_status: "pass",
				allowed_in_scope_status: "pass"
			},
			policy: {
				policy_disabled_status: "pass",
				policy_enforcing_status: "pass",
				denied_egress_status: "pass",
				reason_classes: ["policy_disabled", "egress_denied"]
			},
			parity: {
				api_status: "pass",
				mcp_status: "pass",
				agreement_status: "pass",
				checked_count: 7,
				mismatch_count: 0
			},
			redaction: {
				status: "pass",
				canary_count: 6,
				forbidden_surface_count: 0
			},
			audit: {
				status: "pass",
				aggregate_event_count: 4,
				denied_decision_count: 1,
				raw_event_body_exported: false
			},
			proof_gates: {
				local_governance_status: "pass",
				remote_compose_render_status: "pass",
				remote_compose_runtime_status: "pass",
				helm_render_status: "pass"
			},
			security: {
				secret_scan: "passed",
				private_locator_scan: "passed",
				public_artifact_review: "passed"
			}
		}
	' >"${path}"
}

expect_pass() {
	local label="$1"
	local input="$2"
	local out_json="${tmp_dir}/${label}-summary.json"
	local out_md="${tmp_dir}/${label}-summary.md"
	if ! "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	jq -e '
		.status == "pass" and
		.proof_id == "hosted-governance-proof-test" and
		.parity.checked_count == 7 and
		.parity.mismatch_count == 0 and
		.redaction.canary_count == 6 and
		.audit.denied_decision_count == 1 and
		.proof_gates.remote_compose_runtime_status == "pass"
	' "${out_json}" >/dev/null || {
		printf 'expected %s output to preserve public-safe proof fields\n' "${label}" >&2
		jq . "${out_json}" >&2
		exit 1
	}
	rg --fixed-strings --quiet 'Hosted governance proof' "${out_md}" \
		|| { printf 'expected markdown summary for %s\n' "${label}" >&2; exit 1; }
}

expect_fail() {
	local label="$1"
	local input="$2"
	local expected="$3"
	local out_json="${tmp_dir}/${label}-summary.json"
	local out_md="${tmp_dir}/${label}-summary.md"
	if "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,120p' "${tmp_dir}/${label}.err" >&2; exit 1; }
}

valid="${tmp_dir}/valid.json"
write_valid_proof "${valid}"
expect_pass valid "${valid}"

missing_auth="${tmp_dir}/missing-auth.json"
jq 'del(.auth.permission_denied_status)' "${valid}" >"${missing_auth}"
expect_fail missing_auth "${missing_auth}" "auth proof must pass unauthenticated, permission_denied, and allowed in-scope checks"

policy_gap="${tmp_dir}/policy-gap.json"
jq '.policy.denied_egress_status = "missing"' "${valid}" >"${policy_gap}"
expect_fail policy_gap "${policy_gap}" "policy proof must pass disabled, enforcing, and denied-egress checks"

empty_reason_classes="${tmp_dir}/empty-reason-classes.json"
jq '.policy.reason_classes = []' "${valid}" >"${empty_reason_classes}"
expect_fail empty_reason_classes "${empty_reason_classes}" "policy reason classes must be public-safe low-cardinality strings"

parity_mismatch="${tmp_dir}/parity-mismatch.json"
jq '.parity.mismatch_count = 1' "${valid}" >"${parity_mismatch}"
expect_fail parity_mismatch "${parity_mismatch}" "API/MCP parity proof must pass with zero mismatches"

redaction_leak="${tmp_dir}/redaction-leak.json"
jq '.redaction.forbidden_surface_count = 1' "${valid}" >"${redaction_leak}"
expect_fail redaction_leak "${redaction_leak}" "redaction proof must pass with canaries and zero forbidden surfaces"

audit_leak="${tmp_dir}/audit-leak.json"
jq '.audit.raw_event_body_exported = true' "${valid}" >"${audit_leak}"
expect_fail audit_leak "${audit_leak}" "audit proof must use aggregate events only"

missing_runtime="${tmp_dir}/missing-runtime.json"
jq '.proof_gates.remote_compose_runtime_status = "missing"' "${valid}" >"${missing_runtime}"
expect_fail missing_runtime "${missing_runtime}" "proof gates must pass local, remote render, remote runtime, and Helm render checks"

unsafe_key="${tmp_dir}/unsafe-key.json"
jq '.auth.token = "redacted"' "${valid}" >"${unsafe_key}"
expect_fail unsafe_key "${unsafe_key}" "input looks like private data"

unsafe_value="${tmp_dir}/unsafe-value.json"
jq '.proof_id = "https://example.test/proof"' "${valid}" >"${unsafe_value}"
expect_fail unsafe_value "${unsafe_value}" "input looks like private data"

extra_public_field="${tmp_dir}/extra-public-field.json"
jq '.auth.extra_safe_note = "ignored_public_note"' "${valid}" >"${extra_public_field}"
expect_pass extra_public_field "${extra_public_field}"
extra_out_json="${tmp_dir}/extra_public_field-summary.json"
if jq -e '.auth.extra_safe_note?' "${extra_out_json}" >/dev/null; then
	printf 'expected summary output to omit unknown auth fields\n' >&2
	jq . "${extra_out_json}" >&2
	exit 1
fi

printf 'hosted governance proof artifact verifier tests passed\n'
