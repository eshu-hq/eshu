#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-helm-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

install_fake_helm() {
	local bin_dir="$1"
	mkdir -p "${bin_dir}"
	# The fake helm implementation lives in a sibling data file, not a
	# heredoc: Homebrew bash >= 5.1 writes an entire heredoc body to a pipe
	# before forking the reader, and macOS's 512-byte pipe buffer deadlocks
	# on this 2929-byte body (#5074). The body has no expansion (it was a
	# quoted <<'SH'), so copying the file is behavior-identical; its own
	# inner "template" heredoc (2199B) is converted to printf inside that
	# file too, since a >512B heredoc in a fixture re-trips the same
	# deadlock when the fixture itself runs.
	cat "${repo_root}/scripts/lib/test-verify-hosted-governance-helm-proof-fake-helm.sh" >"${bin_dir}/helm"
	chmod +x "${bin_dir}/helm"
}

write_values() {
	local path="$1"
	local mode="$2"
	# printf, not a heredoc: this 639-byte body sits in the 512B-64KB
	# Homebrew-bash-5.1+ pipe-deadlock zone (#5074). It expands "${mode}" on
	# one line, so it cannot move to a static scripts/lib/ data file; that
	# line is double-quoted and every other line is single-quoted.
	printf '%s\n' \
		'networkPolicy:' \
		'  egress:' \
		"    mode: ${mode}" \
		'    datastores:' \
		'      to:' \
		'        - podSelector:' \
		'            matchLabels:' \
		'              egress.eshu.io/class: datastore' \
		'api:' \
		'  env:' \
		'    ESHU_GOVERNANCE_MODE: hosted_single_tenant' \
		'    ESHU_GOVERNANCE_STATE: enforcing' \
		'    ESHU_GOVERNANCE_SOURCE_KIND: kubernetes_secret' \
		'    ESHU_GOVERNANCE_AUTH_MODE: shared_token' \
		'    ESHU_GOVERNANCE_EGRESS_MODE: restricted' \
		'mcpServer:' \
		'  env:' \
		'    ESHU_GOVERNANCE_MODE: hosted_single_tenant' \
		'    ESHU_GOVERNANCE_STATE: enforcing' \
		'    ESHU_GOVERNANCE_SOURCE_KIND: kubernetes_secret' \
		'    ESHU_GOVERNANCE_AUTH_MODE: shared_token' \
		'    ESHU_GOVERNANCE_EGRESS_MODE: restricted' \
		>"${path}"
}

bin_dir="${tmp_dir}/bin"
install_fake_helm "${bin_dir}"
valid_values="${tmp_dir}/governance-values.yaml"
write_values "${valid_values}" "restricted"

out_dir="${tmp_dir}/proof"
if ! PATH="${bin_dir}:${PATH}" "${verifier}" \
	--out-dir "${out_dir}" \
	--values "${valid_values}" \
	> "${tmp_dir}/valid.out" 2>"${tmp_dir}/valid.err"; then
	printf 'expected valid governance Helm proof to pass\n' >&2
	sed -n '1,120p' "${tmp_dir}/valid.out" >&2
	sed -n '1,120p' "${tmp_dir}/valid.err" >&2
	exit 1
fi

artifact="${out_dir}/hosted-governance-helm-proof.json"
summary="${out_dir}/hosted-governance-helm-proof.md"
[[ -f "${artifact}" && -f "${summary}" ]] || {
	printf 'expected hosted governance Helm proof artifacts\n' >&2
	sed -n '1,120p' "${tmp_dir}/valid.err" >&2
	exit 1
}

jq -e '
	.status == "pass" and
	.helm_rollout_status == "pass" and
	.security_posture_status == "pass" and
	.network_policy_status == "pass" and
	.governance_status_env.api == "pass" and
	.governance_status_env.mcp == "pass" and
	.public_artifact_review == "pass"
' "${artifact}" >/dev/null || {
	printf 'expected public-safe proof fields\n' >&2
	jq . "${artifact}" >&2
	exit 1
}
rg --fixed-strings --quiet 'Hosted governance Helm proof' "${summary}"

broad_values="${tmp_dir}/broad-values.yaml"
write_values "${broad_values}" "broad"
if PATH="${bin_dir}:${PATH}" "${verifier}" --out-dir "${tmp_dir}/broad" --values "${broad_values}" \
	>"${tmp_dir}/broad.out" 2>"${tmp_dir}/broad.err"; then
	printf 'expected broad egress values to fail\n' >&2
	exit 1
fi
rg --fixed-strings --quiet 'networkPolicy.egress.mode must be restricted' "${tmp_dir}/broad.err"

missing_values_out="${tmp_dir}/missing-values"
if PATH="${bin_dir}:${PATH}" "${verifier}" --out-dir "${missing_values_out}" \
	>"${tmp_dir}/missing.out" 2>"${tmp_dir}/missing.err"; then
	printf 'expected missing values to fail\n' >&2
	exit 1
fi
rg --fixed-strings --quiet 'at least one --values file is required' "${tmp_dir}/missing.err"

printf 'hosted governance Helm proof verifier tests passed\n'
