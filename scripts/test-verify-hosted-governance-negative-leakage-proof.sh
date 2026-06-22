#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-negative-leakage-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

surfaces=(
	facts
	logs
	metric_labels
	status_errors
	graph_properties
	api_bodies
	mcp_bodies
	console_surfaces
	audit_events
	generated_docs
	onboarding_artifacts
)

write_safe_bundle() {
	local dir="$1"
	mkdir -p "${dir}/artifacts"
	local surface
	for surface in "${surfaces[@]}"; do
		cat >"${dir}/artifacts/${surface}.txt" <<EOF
surface=${surface}
status=redacted
decision_class=allowed_public_summary
record_count=2
EOF
	done
	jq -n --arg generated_at "2026-06-10T00:00:00Z" '
		{
			schema_version: 1,
			proof_id: "negative-leakage-proof-test",
			generated_at: $generated_at,
			canaries: [
				"sensitive_canary_alpha",
				"sensitive_canary_beta"
			],
			surfaces: [
				{surface: "facts", artifact: "artifacts/facts.txt", record_count: 2},
				{surface: "logs", artifact: "artifacts/logs.txt", record_count: 2},
				{surface: "metric_labels", artifact: "artifacts/metric_labels.txt", record_count: 2},
				{surface: "status_errors", artifact: "artifacts/status_errors.txt", record_count: 2},
				{surface: "graph_properties", artifact: "artifacts/graph_properties.txt", record_count: 2},
				{surface: "api_bodies", artifact: "artifacts/api_bodies.txt", record_count: 2},
				{surface: "mcp_bodies", artifact: "artifacts/mcp_bodies.txt", record_count: 2},
				{surface: "console_surfaces", artifact: "artifacts/console_surfaces.txt", record_count: 2},
				{surface: "audit_events", artifact: "artifacts/audit_events.txt", record_count: 2},
				{surface: "generated_docs", artifact: "artifacts/generated_docs.txt", record_count: 2},
				{surface: "onboarding_artifacts", artifact: "artifacts/onboarding_artifacts.txt", record_count: 2}
			]
		}
	' >"${dir}/manifest.json"
}

expect_pass() {
	local label="$1"
	local manifest="$2"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if ! "${verifier}" --manifest "${manifest}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	jq -e '
		.status == "pass" and
		.proof_id == "negative-leakage-proof-test" and
		.canary_count == 2 and
		.surface_count == 11 and
		(.surfaces | length == 11) and
		([.surfaces[].artifact_sha256 | type == "string" and length == 64] | all)
	' "${out_json}" >/dev/null || {
		printf 'expected public-safe summary fields for %s\n' "${label}" >&2
		jq . "${out_json}" >&2
		exit 1
	}
	rg --fixed-strings --quiet 'Hosted governance negative leakage proof' "${out_md}"
}

expect_fail() {
	local label="$1"
	local manifest="$2"
	local expected="$3"
	local out_json="${tmp_dir}/${label}.summary.json"
	local out_md="${tmp_dir}/${label}.summary.md"
	if "${verifier}" --manifest "${manifest}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" || {
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	}
}

safe_dir="${tmp_dir}/safe"
write_safe_bundle "${safe_dir}"
expect_pass safe "${safe_dir}/manifest.json"

missing_surface_dir="${tmp_dir}/missing-surface"
cp -R "${safe_dir}" "${missing_surface_dir}"
jq 'del(.surfaces[] | select(.surface == "audit_events"))' "${safe_dir}/manifest.json" >"${missing_surface_dir}/manifest.json"
expect_fail missing_surface "${missing_surface_dir}/manifest.json" "missing required surfaces: audit_events"

canary_dir="${tmp_dir}/canary"
cp -R "${safe_dir}" "${canary_dir}"
printf 'leaked=sensitive_canary_alpha\n' >>"${canary_dir}/artifacts/api_bodies.txt"
expect_fail canary "${canary_dir}/manifest.json" "declared canary leaked in surface api_bodies"

private_shape_dir="${tmp_dir}/private-shape"
cp -R "${safe_dir}" "${private_shape_dir}"
printf 'db=postgresql://user:pass@example.invalid/db\n' >>"${private_shape_dir}/artifacts/logs.txt"
expect_fail private_shape "${private_shape_dir}/manifest.json" "private-shaped value leaked in surface logs"

bad_path_dir="${tmp_dir}/bad-path"
cp -R "${safe_dir}" "${bad_path_dir}"
jq '(.surfaces[] | select(.surface == "facts") | .artifact) = "../facts.txt"' "${safe_dir}/manifest.json" >"${bad_path_dir}/manifest.json"
expect_fail bad_path "${bad_path_dir}/manifest.json" "artifact paths must stay relative to the manifest directory"

printf 'hosted governance negative leakage proof tests passed\n'
