#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-okta-saml-live-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

steps=(
	sp_metadata
	saml_login
	group_mapping
	tenant_workspace_selection
	revocation_refresh
	denied_access
	missing_group_claims
	replay
	clock_skew
	disabled_provider
	disabled_user
	disabled_membership
	disabled_grant
	metadata_certificate_failure
)

write_good_manifest() {
	local path="$1"
	jq -n --arg generated_at "2026-06-22T20:00:00Z" '
		{
			schema_version: 1,
			proof_id: "okta-saml-live-proof-test",
			generated_at: $generated_at,
			provider: {
				provider_kind: "external_saml",
				metadata_source_class: "operator_private_secret",
				sp_metadata_path: "/api/v0/auth/saml/providers/{provider_id}/metadata",
				acs_path: "/api/v0/auth/saml/providers/{provider_id}/acs",
				name_id_policy_class: "persistent",
				group_attribute_class: "groups",
				role_mapping_revision: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			revocation: {
				external_group_refresh_window_seconds: 900,
				external_group_refresh_window_source: "security-review-v1"
			},
			proof_steps: [
				{step: "sp_metadata", status: "pass", evidence_count: 2},
				{step: "saml_login", status: "pass", evidence_count: 3},
				{step: "group_mapping", status: "pass", evidence_count: 2},
				{step: "tenant_workspace_selection", status: "pass", evidence_count: 2},
				{step: "revocation_refresh", status: "pass", evidence_count: 2},
				{step: "denied_access", status: "pass", evidence_count: 2},
				{step: "missing_group_claims", status: "pass", evidence_count: 1},
				{step: "replay", status: "pass", evidence_count: 1},
				{step: "clock_skew", status: "pass", evidence_count: 1},
				{step: "disabled_provider", status: "pass", evidence_count: 1},
				{step: "disabled_user", status: "pass", evidence_count: 1},
				{step: "disabled_membership", status: "pass", evidence_count: 1},
				{step: "disabled_grant", status: "pass", evidence_count: 1},
				{step: "metadata_certificate_failure", status: "pass", evidence_count: 1}
			],
			public_summary: {
				login_count: 1,
				denied_count: 7,
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
		.proof_id == "okta-saml-live-proof-test" and
		.provider.provider_kind == "external_saml" and
		.provider.metadata_source_class == "operator_private_secret" and
		.proof_step_count == $expected_steps and
		.total_evidence_count == 21 and
		.revocation.external_group_refresh_window_seconds == 900 and
		.public_summary.login_count == 1 and
		.public_summary.denied_count == 7
	' "${out_json}" >/dev/null || {
		printf 'expected public-safe summary fields for %s\n' "${label}" >&2
		jq . "${out_json}" >&2
		exit 1
	}
	rg --fixed-strings --quiet 'Okta SAML live proof' "${out_md}"
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

missing_replay="${tmp_dir}/missing-replay.json"
jq 'del(.proof_steps[] | select(.step == "replay"))' "${good_manifest}" >"${missing_replay}"
expect_fail missing_replay "${missing_replay}" "missing required proof steps: replay"

wrong_provider="${tmp_dir}/wrong-provider.json"
jq '.provider.provider_kind = "external_oidc"' "${good_manifest}" >"${wrong_provider}"
expect_fail wrong_provider "${wrong_provider}" "provider.provider_kind must be external_saml"

raw_url="${tmp_dir}/raw-url.json"
jq '.provider.idp_metadata_url = "https://tenant.example.com/app/metadata"' "${good_manifest}" >"${raw_url}"
expect_fail raw_url "${raw_url}" "private-shaped value leaked in provider summary"

raw_certificate="${tmp_dir}/raw-certificate.json"
jq '.provider.signing_certificate = "-----BEGIN CERTIFICATE-----"' "${good_manifest}" >"${raw_certificate}"
expect_fail raw_certificate "${raw_certificate}" "private-shaped value leaked in provider summary"

raw_email="${tmp_dir}/raw-email.json"
jq '.public_summary.note = "signed in user@example.com"' "${good_manifest}" >"${raw_email}"
expect_fail raw_email "${raw_email}" "private-shaped value leaked in public summary"

bad_role="${tmp_dir}/bad-role.json"
jq '.public_summary.mapped_role_names = ["tenant-admin@example.com"]' "${good_manifest}" >"${bad_role}"
expect_fail bad_role "${bad_role}" "mapped role names must be public-safe"

bad_path="${tmp_dir}/bad-path.json"
jq '.provider.sp_metadata_path = "/api/v0/auth/saml/providers/../../metadata"' "${good_manifest}" >"${bad_path}"
expect_fail bad_path "${bad_path}" "provider.sp_metadata_path must be a public-safe SAML metadata path"

zero_evidence="${tmp_dir}/zero-evidence.json"
jq '(.proof_steps[] | select(.step == "saml_login") | .evidence_count) = 0' "${good_manifest}" >"${zero_evidence}"
expect_fail zero_evidence "${zero_evidence}" "evidence_count must be positive for proof step saml_login"

zero_login="${tmp_dir}/zero-login.json"
jq '.public_summary.login_count = 0' "${good_manifest}" >"${zero_login}"
expect_fail zero_login "${zero_login}" "public_summary.login_count must be positive"

zero_denied="${tmp_dir}/zero-denied.json"
jq '.public_summary.denied_count = 0' "${good_manifest}" >"${zero_denied}"
expect_fail zero_denied "${zero_denied}" "public_summary.denied_count must be positive"

failed_step="${tmp_dir}/failed-step.json"
jq '(.proof_steps[] | select(.step == "denied_access") | .status) = "fail"' "${good_manifest}" >"${failed_step}"
expect_fail failed_step "${failed_step}" "proof step denied_access must pass"

printf 'okta saml live proof tests passed\n'
