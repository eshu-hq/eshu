#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-okta-oidc-live-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

steps=(
	issuer_metadata
	jwks_validation
	client_redirect_config
	authorization_code_login
	state_nonce
	group_mapping
	tenant_workspace_selection
	bounded_session_refresh
	group_removal_denial
	user_revocation_denial
	expired_mapping_denial
	tombstoned_mapping_denial
	revoked_role_target_denial
	provider_unavailable_fail_closed
	duplicate_refresh_idempotent
	tenant_workspace_boundary
	no_raw_provider_persistence
)

write_good_manifest() {
	local path="$1"
	jq -n --arg generated_at "2026-06-22T21:15:00Z" '
		{
			schema_version: 1,
			proof_id: "okta-oidc-live-proof-20260622",
			generated_at: $generated_at,
			provider: {
				provider_kind: "external_oidc",
				issuer_metadata_source_class: "operator_private_url",
				client_config_source_class: "operator_private_secret",
				login_path: "/api/v0/auth/oidc/login",
				callback_path: "/api/v0/auth/oidc/callback",
				subject_claim_class: "sub",
				group_claim_class: "groups",
				role_mapping_revision: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			revocation: {
				external_group_refresh_window_seconds: 900,
				external_group_refresh_window_source: "ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW"
			},
			proof_steps: [
				{step: "issuer_metadata", status: "pass", evidence_count: 2},
				{step: "jwks_validation", status: "pass", evidence_count: 2},
				{step: "client_redirect_config", status: "pass", evidence_count: 2},
				{step: "authorization_code_login", status: "pass", evidence_count: 3},
				{step: "state_nonce", status: "pass", evidence_count: 2},
				{step: "group_mapping", status: "pass", evidence_count: 2},
				{step: "tenant_workspace_selection", status: "pass", evidence_count: 2},
				{step: "bounded_session_refresh", status: "pass", evidence_count: 2},
				{step: "group_removal_denial", status: "pass", evidence_count: 2},
				{step: "user_revocation_denial", status: "pass", evidence_count: 2},
				{step: "expired_mapping_denial", status: "pass", evidence_count: 1},
				{step: "tombstoned_mapping_denial", status: "pass", evidence_count: 1},
				{step: "revoked_role_target_denial", status: "pass", evidence_count: 1},
				{step: "provider_unavailable_fail_closed", status: "pass", evidence_count: 1},
				{step: "duplicate_refresh_idempotent", status: "pass", evidence_count: 1},
				{step: "tenant_workspace_boundary", status: "pass", evidence_count: 2},
				{step: "no_raw_provider_persistence", status: "pass", evidence_count: 2}
			],
			public_summary: {
				login_count: 1,
				denied_count: 6,
				refresh_attempt_count: 3,
				revoked_session_count: 2,
				mapped_role_names: ["owner", "tenant_admin"],
				decision_families: ["allowed", "denied", "fail_closed"]
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
	jq -e --argjson expected_steps "${#steps[@]}" '
		.status == "pass" and
		.proof_id == "okta-oidc-live-proof-20260622" and
		.provider.provider_kind == "external_oidc" and
		.provider.issuer_metadata_source_class == "operator_private_url" and
		.proof_step_count == $expected_steps and
		.total_evidence_count == 30 and
		.revocation.external_group_refresh_window_seconds == 900 and
		.public_summary.login_count == 1 and
		.public_summary.denied_count == 6 and
		.public_summary.refresh_attempt_count == 3 and
		.public_summary.revoked_session_count == 2
	' "${out_json}" >/dev/null || {
		printf 'expected public-safe summary fields for %s\n' "${label}" >&2
		jq . "${out_json}" >&2
		exit 1
	}
	rg --fixed-strings --quiet 'Okta OIDC live proof' "${out_md}"
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

expect_fail_without_leak() {
	local label="$1"
	local manifest="$2"
	local expected="$3"
	local forbidden="$4"
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
	if rg --fixed-strings --quiet -- "${forbidden}" "${tmp_dir}/${label}.err"; then
		printf 'expected %s failure not to leak %s\n' "${label}" "${forbidden}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
}

good_manifest="${tmp_dir}/good.json"
write_good_manifest "${good_manifest}"
expect_pass good "${good_manifest}"

missing_group_removal="${tmp_dir}/missing-group-removal.json"
jq 'del(.proof_steps[] | select(.step == "group_removal_denial"))' "${good_manifest}" >"${missing_group_removal}"
expect_fail missing_group_removal "${missing_group_removal}" "missing required proof steps: group_removal_denial"

wrong_provider="${tmp_dir}/wrong-provider.json"
jq '.provider.provider_kind = "external_saml"' "${good_manifest}" >"${wrong_provider}"
expect_fail wrong_provider "${wrong_provider}" "provider.provider_kind must be external_oidc"

raw_url="${tmp_dir}/raw-url.json"
jq '.provider.issuer_url = "https://tenant.example.com/oauth2/default"' "${good_manifest}" >"${raw_url}"
expect_fail raw_url "${raw_url}" "private-shaped value leaked in provider summary"

raw_token="${tmp_dir}/raw-token.json"
jq '.public_summary.note = "id_token: eyJraWQiOiJ0ZXN0In0.eyJzdWIiOiJ1c2VyIn0.sig"' "${good_manifest}" >"${raw_token}"
expect_fail raw_token "${raw_token}" "private-shaped value leaked in public summary"

raw_email="${tmp_dir}/raw-email.json"
jq '.public_summary.note = "signed in user@example.com"' "${good_manifest}" >"${raw_email}"
expect_fail raw_email "${raw_email}" "private-shaped value leaked in public summary"

bad_role="${tmp_dir}/bad-role.json"
jq '.public_summary.mapped_role_names = ["tenant-admin@example.com"]' "${good_manifest}" >"${bad_role}"
expect_fail bad_role "${bad_role}" "mapped role names must be public-safe"

bad_path="${tmp_dir}/bad-path.json"
jq '.provider.callback_path = "/api/v0/auth/oidc/callback/../../secret"' "${good_manifest}" >"${bad_path}"
expect_fail bad_path "${bad_path}" "provider.callback_path must be /api/v0/auth/oidc/callback"

zero_evidence="${tmp_dir}/zero-evidence.json"
jq '(.proof_steps[] | select(.step == "authorization_code_login") | .evidence_count) = 0' "${good_manifest}" >"${zero_evidence}"
expect_fail zero_evidence "${zero_evidence}" "evidence_count must be positive for proof step authorization_code_login"

zero_refresh="${tmp_dir}/zero-refresh.json"
jq '.public_summary.refresh_attempt_count = 0' "${good_manifest}" >"${zero_refresh}"
expect_fail zero_refresh "${zero_refresh}" "public_summary.refresh_attempt_count must be positive"

zero_denied="${tmp_dir}/zero-denied.json"
jq '.public_summary.denied_count = 0' "${good_manifest}" >"${zero_denied}"
expect_fail zero_denied "${zero_denied}" "public_summary.denied_count must be positive"

failed_step="${tmp_dir}/failed-step.json"
jq '(.proof_steps[] | select(.step == "group_removal_denial") | .status) = "fail"' "${good_manifest}" >"${failed_step}"
expect_fail failed_step "${failed_step}" "proof step group_removal_denial must pass"

raw_unknown_step="${tmp_dir}/raw-unknown-step.json"
jq '(.proof_steps[] | select(.step == "group_removal_denial") | .step) = "https://tenant.example.com/oauth2/default"' "${good_manifest}" >"${raw_unknown_step}"
expect_fail_without_leak raw_unknown_step "${raw_unknown_step}" "private-shaped value leaked in proof step entry" "tenant.example.com"

unknown_step="${tmp_dir}/unknown-step.json"
jq '(.proof_steps[] | select(.step == "group_removal_denial") | .step) = "unreviewed_step"' "${good_manifest}" >"${unknown_step}"
expect_fail unknown_step "${unknown_step}" "unknown proof step"

tenant_slug_proof_id="${tmp_dir}/tenant-slug-proof-id.json"
jq '.proof_id = "tenant-prod-app"' "${good_manifest}" >"${tenant_slug_proof_id}"
expect_fail_without_leak tenant_slug_proof_id "${tenant_slug_proof_id}" "proof_id must be okta-oidc-live-proof-YYYYMMDD or okta-oidc-live-proof-YYYYMMDD-sha256digest" "tenant-prod-app"

bad_decision="${tmp_dir}/bad-decision.json"
jq '.public_summary.decision_families = ["allowed", "permitted"]' "${good_manifest}" >"${bad_decision}"
expect_fail bad_decision "${bad_decision}" "decision families must be allowed, denied, or fail_closed"

printf 'okta oidc live proof tests passed\n'
