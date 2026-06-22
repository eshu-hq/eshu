#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-auth-audit-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

event_types=(
	api_mcp_authentication
	identity_authentication
	mfa_lifecycle
	session_lifecycle
	token_lifecycle
	idp_config_change
	role_grant_change
	read_authorization
	tenant_switch
	sensitive_data_access
	ask_search_run
	export
	bootstrap
	break_glass
	audit_read
)

write_good_manifest() {
	local path="$1"
	jq -n --arg generated_at "2026-06-10T00:00:00Z" '
		{
			schema_version: 1,
			proof_id: "auth-audit-proof-test",
			generated_at: $generated_at,
			audit_events: [
				{event_type: "api_mcp_authentication", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "identity_authentication", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "mfa_lifecycle", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "session_lifecycle", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "token_lifecycle", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "idp_config_change", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "role_grant_change", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "read_authorization", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "tenant_switch", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "sensitive_data_access", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "ask_search_run", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "export", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "bootstrap", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "break_glass", record_count: 2, decision_counts: {allowed: 1, denied: 1}},
				{event_type: "audit_read", record_count: 2, decision_counts: {allowed: 1, denied: 1}}
			],
			ordinary_reads: {
				structured_telemetry_only: true
			},
			revocation: {
				eshu_owned_sessions: "immediate",
				eshu_owned_tokens: "immediate",
				external_group_refresh_window_seconds: 900,
				external_group_refresh_window_source: "security-review-v1"
			}
		}
	' >"${path}"
}

expect_pass() {
	local label="$1"
	local manifest="$2"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if ! "${verifier}" --input "${manifest}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	jq -e --argjson expected_count "${#event_types[@]}" '
		.status == "pass" and
		.proof_id == "auth-audit-proof-test" and
		.audit_event_type_count == $expected_count and
		.total_audit_records == 30 and
		.ordinary_reads.structured_telemetry_only == true and
		.revocation.eshu_owned_sessions == "immediate" and
		.revocation.eshu_owned_tokens == "immediate"
	' "${out_json}" >/dev/null || {
		printf 'expected public-safe summary fields for %s\n' "${label}" >&2
		jq . "${out_json}" >&2
		exit 1
	}
	rg --fixed-strings --quiet 'Hosted auth audit and revocation proof' "${out_md}"
}

expect_fail() {
	local label="$1"
	local manifest="$2"
	local expected="$3"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if "${verifier}" --input "${manifest}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" || {
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	}
}

good_manifest="${tmp_dir}/good.json"
write_good_manifest "${good_manifest}"
expect_pass good "${good_manifest}"

missing_break_glass="${tmp_dir}/missing-break-glass.json"
jq 'del(.audit_events[] | select(.event_type == "break_glass"))' "${good_manifest}" >"${missing_break_glass}"
expect_fail missing_break_glass "${missing_break_glass}" "missing required audit event types: break_glass"

token_delay="${tmp_dir}/token-delay.json"
jq '.revocation.eshu_owned_tokens = "bounded"' "${good_manifest}" >"${token_delay}"
expect_fail token_delay "${token_delay}" "revocation.eshu_owned_tokens must be immediate"

bad_external_window="${tmp_dir}/bad-external-window.json"
jq '.revocation.external_group_refresh_window_seconds = 0' "${good_manifest}" >"${bad_external_window}"
expect_fail bad_external_window "${bad_external_window}" "revocation.external_group_refresh_window_seconds must be between 1 and 86400"

ordinary_read_audit="${tmp_dir}/ordinary-read-audit.json"
jq '.ordinary_reads.structured_telemetry_only = false' "${good_manifest}" >"${ordinary_read_audit}"
expect_fail ordinary_read_audit "${ordinary_read_audit}" "ordinary_reads.structured_telemetry_only must be true"

inconsistent_counts="${tmp_dir}/inconsistent-counts.json"
jq '(.audit_events[] | select(.event_type == "api_mcp_authentication") | .record_count) = 0 | (.audit_events[] | select(.event_type == "api_mcp_authentication") | .decision_counts.allowed) = 999' "${good_manifest}" >"${inconsistent_counts}"
expect_fail inconsistent_counts "${inconsistent_counts}" "decision counts must sum to record_count for event api_mcp_authentication"

fractional_counts="${tmp_dir}/fractional-counts.json"
jq '(.audit_events[] | select(.event_type == "api_mcp_authentication") | .decision_counts.allowed) = 1.5' "${good_manifest}" >"${fractional_counts}"
expect_fail fractional_counts "${fractional_counts}" "decision count must be a non-negative integer for event api_mcp_authentication"

private_audit_value="${tmp_dir}/private-audit-value.json"
jq '(.audit_events[] | select(.event_type == "audit_read") | .note) = "reviewed user@example.com"' "${good_manifest}" >"${private_audit_value}"
expect_fail private_audit_value "${private_audit_value}" "private-shaped value leaked in audit event summary audit_read"

printf 'hosted auth audit proof tests passed\n'
